package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	_ "time/tzdata" // school time zones on hosts without a zone database

	"github.com/jackc/pgx/v5"
)

// Notifications reach people in the app, and by email, SMS or Telegram when the
// service has that channel set up. The school decides which topics may use each
// outside channel; each person can turn channels off or mute topics. Without any
// channel set up everything still appears in the app, and the outside messages
// are recorded as skipped so the school can see what would have been sent.

var notificationTopics = []string{"attendance", "reports", "schoolwork", "announcements", "account"}
var notificationChannels = []string{"email", "sms", "telegram"}

// notice is one event to tell people about.
type notice struct {
	Topic, Kind    string
	Params         map[string]string
	Link           string // a path in the web app, such as /family/children/<id>
	DedupeKey      string // the same key reaches a person once
	AnnouncementID string
	SendAfter      time.Time // zero means now
	Channels       []string  // outside channels to use; nil means the school's rules
}

// recipient is a person to reach. A guardian without an account has no UserID
// and is reached by phone or email only.
type recipient struct {
	UserID     string
	GuardianID string
	Email      string
	Phone      string
}

type noticeTemplate struct{ Title, Body string }

// noticeTemplates hold every message in each language. {name} is replaced by a
// detail; {date} and {due} are written in the reader's calendar.
var noticeTemplates = map[string]map[string]noticeTemplate{
	"attendance.absent": {
		"en": {"{student} was marked absent", "{student} was marked absent from {class} on {date}. If you did not expect this, please contact the school."},
		"am": {"{student} በዛሬው ዕለት አልተገኙም", "{student} {date} በ{class} ክፍል እንዳልተገኙ ተመዝግቧል። ያልጠበቁት ከሆነ እባክዎ ትምህርት ቤቱን ያነጋግሩ።"},
	},
	"reports.published": {
		"en": {"{student}'s report card is ready", "The report card for {term} has been published. Open FURII School OS to read it."},
		"am": {"የ{student} የውጤት ካርድ ዝግጁ ነው", "የ{term} የውጤት ካርድ ታትሟል። ለማየት FURII School OSን ይክፈቱ።"},
	},
	"schoolwork.quiz": {
		"en": {"New quiz: {title}", "{subject} quiz for {class}, due {due}."},
		"am": {"አዲስ ፈተና፦ {title}", "የ{subject} ፈተና ለ{class}፣ የመጨረሻ ጊዜ {due}።"},
	},
	"schoolwork.assignment": {
		"en": {"New assignment: {title}", "{subject} assignment for {class}, due {due}."},
		"am": {"አዲስ የቤት ሥራ፦ {title}", "የ{subject} የቤት ሥራ ለ{class}፣ የመጨረሻ ጊዜ {due}።"},
	},
	"schoolwork.due_soon": {
		"en": {"Due soon: {title}", "{title} ({subject}) is due {due} and has not been handed in yet."},
		"am": {"ጊዜው እየደረሰ ነው፦ {title}", "{title} ({subject}) እስከ {due} መሰጠት አለበት፤ እስካሁን አልተሰጠም።"},
	},
	"announcement": {
		"en": {"{title}", "{body}"},
		"am": {"{title}", "{body}"},
	},
	"account.staff_invite": {
		"en": {"You're invited to {school}", "{school} has invited you to FURII School OS. Open this link to choose your name and password. It works once and expires {due}."},
		"am": {"ወደ {school} ተጋብዘዋል", "{school} ወደ FURII School OS ጋብዞዎታል። ስምዎንና የይለፍ ቃልዎን ለመምረጥ ይህን ሊንክ ይክፈቱ። አንድ ጊዜ ብቻ ይሠራል፤ {due} ጊዜው ያልፋል።"},
	},
	"account.student_invite": {
		"en": {"Your {school} student account", "{school} has made you a student account on FURII School OS. Open this link to choose your password. It works once and expires {due}."},
		"am": {"የ{school} የተማሪ መለያዎ", "{school} በFURII School OS የተማሪ መለያ ከፍቶልዎታል። የይለፍ ቃልዎን ለመምረጥ ይህን ሊንክ ይክፈቱ። አንድ ጊዜ ብቻ ይሠራል፤ {due} ጊዜው ያልፋል።"},
	},
	"account.parent_invite": {
		"en": {"Follow your child at {school}", "{school} has invited you to its family portal on FURII School OS, to see attendance, marks and report cards. Open this link to choose your password. It works once and expires {due}."},
		"am": {"ልጅዎን በ{school} ይከታተሉ", "{school} ክትትልን፣ ውጤትንና የውጤት ካርድን ለማየት ወደ FURII School OS የቤተሰብ መግቢያ ጋብዞዎታል። የይለፍ ቃልዎን ለመምረጥ ይህን ሊንክ ይክፈቱ። አንድ ጊዜ ብቻ ይሠራል፤ {due} ጊዜው ያልፋል።"},
	},
	"account.password_reset": {
		"en": {"Choose a new FURII School OS password", "{school} made a link for you to choose a new password. It works once and expires {due}. If you did not ask for this, tell the school."},
		"am": {"አዲስ የFURII School OS የይለፍ ቃል ይምረጡ", "{school} አዲስ የይለፍ ቃል እንዲመርጡ ሊንክ አዘጋጅቶልዎታል። አንድ ጊዜ ብቻ ይሠራል፤ {due} ጊዜው ያልፋል። ይህን ካልጠየቁ ለትምህርት ቤቱ ያሳውቁ።"},
	},
	"test": {
		"en": {"Test message from FURII School OS", "{school} sent this to check that {channel} messages arrive. No action is needed."},
		"am": {"ከFURII School OS የሙከራ መልእክት", "{school} የ{channel} መልእክቶች መድረሳቸውን ለማረጋገጥ ይህን ልኳል። ምንም ማድረግ አያስፈልግም።"},
	},
}

