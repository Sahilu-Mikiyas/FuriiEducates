package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
)

const (
	// A teacher can correct a register for this many days, after which it takes
	// an administrator, so records cannot quietly change long afterwards.
	attendanceTeacherEditDays = 14
	attendanceAlertWindowDays = 30
	// A student is flagged after this share of absences, once there is enough to
	// go on, or after this many absences in a row.
	attendanceAlertRate        = 0.10
	attendanceAlertMinDays     = 5
	attendanceAlertMinAbsences = 3
	attendanceAlertStreak      = 3
	maxAttendanceReportDays    = 400
)

var attendanceStatuses = []string{"present", "absent", "late", "excused"}

func (a *API) registerAttendanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/attendance/classes/{classID}", a.staffMember(a.getClassRegister))
	mux.HandleFunc("PUT /api/v1/attendance/classes/{classID}", a.staffMember(a.saveClassRegister))
	mux.HandleFunc("GET /api/v1/attendance/overview", a.admin(a.attendanceOverview))
	mux.HandleFunc("GET /api/v1/attendance/alerts", a.staffMember(a.attendanceAlerts))
	mux.HandleFunc("GET /api/v1/attendance/report", a.staffMember(a.attendanceReport))
	mux.HandleFunc("GET /api/v1/student/attendance", a.student(a.studentAttendance))
}

// parseAttendanceDate reads a day and refuses the future. It allows one day
// ahead of the server's date, because the school's day can start before UTC's.
func parseAttendanceDate(value string) (time.Time, bool) {
	day, err := time.Parse("2006-01-02", value)
	if err != nil || day.After(time.Now().UTC().AddDate(0, 0, 1)) {
		return time.Time{}, false
	}
	return day, true
}

func (a *API) requireClass(w http.ResponseWriter, r *http.Request, id auth.Identity, classID string) bool {
	allowed, err := a.canManageClass(r.Context(), id, classID)
	if err != nil {
		a.serverError(w, r, err)
		return false
	}
	if !allowed {
		writeError(w, 403, "FORBIDDEN", "You are not assigned to this class.", nil)
		return false
	}
	return true
}

func (a *API) getClassRegister(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if !a.requireClass(w, r, id, classID) {
		return
	}
	date := r.URL.Query().Get("date")
	if _, ok := parseAttendanceDate(date); !ok {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a date that is not in the future.", nil)
		return
	}
	// A student who has left still shows on a day they were marked, so an old
	// register stays complete.
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(m.status,''),COALESCE(m.note,'')
		FROM students s
		LEFT JOIN attendance_records m ON m.student_id=s.id AND m.class_id=$2 AND m.attendance_date=$3::date
		WHERE s.school_id=$1 AND (m.student_id IS NOT NULL OR (s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$2 AND e.status='active')))
		ORDER BY s.legal_name,s.student_number LIMIT 1000`, id.SchoolID, classID, date)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type row struct {
		StudentID string `json:"student_id"`
		Number    string `json:"student_number"`
		Name      string `json:"student_name"`
		Status    string `json:"status"`
		Note      string `json:"note"`
	}
	students := []row{}
	taken := false
	for rows.Next() {
		var x row
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Status, &x.Note); err != nil {
			a.serverError(w, r, err)
			return
		}
		taken = taken || x.Status != ""
		students = append(students, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"date": date, "taken": taken, "students": students}})
}

func (a *API) saveClassRegister(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if !a.requireClass(w, r, id, classID) || a.refuseClosedClass(w, r, id.SchoolID, classID) {
		return
	}
	var req struct {
		Date    string `json:"date"`
		Records []struct {
			StudentID string `json:"student_id"`
			Status    string `json:"status"`
			Note      string `json:"note"`
		} `json:"records"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	day, ok := parseAttendanceDate(req.Date)
	if !ok {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a date that is not in the future.", nil)
		return
	}
	if !hasRole(id.Roles, "school_admin") && day.Before(time.Now().UTC().AddDate(0, 0, -attendanceTeacherEditDays)) {
		writeError(w, 403, "ATTENDANCE_LOCKED", "This register is too old for a teacher to change. Ask an administrator.", nil)
		return
	}
	if len(req.Records) == 0 || len(req.Records) > 1000 {
		writeError(w, 400, "VALIDATION_ERROR", "Mark at least one student.", nil)
		return
	}
	studentIDs := make([]string, 0, len(req.Records))
	statuses := make([]string, 0, len(req.Records))
	notes := make([]string, 0, len(req.Records))
	for _, record := range req.Records {
		if !uuidPattern.MatchString(record.StudentID) || !slices.Contains(attendanceStatuses, record.Status) || len(record.Note) > 500 {
			writeError(w, 400, "VALIDATION_ERROR", "A mark could not be read.", nil)
			return
		}
		studentIDs = append(studentIDs, record.StudentID)
		statuses = append(statuses, record.Status)
		notes = append(notes, strings.TrimSpace(record.Note))
	}
	if len(slicesCompactSorted(studentIDs)) != len(studentIDs) {
		writeError(w, 400, "VALIDATION_ERROR", "A student was marked twice.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	// Only students in this class, or already marked in it that day, can be marked.
	var known int
	err = tx.QueryRow(r.Context(), `SELECT count(*) FROM students s WHERE s.id=ANY($1::uuid[]) AND s.school_id=$2 AND (
		EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$3 AND e.status='active')
		OR EXISTS(SELECT 1 FROM attendance_records m WHERE m.student_id=s.id AND m.class_id=$3 AND m.attendance_date=$4::date))`, studentIDs, id.SchoolID, classID, req.Date).Scan(&known)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if known != len(studentIDs) {
		writeError(w, 400, "VALIDATION_ERROR", "A marked student is not in this class.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO attendance_records(school_id,student_id,class_id,attendance_date,status,note,recorded_by)
		SELECT $1,m.student_id,$2,$3::date,m.status,m.note,$4 FROM unnest($5::uuid[],$6::text[],$7::text[]) AS m(student_id,status,note)
		ON CONFLICT (student_id,class_id,attendance_date) DO UPDATE SET status=EXCLUDED.status,note=EXCLUDED.note,recorded_by=EXCLUDED.recorded_by,updated_at=now()`,
		id.SchoolID, classID, req.Date, id.UserID, studentIDs, statuses, notes); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = a.noticeAbsences(r.Context(), tx, id.SchoolID, classID, req.Date, studentIDs, statuses); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'attendance.saved','class',$3,jsonb_build_object('date',$4::text,'count',$5::int))`, id.SchoolID, id.UserID, classID, req.Date, len(req.Records)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"date": req.Date, "saved": len(req.Records)}})
}

