package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubjectTotalAndRank(t *testing.T) {
	items := []markItem{{ID: "quiz", Weight: 20, Max: 40}, {ID: "final", Weight: 80, Max: 100}}
	if total, missing := subjectTotal(items, map[string]float64{"quiz": 30, "final": 75}); total != 75 || missing != 0 {
		t.Fatalf("weighted total: %v %v", total, missing)
	}
	if _, missing := subjectTotal(items, map[string]float64{"quiz": 30}); missing != 1 {
		t.Fatalf("a blank mark is missing, not zero: %d", missing)
	}
	if total, missing := subjectTotal(items, map[string]float64{"quiz": 0, "final": 0}); total != 0 || missing != 0 {
		t.Fatalf("a real zero counts: %v %d", total, missing)
	}
	ranks := rankScores(map[string]float64{"a": 90, "b": 90, "c": 80, "d": 95})
	if ranks["d"] != 1 || ranks["a"] != 2 || ranks["b"] != 2 || ranks["c"] != 4 {
		t.Fatalf("ranks: %v", ranks)
	}
}

func TestGradeBands(t *testing.T) {
	bands := []gradeBand{{"F", 0}, {"A", 90}, {"C", 60}}
	if message := checkGradeBands(bands); message != "" {
		t.Fatal(message)
	}
	if bands[0].Label != "A" || gradeFor(bands, 90) != "A" || gradeFor(bands, 89.99) != "C" || gradeFor(bands, 0) != "F" {
		t.Fatalf("bands: %v", bands)
	}
	for name, wrong := range map[string][]gradeBand{
		"too few":      {{"A", 0}},
		"no floor":     {{"A", 90}, {"B", 50}},
		"same name":    {{"A", 90}, {"a", 0}},
		"same percent": {{"A", 50}, {"B", 50}, {"C", 0}},
		"blank name":   {{"", 50}, {"B", 0}},
		"over 100":     {{"A", 101}, {"B", 0}},
	} {
		if checkGradeBands(wrong) == "" {
			t.Errorf("%s should be refused", name)
		}
	}
}

type reportSetup struct {
	server                         *httptest.Server
	admin, teacher, learner, other *apiClient
	classID, studentID, secondID   string
	yearID, term1, term2           string
	math, english                  string
}

// setupReports builds a class of two students with two subjects and two terms.
// Math belongs to the fixture's teacher; English to no one in particular.
func setupReports(t *testing.T) reportSetup {
	t.Helper()
	server, admin, teacher, learner, classID, studentID := prepFixture(t)
	secondID, _ := enrollLearner(server, admin, classID)
	var teacherID string
	if err := testDB.QueryRow(context.Background(), `SELECT teacher_id::text FROM class_teachers WHERE class_id=$1`, classID).Scan(&teacherID); err != nil {
		t.Fatal(err)
	}
	otherTeacherID, other := inviteTeacher(server, admin)
	admin.call("POST", "/classes/"+classID+"/teachers", map[string]string{"teacher_id": otherTeacherID}).expect(http.StatusOK)
	var yearID string
	if err := testDB.QueryRow(context.Background(), `SELECT academic_year_id::text FROM classes WHERE id=$1`, classID).Scan(&yearID); err != nil {
		t.Fatal(err)
	}
	term1 := admin.create("/terms", map[string]string{"academic_year_id": yearID, "name": "Semester 1", "starts_on": "2026-09-01", "ends_on": "2027-01-15"})
	term2 := admin.create("/terms", map[string]string{"academic_year_id": yearID, "name": "Semester 2", "starts_on": "2027-01-16", "ends_on": "2027-07-01"})
	admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{classID}, "names": []string{"Mathematics", "English"}}).expect(http.StatusCreated)
	s := reportSetup{server: server, admin: admin, teacher: teacher, learner: learner, other: other, classID: classID, studentID: studentID, secondID: secondID, yearID: yearID, term1: term1, term2: term2}
	for _, row := range admin.call("GET", "/classes/"+classID+"/subjects", nil).expect(http.StatusOK).list() {
		if row["name"] == "Mathematics" {
			s.math = row["id"].(string)
		} else {
			s.english = row["id"].(string)
		}
	}
	admin.call("PUT", "/class-subjects/"+s.math, map[string]any{"name": "Mathematics", "teacher_id": teacherID, "counts_in_average": true}).expect(http.StatusOK)
	return s
}