var ethiopianMonths = []string{"መስከረም", "ጥቅምት", "ኅዳር", "ታኅሣሥ", "ጥር", "የካቲት", "መጋቢት", "ሚያዝያ", "ግንቦት", "ሰኔ", "ሐምሌ", "ነሐሴ", "ጳጉሜን"}

// ethiopianDate converts a Gregorian date through its Julian day number.
func ethiopianDate(t time.Time) (year, month, day int) {
	y, m, d := t.Date()
	a := (14 - int(m)) / 12
	yy := y + 4800 - a
	mm := int(m) + 12*a - 3
	jdn := d + (153*mm+2)/5 + 365*yy + yy/4 - yy/100 + yy/400 - 32045
	const epoch = 1724221
	year = (4*(jdn-epoch) + 1463) / 1461
	start := epoch + 365*(year-1) + year/4
	dayOfYear := jdn - start
	return year, dayOfYear/30 + 1, dayOfYear%30 + 1
}

func formatNoticeDate(t time.Time, lang string, withTime bool) string {
	if lang == "am" {
		y, m, d := ethiopianDate(t)
		out := fmt.Sprintf("%s %d፣ %d ዓ.ም.", ethiopianMonths[m-1], d, y)
		if withTime {
			out += " " + t.Format("15:04")
		}
		return out
	}
	if withTime {
		return t.Format("Mon 2 Jan 2006, 15:04")
	}
	return t.Format("Mon 2 Jan 2006")
}

func noticeLanguage(lang string) string {
	if lang == "am" {
		return "am"
	}
	return "en"
}

// renderNotice writes a notice's title and body in a language.
func renderNotice(kind string, params map[string]string, lang string, loc *time.Location) (string, string) {
	lang = noticeLanguage(lang)
	tpl, ok := noticeTemplates[kind][lang]
	if !ok {
		return kind, ""
	}
	if loc == nil {
		loc = time.UTC
	}
	pairs := make([]string, 0, len(params)*2)
	for key, value := range params {
		switch key {
		case "date":
			if t, err := time.Parse("2006-01-02", value); err == nil {
				value = formatNoticeDate(t, lang, false)
			}
		case "due":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				value = formatNoticeDate(t.In(loc), lang, true)
			}
		case "channel":
			value = channelName(value, lang)
		}
		pairs = append(pairs, "{"+key+"}", value)
	}
	replacer := strings.NewReplacer(pairs...)
	return replacer.Replace(tpl.Title), replacer.Replace(tpl.Body)
}

