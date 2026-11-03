package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func choiceQuestion(prompt, topic string) map[string]any {
	return map[string]any{"question_type": "single_choice", "prompt": prompt, "options": []string{"right", "wrong"}, "correct": 0, "topic": topic, "points": 1, "explanation": "Because it is right."}
}

func quizBody(subject string, questions ...map[string]any) map[string]any {
	return map[string]any{"class_subject_id": subject, "title": unique("Quiz"), "instructions": "Do your best.", "kind": "quiz", "duration_minutes": 20, "show_answers": true, "questions": questions}
}

func future(hours int) string {
	return time.Now().UTC().Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)
}

func TestQuizQuestionChecks(t *testing.T) {
	value := 12.0
	good := []quizQuestion{
		{Type: "single_choice", Prompt: " What? ", Options: []string{" a ", "b"}, Correct: 1, Topic: "T", Points: 1},
		{Type: "numeric", Prompt: "How many?", Value: &value, Tolerance: 0.5, Topic: "T", Points: 2},
		{Type: "text", Prompt: "Explain.", Guide: "Mentions gravity", Topic: "T", Points: 5},
	}
	for i := range good {
		if message := checkQuizQuestion(&good[i]); message != "" {
			t.Errorf("%d: %s", i, message)
		}
	}
	if good[0].Prompt != "What?" || good[0].Options[0] != "a" {
		t.Fatalf("not tidied: %+v", good[0])
	}
	bad := map[string]quizQuestion{
		"no prompt":   {Type: "text", Topic: "T", Points: 1},
		"no topic":    {Type: "text", Prompt: "P", Points: 1},
		"no points":   {Type: "text", Prompt: "P", Topic: "T"},
		"too many":    {Type: "text", Prompt: "P", Topic: "T", Points: 101},
		"bad type":    {Type: "essay", Prompt: "P", Topic: "T", Points: 1},
		"one option":  {Type: "single_choice", Prompt: "P", Options: []string{"a"}, Topic: "T", Points: 1},
		"blank":       {Type: "single_choice", Prompt: "P", Options: []string{"a", " "}, Topic: "T", Points: 1},
		"same twice":  {Type: "single_choice", Prompt: "P", Options: []string{"a", "A"}, Topic: "T", Points: 1},
		"bad correct": {Type: "single_choice", Prompt: "P", Options: []string{"a", "b"}, Correct: 2, Topic: "T", Points: 1},
		"no number":   {Type: "numeric", Prompt: "P", Topic: "T", Points: 1},
		"negative":    {Type: "numeric", Prompt: "P", Value: &value, Tolerance: -1, Topic: "T", Points: 1},
	}
	for name, q := range bad {
		if checkQuizQuestion(&q) == "" {
			t.Errorf("%s should be refused", name)
		}
	}
	in := quizInput{SubjectID: "00000000-0000-0000-0000-000000000000", Title: "Quiz", Kind: "quiz", Minutes: 20, Questions: []quizQuestion{{Type: "text", Prompt: "P", Topic: "T", Points: 1}, {Type: "text", Topic: "T", Points: 1}}}
	if message := in.check(); !strings.HasPrefix(message, "Question 2:") {
		t.Fatalf("the question should be named: %q", message)
	}
	assignment := quizInput{SubjectID: in.SubjectID, Title: "Work", Kind: "assignment"}
	if message := assignment.check(); message != "" || assignment.Minutes != assignmentMaxMinutes {
		t.Fatalf("assignment: %q %d", message, assignment.Minutes)
	}
	if opens, due, message := quizWindow("assignment", nil, nil); message == "" || opens != nil || due != nil {
		t.Fatal("an assignment needs a due date")
	}
	far := future(24 * 40)
	if _, _, message := quizWindow("assignment", nil, &far); message == "" {
		t.Fatal("an assignment cannot run past 30 days")
	}
	past := future(-1)
	if _, _, message := quizWindow("quiz", nil, &past); message == "" {
		t.Fatal("a due time cannot be past")
	}
}