func (s reportSetup) item(c *apiClient, subject, term, title string, weight, maxScore float64) string {
	return c.create("/marks/items", map[string]any{"class_subject_id": subject, "term_id": term, "title": title, "weight": weight, "max_score": maxScore})
}

func (s reportSetup) mark(c *apiClient, subject, term, item, student string, score any) apiResponse {
	return c.call("PUT", "/marks", map[string]any{"class_subject_id": subject, "term_id": term, "marks": []map[string]any{{"student_id": student, "item_id": item, "score": score}}})
}

func TestTermsAndSettings(t *testing.T) {
	s := setupReports(t)
	admin := s.admin
	s.teacher.call("POST", "/terms", map[string]string{"academic_year_id": s.yearID, "name": "X", "starts_on": "2026-09-01", "ends_on": "2026-09-02"}).expect(http.StatusForbidden)
	admin.call("POST", "/terms", map[string]string{"academic_year_id": s.yearID, "name": "Backwards", "starts_on": "2026-10-01", "ends_on": "2026-09-01"}).expect(http.StatusBadRequest)
	admin.call("POST", "/terms", map[string]string{"academic_year_id": s.yearID, "name": "Outside", "starts_on": "2020-10-01", "ends_on": "2020-11-01"}).expect(http.StatusBadRequest)
	overlap := admin.call("POST", "/terms", map[string]string{"academic_year_id": s.yearID, "name": "Overlap", "starts_on": "2027-01-01", "ends_on": "2027-02-01"})
	overlap.expect(http.StatusBadRequest)
	if overlap.errorCode() != "TERM_OVERLAP" {
		t.Fatalf("code %s", overlap.errorCode())
	}
	admin.call("PUT", "/terms/"+s.term1, map[string]string{"name": "First semester", "starts_on": "2026-09-01", "ends_on": "2027-01-10"}).expect(http.StatusOK)
	admin.call("PUT", "/terms/"+s.term1, map[string]string{"name": "First semester", "starts_on": "2026-09-01", "ends_on": "2027-02-01"}).expect(http.StatusBadRequest)
	if rows := s.teacher.call("GET", "/terms", nil).expect(http.StatusOK).list(); len(rows) != 2 || rows[0]["name"] != "First semester" {
		t.Fatalf("terms: %v", rows)
	}
	s.item(admin, s.math, s.term1, "Quiz", 10, 10)
	inUse := admin.call("DELETE", "/terms/"+s.term1, nil)
	inUse.expect(http.StatusConflict)
	admin.call("DELETE", "/terms/"+s.term2, nil).expect(http.StatusOK)

	settings := s.teacher.call("GET", "/report-settings", nil).expect(http.StatusOK).object()
	if settings["custom"] != false || settings["pass_percent"] != float64(50) {
		t.Fatalf("defaults: %v", settings)
	}
	s.teacher.call("PUT", "/report-settings", map[string]any{"pass_percent": 40, "grade_bands": []map[string]any{{"label": "P", "min_percent": 40}, {"label": "F", "min_percent": 0}}}).expect(http.StatusForbidden)
	admin.call("PUT", "/report-settings", map[string]any{"pass_percent": 40, "grade_bands": []map[string]any{{"label": "P", "min_percent": 40}}}).expect(http.StatusBadRequest)
	admin.call("PUT", "/report-settings", map[string]any{"pass_percent": 40, "grade_bands": []map[string]any{{"label": "F", "min_percent": 0}, {"label": "P", "min_percent": 40}}}).expect(http.StatusOK)
	saved := admin.call("GET", "/report-settings", nil).expect(http.StatusOK).object()
	if bands := saved["grade_bands"].([]any); saved["custom"] != true || bands[0].(map[string]any)["label"] != "P" {
		t.Fatalf("saved: %v", saved)
	}
}