func channelName(channel, lang string) string {
	names := map[string][2]string{"email": {"email", "ኢሜይል"}, "sms": {"SMS", "ኤስኤምኤስ"}, "telegram": {"Telegram", "ቴሌግራም"}}
	name, ok := names[channel]
	if !ok {
		return channel
	}
	if lang == "am" {
		return name[1]
	}
	return name[0]
}

// outsideText writes the message for a channel. SMS is kept short; email and
// Telegram carry the link.
func outsideText(channel, school, title, body, link string) (string, string) {
	switch channel {
	case "sms":
		text := school + ": " + title
		if body != "" && body != title {
			text += ". " + body
		}
		return "", truncateRunes(text, 320)
	case "telegram":
		text := title + "\n\n" + body
		if link != "" {
			text += "\n\n" + link
		}
		return "", truncateRunes(text+"\n\n— "+school, 3500)
	default:
		text := body
		if link != "" {
			text += "\n\n" + link
		}
		return title, text + "\n\n— " + school + " · FURII School OS"
	}
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}

// normalizePhone writes an Ethiopian mobile number as +2519XXXXXXXX or
// +2517XXXXXXXX. Other numbers with a country code are kept as written.
func normalizePhone(raw string) (string, bool) {
	var digits strings.Builder
	for i, r := range strings.TrimSpace(raw) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == '+' && i == 0:
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '.':
		default:
			return "", false
		}
	}
	n := digits.String()
	switch {
	case len(n) == 10 && strings.HasPrefix(n, "0") && (n[1] == '9' || n[1] == '7'):
		return "+251" + n[1:], true
	case len(n) == 9 && (n[0] == '9' || n[0] == '7'):
		return "+251" + n, true
	case len(n) == 12 && strings.HasPrefix(n, "251"):
		return "+" + n, true
	case strings.HasPrefix(n, "+") && len(n) >= 9 && len(n) <= 16:
		return n, true
	}
	return "", false
}

// schoolNoticeContext is what every notice for a school needs.
type schoolNoticeContext struct {
	Name     string
	Language string
	Location *time.Location
	Topics   map[string][]string // channel -> topics the school allows
}

func loadSchoolNoticeContext(ctx context.Context, q reportQuerier, schoolID string) (schoolNoticeContext, error) {
	var x schoolNoticeContext
	var zone string
	var email, sms, telegram []string
	err := q.QueryRow(ctx, `SELECT s.name,s.default_language,s.timezone,
		COALESCE(r.email_topics,'{attendance,reports,schoolwork,announcements,account}'),
		COALESCE(r.sms_topics,'{attendance,reports}'),
		COALESCE(r.telegram_topics,'{attendance,reports,schoolwork,announcements}')
		FROM schools s LEFT JOIN school_notification_rules r ON r.school_id=s.id WHERE s.id=$1`, schoolID).Scan(&x.Name, &x.Language, &zone, &email, &sms, &telegram)
	if err != nil {
		return x, err
	}
	x.Location, err = time.LoadLocation(zone)
	if err != nil {
		x.Location = time.FixedZone("EAT", 3*60*60)
	}
	// Account messages carry sign-in links and always go by email.
	x.Topics = map[string][]string{"email": append(email, "account"), "sms": sms, "telegram": telegram}
	return x, nil
}

// smsQuietUntil moves an SMS out of the night, 21:00 to 07:00 school time.
func smsQuietUntil(at time.Time, loc *time.Location) time.Time {
	local := at.In(loc)
	if local.Hour() >= 7 && local.Hour() < 21 {
		return at
	}
	morning := time.Date(local.Year(), local.Month(), local.Day(), 7, 0, 0, 0, loc)
	if local.Hour() >= 21 {
		morning = morning.AddDate(0, 0, 1)
	}
	return morning
}

type personSettings struct {
	Language       string
	Email, Phone   string
	Active         bool
	EmailOn, SMSOn bool
	TelegramOn     bool
	Muted          []string
	TelegramChatID string
}

