package httpapi

import (
	"context"
	"net/http"
	"testing"
)

func TestGradeLabels(t *testing.T) {
	for level, want := range map[string]string{"Grade 9": "Grade 10", "9": "10", "KG 2": "KG 3", "Grade 12 (Natural)": "Grade 13 (Natural)", "Nursery": ""} {
		if got := nextGradeLabel(level); got != want {
			t.Fatalf("%q: %q", level, got)
		}
	}
}

func TestYearEndRollover(t *testing.T) {
	s := setupReports(t)
	admin := s.admin
	top := admin.create("/classes", map[string]string{"name": "Section A", "grade_level": "12", "academic_year_id": s.yearID})
	senior := admin.create("/students", map[string]string{"student_number": unique("S"), "legal_name": "Senior Student", "class_id": top})
	next := admin.create("/academic-years", map[string]string{"name": unique("Next"), "starts_on": "2027-09-01", "ends_on": "2028-07-01"})
	copyPath := "/academic-years/" + s.yearID + "/copy"

	// Step 1: the next year gets the same classes, subjects, teachers and terms.
	s.teacher.call("POST", copyPath, map[string]any{"to_year_id": next}).expect(http.StatusForbidden)
	admin.call("POST", copyPath, map[string]any{"to_year_id": s.yearID}).expect(http.StatusBadRequest)
	copied := admin.call("POST", copyPath, map[string]any{"to_year_id": next, "teachers": true, "terms": true}).expect(http.StatusOK).object()
	if copied["classes_created"] != 2.0 || copied["subjects_created"] != 2.0 || copied["terms_created"] != 2.0 || copied["teachers_assigned"].(float64) < 2 {
		t.Fatalf("copy: %v", copied)
	}
	again := admin.call("POST", copyPath, map[string]any{"to_year_id": next, "teachers": true, "terms": true}).expect(http.StatusOK).object()
	if again["classes_created"] != 0.0 || again["classes_kept"] != 2.0 || again["subjects_created"] != 0.0 || again["terms_created"] != 0.0 {
		t.Fatalf("copying twice: %v", again)
	}
	var termStart, termEnd string
	if err := testDB.QueryRow(context.Background(), `SELECT starts_on::text,ends_on::text FROM academic_terms WHERE academic_year_id=$1 AND name='Semester 2'`, next).Scan(&termStart, &termEnd); err != nil || termStart != "2028-01-16" || termEnd != "2028-06-30" {
		t.Fatalf("term dates: %s %s %v", termStart, termEnd, err)
	}
	var mathTeacher string
	if err := testDB.QueryRow(context.Background(), `SELECT COALESCE(cs.teacher_id::text,'') FROM class_subjects cs JOIN classes c ON c.id=cs.class_id WHERE c.academic_year_id=$1 AND cs.name='Mathematics'`, next).Scan(&mathTeacher); err != nil || mathTeacher == "" {
		t.Fatalf("subject teacher not carried over: %v", err)
	}

	// A failing year average suggests repeating the grade.
	if _, err := testDB.Exec(context.Background(), `INSERT INTO report_publications(school_id,term_id,class_id,published_by)
		SELECT c.school_id,$1,$2,u.id FROM classes c JOIN users u ON u.school_id=c.school_id JOIN user_roles ur ON ur.user_id=u.id AND ur.role='school_admin' WHERE c.id=$2 LIMIT 1`, s.term2, s.classID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(context.Background(), `INSERT INTO report_cards(school_id,term_id,class_id,student_id,card) SELECT school_id,$1,$2,$3,'{"average":30,"year_average":38.5,"pass_percent":50}'::jsonb FROM classes WHERE id=$2`, s.term2, s.classID, s.secondID); err != nil {
		t.Fatal(err)
	}
	planPath := "/academic-years/" + s.yearID + "/promotion"
	plan := admin.call("GET", planPath+"?to="+next, nil).expect(http.StatusOK).object()
	classes := map[string]map[string]any{}
	for _, c := range plan["classes"].([]any) {
		classes[c.(map[string]any)["grade_level"].(string)] = c.(map[string]any)
	}
	eighth, twelfth := classes["8"], classes["12"]
	if eighth["promote_to"] != "" || eighth["create_suggested"].(map[string]any)["grade_level"] != "9" || eighth["repeat_in"] == "" || eighth["final_grade"] != false {
		t.Fatalf("grade 8: %v", eighth)
	}
	if twelfth["final_grade"] != true || twelfth["students"].([]any)[0].(map[string]any)["suggestion"] != "graduate" {
		t.Fatalf("grade 12: %v", twelfth)
	}
	suggestions := map[string]string{}
	for _, student := range eighth["students"].([]any) {
		suggestions[student.(map[string]any)["id"].(string)] = student.(map[string]any)["suggestion"].(string)
	}
	if suggestions[s.studentID] != "promote" || suggestions[s.secondID] != "repeat" {
		t.Fatalf("suggestions: %v", suggestions)
	}

	// The missing grade 9 class is created, and then suggested.
	ninth := admin.create("/classes", map[string]any{"name": eighth["name"], "grade_level": "9", "academic_year_id": next})
	plan = admin.call("GET", planPath+"?to="+next, nil).expect(http.StatusOK).object()
	for _, c := range plan["classes"].([]any) {
		if c.(map[string]any)["grade_level"] == "8" && c.(map[string]any)["promote_to"] != ninth {
			t.Fatalf("promote target: %v", c)
		}
	}

	// The year cannot close while students are still in it.
	closed := admin.call("POST", "/academic-years/"+s.yearID+"/close", nil)
	closed.expect(http.StatusConflict)
	if closed.errorCode() != "YEAR_HAS_STUDENTS" {
		t.Fatalf("code %s", closed.errorCode())
	}

	// Step 2: students move on.
	decisions := []map[string]string{
		{"student_id": s.studentID, "action": "promote", "class_id": ninth},
		{"student_id": s.secondID, "action": "repeat", "class_id": eighth["repeat_in"].(string)},
		{"student_id": senior, "action": "graduate"},
	}
	admin.call("POST", planPath, map[string]any{"to_year_id": next, "decisions": []map[string]string{{"student_id": s.studentID, "action": "promote"}}}).expect(http.StatusBadRequest)
	admin.call("POST", planPath, map[string]any{"to_year_id": next, "decisions": []map[string]string{{"student_id": s.studentID, "action": "promote", "class_id": s.classID}}}).expect(http.StatusBadRequest)
	moved := admin.call("POST", planPath, map[string]any{"to_year_id": next, "decisions": decisions}).expect(http.StatusOK).object()
	if moved["promoted"] != 1.0 || moved["repeated"] != 1.0 || moved["graduated"] != 1.0 {
		t.Fatalf("moved: %v", moved)
	}
	admin.call("POST", planPath, map[string]any{"to_year_id": next, "decisions": decisions[:1]}).expect(http.StatusConflict)
	var current, status, enrollmentStatus string
	if err := testDB.QueryRow(context.Background(), `SELECT class_id::text FROM enrollments WHERE student_id=$1 AND status='active'`, s.studentID).Scan(&current); err != nil || current != ninth {
		t.Fatalf("promoted student's class: %s %v", current, err)
	}
	if err := testDB.QueryRow(context.Background(), `SELECT s.status,e.status FROM students s JOIN enrollments e ON e.student_id=s.id WHERE s.id=$1`, senior).Scan(&status, &enrollmentStatus); err != nil || status != "graduated" || enrollmentStatus != "graduated" {
		t.Fatalf("graduate: %s %s %v", status, enrollmentStatus, err)
	}
	s.learner.call("GET", "/student/assessments", nil).expect(http.StatusOK)

	// Step 3: the old year closes and becomes read-only.
	admin.call("POST", "/academic-years/"+s.yearID+"/close", nil).expect(http.StatusOK)
	admin.call("POST", "/academic-years/"+s.yearID+"/close", nil).expect(http.StatusConflict)
	register := map[string]any{"date": "2027-05-04", "records": []map[string]string{{"student_id": s.studentID, "status": "present"}}}
	if code := admin.call("PUT", "/attendance/classes/"+s.classID, register).expect(http.StatusConflict).errorCode(); code != "YEAR_CLOSED" {
		t.Fatalf("register: %s", code)
	}
	if code := s.teacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Late quiz", "weight": 10, "max_score": 10}).expect(http.StatusConflict).errorCode(); code != "YEAR_CLOSED" {
		t.Fatalf("marks: %s", code)
	}
	if code := admin.call("PUT", "/report-cards/classes/"+s.classID+"/notes", map[string]any{"term_id": s.term1, "notes": []map[string]string{{"student_id": s.studentID, "conduct": "A", "comment": "Late"}}}).expect(http.StatusConflict).errorCode(); code != "YEAR_CLOSED" {
		t.Fatalf("notes: %s", code)
	}
	admin.call("POST", "/students", map[string]string{"student_number": unique("S"), "legal_name": "Late joiner", "class_id": s.classID}).expect(http.StatusBadRequest)
	admin.call("POST", planPath, map[string]any{"to_year_id": next, "decisions": decisions[:1]}).expect(http.StatusConflict)
	admin.call("POST", "/academic-years/"+next+"/copy", map[string]any{"to_year_id": s.yearID}).expect(http.StatusConflict)
	// Its records can still be read.
	admin.call("GET", "/attendance/classes/"+s.classID+"?date=2026-10-01", nil).expect(http.StatusOK)
	admin.call("GET", "/report-cards/classes/"+s.classID+"?term_id="+s.term1, nil).expect(http.StatusOK)

	if containsID(admin.call("GET", "/classes", nil).expect(http.StatusOK).list(), s.classID) {
		t.Fatal("a closed year's class is offered in pickers")
	}
	all := admin.call("GET", "/classes?all=1", nil).expect(http.StatusOK).list()
	for _, c := range all {
		if c["id"] == s.classID && c["year_closed"] != true {
			t.Fatalf("closed flag: %v", c)
		}
	}
	if !containsID(all, s.classID) || containsID(s.teacher.call("GET", "/teacher/classes", nil).expect(http.StatusOK).list(), s.classID) {
		t.Fatal("class lists after closing")
	}
	for _, year := range admin.call("GET", "/academic-years", nil).expect(http.StatusOK).list() {
		if year["id"] == s.yearID && (year["closed_at"] == "" || year["active_students"] != 0.0) {
			t.Fatalf("year: %v", year)
		}
		if year["id"] == next && year["active_students"] != 2.0 {
			t.Fatalf("next year: %v", year)
		}
	}

	// Reopening allows corrections again.
	_, other := seedSchool(t, s.server)
	other.call("DELETE", "/academic-years/"+s.yearID+"/close", nil).expect(http.StatusNotFound)
	other.call("POST", copyPath, map[string]any{"to_year_id": next}).expect(http.StatusNotFound)
	admin.call("DELETE", "/academic-years/"+s.yearID+"/close", nil).expect(http.StatusOK)
	s.teacher.call("POST", "/marks/items", map[string]any{"class_subject_id": s.math, "term_id": s.term1, "title": "Correction", "weight": 10, "max_score": 10}).expect(http.StatusCreated)
}