// attendanceOverview is the office's morning view: which registers are in, and
// who is absent with a number to call. Only unexplained absences are listed.
func (a *API) attendanceOverview(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	date := r.URL.Query().Get("date")
	if _, ok := parseAttendanceDate(date); !ok {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a date that is not in the future.", nil)
		return
	}
	classRows, err := a.db.Query(r.Context(), `SELECT c.id::text,c.name,c.grade_level,
		(SELECT count(*)::int FROM enrollments e JOIN students s ON s.id=e.student_id WHERE e.class_id=c.id AND e.status='active' AND s.status='active'),
		count(m.student_id)::int,count(*) FILTER (WHERE m.status='present')::int,count(*) FILTER (WHERE m.status='absent')::int,count(*) FILTER (WHERE m.status='late')::int,count(*) FILTER (WHERE m.status='excused')::int
		FROM classes c LEFT JOIN attendance_records m ON m.class_id=c.id AND m.attendance_date=$2::date
		WHERE c.school_id=$1 AND EXISTS(SELECT 1 FROM enrollments e WHERE e.class_id=c.id AND e.status='active')
		GROUP BY c.id,c.name,c.grade_level ORDER BY c.grade_level,c.name LIMIT 500`, id.SchoolID, date)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer classRows.Close()
	type classRow struct {
		ClassID  string `json:"class_id"`
		Name     string `json:"name"`
		Grade    string `json:"grade_level"`
		Students int    `json:"students"`
		Marked   int    `json:"marked"`
		Present  int    `json:"present"`
		Absent   int    `json:"absent"`
		Late     int    `json:"late"`
		Excused  int    `json:"excused"`
	}
	classes := []classRow{}
	for classRows.Next() {
		var x classRow
		if err = classRows.Scan(&x.ClassID, &x.Name, &x.Grade, &x.Students, &x.Marked, &x.Present, &x.Absent, &x.Late, &x.Excused); err != nil {
			a.serverError(w, r, err)
			return
		}
		classes = append(classes, x)
	}
	if err = classRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	classRows.Close()
	absentRows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,c.grade_level,c.name,m.note,COALESCE(g.full_name,''),COALESCE(g.relationship,''),COALESCE(g.phone,'')
		FROM attendance_records m JOIN students s ON s.id=m.student_id JOIN classes c ON c.id=m.class_id
		LEFT JOIN guardians g ON g.student_id=s.id AND g.is_primary
		WHERE m.school_id=$1 AND m.attendance_date=$2::date AND m.status='absent'
		ORDER BY c.grade_level,c.name,s.legal_name LIMIT 1000`, id.SchoolID, date)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer absentRows.Close()
	type absentee struct {
		StudentID    string `json:"student_id"`
		Number       string `json:"student_number"`
		Name         string `json:"student_name"`
		Grade        string `json:"grade_level"`
		ClassName    string `json:"class_name"`
		Note         string `json:"note"`
		Guardian     string `json:"guardian_name"`
		Relationship string `json:"guardian_relationship"`
		Phone        string `json:"guardian_phone"`
	}
	absentees := []absentee{}
	for absentRows.Next() {
		var x absentee
		if err = absentRows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Grade, &x.ClassName, &x.Note, &x.Guardian, &x.Relationship, &x.Phone); err != nil {
			a.serverError(w, r, err)
			return
		}
		absentees = append(absentees, x)
	}
	if err = absentRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"date": date, "classes": classes, "absentees": absentees}})
}

// absenceStreak counts the absences at the start of a list ordered newest first.
// An excused day neither adds to a streak nor ends one, because the student was
// away for a known reason. Any day attended ends it.
func absenceStreak(statuses []string) int {
	streak := 0
	for _, status := range statuses {
		switch status {
		case "absent":
			streak++
		case "excused":
		default:
			return streak
		}
	}
	return streak
}

// attendanceAlertReason says why a student needs follow-up, or "" if they do not.
func attendanceAlertReason(marked, absent, streak int) string {
	if streak >= attendanceAlertStreak {
		return "streak"
	}
	if marked >= attendanceAlertMinDays && absent >= attendanceAlertMinAbsences && float64(absent)/float64(marked) >= attendanceAlertRate {
		return "rate"
	}
	return ""
}

// attendanceAlerts lists students with repeated unexplained absence in the last
// 30 days. Administrators see the school; teachers must name one of their classes.
func (a *API) attendanceAlerts(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.URL.Query().Get("class_id")
	if classID != "" && !uuidPattern.MatchString(classID) {
		writeError(w, 400, "VALIDATION_ERROR", "A class could not be read.", nil)
		return
	}
	if classID == "" && !hasRole(id.Roles, "school_admin") {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	if classID != "" && !a.requireClass(w, r, id, classID) {
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,c.grade_level,c.name,COALESCE(g.full_name,''),COALESCE(g.phone,''),
		array_agg(m.status ORDER BY m.attendance_date DESC)
		FROM attendance_records m JOIN students s ON s.id=m.student_id AND s.status='active'
		JOIN enrollments e ON e.student_id=s.id AND e.class_id=m.class_id AND e.status='active'
		JOIN classes c ON c.id=m.class_id
		LEFT JOIN guardians g ON g.student_id=s.id AND g.is_primary
		WHERE m.school_id=$1 AND m.attendance_date>=CURRENT_DATE-$2::int AND ($3='' OR m.class_id=$3::uuid)
		GROUP BY s.id,s.student_number,s.legal_name,c.grade_level,c.name,g.full_name,g.phone
		LIMIT 5000`, id.SchoolID, attendanceAlertWindowDays, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type alert struct {
		StudentID string `json:"student_id"`
		Number    string `json:"student_number"`
		Name      string `json:"student_name"`
		Grade     string `json:"grade_level"`
		ClassName string `json:"class_name"`
		Guardian  string `json:"guardian_name"`
		Phone     string `json:"guardian_phone"`
		Marked    int    `json:"days_marked"`
		Absent    int    `json:"days_absent"`
		Streak    int    `json:"current_streak"`
		Reason    string `json:"reason"`
	}
	alerts := []alert{}
	for rows.Next() {
		var x alert
		var statuses []string
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Grade, &x.ClassName, &x.Guardian, &x.Phone, &statuses); err != nil {
			a.serverError(w, r, err)
			return
		}
		x.Marked = len(statuses)
		for _, status := range statuses {
			if status == "absent" {
				x.Absent++
			}
		}
		x.Streak = absenceStreak(statuses)
		x.Reason = attendanceAlertReason(x.Marked, x.Absent, x.Streak)
		if x.Reason != "" {
			alerts = append(alerts, x)
		}
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	slices.SortFunc(alerts, func(p, q alert) int {
		if p.Streak != q.Streak {
			return q.Streak - p.Streak
		}
		return q.Absent - p.Absent
	})
	writeJSON(w, 200, map[string]any{"data": alerts})
}

