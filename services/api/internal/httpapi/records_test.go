package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
)

func TestYearAndClassEditing(t *testing.T) {
	s := setupReports(t)
	admin := s.admin
	path := "/academic-years/" + s.yearID
	s.teacher.call("PATCH", path, map[string]string{"name": "X", "starts_on": "2026-09-01", "ends_on": "2027-07-01"}).expect(http.StatusForbidden)
	admin.call("PATCH", path, map[string]string{"name": "2019 E.C.", "starts_on": "2026-09-01", "ends_on": "2026-08-01"}).expect(http.StatusBadRequest)
	// The year's terms end in July 2027, so the year cannot end before them.
	refused := admin.call("PATCH", path, map[string]string{"name": "2019 E.C.", "starts_on": "2026-09-01", "ends_on": "2027-03-01"})
	refused.expect(http.StatusConflict)
	if refused.errorCode() != "TERMS_OUTSIDE_YEAR" {
		t.Fatalf("code %s", refused.errorCode())
	}
	admin.call("PATCH", path, map[string]string{"name": "2019 E.C.", "starts_on": "2026-08-15", "ends_on": "2027-07-10"}).expect(http.StatusOK)
	admin.call("DELETE", path, nil).expect(http.StatusConflict)
	empty := admin.create("/academic-years", map[string]string{"name": unique("Year"), "starts_on": "2030-09-01", "ends_on": "2031-07-01"})
	admin.call("DELETE", "/academic-years/"+empty, nil).expect(http.StatusOK)

	admin.call("PATCH", "/classes/"+s.classID, map[string]string{"name": "", "grade_level": "12"}).expect(http.StatusBadRequest)
	admin.call("PATCH", "/classes/"+s.classID, map[string]string{"name": "Section A", "grade_level": "Grade 12"}).expect(http.StatusOK)
	var name, grade string
	if err := testDB.QueryRow(context.Background(), `SELECT name,grade_level FROM classes WHERE id=$1`, s.classID).Scan(&name, &grade); err != nil || name != "Section A" || grade != "Grade 12" {
		t.Fatalf("class: %s %s %v", name, grade, err)
	}
	// A class in use stays; a class made by mistake can go.
	inUse := admin.call("DELETE", "/classes/"+s.classID, nil)
	inUse.expect(http.StatusConflict)
	if inUse.errorCode() != "CLASS_IN_USE" {
		t.Fatalf("code %s", inUse.errorCode())
	}
	mistake := createClass(admin)
	teacherID, _ := inviteTeacher(s.server, admin)
	admin.call("POST", "/classes/"+mistake+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	admin.call("DELETE", "/classes/"+mistake, nil).expect(http.StatusOK)

	_, other := seedSchool(t, s.server)
	other.call("PATCH", "/classes/"+s.classID, map[string]string{"name": "X", "grade_level": "1"}).expect(http.StatusNotFound)
	other.call("DELETE", "/classes/"+s.classID, nil).expect(http.StatusNotFound)
	other.call("PATCH", path, map[string]string{"name": "X", "starts_on": "2026-09-01", "ends_on": "2027-07-01"}).expect(http.StatusNotFound)
	other.call("DELETE", path, nil).expect(http.StatusNotFound)
}

func TestUnassignTeacher(t *testing.T) {
	s := setupReports(t)
	var teacherID string
	if err := testDB.QueryRow(context.Background(), `SELECT teacher_id::text FROM class_subjects WHERE id=$1`, s.math).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	path := "/classes/" + s.classID + "/teachers/" + teacherID
	s.teacher.call("DELETE", path, nil).expect(http.StatusForbidden)
	s.admin.call("DELETE", path, nil).expect(http.StatusOK)
	s.admin.call("DELETE", path, nil).expect(http.StatusNotFound)
	s.teacher.call("GET", "/attendance/classes/"+s.classID+"?date=2026-10-01", nil).expect(http.StatusForbidden)
	// Their subject goes back to any teacher of the class.
	for _, row := range s.admin.call("GET", "/classes/"+s.classID+"/subjects", nil).expect(http.StatusOK).list() {
		if row["id"] == s.math && row["teacher_id"] != "" {
			t.Fatalf("subject still named: %v", row)
		}
	}
	s.other.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Quiz", "weight": 10, "max_score": 10}).expect(http.StatusCreated)
}

func TestFinanceVoidAndCurrencies(t *testing.T) {
	_, admin := seedSchool(t, newTestServer(t))
	studentID := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Payer"})
	entry := func(kind, currency string, amount int) string {
		return admin.create("/finance/entries", map[string]any{"entry_type": kind, "category": "Tuition", "description": "Fee", "amount_minor": amount, "currency_code": currency, "student_id": studentID, "status": "recorded"})
	}
	entry("tuition_charge", "ETB", 500000)
	mistake := entry("payment", "ETB", 900000)
	entry("tuition_charge", "USD", 10000)
	admin.call("POST", "/finance/entries/"+mistake+"/void", map[string]string{"reason": ""}).expect(http.StatusBadRequest)
	admin.call("POST", "/finance/entries/"+mistake+"/void", map[string]string{"reason": "Typed the wrong amount"}).expect(http.StatusOK)
	admin.call("POST", "/finance/entries/"+mistake+"/void", map[string]string{"reason": "Again"}).expect(http.StatusConflict)
	summary := admin.call("GET", "/finance/summary", nil).expect(http.StatusOK).object()
	if summary["payments_recorded_minor"] != float64(0) || summary["outstanding_charges_minor"] != float64(500000) {
		t.Fatalf("a void entry must not count: %v", summary)
	}
	currencies := summary["by_currency"].([]any)
	if len(currencies) != 2 || currencies[0].(map[string]any)["currency_code"] != "ETB" || currencies[1].(map[string]any)["tuition_billed_minor"] != float64(10000) {
		t.Fatalf("by currency: %v", currencies)
	}
	var reason string
	if err := testDB.QueryRow(context.Background(), `SELECT changes->>'reason' FROM audit_logs WHERE action='finance.voided' AND entity_id=$1`, mistake).Scan(&reason); err != nil || reason != "Typed the wrong amount" {
		t.Fatalf("reason %q %v", reason, err)
	}
}

func TestResourceRemoval(t *testing.T) {
	s := setupReports(t)
	var teacherID, schoolID string
	if err := testDB.QueryRow(context.Background(), `SELECT teacher_id::text,school_id::text FROM class_subjects WHERE id=$1`, s.math).Scan(&teacherID, &schoolID); err != nil {
		t.Fatal(err)
	}
	add := func(uploader string) string {
		var id string
		if err := testDB.QueryRow(context.Background(), `INSERT INTO teaching_resources(school_id,class_id,uploaded_by,title,file_name,media_type,file_size,file_data) VALUES($1,$2,$3,'Sheet','f.pdf','application/pdf',1,'x') RETURNING id::text`, schoolID, s.classID, uploader).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	mine := add(teacherID)
	quiz := quizBody(s.math, choiceQuestion("Q", "T"))
	quiz["resource_id"] = mine
	quizID := s.teacher.create("/quizzes", quiz)
	s.other.call("DELETE", "/resources/"+mine, nil).expect(http.StatusForbidden)
	s.learner.call("DELETE", "/resources/"+mine, nil).expect(http.StatusForbidden)
	s.teacher.call("DELETE", "/resources/"+mine, nil).expect(http.StatusOK)
	if saved := s.teacher.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusOK).object(); saved["resource_id"] != "" {
		t.Fatalf("the quiz should lose the link: %v", saved["resource_id"])
	}
	s.admin.call("DELETE", "/resources/"+add(teacherID), nil).expect(http.StatusOK)
	_, other := seedSchool(t, s.server)
	other.call("DELETE", "/resources/"+add(teacherID), nil).expect(http.StatusNotFound)
}

func TestAcademicRecordReview(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	studentID := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Transfer"})
	recordID := admin.create("/students/"+studentID+"/academic-records", map[string]any{"school_name": "Old School", "academic_year_label": "2015 E.C.", "grade_level": "8", "subject_name": "Maths", "score": 80, "maximum_score": 100, "source_type": "staff_entry"})
	admin.call("POST", "/academic-records/"+recordID+"/verification", map[string]string{"status": "maybe"}).expect(http.StatusBadRequest)
	admin.call("POST", "/academic-records/"+recordID+"/verification", map[string]string{"status": "verified"}).expect(http.StatusOK)
	if rows := admin.call("GET", "/students/"+studentID+"/academic-records", nil).expect(http.StatusOK).list(); rows[0]["verification_status"] != "verified" {
		t.Fatalf("verified: %v", rows)
	}
	_, other := seedSchool(t, server)
	other.call("POST", "/academic-records/"+recordID+"/verification", map[string]string{"status": "rejected"}).expect(http.StatusNotFound)
	other.call("DELETE", "/academic-records/"+recordID, nil).expect(http.StatusNotFound)
	admin.call("DELETE", "/academic-records/"+recordID, nil).expect(http.StatusOK)
	if rows := admin.call("GET", "/students/"+studentID+"/academic-records", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("deleted: %v", rows)
	}

	// A spreadsheet saved by Excel starts with a byte order mark.
	csv := "\xef\xbb\xbfstudent_number,school_name,academic_year_label,grade_level,subject_name,score,maximum_score,grade_label,source_type\n"
	preview := admin.call("POST", "/import-jobs/preview", rawCSV(csv))
	if preview.Status == http.StatusBadRequest && strings.Contains(string(preview.Body), "headers") {
		t.Fatalf("a byte order mark should not break the headers: %s", preview.Body)
	}
}

func TestQuestionArchive(t *testing.T) {
	server, admin, _, learner, classID, _ := prepFixture(t)
	skill := createSkill(admin)
	question := publishQuestion(admin, skill, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": "1 + 1?"}, "options": []map[string]string{{"id": "A", "text": "2"}, {"id": "B", "text": "3"}}, "answer_key": []string{"A"}})
	_, assignmentID := publishAssessment(admin, classID, []string{question})
	admin.call("POST", "/questions/"+question+"/archive", map[string]any{}).expect(http.StatusOK)
	admin.call("POST", "/questions/"+question+"/archive", map[string]any{}).expect(http.StatusConflict)
	// Work that already uses it carries on.
	learner.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{"start_code": ""}).expect(http.StatusCreated)
	if view := learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object(); len(view["questions"].([]any)) != 1 {
		t.Fatalf("an archived question stays in tests already set: %v", view)
	}
	// New tests cannot use it.
	admin.call("POST", "/assessments", map[string]any{"title": "New", "instructions": "", "duration_minutes": 30, "question_ids": []string{question}}).expect(http.StatusBadRequest)
	_, other := seedSchool(t, server)
	other.call("POST", "/questions/"+question+"/archive", map[string]any{}).expect(http.StatusNotFound)
}

func TestInactiveStudentCannotSignIn(t *testing.T) {
	server, admin, _, learner, _, studentID := prepFixture(t)
	learner.call("GET", "/me", nil).expect(http.StatusOK)
	admin.call("PATCH", "/students/"+studentID, map[string]string{"legal_name": "Test Learner", "status": "inactive"}).expect(http.StatusOK)
	learner.call("GET", "/me", nil).expect(http.StatusUnauthorized)
	var email string
	if err := testDB.QueryRow(context.Background(), `SELECT u.email FROM users u JOIN students s ON s.user_id=u.id WHERE s.id=$1`, studentID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	newClient(t, server).call("POST", "/auth/login", map[string]string{"email": email, "password": testPassword}).expect(http.StatusUnauthorized)
	// Back to active through the profile, and they can sign in again.
	admin.call("PUT", "/students/"+studentID+"/profile", map[string]any{"legal_name": "Test Learner", "status": "active"}).expect(http.StatusOK)
	newClient(t, server).login(email)
}

func TestInvitationsCheckEveryEmail(t *testing.T) {
	server := newTestServer(t)
	_, adminA := seedSchool(t, server)
	_, adminB := seedSchool(t, server)
	teacherID, _ := inviteTeacher(server, adminA)
	var email string
	if err := testDB.QueryRow(context.Background(), `SELECT email FROM users WHERE id=$1`, teacherID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	refused := adminB.call("POST", "/staff/invitations", map[string]string{"email": email})
	refused.expect(http.StatusConflict)
	if !strings.Contains(string(refused.Body), "another school") {
		t.Fatalf("the message should explain: %s", refused.Body)
	}
	studentID := adminB.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "B student"})
	adminB.call("POST", "/students/"+studentID+"/invitation", map[string]string{"email": email}).expect(http.StatusConflict)
	guardian := addGuardian(adminB, studentID, "Parent")
	adminB.call("POST", "/students/"+studentID+"/guardians/"+guardian+"/invitation", map[string]string{"email": email}).expect(http.StatusConflict)
}

func TestRequestsAreLoggedWithoutDetails(t *testing.T) {
	requireDB(t)
	var logs bytes.Buffer
	service := auth.NewService(testDB, []byte("test-only-session-hash-key-32-bytes!!"), time.Hour)
	server := httptest.NewServer(newAPI(service, testDB, false, time.Hour, slog.New(slog.NewTextHandler(&logs, nil)), testRateLimits()).routes())
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/students/00000000-0000-0000-0000-000000000000?q=private")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.Header.Get("X-Request-ID") == "" || response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers: %v", response.Header)
	}
	line := logs.String()
	if !strings.Contains(line, "route=\"GET /api/v1/students/{studentID}\"") || !strings.Contains(line, "status=401") {
		t.Fatalf("log: %s", line)
	}
	if strings.Contains(line, "00000000-0000") || strings.Contains(line, "private") {
		t.Fatalf("the log must not hold IDs or query values: %s", line)
	}
}

func TestCancelledRequestsAreNotErrors(t *testing.T) {
	var logs bytes.Buffer
	a := &API{log: slog.New(slog.NewTextHandler(&logs, nil))}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	a.serverError(recorder, httptest.NewRequest("GET", "/x", nil).WithContext(ctx), ctx.Err())
	if recorder.Code != 499 || strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("status %d, logs %q", recorder.Code, logs.String())
	}
	a.serverError(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil), context.Canceled)
	if !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatal("a cancelled database call during a live request must still be logged")
	}
}
