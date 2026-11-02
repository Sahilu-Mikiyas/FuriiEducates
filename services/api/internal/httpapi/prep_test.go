package httpapi

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePrepCSV(t *testing.T) {
	header := "paper,grade,topic,question,option_a,option_b,option_c,option_d,correct,explanation,difficulty,source,license,exam_only\n"
	good := "mathematics,12,Fractions,What is 1/2 + 1/4?,3/4,2/6,1/8,1,A,Add with a common denominator,2,Teachers,School-owned,yes\n"
	questions, rowErrors, message := parsePrepCSV(strings.NewReader("ï»¿" + header + good))
	if message != "" || len(rowErrors) != 0 || len(questions) != 1 {
		t.Fatalf("good file: message=%q errors=%v questions=%d", message, rowErrors, len(questions))
	}
	if q := questions[0]; q.Paper != "mathematics" || q.Grade != 12 || q.Correct != "A" || !q.ExamOnly || q.Difficulty != 2 {
		t.Fatalf("parsed wrongly: %+v", q)
	}
	_, rowErrors, _ = parsePrepCSV(strings.NewReader(header + good + "cooking,12,T,Q,a,b,c,d,A,,,S,L,\nphysics,12,T,Q,a,a,c,d,A,,,S,L,\nphysics,12,T,Q,a,b,c,d,E,,,S,L,\n"))
	if len(rowErrors) != 3 {
		t.Fatalf("want three bad rows, got %v", rowErrors)
	}
	if rowErrors[0]["row"] != 3 {
		t.Fatalf("row numbers should count the header, got %v", rowErrors[0])
	}
	if _, _, message = parsePrepCSV(strings.NewReader("paper,grade\nphysics,12\n")); !strings.Contains(message, "missing") {
		t.Fatalf("missing columns should be named, got %q", message)
	}
}

func TestEstimateReadiness(t *testing.T) {
	strong := paperStat{Responses: 100, Weight: 100, Correct: 80}
	weak := paperStat{Responses: 100, Weight: 100, Correct: 20}
	mid := paperStat{Responses: 40, Weight: 40, Correct: 20}
	all := func(stat paperStat, papers []string) map[string]paperStat {
		stats := map[string]paperStat{}
		for _, paper := range papers {
			stats[paper] = stat
		}
		return stats
	}

	if got := estimateReadiness("", all(strong, commonPapers)); got.Status != "no_stream" || got.Total != nil {
		t.Fatalf("no stream must not give a total: %+v", got)
	}
	natural := streamPapers("natural")
	partial := all(strong, natural)
	delete(partial, "biology")
	if got := estimateReadiness("natural", partial); got.Status != "need_more_evidence" || got.Total != nil {
		t.Fatalf("a missing paper must withhold the total: %+v", got)
	}
	thin := all(strong, natural)
	thin["physics"] = paperStat{Responses: readinessMinResponses - 1, Weight: 14, Correct: 14}
	if got := estimateReadiness("natural", thin); got.Total != nil {
		t.Fatalf("too few answers on one paper must withhold the total: %+v", got.Total)
	}

	above := estimateReadiness("natural", all(strong, natural))
	if above.Total == nil || above.Total.Status != "above_line" || above.Total.Predicted < 450 || above.Total.Predicted > 500 {
		t.Fatalf("strong student: %+v", above.Total)
	}
	risk := estimateReadiness("social", all(weak, streamPapers("social")))
	if risk.Total == nil || risk.Total.Status != "at_risk" || risk.Total.Distance >= 0 {
		t.Fatalf("weak student: %+v", risk.Total)
	}
	near := estimateReadiness("natural", all(mid, natural))
	if near.Total == nil || near.Total.Status != "near_line" || math.Abs(near.Total.Predicted-300) > 1 {
		t.Fatalf("middle student: %+v", near.Total)
	}
	if near.Total.Low >= near.Total.Predicted || near.Total.High <= near.Total.Predicted || near.Total.Low < 0 || near.Total.High > 600 {
		t.Fatalf("range should surround the estimate: %+v", near.Total)
	}
	// More answers make the range narrower.
	sure := estimateReadiness("natural", all(paperStat{Responses: 150, Weight: 150, Correct: 75}, natural))
	if sure.Total.High-sure.Total.Low >= near.Total.High-near.Total.Low {
		t.Fatalf("more evidence should narrow the range: %+v vs %+v", sure.Total, near.Total)
	}
}

// addPrepQuestions imports count approved questions on a paper. A is correct.
func addPrepQuestions(t *testing.T, admin *apiClient, paper string, count int, examOnly bool) {
	t.Helper()
	var csv strings.Builder
	csv.WriteString("paper,grade,topic,question,option_a,option_b,option_c,option_d,correct,explanation,source,license,exam_only\n")
	for i := 0; i < count; i++ {
		fmt.Fprintf(&csv, "%s,12,Topic %d,%s question %d,right,wrong one,wrong two,wrong three,A,Because A is right,Teachers,School-owned,%t\n", paper, i%2, unique(paper), i, examOnly)
	}
	admin.call("POST", "/prep/questions/import", rawCSV(csv.String())).expect(http.StatusCreated)
	drafts := admin.call("GET", "/prep/questions?status=draft&paper="+paper, nil).expect(http.StatusOK).list()
	ids := []string{}
	for _, draft := range drafts {
		ids = append(ids, draft["id"].(string))
	}
	admin.call("POST", "/prep/questions/approve", map[string]any{"question_ids": ids}).expect(http.StatusOK)
}

