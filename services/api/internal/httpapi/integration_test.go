package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestHealthReportsDatabaseConnectivity(t *testing.T) {
	server := newTestServer(t)
	response, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
}

func TestProtectedRoutesRequireAuthentication(t *testing.T) {
	server := newTestServer(t)
	anonymous := newClient(t, server)
	for _, path := range []string{"/me", "/students", "/teacher/classes", "/student/assessments", "/finance/entries"} {
		if code := anonymous.call("GET", path, nil).expect(http.StatusUnauthorized).errorCode(); code != "UNAUTHENTICATED" {
			t.Errorf("GET %s returned error code %q", path, code)
		}
	}
	anonymous.call("POST", "/auth/login", map[string]string{"email": "nobody@example.test", "password": "wrong-password-123"}).expect(http.StatusUnauthorized)
}

func TestLogoutRevokesSession(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	admin.call("GET", "/school", nil).expect(http.StatusOK)
	admin.call("POST", "/auth/logout", map[string]any{}).expect(http.StatusOK)
	admin.call("GET", "/school", nil).expect(http.StatusUnauthorized)
}

func TestRolesCannotUseOtherRolesRoutes(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	_, teacher := inviteTeacher(server, admin)
	_, learner := enrollLearner(server, admin, classID)

	teacher.call("GET", "/students", nil).expect(http.StatusForbidden)
	teacher.call("GET", "/finance/entries", nil).expect(http.StatusForbidden)
	learner.call("GET", "/students", nil).expect(http.StatusForbidden)
	learner.call("GET", "/teacher/classes", nil).expect(http.StatusForbidden)
	admin.call("GET", "/student/assessments", nil).expect(http.StatusForbidden)
}

func TestMalformedIdentifiersReturnClientErrors(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	for _, path := range []string{"/students/not-a-uuid", "/questions/123", "/students/x/academic-records", "/import-jobs/abc"} {
		if code := admin.call("GET", path, nil).expect(http.StatusNotFound).errorCode(); code != "NOT_FOUND" {
			t.Errorf("GET %s returned error code %q", path, code)
		}
	}
	// A malformed identifier inside a JSON body is a validation error, not a server error.
	admin.call("POST", "/classes", map[string]string{"name": "A", "grade_level": "8", "academic_year_id": "not-a-uuid"}).expect(http.StatusBadRequest)
}

