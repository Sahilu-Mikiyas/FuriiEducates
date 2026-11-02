package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

func (a *API) registerExamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/exam-sessions", a.staffMember(a.listExamSessions))
	mux.HandleFunc("GET /api/v1/exam-sessions/{assignmentID}", a.staffMember(a.getExamSession))
	mux.HandleFunc("POST /api/v1/exam-sessions/{assignmentID}/open", a.staffMember(a.openExamSession))
	mux.HandleFunc("POST /api/v1/exam-sessions/{assignmentID}/close", a.staffMember(a.closeExamSession))
	mux.HandleFunc("GET /api/v1/exam-sessions/{assignmentID}/attempts/{attemptID}/events", a.staffMember(a.listAttemptEvents))
	mux.HandleFunc("POST /api/v1/exam-sessions/{assignmentID}/attempts/{attemptID}/extend", a.staffMember(a.extendAttempt))
	mux.HandleFunc("POST /api/v1/exam-sessions/{assignmentID}/attempts/{attemptID}/finish", a.staffMember(a.finishAttempt))
	mux.HandleFunc("POST /api/v1/exam-sessions/{assignmentID}/attempts/{attemptID}/incident", a.staffMember(a.recordIncident))
	mux.HandleFunc("GET /api/v1/gradebook", a.staffMember(a.getGradebook))
	mux.HandleFunc("POST /api/v1/student/attempts/{attemptID}/integrity-events", a.student(a.recordIntegrityEvent))
}

// staffMember allows school administrators and teachers. Handlers still check
// that a teacher is assigned to the class concerned.
func (a *API) staffMember(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action requires a school staff account.", "school_admin", "teacher")
}

// canManageClass reports whether the user may run exams and read results for
// a class: any administrator in the school, or a teacher assigned to it.
func (a *API) canManageClass(ctx context.Context, id auth.Identity, classID string) (bool, error) {
	if hasRole(id.Roles, "school_admin") {
		var found bool
		err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM classes WHERE id=$1 AND school_id=$2)`, classID, id.SchoolID).Scan(&found)
		return found, err
	}
	var assigned bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND class_id=$2 AND teacher_id=$3)`, id.SchoolID, classID, id.UserID).Scan(&assigned)
	return assigned, err
}

// sessionClass resolves the route's assignment to its class, writing the
// error response and returning false when the user may not manage it.
func (a *API) sessionClass(w http.ResponseWriter, r *http.Request, id auth.Identity) (string, bool) {
	var classID string
	err := a.db.QueryRow(r.Context(), `SELECT class_id::text FROM assessment_assignments WHERE id=$1 AND school_id=$2`, r.PathValue("assignmentID"), id.SchoolID).Scan(&classID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Exam session not found.", nil)
		return "", false
	}
	if err != nil {
		a.serverError(w, r, err)
		return "", false
	}
	allowed, err := a.canManageClass(r.Context(), id, classID)
	if err != nil {
		a.serverError(w, r, err)
		return "", false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You are not assigned to this class.", nil)
		return "", false
	}
	return classID, true
}

const isoTimestamp = `'YYYY-MM-DD"T"HH24:MI:SS"Z"'`