// answerSession answers the first count items in a session view, correctly or not.
func answerSession(t *testing.T, learner *apiClient, session map[string]any, count int, correct bool) {
	t.Helper()
	sessionID := session["id"].(string)
	items := session["items"].([]any)
	for i := 0; i < count && i < len(items); i++ {
		answer := "A"
		if !correct {
			answer = "B"
		}
		questionID := items[i].(map[string]any)["question_id"].(string)
		learner.call("PUT", "/student/prep/sessions/"+sessionID+"/answers/"+questionID, map[string]any{"answer": answer, "flagged": false}).expect(http.StatusOK)
	}
}

func startSession(learner *apiClient, body map[string]any) map[string]any {
	id := learner.create("/student/prep/sessions", body)
	return learner.call("GET", "/student/prep/sessions/"+id, nil).expect(http.StatusOK).object()
}

func prepFixture(t *testing.T) (server *httptest.Server, admin, teacher, learner *apiClient, classID, studentID string) {
	t.Helper()
	server = newTestServer(t)
	_, admin = seedSchool(t, server)
	classID = createClass(admin)
	teacherID, teacherClient := inviteTeacher(server, admin)
	admin.call("POST", "/classes/"+classID+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	studentID, learner = enrollLearner(server, admin, classID)
	return server, admin, teacherClient, learner, classID, studentID
}

func TestPrepQuestionAuthoringAndApproval(t *testing.T) {
	server, admin, teacher, learner, _, _ := prepFixture(t)
	secondTeacherID, secondTeacher := inviteTeacher(server, admin)
	_ = secondTeacherID
	question := map[string]any{"paper": "physics", "grade": 11, "topic": "Motion", "prompt": "Unit of force?", "options": []string{"Newton", "Joule", "Watt", "Pascal"}, "correct": "A", "explanation": "Force is measured in newtons.", "source_name": "Written by teacher", "license_notes": "School-owned"}

	for _, bad := range []map[string]any{{"paper": "cooking"}, {"grade": 8}, {"options": []string{"a", "b", "c"}}, {"options": []string{"a", "a", "b", "c"}}, {"correct": "E"}, {"source_name": ""}} {
		body := map[string]any{}
		for k, v := range question {
			body[k] = v
		}
		for k, v := range bad {
			body[k] = v
		}
		teacher.call("POST", "/prep/questions", body).expect(http.StatusBadRequest)
	}
	questionID := teacher.create("/prep/questions", question)

	// A draft is invisible to students until an administrator approves it.
	learner.call("POST", "/student/prep/sessions", map[string]any{"mode": "drill", "paper": "physics"}).expect(http.StatusConflict)
	teacher.call("POST", "/prep/questions/approve", map[string]any{"question_ids": []string{questionID}}).expect(http.StatusForbidden)
	learner.call("GET", "/prep/questions", nil).expect(http.StatusForbidden)

	// Answers go to the author and administrators only.
	seen := func(client *apiClient) map[string]any {
		for _, item := range client.call("GET", "/prep/questions?status=draft", nil).expect(http.StatusOK).list() {
			if item["id"] == questionID {
				return item
			}
		}
		t.Fatal("question not listed")
		return nil
	}
	if seen(teacher)["correct"] != "A" || seen(admin)["correct"] != "A" {
		t.Fatal("the author and administrators should see the answer")
	}
	if item := seen(secondTeacher); item["correct"] != nil || item["explanation"] != nil {
		t.Fatalf("another teacher must not see the answer: %v", item)
	}

	admin.call("POST", "/prep/questions/approve", map[string]any{"question_ids": []string{questionID}}).expect(http.StatusOK)
	bank := admin.call("GET", "/prep/bank", nil).expect(http.StatusOK).list()
	found := false
	for _, row := range bank {
		if row["paper"] == "physics" {
			found = row["practice"] == float64(1) && row["drafts"] == float64(0)
		}
	}
	if !found || len(bank) != 9 {
		t.Fatalf("bank counts: %v", bank)
	}

	rejectID := teacher.create("/prep/questions", question)
	teacher.call("POST", "/prep/questions/"+rejectID+"/reject", map[string]any{}).expect(http.StatusForbidden)
	admin.call("POST", "/prep/questions/"+rejectID+"/reject", map[string]any{}).expect(http.StatusOK)
	admin.call("POST", "/prep/questions/"+rejectID+"/reject", map[string]any{}).expect(http.StatusNotFound)

	// Questions stay inside their school.
	_, other := seedSchool(t, server)
	if rows := other.call("GET", "/prep/questions", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("another school saw questions: %v", rows)
	}
	other.call("POST", "/prep/questions/approve", map[string]any{"question_ids": []string{rejectID}}).expect(http.StatusOK)
}

func TestPrepImportIsAllOrNothing(t *testing.T) {
	_, admin, teacher, _, _, _ := prepFixture(t)
	header := "paper,grade,topic,question,option_a,option_b,option_c,option_d,correct,source,license\n"
	good := "history,10,Axum,Where was Axum?,Tigray,Afar,Oromia,Sidama,A,Teachers,School-owned\n"
	response := teacher.call("POST", "/prep/questions/import", rawCSV(header+good+"history,10,Axum,,a,b,c,d,A,T,L\n"))
	response.expect(422)
	if code := response.errorCode(); code != "IMPORT_HAS_ERRORS" {
		t.Fatalf("code %s", code)
	}
	if rows := admin.call("GET", "/prep/questions?paper=history", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("a failed import must save nothing, got %d", len(rows))
	}
	teacher.call("POST", "/prep/questions/import", rawCSV(header+good)).expect(http.StatusCreated)
	if rows := admin.call("GET", "/prep/questions?paper=history", nil).expect(http.StatusOK).list(); len(rows) != 1 || rows[0]["status"] != "draft" || rows[0]["topic"] != "Axum" {
		t.Fatalf("imported questions should be drafts: %v", rows)
	}
}

func TestPrepDrillShowsAnswersOnlyAfterAnswering(t *testing.T) {
	server, admin, _, learner, classID, _ := prepFixture(t)
	addPrepQuestions(t, admin, "mathematics", 6, false)
	session := startSession(learner, map[string]any{"mode": "drill", "paper": "mathematics", "question_count": 4})
	raw := learner.call("GET", "/student/prep/sessions/"+session["id"].(string), nil).expect(http.StatusOK)
	if strings.Contains(string(raw.Body), "correct_option") || strings.Contains(string(raw.Body), "Because A is right") {
		t.Fatalf("an unanswered drill must not reveal answers: %s", raw.Body)
	}
	items := session["items"].([]any)
	if len(items) != 4 {
		t.Fatalf("want 4 items, got %d", len(items))
	}
	first := items[0].(map[string]any)["question_id"].(string)
	path := "/student/prep/sessions/" + session["id"].(string) + "/answers/" + first
	learner.call("PUT", path, map[string]any{"answer": "Z"}).expect(http.StatusBadRequest)
	result := learner.call("PUT", path, map[string]any{"answer": "B"}).expect(http.StatusOK).object()
	if result["is_correct"] != false || result["correct_option"] != "A" || result["explanation"] != "Because A is right" {
		t.Fatalf("feedback: %v", result)
	}
	// The first answer stands once the student has seen the explanation.
	again := learner.call("PUT", path, map[string]any{"answer": "A"}).expect(http.StatusOK).object()
	if again["is_correct"] != false || again["answer"] != "B" {
		t.Fatalf("a practice answer must not change: %v", again)
	}
	view := learner.call("GET", "/student/prep/sessions/"+session["id"].(string), nil).expect(http.StatusOK).object()
	revealed := 0
	for _, item := range view["items"].([]any) {
		if item.(map[string]any)["correct_option"] != nil {
			revealed++
		}
	}
	if revealed != 1 {
		t.Fatalf("only the answered question should be revealed, got %d", revealed)
	}

	// Another student cannot open or answer this practice.
	_, other := enrollLearner(server, admin, classID)
	other.call("GET", "/student/prep/sessions/"+session["id"].(string), nil).expect(http.StatusNotFound)
	other.call("PUT", path, map[string]any{"answer": "A"}).expect(http.StatusNotFound)
	other.call("POST", "/student/prep/sessions/"+session["id"].(string)+"/finish", map[string]any{}).expect(http.StatusNotFound)

	// Skipped questions do not count against the student.
	finished := learner.call("POST", "/student/prep/sessions/"+session["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK).object()
	if finished["correct_count"] != float64(0) || finished["score_out_of_100"] != float64(0) || finished["answered_count"] != float64(1) {
		t.Fatalf("result: %v", finished)
	}
	learner.call("PUT", path, map[string]any{"answer": "A"}).expect(http.StatusConflict)

	// Practice feeds skill mastery at a lower weight.
	var evidence int
	var weight float64
	if err := testDB.QueryRow(context.Background(), `SELECT count(*),COALESCE(sum(evidence_weight),0)::float8 FROM skill_evidence WHERE prep_session_id=$1`, session["id"]).Scan(&evidence, &weight); err != nil {
		t.Fatal(err)
	}
	if evidence != 1 || weight != prepPracticeWeight {
		t.Fatalf("evidence=%d weight=%v", evidence, weight)
	}
	if profile := learner.call("GET", "/student/learning-profile", nil).expect(http.StatusOK).object(); len(profile["skills"].([]any)) != 1 {
		t.Fatalf("mastery should update from practice: %v", profile)
	}
}

func TestPrepPracticeKeepsExamOnlyQuestionsOut(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "english", 3, false)
	addPrepQuestions(t, admin, "english", 5, true)
	session := startSession(learner, map[string]any{"mode": "timed", "paper": "english", "question_count": 20})
	if count := len(session["items"].([]any)); count != 3 {
		t.Fatalf("practice must use only the 3 practice questions, got %d", count)
	}
	learner.call("POST", "/student/prep/sessions/"+session["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK)

	admin.call("POST", "/prep/mock-papers", map[string]any{"paper": "english", "title": "Too big", "question_count": 9, "duration_minutes": 60}).expect(http.StatusConflict)
	paper := admin.call("POST", "/prep/mock-papers", map[string]any{"paper": "english", "title": "School mock", "question_count": 6, "duration_minutes": 60}).expect(http.StatusCreated).object()
	if paper["question_count"] != float64(6) || paper["exam_only_count"] != float64(5) {
		t.Fatalf("a mock paper should use exam-only questions first: %v", paper)
	}
}

func TestPrepTimedPaperHidesAnswersAndCountsBlanksWrong(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "biology", 4, false)
	session := startSession(learner, map[string]any{"mode": "timed", "paper": "biology", "question_count": 4, "duration_minutes": 30})
	sessionID := session["id"].(string)
	if seconds := session["seconds_left"].(float64); seconds < 1700 || seconds > 1800 {
		t.Fatalf("the server should report the time left, got %v", seconds)
	}
	items := session["items"].([]any)
	first := items[0].(map[string]any)["question_id"].(string)
	second := items[1].(map[string]any)["question_id"].(string)
	third := items[2].(map[string]any)["question_id"].(string)
	base := "/student/prep/sessions/" + sessionID + "/answers/"

	response := learner.call("PUT", base+first, map[string]any{"answer": "B"}).expect(http.StatusOK)
	if strings.Contains(string(response.Body), "correct_option") {
		t.Fatalf("a timed answer must not reveal the key: %s", response.Body)
	}
	learner.call("PUT", base+first, map[string]any{"answer": "A"}).expect(http.StatusOK) // Changing an answer is allowed.
	learner.call("PUT", base+second, map[string]any{"answer": "B", "flagged": true}).expect(http.StatusOK)
	learner.call("PUT", base+third, map[string]any{"answer": "A"}).expect(http.StatusOK)
	learner.call("PUT", base+third, map[string]any{"answer": nil}).expect(http.StatusOK) // Clearing one is too.

	raw := learner.call("GET", "/student/prep/sessions/"+sessionID, nil).expect(http.StatusOK)
	for _, secret := range []string{"correct_option", "is_correct", "Because A is right"} {
		if strings.Contains(string(raw.Body), secret) {
			t.Fatalf("an open timed paper leaked %q: %s", secret, raw.Body)
		}
	}
	if view := raw.object(); view["items"].([]any)[1].(map[string]any)["flagged"] != true {
		t.Fatal("the flag should be saved")
	}
	learner.call("POST", "/student/prep/sessions", map[string]any{"mode": "timed", "paper": "biology", "question_count": 1}).expect(http.StatusConflict)

	finished := learner.call("POST", "/student/prep/sessions/"+sessionID+"/finish", map[string]any{}).expect(http.StatusOK).object()
	if finished["correct_count"] != float64(1) || finished["score_out_of_100"] != float64(25) {
		t.Fatalf("one of four is 25 of 100: %v", finished)
	}
	for _, item := range finished["items"].([]any) {
		if item.(map[string]any)["correct_option"] != "A" {
			t.Fatalf("a finished paper should show every answer: %v", item)
		}
	}
	again := learner.call("POST", "/student/prep/sessions/"+sessionID+"/finish", map[string]any{}).expect(http.StatusOK).object()
	if again["correct_count"] != float64(1) {
		t.Fatalf("finishing twice must not change the result: %v", again)
	}
}

func TestPrepExpiredPapersAndAbandonedPracticeClose(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "chemistry", 2, false)
	ctx := context.Background()

	timed := startSession(learner, map[string]any{"mode": "timed", "paper": "chemistry", "question_count": 2})
	answerSession(t, learner, timed, 1, true)
	if _, err := testDB.Exec(ctx, `UPDATE prep_sessions SET deadline_at=now()-interval '10 seconds' WHERE id=$1`, timed["id"]); err != nil {
		t.Fatal(err)
	}
	// Answers in flight at the deadline get the grace window.
	items := timed["items"].([]any)
	learner.call("PUT", "/student/prep/sessions/"+timed["id"].(string)+"/answers/"+items[1].(map[string]any)["question_id"].(string), map[string]any{"answer": "A"}).expect(http.StatusOK)
	if _, err := testDB.Exec(ctx, `UPDATE prep_sessions SET deadline_at=now()-interval '1 minute' WHERE id=$1`, timed["id"]); err != nil {
		t.Fatal(err)
	}
	learner.call("PUT", "/student/prep/sessions/"+timed["id"].(string)+"/answers/"+items[1].(map[string]any)["question_id"].(string), map[string]any{"answer": "B"}).expect(http.StatusConflict)
	if _, err := finalizeExpiredPrepSessions(ctx, testDB); err != nil {
		t.Fatal(err)
	}
	view := learner.call("GET", "/student/prep/sessions/"+timed["id"].(string), nil).expect(http.StatusOK).object()
	if view["status"] != "finished" || view["correct_count"] != float64(2) {
		t.Fatalf("an expired paper should be graded from saved answers: %v", view)
	}

	drill := startSession(learner, map[string]any{"mode": "drill", "paper": "chemistry", "question_count": 2})
	answerSession(t, learner, drill, 1, true)
	if _, err := testDB.Exec(ctx, `UPDATE prep_sessions SET started_at=now()-interval '2 days' WHERE id=$1`, drill["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeExpiredPrepSessions(ctx, testDB); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := testDB.QueryRow(ctx, `SELECT status FROM prep_sessions WHERE id=$1`, drill["id"]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "finished" {
		t.Fatalf("abandoned practice should close, got %s", status)
	}
}

func TestPrepReviewBringsBackWrongAnswers(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "geography", 3, false)
	ctx := context.Background()
	learner.call("POST", "/student/prep/sessions", map[string]any{"mode": "review", "paper": "geography"}).expect(http.StatusConflict)

	drill := startSession(learner, map[string]any{"mode": "drill", "paper": "geography", "question_count": 3})
	items := drill["items"].([]any)
	wrong := items[0].(map[string]any)["question_id"].(string)
	base := "/student/prep/sessions/" + drill["id"].(string) + "/answers/"
	learner.call("PUT", base+wrong, map[string]any{"answer": "B"}).expect(http.StatusOK)
	learner.call("PUT", base+items[1].(map[string]any)["question_id"].(string), map[string]any{"answer": "A"}).expect(http.StatusOK)
	learner.call("POST", "/student/prep/sessions/"+drill["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK)

	// Not due until a day has passed.
	learner.call("POST", "/student/prep/sessions", map[string]any{"mode": "review", "paper": "geography"}).expect(http.StatusConflict)
	if _, err := testDB.Exec(ctx, `UPDATE prep_session_items SET answered_at=now()-interval '2 days' WHERE session_id=$1`, drill["id"]); err != nil {
		t.Fatal(err)
	}
	hub := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object()
	due := 0
	for _, paper := range hub["papers"].([]any) {
		if row := paper.(map[string]any); row["paper"] == "geography" {
			due = int(row["due_review"].(float64))
		}
	}
	if due != 1 {
		t.Fatalf("one wrong answer should be due, got %d", due)
	}
	review := startSession(learner, map[string]any{"mode": "review", "paper": "geography"})
	reviewItems := review["items"].([]any)
	if len(reviewItems) != 1 || reviewItems[0].(map[string]any)["question_id"] != wrong {
		t.Fatalf("review should bring back only the wrong answer: %v", reviewItems)
	}
	answerSession(t, learner, review, 1, true)
	learner.call("POST", "/student/prep/sessions/"+review["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK)
	// Answered correctly now, so it no longer comes back.
	learner.call("POST", "/student/prep/sessions", map[string]any{"mode": "review", "paper": "geography"}).expect(http.StatusConflict)
}

func TestPrepWeakModeTargetsLowestMastery(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "economics", 8, false)
	first := startSession(learner, map[string]any{"mode": "drill", "paper": "economics", "question_count": 8})
	base := "/student/prep/sessions/" + first["id"].(string) + "/answers/"
	for _, raw := range first["items"].([]any) {
		item := raw.(map[string]any)
		answer := "A"
		if item["topic"] == "Topic 1" {
			answer = "B"
		}
		learner.call("PUT", base+item["question_id"].(string), map[string]any{"answer": answer}).expect(http.StatusOK)
	}
	learner.call("POST", "/student/prep/sessions/"+first["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK)
	weak := startSession(learner, map[string]any{"mode": "weak", "paper": "economics", "question_count": 4})
	for _, raw := range weak["items"].([]any) {
		if item := raw.(map[string]any); item["topic"] != "Topic 1" {
			t.Fatalf("weak practice should choose the weak topic, got %v", item["topic"])
		}
	}
}

func TestPrepFullMockAndReadiness(t *testing.T) {
	server, admin, teacher, learner, classID, studentID := prepFixture(t)
	learner.call("POST", "/student/prep/mocks", map[string]any{}).expect(http.StatusConflict)
	teacher.call("PUT", "/prep/classes/"+classID+"/stream", map[string]any{"stream": "natural"}).expect(http.StatusForbidden)
	admin.call("PUT", "/prep/classes/"+classID+"/stream", map[string]any{"stream": "marine"}).expect(http.StatusBadRequest)
	admin.call("PUT", "/prep/classes/"+classID+"/stream", map[string]any{"stream": "natural"}).expect(http.StatusOK)

	papers := streamPapers("natural")
	for _, paper := range papers {
		addPrepQuestions(t, admin, paper, readinessMinResponses, false)
	}

	before := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object()
	if readiness := before["readiness"].(map[string]any); readiness["status"] != "need_more_evidence" || readiness["total"] != nil {
		t.Fatalf("no estimate before any work: %v", readiness)
	}
	if before["onboarded"] != false {
		t.Fatal("onboarding should start incomplete")
	}
	learner.call("POST", "/student/prep/onboarding", map[string]any{}).expect(http.StatusOK)

	mockID := learner.create("/student/prep/mocks", map[string]any{})
	if again := learner.create("/student/prep/mocks", map[string]any{}); again != mockID {
		t.Fatal("only one mock can be open")
	}
	learner.call("POST", "/student/prep/mocks/"+mockID+"/papers", map[string]any{"paper": "history"}).expect(http.StatusBadRequest)
	for index, paper := range papers {
		sessionID := learner.create("/student/prep/mocks/"+mockID+"/papers", map[string]any{"paper": paper})
		view := learner.call("GET", "/student/prep/sessions/"+sessionID, nil).expect(http.StatusOK).object()
		if index == 0 {
			if resumed := learner.call("POST", "/student/prep/mocks/"+mockID+"/papers", map[string]any{"paper": paper}).expect(http.StatusOK).object(); resumed["id"] != sessionID {
				t.Fatalf("an open paper should resume: %v", resumed)
			}
			learner.call("POST", "/student/prep/mocks/"+mockID+"/papers", map[string]any{"paper": papers[1]}).expect(http.StatusConflict)
		}
		// Five papers all right and one all wrong: 500 of 600.
		answerSession(t, learner, view, readinessMinResponses, index != 0)
		learner.call("POST", "/student/prep/sessions/"+sessionID+"/finish", map[string]any{}).expect(http.StatusOK)
		learner.call("POST", "/student/prep/mocks/"+mockID+"/papers", map[string]any{"paper": paper}).expect(http.StatusConflict)
	}

	hub := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object()
	if hub["onboarded"] != true || hub["stream"] != "natural" {
		t.Fatalf("hub: %v", hub)
	}
	mock := hub["mocks"].([]any)[0].(map[string]any)
	if mock["status"] != "finished" || mock["total"] != float64(500) {
		t.Fatalf("five full papers of six should total 500: %v", mock)
	}
	readiness := hub["readiness"].(map[string]any)
	total, ok := readiness["total"].(map[string]any)
	if !ok || readiness["status"] != "estimated" || total["status"] != "above_line" {
		t.Fatalf("readiness: %v", readiness)
	}
	if predicted := total["predicted"].(float64); predicted < 400 || predicted > 500 {
		t.Fatalf("predicted %v", predicted)
	}
	weak := readiness["weak_topics"].([]any)
	if len(weak) == 0 || weak[0].(map[string]any)["paper"] != papers[0] {
		t.Fatalf("the wrong paper's topics should come first: %v", weak)
	}

	class := teacher.call("GET", "/prep/classes/"+classID+"/readiness", nil).expect(http.StatusOK).object()
	rows := class["students"].([]any)
	row := rows[0].(map[string]any)
	if row["student_id"] != studentID || row["onboarded"] != true || row["total"] == nil || len(class["topics"].([]any)) == 0 {
		t.Fatalf("class view: %v", class)
	}
	otherTeacherID, otherTeacher := inviteTeacher(server, admin)
	_ = otherTeacherID
	otherTeacher.call("GET", "/prep/classes/"+classID+"/readiness", nil).expect(http.StatusForbidden)
	learner.call("GET", "/prep/classes/"+classID+"/readiness", nil).expect(http.StatusForbidden)
	teacher.call("GET", "/prep/readiness", nil).expect(http.StatusForbidden)

	school := admin.call("GET", "/prep/readiness", nil).expect(http.StatusOK).object()
	natural := school["streams"].([]any)[0].(map[string]any)
	if natural["key"] != "natural" || natural["students"] != float64(1) || natural["estimated"] != float64(1) || natural["above_line"] != float64(1) {
		t.Fatalf("stream summary: %v", natural)
	}
	if classes := school["classes"].([]any); len(classes) != 1 || classes[0].(map[string]any)["key"] != classID {
		t.Fatalf("class summary: %v", classes)
	}

	_, other := seedSchool(t, server)
	other.call("GET", "/prep/classes/"+classID+"/readiness", nil).expect(http.StatusForbidden)
	other.call("PUT", "/prep/students/"+studentID+"/stream", map[string]any{"stream": "social"}).expect(http.StatusNotFound)
	admin.call("PUT", "/prep/students/"+studentID+"/stream", map[string]any{"stream": "social"}).expect(http.StatusOK)
	if hub := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object(); hub["stream"] != "social" {
		t.Fatalf("a student's stream can be corrected: %v", hub["stream"])
	}
}

func TestPrepSchoolMockPaperCountsTowardReadiness(t *testing.T) {
	_, admin, _, learner, classID, _ := prepFixture(t)
	addPrepQuestions(t, admin, "mathematics", 5, true)
	paper := admin.call("POST", "/prep/mock-papers", map[string]any{"paper": "mathematics", "title": "Term 1 mock", "question_count": 5, "duration_minutes": 30}).expect(http.StatusCreated).object()
	assessmentID := paper["id"].(string)
	admin.call("POST", "/assessments/"+assessmentID+"/publish", map[string]any{}).expect(http.StatusOK)
	assignmentID := admin.create("/assessments/"+assessmentID+"/assignments", map[string]any{"class_id": classID})

	view := learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	if view["national_paper"] != "mathematics" || view["locked_mode"] != true {
		t.Fatalf("a mock paper should open in exam format: %v", view)
	}
	attempt := learner.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{}).expect(http.StatusCreated).object()
	answers := map[string]any{}
	opened := learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	for _, raw := range opened["questions"].([]any) {
		answers[raw.(map[string]any)["id"].(string)] = "A"
	}
	learner.call("POST", "/student/attempts/"+attempt["attempt_id"].(string)+"/submit", map[string]any{"answers": answers}).expect(http.StatusOK)

	hub := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object()
	found := false
	for _, raw := range hub["readiness"].(map[string]any)["papers"].([]any) {
		if row := raw.(map[string]any); row["paper"] == "mathematics" {
			found = row["responses"] == float64(5)
		}
	}
	if !found {
		t.Fatalf("a school mock should count toward readiness: %v", hub["readiness"])
	}
}

func uploadSitting(learner *apiClient, clientID, mode, paper string, questionIDs []string, answers map[string]string) apiResponse {
	list := []map[string]any{}
	for questionID, answer := range answers {
		list = append(list, map[string]any{"question_id": questionID, "answer": answer, "answered_at": time.Now().UTC().Format(time.RFC3339)})
	}
	return learner.call("POST", "/student/prep/offline-sittings", map[string]any{"client_id": clientID, "paper": paper, "mode": mode, "started_at": time.Now().UTC().Format(time.RFC3339), "question_ids": questionIDs, "answers": list})
}

func packQuestionIDs(t *testing.T, learner *apiClient, paper string, count int) ([]string, []any) {
	t.Helper()
	pack := learner.call("POST", "/student/prep/packs", map[string]any{"paper": paper, "question_count": count}).expect(http.StatusCreated).object()
	items := pack["items"].([]any)
	ids := []string{}
	for _, raw := range items {
		ids = append(ids, raw.(map[string]any)["question_id"].(string))
	}
	return ids, items
}

func TestPrepPackHoldsPracticeQuestionsOnly(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	learner.call("POST", "/student/prep/packs", map[string]any{"paper": "physics"}).expect(http.StatusConflict)
	addPrepQuestions(t, admin, "physics", 3, false)
	addPrepQuestions(t, admin, "physics", 4, true)
	admin.call("POST", "/student/prep/packs", map[string]any{"paper": "physics"}).expect(http.StatusForbidden)
	learner.call("POST", "/student/prep/packs", map[string]any{"paper": "cooking"}).expect(http.StatusBadRequest)
	learner.call("POST", "/student/prep/packs", map[string]any{"paper": "physics", "question_count": 101}).expect(http.StatusBadRequest)
	_, items := packQuestionIDs(t, learner, "physics", 20)
	if len(items) != 3 {
		t.Fatalf("a pack must leave out exam-only questions, got %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["correct_option"] != "A" || first["explanation"] != "Because A is right" || len(first["options"].([]any)) != 4 {
		t.Fatalf("a pack must carry what the device needs to mark itself: %v", first)
	}
}

func TestPrepOfflineSittingIsMarkedByTheServerAndCountedOnce(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "history", 4, false)
	ids, _ := packQuestionIDs(t, learner, "history", 4)
	clientID := "11111111-1111-4111-8111-111111111111"
	answers := map[string]string{ids[0]: "A", ids[1]: "B", ids[2]: "Z"}

	result := uploadSitting(learner, clientID, "drill", "history", ids, answers).expect(http.StatusCreated).object()
	if result["correct_count"] != float64(1) || result["answered_count"] != float64(2) || result["score_out_of_100"] != float64(50) {
		t.Fatalf("one right of two valid answers: %v", result)
	}
	// A retry after a lost reply returns the same result and records nothing new.
	again := uploadSitting(learner, clientID, "drill", "history", ids, map[string]string{ids[0]: "A", ids[1]: "A", ids[2]: "A", ids[3]: "A"}).expect(http.StatusOK).object()
	if again["id"] != result["id"] || again["correct_count"] != float64(1) {
		t.Fatalf("a repeated upload must not change the result: %v", again)
	}
	var sittings, evidence int
	var weight float64
	ctx := context.Background()
	if err := testDB.QueryRow(ctx, `SELECT count(*) FROM prep_sessions WHERE client_id=$1`, clientID).Scan(&sittings); err != nil {
		t.Fatal(err)
	}
	if err := testDB.QueryRow(ctx, `SELECT count(*),sum(evidence_weight)::float8 FROM skill_evidence WHERE prep_session_id=$1`, result["id"]).Scan(&evidence, &weight); err != nil {
		t.Fatal(err)
	}
	if sittings != 1 || evidence != 2 || weight != 2*prepPracticeWeight {
		t.Fatalf("sittings=%d evidence=%d weight=%v", sittings, evidence, weight)
	}

	// A wrong offline answer comes back for review like any other.
	if _, err := testDB.Exec(ctx, `UPDATE prep_session_items SET answered_at=now()-interval '2 days' WHERE session_id=$1`, result["id"]); err != nil {
		t.Fatal(err)
	}
	review := startSession(learner, map[string]any{"mode": "review", "paper": "history"})
	if items := review["items"].([]any); len(items) != 1 || items[0].(map[string]any)["question_id"] != ids[1] {
		t.Fatalf("review should bring back the wrong offline answer: %v", items)
	}
}

func TestPrepOfflineTimedCountsBlanksWrongAtPracticeWeight(t *testing.T) {
	_, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "biology", 4, false)
	ids, _ := packQuestionIDs(t, learner, "biology", 4)
	result := uploadSitting(learner, "22222222-2222-4222-8222-222222222222", "timed", "biology", ids, map[string]string{ids[0]: "A"}).expect(http.StatusCreated).object()
	if result["score_out_of_100"] != float64(25) {
		t.Fatalf("one of four is 25: %v", result)
	}
	var weight float64
	if err := testDB.QueryRow(context.Background(), `SELECT sum(evidence_weight)::float8 FROM skill_evidence WHERE prep_session_id=$1`, result["id"]).Scan(&weight); err != nil {
		t.Fatal(err)
	}
	if weight != 4*prepPracticeWeight {
		t.Fatalf("offline timed work should count at the practice weight, got %v", weight)
	}
	hub := learner.call("GET", "/student/prep", nil).expect(http.StatusOK).object()
	if active := hub["active_session"].(map[string]any); len(active) != 0 {
		t.Fatalf("an uploaded sitting must not stay open: %v", active)
	}
}

func TestPrepOfflineUploadRejectsQuestionsItCannotUse(t *testing.T) {
	server, admin, _, learner, _, _ := prepFixture(t)
	addPrepQuestions(t, admin, "geography", 2, false)
	addPrepQuestions(t, admin, "economics", 1, false)
	ids, _ := packQuestionIDs(t, learner, "geography", 2)
	wrongPaper, _ := packQuestionIDs(t, learner, "economics", 1)
	addPrepQuestions(t, admin, "geography", 1, true)
	var examOnly string
	if err := testDB.QueryRow(context.Background(), `SELECT id::text FROM questions WHERE exam_only AND status='published' ORDER BY created_at DESC LIMIT 1`).Scan(&examOnly); err != nil {
		t.Fatal(err)
	}
	_, otherAdmin := seedSchool(t, server)
	addPrepQuestions(t, otherAdmin, "geography", 1, false)
	var foreign string
	if err := testDB.QueryRow(context.Background(), `SELECT q.id::text FROM questions q WHERE q.school_id<>(SELECT school_id FROM questions WHERE id=$1) ORDER BY q.created_at DESC LIMIT 1`, ids[0]).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	for name, questionIDs := range map[string][]string{"another paper": {ids[0], wrongPaper[0]}, "exam-only": {ids[0], examOnly}, "another school": {ids[0], foreign}} {
		response := uploadSitting(learner, uuid(t), "drill", "geography", questionIDs, map[string]string{ids[0]: "A"})
		if response.Status != http.StatusBadRequest {
			t.Fatalf("%s: expected a refusal, got %d", name, response.Status)
		}
	}
	learner.call("POST", "/student/prep/offline-sittings", map[string]any{"client_id": "not-a-uuid", "paper": "geography", "mode": "drill", "question_ids": ids}).expect(http.StatusBadRequest)
	learner.call("POST", "/student/prep/offline-sittings", map[string]any{"client_id": uuid(t), "paper": "geography", "mode": "weak", "question_ids": ids}).expect(http.StatusBadRequest)
	var saved int
	if err := testDB.QueryRow(context.Background(), `SELECT count(*) FROM prep_sessions WHERE offline AND paper='geography'`).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != 0 {
		t.Fatalf("a refused upload must save nothing, got %d", saved)
	}
}

func TestClampOfflineTime(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	if got := clampOfflineTime(now.Add(time.Hour), now); !got.Equal(now) {
		t.Fatalf("a future time should become now, got %v", got)
	}
	if got := clampOfflineTime(time.Time{}, now); !got.Equal(now) {
		t.Fatalf("a missing time should become now, got %v", got)
	}
	if got := clampOfflineTime(now.Add(-24*time.Hour*365), now); !got.Equal(now.Add(-maxOfflineAge)) {
		t.Fatalf("an old time should be limited, got %v", got)
	}
	recent := now.Add(-time.Hour)
	if got := clampOfflineTime(recent, now); !got.Equal(recent) {
		t.Fatalf("a recent time should be kept, got %v", got)
	}
}

func uuid(t *testing.T) string {
	t.Helper()
	var value string
	if err := testDB.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
