package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// inviteStaff runs the invitation flow for a role and returns the user ID and
// a signed-in client.
func inviteStaff(server *httptest.Server, admin *apiClient, role string) (string, *apiClient) {
	t := admin.t
	t.Helper()
	email := uniqueEmail(role)
	invite := admin.call("POST", "/staff/invitations", map[string]string{"email": email, "role": role}).expect(http.StatusCreated).object()
	if invite["role"] != role {
		t.Fatalf("invitation role %v", invite["role"])
	}
	newClient(t, server).call("POST", "/staff/invitations/accept", map[string]string{"token": invite["token"].(string), "display_name": "Office " + role, "password": testPassword}).expect(http.StatusCreated)
	client := newClient(t, server)
	client.login(email)
	userID, _ := client.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	return userID, client
}

func TestOfficeRoles(t *testing.T) {
	server, admin, teacher, _, classID, studentID := prepFixture(t)
	admin.call("POST", "/staff/invitations", map[string]string{"email": uniqueEmail("x"), "role": "school_admin"}).expect(http.StatusBadRequest)
	registrarID, registrar := inviteStaff(server, admin, "registrar")
	_, finance := inviteStaff(server, admin, "finance_officer")

	// The registrar keeps student records.
	newStudent := registrar.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "New Admission", "class_id": classID})
	registrar.call("GET", "/students?q=Admission", nil).expect(http.StatusOK)
	registrar.call("GET", "/students/"+newStudent+"/profile", nil).expect(http.StatusOK)
	registrar.create("/students/"+newStudent+"/guardians", map[string]any{"full_name": "Parent", "relationship": "mother", "phone": "0911000000"})
	registrar.call("POST", "/students/"+studentID+"/academic-records", map[string]any{"school_name": "Old School", "academic_year_label": "2015 E.C.", "grade_level": "7", "subject_name": "Maths", "score": 70, "maximum_score": 100, "source_type": "staff_entry"}).expect(http.StatusCreated)
	registrar.call("GET", "/attendance/overview?date=2026-10-01", nil).expect(http.StatusOK)
	registrar.call("POST", "/students/"+newStudent+"/invitation", map[string]string{"email": uniqueEmail("kid")}).expect(http.StatusCreated)
	// It does not run the school, its money or its teaching.
	for _, forbidden := range [][3]string{
		{"GET", "/finance/entries", ""}, {"GET", "/staff", ""}, {"POST", "/classes", "{}"}, {"PATCH", "/school", "{}"},
		{"GET", "/notifications/overview", ""}, {"POST", "/terms", "{}"}, {"PUT", "/attendance/classes/" + classID, "{}"},
	} {
		var body any
		if forbidden[2] != "" {
			body = map[string]any{}
		}
		registrar.call(forbidden[0], forbidden[1], body).expect(http.StatusForbidden)
	}

	// The finance officer keeps the ledger and looks up students and staff.
	finance.create("/finance/entries", map[string]any{"entry_type": "tuition_charge", "category": "Tuition", "description": "Term fee", "amount_minor": 100000, "currency_code": "ETB", "student_id": studentID, "status": "recorded"})
	finance.call("GET", "/finance/summary", nil).expect(http.StatusOK)
	finance.call("GET", "/students", nil).expect(http.StatusOK)
	finance.call("GET", "/staff?all=1", nil).expect(http.StatusOK)
	finance.call("POST", "/students", map[string]string{"student_number": unique("S"), "legal_name": "X"}).expect(http.StatusForbidden)
	finance.call("GET", "/students/"+studentID+"/profile", nil).expect(http.StatusForbidden)
	finance.call("POST", "/staff/invitations", map[string]string{"email": uniqueEmail("y")}).expect(http.StatusForbidden)
	teacher.call("GET", "/finance/entries", nil).expect(http.StatusForbidden)
	teacher.call("POST", "/students", map[string]string{"student_number": unique("S"), "legal_name": "X"}).expect(http.StatusForbidden)

	// Everyone in the office has an inbox.
	registrar.call("GET", "/me/notifications/unread", nil).expect(http.StatusOK)
	finance.call("GET", "/me/notifications/unread", nil).expect(http.StatusOK)

	// The staff list shows roles; pickers still get teachers only.
	everyone := admin.call("GET", "/staff?all=1", nil).expect(http.StatusOK).list()
	teachers := admin.call("GET", "/staff", nil).expect(http.StatusOK).list()
	if !containsID(everyone, registrarID) || containsID(teachers, registrarID) {
		t.Fatal("staff lists")
	}
	// Roles change, but the school keeps an administrator.
	admin.call("PUT", "/staff/"+registrarID+"/roles", map[string]any{"roles": []string{"registrar", "teacher"}}).expect(http.StatusOK)
	admin.call("PUT", "/staff/"+registrarID+"/roles", map[string]any{"roles": []string{"student"}}).expect(http.StatusBadRequest)
	adminID, _ := admin.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	if code := admin.call("PUT", "/staff/"+adminID+"/roles", map[string]any{"roles": []string{"teacher"}}).expect(http.StatusConflict).errorCode(); code != "OWN_ADMIN_ROLE" {
		t.Fatalf("code %s", code)
	}
	admin.call("PATCH", "/staff/"+adminID, map[string]string{"status": "disabled"}).expect(http.StatusConflict)
	registrar.call("GET", "/teacher/classes", nil).expect(http.StatusOK)
	admin.call("PUT", "/staff/"+registrarID+"/roles", map[string]any{"roles": []string{"teacher"}}).expect(http.StatusOK)
	registrar.call("GET", "/students", nil).expect(http.StatusForbidden)
	admin.call("PATCH", "/staff/"+registrarID, map[string]string{"status": "disabled"}).expect(http.StatusOK)
	_, other := seedSchool(t, server)
	other.call("PUT", "/staff/"+registrarID+"/roles", map[string]any{"roles": []string{"teacher"}}).expect(http.StatusNotFound)
}