func TestClassSubjectsSetup(t *testing.T) {
	s := setupReports(t)
	admin := s.admin
	second := createClass(admin)
	// Setting up a whole grade adds what is new and leaves what exists.
	created := admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{s.classID, second}, "names": []string{"english", "Physics", " physics "}}).expect(http.StatusCreated).object()
	if created["created"] != float64(3) {
		t.Fatalf("created: %v", created)
	}
	if rows := admin.call("GET", "/classes/"+second+"/subjects", nil).expect(http.StatusOK).list(); len(rows) != 2 {
		t.Fatalf("second class: %v", rows)
	}
	admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{"nope"}, "names": []string{"Art"}}).expect(http.StatusBadRequest)
	admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{s.classID}, "names": []string{" "}}).expect(http.StatusBadRequest)
	s.teacher.call("POST", "/class-subjects", map[string]any{"class_ids": []string{s.classID}, "names": []string{"Art"}}).expect(http.StatusForbidden)
	admin.call("PUT", "/class-subjects/"+s.math, map[string]any{"name": "Mathematics", "teacher_id": s.math, "counts_in_average": true}).expect(http.StatusBadRequest)

	// Naming a teacher puts the class in their workspace.
	teacherID, teacher := inviteTeacher(s.server, admin)
	subject := admin.call("GET", "/classes/"+second+"/subjects", nil).expect(http.StatusOK).list()[0]["id"].(string)
	teacher.call("GET", "/classes/"+second+"/subjects", nil).expect(http.StatusForbidden)
	admin.call("PUT", "/class-subjects/"+subject, map[string]any{"name": "Biology", "teacher_id": teacherID, "counts_in_average": false}).expect(http.StatusOK)
	rows := teacher.call("GET", "/classes/"+second+"/subjects", nil).expect(http.StatusOK).list()
	if !containsID(rows, subject) {
		t.Fatalf("the teacher should see the class: %v", rows)
	}

	// A subject with tests stays.
	s.item(admin, s.math, s.term1, "Quiz", 10, 10)
	admin.call("DELETE", "/class-subjects/"+s.math, nil).expect(http.StatusConflict)
	admin.call("DELETE", "/class-subjects/"+s.english, nil).expect(http.StatusOK)
}

