package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const (
	maxMarksPerSave = 20000
	maxMarkScore    = 1000
)

func (a *API) registerMarkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/terms", a.staffMember(a.listTerms))
	mux.HandleFunc("POST /api/v1/terms", a.admin(a.createTerm))
	mux.HandleFunc("PUT /api/v1/terms/{termID}", a.admin(a.updateTerm))
	mux.HandleFunc("DELETE /api/v1/terms/{termID}", a.admin(a.deleteTerm))
	mux.HandleFunc("GET /api/v1/report-settings", a.staffMember(a.getReportSettings))
	mux.HandleFunc("PUT /api/v1/report-settings", a.admin(a.saveReportSettings))
	mux.HandleFunc("GET /api/v1/classes/{classID}/subjects", a.staffMember(a.listClassSubjects))
	mux.HandleFunc("POST /api/v1/class-subjects", a.admin(a.createClassSubjects))
	mux.HandleFunc("PUT /api/v1/class-subjects/{subjectID}", a.admin(a.updateClassSubject))
	mux.HandleFunc("DELETE /api/v1/class-subjects/{subjectID}", a.admin(a.deleteClassSubject))
	mux.HandleFunc("GET /api/v1/marks", a.staffMember(a.getMarks))
	mux.HandleFunc("PUT /api/v1/marks", a.staffMember(a.saveMarks))
	mux.HandleFunc("POST /api/v1/marks/items", a.staffMember(a.createMarkItem))
	mux.HandleFunc("PUT /api/v1/marks/items/{itemID}", a.staffMember(a.updateMarkItem))
	mux.HandleFunc("DELETE /api/v1/marks/items/{itemID}", a.staffMember(a.deleteMarkItem))
}

func round2(value float64) float64 { return math.Round(value*100) / 100 }

type termInput struct {
	YearID string `json:"academic_year_id"`
	Name   string `json:"name"`
	Starts string `json:"starts_on"`
	Ends   string `json:"ends_on"`
}

func (a *API) listTerms(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT t.id::text,t.academic_year_id::text,y.name,t.name,t.starts_on::text,t.ends_on::text,
		EXISTS(SELECT 1 FROM mark_items i WHERE i.term_id=t.id) OR EXISTS(SELECT 1 FROM report_publications p WHERE p.term_id=t.id)
		FROM academic_terms t JOIN academic_years y ON y.id=t.academic_year_id
		WHERE t.school_id=$1 ORDER BY y.starts_on DESC,t.starts_on LIMIT 200`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var termID, yearID, yearName, name, starts, ends string
		var inUse bool
		if err = rows.Scan(&termID, &yearID, &yearName, &name, &starts, &ends, &inUse); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": termID, "academic_year_id": yearID, "academic_year_name": yearName, "name": name, "starts_on": starts, "ends_on": ends, "in_use": inUse})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

// checkTerm validates a term against its academic year and the year's other
// terms. A day in two terms would count its attendance on both reports.
func (a *API) checkTerm(ctx context.Context, schoolID, termID string, in *termInput) (code, message string, err error) {
	in.Name = strings.TrimSpace(in.Name)
	starts, errStarts := time.Parse("2006-01-02", in.Starts)
	ends, errEnds := time.Parse("2006-01-02", in.Ends)
	if in.Name == "" || len(in.Name) > 80 || !uuidPattern.MatchString(in.YearID) || errStarts != nil || errEnds != nil || ends.Before(starts) {
		return "VALIDATION_ERROR", "Enter a name, and an end date that is not before the start date.", nil
	}
	var inYear, overlaps bool
	err = a.db.QueryRow(ctx, `SELECT $3::date>=y.starts_on AND $4::date<=y.ends_on,
		EXISTS(SELECT 1 FROM academic_terms t WHERE t.academic_year_id=y.id AND t.id::text<>$5 AND t.starts_on<=$4::date AND t.ends_on>=$3::date)
		FROM academic_years y WHERE y.id=$1 AND y.school_id=$2`, in.YearID, schoolID, in.Starts, in.Ends, termID).Scan(&inYear, &overlaps)
	if errors.Is(err, pgx.ErrNoRows) {
		return "VALIDATION_ERROR", "That academic year is not in this school.", nil
	}
	if err != nil {
		return "", "", err
	}
	if !inYear {
		return "VALIDATION_ERROR", "The term must fall inside its academic year.", nil
	}
	if overlaps {
		return "TERM_OVERLAP", "These dates overlap another term in the same year.", nil
	}
	return "", "", nil
}

func (a *API) createTerm(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in termInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	code, message, err := a.checkTerm(r.Context(), id.SchoolID, "", &in)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if code != "" {
		writeError(w, 400, code, message, nil)
		return
	}
	var termID string
	err = a.db.QueryRow(r.Context(), `INSERT INTO academic_terms(school_id,academic_year_id,name,starts_on,ends_on) VALUES($1,$2,$3,$4::date,$5::date) RETURNING id::text`, id.SchoolID, in.YearID, in.Name, in.Starts, in.Ends).Scan(&termID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": termID}})
}

func (a *API) updateTerm(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in termInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	termID := r.PathValue("termID")
	var yearID string
	if err := a.db.QueryRow(r.Context(), `SELECT academic_year_id::text FROM academic_terms WHERE id=$1 AND school_id=$2`, termID, id.SchoolID).Scan(&yearID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	in.YearID = yearID
	code, message, err := a.checkTerm(r.Context(), id.SchoolID, termID, &in)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if code != "" {
		writeError(w, 400, code, message, nil)
		return
	}
	if _, err = a.db.Exec(r.Context(), `UPDATE academic_terms SET name=$3,starts_on=$4::date,ends_on=$5::date WHERE id=$1 AND school_id=$2`, termID, id.SchoolID, in.Name, in.Starts, in.Ends); err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": termID}})
}

func (a *API) deleteTerm(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	termID := r.PathValue("termID")
	var inUse bool
	err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM mark_items i WHERE i.term_id=t.id) OR EXISTS(SELECT 1 FROM report_publications p WHERE p.term_id=t.id) FROM academic_terms t WHERE t.id=$1 AND t.school_id=$2`, termID, id.SchoolID).Scan(&inUse)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if inUse {
		writeError(w, 409, "TERM_IN_USE", "This term already has marks or reports, so it cannot be deleted.", nil)
		return
	}
	if _, err = a.db.Exec(r.Context(), `DELETE FROM academic_terms WHERE id=$1 AND school_id=$2`, termID, id.SchoolID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": termID}})
}

