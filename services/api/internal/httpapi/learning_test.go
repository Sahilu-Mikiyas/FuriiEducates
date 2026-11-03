package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func cardWith(pass float64, lines ...cardSubject) reportCard {
	return reportCard{PassPercent: pass, Subjects: lines}
}

func line(name string, total float64) cardSubject {
	return cardSubject{SubjectID: name, Name: name, Total: &total}
}

func names(subjects []learningSubject) []string {
	result := []string{}
	for _, x := range subjects {
		result = append(result, x.Name)
	}
	return result
}

func equalStrings(p, q []string) bool {
	if len(p) != len(q) {
		return false
	}
	for i := range p {
		if p[i] != q[i] {
			return false
		}
	}
	return true
}

func TestPickSubjects(t *testing.T) {
	best, focus := pickSubjects(cardWith(50, line("Maths", 90), line("English", 70), line("Physics", 55), line("Amharic", 80)))
	if !equalStrings(names(best), []string{"Maths", "Amharic"}) || !equalStrings(names(focus), []string{"Physics", "English"}) {
		t.Fatalf("four subjects: %v %v", names(best), names(focus))
	}
	best, focus = pickSubjects(cardWith(50, line("Maths", 90), line("Physics", 55), line("English", 70)))
	if !equalStrings(names(best), []string{"Maths"}) || !equalStrings(names(focus), []string{"Physics"}) {
		t.Fatalf("three subjects: %v %v", names(best), names(focus))
	}
	// Nothing below the pass mark is called going well, however it ranks.
	best, focus = pickSubjects(cardWith(50, line("Maths", 45), line("Physics", 30)))
	if len(best) != 0 || !equalStrings(names(focus), []string{"Physics"}) || !focus[0].BelowPass {
		t.Fatalf("all failing: %v %+v", names(best), focus)
	}
	// Nothing can be told apart.
	if best, focus = pickSubjects(cardWith(50, line("Maths", 80), line("Physics", 80))); len(best)+len(focus) != 0 {
		t.Fatalf("all equal: %v %v", best, focus)
	}
	if best, focus = pickSubjects(cardWith(50, line("Maths", 80))); len(best)+len(focus) != 0 {
		t.Fatalf("one subject: %v %v", best, focus)
	}
	// A missing mark leaves a subject out rather than ranking it last.
	blank := cardSubject{Name: "Blank"}
	best, focus = pickSubjects(cardWith(50, line("Maths", 90), blank, line("Physics", 60)))
	if !equalStrings(names(focus), []string{"Physics"}) || !equalStrings(names(best), []string{"Maths"}) {
		t.Fatalf("missing mark: %v %v", names(best), names(focus))
	}
	// The change compares with the term just before.
	first, second := 80.0, 62.5
	withEarlier := line("Maths", 90)
	withEarlier.Earlier = map[string]*float64{"t1": &first}
	lower := line("Physics", 50)
	lower.Earlier = map[string]*float64{"t1": &second}
	card := cardWith(50, withEarlier, lower)
	card.EarlierTerms = []earlierTerm{{TermID: "t1"}}
	best, focus = pickSubjects(card)
	if *best[0].Change != 10 || *focus[0].Change != -12.5 {
		t.Fatalf("changes: %v %v", *best[0].Change, *focus[0].Change)
	}
}

func TestPickTopicsNeedsEnoughAnswers(t *testing.T) {
	strong, weak := pickTopics([]learningTopic{
		{Title: "Thin but bad", Percent: 0, Responses: learningMinResponses - 1},
		{Title: "Thin but good", Percent: 100, Responses: learningMinResponses - 1},
		{Title: "Middling", Percent: 70, Responses: 20},
		{Title: "Weak", Percent: 40, Responses: learningMinResponses},
		{Title: "Weakest", Percent: 20, Responses: 9},
		{Title: "Edge of weak", Percent: learningWeakTopic, Responses: 9},
		{Title: "Strong", Percent: learningStrongTopic, Responses: 9},
		{Title: "Strongest", Percent: 95, Responses: 9},
	})
	titles := func(items []learningTopic) []string {
		result := []string{}
		for _, x := range items {
			result = append(result, x.Title)
		}
		return result
	}
	if !equalStrings(titles(weak), []string{"Weakest", "Weak"}) || !equalStrings(titles(strong), []string{"Strongest", "Strong"}) {
		t.Fatalf("strong %v weak %v", titles(strong), titles(weak))
	}
}

