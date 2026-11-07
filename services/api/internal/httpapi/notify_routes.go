package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

func (a *API) registerNotificationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/me/notifications", a.signedIn(a.listMyNotifications))
	mux.HandleFunc("GET /api/v1/me/notifications/unread", a.signedIn(a.countMyUnread))
	mux.HandleFunc("POST /api/v1/me/notifications/read", a.signedIn(a.markNotificationsRead))
	mux.HandleFunc("GET /api/v1/me/notification-settings", a.signedIn(a.getNotificationSettings))
	mux.HandleFunc("PUT /api/v1/me/notification-settings", a.signedIn(a.saveNotificationSettings))
	mux.HandleFunc("POST /api/v1/me/telegram-link", a.signedIn(a.startTelegramLink))
	mux.HandleFunc("POST /api/v1/me/telegram-link/check", a.signedIn(a.checkTelegramLink))
	mux.HandleFunc("DELETE /api/v1/me/telegram-link", a.signedIn(a.removeTelegramLink))
	mux.HandleFunc("GET /api/v1/notifications/overview", a.admin(a.notificationOverview))
	mux.HandleFunc("PUT /api/v1/notifications/rules", a.admin(a.saveNotificationRules))
	mux.HandleFunc("GET /api/v1/notifications/deliveries", a.admin(a.listDeliveries))
	mux.HandleFunc("POST /api/v1/notifications/deliveries/{deliveryID}/retry", a.admin(a.retryDelivery))
	mux.HandleFunc("POST /api/v1/notifications/test", a.admin(a.sendTestMessage))
	mux.HandleFunc("GET /api/v1/announcements", a.admin(a.listAnnouncements))
	mux.HandleFunc("POST /api/v1/announcements", a.admin(a.createAnnouncement))
}

type notificationItem struct {
	ID        string `json:"id"`
	Topic     string `json:"topic"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Link      string `json:"link"`
	CreatedAt string `json:"created_at"`
	Read      bool   `json:"read"`
}

// listMyNotifications returns the newest notices, written in the language the
// app is showing.
func (a *API) listMyNotifications(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	limit := 30
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = value
	}
	school, err := loadSchoolNoticeContext(r.Context(), a.db, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	lang := r.URL.Query().Get("lang")
	rows, err := a.db.Query(r.Context(), `SELECT id::text,topic,kind,params,link,created_at,read_at IS NOT NULL FROM notifications
		WHERE user_id=$1 AND school_id=$2 ORDER BY created_at DESC LIMIT $3`, id.UserID, id.SchoolID, limit)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []notificationItem{}
	for rows.Next() {
		var item notificationItem
		var params map[string]string
		var created time.Time
		if err := rows.Scan(&item.ID, &item.Topic, &item.Kind, &params, &item.Link, &created, &item.Read); err != nil {
			a.serverError(w, r, err)
			return
		}
		item.Title, item.Body = renderNotice(item.Kind, params, lang, school.Location)
		item.CreatedAt = rfc3339(created)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	unread, err := a.unreadCount(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"items": items, "unread": unread}})
}

func (a *API) unreadCount(ctx context.Context, id auth.Identity) (int, error) {
	var count int
	err := a.db.QueryRow(ctx, `SELECT count(*)::int FROM notifications WHERE user_id=$1 AND school_id=$2 AND read_at IS NULL`, id.UserID, id.SchoolID).Scan(&count)
	return count, err
}

func (a *API) countMyUnread(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	count, err := a.unreadCount(r.Context(), id)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]int{"unread": count}})
}

func (a *API) markNotificationsRead(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		IDs []string `json:"ids"`
		All bool     `json:"all"`
	}
	if !decode(w, r, &req) || (!req.All && (len(req.IDs) == 0 || len(req.IDs) > 100)) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose notifications to mark as read.", nil)
		return
	}
	for _, value := range req.IDs {
		if !uuidPattern.MatchString(value) {
			writeError(w, 400, "VALIDATION_ERROR", "A notification could not be read.", nil)
			return
		}
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE notifications SET read_at=now() WHERE user_id=$1 AND school_id=$2 AND read_at IS NULL AND ($3 OR id=ANY($4::uuid[]))`, id.UserID, id.SchoolID, req.All, req.IDs); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.countMyUnread(w, r, id)
}

type channelState struct {
	Channel  string `json:"channel"`
	Ready    bool   `json:"ready"`
	Provider string `json:"provider"`
}