func (a *API) listExamSessions(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT aa.id::text,a.title,c.id::text,c.name,c.grade_level,a.duration_minutes,a.locked_mode,aa.requires_start_code,aa.session_open,
		       to_char(aa.opens_at AT TIME ZONE 'UTC',`+isoTimestamp+`),to_char(aa.due_at AT TIME ZONE 'UTC',`+isoTimestamp+`),
		       (SELECT count(*)::int FROM enrollments e WHERE e.class_id=aa.class_id AND e.school_id=aa.school_id AND e.status='active'),
		       (SELECT count(*)::int FROM assessment_attempts t WHERE t.assignment_id=aa.id AND t.status='in_progress'),
		       (SELECT count(*)::int FROM assessment_attempts t WHERE t.assignment_id=aa.id AND t.status='submitted')
		FROM assessment_assignments aa
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		JOIN classes c ON c.id=aa.class_id AND c.school_id=aa.school_id
		WHERE aa.school_id=$1 AND ($2 OR EXISTS(SELECT 1 FROM class_teachers ct WHERE ct.class_id=aa.class_id AND ct.school_id=aa.school_id AND ct.teacher_id=$3))
		ORDER BY aa.assigned_at DESC LIMIT 200`, id.SchoolID, hasRole(id.Roles, "school_admin"), id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type session struct {
		AssignmentID      string  `json:"assignment_id"`
		Title             string  `json:"title"`
		ClassID           string  `json:"class_id"`
		ClassName         string  `json:"class_name"`
		GradeLevel        string  `json:"grade_level"`
		DurationMinutes   int     `json:"duration_minutes"`
		LockedMode        bool    `json:"locked_mode"`
		RequiresStartCode bool    `json:"requires_start_code"`
		SessionOpen       bool    `json:"session_open"`
		OpensAt           *string `json:"opens_at"`
		DueAt             *string `json:"due_at"`
		Enrolled          int     `json:"enrolled"`
		InProgress        int     `json:"in_progress"`
		Submitted         int     `json:"submitted"`
	}
	items := []session{}
	for rows.Next() {
		var x session
		if err = rows.Scan(&x.AssignmentID, &x.Title, &x.ClassID, &x.ClassName, &x.GradeLevel, &x.DurationMinutes, &x.LockedMode, &x.RequiresStartCode, &x.SessionOpen, &x.OpensAt, &x.DueAt, &x.Enrolled, &x.InProgress, &x.Submitted); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

// getExamSession is the invigilator console: session state, the start code,
// and each enrolled student's live status.
func (a *API) getExamSession(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID, ok := a.sessionClass(w, r, id)
	if !ok {
		return
	}
	assignmentID := r.PathValue("assignmentID")
	var title string
	var duration, questionCount int
	var locked, requiresCode, sessionOpen bool
	var startCode *string
	err := a.db.QueryRow(r.Context(), `
		SELECT a.title,a.duration_minutes,a.locked_mode,aa.requires_start_code,aa.session_open,aa.start_code,
		       (SELECT count(*)::int FROM assessment_items ai WHERE ai.assessment_id=a.id)
		FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		WHERE aa.id=$1 AND aa.school_id=$2`, assignmentID, id.SchoolID).Scan(&title, &duration, &locked, &requiresCode, &sessionOpen, &startCode, &questionCount)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT s.id::text,s.student_number,s.legal_name,COALESCE(t.id::text,''),COALESCE(t.status,'not_started'),
		       to_char(t.deadline_at AT TIME ZONE 'UTC',`+isoTimestamp+`),to_char(t.submitted_at AT TIME ZONE 'UTC',`+isoTimestamp+`),
		       (SELECT count(*)::int FROM assessment_answers ans WHERE ans.attempt_id=t.id AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb),
		       (SELECT to_char(max(ans.saved_at) AT TIME ZONE 'UTC',`+isoTimestamp+`) FROM assessment_answers ans WHERE ans.attempt_id=t.id),
		       (SELECT count(*)::int FROM assessment_attempt_events ev WHERE ev.attempt_id=t.id AND ev.event_type='integrity_flag'),
		       (SELECT count(*)::int FROM assessment_attempt_events ev WHERE ev.attempt_id=t.id AND ev.event_type='incident')
		FROM enrollments e
		JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		LEFT JOIN assessment_attempts t ON t.assignment_id=$1 AND t.student_id=s.id
		WHERE e.class_id=$2 AND e.school_id=$3 AND e.status='active' AND s.status='active'
		ORDER BY s.legal_name,s.student_number LIMIT 1000`, assignmentID, classID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type rosterRow struct {
		StudentID      string  `json:"student_id"`
		StudentNumber  string  `json:"student_number"`
		StudentName    string  `json:"student_name"`
		AttemptID      string  `json:"attempt_id"`
		Status         string  `json:"status"`
		DeadlineAt     *string `json:"deadline_at"`
		SubmittedAt    *string `json:"submitted_at"`
		Answered       int     `json:"answered"`
		LastSavedAt    *string `json:"last_saved_at"`
		IntegrityFlags int     `json:"integrity_flags"`
		Incidents      int     `json:"incidents"`
	}
	roster := []rosterRow{}
	for rows.Next() {
		var x rosterRow
		if err = rows.Scan(&x.StudentID, &x.StudentNumber, &x.StudentName, &x.AttemptID, &x.Status, &x.DeadlineAt, &x.SubmittedAt, &x.Answered, &x.LastSavedAt, &x.IntegrityFlags, &x.Incidents); err != nil {
			a.serverError(w, r, err)
			return
		}
		roster = append(roster, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"assignment_id": assignmentID, "class_id": classID, "title": title, "duration_minutes": duration, "question_count": questionCount,
		"locked_mode": locked, "requires_start_code": requiresCode, "session_open": sessionOpen, "start_code": startCode, "roster": roster,
	}})
}