func TestMarkEntryRules(t *testing.T) {
	s := setupReports(t)
	quiz := s.item(s.teacher, s.math, s.term1, "Quiz", 20, 40)

	// Who can do what.
	s.other.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Sneaky", "weight": 5, "max_score": 5}).expect(http.StatusForbidden)
	s.mark(s.other, s.math, s.term1, quiz, s.studentID, 10).expect(http.StatusForbidden)
	s.mark(s.learner, s.math, s.term1, quiz, s.studentID, 40).expect(http.StatusForbidden)
	s.other.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusOK)
	s.learner.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusForbidden)
	// A subject with no teacher named is open to the class's teachers.
	s.item(s.other, s.english, s.term1, "Essay", 100, 100)

	// A teacher outside the class gets a refusal for a subject with no teacher too.
	_, outsiderTeacher := inviteTeacher(s.server, s.admin)
	outsiderTeacher.call("GET", "/marks?class_subject_id="+s.english+"&term_id="+s.term1, nil).expect(http.StatusForbidden)
	outsiderTeacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.english, "term_id": s.term1, "title": "Sneaky", "weight": 5, "max_score": 5}).expect(http.StatusForbidden)

	// Weights stop at 100.
	over := s.teacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Too much", "weight": 90, "max_score": 100})
	over.expect(http.StatusBadRequest)
	if over.errorCode() != "WEIGHTS_OVER_100" {
		t.Fatalf("code %s", over.errorCode())
	}
	s.teacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Quiz", "weight": 5, "max_score": 5}).expect(http.StatusConflict)
	s.teacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Zero", "weight": 0, "max_score": 5}).expect(http.StatusBadRequest)

	// Marks stay within the test's maximum, and within the class.
	s.mark(s.teacher, s.math, s.term1, quiz, s.studentID, 41).expect(http.StatusBadRequest)
	s.mark(s.teacher, s.math, s.term1, quiz, s.studentID, -1).expect(http.StatusBadRequest)
	outsider := s.admin.create("/students", map[string]string{"student_number": unique("X"), "legal_name": "Outsider", "class_id": createClass(s.admin)})
	s.mark(s.teacher, s.math, s.term1, quiz, outsider, 10).expect(http.StatusBadRequest)
	s.mark(s.teacher, s.math, s.term1, s.math, s.studentID, 10).expect(http.StatusBadRequest)
	s.teacher.call("PUT", "/marks", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "marks": []map[string]any{{"student_id": s.studentID, "item_id": quiz, "score": 1}, {"student_id": s.studentID, "item_id": quiz, "score": 2}}}).expect(http.StatusBadRequest)

	s.mark(s.teacher, s.math, s.term1, quiz, s.studentID, 30).expect(http.StatusOK)
	sheet := s.teacher.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusOK).object()
	found := false
	for _, raw := range sheet["students"].([]any) {
		row := raw.(map[string]any)
		if row["student_id"] == s.studentID {
			found = row["scores"].(map[string]any)[quiz] == float64(30)
		}
	}
	if !found || sheet["weight_total"] != float64(20) || sheet["can_edit"] != true {
		t.Fatalf("sheet: %v", sheet)
	}
	progress := s.teacher.call("GET", "/classes/"+s.classID+"/subjects?term_id="+s.term1, nil).expect(http.StatusOK).list()
	for _, row := range progress {
		if row["id"] == s.math && (row["missing_marks"] != float64(1) || row["weight_total"] != float64(20) || row["can_edit"] != true) {
			t.Fatalf("progress: %v", row)
		}
		if row["id"] == s.english && row["can_edit"] != true {
			t.Fatalf("open subject: %v", row)
		}
	}

	// A correction is logged with both values; a first mark is not a correction.
	s.mark(s.teacher, s.math, s.term1, quiz, s.studentID, 35).expect(http.StatusOK)
	var logged string
	if err := testDB.QueryRow(context.Background(), `SELECT changes->>'corrections' FROM audit_logs WHERE action='marks.saved' AND entity_id=$1 ORDER BY created_at DESC LIMIT 1`, s.classID).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged, `"from": 30`) || !strings.Contains(logged, `"to": 35`) {
		t.Fatalf("correction log: %s", logged)
	}

	// Lowering a maximum below a mark is refused, and a blank clears a mark.
	s.teacher.call("PUT", "/marks/items/"+quiz, map[string]any{"title": "Quiz", "weight": 20, "max_score": 20}).expect(http.StatusBadRequest)
	s.teacher.call("PUT", "/marks/items/"+quiz, map[string]any{"title": "Quiz 1", "weight": 25, "max_score": 40}).expect(http.StatusOK)
	s.teacher.call("DELETE", "/marks/items/"+quiz, nil).expect(http.StatusConflict)
	s.mark(s.teacher, s.math, s.term1, quiz, s.studentID, nil).expect(http.StatusOK)
	s.teacher.call("DELETE", "/marks/items/"+quiz, nil).expect(http.StatusOK)

	withMark := s.item(s.teacher, s.math, s.term1, "Test", 10, 10)
	s.mark(s.teacher, s.math, s.term1, withMark, s.studentID, 5).expect(http.StatusOK)
	s.teacher.call("DELETE", "/marks/items/"+withMark+"?with_marks=true", nil).expect(http.StatusOK)

	// A term from another year cannot be used.
	otherClass := createClass(s.admin)
	s.admin.call("POST", "/class-subjects", map[string]any{"class_ids": []string{otherClass}, "names": []string{"Art"}}).expect(http.StatusCreated)
	art := s.admin.call("GET", "/classes/"+otherClass+"/subjects", nil).expect(http.StatusOK).list()[0]["id"].(string)
	s.admin.call("POST", "/marks/items", map[string]any{"class_subject_id": art, "term_id": s.term1, "title": "X", "weight": 5, "max_score": 5}).expect(http.StatusBadRequest)
	s.admin.call("GET", "/marks?class_subject_id="+art+"&term_id="+s.term1, nil).expect(http.StatusBadRequest)
}

func cardFor(t *testing.T, cards []any, studentID string) map[string]any {
	t.Helper()
	for _, raw := range cards {
		if card := raw.(map[string]any); card["student_id"] == studentID {
			return card
		}
	}
	t.Fatalf("no card for %s in %v", studentID, cards)
	return nil
}