func (a *API) channelStates() []channelState {
	return []channelState{
		{"email", a.notify.ready("email"), a.notify.emailProvider},
		{"sms", a.notify.ready("sms"), a.notify.smsProvider},
		{"telegram", a.notify.ready("telegram"), map[bool]string{true: "Telegram", false: ""}[a.notify.ready("telegram")]},
	}
}

func (a *API) getNotificationSettings(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var out struct {
		Language          string         `json:"language"`
		EmailEnabled      bool           `json:"email_enabled"`
		SMSEnabled        bool           `json:"sms_enabled"`
		TelegramEnabled   bool           `json:"telegram_enabled"`
		MutedTopics       []string       `json:"muted_topics"`
		Email             string         `json:"email"`
		Phone             string         `json:"phone"`
		TelegramConnected bool           `json:"telegram_connected"`
		Channels          []channelState `json:"channels"`
	}
	err := a.db.QueryRow(r.Context(), `SELECT COALESCE(ns.language,s.default_language),COALESCE(ns.email_enabled,true),COALESCE(ns.sms_enabled,true),COALESCE(ns.telegram_enabled,true),
		COALESCE(ns.muted_topics,'{}'),COALESCE(u.email,''),COALESCE(NULLIF(u.phone,''),(SELECT g.phone FROM guardians g WHERE g.user_id=u.id ORDER BY g.is_primary DESC LIMIT 1),''),
		COALESCE(ns.telegram_chat_id,'')<>''
		FROM users u JOIN schools s ON s.id=u.school_id LEFT JOIN notification_settings ns ON ns.user_id=u.id WHERE u.id=$1 AND u.school_id=$2`, id.UserID, id.SchoolID).
		Scan(&out.Language, &out.EmailEnabled, &out.SMSEnabled, &out.TelegramEnabled, &out.MutedTopics, &out.Email, &out.Phone, &out.TelegramConnected)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	out.Channels = a.channelStates()
	writeJSON(w, 200, map[string]any{"data": out})
}

func (a *API) saveNotificationSettings(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Language        string   `json:"language"`
		EmailEnabled    bool     `json:"email_enabled"`
		SMSEnabled      bool     `json:"sms_enabled"`
		TelegramEnabled bool     `json:"telegram_enabled"`
		MutedTopics     []string `json:"muted_topics"`
		Phone           *string  `json:"phone"`
	}
	if !decode(w, r, &req) || (req.Language != "en" && req.Language != "am") || len(req.MutedTopics) > len(notificationTopics) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a language and the topics to mute.", nil)
		return
	}
	for _, topic := range req.MutedTopics {
		if topic == "account" || !slices.Contains(notificationTopics, topic) {
			writeError(w, 400, "VALIDATION_ERROR", "A topic could not be read.", nil)
			return
		}
	}
	phone := ""
	if req.Phone != nil && strings.TrimSpace(*req.Phone) != "" {
		normalized, ok := normalizePhone(*req.Phone)
		if !ok {
			writeError(w, 400, "INVALID_PHONE", "Enter a phone number such as 0911 234 567 or +251 911 234 567.", nil)
			return
		}
		phone = normalized
	}
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		if _, err := tx.Exec(r.Context(), `INSERT INTO notification_settings(user_id,school_id,language,email_enabled,sms_enabled,telegram_enabled,muted_topics)
			VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (user_id) DO UPDATE SET language=EXCLUDED.language,email_enabled=EXCLUDED.email_enabled,
			sms_enabled=EXCLUDED.sms_enabled,telegram_enabled=EXCLUDED.telegram_enabled,muted_topics=EXCLUDED.muted_topics,updated_at=now()`,
			id.UserID, id.SchoolID, req.Language, req.EmailEnabled, req.SMSEnabled, req.TelegramEnabled, slicesCompactSorted(append([]string{}, req.MutedTopics...))); err != nil {
			return false, err
		}
		if req.Phone != nil {
			if _, err := tx.Exec(r.Context(), `UPDATE users SET phone=NULLIF($3,'') WHERE id=$1 AND school_id=$2`, id.UserID, id.SchoolID, phone); err != nil {
				return false, err
			}
		}
		return false, nil
	}) {
		return
	}
	a.getNotificationSettings(w, r, id)
}