func TestHomeroomTeacher(t *testing.T) {
	s := setupReports(t)
	var teacherID string
	teacherID, _ = s.teacher.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	path := "/classes/" + s.classID + "/homeroom"
	outsiderID, _ := inviteTeacher(s.server, s.admin)
	s.admin.call("PUT", path, map[string]string{"teacher_id": outsiderID}).expect(http.StatusBadRequest)
	s.teacher.call("PUT", path, map[string]string{"teacher_id": teacherID}).expect(http.StatusForbidden)
	s.admin.call("PUT", path, map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)

	classes := s.teacher.call("GET", "/teacher/classes", nil).expect(http.StatusOK).list()
	if len(classes) != 1 || classes[0]["homeroom"] != true {
		t.Fatalf("teacher classes: %v", classes)
	}
	staff := s.admin.call("GET", "/staff", nil).expect(http.StatusOK).list()
	for _, member := range staff {
		if member["id"] == teacherID && len(member["homeroom_class_ids"].([]any)) != 1 {
			t.Fatalf("staff: %v", member)
		}
	}

	// Report card comments belong to the homeroom teacher.
	notes := map[string]any{"term_id": s.term1, "notes": []map[string]string{{"student_id": s.studentID, "conduct": "A", "comment": "Kind and careful."}}}
	if code := s.other.call("PUT", "/report-cards/classes/"+s.classID+"/notes", notes).expect(http.StatusForbidden).errorCode(); code != "HOMEROOM_ONLY" {
		t.Fatalf("code %s", code)
	}
	s.teacher.call("PUT", "/report-cards/classes/"+s.classID+"/notes", notes).expect(http.StatusOK)
	s.admin.call("PUT", "/report-cards/classes/"+s.classID+"/notes", notes).expect(http.StatusOK)

	// The homeroom teacher can message the class, but not by SMS.
	message := map[string]any{"title": "Trip on Friday", "body": "Bring lunch.", "channels": []string{"email"}}
	s.other.call("POST", "/teacher/classes/"+s.classID+"/announcements", message).expect(http.StatusForbidden)
	s.teacher.call("POST", "/teacher/classes/"+s.classID+"/announcements", map[string]any{"title": "X", "body": "Y", "channels": []string{"sms"}}).expect(http.StatusForbidden)
	sent := s.teacher.call("POST", "/teacher/classes/"+s.classID+"/announcements", message).expect(http.StatusCreated).object()
	if sent["recipients"].(float64) < 2 {
		t.Fatalf("sent: %v", sent)
	}
	items := s.learner.call("GET", "/me/notifications", nil).expect(http.StatusOK).object()["items"].([]any)
	if len(items) == 0 || items[0].(map[string]any)["title"] != "Trip on Friday" {
		t.Fatalf("learner inbox: %v", items)
	}

	// Taking the teacher off the class ends the homeroom.
	s.admin.call("DELETE", "/classes/"+s.classID+"/teachers/"+teacherID, nil).expect(http.StatusOK)
	for _, c := range s.admin.call("GET", "/classes", nil).expect(http.StatusOK).list() {
		if c["id"] == s.classID && c["homeroom_teacher_id"] != "" {
			t.Fatalf("homeroom kept: %v", c)
		}
	}
	s.other.call("PUT", "/report-cards/classes/"+s.classID+"/notes", notes).expect(http.StatusOK)
}