type gradeBand struct {
	Label string  `json:"label"`
	Min   float64 `json:"min_percent"`
}

type reportSettings struct {
	PassPercent float64     `json:"pass_percent"`
	Bands       []gradeBand `json:"grade_bands"`
	Custom      bool        `json:"custom"`
}

// A starting point only. Each school sets the bands its own rules call for.
var defaultReportSettings = reportSettings{PassPercent: 50, Bands: []gradeBand{{"A", 90}, {"B", 80}, {"C", 60}, {"D", 50}, {"F", 0}}}

func (a *API) loadReportSettings(ctx context.Context, q reportQuerier, schoolID string) (reportSettings, error) {
	settings := reportSettings{Custom: true}
	var bands []byte
	err := q.QueryRow(ctx, `SELECT pass_percent::float8,grade_bands FROM report_settings WHERE school_id=$1`, schoolID).Scan(&settings.PassPercent, &bands)
	if errors.Is(err, pgx.ErrNoRows) {
		return defaultReportSettings, nil
	}
	if err != nil {
		return settings, err
	}
	return settings, json.Unmarshal(bands, &settings.Bands)
}

func (a *API) getReportSettings(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	settings, err := a.loadReportSettings(r.Context(), a.db, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": settings})
}

// checkGradeBands puts bands in order from highest and says what is wrong with
// them, if anything. Every possible score must land in a band.
func checkGradeBands(bands []gradeBand) string {
	if len(bands) < 2 || len(bands) > 12 {
		return "Use from 2 to 12 grade bands."
	}
	slices.SortStableFunc(bands, func(p, q gradeBand) int {
		if p.Min > q.Min {
			return -1
		}
		if p.Min < q.Min {
			return 1
		}
		return 0
	})
	seen := map[string]bool{}
	for i := range bands {
		bands[i].Label = strings.TrimSpace(bands[i].Label)
		label := strings.ToLower(bands[i].Label)
		if label == "" || len(label) > 12 || seen[label] {
			return "Give each grade band its own short name."
		}
		if bands[i].Min < 0 || bands[i].Min > 100 || (i > 0 && bands[i].Min == bands[i-1].Min) {
			return "Give each grade band its own lowest percent, from 0 to 100."
		}
		seen[label] = true
	}
	if bands[len(bands)-1].Min != 0 {
		return "The lowest grade band must start at 0, so every score has a grade."
	}
	return ""
}

func gradeFor(bands []gradeBand, percent float64) string {
	for _, band := range bands {
		if percent >= band.Min {
			return band.Label
		}
	}
	return ""
}

func (a *API) saveReportSettings(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		PassPercent float64     `json:"pass_percent"`
		Bands       []gradeBand `json:"grade_bands"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if req.PassPercent < 0 || req.PassPercent > 100 {
		writeError(w, 400, "VALIDATION_ERROR", "The pass mark must be from 0 to 100.", nil)
		return
	}
	if message := checkGradeBands(req.Bands); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	bands, err := json.Marshal(req.Bands)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO report_settings(school_id,pass_percent,grade_bands) VALUES($1,$2,$3::text::jsonb)
		ON CONFLICT (school_id) DO UPDATE SET pass_percent=EXCLUDED.pass_percent,grade_bands=EXCLUDED.grade_bands,updated_at=now()`, id.SchoolID, req.PassPercent, string(bands))
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": req})
}

