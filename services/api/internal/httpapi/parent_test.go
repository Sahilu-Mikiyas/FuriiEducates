package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// signUpParent redeems an invitation and returns a signed-in parent.
func signUpParent(t *testing.T, server *httptest.Server, email, token string) *apiClient {
	t.Helper()
	newClient(t, server).call("POST", "/guardian-invitations/accept", map[string]string{"token": token, "display_name": "Almaz Tesfaye", "password": testPassword}).expect(http.StatusCreated)
	parent := newClient(t, server)
	parent.login(email)
	return parent
}

func addGuardian(admin *apiClient, studentID, name string) string {
	admin.t.Helper()
	return admin.create("/students/"+studentID+"/guardians", map[string]any{"full_name": name, "relationship": "mother", "phone": "0911234567"})
}

func inviteToken(admin *apiClient, studentID, guardianID, email string) string {
	admin.t.Helper()
	token, _ := admin.call("POST", "/students/"+studentID+"/guardians/"+guardianID+"/invitation", map[string]string{"email": email}).expect(http.StatusCreated).object()["token"].(string)
	if token == "" {
		admin.t.Fatal("no invitation token")
	}
	return token
}

func TestParentInvitationAndAccount(t *testing.T) {
	server, admin, teacher, learner, _, studentID := prepFixture(t)
	guardianID := addGuardian(admin, studentID, "Almaz")
	path := "/students/" + studentID + "/guardians/" + guardianID
	email := uniqueEmail("parent")

	teacher.call("POST", path+"/invitation", map[string]string{"email": email}).expect(http.StatusForbidden)
	admin.call("POST", path+"/invitation", map[string]string{"email": "not an email"}).expect(http.StatusBadRequest)
	first := inviteToken(admin, studentID, guardianID, email)
	token := inviteToken(admin, studentID, guardianID, email)

	// A new invitation replaces the old; weak passwords and used links are refused.
	newClient(t, server).call("POST", "/guardian-invitations/accept", map[string]string{"token": first, "display_name": "X", "password": testPassword}).expect(http.StatusBadRequest)
	newClient(t, server).call("POST", "/guardian-invitations/accept", map[string]string{"token": token, "display_name": "X", "password": "short"}).expect(http.StatusBadRequest)
	profile := admin.call("GET", "/students/"+studentID+"/profile", nil).expect(http.StatusOK).object()["guardians"].([]any)[0].(map[string]any)
	if profile["has_account"] != false || profile["invite_pending"] != true {
		t.Fatalf("before accepting: %v", profile)
	}
	parent := signUpParent(t, server, email, token)
	newClient(t, server).call("POST", "/guardian-invitations/accept", map[string]string{"token": token, "display_name": "X", "password": testPassword}).expect(http.StatusBadRequest)
	profile = admin.call("GET", "/students/"+studentID+"/profile", nil).expect(http.StatusOK).object()["guardians"].([]any)[0].(map[string]any)
	if profile["has_account"] != true || profile["invite_pending"] != false {
		t.Fatalf("after accepting: %v", profile)
	}
	admin.call("POST", path+"/invitation", map[string]string{"email": uniqueEmail("x")}).expect(http.StatusConflict)

	// The account can read the portal and nothing staff or students use.
	children := parent.call("GET", "/parent/children", nil).expect(http.StatusOK).list()
	if len(children) != 1 || children[0]["id"] != studentID {
		t.Fatalf("children: %v", children)
	}
	parent.call("GET", "/students", nil).expect(http.StatusForbidden)
	parent.call("GET", "/students/"+studentID+"/profile", nil).expect(http.StatusForbidden)
	parent.call("GET", "/student/report-cards", nil).expect(http.StatusForbidden)
	parent.call("GET", "/teacher/classes", nil).expect(http.StatusForbidden)
	learner.call("GET", "/parent/children", nil).expect(http.StatusForbidden)
	teacher.call("GET", "/parent/children", nil).expect(http.StatusForbidden)
	newClient(t, server).call("GET", "/parent/children", nil).expect(http.StatusUnauthorized)

	// A reset link works; a guardian with no account has none to reset.
	if reset := admin.call("POST", path+"/password-reset", map[string]any{}).expect(http.StatusCreated).object(); reset["token"] == nil {
		t.Fatalf("reset: %v", reset)
	}
	other := addGuardian(admin, studentID, "Bekele")
	admin.call("POST", "/students/"+studentID+"/guardians/"+other+"/password-reset", map[string]any{}).expect(http.StatusNotFound)

	// Removing access takes effect at once and keeps the guardian's record.
	teacher.call("DELETE", path+"/account", nil).expect(http.StatusForbidden)
	admin.call("DELETE", path+"/account", nil).expect(http.StatusOK)
	if rows := parent.call("GET", "/parent/children", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("access should be gone: %v", rows)
	}
	parent.call("GET", "/parent/children/"+studentID, nil).expect(http.StatusNotFound)
	if guardians := admin.call("GET", "/students/"+studentID+"/profile", nil).expect(http.StatusOK).object()["guardians"].([]any); len(guardians) != 2 {
		t.Fatalf("guardian records stay: %v", guardians)
	}
}

