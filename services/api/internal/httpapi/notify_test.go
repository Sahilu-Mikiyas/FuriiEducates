package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoticeWordingAndPhones(t *testing.T) {
	for raw, want := range map[string]string{"0911 234 567": "+251911234567", "911-234-567": "+251911234567", "251711223344": "+251711223344", "+251 911 234 567": "+251911234567", "+44 7700 900123": "+447700900123"} {
		if got, ok := normalizePhone(raw); !ok || got != want {
			t.Fatalf("%q: %q %v", raw, got, ok)
		}
	}
	for _, bad := range []string{"", "12345", "0811234567", "09112x4567", "++251911234567"} {
		if got, ok := normalizePhone(bad); ok {
			t.Fatalf("%q accepted as %q", bad, got)
		}
	}
	addis, _ := time.LoadLocation("Africa/Addis_Ababa")
	params := map[string]string{"student": "Hanna", "class": "10A", "date": "2026-11-06"}
	title, body := renderNotice("attendance.absent", params, "en", addis)
	if title != "Hanna was marked absent" || !strings.Contains(body, "Fri 6 Nov 2026") || !strings.Contains(body, "10A") {
		t.Fatalf("english: %q %q", title, body)
	}
	_, body = renderNotice("attendance.absent", params, "am", addis)
	if !strings.Contains(body, "ጥቅምት 27፣ 2019 ዓ.ም.") || strings.Contains(body, "{") {
		t.Fatalf("amharic: %q", body)
	}
	for kind, languages := range noticeTemplates {
		for _, lang := range []string{"en", "am"} {
			if _, ok := languages[lang]; !ok {
				t.Fatalf("%s has no %s wording", kind, lang)
			}
		}
	}
	night := time.Date(2026, 11, 6, 22, 30, 0, 0, addis)
	if got := smsQuietUntil(night, addis); !got.Equal(time.Date(2026, 11, 7, 7, 0, 0, 0, addis)) {
		t.Fatalf("night: %v", got)
	}
	if got := smsQuietUntil(time.Date(2026, 11, 6, 3, 0, 0, 0, addis), addis); !got.Equal(time.Date(2026, 11, 6, 7, 0, 0, 0, addis)) {
		t.Fatalf("early: %v", got)
	}
	if day := time.Date(2026, 11, 6, 10, 0, 0, 0, addis); !smsQuietUntil(day, addis).Equal(day) {
		t.Fatal("daytime SMS was delayed")
	}
	_, sms := outsideText("sms", "School", "Title", strings.Repeat("ሀ", 500), "https://x")
	if len([]rune(sms)) > 320 || strings.Contains(sms, "https://") {
		t.Fatalf("sms: %d runes", len([]rune(sms)))
	}
	err := redactToken(permanent(errors.New(`Post "https://api.telegram.org/bot123:SECRET/sendMessage": refused`)), "123:SECRET")
	var p permanentError
	if strings.Contains(err.Error(), "SECRET") || !errors.As(err, &p) {
		t.Fatalf("token kept: %v", err)
	}
}

// fakeProviders stands in for Resend, AfroMessage and Telegram.
type fakeProviders struct {
	mu       sync.Mutex
	requests []fakeRequest
	updates  []map[string]any
	failFor  map[string]int
}

type fakeRequest struct {
	path, auth string
	body       map[string]any
}