// attendanceReport totals a class over a period, for monthly reports. A day
// counts only if the class's register was taken.
func (a *API) attendanceReport(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.URL.Query().Get("class_id")
	if !uuidPattern.MatchString(classID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	from, okFrom := parseAttendanceDate(r.URL.Query().Get("from"))
	to, okTo := parseAttendanceDate(r.URL.Query().Get("to"))
	if !okFrom || !okTo || to.Before(from) || to.Sub(from).Hours()/24 > maxAttendanceReportDays {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a start and end date, with the end not before the start.", nil)
		return
	}
	if !a.requireClass(w, r, id, classID) {
		return
	}
	var daysTaken int
	if err := a.db.QueryRow(r.Context(), `SELECT count(DISTINCT attendance_date)::int FROM attendance_records WHERE school_id=$1 AND class_id=$2 AND attendance_date BETWEEN $3 AND $4`, id.SchoolID, classID, from, to).Scan(&daysTaken); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,
		count(*) FILTER (WHERE m.status='present')::int,count(*) FILTER (WHERE m.status='absent')::int,count(*) FILTER (WHERE m.status='late')::int,count(*) FILTER (WHERE m.status='excused')::int
		FROM students s LEFT JOIN attendance_records m ON m.student_id=s.id AND m.class_id=$2 AND m.attendance_date BETWEEN $3 AND $4
		WHERE s.school_id=$1 AND (m.student_id IS NOT NULL OR (s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$2 AND e.status='active')))
		GROUP BY s.id,s.student_number,s.legal_name ORDER BY s.legal_name LIMIT 1000`, id.SchoolID, classID, from, to)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type row struct {
		StudentID string `json:"student_id"`
		Number    string `json:"student_number"`
		Name      string `json:"student_name"`
		Present   int    `json:"present"`
		Absent    int    `json:"absent"`
		Late      int    `json:"late"`
		Excused   int    `json:"excused"`
	}
	students := []row{}
	for rows.Next() {
		var x row
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.Present, &x.Absent, &x.Late, &x.Excused); err != nil {
			a.serverError(w, r, err)
			return
		}
		students = append(students, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"days_taken": daysTaken, "students": students}})
}

type attendanceSummaryData struct {
	Days    int `json:"days_marked"`
	Present int `json:"present"`
	Absent  int `json:"absent"`
	Late    int `json:"late"`
	Excused int `json:"excused"`
	Recent  []struct {
		Date   string `json:"date"`
		Status string `json:"status"`
		Note   string `json:"note"`
	} `json:"recent"`
}

// attendanceSummary is one student's last 30 days, with recent days away or late.
func (a *API) attendanceSummary(ctx context.Context, schoolID, studentID string) (attendanceSummaryData, error) {
	var summary attendanceSummaryData
	err := a.db.QueryRow(ctx, `SELECT count(*)::int,count(*) FILTER (WHERE status='present')::int,count(*) FILTER (WHERE status='absent')::int,count(*) FILTER (WHERE status='late')::int,count(*) FILTER (WHERE status='excused')::int,
		COALESCE((SELECT jsonb_agg(jsonb_build_object('date',attendance_date,'status',status,'note',note) ORDER BY attendance_date DESC) FROM (SELECT attendance_date,status,note FROM attendance_records WHERE student_id=$1 AND school_id=$2 AND status<>'present' ORDER BY attendance_date DESC LIMIT 10) recent),'[]'::jsonb)
		FROM attendance_records WHERE student_id=$1 AND school_id=$2 AND attendance_date>=CURRENT_DATE-$3::int`, studentID, schoolID, attendanceAlertWindowDays).Scan(&summary.Days, &summary.Present, &summary.Absent, &summary.Late, &summary.Excused, jsonInto(&summary.Recent))
	return summary, err
}

func (a *API) studentAttendance(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, ok := a.prepStudent(w, r, id)
	if !ok {
		return
	}
	summary, err := a.attendanceSummary(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": summary})
}

// jsonValue lets a query scan a JSON column straight into a Go value.
type jsonValue struct{ target any }

func (j jsonValue) Scan(source any) error {
	raw, _ := source.(string)
	if bytes, ok := source.([]byte); ok {
		raw = string(bytes)
	}
	return json.Unmarshal([]byte(raw), j.target)
}

func jsonInto(target any) jsonValue { return jsonValue{target} }