// startTelegramLink makes a one-time code that connects the person's Telegram
// chat when they press Start in the bot.
func (a *API) startTelegramLink(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if !a.notify.ready("telegram") {
		writeError(w, 409, "CHANNEL_NOT_SET_UP", "Telegram is not set up for this service yet.", nil)
		return
	}
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		a.serverError(w, r, err)
		return
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(code))
	expires := time.Now().Add(30 * time.Minute)
	if _, err := a.db.Exec(r.Context(), `INSERT INTO notification_settings(user_id,school_id,telegram_link_hash,telegram_link_expires_at) VALUES($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE SET telegram_link_hash=EXCLUDED.telegram_link_hash,telegram_link_expires_at=EXCLUDED.telegram_link_expires_at,updated_at=now()`,
		id.UserID, id.SchoolID, hash[:], expires); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"url": telegramLink(a.notify.telegramBot, code), "expires_at": rfc3339(expires)}})
}

func (a *API) checkTelegramLink(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if a.notify.ready("telegram") {
		if _, err := a.readTelegram(r.Context()); err != nil {
			a.log.Warn("reading telegram failed", "error", err)
		}
	}
	var connected bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM notification_settings WHERE user_id=$1 AND COALESCE(telegram_chat_id,'')<>'')`, id.UserID).Scan(&connected); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"connected": connected}})
}

func (a *API) removeTelegramLink(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if _, err := a.db.Exec(r.Context(), `UPDATE notification_settings SET telegram_chat_id=NULL,telegram_link_hash=NULL,updated_at=now() WHERE user_id=$1`, id.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"connected": false}})
}

func (a *API) notificationOverview(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	school, err := loadSchoolNoticeContext(r.Context(), a.db, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type count struct {
		Channel string `json:"channel"`
		Status  string `json:"status"`
		Count   int    `json:"count"`
	}
	rows, err := a.db.Query(r.Context(), `SELECT channel,status,count(*)::int FROM notification_deliveries WHERE school_id=$1 AND created_at > now() - interval '30 days' GROUP BY channel,status ORDER BY channel,status`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	counts := []count{}
	for rows.Next() {
		var c count
		if err := rows.Scan(&c.Channel, &c.Status, &c.Count); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		counts = append(counts, c)
	}
	rows.Close()
	var smsThisMonth int
	if err := a.db.QueryRow(r.Context(), `SELECT count(*)::int FROM notification_deliveries WHERE school_id=$1 AND channel='sms' AND status='sent' AND sent_at >= date_trunc('month', now() AT TIME ZONE $2) AT TIME ZONE $2`,
		id.SchoolID, school.Location.String()).Scan(&smsThisMonth); err != nil {
		a.serverError(w, r, err)
		return
	}
	emailTopics := slices.DeleteFunc(slices.Clone(school.Topics["email"]), func(t string) bool { return t == "account" })
	writeJSON(w, 200, map[string]any{"data": map[string]any{
		"channels":       a.channelStates(),
		"links_ready":    a.notify.webURL != "",
		"rules":          map[string][]string{"email": slicesCompactSorted(emailTopics), "sms": school.Topics["sms"], "telegram": school.Topics["telegram"]},
		"last_30_days":   counts,
		"sms_this_month": smsThisMonth,
	}})
}

func validTopics(topics []string) bool {
	for _, topic := range topics {
		if topic == "account" || !slices.Contains(notificationTopics, topic) {
			return false
		}
	}
	return len(topics) <= len(notificationTopics)
}

func (a *API) saveNotificationRules(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Email    []string `json:"email"`
		SMS      []string `json:"sms"`
		Telegram []string `json:"telegram"`
	}
	if !decode(w, r, &req) || !validTopics(req.Email) || !validTopics(req.SMS) || !validTopics(req.Telegram) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose topics for each channel.", nil)
		return
	}
	clean := func(values []string) []string { return slicesCompactSorted(append([]string{}, values...)) }
	if !a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		if _, err := tx.Exec(r.Context(), `INSERT INTO school_notification_rules(school_id,email_topics,sms_topics,telegram_topics) VALUES($1,$2,$3,$4)
			ON CONFLICT (school_id) DO UPDATE SET email_topics=EXCLUDED.email_topics,sms_topics=EXCLUDED.sms_topics,telegram_topics=EXCLUDED.telegram_topics,updated_at=now()`,
			id.SchoolID, clean(req.Email), clean(req.SMS), clean(req.Telegram)); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "notifications.rules_saved", "school", id.SchoolID, map[string]any{"email": clean(req.Email), "sms": clean(req.SMS), "telegram": clean(req.Telegram)})
	}) {
		return
	}
	a.notificationOverview(w, r, id)
}

func (a *API) listDeliveries(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	status, channel := r.URL.Query().Get("status"), r.URL.Query().Get("channel")
	if (status != "" && !slices.Contains([]string{"pending", "sending", "sent", "failed", "cancelled", "skipped"}, status)) || (channel != "" && !slices.Contains(notificationChannels, channel)) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a status and a channel.", nil)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT d.id::text,d.channel,d.address,d.topic,d.kind,d.subject,CASE WHEN d.sensitive THEN '' ELSE d.body END,d.status,d.attempts,d.last_error,d.created_at,d.sent_at,
		COALESCE(u.display_name,g.full_name,'')
		FROM notification_deliveries d LEFT JOIN users u ON u.id=d.user_id LEFT JOIN guardians g ON g.id=d.guardian_id
		WHERE d.school_id=$1 AND ($2='' OR d.status=$2) AND ($3='' OR d.channel=$3) ORDER BY d.created_at DESC LIMIT 100`, id.SchoolID, status, channel)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID        string `json:"id"`
		Channel   string `json:"channel"`
		Address   string `json:"address"`
		Topic     string `json:"topic"`
		Kind      string `json:"kind"`
		Subject   string `json:"subject"`
		Body      string `json:"body"`
		Status    string `json:"status"`
		Attempts  int    `json:"attempts"`
		Error     string `json:"error"`
		CreatedAt string `json:"created_at"`
		SentAt    string `json:"sent_at,omitempty"`
		Name      string `json:"name"`
	}
	items := []item{}
	for rows.Next() {
		var x item
		var created time.Time
		var sent *time.Time
		if err := rows.Scan(&x.ID, &x.Channel, &x.Address, &x.Topic, &x.Kind, &x.Subject, &x.Body, &x.Status, &x.Attempts, &x.Error, &created, &sent, &x.Name); err != nil {
			a.serverError(w, r, err)
			return
		}
		x.CreatedAt = rfc3339(created)
		if sent != nil {
			x.SentAt = rfc3339(*sent)
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

// retryDelivery sends a failed message again. Messages that carried a sign-in
// link cannot be retried, because the link is not kept; make a new link.
func (a *API) retryDelivery(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	tag, err := a.db.Exec(r.Context(), `UPDATE notification_deliveries SET status='pending',attempts=0,last_error='',send_after=now(),expires_at=now()+interval '1 day',claimed_at=NULL
		WHERE id=$1 AND school_id=$2 AND status IN ('failed','skipped') AND NOT sensitive AND body<>''`, r.PathValue("deliveryID"), id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 409, "CANNOT_RETRY", "Only failed or skipped messages without a sign-in link can be sent again.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"queued": true}})
}

// sendTestMessage sends one message straight away, so the school can see a
// channel work. Email goes to the administrator's own address unless another
// is given; Telegram goes to their connected chat.
func (a *API) sendTestMessage(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Channel string `json:"channel"`
		Address string `json:"address"`
	}
	if !decode(w, r, &req) || !slices.Contains(notificationChannels, req.Channel) || len(req.Address) > 320 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a channel.", nil)
		return
	}
	s := a.notify.senderFor(req.Channel)
	if s == nil {
		writeError(w, 409, "CHANNEL_NOT_SET_UP", "This channel is not set up for this service yet.", nil)
		return
	}
	school, err := loadSchoolNoticeContext(r.Context(), a.db, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var email, phone, chat, language string
	if err := a.db.QueryRow(r.Context(), `SELECT COALESCE(u.email,''),COALESCE(u.phone,''),COALESCE(ns.telegram_chat_id,''),COALESCE(ns.language,'') FROM users u LEFT JOIN notification_settings ns ON ns.user_id=u.id WHERE u.id=$1`, id.UserID).Scan(&email, &phone, &chat, &language); err != nil {
		a.serverError(w, r, err)
		return
	}
	if language == "" {
		language = school.Language
	}
	address := strings.TrimSpace(req.Address)
	switch req.Channel {
	case "email":
		if address == "" {
			address = email
		}
		if !strings.Contains(address, "@") {
			writeError(w, 400, "VALIDATION_ERROR", "Enter an email address.", nil)
			return
		}
	case "sms":
		if address == "" {
			address = phone
		}
		normalized, ok := normalizePhone(address)
		if !ok {
			writeError(w, 400, "INVALID_PHONE", "Enter a phone number such as 0911 234 567 or +251 911 234 567.", nil)
			return
		}
		address = normalized
	case "telegram":
		address = chat
		if address == "" {
			writeError(w, 409, "TELEGRAM_NOT_CONNECTED", "Connect your own Telegram first, under Notifications in your account.", nil)
			return
		}
	}
	title, body := renderNotice("test", map[string]string{"school": school.Name, "channel": req.Channel}, language, school.Location)
	subject, text := outsideText(req.Channel, school.Name, title, body, "")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ref, sendErr := s.Send(ctx, outsideMessage{Channel: req.Channel, Address: address, Subject: subject, Body: text})
	status, reason := "sent", ""
	if sendErr != nil {
		status, reason = "failed", truncateRunes(sendErr.Error(), 300)
	}
	if _, err := a.db.Exec(r.Context(), `INSERT INTO notification_deliveries(school_id,user_id,topic,kind,channel,address,subject,body,status,attempts,last_error,provider_ref,sent_at)
		VALUES($1,$2,'account','test',$3,$4,$5,$6,$7,1,$8,$9,CASE WHEN $7='sent' THEN now() END)`, id.SchoolID, id.UserID, req.Channel, address, subject, text, status, reason, truncateRunes(ref, 200)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if sendErr != nil {
		writeError(w, 502, "SEND_FAILED", "The message was not accepted: "+reason, nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"address": address, "status": status}})
}

// announcementAudience lists who an announcement reaches.
func announcementAudience(ctx context.Context, tx pgx.Tx, schoolID, audience, classID string) ([]recipient, error) {
	var people []recipient
	addUsers := func(sql string, args ...any) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p recipient
			if err := rows.Scan(&p.UserID); err != nil {
				return err
			}
			people = append(people, p)
		}
		return rows.Err()
	}
	addGuardians := func(classOnly bool) error {
		rows, err := tx.Query(ctx, `SELECT COALESCE(g.user_id::text,''),g.id::text,COALESCE(g.email,''),g.phone FROM guardians g JOIN students s ON s.id=g.student_id AND s.status='active'
			WHERE g.school_id=$1 AND (NOT $2 OR EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$3::uuid AND e.status='active'))
			ORDER BY g.is_primary DESC,g.created_at`, schoolID, classOnly, nullableUUID(classID))
		if err != nil {
			return err
		}
		defer rows.Close()
		phones := map[string]bool{}
		for rows.Next() {
			var p recipient
			if err := rows.Scan(&p.UserID, &p.GuardianID, &p.Email, &p.Phone); err != nil {
				return err
			}
			// Two children's guardian rows with the same phone get one message.
			if p.UserID == "" {
				if normalized, ok := normalizePhone(p.Phone); ok {
					if phones[normalized] {
						continue
					}
					phones[normalized] = true
				}
			}
			people = append(people, p)
		}
		return rows.Err()
	}
	staff := `SELECT DISTINCT u.id::text FROM users u JOIN user_roles ur ON ur.user_id=u.id AND ur.role IN ('school_admin','teacher') WHERE u.school_id=$1 AND u.status='active'`
	students := `SELECT s.user_id::text FROM students s WHERE s.school_id=$1 AND s.status='active' AND s.user_id IS NOT NULL`
	var err error
	switch audience {
	case "staff":
		err = addUsers(staff, schoolID)
	case "students":
		err = addUsers(students, schoolID)
	case "parents":
		err = addGuardians(false)
	case "everyone":
		if err = addUsers(staff, schoolID); err == nil {
			if err = addUsers(students, schoolID); err == nil {
				err = addGuardians(false)
			}
		}
	case "class":
		if err = addUsers(`SELECT s.user_id::text FROM students s JOIN enrollments e ON e.student_id=s.id AND e.class_id=$2 AND e.status='active' WHERE s.school_id=$1 AND s.status='active' AND s.user_id IS NOT NULL`, schoolID, classID); err == nil {
			err = addGuardians(true)
		}
	}
	return people, err
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type announcementRequest struct {
	Title    string   `json:"title"`
	Body     string   `json:"body"`
	Audience string   `json:"audience"`
	ClassID  string   `json:"class_id"`
	Channels []string `json:"channels"`
	DryRun   bool     `json:"dry_run"`
}

// createAnnouncement sends a message to a group. With dry_run it only counts
// who would get it on each channel, so the school sees what SMS will cost.
func (a *API) createAnnouncement(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req announcementRequest
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	a.sendAnnouncement(w, r, id, req)
}

// sendAnnouncement checks, counts or sends an announcement.
func (a *API) sendAnnouncement(w http.ResponseWriter, r *http.Request, id auth.Identity, req announcementRequest) {
	req.Title, req.Body = strings.TrimSpace(req.Title), strings.TrimSpace(req.Body)
	if req.Title == "" || len([]rune(req.Title)) > 120 || req.Body == "" || len([]rune(req.Body)) > 2000 {
		writeError(w, 400, "VALIDATION_ERROR", "Write a title of up to 120 characters and a message of up to 2,000.", nil)
		return
	}
	if !slices.Contains([]string{"everyone", "staff", "parents", "students", "class"}, req.Audience) || (req.Audience == "class") != uuidPattern.MatchString(req.ClassID) || len(req.Channels) > 3 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose who gets this message.", nil)
		return
	}
	for _, channel := range req.Channels {
		if !slices.Contains(notificationChannels, channel) {
			writeError(w, 400, "VALIDATION_ERROR", "A channel could not be read.", nil)
			return
		}
	}
	channels := slicesCompactSorted(append([]string{}, req.Channels...))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if req.Audience == "class" {
		if !a.requireClass(w, r, id, req.ClassID) {
			return
		}
	}
	var announcementID string
	if err := tx.QueryRow(r.Context(), `INSERT INTO announcements(school_id,title,body,audience,class_id,channels,created_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id::text`,
		id.SchoolID, req.Title, req.Body, req.Audience, nullableUUID(req.ClassID), channels, id.UserID).Scan(&announcementID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	people, err := announcementAudience(r.Context(), tx, id.SchoolID, req.Audience, req.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	reached, err := a.queueNotice(r.Context(), tx, id.SchoolID, notice{Topic: "announcements", Kind: "announcement", AnnouncementID: announcementID,
		DedupeKey: "ann:" + announcementID, Channels: channels, Params: map[string]string{"title": req.Title, "body": req.Body}}, people)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	byChannel := map[string]map[string]int{}
	rows, err := tx.Query(r.Context(), `SELECT channel,status,count(*)::int FROM notification_deliveries WHERE announcement_id=$1 GROUP BY channel,status`, announcementID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for rows.Next() {
		var channel, status string
		var count int
		if err := rows.Scan(&channel, &status, &count); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		if byChannel[channel] == nil {
			byChannel[channel] = map[string]int{}
		}
		byChannel[channel][status] = count
	}
	rows.Close()
	result := map[string]any{"id": announcementID, "recipients": reached, "channels": byChannel, "dry_run": req.DryRun}
	if req.DryRun {
		writeJSON(w, 200, map[string]any{"data": result})
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE announcements SET recipients=$2 WHERE id=$1`, announcementID, reached); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := audit(r.Context(), tx, id, "announcement.sent", "announcement", announcementID, map[string]any{"audience": req.Audience, "recipients": reached, "channels": channels}); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": result})
}

func (a *API) listAnnouncements(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT an.id::text,an.title,an.body,an.audience,COALESCE(c.name,''),an.channels,an.recipients,an.created_at,COALESCE(u.display_name,''),
		COALESCE((SELECT jsonb_object_agg(k,v) FROM (SELECT d.channel||'.'||d.status AS k,count(*) AS v FROM notification_deliveries d WHERE d.announcement_id=an.id GROUP BY d.channel,d.status) x),'{}'::jsonb)
		FROM announcements an LEFT JOIN classes c ON c.id=an.class_id LEFT JOIN users u ON u.id=an.created_by
		WHERE an.school_id=$1 ORDER BY an.created_at DESC LIMIT 50`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID         string         `json:"id"`
		Title      string         `json:"title"`
		Body       string         `json:"body"`
		Audience   string         `json:"audience"`
		ClassName  string         `json:"class_name"`
		Channels   []string       `json:"channels"`
		Recipients int            `json:"recipients"`
		CreatedAt  string         `json:"created_at"`
		SentBy     string         `json:"sent_by"`
		Deliveries map[string]int `json:"deliveries"`
	}
	items := []item{}
	for rows.Next() {
		var x item
		var created time.Time
		if err := rows.Scan(&x.ID, &x.Title, &x.Body, &x.Audience, &x.ClassName, &x.Channels, &x.Recipients, &created, &x.SentBy, &x.Deliveries); err != nil {
			a.serverError(w, r, err)
			return
		}
		x.CreatedAt = rfc3339(created)
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