func TestReportCardsFromMarksToPublishing(t *testing.T) {
	s := setupReports(t)
	admin, teacher := s.admin, s.teacher
	path := "/report-cards/classes/" + s.classID
	report := func(term string) map[string]any {
		return teacher.call("GET", path+"?term_id="+term, nil).expect(http.StatusOK).object()
	}
	problemKinds := func(data map[string]any) string {
		kinds := []string{}
		for _, raw := range data["problems"].([]any) {
			kinds = append(kinds, raw.(map[string]any)["kind"].(string))
		}
		return strings.Join(kinds, ",")
	}

	// Nothing is ready yet.
	if kinds := problemKinds(report(s.term1)); kinds != "no_items,no_items" {
		t.Fatalf("problems: %s", kinds)
	}
	admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1, "allow_incomplete": true}).expect(422)

	quiz := s.item(teacher, s.math, s.term1, "Quiz", 20, 40)
	final := s.item(teacher, s.math, s.term1, "Final", 70, 70)
	essay := s.item(admin, s.english, s.term1, "Essay", 100, 100)

	// Weights short of 100 stop publishing outright.
	stopped := admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1, "allow_incomplete": true})
	stopped.expect(422)
	if stopped.errorCode() != "REPORT_NOT_READY" || !strings.Contains(string(stopped.Body), `"weights"`) {
		t.Fatalf("refusal: %s", stopped.Body)
	}
	project := s.item(teacher, s.math, s.term1, "Project", 10, 10)

	// Hanna: Math 20/40->10 + 70/70->70 + 10/10->10 = 90, English 80. Second: Math blank.
	s.mark(teacher, s.math, s.term1, quiz, s.studentID, 20).expect(http.StatusOK)
	s.mark(teacher, s.math, s.term1, final, s.studentID, 70).expect(http.StatusOK)
	s.mark(teacher, s.math, s.term1, project, s.studentID, 10).expect(http.StatusOK)
	s.mark(admin, s.english, s.term1, essay, s.studentID, 80).expect(http.StatusOK)
	s.mark(admin, s.english, s.term1, essay, s.secondID, 50).expect(http.StatusOK)

	data := report(s.term1)
	if problemKinds(data) != "missing_marks" {
		t.Fatalf("problems: %v", data["problems"])
	}
	first := cardFor(t, data["cards"].([]any), s.studentID)
	if first["average"] != float64(85) || first["total"] != float64(170) || first["grade"] != "B" || first["rank"] != float64(1) || first["students_ranked"] != float64(1) {
		t.Fatalf("first card: %v", first)
	}
	second := cardFor(t, data["cards"].([]any), s.secondID)
	if second["average"] != nil || second["rank"] != nil || second["complete"] != false {
		t.Fatalf("a student with missing marks has no average or rank: %v", second)
	}

	// Attendance within the term's dates reaches the card; other days do not.
	admin.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": "2026-09-05", "records": []map[string]any{{"student_id": s.studentID, "status": "absent"}}}).expect(http.StatusOK)
	admin.call("PUT", "/attendance/classes/"+s.classID, map[string]any{"date": "2026-09-06", "records": []map[string]any{{"student_id": s.studentID, "status": "present"}}}).expect(http.StatusOK)

	// Notes.
	teacher.call("PUT", path+"/notes", map[string]any{"term_id": s.term1, "notes": []map[string]any{{"student_id": s.studentID, "conduct": "Z", "comment": ""}}}).expect(http.StatusBadRequest)
	teacher.call("PUT", path+"/notes", map[string]any{"term_id": s.term1, "notes": []map[string]any{{"student_id": s.studentID, "conduct": "A", "comment": "Works hard"}}}).expect(http.StatusOK)

	// Missing marks need confirming, and teachers cannot publish.
	teacher.call("POST", path+"/publish", map[string]any{"term_id": s.term1, "allow_incomplete": true}).expect(http.StatusForbidden)
	admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1}).expect(422)
	s.learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK)
	if cards := s.learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK).list(); len(cards) != 0 {
		t.Fatalf("students see nothing before publishing: %v", cards)
	}
	for _, id := range []string{quiz, final, project} {
		s.mark(teacher, s.math, s.term1, id, s.secondID, 0).expect(http.StatusOK)
	}
	admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1}).expect(http.StatusOK)
	admin.call("POST", path+"/publish", map[string]any{"term_id": s.term1}).expect(http.StatusConflict)

	// Published means locked.
	s.mark(teacher, s.math, s.term1, quiz, s.studentID, 1).expect(http.StatusConflict)
	teacher.call("PUT", path+"/notes", map[string]any{"term_id": s.term1, "notes": []map[string]any{{"student_id": s.studentID, "conduct": "B", "comment": ""}}}).expect(http.StatusConflict)
	admin.call("POST", "/marks/items", map[string]any{"class_subject_id": s.english, "term_id": s.term1, "title": "Late", "weight": 1, "max_score": 1}).expect(http.StatusConflict)
	teacher.call("PUT", "/marks/items/"+quiz, map[string]any{"title": "Quiz", "weight": 20, "max_score": 40}).expect(http.StatusConflict)
	teacher.call("DELETE", "/marks/items/"+quiz, nil).expect(http.StatusConflict)
	if sheet := teacher.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusOK).object(); sheet["can_edit"] != false || sheet["published"] != true {
		t.Fatalf("sheet: %v", sheet)
	}

	// The student sees their own card and only theirs.
	cards := s.learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK).list()
	if len(cards) != 1 || cards[0]["student_id"] != s.studentID || cards[0]["conduct"] != "A" || cards[0]["comment"] != "Works hard" {
		t.Fatalf("student cards: %v", cards)
	}
	if attendance := cards[0]["attendance"].(map[string]any); attendance["days_marked"] != float64(2) || attendance["absent"] != float64(1) {
		t.Fatalf("attendance: %v", attendance)
	}
	if cards[0]["average"] != float64(85) || cards[0]["rank"] != float64(1) {
		t.Fatalf("card: %v", cards[0])
	}

	// An issued card does not change when the rules or the class do.
	admin.call("PUT", "/report-settings", map[string]any{"pass_percent": 40, "grade_bands": []map[string]any{{"label": "P", "min_percent": 40}, {"label": "F", "min_percent": 0}}}).expect(http.StatusOK)
	if card := cardFor(t, report(s.term1)["cards"].([]any), s.studentID); card["grade"] != "B" {
		t.Fatalf("a published grade changed: %v", card["grade"])
	}
	admin.call("PUT", "/report-settings", map[string]any{"pass_percent": 50, "grade_bands": []map[string]any{{"label": "A", "min_percent": 90}, {"label": "B", "min_percent": 80}, {"label": "F", "min_percent": 0}}}).expect(http.StatusOK)

	// The second term brings in the first, with a year average and rank.
	exam := s.item(teacher, s.math, s.term2, "Exam", 100, 100)
	essay2 := s.item(admin, s.english, s.term2, "Essay", 100, 100)
	s.mark(teacher, s.math, s.term2, exam, s.studentID, 70).expect(http.StatusOK)
	s.mark(admin, s.english, s.term2, essay2, s.studentID, 100).expect(http.StatusOK)
	s.mark(teacher, s.math, s.term2, exam, s.secondID, 100).expect(http.StatusOK)
	s.mark(admin, s.english, s.term2, essay2, s.secondID, 90).expect(http.StatusOK)
	next := report(s.term2)
	card := cardFor(t, next["cards"].([]any), s.studentID)
	if card["average"] != float64(85) || card["rank"] != float64(2) {
		t.Fatalf("second term: %v", card)
	}
	if card["year_average"] != float64(85) || card["year_rank"] != float64(1) {
		t.Fatalf("year figures: %v", card)
	}
	earlier := card["earlier_terms"].([]any)
	if len(earlier) != 1 || earlier[0].(map[string]any)["average"] != float64(85) || earlier[0].(map[string]any)["name"] != "Semester 1" {
		t.Fatalf("earlier terms: %v", earlier)
	}
	for _, raw := range card["subjects"].([]any) {
		line := raw.(map[string]any)
		if line["subject_id"] == s.math && (line["year_average"] != float64(80) || line["earlier"].(map[string]any)[s.term1] != float64(90)) {
			t.Fatalf("subject year: %v", line)
		}
	}
	admin.call("POST", path+"/publish", map[string]any{"term_id": s.term2}).expect(http.StatusOK)
	if cards := s.learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK).list(); len(cards) != 2 || cards[0]["term_name"] != "Semester 2" {
		t.Fatalf("newest first: %v", cards)
	}

	// Taking reports back needs a reason, runs latest first, and is logged.
	teacher.call("POST", path+"/unpublish", map[string]any{"term_id": s.term2, "reason": "Wrong marks"}).expect(http.StatusForbidden)
	admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term2, "reason": ""}).expect(http.StatusBadRequest)
	later := admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term1, "reason": "Wrong marks"})
	later.expect(http.StatusConflict)
	if later.errorCode() != "LATER_TERM_PUBLISHED" {
		t.Fatalf("code %s", later.errorCode())
	}
	admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term2, "reason": "Wrong marks"}).expect(http.StatusOK)
	admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term2, "reason": "Wrong marks"}).expect(http.StatusConflict)
	admin.call("POST", path+"/unpublish", map[string]any{"term_id": s.term1, "reason": "Wrong marks"}).expect(http.StatusOK)
	if cards := s.learner.call("GET", "/student/report-cards", nil).expect(http.StatusOK).list(); len(cards) != 0 {
		t.Fatalf("students stop seeing withdrawn reports: %v", cards)
	}
	s.mark(teacher, s.math, s.term1, quiz, s.studentID, 40).expect(http.StatusOK)
	var reason string
	if err := testDB.QueryRow(context.Background(), `SELECT changes->>'reason' FROM audit_logs WHERE action='reports.unpublished' AND entity_id=$1 LIMIT 1`, s.classID).Scan(&reason); err != nil || reason != "Wrong marks" {
		t.Fatalf("logged reason %q: %v", reason, err)
	}
}