func TestParentWithSeveralChildren(t *testing.T) {
	server, admin, _, learner, classID, firstChild := prepFixture(t)
	secondChild, _ := enrollLearner(server, admin, classID)
	thirdChild, _ := enrollLearner(server, admin, classID)
	stranger, _ := enrollLearner(server, admin, classID)
	email := uniqueEmail("parent")

	// Both invited before signing up: accepting one link connects both children.
	firstGuardian := addGuardian(admin, firstChild, "Almaz")
	secondGuardian := addGuardian(admin, secondChild, "Almaz")
	token := inviteToken(admin, firstChild, firstGuardian, email)
	otherToken := inviteToken(admin, secondChild, secondGuardian, email)
	parent := signUpParent(t, server, email, token)
	if rows := parent.call("GET", "/parent/children", nil).expect(http.StatusOK).list(); len(rows) != 2 {
		t.Fatalf("both children should be linked: %v", rows)
	}
	newClient(t, server).call("POST", "/guardian-invitations/accept", map[string]string{"token": otherToken, "display_name": "X", "password": testPassword}).expect(http.StatusBadRequest)

	// Invited afterwards: the existing account is linked with no new sign-up.
	thirdGuardian := addGuardian(admin, thirdChild, "Almaz")
	linked := admin.call("POST", "/students/"+thirdChild+"/guardians/"+thirdGuardian+"/invitation", map[string]string{"email": email}).expect(http.StatusOK).object()
	if linked["linked"] != true || linked["token"] != nil {
		t.Fatalf("link: %v", linked)
	}
	if rows := parent.call("GET", "/parent/children", nil).expect(http.StatusOK).list(); len(rows) != 3 {
		t.Fatalf("three children: %v", rows)
	}
	// The same account cannot be linked twice to one child.
	secondParent := addGuardian(admin, thirdChild, "Bekele")
	admin.call("POST", "/students/"+thirdChild+"/guardians/"+secondParent+"/invitation", map[string]string{"email": email}).expect(http.StatusConflict)

	// No child but their own.
	parent.call("GET", "/parent/children/"+firstChild, nil).expect(http.StatusOK)
	parent.call("GET", "/parent/children/"+stranger, nil).expect(http.StatusNotFound)
	parent.call("GET", "/parent/children/nope", nil).expect(http.StatusNotFound)

	// A student's account cannot become a parent's.
	var learnerEmail string
	if err := testDB.QueryRow(context.Background(), `SELECT u.email FROM users u JOIN students s ON s.user_id=u.id WHERE s.id=$1`, firstChild).Scan(&learnerEmail); err != nil {
		t.Fatal(err)
	}
	strangerGuardian := addGuardian(admin, stranger, "Someone")
	refused := admin.call("POST", "/students/"+stranger+"/guardians/"+strangerGuardian+"/invitation", map[string]string{"email": learnerEmail})
	refused.expect(http.StatusConflict)
	if refused.errorCode() != "ACCOUNT_EXISTS" {
		t.Fatalf("code %s", refused.errorCode())
	}
	if rows := learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("learner: %v", rows)
	}

	// A teacher who is also a parent keeps both roles.
	teacherID, teacher := inviteTeacher(server, admin)
	var teacherEmail string
	if err := testDB.QueryRow(context.Background(), `SELECT email FROM users WHERE id=$1`, teacherID).Scan(&teacherEmail); err != nil {
		t.Fatal(err)
	}
	admin.call("POST", "/students/"+stranger+"/guardians/"+strangerGuardian+"/invitation", map[string]string{"email": teacherEmail}).expect(http.StatusOK)
	if rows := teacher.call("GET", "/parent/children", nil).expect(http.StatusOK).list(); len(rows) != 1 || rows[0]["id"] != stranger {
		t.Fatalf("teacher as parent: %v", rows)
	}
	teacher.call("GET", "/teacher/classes", nil).expect(http.StatusOK)
}