func TestTeacherWritesAndPublishesQuiz(t *testing.T) {
	s := setupReports(t)
	teacher, other, learner := s.teacher, s.other, s.learner

	// Who can write.
	learner.call("POST", "/quizzes", quizBody(s.math, choiceQuestion("Q", "Motion"))).expect(http.StatusForbidden)
	other.call("POST", "/quizzes", quizBody(s.math, choiceQuestion("Q", "Motion"))).expect(http.StatusForbidden)
	bad := teacher.call("POST", "/quizzes", quizBody(s.math, map[string]any{"question_type": "text", "prompt": "No topic", "points": 1}))
	bad.expect(http.StatusBadRequest)
	if !strings.Contains(string(bad.Body), "Question 1") {
		t.Fatalf("the question should be named: %s", bad.Body)
	}

	// A draft students cannot see.
	numeric := map[string]any{"question_type": "numeric", "prompt": "3 x 4?", "value": 12, "tolerance": 0, "topic": "motion", "points": 2}
	quizID := teacher.create("/quizzes", quizBody(s.math, choiceQuestion("1 + 1?", "Motion"), numeric))
	if rows := learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("drafts stay hidden: %v", rows)
	}
	saved := teacher.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusOK).object()
	if len(saved["questions"].([]any)) != 2 || saved["status"] != "draft" || saved["can_edit"] != true {
		t.Fatalf("saved: %v", saved)
	}
	if q := saved["questions"].([]any)[1].(map[string]any); q["value"] != float64(12) || q["topic"] != "Motion" {
		t.Fatalf("a topic with other capitals joins the same topic: %v", q)
	}
	// Another teacher of the class can read but not change or publish.
	other.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusOK)
	other.call("PUT", "/quizzes/"+quizID, quizBody(s.math, choiceQuestion("Q", "T"))).expect(http.StatusForbidden)
	other.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{}).expect(http.StatusForbidden)
	other.call("DELETE", "/quizzes/"+quizID, nil).expect(http.StatusForbidden)
	learner.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusForbidden)
	_, outsider := inviteTeacher(s.server, s.admin)
	outsider.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusForbidden)
	outsider.call("GET", "/quizzes?class_id="+s.classID, nil).expect(http.StatusForbidden)

	// Editing replaces the questions, and leaves none behind.
	edited := quizBody(s.math, choiceQuestion("1 + 1?", "Motion"), numeric, map[string]any{"question_type": "text", "prompt": "Explain motion.", "guide": "Mentions distance", "topic": "Motion", "points": 5})
	teacher.call("PUT", "/quizzes/"+quizID, edited).expect(http.StatusOK)
	var questionCount int
	if err := testDB.QueryRow(context.Background(), `SELECT count(*) FROM assessment_items WHERE assessment_id=$1`, quizID).Scan(&questionCount); err != nil {
		t.Fatal(err)
	}
	if questionCount != 3 {
		t.Fatalf("items: %d", questionCount)
	}
	teacher.call("GET", "/quizzes/topics?class_subject_id="+s.math, nil).expect(http.StatusOK)

	// Empty drafts cannot go out.
	empty := teacher.create("/quizzes", quizBody(s.math))
	teacher.call("POST", "/quizzes/"+empty+"/publish", map[string]any{}).expect(http.StatusConflict)
	teacher.call("DELETE", "/quizzes/"+empty, nil).expect(http.StatusOK)

	// Published: students see it, and it can no longer change.
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{"due_at": future(48)}).expect(http.StatusOK)
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{}).expect(http.StatusConflict)
	teacher.call("PUT", "/quizzes/"+quizID, edited).expect(http.StatusConflict)
	rows := learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list()
	if len(rows) != 1 {
		t.Fatalf("students should see it: %v", rows)
	}
	assignmentID := rows[0]["assignment_id"].(string)
	next := learner.call("GET", "/student/learning-plan", nil).expect(http.StatusOK).object()["next"].([]any)
	if len(next) != 1 || next[0].(map[string]any)["assignment_id"] != assignmentID {
		t.Fatalf("My learning should offer it: %v", next)
	}
	list := teacher.call("GET", "/quizzes?class_id="+s.classID, nil).expect(http.StatusOK).list()
	if len(list) != 1 || list[0]["students"] != float64(2) || list[0]["submitted"] != float64(0) || list[0]["total_points"] != float64(8) || list[0]["assignment_id"] != assignmentID {
		t.Fatalf("list: %v", list)
	}

	// Taken back before anyone starts: students stop seeing it.
	second := teacher.create("/quizzes", quizBody(s.math, choiceQuestion("Q", "Motion")))
	teacher.call("POST", "/quizzes/"+second+"/publish", map[string]any{}).expect(http.StatusOK)
	teacher.call("DELETE", "/quizzes/"+second, nil).expect(http.StatusOK)
	if rows := learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list(); len(rows) != 1 {
		t.Fatalf("taken back: %v", rows)
	}

	// Once a student starts, it stays.
	learner.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{"start_code": ""}).expect(http.StatusCreated)
	teacher.call("DELETE", "/quizzes/"+quizID, nil).expect(http.StatusConflict)
	if data := learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object(); data["kind"] != "quiz" {
		t.Fatalf("kind: %v", data["kind"])
	}
}