func TestSameSubjectAndAttachment(t *testing.T) {
	motion := learningTopic{Title: "Motion", Subject: "Physics Grade 12", Paper: "physics"}
	verbs := learningTopic{Title: "Verbs", Subject: "english"}
	geography := learningTopic{Title: "Maps", Subject: "Geography"}
	subjects := []learningSubject{{Name: " PHYSICS "}, {Name: "English"}}
	rest := attachTopics(subjects, []learningTopic{motion, verbs, geography})
	if len(subjects[0].Topics) != 1 || subjects[0].Topics[0].Title != "Motion" || len(subjects[1].Topics) != 1 || len(rest) != 1 || rest[0].Title != "Maps" {
		t.Fatalf("attached: %+v rest %+v", subjects, rest)
	}
	if sameSubject("", learningTopic{Subject: ""}) {
		t.Fatal("blank names must not match")
	}
}

func TestBuildNextOrder(t *testing.T) {
	resume := &learningAction{Type: "resume"}
	review := &learningAction{Type: "review"}
	assignments := []learningAction{{Type: "assignment", Title: "Quiz"}}
	weak := []learningTopic{{Title: "No questions", Subject: "Maths"}, {Title: "Motion", Subject: "Physics", Paper: "physics", CanPractice: true, SkillID: "s1"}}
	focus := []learningSubject{{Name: "Physics"}}
	resources := []learningResource{{ID: "r1", Title: "Forces worksheet", Subject: "physics"}, {ID: "r2", Title: "Art notes", Subject: "Art"}, {ID: "r3", Title: "Algebra sheet", Subject: "Maths"}}
	got := buildNext(resume, assignments, weak, focus, resources, review)
	kinds := []string{}
	for _, x := range got {
		kinds = append(kinds, x.Type+":"+x.Title)
	}
	want := []string{"resume:", "assignment:Quiz", "practice:Motion", "resource:Forces worksheet", "resource:Algebra sheet"}
	if !equalStrings(kinds, want) {
		t.Fatalf("got %v", kinds)
	}
	if empty := buildNext(nil, nil, nil, nil, nil, nil); len(empty) != 0 {
		t.Fatalf("nothing to do: %v", empty)
	}
	if only := buildNext(nil, nil, nil, nil, nil, review); len(only) != 1 || only[0].Type != "review" {
		t.Fatalf("review: %v", only)
	}
}