func TestSchoolDataIsIsolatedBetweenSchools(t *testing.T) {
	server := newTestServer(t)
	_, schoolA := seedSchool(t, server)
	_, schoolB := seedSchool(t, server)

	classA := createClass(schoolA)
	studentA := schoolA.create("/students", map[string]string{"student_number": unique("A"), "legal_name": "School A Learner", "class_id": classA})
	schoolA.create("/students/"+studentA+"/academic-records", map[string]any{"school_name": "Prior", "academic_year_label": "2024", "grade_level": "7", "subject_name": "Math", "score": 80, "maximum_score": 100, "source_type": "staff_entry"})
	skillA := createSkill(schoolA)
	questionA := publishQuestion(schoolA, skillA, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": "Pick A"}, "options": []map[string]string{{"id": "A", "text": "A"}, {"id": "B", "text": "B"}}, "answer_key": []string{"A"}})
	assessmentA, _ := publishAssessment(schoolA, classA, []string{questionA})
	financeA := schoolA.create("/finance/entries", map[string]any{"entry_type": "expense", "category": "Supplies", "description": "Chalk", "amount_minor": 5000, "currency_code": "ETB", "status": "recorded"})

	classB := createClass(schoolB)

	if containsID(schoolB.call("GET", "/students", nil).expect(http.StatusOK).list(), studentA) {
		t.Error("school B can list school A's student")
	}
	if containsID(schoolB.call("GET", "/classes", nil).expect(http.StatusOK).list(), classA) {
		t.Error("school B can list school A's class")
	}
	if containsID(schoolB.call("GET", "/questions", nil).expect(http.StatusOK).list(), questionA) {
		t.Error("school B can list school A's question")
	}
	if containsID(schoolB.call("GET", "/assessments", nil).expect(http.StatusOK).list(), assessmentA) {
		t.Error("school B can list school A's assessment")
	}
	if containsID(schoolB.call("GET", "/finance/entries", nil).expect(http.StatusOK).list(), financeA) {
		t.Error("school B can list school A's finance entry")
	}

	schoolB.call("GET", "/students/"+studentA, nil).expect(http.StatusNotFound)
	schoolB.call("PATCH", "/students/"+studentA, map[string]string{"legal_name": "Hijacked", "status": "active"}).expect(http.StatusNotFound)
	schoolB.call("GET", "/students/"+studentA+"/academic-records", nil).expect(http.StatusNotFound)
	schoolB.call("POST", "/students/"+studentA+"/academic-records", map[string]any{"school_name": "X", "academic_year_label": "2024", "grade_level": "7", "subject_name": "Math", "source_type": "staff_entry"}).expect(http.StatusNotFound)
	schoolB.call("POST", "/students/"+studentA+"/invitation", map[string]string{"email": uniqueEmail("x")}).expect(http.StatusNotFound)
	schoolB.call("GET", "/questions/"+questionA, nil).expect(http.StatusNotFound)
	schoolB.call("POST", "/questions/"+questionA+"/publish", map[string]any{}).expect(http.StatusNotFound)
	schoolB.call("POST", "/assessments", map[string]any{"title": "Stolen", "duration_minutes": 30, "question_ids": []string{questionA}}).expect(http.StatusBadRequest)
	schoolB.call("POST", "/assessments/"+assessmentA+"/assignments", map[string]string{"class_id": classB}).expect(http.StatusNotFound)
	schoolB.call("POST", "/students", map[string]string{"student_number": unique("B"), "legal_name": "B", "class_id": classA}).expect(http.StatusBadRequest)

	_, teacherB := inviteTeacher(server, schoolB)
	teacherBID, _ := teacherB.call("GET", "/me", nil).expect(http.StatusOK).object()["user_id"].(string)
	schoolA.call("POST", "/classes/"+classA+"/teachers", map[string]string{"teacher_id": teacherBID}).expect(http.StatusNotFound)

	// School A's record is untouched.
	if name := schoolA.call("GET", "/students/"+studentA, nil).expect(http.StatusOK).object()["legal_name"]; name != "School A Learner" {
		t.Errorf("school A's student was modified: %v", name)
	}
}

func TestCurriculumIsIsolatedBetweenSchools(t *testing.T) {
	t.Skip("known issue P0-1 in docs/system-analysis.md: curriculum tables are not school-scoped yet (Phase 1)")
	server := newTestServer(t)
	_, schoolA := seedSchool(t, server)
	_, schoolB := seedSchool(t, server)
	curriculumA := schoolA.create("/curricula", map[string]string{"name": "Private", "provider": "School A", "version_label": unique("v")})
	if containsID(schoolB.call("GET", "/curricula", nil).expect(http.StatusOK).list(), curriculumA) {
		t.Error("school B can list school A's curriculum")
	}
	schoolB.call("POST", "/curricula/"+curriculumA+"/subjects", map[string]string{"name": "Injected"}).expect(http.StatusNotFound)
}

func TestTeachersOnlyReachAssignedClasses(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	assignedClass := createClass(admin)
	otherClass := createClass(admin)
	teacherID, teacher := inviteTeacher(server, admin)
	admin.call("POST", "/classes/"+assignedClass+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	studentInOther := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Other Class Learner", "class_id": otherClass})

	classes := teacher.call("GET", "/teacher/classes", nil).expect(http.StatusOK).list()
	if len(classes) != 1 || classes[0]["id"] != assignedClass {
		t.Fatalf("teacher should see exactly the assigned class, got %v", classes)
	}
	if containsID(teacher.call("GET", "/teacher/classes/"+otherClass+"/students", nil).expect(http.StatusOK).list(), studentInOther) {
		t.Error("teacher can see the roster of an unassigned class")
	}
	teacher.call("GET", "/teacher/reviews?class_id="+otherClass, nil).expect(http.StatusForbidden)
	teacher.call("GET", "/teacher/classes/"+otherClass+"/learning-profile", nil).expect(http.StatusForbidden)
	teacher.call("GET", "/teacher/classes/"+otherClass+"/resources", nil).expect(http.StatusForbidden)

	// Disabling the teacher revokes their existing session.
	admin.call("PATCH", "/staff/"+teacherID, map[string]string{"status": "disabled"}).expect(http.StatusOK)
	teacher.call("GET", "/teacher/classes", nil).expect(http.StatusUnauthorized)
}

// lifecycleFixture is a published, assigned three-question assessment with an
// enrolled learner and an assigned teacher.
type lifecycleFixture struct {
	admin, teacher, learner        *apiClient
	assignmentID                   string
	choiceID, numericID, writtenID string
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	teacherID, teacher := inviteTeacher(server, admin)
	admin.call("POST", "/classes/"+classID+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	_, learner := enrollLearner(server, admin, classID)
	skillID := createSkill(admin)
	choiceID := publishQuestion(admin, skillID, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": "1/2 + 1/4?"}, "options": []map[string]string{{"id": "A", "text": "3/4"}, {"id": "B", "text": "2/6"}}, "answer_key": []string{"A"}})
	numericID := publishQuestion(admin, skillID, map[string]any{"question_type": "numeric", "prompt": map[string]string{"text": "3 x 4?"}, "answer_key": map[string]float64{"value": 12, "tolerance": 0.5}})
	writtenID := publishQuestion(admin, skillID, map[string]any{"question_type": "text", "prompt": map[string]string{"text": "Explain your method."}, "answer_key": map[string]string{"guide": "Mentions common denominators."}})
	_, assignmentID := publishAssessment(admin, classID, []string{choiceID, numericID, writtenID})
	return lifecycleFixture{admin: admin, teacher: teacher, learner: learner, assignmentID: assignmentID, choiceID: choiceID, numericID: numericID, writtenID: writtenID}
}

func (f lifecycleFixture) start(t *testing.T) string {
	t.Helper()
	// The start endpoint currently responds without the {data: ...} envelope.
	attemptID, _ := f.learner.call("POST", "/student/assessments/"+f.assignmentID+"/start", map[string]any{}).expect(http.StatusCreated).envelope()["attempt_id"].(string)
	if attemptID == "" {
		t.Fatal("start returned no attempt_id")
	}
	return attemptID
}

func TestAssessmentLifecycle(t *testing.T) {
	f := newLifecycleFixture(t)
	assignments := f.learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list()
	if len(assignments) != 1 || assignments[0]["attempt_status"] != "not_started" {
		t.Fatalf("learner should see one unstarted assessment, got %v", assignments)
	}

	attemptID := f.start(t)
	view := f.learner.call("GET", "/student/assessments/"+f.assignmentID, nil).expect(http.StatusOK)
	if strings.Contains(string(view.Body), "answer_key") || strings.Contains(string(view.Body), "common denominators") {
		t.Fatalf("learner view exposes the answer key: %s", view.Body)
	}

	// Autosave: first save, idempotent replay, stale revision.
	saveKey := "6f1c1a64-8a64-4a51-9c2d-2b8f6ad1b001"
	saved := f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "A", "revision": 1, "idempotency_key": saveKey}).expect(http.StatusOK).object()
	if saved["revision"] != float64(1) {
		t.Fatalf("expected revision 1, got %v", saved)
	}
	replay := f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "A", "revision": 1, "idempotency_key": saveKey}).expect(http.StatusOK).object()
	if replay["idempotent_replay"] != true {
		t.Fatalf("expected idempotent replay, got %v", replay)
	}
	stale := f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "B", "revision": 1, "idempotency_key": "6f1c1a64-8a64-4a51-9c2d-2b8f6ad1b002"}).expect(http.StatusConflict)
	if stale.errorCode() != "STALE_REVISION" {
		t.Fatalf("expected STALE_REVISION, got %s", stale.Body)
	}

	submitted := f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{f.numericID: "12.3", f.writtenID: "I used a common denominator."}}).expect(http.StatusOK).object()
	if submitted["auto_score"] != float64(2) || submitted["auto_max_score"] != float64(2) || submitted["manual_review_count"] != float64(1) || submitted["results_released"] != false {
		t.Fatalf("unexpected submission result: %v", submitted)
	}
	again := f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{}}).expect(http.StatusOK).object()
	if again["idempotent_replay"] != true {
		t.Fatalf("second submit should be an idempotent replay, got %v", again)
	}
	f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "B", "revision": 5, "idempotency_key": "6f1c1a64-8a64-4a51-9c2d-2b8f6ad1b003"}).expect(http.StatusConflict)

	// Teacher grades the written answer, which releases the result.
	classes := f.teacher.call("GET", "/teacher/classes", nil).expect(http.StatusOK).list()
	reviews := f.teacher.call("GET", "/teacher/reviews?class_id="+classes[0]["id"].(string), nil).expect(http.StatusOK).list()
	if len(reviews) != 1 || reviews[0]["question_id"] != f.writtenID {
		t.Fatalf("expected one written answer to review, got %v", reviews)
	}
	f.teacher.call("PUT", "/teacher/attempts/"+attemptID+"/answers/"+f.writtenID, map[string]any{"points": 2, "feedback": "Too many points"}).expect(http.StatusBadRequest)
	graded := f.teacher.call("PUT", "/teacher/attempts/"+attemptID+"/answers/"+f.writtenID, map[string]any{"points": 0.5, "feedback": "Good start."}).expect(http.StatusOK).object()
	if graded["results_released"] != true || graded["final_score"] != 2.5 || graded["final_max_score"] != float64(3) {
		t.Fatalf("unexpected grading result: %v", graded)
	}

	result := f.learner.call("GET", "/student/assessments/"+f.assignmentID, nil).expect(http.StatusOK).object()
	if result["results_released"] != true || result["final_score"] != 2.5 {
		t.Fatalf("learner should see the released final score, got %v", result)
	}

	// Two auto-graded answers were tagged to the skill; the written answer is not evidence.
	profile := f.learner.call("GET", "/student/learning-profile", nil).expect(http.StatusOK).object()
	skills, _ := profile["skills"].([]any)
	if len(skills) != 1 || skills[0].(map[string]any)["evidence_count"] != float64(2) {
		t.Fatalf("expected one skill with two pieces of evidence, got %v", profile["skills"])
	}
}