// assignSubjectTeacher checks the teacher and adds them to the class, so the
// class appears in their workspace.
func assignSubjectTeacher(ctx context.Context, tx pgx.Tx, schoolID, classID, teacherID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO class_teachers(school_id,class_id,teacher_id) VALUES($1,$2,$3) ON CONFLICT (class_id,teacher_id) DO NOTHING`, schoolID, classID, teacherID)
	return err
}

func (a *API) activeTeacher(ctx context.Context, schoolID, teacherID string) (bool, error) {
	var found bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id AND ur.role='teacher' WHERE u.id=$1 AND u.school_id=$2 AND u.status='active')`, teacherID, schoolID).Scan(&found)
	return found, err
}

// listClassSubjects shows each subject's progress for a term, including the
// marks still missing from students now in the class.
func (a *API) listClassSubjects(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if !a.requireClass(w, r, id, classID) {
		return
	}
	termID := r.URL.Query().Get("term_id")
	if termID != "" && !uuidPattern.MatchString(termID) {
		writeError(w, 400, "VALIDATION_ERROR", "A term could not be read.", nil)
		return
	}
	rows, err := a.db.Query(r.Context(), `WITH roster AS (SELECT s.id FROM enrollments e JOIN students s ON s.id=e.student_id WHERE e.class_id=$1 AND e.status='active' AND s.status='active')
		SELECT cs.id::text,cs.name,COALESCE(cs.teacher_id::text,''),COALESCE(u.display_name,''),cs.counts_in_average,
		$3 OR COALESCE(cs.teacher_id=$4,false) OR (cs.teacher_id IS NULL AND EXISTS(SELECT 1 FROM class_teachers ct WHERE ct.class_id=cs.class_id AND ct.teacher_id=$4)),
		i.items,i.weight,i.items*(SELECT count(*)::int FROM roster)-(SELECT count(*)::int FROM marks m JOIN mark_items it ON it.id=m.item_id WHERE it.class_subject_id=cs.id AND it.term_id::text=$5 AND m.student_id IN (SELECT id FROM roster))
		FROM class_subjects cs LEFT JOIN users u ON u.id=cs.teacher_id
		CROSS JOIN LATERAL (SELECT count(*)::int items,COALESCE(sum(weight),0)::float8 weight FROM mark_items x WHERE x.class_subject_id=cs.id AND x.term_id::text=$5) i
		WHERE cs.class_id=$1 AND cs.school_id=$2 ORDER BY cs.sort_order,cs.name`, classID, id.SchoolID, hasRole(id.Roles, "school_admin"), id.UserID, termID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type subject struct {
		ID          string  `json:"id"`
		Name        string  `json:"name"`
		Teacher     string  `json:"teacher_id"`
		TeacherName string  `json:"teacher_name"`
		Counts      bool    `json:"counts_in_average"`
		CanEdit     bool    `json:"can_edit"`
		Items       int     `json:"items"`
		Weight      float64 `json:"weight_total"`
		Missing     int     `json:"missing_marks"`
	}
	subjects := []subject{}
	for rows.Next() {
		var x subject
		if err = rows.Scan(&x.ID, &x.Name, &x.Teacher, &x.TeacherName, &x.Counts, &x.CanEdit, &x.Items, &x.Weight, &x.Missing); err != nil {
			a.serverError(w, r, err)
			return
		}
		subjects = append(subjects, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": subjects})
}

// createClassSubjects adds a list of subjects to a list of classes, which is how
// a school sets up a whole grade at once. A class that has a subject keeps it.
func (a *API) createClassSubjects(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ClassIDs []string `json:"class_ids"`
		Names    []string `json:"names"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	names := []string{}
	seen := map[string]bool{}
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 120 {
			writeError(w, 400, "VALIDATION_ERROR", "Keep each subject name under 120 characters.", nil)
			return
		}
		if !seen[strings.ToLower(name)] {
			names = append(names, name)
			seen[strings.ToLower(name)] = true
		}
	}
	if len(names) == 0 || len(names) > 40 || len(req.ClassIDs) == 0 || len(req.ClassIDs) > 500 {
		writeError(w, 400, "VALIDATION_ERROR", "Choose at least one class and enter at least one subject.", nil)
		return
	}
	for _, classID := range req.ClassIDs {
		if !uuidPattern.MatchString(classID) {
			writeError(w, 400, "VALIDATION_ERROR", "A class could not be read.", nil)
			return
		}
	}
	var found int
	if err := a.db.QueryRow(r.Context(), `SELECT count(*)::int FROM classes WHERE id=ANY($1::uuid[]) AND school_id=$2`, req.ClassIDs, id.SchoolID).Scan(&found); err != nil {
		a.serverError(w, r, err)
		return
	}
	if found != len(slicesCompactSorted(req.ClassIDs)) {
		writeError(w, 400, "VALIDATION_ERROR", "A chosen class is not in this school.", nil)
		return
	}
	tag, err := a.db.Exec(r.Context(), `INSERT INTO class_subjects(school_id,class_id,name,sort_order)
		SELECT $1,c.id,n.name,n.position::int+(SELECT COALESCE(max(sort_order),0) FROM class_subjects x WHERE x.class_id=c.id)
		FROM unnest($2::uuid[]) AS c(id) CROSS JOIN unnest($3::text[]) WITH ORDINALITY AS n(name,position)
		ON CONFLICT (class_id,lower(name)) DO NOTHING`, id.SchoolID, slicesCompactSorted(req.ClassIDs), names)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]int64{"created": tag.RowsAffected()}})
}

func (a *API) updateClassSubject(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name    string `json:"name"`
		Teacher string `json:"teacher_id"`
		Counts  bool   `json:"counts_in_average"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 120 || (req.Teacher != "" && !uuidPattern.MatchString(req.Teacher)) {
		writeError(w, 400, "VALIDATION_ERROR", "Enter a subject name.", nil)
		return
	}
	if req.Teacher != "" {
		found, err := a.activeTeacher(r.Context(), id.SchoolID, req.Teacher)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if !found {
			writeError(w, 400, "VALIDATION_ERROR", "That teacher is not active in this school.", nil)
			return
		}
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	subjectID := r.PathValue("subjectID")
	var classID string
	err = tx.QueryRow(r.Context(), `UPDATE class_subjects SET name=$3,teacher_id=NULLIF($4,'')::uuid,counts_in_average=$5 WHERE id=$1 AND school_id=$2 RETURNING class_id::text`, subjectID, id.SchoolID, req.Name, req.Teacher, req.Counts).Scan(&classID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if req.Teacher != "" {
		if err = assignSubjectTeacher(r.Context(), tx, id.SchoolID, classID, req.Teacher); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": subjectID}})
}

func (a *API) deleteClassSubject(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	subjectID := r.PathValue("subjectID")
	var hasItems bool
	err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM mark_items i WHERE i.class_subject_id=cs.id) OR EXISTS(SELECT 1 FROM assessments q WHERE q.class_subject_id=cs.id) FROM class_subjects cs WHERE cs.id=$1 AND cs.school_id=$2`, subjectID, id.SchoolID).Scan(&hasItems)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if hasItems {
		writeError(w, 409, "SUBJECT_HAS_MARKS", "This subject has tests, quizzes or marks, so it cannot be deleted.", nil)
		return
	}
	if _, err = a.db.Exec(r.Context(), `DELETE FROM class_subjects WHERE id=$1 AND school_id=$2`, subjectID, id.SchoolID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": subjectID}})
}

type subjectAccess struct {
	ClassID string
	Name    string
	CanEdit bool
}

// loadSubjectAccess finds who can use a subject. An administrator or a teacher of
// the class can read its marks. Only an administrator or its own teacher can
// change them, or any teacher of the class when the subject has none.
func (a *API) loadSubjectAccess(w http.ResponseWriter, r *http.Request, id auth.Identity, subjectID string) (subjectAccess, bool) {
	var access subjectAccess
	var assigned bool
	err := a.db.QueryRow(r.Context(), `SELECT cs.class_id::text,cs.name,EXISTS(SELECT 1 FROM class_teachers ct WHERE ct.class_id=cs.class_id AND ct.teacher_id=$3),
		$4 OR COALESCE(cs.teacher_id=$3,false) OR (cs.teacher_id IS NULL AND EXISTS(SELECT 1 FROM class_teachers ct WHERE ct.class_id=cs.class_id AND ct.teacher_id=$3))
		FROM class_subjects cs WHERE cs.id=$1 AND cs.school_id=$2`, subjectID, id.SchoolID, id.UserID, hasRole(id.Roles, "school_admin")).Scan(&access.ClassID, &access.Name, &assigned, &access.CanEdit)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Subject not found.", nil)
		return access, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return access, false
	}
	if !assigned && !hasRole(id.Roles, "school_admin") {
		writeError(w, 403, "FORBIDDEN", "You are not assigned to this class.", nil)
		return access, false
	}
	return access, true
}

func (a *API) requireSubjectEdit(w http.ResponseWriter, access subjectAccess) bool {
	if !access.CanEdit {
		writeError(w, 403, "FORBIDDEN", "Another teacher enters the marks for this subject.", nil)
	}
	return access.CanEdit
}

// termInClass checks that a term belongs to the class's academic year.
func (a *API) termInClass(ctx context.Context, q reportQuerier, schoolID, termID, classID string) (bool, error) {
	var found bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM academic_terms t JOIN classes c ON c.academic_year_id=t.academic_year_id WHERE t.id=$1 AND c.id=$2 AND t.school_id=$3 AND c.school_id=$3)`, termID, classID, schoolID).Scan(&found)
	return found, err
}

func reportPublished(ctx context.Context, q reportQuerier, termID, classID string) (bool, error) {
	var published bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM report_publications WHERE term_id=$1 AND class_id=$2)`, termID, classID).Scan(&published)
	return published, err
}

func writeReportPublished(w http.ResponseWriter) {
	writeError(w, 409, "REPORT_PUBLISHED", "Reports for this term are published, so its marks are locked. An administrator can unpublish them.", nil)
}

func (a *API) getMarks(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	subjectID, termID := r.URL.Query().Get("class_subject_id"), r.URL.Query().Get("term_id")
	if !uuidPattern.MatchString(subjectID) || !uuidPattern.MatchString(termID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a subject and a term.", nil)
		return
	}
	access, ok := a.loadSubjectAccess(w, r, id, subjectID)
	if !ok {
		return
	}
	found, err := a.termInClass(r.Context(), a.db, id.SchoolID, termID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That term is not in this class's academic year.", nil)
		return
	}
	published, err := reportPublished(r.Context(), a.db, termID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	itemRows, err := a.db.Query(r.Context(), `SELECT id::text,title,weight::float8,max_score::float8 FROM mark_items WHERE class_subject_id=$1 AND term_id=$2 ORDER BY created_at,title`, subjectID, termID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items, err := pgx.CollectRows(itemRows, func(row pgx.CollectableRow) (markItem, error) {
		var x markItem
		err := row.Scan(&x.ID, &x.Title, &x.Weight, &x.Max)
		return x, err
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if items == nil {
		items = []markItem{}
	}
	studentRows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,
		s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$2 AND e.status='active'),
		COALESCE((SELECT jsonb_object_agg(m.item_id,m.score) FROM marks m JOIN mark_items i ON i.id=m.item_id WHERE m.student_id=s.id AND i.class_subject_id=$3 AND i.term_id=$4),'{}'::jsonb)
		FROM students s WHERE s.school_id=$1 AND (
			(s.status='active' AND EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$2 AND e.status='active'))
			OR EXISTS(SELECT 1 FROM marks m JOIN mark_items i ON i.id=m.item_id WHERE m.student_id=s.id AND i.class_subject_id=$3 AND i.term_id=$4))
		ORDER BY s.legal_name,s.student_number LIMIT 1000`, id.SchoolID, access.ClassID, subjectID, termID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer studentRows.Close()
	type studentRow struct {
		ID      string             `json:"student_id"`
		Number  string             `json:"student_number"`
		Name    string             `json:"student_name"`
		Current bool               `json:"in_class"`
		Scores  map[string]float64 `json:"scores"`
	}
	students := []studentRow{}
	for studentRows.Next() {
		var x studentRow
		if err = studentRows.Scan(&x.ID, &x.Number, &x.Name, &x.Current, jsonInto(&x.Scores)); err != nil {
			a.serverError(w, r, err)
			return
		}
		students = append(students, x)
	}
	if err = studentRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	weightTotal := 0.0
	for _, item := range items {
		weightTotal += item.Weight
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"subject_name": access.Name, "can_edit": access.CanEdit && !published, "published": published, "weight_total": round2(weightTotal), "items": items, "students": students}})
}

// lockClass holds a class for the rest of a transaction. Saving marks shares the
// lock and publishing takes it alone, so no mark lands during a publish.
func lockClass(ctx context.Context, tx pgx.Tx, classID, mode string) error {
	var found string
	return tx.QueryRow(ctx, `SELECT id::text FROM classes WHERE id=$1 FOR `+mode, classID).Scan(&found)
}

func (a *API) saveMarks(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		SubjectID string `json:"class_subject_id"`
		TermID    string `json:"term_id"`
		Marks     []struct {
			StudentID string   `json:"student_id"`
			ItemID    string   `json:"item_id"`
			Score     *float64 `json:"score"`
		} `json:"marks"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !uuidPattern.MatchString(req.SubjectID) || !uuidPattern.MatchString(req.TermID) || len(req.Marks) == 0 || len(req.Marks) > maxMarksPerSave {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a subject and a term, and enter at least one mark.", nil)
		return
	}
	access, ok := a.loadSubjectAccess(w, r, id, req.SubjectID)
	if !ok || !a.requireSubjectEdit(w, access) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if err = lockClass(r.Context(), tx, access.ClassID, "SHARE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	published, err := reportPublished(r.Context(), tx, req.TermID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if published {
		writeReportPublished(w)
		return
	}
	itemRows, err := tx.Query(r.Context(), `SELECT id::text,title,max_score::float8 FROM mark_items WHERE class_subject_id=$1 AND term_id=$2`, req.SubjectID, req.TermID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	items := map[string]markItem{}
	for itemRows.Next() {
		var x markItem
		if err = itemRows.Scan(&x.ID, &x.Title, &x.Max); err != nil {
			itemRows.Close()
			a.serverError(w, r, err)
			return
		}
		items[x.ID] = x
	}
	itemRows.Close()
	if err = itemRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	var saveStudents, saveItems, clearStudents, clearItems, students []string
	var saveScores []float64
	seen := map[string]bool{}
	for _, mark := range req.Marks {
		item, known := items[mark.ItemID]
		if !known || !uuidPattern.MatchString(mark.StudentID) {
			writeError(w, 400, "VALIDATION_ERROR", "A mark could not be read.", nil)
			return
		}
		pair := mark.ItemID + "|" + mark.StudentID
		if seen[pair] {
			writeError(w, 400, "VALIDATION_ERROR", "A student has two marks for the same test.", nil)
			return
		}
		seen[pair] = true
		students = append(students, mark.StudentID)
		if mark.Score == nil {
			clearStudents, clearItems = append(clearStudents, mark.StudentID), append(clearItems, mark.ItemID)
			continue
		}
		score := round2(*mark.Score)
		if math.IsNaN(score) || score < 0 || score > item.Max {
			writeError(w, 400, "VALIDATION_ERROR", fmt.Sprintf("%s is marked out of %g, so a mark of %g cannot be saved.", item.Title, item.Max, *mark.Score), nil)
			return
		}
		saveStudents, saveItems, saveScores = append(saveStudents, mark.StudentID), append(saveItems, mark.ItemID), append(saveScores, score)
	}
	students = slicesCompactSorted(students)
	var known int
	itemIDs := make([]string, 0, len(items))
	for itemID := range items {
		itemIDs = append(itemIDs, itemID)
	}
	err = tx.QueryRow(r.Context(), `SELECT count(*)::int FROM students s WHERE s.id=ANY($1::uuid[]) AND s.school_id=$2 AND (
		EXISTS(SELECT 1 FROM enrollments e WHERE e.student_id=s.id AND e.class_id=$3 AND e.status='active')
		OR EXISTS(SELECT 1 FROM marks m WHERE m.student_id=s.id AND m.item_id=ANY($4::uuid[])))`, students, id.SchoolID, access.ClassID, itemIDs).Scan(&known)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if known != len(students) {
		writeError(w, 400, "VALIDATION_ERROR", "A marked student is not in this class.", nil)
		return
	}
	// A change to a mark already given is kept in the audit log with both values.
	corrections, err := markCorrections(r.Context(), tx, itemIDs, students, seen, saveStudents, saveItems, saveScores)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(saveStudents) > 0 {
		if _, err = tx.Exec(r.Context(), `INSERT INTO marks(school_id,item_id,student_id,score,recorded_by)
			SELECT $1,m.item_id,m.student_id,m.score::numeric(6,2),$2 FROM unnest($3::uuid[],$4::uuid[],$5::float8[]) AS m(item_id,student_id,score)
			ON CONFLICT (item_id,student_id) DO UPDATE SET score=EXCLUDED.score,recorded_by=EXCLUDED.recorded_by,updated_at=now() WHERE marks.score<>EXCLUDED.score`,
			id.SchoolID, id.UserID, saveItems, saveStudents, saveScores); err != nil {
			a.databaseError(w, r, err)
			return
		}
	}
	if len(clearStudents) > 0 {
		if _, err = tx.Exec(r.Context(), `DELETE FROM marks m USING unnest($1::uuid[],$2::uuid[]) AS c(item_id,student_id) WHERE m.item_id=c.item_id AND m.student_id=c.student_id`, clearItems, clearStudents); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	changes, err := json.Marshal(map[string]any{"term_id": req.TermID, "subject": access.Name, "saved": len(saveStudents), "cleared": len(clearStudents), "corrections": corrections})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'marks.saved','class',$3,$4::text::jsonb)`, id.SchoolID, id.UserID, access.ClassID, string(changes)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]int{"saved": len(saveStudents), "cleared": len(clearStudents)}})
}