func TestLearningPlanFromRealWork(t *testing.T) {
	s := setupReports(t)
	plan := func() map[string]any {
		return s.learner.call("GET", "/student/learning-plan", nil).expect(http.StatusOK).object()
	}

	// Nothing yet: it says so rather than guessing.
	empty := plan()
	if empty["has_evidence"] != false || len(empty["next"].([]any)) != 0 || empty["attendance"] != nil {
		t.Fatalf("empty plan: %v", empty)
	}

	// Unpublished marks stay out.
	mathExam := s.item(s.admin, s.math, s.term1, "Exam", 100, 100)
	englishExam := s.item(s.admin, s.english, s.term1, "Exam", 100, 100)
	s.mark(s.admin, s.math, s.term1, mathExam, s.studentID, 45).expect(http.StatusOK)
	s.mark(s.admin, s.english, s.term1, englishExam, s.studentID, 85).expect(http.StatusOK)
	s.mark(s.admin, s.math, s.term1, mathExam, s.secondID, 50).expect(http.StatusOK)
	s.mark(s.admin, s.english, s.term1, englishExam, s.secondID, 50).expect(http.StatusOK)
	if unpublished := plan(); unpublished["has_evidence"] != false {
		t.Fatalf("unpublished marks must not count: %v", unpublished)
	}
	s.admin.call("POST", "/report-cards/classes/"+s.classID+"/publish", map[string]any{"term_id": s.term1}).expect(http.StatusOK)

	published := plan()
	if published["report_term"] != "Semester 1" || published["has_evidence"] != true {
		t.Fatalf("published: %v", published)
	}
	best := published["going_well"].([]any)
	focus := published["room_to_improve"].([]any)
	if len(best) != 1 || best[0].(map[string]any)["name"] != "English" || len(focus) != 1 || focus[0].(map[string]any)["name"] != "Mathematics" || focus[0].(map[string]any)["below_pass"] != true {
		t.Fatalf("subjects: %v %v", best, focus)
	}

	// Practice on a paper: weak topics appear, with practice offered.
	addPrepQuestions(t, s.admin, "mathematics", 12, false)
	session := startSession(s.learner, map[string]any{"mode": "drill", "paper": "mathematics", "question_count": 12})
	answerSession(t, s.learner, session, 12, false)
	inProgress := plan()
	next := inProgress["next"].([]any)
	if next[0].(map[string]any)["type"] != "resume" || next[0].(map[string]any)["session_id"] != session["id"] {
		t.Fatalf("unfinished practice comes first: %v", next)
	}
	s.learner.call("POST", "/student/prep/sessions/"+session["id"].(string)+"/finish", map[string]any{}).expect(http.StatusOK)

	after := plan()
	subject := after["room_to_improve"].([]any)[0].(map[string]any)
	topics := subject["topics"].([]any)
	if len(topics) != 2 || topics[0].(map[string]any)["percent"].(float64) >= learningWeakTopic || topics[0].(map[string]any)["can_practice"] != true {
		t.Fatalf("topics should sit under their subject: %v", subject)
	}
	if rest := after["weak_topics"].([]any); len(rest) != 0 {
		t.Fatalf("attached topics are not repeated: %v", rest)
	}
	var practice map[string]any
	for _, raw := range after["next"].([]any) {
		if action := raw.(map[string]any); action["type"] == "practice" {
			practice = action
			break
		}
	}
	if practice == nil || practice["paper"] != "mathematics" {
		t.Fatalf("practice should be offered: %v", after["next"])
	}
	// The offered practice really starts.
	offered := s.learner.create("/student/prep/sessions", map[string]any{"mode": "drill", "paper": practice["paper"], "skill_id": practice["skill_id"]})
	s.learner.call("POST", "/student/prep/sessions/"+offered+"/finish", map[string]any{}).expect(http.StatusOK)

	// The plan's figure is the same estimate the student sees in skill mastery.
	profile := s.learner.call("GET", "/student/learning-profile", nil).expect(http.StatusOK).object()
	for _, raw := range profile["skills"].([]any) {
		skill := raw.(map[string]any)
		if skill["skill_id"] == topics[0].(map[string]any)["skill_id"] && round2(skill["estimated_mastery"].(float64)) != topics[0].(map[string]any)["percent"] {
			t.Fatalf("plan %v and profile %v disagree", topics[0], skill)
		}
	}

	// A class file on the subject is suggested; another class's or subject's is not.
	var uploader string
	if err := testDB.QueryRow(context.Background(), `SELECT teacher_id::text FROM class_teachers WHERE class_id=$1 LIMIT 1`, s.classID).Scan(&uploader); err != nil {
		t.Fatal(err)
	}
	var schoolID string
	if err := testDB.QueryRow(context.Background(), `SELECT school_id::text FROM classes WHERE id=$1`, s.classID).Scan(&schoolID); err != nil {
		t.Fatal(err)
	}
	addResource := func(classID, title, subject string) {
		t.Helper()
		if _, err := testDB.Exec(context.Background(), `INSERT INTO teaching_resources(school_id,class_id,uploaded_by,title,subject,file_name,media_type,file_size,file_data) VALUES($1,$2,$3,$4,$5,'f.pdf','application/pdf',1,'x')`, schoolID, classID, uploader, title, subject); err != nil {
			t.Fatal(err)
		}
	}
	otherClass := createClass(s.admin)
	addResource(s.classID, "Algebra worksheet", "mathematics")
	addResource(s.classID, "Essay guide", "English")
	addResource(otherClass, "Other class maths", "Mathematics")
	titles := []string{}
	for _, raw := range plan()["next"].([]any) {
		if action := raw.(map[string]any); action["type"] == "resource" {
			titles = append(titles, action["title"].(string))
		}
	}
	if !equalStrings(titles, []string{"Algebra worksheet"}) {
		t.Fatalf("resources: %v", titles)
	}

	// Attendance: an alert once there is enough to go on.
	day := func(date, status string) {
		s.admin.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": date, "records": []map[string]any{{"student_id": s.studentID, "status": status}}}).expect(http.StatusOK)
	}
	today := time.Now().UTC()
	for back, status := range []string{"absent", "absent", "absent", "present", "present"} {
		day(today.AddDate(0, 0, -(back+1)).Format("2006-01-02"), status)
	}
	attendance := plan()["attendance"].(map[string]any)
	if attendance["reason"] != "streak" || attendance["days_absent"] != float64(3) || attendance["days_marked"] != float64(5) {
		t.Fatalf("attendance: %v", attendance)
	}

	// The same plan reaches staff who may see the student, and nobody else.
	if staff := s.teacher.call("GET", "/students/"+s.studentID+"/learning-plan", nil).expect(http.StatusOK).object(); staff["report_term"] != "Semester 1" {
		t.Fatalf("teacher: %v", staff)
	}
	s.admin.call("GET", "/students/"+s.studentID+"/learning-plan", nil).expect(http.StatusOK)
	_, outsider := inviteTeacher(s.server, s.admin)
	outsider.call("GET", "/students/"+s.studentID+"/learning-plan", nil).expect(http.StatusNotFound)
	s.learner.call("GET", "/students/"+s.studentID+"/learning-plan", nil).expect(http.StatusForbidden)
	s.admin.call("GET", "/student/learning-plan", nil).expect(http.StatusForbidden)
	_, otherSchool := seedSchool(t, s.server)
	otherSchool.call("GET", "/students/"+s.studentID+"/learning-plan", nil).expect(http.StatusNotFound)

	// A parent sees it through their own child.
	guardianID := addGuardian(s.admin, s.studentID, "Almaz")
	email := uniqueEmail("parent")
	parent := signUpParent(t, s.server, email, inviteToken(s.admin, s.studentID, guardianID, email))
	child := parent.call("GET", "/parent/children/"+s.studentID, nil).expect(http.StatusOK).object()
	if child["learning_plan"].(map[string]any)["report_term"] != "Semester 1" {
		t.Fatalf("parent: %v", child["learning_plan"])
	}
}