func newFakeProviders(t *testing.T) (*fakeProviders, *httptest.Server) {
	f := &fakeProviders{failFor: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, fakeRequest{path: r.URL.Path, auth: r.Header.Get("Authorization"), body: body})
		address := ""
		switch to := body["to"].(type) {
		case string:
			address = to
		case []any:
			address, _ = to[0].(string)
		}
		if status := f.failFor[address]; status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"refused"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/emails":
			_, _ = w.Write([]byte(`{"id":"email-1"}`))
		case r.URL.Path == "/api/send":
			_, _ = w.Write([]byte(`{"acknowledge":"success","response":{"message_id":"sms-1"}}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			offset, _ := body["offset"].(float64)
			result := []map[string]any{}
			for _, update := range f.updates {
				if update["update_id"].(float64) >= offset {
					result = append(result, update)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeProviders) sentTo(pathSuffix, address string) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if !strings.HasSuffix(r.path, pathSuffix) {
			continue
		}
		to := ""
		switch value := r.body["to"].(type) {
		case string:
			to = value
		case []any:
			to, _ = value[0].(string)
		}
		if chat, ok := r.body["chat_id"].(string); ok {
			to = chat
		}
		if to == address {
			out = append(out, r)
		}
	}
	return out
}

func setChannels(t *testing.T, providers *httptest.Server) {
	t.Setenv("PUBLIC_WEB_URL", "https://school.example")
	t.Setenv("RESEND_API_KEY", "re_test")
	t.Setenv("RESEND_API_URL", providers.URL+"/emails")
	t.Setenv("EMAIL_FROM", "FURII <school@example.test>")
	t.Setenv("SMS_PROVIDER", "afromessage")
	t.Setenv("AFROMESSAGE_TOKEN", "afro-test")
	t.Setenv("AFROMESSAGE_API_URL", providers.URL+"/api/send")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123:test")
	t.Setenv("TELEGRAM_BOT_USERNAME", "furii_test_bot")
	t.Setenv("TELEGRAM_API_URL", providers.URL)
}

// sendNow runs the delivery worker for a school's queue, without waiting.
func sendNow(t *testing.T, schoolID string) {
	t.Helper()
	if _, err := testDB.Exec(context.Background(), `UPDATE notification_deliveries SET send_after=now() WHERE school_id=$1 AND status='pending'`, schoolID); err != nil {
		t.Fatal(err)
	}
	worker := &API{db: testDB, log: slog.New(slog.NewTextHandler(io.Discard, nil)), notify: newNotifier(nil)}
	if _, _, err := worker.deliverDue(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func schoolOf(t *testing.T, classID string) string {
	var id string
	if err := testDB.QueryRow(context.Background(), `SELECT school_id::text FROM classes WHERE id=$1`, classID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func todayInAddis() string {
	addis, _ := time.LoadLocation("Africa/Addis_Ababa")
	return time.Now().In(addis).Format("2006-01-02")
}

func deliveryStatuses(t *testing.T, schoolID, kind string) map[string]string {
	rows, err := testDB.Query(context.Background(), `SELECT channel||':'||address,status FROM notification_deliveries WHERE school_id=$1 AND kind=$2`, schoolID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, status string
		if err := rows.Scan(&key, &status); err != nil {
			t.Fatal(err)
		}
		out[key] = status
	}
	return out
}

func TestNotificationsWithoutChannels(t *testing.T) {
	s := setupReports(t)
	schoolID := schoolOf(t, s.classID)
	guardianID := addGuardian(s.admin, s.studentID, "Almaz")
	email := uniqueEmail("parent")
	parent := signUpParent(t, s.server, email, inviteToken(s.admin, s.studentID, guardianID, email))
	s.admin.create("/students/"+s.studentID+"/guardians", map[string]any{"full_name": "Kebede", "relationship": "father", "phone": "0922000111"})

	register := func(status string) {
		s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.studentID, "status": status}, {"student_id": s.secondID, "status": "present"}}}).expect(http.StatusOK)
	}
	register("absent")
	inbox := parent.call("GET", "/me/notifications?lang=en", nil).expect(http.StatusOK).object()
	items := inbox["items"].([]any)
	if len(items) != 1 || inbox["unread"].(float64) != 1 || !strings.Contains(items[0].(map[string]any)["title"].(string), "Test Learner") {
		t.Fatalf("inbox: %v", inbox)
	}
	statuses := deliveryStatuses(t, schoolID, "attendance.absent")
	if statuses["sms:+251922000111"] != "skipped" || statuses["sms:+251911234567"] != "skipped" || statuses["email:"+email] != "skipped" {
		t.Fatalf("without channels everything is kept as skipped: %v", statuses)
	}
	// Saving the same register again does not repeat the notice.
	register("absent")
	if count := parent.call("GET", "/me/notifications/unread", nil).expect(http.StatusOK).object()["unread"].(float64); count != 1 {
		t.Fatalf("unread %v", count)
	}
	// A correction takes back the unread notice.
	register("present")
	if count := parent.call("GET", "/me/notifications/unread", nil).expect(http.StatusOK).object()["unread"].(float64); count != 0 {
		t.Fatalf("after correction %v", count)
	}
	// Registers for earlier days tell nobody.
	s.admin.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": "2026-10-01", "records": []map[string]string{{"student_id": s.secondID, "status": "absent"}}}).expect(http.StatusOK)
	if items := s.admin.call("GET", "/notifications/deliveries", nil).expect(http.StatusOK).list(); len(items) != 3 {
		t.Fatalf("deliveries: %d", len(items))
	}

	overview := s.admin.call("GET", "/notifications/overview", nil).expect(http.StatusOK).object()
	for _, channel := range overview["channels"].([]any) {
		if channel.(map[string]any)["ready"].(bool) {
			t.Fatalf("channel ready: %v", channel)
		}
	}
	s.teacher.call("GET", "/notifications/overview", nil).expect(http.StatusForbidden)
	parent.call("POST", "/announcements", map[string]any{"title": "X", "body": "Y", "audience": "everyone"}).expect(http.StatusForbidden)
	s.admin.call("POST", "/notifications/test", map[string]string{"channel": "sms", "address": "0911000000"}).expect(http.StatusConflict)
	parent.call("POST", "/me/telegram-link", nil).expect(http.StatusConflict)
	if emailed := s.admin.call("POST", "/staff/invitations", map[string]string{"email": uniqueEmail("teacher")}).expect(http.StatusCreated).object()["emailed"]; emailed != false {
		t.Fatalf("emailed %v", emailed)
	}
}

func TestNotificationsAreDelivered(t *testing.T) {
	providers, providerServer := newFakeProviders(t)
	setChannels(t, providerServer)
	s := setupReports(t)
	schoolID := schoolOf(t, s.classID)
	guardianID := addGuardian(s.admin, s.studentID, "Almaz")
	email := uniqueEmail("parent")
	parent := signUpParent(t, s.server, email, inviteToken(s.admin, s.studentID, guardianID, email))
	s.admin.create("/students/"+s.studentID+"/guardians", map[string]any{"full_name": "Kebede", "relationship": "father", "phone": "0922000111"})
	sendNow(t, schoolID) // the parent's invitation email

	if len(providers.sentTo("/emails", email)) != 1 {
		t.Fatal("the parent invitation was not emailed")
	}
	invite := providers.sentTo("/emails", email)[0]
	if !strings.Contains(invite.body["text"].(string), "https://school.example/#parent-invite=") || invite.auth != "Bearer re_test" {
		t.Fatalf("invitation email: %v", invite)
	}
	var stored string
	if err := testDB.QueryRow(context.Background(), `SELECT body FROM notification_deliveries WHERE school_id=$1 AND kind='account.parent_invite'`, schoolID).Scan(&stored); err != nil || stored != "" {
		t.Fatalf("the sign-in link was kept: %q %v", stored, err)
	}

	s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.studentID, "status": "absent"}, {"student_id": s.secondID, "status": "absent"}}}).expect(http.StatusOK)
	// The second student's mark is corrected before the message goes.
	s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.studentID, "status": "absent"}, {"student_id": s.secondID, "status": "late"}}}).expect(http.StatusOK)
	var waiting time.Time
	if err := testDB.QueryRow(context.Background(), `SELECT min(send_after) FROM notification_deliveries WHERE school_id=$1 AND kind='attendance.absent' AND channel='email'`, schoolID).Scan(&waiting); err != nil || time.Until(waiting) < 5*time.Minute {
		t.Fatalf("absence messages should wait for corrections: %v %v", waiting, err)
	}
	sendNow(t, schoolID)
	father := providers.sentTo("/api/send", "+251922000111")
	if len(father) != 1 || !strings.Contains(father[0].body["message"].(string), "Test Learner") || father[0].auth != "Bearer afro-test" {
		t.Fatalf("father's SMS: %v", father)
	}
	if mother := providers.sentTo("/api/send", "+251911234567"); len(mother) != 1 {
		t.Fatalf("mother's SMS: %v", mother)
	}
	mail := providers.sentTo("/emails", email)
	if len(mail) != 2 || !strings.Contains(mail[1].body["text"].(string), "https://school.example/family/children/"+s.studentID) {
		t.Fatalf("absence email: %v", mail)
	}
	if statuses := deliveryStatuses(t, schoolID, "attendance.absent"); len(statuses) != 3 {
		t.Fatalf("the corrected student was reported: %v", statuses)
	}

	// Telegram: the parent connects a chat by pressing Start in the bot.
	link := parent.call("POST", "/me/telegram-link", nil).expect(http.StatusCreated).object()["url"].(string)
	code := strings.TrimPrefix(link, "https://t.me/furii_test_bot?start=")
	if code == link || code == "" {
		t.Fatalf("link %s", link)
	}
	providers.mu.Lock()
	providers.updates = append(providers.updates,
		map[string]any{"update_id": 41.0, "message": map[string]any{"text": "/start wrong-code", "chat": map[string]any{"id": 999, "type": "private"}}},
		map[string]any{"update_id": 42.0, "message": map[string]any{"text": "/start " + code, "chat": map[string]any{"id": 555, "type": "private"}}})
	providers.mu.Unlock()
	if connected := parent.call("POST", "/me/telegram-link/check", nil).expect(http.StatusOK).object()["connected"]; connected != true {
		t.Fatal("telegram not connected")
	}
	if replies := providers.sentTo("/sendMessage", "555"); len(replies) != 1 || !strings.Contains(replies[0].body["text"].(string), "Connected") {
		t.Fatalf("reply: %v", replies)
	}
	if replies := providers.sentTo("/sendMessage", "999"); len(replies) != 1 || !strings.Contains(replies[0].body["text"].(string), "expired") {
		t.Fatalf("wrong code reply: %v", replies)
	}

	// Announcements: counted first, then sent.
	announce := map[string]any{"title": "School closed Friday", "body": "For the holiday.", "audience": "parents", "channels": []string{"sms", "telegram"}, "dry_run": true}
	preview := s.admin.call("POST", "/announcements", announce).expect(http.StatusOK).object()
	if preview["recipients"].(float64) != 2 || preview["channels"].(map[string]any)["sms"].(map[string]any)["pending"].(float64) != 2 {
		t.Fatalf("preview: %v", preview)
	}
	if list := s.admin.call("GET", "/announcements", nil).expect(http.StatusOK).list(); len(list) != 0 {
		t.Fatal("a preview was saved")
	}
	announce["dry_run"] = false
	s.admin.call("POST", "/announcements", announce).expect(http.StatusCreated)
	sendNow(t, schoolID)
	if got := providers.sentTo("/sendMessage", "555"); len(got) != 2 || !strings.Contains(got[1].body["text"].(string), "School closed Friday") {
		t.Fatalf("telegram announcement: %v", got)
	}
	if inbox := parent.call("GET", "/me/notifications", nil).expect(http.StatusOK).object()["items"].([]any); inbox[0].(map[string]any)["title"] != "School closed Friday" {
		t.Fatalf("inbox: %v", inbox[0])
	}

	// The parent turns SMS off and mutes announcements; the father still gets SMS.
	parent.call("PUT", "/me/notification-settings", map[string]any{"language": "am", "email_enabled": true, "sms_enabled": false, "telegram_enabled": true, "muted_topics": []string{"announcements"}, "phone": "0933 444 555"}).expect(http.StatusOK)
	parent.call("PUT", "/me/notification-settings", map[string]any{"language": "en", "email_enabled": true, "sms_enabled": true, "telegram_enabled": true, "muted_topics": []string{"account"}}).expect(http.StatusBadRequest)
	parent.call("PUT", "/me/notification-settings", map[string]any{"language": "am", "email_enabled": true, "sms_enabled": true, "telegram_enabled": true, "muted_topics": []string{}, "phone": "12"}).expect(http.StatusBadRequest)
	settings := parent.call("GET", "/me/notification-settings", nil).expect(http.StatusOK).object()
	if settings["phone"] != "+251933444555" || settings["sms_enabled"] != false || settings["telegram_connected"] != true {
		t.Fatalf("settings: %v", settings)
	}
	announce["title"] = "Second notice"
	s.admin.call("POST", "/announcements", announce).expect(http.StatusCreated)
	sendNow(t, schoolID)
	if got := providers.sentTo("/sendMessage", "555"); len(got) != 2 {
		t.Fatalf("muted announcement went to telegram: %d", len(got))
	}
	if got := providers.sentTo("/api/send", "+251922000111"); len(got) != 3 {
		t.Fatalf("father's SMS: %d", len(got))
	}
	// The muted topic still shows in the app, in the person's language.
	items := parent.call("GET", "/me/notifications?lang=am", nil).expect(http.StatusOK).object()["items"].([]any)
	if items[0].(map[string]any)["title"] != "Second notice" || !strings.Contains(items[2].(map[string]any)["title"].(string), "አልተገኙም") {
		t.Fatalf("inbox: %v", items)
	}
	parent.call("POST", "/me/notifications/read", map[string]any{"all": true}).expect(http.StatusOK)
	if count := parent.call("GET", "/me/notifications/unread", nil).expect(http.StatusOK).object()["unread"].(float64); count != 0 {
		t.Fatalf("unread %v", count)
	}

	// Failures: a temporary one waits and retries, a refused one stops.
	providers.mu.Lock()
	providers.failFor["+251911000001"] = 503
	providers.failFor["+251911000002"] = 400
	providers.mu.Unlock()
	s.admin.call("POST", "/notifications/test", map[string]string{"channel": "sms", "address": "0911000002"}).expect(http.StatusBadGateway)
	s.admin.call("POST", "/notifications/test", map[string]string{"channel": "sms", "address": "0911 000 003"}).expect(http.StatusOK)
	if got := providers.sentTo("/api/send", "+251911000003"); len(got) != 1 || !strings.Contains(got[0].body["message"].(string), "Test message") {
		t.Fatalf("test SMS: %v", got)
	}
	failing := s.admin.create("/students/"+s.secondID+"/guardians", map[string]any{"full_name": "Busy line", "relationship": "guardian", "phone": "0911000001"})
	_ = failing
	s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.secondID, "status": "absent"}}}).expect(http.StatusOK)
	sendNow(t, schoolID)
	var status string
	var attempts int
	var next time.Time
	if err := testDB.QueryRow(context.Background(), `SELECT status,attempts,send_after FROM notification_deliveries WHERE school_id=$1 AND address='+251911000001'`, schoolID).Scan(&status, &attempts, &next); err != nil || status != "pending" || attempts != 1 || time.Until(next) < 30*time.Second {
		t.Fatalf("temporary failure: %s %d %v %v", status, attempts, next, err)
	}

	// New schoolwork reaches the class, and a reminder comes before it is due.
	quizID := s.teacher.create("/quizzes", quizBody(s.math, choiceQuestion("1 + 1?", "Motion")))
	due := time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339)
	s.teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{"due_at": due}).expect(http.StatusOK)
	worker := &API{db: testDB, log: slog.New(slog.NewTextHandler(io.Discard, nil)), notify: newNotifier(nil)}
	if _, err := worker.queueDueSoon(context.Background()); err != nil {
		t.Fatal(err)
	}
	titles := []string{}
	for _, item := range s.learner.call("GET", "/me/notifications", nil).expect(http.StatusOK).object()["items"].([]any) {
		titles = append(titles, item.(map[string]any)["title"].(string))
	}
	if len(titles) != 2 || !strings.HasPrefix(titles[0], "Due soon") || !strings.HasPrefix(titles[1], "New quiz") {
		t.Fatalf("learner inbox: %v", titles)
	}

	// Report cards tell students and families.
	tx, err := testDB.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.noticeReports(context.Background(), tx, schoolID, s.term1, []string{s.studentID}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if items := parent.call("GET", "/me/notifications", nil).expect(http.StatusOK).object()["items"].([]any); !strings.Contains(items[0].(map[string]any)["title"].(string), "report card") {
		t.Fatalf("parent: %v", items[0])
	}

	// The school's rules limit what goes by SMS.
	s.admin.call("PUT", "/notifications/rules", map[string]any{"email": []string{"attendance"}, "sms": []string{}, "telegram": []string{"reports"}}).expect(http.StatusOK)
	s.admin.call("PUT", "/notifications/rules", map[string]any{"email": []string{"account"}, "sms": []string{}, "telegram": []string{}}).expect(http.StatusBadRequest)
	overview := s.admin.call("GET", "/notifications/overview", nil).expect(http.StatusOK).object()
	if rules := overview["rules"].(map[string]any); len(rules["sms"].([]any)) != 0 || overview["sms_this_month"].(float64) < 4 {
		t.Fatalf("overview: %v", overview)
	}
	sendNow(t, schoolID) // the report card SMS queued under the old rules
	before := len(providers.sentTo("/api/send", "+251922000111"))
	s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.studentID, "status": "present"}}}).expect(http.StatusOK)
	if _, err := testDB.Exec(context.Background(), `DELETE FROM notifications WHERE dedupe_key LIKE 'absent:%' AND school_id=$1`, schoolID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(context.Background(), `UPDATE notification_deliveries SET dedupe_key=NULL WHERE kind='attendance.absent' AND school_id=$1`, schoolID); err != nil {
		t.Fatal(err)
	}
	s.teacher.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": todayInAddis(), "records": []map[string]string{{"student_id": s.studentID, "status": "absent"}}}).expect(http.StatusOK)
	sendNow(t, schoolID)
	if after := len(providers.sentTo("/api/send", "+251922000111")); after != before {
		t.Fatalf("SMS sent against the school's rules: %d -> %d", before, after)
	}

	// Failed messages can be retried; messages with sign-in links cannot.
	var failedID, inviteID string
	if err := testDB.QueryRow(context.Background(), `SELECT id::text FROM notification_deliveries WHERE school_id=$1 AND address='+251911000002'`, schoolID).Scan(&failedID); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(context.Background(), `SELECT id::text FROM notification_deliveries WHERE school_id=$1 AND kind='account.parent_invite'`, schoolID).Scan(&inviteID); err != nil {
		t.Fatal(err)
	}
	s.admin.call("POST", "/notifications/deliveries/"+inviteID+"/retry", nil).expect(http.StatusConflict)
	s.admin.call("POST", "/notifications/deliveries/"+failedID+"/retry", nil).expect(http.StatusOK)
	_, other := seedSchool(t, s.server)
	other.call("POST", "/notifications/deliveries/"+failedID+"/retry", nil).expect(http.StatusConflict)
	if items := other.call("GET", "/notifications/deliveries", nil).expect(http.StatusOK).list(); len(items) != 0 {
		t.Fatalf("another school sees deliveries: %d", len(items))
	}
}