type markCorrection struct {
	ItemID    string   `json:"item_id"`
	StudentID string   `json:"student_id"`
	From      float64  `json:"from"`
	To        *float64 `json:"to"`
}

func markCorrections(ctx context.Context, tx pgx.Tx, itemIDs, students []string, submitted map[string]bool, saveStudents, saveItems []string, saveScores []float64) ([]markCorrection, error) {
	next := make(map[string]float64, len(saveStudents))
	for i := range saveStudents {
		next[saveItems[i]+"|"+saveStudents[i]] = saveScores[i]
	}
	rows, err := tx.Query(ctx, `SELECT item_id::text,student_id::text,score::float8 FROM marks WHERE item_id=ANY($1::uuid[]) AND student_id=ANY($2::uuid[])`, itemIDs, students)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	corrections := []markCorrection{}
	for rows.Next() {
		var x markCorrection
		if err = rows.Scan(&x.ItemID, &x.StudentID, &x.From); err != nil {
			return nil, err
		}
		pair := x.ItemID + "|" + x.StudentID
		if !submitted[pair] {
			continue
		}
		to, saved := next[pair]
		if saved && to == x.From {
			continue
		}
		if saved {
			x.To = &to
		}
		corrections = append(corrections, x)
	}
	return corrections, rows.Err()
}