func TestLearningPlanAssignments(t *testing.T) {
	server, admin, _, learner, classID, _ := prepFixture(t)
	skill := createSkill(admin)
	question := publishQuestion(admin, skill, map[string]any{"question_type": "single_choice", "prompt": map[string]string{"text": "1 + 1?"}, "options": []map[string]string{{"id": "A", "text": "2"}, {"id": "B", "text": "3"}}, "answer_key": []string{"A"}})
	_, assignmentID := publishAssessment(admin, classID, []string{question})
	actions := func(client *apiClient) []any {
		return client.call("GET", "/student/learning-plan", nil).expect(http.StatusOK).object()["next"].([]any)
	}
	next := actions(learner)
	if len(next) != 1 || next[0].(map[string]any)["type"] != "assignment" || next[0].(map[string]any)["assignment_id"] != assignmentID {
		t.Fatalf("an open test should be next: %v", next)
	}
	// Past its deadline or not yet open, it is not work to do now.
	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_assignments SET due_at=now()-interval '1 hour',opens_at=now()-interval '2 hours' WHERE id=$1`, assignmentID); err != nil {
		t.Fatal(err)
	}
	if next := actions(learner); len(next) != 0 {
		t.Fatalf("past due: %v", next)
	}
	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_assignments SET due_at=NULL,opens_at=now()+interval '1 hour' WHERE id=$1`, assignmentID); err != nil {
		t.Fatal(err)
	}
	if next := actions(learner); len(next) != 0 {
		t.Fatalf("not open yet: %v", next)
	}
	// Another class's student is not told about it.
	if _, err := testDB.Exec(context.Background(), `UPDATE assessment_assignments SET opens_at=NULL WHERE id=$1`, assignmentID); err != nil {
		t.Fatal(err)
	}
	_, other := enrollLearner(server, admin, createClass(admin))
	if next := actions(other); len(next) != 0 {
		t.Fatalf("another class: %v", next)
	}
}