func TestIgnoredSubjectAndRankTies(t *testing.T) {
	s := setupReports(t)
	admin := s.admin
	// A subject outside the average still shows a grade but cannot hold up the rank.
	admin.call("PUT", "/class-subjects/"+s.english, map[string]any{"name": "English", "teacher_id": "", "counts_in_average": false}).expect(http.StatusOK)
	exam := s.item(admin, s.math, s.term1, "Exam", 100, 100)
	s.item(admin, s.english, s.term1, "Essay", 100, 100)
	s.mark(admin, s.math, s.term1, exam, s.studentID, 77.5).expect(http.StatusOK)
	s.mark(admin, s.math, s.term1, exam, s.secondID, 77.5).expect(http.StatusOK)
	cards := admin.call("GET", "/report-cards/classes/"+s.classID+"?term_id="+s.term1, nil).expect(http.StatusOK).object()["cards"].([]any)
	for _, id := range []string{s.studentID, s.secondID} {
		card := cardFor(t, cards, id)
		if card["average"] != 77.5 || card["rank"] != float64(1) || card["complete"] != true {
			t.Fatalf("tied students share a rank: %v", card)
		}
	}
}

func TestReportsStayInsideTheSchool(t *testing.T) {
	s := setupReports(t)
	quiz := s.item(s.admin, s.math, s.term1, "Quiz", 100, 100)
	s.mark(s.admin, s.math, s.term1, quiz, s.studentID, 50).expect(http.StatusOK)
	_, other := seedSchool(t, s.server)
	path := "/report-cards/classes/" + s.classID
	other.call("GET", path+"?term_id="+s.term1, nil).expect(http.StatusForbidden)
	other.call("POST", path+"/publish", map[string]any{"term_id": s.term1, "allow_incomplete": true}).expect(http.StatusForbidden)
	other.call("GET", "/classes/"+s.classID+"/subjects", nil).expect(http.StatusForbidden)
	other.call("GET", "/marks?class_subject_id="+s.math+"&term_id="+s.term1, nil).expect(http.StatusNotFound)
	other.call("PUT", "/marks", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "marks": []map[string]any{{"student_id": s.studentID, "item_id": quiz, "score": 1}}}).expect(http.StatusNotFound)
	other.call("PUT", "/marks/items/"+quiz, map[string]any{"title": "X", "weight": 5, "max_score": 5}).expect(http.StatusNotFound)
	other.call("DELETE", "/marks/items/"+quiz, nil).expect(http.StatusNotFound)
	other.call("PUT", "/class-subjects/"+s.math, map[string]any{"name": "X", "teacher_id": "", "counts_in_average": true}).expect(http.StatusNotFound)
	other.call("DELETE", "/class-subjects/"+s.english, nil).expect(http.StatusNotFound)
	other.call("PUT", "/terms/"+s.term1, map[string]string{"name": "X", "starts_on": "2026-09-01", "ends_on": "2026-09-02"}).expect(http.StatusNotFound)
	other.call("DELETE", "/terms/"+s.term1, nil).expect(http.StatusNotFound)
	if rows := other.call("GET", "/terms", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("another school's terms must not appear: %v", rows)
	}
	other.call("POST", "/class-subjects", map[string]any{"class_ids": []string{s.classID}, "names": []string{"X"}}).expect(http.StatusBadRequest)
	other.call("POST", "/terms", map[string]string{"academic_year_id": s.yearID, "name": "X", "starts_on": "2026-09-01", "ends_on": "2026-09-02"}).expect(http.StatusBadRequest)
}