type markItemInput struct {
	SubjectID string  `json:"class_subject_id"`
	TermID    string  `json:"term_id"`
	Title     string  `json:"title"`
	Weight    float64 `json:"weight"`
	Max       float64 `json:"max_score"`
}

func (in *markItemInput) check() string {
	in.Title = strings.TrimSpace(in.Title)
	switch {
	case in.Title == "" || len(in.Title) > 120:
		return "Name the test or assignment."
	case in.Weight <= 0 || in.Weight > 100:
		return "Its share of the total must be more than 0 and at most 100."
	case in.Max <= 0 || in.Max > maxMarkScore:
		return fmt.Sprintf("It must be marked out of more than 0 and at most %d.", maxMarkScore)
	}
	in.Weight, in.Max = round2(in.Weight), round2(in.Max)
	return ""
}

// checkItemWeights refuses a change that takes a subject past 100 in a term.
func checkItemWeights(ctx context.Context, tx pgx.Tx, subjectID, termID, itemID string, weight float64) (string, error) {
	var other float64
	err := tx.QueryRow(ctx, `SELECT COALESCE(sum(weight),0)::float8 FROM mark_items WHERE class_subject_id=$1 AND term_id=$2 AND id::text<>$3`, subjectID, termID, itemID).Scan(&other)
	if err != nil {
		return "", err
	}
	if round2(other+weight) > 100 {
		return fmt.Sprintf("The tests for this subject would add up to %g. They must add up to 100.", round2(other+weight)), nil
	}
	return "", nil
}

