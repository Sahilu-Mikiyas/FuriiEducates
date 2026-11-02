package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// examFixture is an enrolled learner, an assigned teacher, and a published
// three-question assessment that the test assigns however it needs.
type examFixture struct {
	server                         *httptest.Server
	admin, teacher, learner        *apiClient
	classID, assessmentID          string
	choiceID, numericID, writtenID string
}

func newExamFixture(t *testing.T, settings map[string]any) examFixture {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	teacherID, teacher := inviteTeacher(server, admin)
	admin.call("POST", "/classes/"+classID+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	_, learner := enrollLearner(server, admin, classID)
	skillID := createSkill(admin)
	choiceID := publishQuestion(admin, skillID, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": "SECRET-PROMPT 1/2 + 1/4?"}, "options": []map[string]string{{"id": "A", "text": "3/4"}, {"id": "B", "text": "2/6"}}, "answer_key": []string{"A"}})
	numericID := publishQuestion(admin, skillID, map[string]any{"question_type": "numeric", "prompt": map[string]string{"text": "3 x 4?"}, "answer_key": map[string]float64{"value": 12, "tolerance": 0.5}})
	writtenID := publishQuestion(admin, skillID, map[string]any{"question_type": "text", "prompt": map[string]string{"text": "Explain your method."}, "answer_key": map[string]string{"guide": "Mentions common denominators."}})
	body := map[string]any{"title": unique("Exam"), "instructions": "Answer every question.", "duration_minutes": 30, "question_ids": []string{choiceID, numericID, writtenID}}
	for key, value := range settings {
		body[key] = value
	}
	assessmentID := admin.create("/assessments", body)
	admin.call("POST", "/assessments/"+assessmentID+"/publish", map[string]any{}).expect(http.StatusOK)
	return examFixture{server: server, admin: admin, teacher: teacher, learner: learner, classID: classID, assessmentID: assessmentID, choiceID: choiceID, numericID: numericID, writtenID: writtenID}
}

func (f examFixture) assign(fields map[string]any) string {
	f.admin.t.Helper()
	body := map[string]any{"class_id": f.classID}
	for key, value := range fields {
		body[key] = value
	}
	return f.admin.create("/assessments/"+f.assessmentID+"/assignments", body)
}

func startAttempt(learner *apiClient, assignmentID string, body map[string]any) string {
	learner.t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	attemptID, _ := learner.call("POST", "/student/assessments/"+assignmentID+"/start", body).expect(http.StatusCreated).object()["attempt_id"].(string)
	if attemptID == "" {
		learner.t.Fatal("start returned no attempt_id")
	}
	return attemptID
}

func saveAnswer(learner *apiClient, attemptID, questionID string, answer any, revision int) apiResponse {
	learner.t.Helper()
	key := fmt.Sprintf("0b6f8f7e-1a52-4f0e-9a51-%012d", uniqueCounter.Add(1))
	return learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+questionID, map[string]any{"answer": answer, "revision": revision, "idempotency_key": key})
}

func moveDeadline(t *testing.T, attemptID, interval string) {
	t.Helper()
	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_attempts SET deadline_at = now() + $2::interval WHERE id = $1`, attemptID, interval); err != nil {
		t.Fatal(err)
	}
}

func TestQuestionsAreHiddenUntilTheAttemptStarts(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(nil)
	before := f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK)
	if strings.Contains(string(before.Body), "SECRET-PROMPT") {
		t.Fatalf("questions are visible before the attempt starts: %s", before.Body)
	}
	summary := before.object()
	if summary["question_count"] != float64(3) || summary["duration_minutes"] != float64(30) {
		t.Fatalf("the start screen needs the question count and duration, got %v", summary)
	}
	startAttempt(f.learner, assignmentID, nil)
	after := f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK)
	if !strings.Contains(string(after.Body), "SECRET-PROMPT") {
		t.Fatalf("questions should be visible after starting: %s", after.Body)
	}
	if strings.Contains(string(after.Body), "answer_key") || strings.Contains(string(after.Body), "common denominators") {
		t.Fatalf("the learner view exposes the answer key: %s", after.Body)
	}
}

func TestServerFinalizesAbandonedAttempts(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(nil)
	attemptID := startAttempt(f.learner, assignmentID, nil)
	saveAnswer(f.learner, attemptID, f.choiceID, "A", 1).expect(http.StatusOK)
	moveDeadline(t, attemptID, "-5 minutes")

	if _, err := finalizeExpiredAttempts(context.Background(), testDB); err != nil {
		t.Fatal(err)
	}
	result := f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	if result["status"] != "submitted" || result["score"] != float64(1) {
		t.Fatalf("an abandoned attempt should be graded from saved answers, got %v", result)
	}
	var eventType string
	if err := testDB.QueryRow(context.Background(), `SELECT event_type FROM assessment_attempt_events WHERE attempt_id=$1 ORDER BY occurred_at DESC LIMIT 1`, attemptID).Scan(&eventType); err != nil {
		t.Fatal(err)
	}
	if eventType != "deadline_submission" {
		t.Fatalf("expected a deadline_submission event, got %s", eventType)
	}
}

func TestOpeningAnExpiredAttemptFinalizesIt(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(nil)
	attemptID := startAttempt(f.learner, assignmentID, nil)
	saveAnswer(f.learner, attemptID, f.numericID, "12", 1).expect(http.StatusOK)
	moveDeadline(t, attemptID, "-5 minutes")
	result := f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	if result["status"] != "submitted" || result["score"] != float64(1) {
		t.Fatalf("expected the expired attempt to be finalized on open, got %v", result)
	}
}

func TestGraceWindowKeepsAnswersInFlightAtTheDeadline(t *testing.T) {
	f := newExamFixture(t, nil)
	attemptID := startAttempt(f.learner, f.assign(nil), nil)
	moveDeadline(t, attemptID, "-5 seconds")
	saveAnswer(f.learner, attemptID, f.choiceID, "A", 1).expect(http.StatusOK)
	result := f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{f.numericID: "12"}}).expect(http.StatusOK).object()
	if result["auto_score"] != float64(2) || result["deadline_submission"] != false {
		t.Fatalf("answers inside the grace window should count, got %v", result)
	}
}

func TestAssignmentWindow(t *testing.T) {
	f := newExamFixture(t, nil)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	soon := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)

	f.admin.call("POST", "/assessments/"+f.assessmentID+"/assignments", map[string]any{"class_id": f.classID, "opens_at": future, "due_at": past}).expect(http.StatusBadRequest)
	f.admin.call("POST", "/assessments/"+f.assessmentID+"/assignments", map[string]any{"class_id": f.classID, "due_at": "tomorrow"}).expect(http.StatusBadRequest)

	assignmentID := f.assign(map[string]any{"opens_at": future})
	start := "/student/assessments/" + assignmentID + "/start"
	if code := f.learner.call("POST", start, map[string]any{}).expect(http.StatusConflict).errorCode(); code != "ASSESSMENT_NOT_OPEN" {
		t.Fatalf("expected ASSESSMENT_NOT_OPEN, got %s", code)
	}
	f.admin.call("PATCH", "/assignments/"+assignmentID, map[string]any{"due_at": past}).expect(http.StatusOK)
	if code := f.learner.call("POST", start, map[string]any{}).expect(http.StatusConflict).errorCode(); code != "ASSESSMENT_CLOSED" {
		t.Fatalf("expected ASSESSMENT_CLOSED, got %s", code)
	}
	f.admin.call("PATCH", "/assignments/"+assignmentID, map[string]any{"due_at": soon}).expect(http.StatusOK)
	deadline, _ := f.learner.call("POST", start, map[string]any{}).expect(http.StatusCreated).object()["deadline_at"].(string)
	parsed, err := time.Parse(time.RFC3339, deadline)
	if err != nil {
		t.Fatalf("deadline is not RFC 3339: %q", deadline)
	}
	if parsed.After(time.Now().Add(11 * time.Minute)) {
		t.Fatalf("the deadline must not pass the due time, got %s", deadline)
	}
}

func TestStartCodeSession(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(map[string]any{"requires_start_code": true})
	start := "/student/assessments/" + assignmentID + "/start"
	session := "/exam-sessions/" + assignmentID

	if code := f.learner.call("POST", start, map[string]any{}).expect(http.StatusConflict).errorCode(); code != "SESSION_NOT_OPEN" {
		t.Fatalf("expected SESSION_NOT_OPEN, got %s", code)
	}
	startCode, _ := f.teacher.call("POST", session+"/open", map[string]any{}).expect(http.StatusOK).object()["start_code"].(string)
	if len(startCode) != 6 {
		t.Fatalf("expected a six-character code, got %q", startCode)
	}
	if strings.Contains(string(f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).Body), startCode) {
		t.Fatal("the learner view must not reveal the start code")
	}
	if code := f.learner.call("POST", start, map[string]any{"start_code": "WRONG1"}).expect(http.StatusForbidden).errorCode(); code != "START_CODE_INVALID" {
		t.Fatalf("expected START_CODE_INVALID, got %s", code)
	}
	attemptID := startAttempt(f.learner, assignmentID, map[string]any{"start_code": strings.ToLower(startCode)})

	// Closing the session stops new starts but never locks out a student who
	// is already in, for example one reconnecting after a power cut.
	f.teacher.call("POST", session+"/close", map[string]any{}).expect(http.StatusOK)
	if again := startAttempt(f.learner, assignmentID, nil); again != attemptID {
		t.Fatalf("a reconnecting student should get the same attempt, got %s", again)
	}
	reopened, _ := f.teacher.call("POST", session+"/open", map[string]any{}).expect(http.StatusOK).object()["start_code"].(string)
	if reopened == "" {
		t.Fatal("reopening should issue a code")
	}
}

func TestStartCodeGuessesAreLimited(t *testing.T) {
	limits := testRateLimits()
	limits.startCodeFailuresPerUser = limitRule{limit: 2, window: time.Minute}
	server := newTestServerWithLimits(t, limits)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	_, learner := enrollLearner(server, admin, classID)
	skillID := createSkill(admin)
	questionID := publishQuestion(admin, skillID, map[string]any{"question_type": "numeric", "prompt": map[string]string{"text": "1+1?"}, "answer_key": map[string]float64{"value": 2}})
	assessmentID := admin.create("/assessments", map[string]any{"title": unique("Exam"), "duration_minutes": 30, "question_ids": []string{questionID}})
	admin.call("POST", "/assessments/"+assessmentID+"/publish", map[string]any{}).expect(http.StatusOK)
	assignmentID := admin.create("/assessments/"+assessmentID+"/assignments", map[string]any{"class_id": classID, "requires_start_code": true})
	admin.call("POST", "/exam-sessions/"+assignmentID+"/open", map[string]any{}).expect(http.StatusOK)
	start := "/student/assessments/" + assignmentID + "/start"
	learner.call("POST", start, map[string]any{"start_code": "WRONG1"}).expect(http.StatusForbidden)
	learner.call("POST", start, map[string]any{"start_code": "WRONG2"}).expect(http.StatusForbidden)
	learner.call("POST", start, map[string]any{"start_code": "WRONG3"}).expect(http.StatusTooManyRequests)
}

func TestExamSessionAccessIsScoped(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(map[string]any{"requires_start_code": true})
	session := "/exam-sessions/" + assignmentID
	_, otherTeacher := inviteTeacher(f.server, f.admin)
	_, otherSchool := seedSchool(t, f.server)

	otherTeacher.call("GET", session, nil).expect(http.StatusForbidden)
	otherTeacher.call("POST", session+"/open", map[string]any{}).expect(http.StatusForbidden)
	otherSchool.call("GET", session, nil).expect(http.StatusNotFound)
	otherSchool.call("POST", session+"/open", map[string]any{}).expect(http.StatusNotFound)
	f.learner.call("GET", session, nil).expect(http.StatusForbidden)

	if containsSession(otherTeacher.call("GET", "/exam-sessions", nil).expect(http.StatusOK).list(), assignmentID) {
		t.Error("a teacher must only list sessions for assigned classes")
	}
	if !containsSession(f.teacher.call("GET", "/exam-sessions", nil).expect(http.StatusOK).list(), assignmentID) {
		t.Error("the assigned teacher should see the session")
	}
	if !containsSession(f.admin.call("GET", "/exam-sessions", nil).expect(http.StatusOK).list(), assignmentID) {
		t.Error("the administrator should see the session")
	}
}

func containsSession(items []map[string]any, assignmentID string) bool {
	for _, item := range items {
		if item["assignment_id"] == assignmentID {
			return true
		}
	}
	return false
}

func TestInvigilatorConsoleAndActions(t *testing.T) {
	f := newExamFixture(t, map[string]any{"locked_mode": true})
	assignmentID := f.assign(nil)
	session := "/exam-sessions/" + assignmentID

	console := f.teacher.call("GET", session, nil).expect(http.StatusOK).object()
	roster := console["roster"].([]any)
	if len(roster) != 1 || roster[0].(map[string]any)["status"] != "not_started" || console["locked_mode"] != true {
		t.Fatalf("unexpected console before start: %v", console)
	}
	if f.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()["locked_mode"] != true {
		t.Fatal("the learner page needs to know it should lock")
	}

	attemptID := startAttempt(f.learner, assignmentID, nil)
	saveAnswer(f.learner, attemptID, f.choiceID, "A", 1).expect(http.StatusOK)
	flagged := f.learner.call("POST", "/student/attempts/"+attemptID+"/integrity-events", map[string]string{"type": "tab_hidden"}).expect(http.StatusOK).object()
	if flagged["recorded"] != true {
		t.Fatalf("integrity event was not recorded: %v", flagged)
	}
	f.learner.call("POST", "/student/attempts/"+attemptID+"/integrity-events", map[string]string{"type": "not_a_signal"}).expect(http.StatusBadRequest)

	attemptPath := session + "/attempts/" + attemptID
	f.teacher.call("POST", attemptPath+"/incident", map[string]string{"note": ""}).expect(http.StatusBadRequest)
	f.teacher.call("POST", attemptPath+"/incident", map[string]string{"note": "Phone seen on desk."}).expect(http.StatusCreated)

	before := f.teacher.call("GET", session, nil).expect(http.StatusOK).object()["roster"].([]any)[0].(map[string]any)
	if before["status"] != "in_progress" || before["answered"] != float64(1) || before["integrity_flags"] != float64(1) || before["incidents"] != float64(1) {
		t.Fatalf("unexpected live status: %v", before)
	}

	f.teacher.call("POST", attemptPath+"/extend", map[string]any{"minutes": 0}).expect(http.StatusBadRequest)
	extended, _ := f.teacher.call("POST", attemptPath+"/extend", map[string]any{"minutes": 15, "reason": "Power cut"}).expect(http.StatusOK).object()["deadline_at"].(string)
	parsed, err := time.Parse(time.RFC3339, extended)
	if err != nil || parsed.Before(time.Now().Add(40*time.Minute)) {
		t.Fatalf("expected the deadline to move out by 15 minutes, got %q", extended)
	}

	timeline := f.teacher.call("GET", attemptPath+"/events", nil).expect(http.StatusOK).list()
	seen := map[string]bool{}
	for _, event := range timeline {
		seen[event["event_type"].(string)] = true
	}
	if seen["answer_saved"] || !seen["started"] || !seen["integrity_flag"] || !seen["incident"] || !seen["time_extended"] {
		t.Fatalf("unexpected timeline: %v", timeline)
	}

	finished := f.teacher.call("POST", attemptPath+"/finish", map[string]any{}).expect(http.StatusOK).object()
	if finished["auto_score"] != float64(1) {
		t.Fatalf("finishing should grade saved answers, got %v", finished)
	}
	f.teacher.call("POST", attemptPath+"/finish", map[string]any{}).expect(http.StatusConflict)
	f.teacher.call("POST", attemptPath+"/extend", map[string]any{"minutes": 5}).expect(http.StatusConflict)
	if recorded := f.learner.call("POST", "/student/attempts/"+attemptID+"/integrity-events", map[string]string{"type": "window_blur"}).expect(http.StatusOK).object()["recorded"]; recorded != false {
		t.Fatalf("a finished attempt should not collect signals, got %v", recorded)
	}
}

func TestLearnersCannotReportOnOtherAttempts(t *testing.T) {
	f := newExamFixture(t, nil)
	attemptID := startAttempt(f.learner, f.assign(nil), nil)
	_, other := enrollLearner(f.server, f.admin, f.classID)
	if recorded := other.call("POST", "/student/attempts/"+attemptID+"/integrity-events", map[string]string{"type": "tab_hidden"}).expect(http.StatusOK).object()["recorded"]; recorded != false {
		t.Fatal("a learner must not add events to another learner's attempt")
	}
}

func TestShuffledAttemptsDifferButGradeCorrectly(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	classID := createClass(admin)
	_, first := enrollLearner(server, admin, classID)
	_, second := enrollLearner(server, admin, classID)
	skillID := createSkill(admin)
	questionIDs := []string{}
	for i := 0; i < 8; i++ {
		questionIDs = append(questionIDs, publishQuestion(admin, skillID, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": fmt.Sprintf("Question %d", i)}, "options": []map[string]string{{"id": "A", "text": "Right"}, {"id": "B", "text": "Wrong"}, {"id": "C", "text": "Wrong"}, {"id": "D", "text": "Wrong"}}, "answer_key": []string{"A"}}))
	}
	assessmentID := admin.create("/assessments", map[string]any{"title": unique("Shuffled"), "duration_minutes": 30, "question_ids": questionIDs, "shuffle_questions": true, "shuffle_options": true})
	admin.call("POST", "/assessments/"+assessmentID+"/publish", map[string]any{}).expect(http.StatusOK)
	assignmentID := admin.create("/assessments/"+assessmentID+"/assignments", map[string]any{"class_id": classID})

	order := func(learner *apiClient) []string {
		startAttempt(learner, assignmentID, nil)
		ids := []string{}
		for _, raw := range learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()["questions"].([]any) {
			question := raw.(map[string]any)
			ids = append(ids, question["id"].(string))
			if options := question["options"].([]any); len(options) != 4 {
				t.Fatalf("shuffling must keep every option, got %v", options)
			}
		}
		return ids
	}
	firstOrder, secondOrder := order(first), order(second)
	if len(firstOrder) != 8 || strings.Join(firstOrder, ",") == strings.Join(secondOrder, ",") {
		t.Fatalf("two attempts should have different question orders: %v %v", firstOrder, secondOrder)
	}
	// A student who stays on the page sees a stable order.
	if strings.Join(firstOrder, ",") != strings.Join(order(first), ",") {
		t.Fatal("an attempt's order must not change on reload")
	}
	attemptID, _ := first.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{}).expect(http.StatusCreated).object()["attempt_id"].(string)
	answers := map[string]any{}
	for _, id := range questionIDs {
		answers[id] = "A"
	}
	result := first.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": answers}).expect(http.StatusOK).object()
	if result["auto_score"] != float64(8) {
		t.Fatalf("shuffled options must still grade by option id, got %v", result)
	}
}

func TestAdministratorCanGradeWhenClassHasNoTeacher(t *testing.T) {
	server := newTestServer(t)
	_, admin := seedSchool(t, server)
	_, otherSchool := seedSchool(t, server)
	classID := createClass(admin)
	_, learner := enrollLearner(server, admin, classID)
	skillID := createSkill(admin)
	writtenID := publishQuestion(admin, skillID, map[string]any{"question_type": "text", "prompt": map[string]string{"text": "Explain."}, "answer_key": map[string]string{"guide": "x"}})
	_, assignmentID := publishAssessment(admin, classID, []string{writtenID})
	attemptID := startAttempt(learner, assignmentID, nil)
	learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{writtenID: "My answer"}}).expect(http.StatusOK)

	otherSchool.call("GET", "/reviews?class_id="+classID, nil).expect(http.StatusForbidden)
	otherSchool.call("PUT", "/attempts/"+attemptID+"/answers/"+writtenID+"/grade", map[string]any{"points": 1}).expect(http.StatusNotFound)
	learner.call("GET", "/reviews?class_id="+classID, nil).expect(http.StatusForbidden)

	reviews := admin.call("GET", "/reviews?class_id="+classID, nil).expect(http.StatusOK).list()
	if len(reviews) != 1 {
		t.Fatalf("expected one written answer in the queue, got %v", reviews)
	}
	graded := admin.call("PUT", "/attempts/"+attemptID+"/answers/"+writtenID+"/grade", map[string]any{"points": 1, "feedback": "Good."}).expect(http.StatusOK).object()
	if graded["results_released"] != true || graded["final_score"] != float64(1) {
		t.Fatalf("grading the last answer should release the result, got %v", graded)
	}
}

func TestTeacherCannotGradeUnassignedClass(t *testing.T) {
	f := newExamFixture(t, nil)
	attemptID := startAttempt(f.learner, f.assign(nil), nil)
	f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{f.writtenID: "Answer"}}).expect(http.StatusOK)
	_, other := inviteTeacher(f.server, f.admin)
	other.call("GET", "/teacher/reviews?class_id="+f.classID, nil).expect(http.StatusForbidden)
	other.call("PUT", "/teacher/attempts/"+attemptID+"/answers/"+f.writtenID, map[string]any{"points": 1}).expect(http.StatusNotFound)
}

func TestGradebook(t *testing.T) {
	f := newExamFixture(t, nil)
	assignmentID := f.assign(nil)
	notStartedID := f.admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Zed Not Started", "class_id": f.classID})
	attemptID := startAttempt(f.learner, assignmentID, nil)
	f.learner.call("POST", "/student/attempts/"+attemptID+"/submit", map[string]any{"answers": map[string]any{f.choiceID: "A", f.writtenID: "Answer"}}).expect(http.StatusOK)

	_, otherTeacher := inviteTeacher(f.server, f.admin)
	_, otherSchool := seedSchool(t, f.server)
	otherTeacher.call("GET", "/gradebook?class_id="+f.classID, nil).expect(http.StatusForbidden)
	otherSchool.call("GET", "/gradebook?class_id="+f.classID, nil).expect(http.StatusForbidden)
	f.learner.call("GET", "/gradebook?class_id="+f.classID, nil).expect(http.StatusForbidden)
	f.admin.call("GET", "/gradebook?class_id=nope", nil).expect(http.StatusBadRequest)

	for _, viewer := range []*apiClient{f.admin, f.teacher} {
		book := viewer.call("GET", "/gradebook?class_id="+f.classID, nil).expect(http.StatusOK).object()
		columns := book["assignments"].([]any)
		if len(columns) != 1 || columns[0].(map[string]any)["max_points"] != float64(3) {
			t.Fatalf("unexpected columns: %v", columns)
		}
		students := book["students"].([]any)
		if len(students) != 2 {
			t.Fatalf("expected two students, got %v", students)
		}
		for _, raw := range students {
			row := raw.(map[string]any)
			results := row["results"].(map[string]any)
			if row["student_id"] == notStartedID {
				if len(results) != 0 {
					t.Fatalf("a student who never started should have no result: %v", row)
				}
				continue
			}
			cell := results[assignmentID].(map[string]any)
			if cell["status"] != "submitted" || cell["score"] != float64(1) || cell["pending_review"] != float64(1) || cell["results_released"] != false {
				t.Fatalf("unexpected gradebook cell: %v", cell)
			}
		}
	}
	f.teacher.call("PUT", "/teacher/attempts/"+attemptID+"/answers/"+f.writtenID, map[string]any{"points": 1, "feedback": "Fine."}).expect(http.StatusOK)
	book := f.admin.call("GET", "/gradebook?class_id="+f.classID, nil).expect(http.StatusOK).object()
	for _, raw := range book["students"].([]any) {
		row := raw.(map[string]any)
		if row["student_id"] == notStartedID {
			continue
		}
		cell := row["results"].(map[string]any)[assignmentID].(map[string]any)
		if cell["final_score"] != float64(2) || cell["final_max_score"] != float64(3) || cell["results_released"] != true {
			t.Fatalf("unexpected released cell: %v", cell)
		}
	}
}

func TestInvalidIdempotencyKeyIsRejected(t *testing.T) {
	f := newExamFixture(t, nil)
	attemptID := startAttempt(f.learner, f.assign(nil), nil)
	f.learner.call("PUT", "/student/attempts/"+attemptID+"/answers/"+f.choiceID, map[string]any{"answer": "A", "revision": 1, "idempotency_key": "not-a-uuid"}).expect(http.StatusBadRequest)
}

func TestNewStartCodeAlphabet(t *testing.T) {
	for i := 0; i < 50; i++ {
		code, err := newStartCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 6 || strings.ContainsAny(code, "01OIL") {
			t.Fatalf("hard-to-read start code: %q", code)
		}
	}
}