func answerAll(t *testing.T, learner *apiClient, assignmentID string, answers map[string]any) map[string]any {
	t.Helper()
	learner.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{"start_code": ""}).expect(http.StatusCreated)
	view := learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	attempt := view["attempt_id"].(string)
	for i, raw := range view["questions"].([]any) {
		q := raw.(map[string]any)
		prompt := q["prompt"].(map[string]any)["text"].(string)
		answer, found := answers[prompt]
		if !found {
			continue
		}
		learner.call("PUT", "/student/attempts/"+attempt+"/answers/"+q["id"].(string), map[string]any{"answer": answer, "revision": i + 1, "idempotency_key": uuid(t), "flagged": false}).expect(http.StatusOK)
	}
	return learner.call("POST", "/student/attempts/"+attempt+"/submit", map[string]any{}).expect(http.StatusOK).object()
}

func TestQuizResultsReviewAndMarks(t *testing.T) {
	s := setupReports(t)
	teacher := s.teacher
	body := quizBody(s.math, choiceQuestion("Motion one", "Motion"), choiceQuestion("Force one", "Force"), map[string]any{"question_type": "numeric", "prompt": "Motion two", "value": 10, "tolerance": 0, "topic": "Motion", "points": 2}, map[string]any{"question_type": "text", "prompt": "Explain force", "guide": "SECRET-GUIDE", "topic": "Force", "points": 4})
	quizID := teacher.create("/quizzes", body)
	due := time.Now().UTC().Add(time.Hour)
	dueText := due.Format(time.RFC3339)
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{"due_at": dueText}).expect(http.StatusOK)
	rows := s.learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list()
	assignmentID := rows[0]["assignment_id"].(string)

	result := answerAll(t, s.learner, assignmentID, map[string]any{"Motion one": "A", "Force one": "B", "Motion two": "10", "Explain force": "Things push."})
	if result["results_released"] != false || result["manual_review_count"] != float64(1) {
		t.Fatalf("written work waits for the teacher: %v", result)
	}

	// The class view before marking leaves written work out of the figures.
	data := teacher.call("GET", "/quizzes/"+quizID+"/results", nil).expect(http.StatusOK).object()
	students := data["students"].([]any)
	if len(students) != 2 {
		t.Fatalf("students: %v", students)
	}
	statuses := map[string]string{}
	for _, raw := range students {
		row := raw.(map[string]any)
		statuses[row["student_id"].(string)] = row["status"].(string)
	}
	if statuses[s.studentID] != "submitted" || statuses[s.secondID] != "not_started" {
		t.Fatalf("statuses: %v", statuses)
	}
	questions := data["questions"].([]any)
	if q := questions[3].(map[string]any); q["answers_marked"] != float64(0) || q["percent"] != nil {
		t.Fatalf("unmarked written work: %v", q)
	}
	if q := questions[1].(map[string]any); q["percent"] != float64(0) {
		t.Fatalf("question: %v", q)
	}
	topics := data["topics"].([]any)
	if topics[0].(map[string]any)["topic"] != "Force" || topics[1].(map[string]any)["percent"] != float64(100) {
		t.Fatalf("hardest topic first: %v", topics)
	}

	// Nothing reaches marks until it is marked.
	sent := teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 20}).expect(http.StatusOK).object()
	if sent["sent"] != float64(0) || sent["waiting"] != float64(1) {
		t.Fatalf("sent early: %v", sent)
	}
	sheet := teacher.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusOK).object()
	if items := sheet["items"].([]any); len(items) != 1 || items[0].(map[string]any)["max_score"] != float64(8) || items[0].(map[string]any)["weight"] != float64(20) {
		t.Fatalf("mark item: %v", items)
	}

	// Answers stay hidden before the deadline, even after submitting.
	before := s.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	for _, raw := range before["questions"].([]any) {
		if q := raw.(map[string]any); q["correct_answer"] != nil || q["explanation"] != nil {
			t.Fatalf("answers before the deadline: %v", q)
		}
	}

	// The teacher marks the written answer, which releases the result.
	attempt := before["attempt_id"].(string)
	var writtenID string
	if err := testDB.QueryRow(context.Background(), `SELECT item.question_id::text FROM assessment_attempt_items item WHERE item.attempt_id=$1 AND item.question_snapshot->>'question_type'='text'`, attempt).Scan(&writtenID); err != nil {
		t.Fatal(err)
	}
	teacher.call("PUT", "/teacher/attempts/"+attempt+"/answers/"+writtenID, map[string]any{"points": 2, "feedback": "Say more."}).expect(http.StatusOK)

	// Sending again updates the same test and fills the mark: 1+0+2+2 of 8.
	again := teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 30}).expect(http.StatusOK).object()
	if again["sent"] != float64(1) || again["waiting"] != float64(0) {
		t.Fatalf("sent: %v", again)
	}
	sheet = teacher.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusOK).object()
	items := sheet["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["weight"] != float64(30) {
		t.Fatalf("the same test should update: %v", items)
	}
	item := items[0].(map[string]any)["id"].(string)
	for _, raw := range sheet["students"].([]any) {
		row := raw.(map[string]any)
		score, found := row["scores"].(map[string]any)[item]
		if row["student_id"] == s.studentID && score != float64(5) {
			t.Fatalf("mark: %v", row)
		}
		if row["student_id"] == s.secondID && found {
			t.Fatalf("no work means a blank, not a zero: %v", row)
		}
	}
	// Limits on sending.
	teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term2, "weight": 10}).expect(http.StatusConflict)
	teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 0}).expect(http.StatusBadRequest)
	teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 101}).expect(http.StatusBadRequest)
	s.other.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 10}).expect(http.StatusForbidden)
	s.learner.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 10}).expect(http.StatusForbidden)

	// Once the deadline passes the student can learn from it, but never sees the marking guide.
	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_assignments SET due_at=now()-interval '1 minute',assigned_at=now()-interval '1 day' WHERE id=$1`, assignmentID); err != nil {
		t.Fatal(err)
	}
	raw := s.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK)
	if strings.Contains(string(raw.Body), "SECRET-GUIDE") {
		t.Fatal("the marking guide must stay private")
	}
	review := raw.object()
	seenExplanation, seenWrong := false, false
	for _, entry := range review["questions"].([]any) {
		q := entry.(map[string]any)
		if q["explanation"] != nil {
			seenExplanation = true
		}
		if q["question_type"] == "single_choice" && q["correct_answer"] == nil {
			t.Fatalf("correct answer: %v", q)
		}
		if q["is_correct"] == false {
			seenWrong = true
		}
	}
	if !seenExplanation || !seenWrong {
		t.Fatalf("review: %v", review["questions"])
	}

	// The second student's teacher turned answers off for another quiz: they stay hidden.
	hiddenBody := quizBody(s.math, choiceQuestion("Hidden", "Motion"))
	hiddenBody["show_answers"] = false
	hidden := teacher.create("/quizzes", hiddenBody)
	teacher.call("POST", "/quizzes/"+hidden+"/publish", map[string]any{}).expect(http.StatusOK)
	var hiddenAssignment string
	if err := testDB.QueryRow(context.Background(), `SELECT id::text FROM assessment_assignments WHERE assessment_id=$1`, hidden).Scan(&hiddenAssignment); err != nil {
		t.Fatal(err)
	}
	answerAll(t, s.learner, hiddenAssignment, map[string]any{"Hidden": "A"})
	for _, entry := range s.learner.call("GET", "/student/assessments/"+hiddenAssignment, nil).expect(http.StatusOK).object()["questions"].([]any) {
		if q := entry.(map[string]any); q["correct_answer"] != nil {
			t.Fatalf("answers were turned off: %v", q)
		}
	}

	// Report cards lock it.
	exam := s.item(teacher, s.math, s.term1, "Exam", 70, 70)
	s.mark(teacher, s.math, s.term1, exam, s.studentID, 50).expect(http.StatusOK)
	s.admin.call("POST", "/report-cards/classes/"+s.classID+"/publish", map[string]any{"term_id": s.term1, "allow_incomplete": true}).expect(http.StatusOK)
	teacher.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 30}).expect(http.StatusConflict)

	// A quiz score shows up as evidence in My learning, by topic and under its subject.
	subjects := s.learner.call("GET", "/student/learning-profile", nil).expect(http.StatusOK).object()["skills"].([]any)
	if len(subjects) < 2 {
		t.Fatalf("topics should reach skill tracking: %v", subjects)
	}
}

func TestAssignmentsAndCopying(t *testing.T) {
	s := setupReports(t)
	teacher := s.teacher
	body := quizBody(s.math, map[string]any{"question_type": "text", "prompt": "Show your working.", "topic": "Algebra", "points": 10})
	body["kind"] = "assignment"
	body["duration_minutes"] = 0

	// A worksheet from this class only.
	var teacherID, schoolID string
	if err := testDB.QueryRow(context.Background(), `SELECT teacher_id::text,school_id::text FROM class_subjects WHERE id=$1`, s.math).Scan(&teacherID, &schoolID); err != nil {
		t.Fatal(err)
	}
	addFile := func(classID, title string) string {
		t.Helper()
		var id string
		if err := testDB.QueryRow(context.Background(), `INSERT INTO teaching_resources(school_id,class_id,uploaded_by,title,subject,file_name,media_type,file_size,file_data) VALUES($1,$2,$3,$4,'Mathematics','f.pdf','application/pdf',1,'x') RETURNING id::text`, schoolID, classID, teacherID, title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	otherClass := createClass(s.admin)
	body["resource_id"] = addFile(otherClass, "Elsewhere")
	teacher.call("POST", "/quizzes", body).expect(http.StatusBadRequest)
	worksheet := addFile(s.classID, "Algebra worksheet")
	body["resource_id"] = worksheet
	quizID := teacher.create("/quizzes", body)

	// An assignment needs a due date, and stays open for days.
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{}).expect(http.StatusBadRequest)
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{"due_at": future(24 * 40)}).expect(http.StatusBadRequest)
	teacher.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{"due_at": future(24 * 3)}).expect(http.StatusOK)
	assignmentID := s.learner.call("GET", "/student/assessments", nil).expect(http.StatusOK).list()[0]["assignment_id"].(string)
	opening := s.learner.call("GET", "/student/assessments/"+assignmentID, nil).expect(http.StatusOK).object()
	if opening["kind"] != "assignment" || opening["resource_id"] != worksheet || opening["resource_title"] != "Algebra worksheet" {
		t.Fatalf("the student should see the worksheet: %v", opening)
	}
	s.learner.call("GET", "/resources/"+worksheet+"/download", nil).expect(http.StatusOK)
	s.learner.call("POST", "/student/assessments/"+assignmentID+"/start", map[string]any{"start_code": ""}).expect(http.StatusCreated)
	var left time.Duration
	var deadline time.Time
	if err := testDB.QueryRow(context.Background(), `SELECT deadline_at FROM assessment_attempts WHERE assignment_id=$1`, assignmentID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	left = time.Until(deadline)
	if left < 70*time.Hour {
		t.Fatalf("an assignment should run to its due date: %v", left)
	}

	// Extending the date also extends work under way.
	teacher.call("PUT", "/quizzes/"+quizID+"/schedule", map[string]any{"due_at": future(24 * 5)}).expect(http.StatusOK)
	s.other.call("PUT", "/quizzes/"+quizID+"/schedule", map[string]any{"due_at": future(24 * 6)}).expect(http.StatusForbidden)
	teacher.call("PUT", "/quizzes/"+quizID+"/schedule", map[string]any{"due_at": nil}).expect(http.StatusBadRequest)
	if err := testDB.QueryRow(context.Background(), `SELECT deadline_at FROM assessment_attempts WHERE assignment_id=$1`, assignmentID).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if time.Until(deadline) < 118*time.Hour {
		t.Fatalf("the extension should reach work under way: %v", time.Until(deadline))
	}

	// A copy for another section is a draft, without the other class's worksheet.
	s.admin.call("POST", "/classes/"+otherClass+"/teachers", map[string]string{"teacher_id": teacherID}).expect(http.StatusOK)
	s.admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{otherClass}, "names": []string{"Mathematics"}}).expect(http.StatusCreated)
	target := s.admin.call("GET", "/classes/"+otherClass+"/subjects", nil).expect(http.StatusOK).list()[0]["id"].(string)
	copyID := teacher.create("/quizzes/"+quizID+"/copy", map[string]string{"class_subject_id": target})
	copied := teacher.call("GET", "/quizzes/"+copyID, nil).expect(http.StatusOK).object()
	if copied["status"] != "draft" || copied["resource_id"] != "" || len(copied["questions"].([]any)) != 1 {
		t.Fatalf("copy: %v", copied)
	}
	// Editing the copy leaves the original's questions alone, and deleting it too.
	edit := quizBody(target, choiceQuestion("Changed", "Algebra"))
	teacher.call("PUT", "/quizzes/"+copyID, edit).expect(http.StatusOK)
	if original := teacher.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusOK).object(); original["questions"].([]any)[0].(map[string]any)["prompt"] != "Show your working." {
		t.Fatalf("the original changed: %v", original)
	}
	second := teacher.create("/quizzes/"+quizID+"/copy", map[string]string{"class_subject_id": target})
	teacher.call("DELETE", "/quizzes/"+second, nil).expect(http.StatusOK)
	if original := teacher.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusOK).object(); len(original["questions"].([]any)) != 1 {
		t.Fatalf("deleting a copy must keep the original: %v", original)
	}
	s.other.call("POST", "/quizzes/"+quizID+"/copy", map[string]string{"class_subject_id": target}).expect(http.StatusForbidden)

	// A subject with quizzes stays.
	s.admin.call("DELETE", "/class-subjects/"+s.math, nil).expect(http.StatusConflict)
}

func TestQuizzesStayInsideTheSchool(t *testing.T) {
	s := setupReports(t)
	quizID := s.teacher.create("/quizzes", quizBody(s.math, choiceQuestion("Q", "Motion")))
	_, other := seedSchool(t, s.server)
	other.call("GET", "/quizzes/"+quizID, nil).expect(http.StatusNotFound)
	other.call("PUT", "/quizzes/"+quizID, quizBody(s.math, choiceQuestion("Q", "T"))).expect(http.StatusNotFound)
	other.call("DELETE", "/quizzes/"+quizID, nil).expect(http.StatusNotFound)
	other.call("POST", "/quizzes/"+quizID+"/publish", map[string]any{}).expect(http.StatusNotFound)
	other.call("GET", "/quizzes/"+quizID+"/results", nil).expect(http.StatusNotFound)
	other.call("POST", "/quizzes/"+quizID+"/send-to-marks", map[string]any{"term_id": s.term1, "weight": 5}).expect(http.StatusNotFound)
	other.call("POST", "/quizzes", quizBody(s.math, choiceQuestion("Q", "T"))).expect(http.StatusNotFound)
	other.call("GET", "/quizzes?class_id="+s.classID, nil).expect(http.StatusForbidden)
	other.call("GET", "/quizzes/topics?class_subject_id="+s.math, nil).expect(http.StatusNotFound)
}