// newStartCode returns six characters that are easy to read aloud and copy
// from a board: no 0/O or 1/I/L.
func newStartCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	code := make([]byte, len(raw))
	for i, value := range raw {
		code[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(code), nil
}

func (a *API) openExamSession(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	code, err := newStartCode()
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	assignmentID := r.PathValue("assignmentID")
	tag, err := a.db.Exec(r.Context(), `UPDATE assessment_assignments SET session_open=TRUE,start_code=$3 WHERE id=$1 AND school_id=$2 AND requires_start_code`, assignmentID, id.SchoolID, code)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "START_CODE_NOT_REQUIRED", "This assessment does not use a start code.", nil)
		return
	}
	if err = a.auditExam(r, id, "exam_session.opened", "assessment_assignment", assignmentID, nil); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"session_open": true, "start_code": code}})
}

func (a *API) closeExamSession(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	assignmentID := r.PathValue("assignmentID")
	if _, err := a.db.Exec(r.Context(), `UPDATE assessment_assignments SET session_open=FALSE,start_code=NULL WHERE id=$1 AND school_id=$2`, assignmentID, id.SchoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := a.auditExam(r, id, "exam_session.closed", "assessment_assignment", assignmentID, nil); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"session_open": false}})
}

func (a *API) auditExam(r *http.Request, id auth.Identity, action, entity, entityID string, changes map[string]any) error {
	if changes == nil {
		changes = map[string]any{}
	}
	raw, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,$3,$4,$5,$6)`, id.SchoolID, id.UserID, action, entity, entityID, raw)
	return err
}

// lockSessionAttempt locks an attempt that belongs to the route's assignment.
// It writes the error response and returns false when there is none.
func (a *API) lockSessionAttempt(w http.ResponseWriter, r *http.Request, tx pgx.Tx, id auth.Identity) (string, bool) {
	var status string
	err := tx.QueryRow(r.Context(), `SELECT status FROM assessment_attempts WHERE id=$1 AND assignment_id=$2 AND school_id=$3 FOR UPDATE`, r.PathValue("attemptID"), r.PathValue("assignmentID"), id.SchoolID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "This student has not started the exam.", nil)
		return "", false
	}
	if err != nil {
		a.serverError(w, r, err)
		return "", false
	}
	return status, true
}

// extendAttempt adds time for one student, for example after a power cut.
func (a *API) extendAttempt(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Minutes int    `json:"minutes"`
		Reason  string `json:"reason"`
	}
	if !decode(w, r, &req) || req.Minutes < 1 || req.Minutes > 240 || len(req.Reason) > 1000 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter between 1 and 240 extra minutes.", nil)
		return
	}
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	status, ok := a.lockSessionAttempt(w, r, tx, id)
	if !ok {
		return
	}
	if status != "in_progress" {
		writeError(w, http.StatusConflict, "ATTEMPT_SUBMITTED", "This attempt has already been submitted.", nil)
		return
	}
	attemptID := r.PathValue("attemptID")
	var deadline string
	err = tx.QueryRow(r.Context(), `UPDATE assessment_attempts SET deadline_at=GREATEST(deadline_at,now())+make_interval(mins=>$2) WHERE id=$1 RETURNING to_char(deadline_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')`, attemptID, req.Minutes).Scan(&deadline)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,'time_extended',jsonb_build_object('minutes',$3::int,'reason',$4::text,'by',$5::text))`, id.SchoolID, attemptID, req.Minutes, strings.TrimSpace(req.Reason), id.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'exam_attempt.time_extended','assessment_attempt',$3,jsonb_build_object('minutes',$4::int,'reason',$5::text))`, id.SchoolID, id.UserID, attemptID, req.Minutes, strings.TrimSpace(req.Reason)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"deadline_at": deadline}})
}