func (a *API) createMarkItem(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in markItemInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if !uuidPattern.MatchString(in.SubjectID) || !uuidPattern.MatchString(in.TermID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a subject and a term.", nil)
		return
	}
	if message := in.check(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	access, ok := a.loadSubjectAccess(w, r, id, in.SubjectID)
	if !ok || !a.requireSubjectEdit(w, access) {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if err = lockClass(r.Context(), tx, access.ClassID, "UPDATE"); err != nil {
		a.serverError(w, r, err)
		return
	}
	found, err := a.termInClass(r.Context(), tx, id.SchoolID, in.TermID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !found {
		writeError(w, 400, "VALIDATION_ERROR", "That term is not in this class's academic year.", nil)
		return
	}
	published, err := reportPublished(r.Context(), tx, in.TermID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if published {
		writeReportPublished(w)
		return
	}
	message, err := checkItemWeights(r.Context(), tx, in.SubjectID, in.TermID, "", in.Weight)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if message != "" {
		writeError(w, 400, "WEIGHTS_OVER_100", message, nil)
		return
	}
	var itemID string
	err = tx.QueryRow(r.Context(), `INSERT INTO mark_items(school_id,class_subject_id,term_id,title,weight,max_score) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, id.SchoolID, in.SubjectID, in.TermID, in.Title, in.Weight, in.Max).Scan(&itemID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": itemID}})
}

// loadItemForChange finds an item and takes the checks every change shares. Its
// transaction holds the class until the caller commits.
func (a *API) loadItemForChange(w http.ResponseWriter, r *http.Request, id auth.Identity, tx pgx.Tx, itemID string) (subjectID, termID, classID string, ok bool) {
	err := tx.QueryRow(r.Context(), `SELECT i.class_subject_id::text,i.term_id::text FROM mark_items i WHERE i.id=$1 AND i.school_id=$2`, itemID, id.SchoolID).Scan(&subjectID, &termID)
	if err != nil {
		a.databaseError(w, r, err)
		return "", "", "", false
	}
	access, allowed := a.loadSubjectAccess(w, r, id, subjectID)
	if !allowed || !a.requireSubjectEdit(w, access) {
		return "", "", "", false
	}
	if err = lockClass(r.Context(), tx, access.ClassID, "UPDATE"); err != nil {
		a.serverError(w, r, err)
		return "", "", "", false
	}
	published, err := reportPublished(r.Context(), tx, termID, access.ClassID)
	if err != nil {
		a.serverError(w, r, err)
		return "", "", "", false
	}
	if published {
		writeReportPublished(w)
		return "", "", "", false
	}
	return subjectID, termID, access.ClassID, true
}

func (a *API) updateMarkItem(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var in markItemInput
	if !decode(w, r, &in) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := in.check(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	itemID := r.PathValue("itemID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	subjectID, termID, _, ok := a.loadItemForChange(w, r, id, tx, itemID)
	if !ok {
		return
	}
	message, err := checkItemWeights(r.Context(), tx, subjectID, termID, itemID, in.Weight)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if message != "" {
		writeError(w, 400, "WEIGHTS_OVER_100", message, nil)
		return
	}
	var above bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM marks WHERE item_id=$1 AND score>$2)`, itemID, in.Max).Scan(&above); err != nil {
		a.serverError(w, r, err)
		return
	}
	if above {
		writeError(w, 400, "VALIDATION_ERROR", fmt.Sprintf("Some students already have more than %g on this test.", in.Max), nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE mark_items SET title=$2,weight=$3,max_score=$4 WHERE id=$1`, itemID, in.Title, in.Weight, in.Max); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": itemID}})
}

// deleteMarkItem removes a test. One with marks needs saying so, and is logged.
func (a *API) deleteMarkItem(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	itemID := r.PathValue("itemID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	_, _, classID, ok := a.loadItemForChange(w, r, id, tx, itemID)
	if !ok {
		return
	}
	var marks int
	var title string
	if err = tx.QueryRow(r.Context(), `SELECT i.title,(SELECT count(*)::int FROM marks m WHERE m.item_id=i.id) FROM mark_items i WHERE i.id=$1`, itemID).Scan(&title, &marks); err != nil {
		a.serverError(w, r, err)
		return
	}
	if marks > 0 && r.URL.Query().Get("with_marks") != "true" {
		writeError(w, 409, "ITEM_HAS_MARKS", "Students already have marks on this test.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `DELETE FROM mark_items WHERE id=$1`, itemID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'marks.item_deleted','class',$3,jsonb_build_object('title',$4::text,'marks',$5::int))`, id.SchoolID, id.UserID, classID, title, marks); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": itemID}})
}