func loadPersonSettings(ctx context.Context, q reportQuerier, schoolID string, userIDs []string) (map[string]personSettings, error) {
	out := map[string]personSettings{}
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT u.id::text,COALESCE(ns.language,''),COALESCE(u.email,''),COALESCE(u.phone,''),u.status='active',
		COALESCE(ns.email_enabled,true),COALESCE(ns.sms_enabled,true),COALESCE(ns.telegram_enabled,true),COALESCE(ns.muted_topics,'{}'),COALESCE(ns.telegram_chat_id,'')
		FROM users u LEFT JOIN notification_settings ns ON ns.user_id=u.id WHERE u.id=ANY($1::uuid[]) AND u.school_id=$2`, userIDs, schoolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p personSettings
		if err := rows.Scan(&id, &p.Language, &p.Email, &p.Phone, &p.Active, &p.EmailOn, &p.SMSOn, &p.TelegramOn, &p.Muted, &p.TelegramChatID); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// queueNotice records the notice in each recipient's app inbox and queues the
// outside messages the school's rules and each person's choices allow. It
// returns how many people it reached. It runs inside the caller's transaction,
// so a notice exists only if the change that caused it was saved.
func (a *API) queueNotice(ctx context.Context, tx pgx.Tx, schoolID string, n notice, people []recipient) (int, error) {
	if len(people) == 0 {
		return 0, nil
	}
	school, err := loadSchoolNoticeContext(ctx, tx, schoolID)
	if err != nil {
		return 0, err
	}
	var userIDs []string
	for _, p := range people {
		if p.UserID != "" {
			userIDs = append(userIDs, p.UserID)
		}
	}
	settings, err := loadPersonSettings(ctx, tx, schoolID, userIDs)
	if err != nil {
		return 0, err
	}
	params, err := json.Marshal(n.Params)
	if err != nil {
		return 0, err
	}
	sendAfter := n.SendAfter
	if sendAfter.IsZero() {
		sendAfter = time.Now()
	}
	reached := 0
	seen := map[string]bool{}
	for _, p := range people {
		key := p.UserID
		if key == "" {
			key = "g:" + p.GuardianID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		language := school.Language
		email, phone, chat := p.Email, p.Phone, ""
		allowed := map[string]bool{"email": true, "sms": true, "telegram": false}
		var notificationID *string
		if p.UserID != "" {
			person, ok := settings[p.UserID]
			if !ok || !person.Active {
				continue
			}
			var nid string
			err := tx.QueryRow(ctx, `INSERT INTO notifications(school_id,user_id,topic,kind,params,link,dedupe_key,announcement_id)
				VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,'')::uuid)
				ON CONFLICT (user_id,dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING RETURNING id::text`,
				schoolID, p.UserID, n.Topic, n.Kind, params, n.Link, n.DedupeKey, n.AnnouncementID).Scan(&nid)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // already told
			}
			if err != nil {
				return reached, err
			}
			notificationID = &nid
			if person.Language != "" {
				language = person.Language
			}
			if person.Email != "" {
				email = person.Email
			}
			if person.Phone != "" {
				phone = person.Phone
			}
			chat = person.TelegramChatID
			muted := n.Topic != "account" && slices.Contains(person.Muted, n.Topic)
			allowed = map[string]bool{"email": person.EmailOn && !muted, "sms": person.SMSOn && !muted, "telegram": person.TelegramOn && !muted}
		}
		reached++
		title, body := renderNotice(n.Kind, n.Params, language, school.Location)
		link := ""
		if n.Link != "" {
			link = a.notify.webURL + n.Link
		}
		for _, channel := range notificationChannels {
			if !allowed[channel] {
				continue
			}
			if n.Channels != nil {
				if !slices.Contains(n.Channels, channel) {
					continue
				}
			} else if !slices.Contains(school.Topics[channel], n.Topic) {
				continue
			}
			address := ""
			switch channel {
			case "email":
				address = strings.TrimSpace(email)
			case "sms":
				address, _ = normalizePhone(phone)
			case "telegram":
				address = chat
			}
			if address == "" {
				continue
			}
			subject, text := outsideText(channel, school.Name, title, body, link)
			when := sendAfter
			if channel == "sms" {
				when = smsQuietUntil(when, school.Location)
			}
			if err := a.queueDelivery(ctx, tx, delivery{
				SchoolID: schoolID, NotificationID: notificationID, UserID: p.UserID, GuardianID: p.GuardianID, AnnouncementID: n.AnnouncementID,
				Topic: n.Topic, Kind: n.Kind, Channel: channel, Address: address, Subject: subject, Body: text, DedupeKey: n.DedupeKey,
				SendAfter: when, Expires: when.Add(24 * time.Hour),
			}); err != nil {
				return reached, err
			}
		}
	}
	return reached, nil
}

type delivery struct {
	SchoolID       string
	NotificationID *string
	UserID         string
	GuardianID     string
	AnnouncementID string
	Topic, Kind    string
	Channel        string
	Address        string
	Subject, Body  string
	Sensitive      bool
	DedupeKey      string
	SendAfter      time.Time
	Expires        time.Time
}

// queueDelivery stores one outside message. When the channel is not set up it
// is stored as skipped, and its text is dropped if it carries a sign-in link.
func (a *API) queueDelivery(ctx context.Context, tx pgx.Tx, d delivery) error {
	status, reason, body := "pending", "", d.Body
	if !a.notify.ready(d.Channel) {
		status, reason = "skipped", notSetUp
		if d.Sensitive {
			body = ""
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO notification_deliveries(school_id,notification_id,user_id,guardian_id,announcement_id,topic,kind,channel,address,subject,body,sensitive,dedupe_key,status,last_error,send_after,expires_at)
		VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid,$6,$7,$8,$9,$10,$11,$12,NULLIF($13,''),$14,$15,$16,$17)
		ON CONFLICT (school_id,channel,address,dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING`,
		d.SchoolID, d.NotificationID, d.UserID, d.GuardianID, d.AnnouncementID, d.Topic, d.Kind, d.Channel, d.Address, d.Subject, body, d.Sensitive, d.DedupeKey, status, reason, d.SendAfter, d.Expires)
	return err
}