// finishAttempt ends one student's attempt now, grading their saved answers.
func (a *API) finishAttempt(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	status, ok := a.lockSessionAttempt(w, r, tx, id)
	if !ok {
		return
	}
	if status != "in_progress" {
		writeError(w, http.StatusConflict, "ATTEMPT_SUBMITTED", "This attempt has already been submitted.", nil)
		return
	}
	actor := id.UserID
	result, err := finalizeAttempt(r.Context(), tx, id.SchoolID, r.PathValue("attemptID"), &actor, nil, "force_submitted")
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"status": "submitted", "auto_score": result.Total, "manual_review_count": result.NeedsReview}})
}

func (a *API) recordIncident(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Note string `json:"note"`
	}
	if !decode(w, r, &req) || strings.TrimSpace(req.Note) == "" || len(req.Note) > 2000 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Describe the incident in under 2,000 characters.", nil)
		return
	}
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	attemptID := r.PathValue("attemptID")
	tag, err := a.db.Exec(r.Context(), `
		INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data)
		SELECT t.school_id,t.id,'incident',jsonb_build_object('note',$4::text,'by',$5::text)
		FROM assessment_attempts t WHERE t.id=$1 AND t.assignment_id=$2 AND t.school_id=$3`, attemptID, r.PathValue("assignmentID"), id.SchoolID, strings.TrimSpace(req.Note), id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "This student has not started the exam.", nil)
		return
	}
	if err = a.auditExam(r, id, "exam_attempt.incident", "assessment_attempt", attemptID, map[string]any{"note": strings.TrimSpace(req.Note)}); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]bool{"recorded": true}})
}

