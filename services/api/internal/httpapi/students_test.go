package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEthiopianDateToISO(t *testing.T) {
	for value, want := range map[string]string{"2016-01-01": "2023-09-12", "2015-13-06": "2023-09-11", "2017-06-23": "2025-03-02", "1992-04-22": "2000-01-01", "2019-02-23": "2026-11-02"} {
		if got, ok := ethiopianDateToISO(value); !ok || got != want {
			t.Errorf("%s: got %s, %v, want %s", value, got, ok, want)
		}
	}
	for _, value := range []string{"", "2016", "2016-14-01", "2016-01-31", "2016-13-06", "2016-13-00", "abc-01-01"} {
		if got, ok := ethiopianDateToISO(value); ok {
			t.Errorf("%q should be refused, got %s", value, got)
		}
	}
}

func TestAttendanceAlertRules(t *testing.T) {
	if got := absenceStreak([]string{"absent", "excused", "absent", "present", "absent"}); got != 2 {
		t.Fatalf("an excused day should not end a streak, and a present one should: %d", got)
	}
	if got := absenceStreak([]string{"late", "absent", "absent"}); got != 0 {
		t.Fatalf("coming in late ends a streak: %d", got)
	}
	for _, c := range []struct {
		marked, absent, streak int
		want                   string
	}{{3, 3, 3, "streak"}, {20, 3, 0, "rate"}, {30, 3, 0, "rate"}, {31, 3, 0, ""}, {4, 2, 2, ""}, {5, 2, 0, ""}, {4, 3, 0, ""}, {20, 0, 0, ""}} {
		if got := attendanceAlertReason(c.marked, c.absent, c.streak); got != c.want {
			t.Errorf("%+v: got %q", c, got)
		}
	}
}

func TestParseStudentCSV(t *testing.T) {
	rows, rowErrors, message := parseStudentCSV(strings.NewReader("\xef\xbb\xbfstudent_number,full_name,grade,section,date_of_birth_ec,gender,stream,guardian_name,guardian_phone,guardian_relationship\nS1,Hanna Bekele,Grade 12,A,2001-03-15,Female,Natural,Almaz Tesfaye,0911 234 567,Mother\n"))
	if message != "" || len(rowErrors) != 0 || len(rows) != 1 {
		t.Fatalf("message=%q errors=%v rows=%d", message, rowErrors, len(rows))
	}
	if x := rows[0]; x.gender != "female" || x.stream != "natural" || x.birth == "" || x.guardian == nil || x.guardian.Relationship != "mother" {
		t.Fatalf("parsed wrongly: %+v", x)
	}
	_, rowErrors, _ = parseStudentCSV(strings.NewReader("student_number,full_name,gender,grade,section,guardian_phone,guardian_name\nS1,A,,,,,\nS1,B,,,,,\n,C,,,,,\nS3,,,,,,\nS4,D,other,,,,\nS5,E,,9,,,\nS6,F,,,,abc,Someone\n"))
	if len(rowErrors) != 6 {
		t.Fatalf("want six bad rows, got %v", rowErrors)
	}
	if _, _, message = parseStudentCSV(strings.NewReader("name\nx\n")); !strings.Contains(message, "student_number") {
		t.Fatalf("a missing column should be named: %q", message)
	}
}

func recordsFixture(t *testing.T) (admin, teacher, learner *apiClient, classID, studentID string, other *apiClient) {
	t.Helper()
	server, adminClient, teacherClient, learnerClient, class, student := prepFixture(t)
	_, otherTeacher := inviteTeacher(server, adminClient)
	return adminClient, teacherClient, learnerClient, class, student, otherTeacher
}