func TestSubmissionAfterDeadlineUsesOnlyServerSavedAnswers(t *testing.T) {
	f := newLifecycleFixture(t)
	attemptID := f.start(t)
	f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "B", "revision": 1, "idempotency_key": "0b6f8f7e-1a52-4f0e-9a51-6d9a1c2b3c01"}).expect(http.StatusOK)

	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_attempts SET deadline_at = now() - interval '1 minute' WHERE id = $1`, attemptID); err != nil {
		t.Fatal(err)
	}
	late := f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "A", "revision": 2, "idempotency_key": "0b6f8f7e-1a52-4f0e-9a51-6d9a1c2b3c02"}).expect(http.StatusConflict)
	if late.errorCode() != "ATTEMPT_DEADLINE_PASSED" {
		t.Fatalf("expected ATTEMPT_DEADLINE_PASSED, got %s", late.Body)
	}
	result := f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{f.choiceID: "A", f.numericID: "12"}}).expect(http.StatusOK).object()
	if result["deadline_submission"] != true || result["auto_score"] != float64(0) {
		t.Fatalf("late submission must ignore the payload and grade saved answers only, got %v", result)
	}
}

func TestLearnerCannotSeeQuestionsBeforeStarting(t *testing.T) {
	t.Skip("known issue P0-4 in docs/system-analysis.md: questions are returned before the attempt starts (Phase 2)")
	f := newLifecycleFixture(t)
	view := f.learner.call("GET", "/student/assessments/"+f.assignmentID, nil).expect(http.StatusOK).object()
	if questions, _ := view["questions"].([]any); len(questions) != 0 {
		t.Fatalf("questions are visible before the attempt starts: %v", questions)
	}
}