const notSetUp = "Not set up"

// queueAccountEmail emails a sign-in link (an invitation or a password reset)
// when email is set up. It reports whether the email will be sent. The link is
// removed from the stored message once it is sent or given up on.
func (a *API) queueAccountEmail(ctx context.Context, tx pgx.Tx, schoolID, kind, email, hashLink string, expires time.Time) (bool, error) {
	if !a.notify.ready("email") || a.notify.webURL == "" || strings.TrimSpace(email) == "" {
		return false, nil
	}
	school, err := loadSchoolNoticeContext(ctx, tx, schoolID)
	if err != nil {
		return false, err
	}
	title, body := renderNotice(kind, map[string]string{"school": school.Name, "due": expires.UTC().Format(time.RFC3339)}, school.Language, school.Location)
	subject, text := outsideText("email", school.Name, title, body, a.notify.webURL+"/"+hashLink)
	err = a.queueDelivery(ctx, tx, delivery{SchoolID: schoolID, Topic: "account", Kind: kind, Channel: "email", Address: strings.TrimSpace(email),
		Subject: subject, Body: text, Sensitive: true, SendAfter: time.Now(), Expires: expires})
	return err == nil, err
}

// guardiansOf lists the guardians of students, each once per student.
func guardiansOf(ctx context.Context, q reportQuerier, schoolID string, studentIDs []string) (map[string][]recipient, error) {
	rows, err := q.Query(ctx, `SELECT g.student_id::text,COALESCE(g.user_id::text,''),g.id::text,COALESCE(g.email,''),g.phone
		FROM guardians g WHERE g.school_id=$1 AND g.student_id=ANY($2::uuid[]) ORDER BY g.is_primary DESC,g.created_at`, schoolID, studentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]recipient{}
	for rows.Next() {
		var studentID string
		var p recipient
		if err := rows.Scan(&studentID, &p.UserID, &p.GuardianID, &p.Email, &p.Phone); err != nil {
			return nil, err
		}
		out[studentID] = append(out[studentID], p)
	}
	return out, rows.Err()
}