func TestStudentListSearchesAndPages(t *testing.T) {
	admin, _, _, classID, _, _ := recordsFixture(t)
	other := createClass(admin)
	for i := 0; i < 30; i++ {
		admin.create("/students", map[string]string{"student_number": fmt.Sprintf("P%03d", i), "legal_name": fmt.Sprintf("Paged Student %03d", i), "class_id": other})
	}
	admin.create("/students", map[string]string{"student_number": "Z100%", "legal_name": "Zed 100% Learner", "class_id": classID})

	page := admin.call("GET", "/students?page=2&page_size=10&q=paged", nil).expect(http.StatusOK)
	if rows := page.list(); len(rows) != 10 || rows[0]["student_number"] != "P010" {
		t.Fatalf("page two should start at the eleventh match: %v", rows)
	}
	if total := page.envelope()["total"]; total != float64(30) {
		t.Fatalf("total: %v", total)
	}
	if rows := admin.call("GET", "/students?page=1&class_id="+other+"&q=P029", nil).expect(http.StatusOK).list(); len(rows) != 1 || rows[0]["grade_level"] != "8" || rows[0]["class_id"] != other {
		t.Fatalf("search by number within a class: %v", rows)
	}
	if rows := admin.call("GET", "/students?page=1&q=%25", nil).expect(http.StatusOK).list(); len(rows) != 1 || rows[0]["student_number"] != "Z100%" {
		t.Fatalf("a percent sign is a literal search, not a wildcard: %v", rows)
	}
	if rows := admin.call("GET", "/students", nil).expect(http.StatusOK).list(); len(rows) != 32 {
		t.Fatalf("without a page the list is complete: %d", len(rows))
	}
	admin.call("GET", "/students?page=0", nil).expect(http.StatusBadRequest)
	admin.call("GET", "/students?page=1&page_size=500", nil).expect(http.StatusBadRequest)
	admin.call("GET", "/students?class_id=nope", nil).expect(http.StatusBadRequest)
}

