package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const deliveryMaxAttempts = 5

// StartNotifier sends queued messages, reads Telegram for people connecting
// their chats, and reminds students of work due soon. It runs until ctx is
// cancelled. A sleeping free-plan instance catches up when it wakes; messages
// more than a day late are given up instead of sent.
func StartNotifier(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, every time.Duration) {
	var origins []string
	for _, origin := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		origins = append(origins, strings.TrimSpace(origin))
	}
	a := &API{db: db, log: log, notify: newNotifier(origins)}
	log.Info("notification channels", "email", a.notify.emailProvider, "sms", a.notify.smsProvider, "telegram", a.notify.telegramBot != "", "web_url", a.notify.webURL)
	go func() {
		var lastReminders time.Time
		run := func() {
			if a.notify.telegram != nil {
				if _, err := a.readTelegram(ctx); err != nil && ctx.Err() == nil {
					log.Warn("reading telegram failed", "error", err)
				}
			}
			if time.Since(lastReminders) >= 10*time.Minute {
				lastReminders = time.Now()
				if count, err := a.queueDueSoon(ctx); err != nil && ctx.Err() == nil {
					log.Error("due-soon reminders failed", "error", err)
				} else if count > 0 {
					log.Info("queued due-soon reminders", "people", count)
				}
			}
			sent, failed, err := a.deliverDue(ctx)
			if err != nil && ctx.Err() == nil {
				log.Error("sending notifications failed", "error", err)
			} else if sent > 0 || failed > 0 {
				log.Info("sent notifications", "sent", sent, "failed", failed)
			}
		}
		run()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// deliverDue sends the messages that are due, in batches of 50.
func (a *API) deliverDue(ctx context.Context) (int, int, error) {
	// A message claimed by a process that died is tried again.
	if _, err := a.db.Exec(ctx, `UPDATE notification_deliveries SET status='pending',claimed_at=NULL WHERE status='sending' AND claimed_at < now() - interval '10 minutes'`); err != nil {
		return 0, 0, err
	}
	if _, err := a.db.Exec(ctx, `UPDATE notification_deliveries SET status='skipped',last_error='Too late to send',body=CASE WHEN sensitive THEN '' ELSE body END
		WHERE status='pending' AND expires_at < now()`); err != nil {
		return 0, 0, err
	}
	rows, err := a.db.Query(ctx, `UPDATE notification_deliveries SET status='sending',claimed_at=now(),attempts=attempts+1
		WHERE id IN (SELECT id FROM notification_deliveries WHERE status='pending' AND send_after <= now() ORDER BY send_after LIMIT 50 FOR UPDATE SKIP LOCKED)
		RETURNING id::text,channel,address,subject,body,attempts`)
	if err != nil {
		return 0, 0, err
	}
	type claimed struct {
		id       string
		message  outsideMessage
		attempts int
	}
	var batch []claimed
	for rows.Next() {
		var c claimed
		if err := rows.Scan(&c.id, &c.message.Channel, &c.message.Address, &c.message.Subject, &c.message.Body, &c.attempts); err != nil {
			rows.Close()
			return 0, 0, err
		}
		batch = append(batch, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	sent, failed := 0, 0
	for _, c := range batch {
		s := a.notify.senderFor(c.message.Channel)
		if s == nil {
			if _, err := a.db.Exec(ctx, `UPDATE notification_deliveries SET status='skipped',last_error=$2,body=CASE WHEN sensitive THEN '' ELSE body END WHERE id=$1`, c.id, notSetUp); err != nil {
				return sent, failed, err
			}
			continue
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		ref, err := s.Send(sendCtx, c.message)
		cancel()
		if err == nil {
			sent++
			if _, err := a.db.Exec(ctx, `UPDATE notification_deliveries SET status='sent',sent_at=now(),provider_ref=$2,last_error='',body=CASE WHEN sensitive THEN '' ELSE body END WHERE id=$1`, c.id, truncateRunes(ref, 200)); err != nil {
				return sent, failed, err
			}
			continue
		}
		reason := truncateRunes(err.Error(), 300)
		var p permanentError
		if errors.As(err, &p) || c.attempts >= deliveryMaxAttempts {
			failed++
			_, err = a.db.Exec(ctx, `UPDATE notification_deliveries SET status='failed',last_error=$2,body=CASE WHEN sensitive THEN '' ELSE body END WHERE id=$1`, c.id, reason)
		} else {
			wait := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}[min(c.attempts, 4)-1]
			_, err = a.db.Exec(ctx, `UPDATE notification_deliveries SET status='pending',claimed_at=NULL,last_error=$2,send_after=now()+$3::interval WHERE id=$1`, c.id, reason, fmt.Sprintf("%d seconds", int(wait.Seconds())))
		}
		if err != nil {
			return sent, failed, err
		}
	}
	return sent, failed, nil
}

// queueDueSoon reminds students of a quiz or assignment due within the next
// day that they have not handed in. Each student is reminded once per piece of
// work.
func (a *API) queueDueSoon(ctx context.Context) (int, error) {
	rows, err := a.db.Query(ctx, `SELECT aa.id::text,aa.school_id::text,a.title,COALESCE(cs.name,''),aa.due_at,
		array_agg(st.user_id::text)
		FROM assessment_assignments aa
		JOIN assessments a ON a.id=aa.assessment_id AND a.kind IN ('quiz','assignment') AND a.status='published'
		LEFT JOIN class_subjects cs ON cs.id=a.class_subject_id
		JOIN enrollments e ON e.class_id=aa.class_id AND e.status='active'
		JOIN students st ON st.id=e.student_id AND st.user_id IS NOT NULL AND st.status='active'
		WHERE aa.due_at BETWEEN now() + interval '1 hour' AND now() + interval '24 hours'
		AND (aa.opens_at IS NULL OR aa.opens_at <= now())
		AND NOT EXISTS(SELECT 1 FROM assessment_attempts at WHERE at.assignment_id=aa.id AND at.student_id=st.id AND at.status='submitted')
		GROUP BY aa.id,aa.school_id,a.title,cs.name,aa.due_at`)
	if err != nil {
		return 0, err
	}
	type due struct {
		id, school, title, subject string
		at                         time.Time
		users                      []string
	}
	var list []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.school, &d.title, &d.subject, &d.at, &d.users); err != nil {
			rows.Close()
			return 0, err
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	total := 0
	for _, d := range list {
		people := make([]recipient, 0, len(d.users))
		for _, user := range d.users {
			people = append(people, recipient{UserID: user})
		}
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return total, err
		}
		count, err := a.queueNotice(ctx, tx, d.school, notice{Topic: "schoolwork", Kind: "schoolwork.due_soon", Link: "/learn",
			DedupeKey: "due:" + d.id, Params: map[string]string{"title": d.title, "subject": d.subject, "due": d.at.UTC().Format(time.RFC3339)}}, people)
		if err == nil {
			err = tx.Commit(ctx)
		}
		_ = tx.Rollback(context.Background())
		if err != nil {
			return total, err
		}
		total += count
	}
	return total, nil
}

var telegramMu sync.Mutex

// readTelegram reads new messages to the bot. "/start <code>" links the chat to
// the person who made the code; "/stop" unlinks it. It returns how many chats
// were linked.
func (a *API) readTelegram(ctx context.Context) (int, error) {
	n := a.notify
	if n.telegram == nil {
		return 0, nil
	}
	telegramMu.Lock()
	defer telegramMu.Unlock()
	var offset int64
	if err := a.db.QueryRow(ctx, `SELECT COALESCE((SELECT next_offset FROM telegram_cursor WHERE id=1),0)`).Scan(&offset); err != nil {
		return 0, err
	}
	body, err := postJSON(ctx, n.client, n.telegramAPI+"/bot"+n.telegramToken+"/getUpdates", "", map[string]any{"offset": offset, "timeout": 0, "allowed_updates": []string{"message"}})
	if err != nil {
		return 0, redactToken(err, n.telegramToken)
	}
	var out struct {
		Result []struct {
			UpdateID int64 `json:"update_id"`
			Message  *struct {
				Text string `json:"text"`
				Chat struct {
					ID   int64  `json:"id"`
					Type string `json:"type"`
				} `json:"chat"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	linked := 0
	for _, update := range out.Result {
		if update.UpdateID >= offset {
			offset = update.UpdateID + 1
		}
		m := update.Message
		if m == nil || m.Chat.Type != "private" {
			continue
		}
		chat := fmt.Sprint(m.Chat.ID)
		text := strings.TrimSpace(m.Text)
		reply := "This bot sends messages from your school on FURII School OS. To connect, open FURII School OS, go to your account and choose Connect Telegram.\n\nይህ ቦት ከትምህርት ቤትዎ መልእክቶችን ይልካል። ለማገናኘት FURII School OSን ከፍተው Telegram ያገናኙ።"
		switch {
		case strings.HasPrefix(text, "/start "):
			code := strings.TrimSpace(strings.TrimPrefix(text, "/start "))
			hash := sha256.Sum256([]byte(code))
			var school string
			err := a.db.QueryRow(ctx, `UPDATE notification_settings ns SET telegram_chat_id=$2,telegram_link_hash=NULL,telegram_link_expires_at=NULL,telegram_enabled=true,updated_at=now()
				FROM schools s WHERE s.id=ns.school_id AND ns.telegram_link_hash=$1 AND ns.telegram_link_expires_at > now() RETURNING s.name`, hash[:], chat).Scan(&school)
			if errors.Is(err, pgx.ErrNoRows) {
				reply = "This link has expired or was already used. Open FURII School OS and choose Connect Telegram again.\n\nይህ ሊንክ ጊዜው አልፏል። FURII School OSን ከፍተው እንደገና ያገናኙ።"
			} else if err != nil {
				return linked, err
			} else {
				linked++
				reply = "Connected. Messages from " + school + " will arrive here. Send /stop to turn them off.\n\nተገናኝቷል። ከ" + school + " የሚመጡ መልእክቶች እዚህ ይደርሳሉ። ለማቆም /stop ይላኩ።"
			}
		case text == "/stop":
			if _, err := a.db.Exec(ctx, `UPDATE notification_settings SET telegram_chat_id=NULL,updated_at=now() WHERE telegram_chat_id=$1`, chat); err != nil {
				return linked, err
			}
			reply = "Disconnected. You will not get school messages here.\n\nተቋርጧል። የትምህርት ቤት መልእክቶች እዚህ አይደርሱም።"
		}
		if _, err := n.telegram.Send(ctx, outsideMessage{Channel: "telegram", Address: chat, Body: reply}); err != nil {
			a.log.Warn("telegram reply failed", "error", err)
		}
	}
	if len(out.Result) > 0 {
		if _, err := a.db.Exec(ctx, `INSERT INTO telegram_cursor(id,next_offset) VALUES(1,$1) ON CONFLICT (id) DO UPDATE SET next_offset=EXCLUDED.next_offset`, offset); err != nil {
			return linked, err
		}
	}
	return linked, nil
}

func telegramLink(bot, code string) string {
	return "https://t.me/" + url.PathEscape(bot) + "?start=" + url.QueryEscape(code)
}