// listAttemptEvents returns the attempt's timeline for invigilator review,
// leaving out routine autosaves.
func (a *API) listAttemptEvents(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	if _, ok := a.sessionClass(w, r, id); !ok {
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT ev.event_type,ev.event_data,to_char(ev.occurred_at AT TIME ZONE 'UTC',`+isoTimestamp+`)
		FROM assessment_attempt_events ev
		JOIN assessment_attempts t ON t.id=ev.attempt_id AND t.school_id=ev.school_id
		WHERE ev.attempt_id=$1 AND t.assignment_id=$2 AND ev.school_id=$3 AND ev.event_type NOT IN ('answer_saved','answer_flagged')
		ORDER BY ev.occurred_at LIMIT 1000`, r.PathValue("attemptID"), r.PathValue("assignmentID"), id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type event struct {
		Type       string          `json:"event_type"`
		Data       json.RawMessage `json:"event_data"`
		OccurredAt string          `json:"occurred_at"`
	}
	items := []event{}
	for rows.Next() {
		var x event
		if err = rows.Scan(&x.Type, &x.Data, &x.OccurredAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

const maxIntegrityEventsPerAttempt = 500

var integrityEventTypes = map[string]bool{"tab_hidden": true, "window_blur": true, "fullscreen_exit": true, "paste_attempt": true, "copy_attempt": true}

// recordIntegrityEvent stores a signal from the locked exam page. These are
// signals for an invigilator to review, not proof of cheating, and a browser
// can always be made to stop sending them.
func (a *API) recordIntegrityEvent(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Type string `json:"type"`
	}
	if !decode(w, r, &req) || !integrityEventTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Unknown integrity event.", nil)
		return
	}
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusForbidden, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	tag, err := a.db.Exec(r.Context(), `
		INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data)
		SELECT t.school_id,t.id,'integrity_flag',jsonb_build_object('type',$4::text)
		FROM assessment_attempts t
		WHERE t.id=$1 AND t.school_id=$2 AND t.student_id=$3 AND t.status='in_progress'
		  AND (SELECT count(*) FROM assessment_attempt_events ev WHERE ev.attempt_id=t.id AND ev.event_type='integrity_flag') < $5`, r.PathValue("attemptID"), id.SchoolID, studentID, req.Type, maxIntegrityEventsPerAttempt)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"recorded": tag.RowsAffected() > 0}})
}

// getGradebook returns one row per active student with a cell per assessment
// assigned to the class.
func (a *API) getGradebook(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := strings.TrimSpace(r.URL.Query().Get("class_id"))
	if !uuidPattern.MatchString(classID) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	allowed, err := a.canManageClass(r.Context(), id, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You are not assigned to this class.", nil)
		return
	}
	columns, err := a.db.Query(r.Context(), `
		SELECT aa.id::text,a.title,COALESCE((SELECT sum(ai.points)::float8 FROM assessment_items ai WHERE ai.assessment_id=a.id),0),
		       to_char(aa.due_at AT TIME ZONE 'UTC',`+isoTimestamp+`)
		FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		WHERE aa.class_id=$1 AND aa.school_id=$2 ORDER BY aa.assigned_at`, classID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type column struct {
		AssignmentID string  `json:"assignment_id"`
		Title        string  `json:"title"`
		MaxPoints    float64 `json:"max_points"`
		DueAt        *string `json:"due_at"`
	}
	assignments := []column{}
	for columns.Next() {
		var x column
		if err = columns.Scan(&x.AssignmentID, &x.Title, &x.MaxPoints, &x.DueAt); err != nil {
			columns.Close()
			a.serverError(w, r, err)
			return
		}
		assignments = append(assignments, x)
	}
	if err = columns.Err(); err != nil {
		columns.Close()
		a.serverError(w, r, err)
		return
	}
	columns.Close()
	rows, err := a.db.Query(r.Context(), `
		SELECT s.id::text,s.student_number,s.legal_name,COALESCE(t.assignment_id::text,''),COALESCE(t.status,''),
		       t.score::float8,t.max_score::float8,t.final_score::float8,t.final_max_score::float8,COALESCE(t.results_released,FALSE),
		       COALESCE((SELECT count(*)::int FROM assessment_answers ans JOIN assessment_attempt_items item ON item.attempt_id=ans.attempt_id AND item.question_id=ans.question_id
		                 WHERE ans.attempt_id=t.id AND item.question_snapshot->>'question_type'='text' AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL),0)
		FROM enrollments e
		JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		LEFT JOIN assessment_attempts t ON t.student_id=s.id AND t.school_id=s.school_id
		     AND t.assignment_id IN (SELECT id FROM assessment_assignments WHERE class_id=$1 AND school_id=$2)
		WHERE e.class_id=$1 AND e.school_id=$2 AND e.status='active' AND s.status='active'
		ORDER BY s.legal_name,s.student_number`, classID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type cell struct {
		Status          string   `json:"status"`
		Score           *float64 `json:"score"`
		MaxScore        *float64 `json:"max_score"`
		FinalScore      *float64 `json:"final_score"`
		FinalMaxScore   *float64 `json:"final_max_score"`
		ResultsReleased bool     `json:"results_released"`
		PendingReview   int      `json:"pending_review"`
	}
	type student struct {
		StudentID     string          `json:"student_id"`
		StudentNumber string          `json:"student_number"`
		StudentName   string          `json:"student_name"`
		Results       map[string]cell `json:"results"`
	}
	students := []*student{}
	byID := map[string]*student{}
	for rows.Next() {
		var studentID, number, name, assignmentID string
		var c cell
		if err = rows.Scan(&studentID, &number, &name, &assignmentID, &c.Status, &c.Score, &c.MaxScore, &c.FinalScore, &c.FinalMaxScore, &c.ResultsReleased, &c.PendingReview); err != nil {
			a.serverError(w, r, err)
			return
		}
		row, seen := byID[studentID]
		if !seen {
			row = &student{StudentID: studentID, StudentNumber: number, StudentName: name, Results: map[string]cell{}}
			byID[studentID] = row
			students = append(students, row)
		}
		if assignmentID != "" {
			row.Results[assignmentID] = c
		}
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"class_id": classID, "assignments": assignments, "students": students}})
}