func TestStudentProfileAndGuardians(t *testing.T) {
	admin, teacher, learner, classID, studentID, otherTeacher := recordsFixture(t)
	path := "/students/" + studentID
	profile := map[string]any{"legal_name": "Hanna Bekele", "date_of_birth": "2008-05-20", "gender": "female", "address": "Bole, Addis Ababa", "medical_notes": "Asthma", "status": "active"}
	teacher.call("PUT", path+"/profile", profile).expect(http.StatusForbidden)
	admin.call("PUT", path+"/profile", map[string]any{"legal_name": "", "status": "active"}).expect(http.StatusBadRequest)
	admin.call("PUT", path+"/profile", map[string]any{"legal_name": "X", "status": "active", "date_of_birth": "2999-01-01"}).expect(http.StatusBadRequest)
	admin.call("PUT", path+"/profile", map[string]any{"legal_name": "X", "status": "active", "gender": "other"}).expect(http.StatusBadRequest)
	admin.call("PUT", path+"/profile", profile).expect(http.StatusOK)

	guardian := func(name string, primary bool) map[string]any {
		return map[string]any{"full_name": name, "relationship": "mother", "phone": "+251 911 234 567", "email": "", "is_primary": primary}
	}
	admin.call("POST", path+"/guardians", map[string]any{"full_name": "", "relationship": "mother", "phone": "0911234567"}).expect(http.StatusBadRequest)
	admin.call("POST", path+"/guardians", map[string]any{"full_name": "A", "relationship": "aunt", "phone": "0911234567"}).expect(http.StatusBadRequest)
	admin.call("POST", path+"/guardians", map[string]any{"full_name": "A", "relationship": "mother", "phone": "call me"}).expect(http.StatusBadRequest)
	teacher.call("POST", path+"/guardians", guardian("Nope", false)).expect(http.StatusForbidden)
	admin.create(path+"/guardians", guardian("First", false))
	second := admin.create(path+"/guardians", guardian("Second", false))

	primaryName := func() string {
		for _, raw := range admin.call("GET", path+"/profile", nil).expect(http.StatusOK).object()["guardians"].([]any) {
			if row := raw.(map[string]any); row["is_primary"] == true {
				return row["full_name"].(string)
			}
		}
		return ""
	}
	if got := primaryName(); got != "First" {
		t.Fatalf("the first guardian should become primary, got %q", got)
	}
	admin.call("PUT", path+"/guardians/"+second, guardian("Second", true)).expect(http.StatusOK)
	if got := primaryName(); got != "Second" {
		t.Fatalf("a new primary replaces the old one, got %q", got)
	}
	// The primary contact cannot be switched off without a replacement.
	admin.call("PUT", path+"/guardians/"+second, guardian("Second renamed", false)).expect(http.StatusOK)
	if got := primaryName(); got != "Second renamed" {
		t.Fatalf("there must always be a primary, got %q", got)
	}
	admin.call("DELETE", path+"/guardians/"+second, nil).expect(http.StatusOK)
	if got := primaryName(); got != "First" {
		t.Fatalf("deleting the primary should promote the other, got %q", got)
	}

	// An assigned teacher can read the profile; nobody else can.
	data := teacher.call("GET", path+"/profile", nil).expect(http.StatusOK).object()
	if student := data["student"].(map[string]any); student["medical_notes"] != "Asthma" || student["date_of_birth"] != "2008-05-20" {
		t.Fatalf("profile: %v", student)
	}
	otherTeacher.call("GET", path+"/profile", nil).expect(http.StatusNotFound)
	learner.call("GET", path+"/profile", nil).expect(http.StatusForbidden)

	// Medical details stay out of the audit log.
	var logged int
	if err := testDB.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE entity_id::text=$1 AND changes::text ILIKE '%asthma%'`, studentID).Scan(&logged); err != nil {
		t.Fatal(err)
	}
	if logged != 0 {
		t.Fatal("medical notes must not be logged")
	}

	// Moving class ends the old enrolment; the teacher of the old class loses access.
	newClass := createClass(admin)
	teacher.call("POST", path+"/move", map[string]string{"class_id": newClass}).expect(http.StatusForbidden)
	admin.call("POST", path+"/move", map[string]string{"class_id": "nope"}).expect(http.StatusBadRequest)
	admin.call("POST", path+"/move", map[string]string{"class_id": newClass}).expect(http.StatusOK)
	admin.call("POST", path+"/move", map[string]string{"class_id": newClass}).expect(http.StatusOK)
	var active int
	if err := testDB.QueryRow(context.Background(), `SELECT count(*) FROM enrollments WHERE student_id=$1 AND status='active'`, studentID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("a student should have one active class, got %d", active)
	}
	teacher.call("GET", path+"/profile", nil).expect(http.StatusNotFound)
	if got := admin.call("GET", path+"/profile", nil).expect(http.StatusOK).object()["student"].(map[string]any)["class_id"]; got != newClass {
		t.Fatalf("profile should show the new class: %v", got)
	}
	_ = classID
}

func TestStudentRecordsStayInsideTheSchool(t *testing.T) {
	server, admin, _, _, classID, studentID := prepFixture(t)
	guardianID := admin.create("/students/"+studentID+"/guardians", map[string]any{"full_name": "A", "relationship": "mother", "phone": "0911234567"})
	_, other := seedSchool(t, server)
	path := "/students/" + studentID
	other.call("GET", path+"/profile", nil).expect(http.StatusNotFound)
	other.call("PUT", path+"/profile", map[string]any{"legal_name": "X", "status": "active"}).expect(http.StatusNotFound)
	other.call("POST", path+"/guardians", map[string]any{"full_name": "A", "relationship": "mother", "phone": "0911234567"}).expect(http.StatusNotFound)
	other.call("PUT", path+"/guardians/"+guardianID, map[string]any{"full_name": "A", "relationship": "mother", "phone": "0911234567"}).expect(http.StatusNotFound)
	other.call("DELETE", path+"/guardians/"+guardianID, nil).expect(http.StatusNotFound)
	other.call("POST", path+"/move", map[string]string{"class_id": classID}).expect(http.StatusNotFound)
	otherClass := createClass(other)
	admin.call("POST", path+"/move", map[string]string{"class_id": otherClass}).expect(http.StatusBadRequest)
	if rows := other.call("GET", "/students?q=Test", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("another school's students must not appear: %v", rows)
	}
}

func TestStudentImport(t *testing.T) {
	admin, _, _, classID, _, _ := recordsFixture(t)
	var grade, section string
	if err := testDB.QueryRow(context.Background(), `SELECT grade_level,name FROM classes WHERE id=$1`, classID).Scan(&grade, &section); err != nil {
		t.Fatal(err)
	}
	header := "student_number,full_name,grade,section,date_of_birth,date_of_birth_ec,gender,stream,guardian_name,guardian_phone,guardian_relationship\n"
	good := fmt.Sprintf("A1,Abel Tadesse,%s,%s,2008-01-15,,male,natural,Tadesse Abebe,0911000001,father\nA2,Sara Kebede,,,,2001-03-15,female,,,,\n", grade, section)

	bad := admin.call("POST", "/students/import", rawCSV(header+good+"A3,Unknown Class,12,Z,,,,,,,\n"))
	bad.expect(422)
	if !strings.Contains(string(bad.Body), "No class 12") {
		t.Fatalf("the unknown class should be named: %s", bad.Body)
	}
	if rows := admin.call("GET", "/students?q=Abel", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("a failed import must save nothing: %v", rows)
	}
	created := admin.call("POST", "/students/import", rawCSV(header+good)).expect(http.StatusCreated).object()
	if created["imported"] != float64(2) {
		t.Fatalf("imported: %v", created)
	}
	rows := admin.call("GET", "/students?q=Abel", nil).expect(http.StatusOK).list()
	if len(rows) != 1 || rows[0]["class_id"] != classID || rows[0]["stream"] != "natural" {
		t.Fatalf("imported row: %v", rows)
	}
	profile := admin.call("GET", "/students/"+rows[0]["id"].(string)+"/profile", nil).expect(http.StatusOK).object()
	if guardians := profile["guardians"].([]any); len(guardians) != 1 || guardians[0].(map[string]any)["is_primary"] != true {
		t.Fatalf("guardian: %v", guardians)
	}
	sara := admin.call("GET", "/students?q=Sara", nil).expect(http.StatusOK).list()
	saraProfile := admin.call("GET", "/students/"+sara[0]["id"].(string)+"/profile", nil).expect(http.StatusOK).object()["student"].(map[string]any)
	if saraProfile["date_of_birth"] != "2008-11-24" {
		t.Fatalf("an Ethiopian birth date should convert: %v", saraProfile["date_of_birth"])
	}

	// A file with no classes or guardians at all is fine too.
	admin.call("POST", "/students/import", rawCSV("student_number,full_name\nN1,Names Only\nN2,Numbers Only\n")).expect(http.StatusCreated)
	// The same number again is refused outright.
	admin.call("POST", "/students/import", rawCSV(header+"A1,Someone Else,,,,,,,,,\n")).expect(422)
	// The same person under a new number is refused unless the school confirms.
	duplicate := header + "B9,Abel Tadesse,,,2008-01-15,,,,,,\n"
	refused := admin.call("POST", "/students/import", rawCSV(duplicate)).expect(422)
	if !strings.Contains(string(refused.Body), "may already be enrolled as A1") {
		t.Fatalf("a likely duplicate should be named: %s", refused.Body)
	}
	admin.call("POST", "/students/import?allow_duplicates=true", rawCSV(duplicate)).expect(http.StatusCreated)
}

func TestAttendanceRegister(t *testing.T) {
	server, admin, teacher, learner, classID, studentID := prepFixture(t)
	secondID, _ := enrollLearner(server, admin, classID)
	_, otherTeacher := inviteTeacher(server, admin)
	today := time.Now().UTC().Format("2006-01-02")
	path := "/attendance/classes/" + classID

	empty := teacher.call("GET", path+"?date="+today, nil).expect(http.StatusOK).object()
	if empty["taken"] != false || len(empty["students"].([]any)) != 2 {
		t.Fatalf("an untaken register: %v", empty)
	}
	mark := func(client *apiClient, date, id, status string) apiResponse {
		return client.call("PUT", path, map[string]any{"date": date, "records": []map[string]any{{"student_id": id, "status": status, "note": ""}}})
	}
	mark(teacher, today, studentID, "sleeping").expect(http.StatusBadRequest)
	mark(teacher, time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02"), studentID, "present").expect(http.StatusBadRequest)
	mark(teacher, "soon", studentID, "present").expect(http.StatusBadRequest)
	teacher.call("GET", path+"?date=soon", nil).expect(http.StatusBadRequest)
	mark(otherTeacher, today, studentID, "present").expect(http.StatusForbidden)
	otherTeacher.call("GET", path+"?date="+today, nil).expect(http.StatusForbidden)
	mark(learner, today, studentID, "present").expect(http.StatusForbidden)

	outsider := admin.create("/students", map[string]string{"student_number": unique("X"), "legal_name": "Outsider", "class_id": createClass(admin)})
	mark(teacher, today, outsider, "present").expect(http.StatusBadRequest)

	teacher.call("PUT", path, map[string]any{"date": today, "records": []map[string]any{{"student_id": studentID, "status": "absent", "note": "Sick"}, {"student_id": secondID, "status": "late", "note": ""}}}).expect(http.StatusOK)
	saved := teacher.call("GET", path+"?date="+today, nil).expect(http.StatusOK).object()
	if saved["taken"] != true {
		t.Fatalf("saved: %v", saved)
	}
	teacher.call("PUT", path, map[string]any{"date": today, "records": []map[string]any{{"student_id": studentID, "status": "absent"}, {"student_id": studentID, "status": "present"}}}).expect(http.StatusBadRequest)
	// Saving again corrects the mark rather than adding one.
	mark(teacher, today, secondID, "present").expect(http.StatusOK)
	var marks int
	if err := testDB.QueryRow(context.Background(), `SELECT count(*) FROM attendance_records WHERE student_id=$1`, secondID).Scan(&marks); err != nil {
		t.Fatal(err)
	}
	if marks != 1 {
		t.Fatalf("one mark per day, got %d", marks)
	}

	// Old registers need an administrator.
	old := time.Now().UTC().AddDate(0, 0, -(attendanceTeacherEditDays + 2)).Format("2006-01-02")
	locked := mark(teacher, old, studentID, "present")
	locked.expect(http.StatusForbidden)
	if locked.errorCode() != "ATTENDANCE_LOCKED" {
		t.Fatalf("code %s", locked.errorCode())
	}
	mark(admin, old, studentID, "excused").expect(http.StatusOK)

	// A student who moves stays on the old day's register, and only there.
	newClass := createClass(admin)
	admin.call("POST", "/students/"+secondID+"/move", map[string]string{"class_id": newClass}).expect(http.StatusOK)
	after := teacher.call("GET", path+"?date="+today, nil).expect(http.StatusOK).object()
	if len(after["students"].([]any)) != 2 {
		t.Fatalf("today's register keeps the student marked that day: %v", after)
	}
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	if rows := teacher.call("GET", path+"?date="+yesterday, nil).expect(http.StatusOK).object()["students"].([]any); len(rows) != 1 {
		t.Fatalf("another day shows only current students: %v", rows)
	}

	// The office's view, with someone to call.
	admin.create("/students/"+studentID+"/guardians", map[string]any{"full_name": "Almaz", "relationship": "mother", "phone": "0911234567"})
	teacher.call("GET", "/attendance/overview?date="+today, nil).expect(http.StatusForbidden)
	overview := admin.call("GET", "/attendance/overview?date="+today, nil).expect(http.StatusOK).object()
	absentees := overview["absentees"].([]any)
	if len(absentees) != 1 || absentees[0].(map[string]any)["guardian_phone"] != "0911234567" || absentees[0].(map[string]any)["note"] != "Sick" {
		t.Fatalf("absentees: %v", absentees)
	}
	foundClass := false
	for _, raw := range overview["classes"].([]any) {
		if row := raw.(map[string]any); row["class_id"] == classID {
			foundClass = row["absent"] == float64(1) && row["present"] == float64(1) && row["marked"] == float64(2)
		}
	}
	if !foundClass {
		t.Fatalf("class counts: %v", overview["classes"])
	}

	// The student's own view.
	own := learner.call("GET", "/student/attendance", nil).expect(http.StatusOK).object()
	if own["absent"] != float64(1) || len(own["recent"].([]any)) == 0 {
		t.Fatalf("student view: %v", own)
	}
	admin.call("GET", "/student/attendance", nil).expect(http.StatusForbidden)
}

func TestAttendanceAlertsAndReport(t *testing.T) {
	server, admin, teacher, _, classID, studentID := prepFixture(t)
	steadyID, _ := enrollLearner(server, admin, classID)
	_, otherTeacher := inviteTeacher(server, admin)
	path := "/attendance/classes/" + classID
	day := func(back int) string { return time.Now().UTC().AddDate(0, 0, -back).Format("2006-01-02") }
	// Three absences in a row for one student; the other attends throughout.
	for back := 1; back <= 6; back++ {
		status := "present"
		if back <= 3 {
			status = "absent"
		}
		admin.call("PUT", path, map[string]any{"date": day(back), "records": []map[string]any{{"student_id": studentID, "status": status}, {"student_id": steadyID, "status": "present"}}}).expect(http.StatusOK)
	}
	admin.create("/students/"+studentID+"/guardians", map[string]any{"full_name": "Almaz", "relationship": "mother", "phone": "0911234567"})

	alerts := admin.call("GET", "/attendance/alerts", nil).expect(http.StatusOK).list()
	if len(alerts) != 1 || alerts[0]["student_id"] != studentID || alerts[0]["reason"] != "streak" || alerts[0]["current_streak"] != float64(3) || alerts[0]["guardian_phone"] != "0911234567" {
		t.Fatalf("alerts: %v", alerts)
	}
	if rows := teacher.call("GET", "/attendance/alerts?class_id="+classID, nil).expect(http.StatusOK).list(); len(rows) != 1 {
		t.Fatalf("a teacher sees their class's alerts: %v", rows)
	}
	teacher.call("GET", "/attendance/alerts", nil).expect(http.StatusBadRequest)
	otherTeacher.call("GET", "/attendance/alerts?class_id="+classID, nil).expect(http.StatusForbidden)

	report := teacher.call("GET", "/attendance/report?class_id="+classID+"&from="+day(30)+"&to="+day(0), nil).expect(http.StatusOK).object()
	if report["days_taken"] != float64(6) {
		t.Fatalf("days taken: %v", report)
	}
	for _, raw := range report["students"].([]any) {
		row := raw.(map[string]any)
		if row["student_id"] == studentID && (row["absent"] != float64(3) || row["present"] != float64(3)) {
			t.Fatalf("report row: %v", row)
		}
	}
	narrow := admin.call("GET", "/attendance/report?class_id="+classID+"&from="+day(2)+"&to="+day(1), nil).expect(http.StatusOK).object()
	if narrow["days_taken"] != float64(2) {
		t.Fatalf("a date range should narrow the report: %v", narrow)
	}
	otherTeacher.call("GET", "/attendance/report?class_id="+classID+"&from="+day(2)+"&to="+day(1), nil).expect(http.StatusForbidden)
	admin.call("GET", "/attendance/report?class_id="+classID+"&from="+day(1)+"&to="+day(2), nil).expect(http.StatusBadRequest)
	admin.call("GET", "/attendance/report?class_id=nope&from="+day(1)+"&to="+day(0), nil).expect(http.StatusBadRequest)

	_, otherSchool := seedSchool(t, server)
	otherSchool.call("GET", path+"?date="+day(1), nil).expect(http.StatusForbidden)
	if rows := otherSchool.call("GET", "/attendance/alerts", nil).expect(http.StatusOK).list(); len(rows) != 0 {
		t.Fatalf("another school must not see alerts: %v", rows)
	}
	if overview := otherSchool.call("GET", "/attendance/overview?date="+day(1), nil).expect(http.StatusOK).object(); len(overview["absentees"].([]any)) != 0 {
		t.Fatalf("another school must not see absentees: %v", overview)
	}
}