func TestParentSeesOnlyWhatIsPublished(t *testing.T) {
	s := setupReports(t)
	guardianID := addGuardian(s.admin, s.studentID, "Almaz")
	email := uniqueEmail("parent")
	parent := signUpParent(t, s.server, email, inviteToken(s.admin, s.studentID, guardianID, email))
	child := "/parent/children/" + s.studentID

	empty := parent.call("GET", child, nil).expect(http.StatusOK).object()
	if len(empty["report_cards"].([]any)) != 0 || empty["readiness"] != nil {
		t.Fatalf("empty child: %v", empty)
	}
	if fees := empty["fees"].(map[string]any); len(fees["balances"].([]any)) != 0 {
		t.Fatalf("no fees yet: %v", fees)
	}

	// Marks entered but not published stay out of view.
	exam := s.item(s.admin, s.math, s.term1, "Exam", 100, 100)
	essay := s.item(s.admin, s.english, s.term1, "Essay", 100, 100)
	for _, student := range []string{s.studentID, s.secondID} {
		s.mark(s.admin, s.math, s.term1, exam, student, 80).expect(http.StatusOK)
		s.mark(s.admin, s.english, s.term1, essay, student, 60).expect(http.StatusOK)
	}
	if cards := parent.call("GET", child, nil).expect(http.StatusOK).object()["report_cards"].([]any); len(cards) != 0 {
		t.Fatalf("unpublished marks must not show: %v", cards)
	}
	path := "/report-cards/classes/" + s.classID
	s.admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1}).expect(http.StatusOK)
	data := parent.call("GET", child, nil).expect(http.StatusOK).object()
	cards := data["report_cards"].([]any)
	if len(cards) != 1 || cards[0].(map[string]any)["student_id"] != s.studentID || cards[0].(map[string]any)["average"] != float64(70) {
		t.Fatalf("published card: %v", cards)
	}
	summary := parent.call("GET", "/parent/children", nil).expect(http.StatusOK).list()[0]
	if summary["latest_average"] != float64(70) || summary["latest_term"] != "Semester 1" || summary["latest_rank"] != float64(1) {
		t.Fatalf("summary: %v", summary)
	}
	// Taking reports back takes them from the parent too.
	s.admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term1, "reason": "Correcting a mark"}).expect(http.StatusOK)
	if cards := parent.call("GET", child, nil).expect(http.StatusOK).object()["report_cards"].([]any); len(cards) != 0 {
		t.Fatalf("withdrawn card: %v", cards)
	}

	// Attendance.
	s.admin.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": "2026-10-20", "records": []map[string]any{{"student_id": s.studentID, "status": "absent", "note": "Fever"}}}).expect(http.StatusOK)
	recent := parent.call("GET", child, nil).expect(http.StatusOK).object()["attendance"].(map[string]any)["recent"].([]any)
	if len(recent) != 1 || recent[0].(map[string]any)["note"] != "Fever" {
		t.Fatalf("attendance: %v", recent)
	}

	// Fees: voided entries, other students and other currencies stay apart.
	entry := func(kind, student, currency string, amount int, status string) {
		s.admin.call("POST", "/finance/entries", map[string]any{"entry_type": kind, "category": "Tuition", "description": "Term fee", "amount_minor": amount, "currency_code": currency, "student_id": student, "status": status}).expect(http.StatusCreated)
	}
	entry("tuition_charge", s.studentID, "ETB", 500000, "recorded")
	entry("payment", s.studentID, "ETB", 200000, "paid")
	entry("payment", s.studentID, "ETB", 100000, "void")
	entry("tuition_charge", s.secondID, "ETB", 999900, "recorded")
	fees := parent.call("GET", child, nil).expect(http.StatusOK).object()["fees"].(map[string]any)
	balances := fees["balances"].([]any)
	if len(balances) != 1 || balances[0].(map[string]any)["due_minor"] != float64(300000) {
		t.Fatalf("balance: %v", balances)
	}
	if entries := fees["entries"].([]any); len(entries) != 2 {
		t.Fatalf("entries: %v", entries)
	}

	// Readiness once a stream is chosen.
	if _, err := testDB.Exec(context.Background(), `UPDATE students SET stream='natural' WHERE id=$1`, s.studentID); err != nil {
		t.Fatal(err)
	}
	if readiness := parent.call("GET", child, nil).expect(http.StatusOK).object()["readiness"].(map[string]any); readiness["status"] != "need_more_evidence" {
		t.Fatalf("readiness: %v", readiness)
	}
}

func TestParentStaysInsideTheSchool(t *testing.T) {
	server, admin, _, _, _, studentID := prepFixture(t)
	guardianID := addGuardian(admin, studentID, "Almaz")
	email := uniqueEmail("parent")
	token := inviteToken(admin, studentID, guardianID, email)
	path := "/students/" + studentID + "/guardians/" + guardianID
	_, other := seedSchool(t, server)
	other.call("POST", path+"/invitation", map[string]string{"email": uniqueEmail("x")}).expect(http.StatusNotFound)
	other.call("POST", path+"/password-reset", map[string]any{}).expect(http.StatusNotFound)
	other.call("DELETE", path+"/account", nil).expect(http.StatusNotFound)
	// A guardian reached through the wrong student is not found.
	secondStudent := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Other"})
	admin.call("POST", "/students/"+secondStudent+"/guardians/"+guardianID+"/invitation", map[string]string{"email": uniqueEmail("x")}).expect(http.StatusNotFound)
	parent := signUpParent(t, server, email, token)

	// The same email in another school is a separate account with no children.
	_, otherAdmin := seedSchool(t, server)
	otherClass := createClass(otherAdmin)
	otherStudent, _ := enrollLearner(server, otherAdmin, otherClass)
	parent.call("GET", "/parent/children/"+otherStudent, nil).expect(http.StatusNotFound)
}
