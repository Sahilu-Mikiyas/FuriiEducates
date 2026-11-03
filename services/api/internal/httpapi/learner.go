package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const studentInviteTTL = 7 * 24 * time.Hour

func (a *API) registerLearnerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{studentID}/invitation", a.admin(a.createStudentInvitation))
	mux.HandleFunc("POST /api/v1/student-invitations/accept", a.limitByIP("token-redeem", a.limits.tokenRedemptionsPerIP, a.acceptStudentInvitation))
	mux.HandleFunc("GET /api/v1/student/assessments", a.student(a.listStudentAssessments))
	mux.HandleFunc("GET /api/v1/student/assessments/{assignmentID}", a.student(a.getStudentAssessment))
	mux.HandleFunc("POST /api/v1/student/assessments/{assignmentID}/start", a.student(a.startStudentAssessment))
	mux.HandleFunc("PUT /api/v1/student/attempts/{attemptID}/answers/{questionID}", a.student(a.saveStudentAnswer))
	mux.HandleFunc("POST /api/v1/student/attempts/{attemptID}/submit", a.student(a.submitStudentAssessment))
}

func (a *API) student(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action requires a student account.", "student")
}

func (a *API) createStudentInvitation(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		writeError(w, 400, "VALIDATION_ERROR", "Enter a valid email address.", nil)
		return
	}
	studentID := r.PathValue("studentID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var studentStatus string
	var existingUser *string
	err = tx.QueryRow(r.Context(), `SELECT status,user_id::text FROM students WHERE id=$1 AND school_id=$2 FOR UPDATE`, studentID, id.SchoolID).Scan(&studentStatus, &existingUser)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Student was not found in this school.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if studentStatus != "active" || existingUser != nil {
		writeError(w, 409, "STUDENT_INACTIVE", "Only active students without an existing account can receive an invitation.", nil)
		return
	}
	var accountExists bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE school_id=$1 AND lower(email)=lower($2))`, id.SchoolID, email).Scan(&accountExists); err != nil {
		a.serverError(w, r, err)
		return
	}
	if accountExists {
		writeError(w, 409, "ACCOUNT_EXISTS", "An account already uses this email in the school.", nil)
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		a.serverError(w, r, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(studentInviteTTL)
	if _, err = tx.Exec(r.Context(), `UPDATE student_invitations SET revoked_at=now() WHERE student_id=$1 AND school_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, studentID, id.SchoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	var inviteID string
	err = tx.QueryRow(r.Context(), `INSERT INTO student_invitations(school_id,student_id,email,token_hash,created_by,expires_at) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, id.SchoolID, studentID, email, tokenHash[:], id.UserID, expires).Scan(&inviteID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, 409, "INVITATION_EXISTS", "This student already has a pending invitation.", nil)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.invited','student_invitation',$3,jsonb_build_object('email',$4::text,'student_id',$5::text))`, id.SchoolID, id.UserID, inviteID, email, studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"id": inviteID, "email": email, "token": token, "expires_at": expires.Format(time.RFC3339)}})
}

func (a *API) acceptStudentInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if len(req.Token) < 32 || len(req.Token) > 128 || req.DisplayName == "" || len(req.DisplayName) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Invitation and display name are required.", nil)
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 1024 {
		writeError(w, 400, "VALIDATION_ERROR", "Password must contain at least 12 characters.", nil)
		return
	}
	hashToken := sha256.Sum256([]byte(req.Token))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var inviteID, schoolID, studentID, email string
	var expires time.Time
	err = tx.QueryRow(r.Context(), `SELECT id::text,school_id::text,student_id::text,email,expires_at FROM student_invitations WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL FOR UPDATE`, hashToken[:]).Scan(&inviteID, &schoolID, &studentID, &email, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !expires.After(time.Now())) {
		writeError(w, 400, "INVITATION_INVALID", "This invitation is invalid, expired, or already used.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var status string
	var existingUser *string
	err = tx.QueryRow(r.Context(), `SELECT status,user_id::text FROM students WHERE id=$1 AND school_id=$2 FOR UPDATE`, studentID, schoolID).Scan(&status, &existingUser)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" || existingUser != nil {
		writeError(w, 409, "STUDENT_INACTIVE", "This student cannot activate an account. Contact the school administrator.", nil)
		return
	}
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var userID string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(school_id,email,password_hash,display_name) VALUES($1,$2,$3,$4) RETURNING id::text`, schoolID, email, passwordHash, req.DisplayName).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, 409, "ACCOUNT_EXISTS", "An account already uses this email. Contact the school administrator.", nil)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,'student')`, userID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE students SET user_id=$1 WHERE id=$2 AND school_id=$3`, userID, studentID, schoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE student_invitations SET accepted_at=now() WHERE id=$1`, inviteID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,action,entity_type,entity_id,changes) VALUES($1,'student.invitation_accepted','user',$2,jsonb_build_object('email',$3::text,'student_id',$4::text))`, schoolID, userID, email, studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"email": email}})
}

func (a *API) learnerStudent(r *http.Request, id auth.Identity) (string, error) {
	var studentID string
	err := a.db.QueryRow(r.Context(), `SELECT id::text FROM students WHERE user_id=$1 AND school_id=$2 AND status='active'`, id.UserID, id.SchoolID).Scan(&studentID)
	return studentID, err
}

func (a *API) listStudentAssessments(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT aa.id::text,a.title,a.instructions,c.name,c.grade_level,aa.assigned_at::text,COALESCE(aa.due_at::text,''),COALESCE(at.status,'not_started'),COALESCE(at.id::text,'') FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id JOIN classes c ON c.id=aa.class_id AND c.school_id=aa.school_id JOIN enrollments e ON e.class_id=c.id AND e.school_id=c.school_id AND e.student_id=$2 AND e.status='active' JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id AND s.status='active' LEFT JOIN assessment_attempts at ON at.assignment_id=aa.id AND at.student_id=$2 WHERE aa.school_id=$1 AND a.status='published' AND (aa.stream IS NULL OR aa.stream=s.stream OR at.id IS NOT NULL) ORDER BY aa.assigned_at DESC LIMIT 100`, id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var assignment, title, instructions, className, grade, assigned, due, status, attemptID string
		if err = rows.Scan(&assignment, &title, &instructions, &className, &grade, &assigned, &due, &status, &attemptID); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{"assignment_id": assignment, "title": title, "instructions": instructions, "class_name": className, "grade_level": grade, "assigned_at": assigned, "due_at": due, "attempt_status": status, "attempt_id": attemptID})
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

// studentAssignmentView is an assignment as a student in its class may see it.
type studentAssignmentView struct {
	StudentID         string
	Title             string
	Instructions      string
	DurationMinutes   int
	QuestionCount     int
	RequiresStartCode bool
	SessionOpen       bool
	StartCode         *string
	LockedMode        bool
	NationalPaper     *string
	OpensAt           *string
	DueAt             *string
	NotOpenYet        bool
	PastDue           bool
	Kind              string
	ResourceID        string
	ResourceTitle     string
	ShowAnswers       bool
}

// studentAssignment loads the assignment for the signed-in student, writing
// the error response and returning false when they cannot see it.
func (a *API) studentAssignment(w http.ResponseWriter, r *http.Request, id auth.Identity) (studentAssignmentView, bool) {
	var view studentAssignmentView
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return view, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return view, false
	}
	view.StudentID = studentID
	err = a.db.QueryRow(r.Context(), `
		SELECT a.title,a.instructions,a.duration_minutes,
		       (SELECT count(*)::int FROM assessment_items ai WHERE ai.assessment_id=a.id),
		       aa.requires_start_code,aa.session_open,aa.start_code,a.locked_mode,a.national_paper,
		       to_char(aa.opens_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       to_char(aa.due_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
		       COALESCE(aa.opens_at>now(),FALSE),COALESCE(aa.due_at<=now(),FALSE),
		       a.kind,COALESCE(a.resource_id::text,''),COALESCE(tr.title,''),a.show_answers
		FROM assessment_assignments aa
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		LEFT JOIN teaching_resources tr ON tr.id=a.resource_id
		JOIN enrollments e ON e.class_id=aa.class_id AND e.school_id=aa.school_id AND e.student_id=$3 AND e.status='active'
		JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		WHERE aa.id=$1 AND aa.school_id=$2 AND a.status='published'
		AND (aa.stream IS NULL OR aa.stream=s.stream OR EXISTS(SELECT 1 FROM assessment_attempts t WHERE t.assignment_id=aa.id AND t.student_id=s.id))`, r.PathValue("assignmentID"), id.SchoolID, studentID).Scan(
		&view.Title, &view.Instructions, &view.DurationMinutes, &view.QuestionCount, &view.RequiresStartCode, &view.SessionOpen, &view.StartCode, &view.LockedMode, &view.NationalPaper, &view.OpensAt, &view.DueAt, &view.NotOpenYet, &view.PastDue, &view.Kind, &view.ResourceID, &view.ResourceTitle, &view.ShowAnswers)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "This assessment is not assigned to your active class.", nil)
		return view, false
	}
	if err != nil {
		a.serverError(w, r, err)
		return view, false
	}
	return view, true
}

// getStudentAssessment returns the assignment summary. Questions are only
// included once the student has an attempt, so nobody can read an exam before
// its timer starts. An attempt past its deadline is finalized first.
func (a *API) getStudentAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	view, ok := a.studentAssignment(w, r, id)
	if !ok {
		return
	}
	assignmentID := r.PathValue("assignmentID")
	var expiredAttempt string
	err := a.db.QueryRow(r.Context(), `SELECT id::text FROM assessment_attempts WHERE assignment_id=$1 AND student_id=$2 AND status='in_progress' AND `+attemptExpiredSQL, assignmentID, view.StudentID).Scan(&expiredAttempt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	if expiredAttempt != "" {
		if err := finalizeExpiredAttempt(r.Context(), a.db, id.SchoolID, expiredAttempt); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	var attemptID, status string
	var deadlineAt *string
	var score, maxScore *float64
	var manualReview int
	var resultsReleased bool
	var finalScore, finalMax *float64
	err = a.db.QueryRow(r.Context(), `SELECT id::text,status,score::float8,max_score::float8,(SELECT count(*)::int FROM assessment_answers WHERE attempt_id=assessment_attempts.id AND is_correct IS NULL AND answer IS NOT NULL AND answer <> 'null'::jsonb AND reviewed_at IS NULL),results_released,final_score::float8,final_max_score::float8,to_char(deadline_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"') FROM assessment_attempts WHERE assignment_id=$1 AND student_id=$2`, assignmentID, view.StudentID).Scan(&attemptID, &status, &score, &maxScore, &manualReview, &resultsReleased, &finalScore, &finalMax, &deadlineAt)
	if errors.Is(err, pgx.ErrNoRows) {
		attemptID = ""
		status = "not_started"
		resultsReleased = false
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	type assessmentQuestion struct {
		ID       string          `json:"id"`
		Type     string          `json:"question_type"`
		Prompt   json.RawMessage `json:"prompt"`
		Options  json.RawMessage `json:"options,omitempty"`
		Points   string          `json:"points"`
		Answer   json.RawMessage `json:"answer,omitempty"`
		Awarded  string          `json:"awarded_points,omitempty"`
		Feedback string          `json:"feedback,omitempty"`
		Revision int             `json:"revision"`
		Flagged  bool            `json:"flagged"`
		// Only for a teacher's quiz, once answers can be shown.
		Correct     json.RawMessage `json:"correct_answer,omitempty"`
		IsCorrect   *bool           `json:"is_correct,omitempty"`
		Explanation *string         `json:"explanation,omitempty"`
	}
	// A quiz shows its answers after the deadline, so nobody still working sees
	// them from a friend. A written question's marking guide stays private.
	reviewable := view.Kind != "exam" && view.ShowAnswers && resultsReleased && (view.DueAt == nil || view.PastDue)
	questions := []assessmentQuestion{}
	if attemptID != "" {
		rows, err := a.db.Query(r.Context(), `
			SELECT item.question_id::text,item.question_snapshot->>'question_type',item.question_snapshot->'prompt',item.question_snapshot->'options',item.points::text,
			       answers.answer,
			       CASE WHEN $2 THEN answers.awarded_points::text ELSE '' END,
			       CASE WHEN $2 THEN answers.feedback ELSE '' END,
			       COALESCE(answers.revision,0),COALESCE(answers.flagged,FALSE),
			       CASE WHEN $3 AND item.question_snapshot->>'question_type'<>'text' THEN item.question_snapshot->'answer_key' END,
			       CASE WHEN $3 THEN answers.is_correct END,
			       CASE WHEN $3 THEN q.explanation->>'text' END
			FROM assessment_attempt_items item
			JOIN questions q ON q.id=item.question_id
			LEFT JOIN assessment_answers answers ON answers.attempt_id=item.attempt_id AND answers.question_id=item.question_id
			WHERE item.attempt_id=$1
			ORDER BY item.position`, attemptID, resultsReleased, reviewable)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var q assessmentQuestion
			var awarded, feedback *string
			if err = rows.Scan(&q.ID, &q.Type, &q.Prompt, &q.Options, &q.Points, &q.Answer, &awarded, &feedback, &q.Revision, &q.Flagged, &q.Correct, &q.IsCorrect, &q.Explanation); err != nil {
				a.serverError(w, r, err)
				return
			}
			if awarded != nil {
				q.Awarded = *awarded
			}
			if feedback != nil {
				q.Feedback = *feedback
			}
			questions = append(questions, q)
		}
		if err = rows.Err(); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{
		"assignment_id": assignmentID, "title": view.Title, "instructions": view.Instructions, "status": status, "attempt_id": attemptID,
		"deadline_at": deadlineAt, "score": score, "max_score": maxScore, "results_released": resultsReleased,
		"final_score": finalScore, "final_max_score": finalMax, "manual_review_count": manualReview, "questions": questions,
		"question_count": view.QuestionCount, "duration_minutes": view.DurationMinutes, "requires_start_code": view.RequiresStartCode,
		"session_open": view.SessionOpen, "locked_mode": view.LockedMode, "national_paper": view.NationalPaper, "opens_at": view.OpensAt, "due_at": view.DueAt,
		"not_open_yet": view.NotOpenYet, "past_due": view.PastDue, "kind": view.Kind, "resource_id": view.ResourceID, "resource_title": view.ResourceTitle,
		"answers_after_due": view.Kind != "exam" && view.ShowAnswers && view.DueAt != nil && !view.PastDue,
	}})
}

// startStudentAssessment creates the attempt, snapshotting the questions and
// setting the server deadline. A student who already started gets the same
// attempt back with no further checks, so a reconnecting student is never
// locked out by a closed session or a changed start code.
func (a *API) startStudentAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		StartCode string `json:"start_code"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid start request.", nil)
		return
	}
	view, ok := a.studentAssignment(w, r, id)
	if !ok {
		return
	}
	assignmentID := r.PathValue("assignmentID")
	respond := func(attemptID, status, deadline string) {
		writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"attempt_id": attemptID, "status": status, "deadline_at": deadline}})
	}
	const deadlineText = `to_char(deadline_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')`
	var attemptID, status, deadline string
	err := a.db.QueryRow(r.Context(), `SELECT id::text,status,`+deadlineText+` FROM assessment_attempts WHERE assignment_id=$1 AND student_id=$2`, assignmentID, view.StudentID).Scan(&attemptID, &status, &deadline)
	if err == nil {
		respond(attemptID, status, deadline)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	if view.NotOpenYet {
		writeError(w, http.StatusConflict, "ASSESSMENT_NOT_OPEN", "This assessment has not opened yet.", nil)
		return
	}
	if view.PastDue {
		writeError(w, http.StatusConflict, "ASSESSMENT_CLOSED", "The time to start this assessment has passed.", nil)
		return
	}
	if view.RequiresStartCode {
		if !view.SessionOpen || view.StartCode == nil {
			writeError(w, http.StatusConflict, "SESSION_NOT_OPEN", "Wait for your invigilator to open this exam session.", nil)
			return
		}
		failureKey := "start-code:" + id.UserID
		if blocked, retry := a.limiter.blocked(failureKey, a.limits.startCodeFailuresPerUser, time.Now()); blocked {
			writeRateLimited(w, retry)
			return
		}
		if !strings.EqualFold(strings.TrimSpace(req.StartCode), *view.StartCode) {
			a.limiter.hit(failureKey, a.limits.startCodeFailuresPerUser, time.Now())
			writeError(w, http.StatusForbidden, "START_CODE_INVALID", "That start code is not correct. Check the code shown by your invigilator.", nil)
			return
		}
		a.limiter.reset(failureKey)
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	err = tx.QueryRow(r.Context(), `
		INSERT INTO assessment_attempts(school_id,assignment_id,student_id,max_score,deadline_at)
		SELECT aa.school_id,aa.id,$3,
		       (SELECT COALESCE(sum(ai.points),0) FROM assessment_items ai JOIN questions q ON q.id=ai.question_id AND q.school_id=ai.school_id WHERE ai.assessment_id=aa.assessment_id AND q.question_type<>'text'),
		       CASE WHEN aa.due_at IS NOT NULL THEN LEAST(now()+make_interval(mins=>a.duration_minutes),aa.due_at) ELSE now()+make_interval(mins=>a.duration_minutes) END
		FROM assessment_assignments aa
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		WHERE aa.id=$1 AND aa.school_id=$2 AND a.status='published'
		ON CONFLICT(assignment_id,student_id) DO UPDATE SET assignment_id=EXCLUDED.assignment_id
		RETURNING id::text,status,`+deadlineText, assignmentID, id.SchoolID, view.StudentID).Scan(&attemptID, &status, &deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "This assessment is not assigned to your active class.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	// Each attempt gets its own question order and answer-option order when the
	// assessment asks for shuffling. Grading uses option IDs, so order is safe.
	_, err = tx.Exec(r.Context(), `
		INSERT INTO assessment_attempt_items(attempt_id,school_id,question_id,position,points,question_snapshot)
		SELECT at.id,at.school_id,ai.question_id,
		       (CASE WHEN a.shuffle_questions THEN row_number() OVER (ORDER BY random()) ELSE row_number() OVER (ORDER BY ai.position) END)::int,
		       ai.points,
		       jsonb_build_object('question_type',q.question_type,'prompt',q.prompt,
		           'options',CASE WHEN a.shuffle_options AND jsonb_typeof(q.options)='array' THEN (SELECT jsonb_agg(option_item ORDER BY random()) FROM jsonb_array_elements(q.options) AS option_item) ELSE q.options END,
		           'answer_key',q.answer_key,'explanation',q.explanation)
		FROM assessment_attempts at
		JOIN assessment_assignments aa ON aa.id=at.assignment_id AND aa.school_id=at.school_id
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		JOIN assessment_items ai ON ai.assessment_id=aa.assessment_id AND ai.school_id=aa.school_id
		JOIN questions q ON q.id=ai.question_id AND q.school_id=ai.school_id
		WHERE at.id=$1
		ON CONFLICT(attempt_id,question_id) DO NOTHING`, attemptID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,'started',jsonb_build_object('deadline_at',$3::text))`, id.SchoolID, attemptID, deadline); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	respond(attemptID, status, deadline)
}

func (a *API) saveStudentAnswer(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Answer    json.RawMessage `json:"answer"`
		Revision  int             `json:"revision"`
		RequestID string          `json:"idempotency_key"`
		Flagged   bool            `json:"flagged"`
	}
	if !decode(w, r, &req) || len(req.Answer) == 0 || !json.Valid(req.Answer) || req.Revision < 0 || (req.RequestID != "" && !uuidPattern.MatchString(req.RequestID)) {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a valid JSON answer and a UUID idempotency key.", nil)
		return
	}
	req.RequestID = strings.ToLower(req.RequestID)
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	var expired bool
	err = tx.QueryRow(r.Context(), `SELECT status,`+attemptExpiredSQL+` FROM assessment_attempts WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, r.PathValue("attemptID"), id.SchoolID, studentID).Scan(&status, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Assessment attempt not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "in_progress" {
		writeError(w, 409, "ATTEMPT_SUBMITTED", "This attempt has already been submitted.", nil)
		return
	}
	if expired {
		writeError(w, 409, "ATTEMPT_DEADLINE_PASSED", "The deadline has passed. Your last server-saved answers will be submitted.", nil)
		return
	}
	var allowed bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM assessment_attempt_items WHERE attempt_id=$1 AND school_id=$2 AND question_id=$3)`, r.PathValue("attemptID"), id.SchoolID, r.PathValue("questionID")).Scan(&allowed); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 404, "NOT_FOUND", "Question was not found in this assessment attempt.", nil)
		return
	}
	var currentRevision int
	var currentRequest string
	err = tx.QueryRow(r.Context(), `SELECT revision,COALESCE(idempotency_key::text,'') FROM assessment_answers WHERE attempt_id=$1 AND question_id=$2`, r.PathValue("attemptID"), r.PathValue("questionID")).Scan(&currentRevision, &currentRequest)
	if errors.Is(err, pgx.ErrNoRows) {
		currentRevision = 0
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	if req.RequestID != "" && req.RequestID == currentRequest {
		if err = tx.Commit(r.Context()); err != nil {
			a.serverError(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"saved": true, "revision": currentRevision, "idempotent_replay": true}})
		return
	}
	if req.Revision == 0 {
		req.Revision = currentRevision + 1
	}
	if req.Revision <= currentRevision {
		writeError(w, 409, "STALE_REVISION", "A newer answer is already saved. Reload the assessment to reconcile it.", map[string]string{"current_revision": strconv.Itoa(currentRevision)})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO assessment_answers(attempt_id,school_id,question_id,answer,revision,idempotency_key,flagged,saved_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,'')::uuid,$7,now()) ON CONFLICT(attempt_id,question_id) DO UPDATE SET answer=EXCLUDED.answer,revision=EXCLUDED.revision,idempotency_key=EXCLUDED.idempotency_key,flagged=EXCLUDED.flagged,saved_at=now() WHERE assessment_answers.is_correct IS NULL AND EXCLUDED.revision>assessment_answers.revision`, r.PathValue("attemptID"), id.SchoolID, r.PathValue("questionID"), []byte(req.Answer), req.Revision, req.RequestID, req.Flagged)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	eventType := "answer_saved"
	if req.Flagged {
		eventType = "answer_flagged"
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,$3,jsonb_build_object('question_id',$4::text,'revision',$5::int,'flagged',$6::boolean))`, id.SchoolID, r.PathValue("attemptID"), eventType, r.PathValue("questionID"), req.Revision, req.Flagged)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"saved": true, "revision": req.Revision}})
}

func (a *API) submitStudentAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid answer submission.", nil)
		return
	}
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	attemptID := r.PathValue("attemptID")
	var status string
	var expired bool
	err = tx.QueryRow(r.Context(), `SELECT status,`+attemptExpiredSQL+` FROM assessment_attempts WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, attemptID, id.SchoolID, studentID).Scan(&status, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Assessment attempt not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "in_progress" {
		var score, maxScore *float64
		var released bool
		if err = tx.QueryRow(r.Context(), `SELECT score::float8,max_score::float8,results_released FROM assessment_attempts WHERE id=$1`, attemptID).Scan(&score, &maxScore, &released); err != nil {
			a.serverError(w, r, err)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			a.serverError(w, r, err)
			return
		}
		writeJSON(w, 200, map[string]any{"data": map[string]any{"attempt_id": attemptID, "status": "submitted", "auto_score": score, "auto_max_score": maxScore, "results_released": released, "idempotent_replay": true}})
		return
	}
	// After the deadline and grace window only server-saved answers count.
	payload := req.Answers
	eventType := "submitted"
	if expired {
		payload = nil
		eventType = "deadline_submission"
	}
	actor := id.UserID
	result, err := finalizeAttempt(r.Context(), tx, id.SchoolID, attemptID, &actor, payload, eventType)
	var invalid *invalidAnswerError
	if errors.As(err, &invalid) {
		writeError(w, 400, "INVALID_ANSWER", invalid.message, nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	var finalScore, finalMaxScore any
	if result.NeedsReview == 0 {
		finalScore, finalMaxScore = result.Total, result.FinalMax
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"attempt_id": attemptID, "status": "submitted", "auto_score": result.Total, "auto_max_score": result.Max, "manual_review_count": result.NeedsReview, "results_released": result.NeedsReview == 0, "final_score": finalScore, "final_max_score": finalMaxScore, "deadline_submission": expired}})
}

// attemptExpiredSQL is true once an attempt's deadline and grace window have
// passed. The grace window lets answers already on their way when the timer
// reaches zero still count; the student's clock stops at the deadline itself.
const attemptExpiredSQL = `now() >= deadline_at + interval '30 seconds'`

type attemptResult struct {
	Total, Max, FinalMax float64
	NeedsReview          int
}

type invalidAnswerError struct{ message string }

func (e *invalidAnswerError) Error() string { return e.message }

// finalizeAttempt grades an in-progress attempt from its question snapshot
// and marks it submitted. The caller must hold the attempt's row lock in tx.
// payload answers, when given, override saved answers. actor is nil when the
// server finalizes an abandoned attempt.
func finalizeAttempt(ctx context.Context, tx pgx.Tx, schoolID, attemptID string, actor *string, payload map[string]json.RawMessage, eventType string) (attemptResult, error) {
	var result attemptResult
	stored, err := tx.Query(ctx, `SELECT question_id::text,answer FROM assessment_answers WHERE attempt_id=$1 AND answer IS NOT NULL`, attemptID)
	if err != nil {
		return result, err
	}
	answers := map[string]json.RawMessage{}
	for stored.Next() {
		var questionID string
		var answer json.RawMessage
		if err = stored.Scan(&questionID, &answer); err != nil {
			stored.Close()
			return result, err
		}
		answers[questionID] = answer
	}
	if err = stored.Err(); err != nil {
		stored.Close()
		return result, err
	}
	stored.Close()
	for questionID, answer := range payload {
		answers[questionID] = answer
	}
	rows, err := tx.Query(ctx, `SELECT question_id::text,question_snapshot->>'question_type',question_snapshot->'answer_key',points::float8 FROM assessment_attempt_items WHERE attempt_id=$1 AND school_id=$2 ORDER BY position`, attemptID, schoolID)
	if err != nil {
		return result, err
	}
	type gradingItem struct {
		id, kind string
		key      []byte
		points   float64
	}
	items := []gradingItem{}
	for rows.Next() {
		var item gradingItem
		if err = rows.Scan(&item.id, &item.kind, &item.key, &item.points); err != nil {
			rows.Close()
			return result, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	valid := map[string]bool{}
	for _, item := range items {
		valid[item.id] = true
	}
	for questionID := range answers {
		if !valid[questionID] {
			return result, &invalidAnswerError{"Submission contains a question outside this assessment."}
		}
	}
	for _, item := range items {
		answer := bytesTrimJSON(answers[item.id])
		if len(answer) == 0 {
			answer = []byte("null")
		}
		if !json.Valid(answer) {
			return result, &invalidAnswerError{"An answer is not valid JSON."}
		}
		correct, manual := gradeAnswer(item.kind, item.key, answer)
		if manual {
			result.NeedsReview++
		}
		awarded := 0.0
		if correct {
			awarded = item.points
		}
		if !manual {
			result.Max += item.points
		}
		result.FinalMax += item.points
		result.Total += awarded
		var isCorrect any
		if !manual {
			isCorrect = correct
		}
		if _, err = tx.Exec(ctx, `INSERT INTO assessment_answers(attempt_id,school_id,question_id,answer,is_correct,awarded_points,saved_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(attempt_id,question_id) DO UPDATE SET answer=EXCLUDED.answer,is_correct=EXCLUDED.is_correct,awarded_points=EXCLUDED.awarded_points,saved_at=now()`, attemptID, schoolID, item.id, answer, isCorrect, awarded); err != nil {
			return result, err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE assessment_attempts SET status='submitted',submitted_at=now(),score=$2::numeric,max_score=$3::numeric,results_released=($4::int=0),final_score=CASE WHEN $4::int=0 THEN $2::numeric ELSE NULL END,final_max_score=CASE WHEN $4::int=0 THEN $5::numeric ELSE NULL END WHERE id=$1`, attemptID, result.Total, result.Max, result.NeedsReview, result.FinalMax); err != nil {
		return result, err
	}
	if err = updateLearningIntelligence(ctx, tx, schoolID, attemptID); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.submitted','assessment_attempt',$3,jsonb_build_object('auto_score',$4::numeric,'auto_max_score',$5::numeric,'manual_review_count',$6::int,'event',$7::text))`, schoolID, actor, attemptID, result.Total, result.Max, result.NeedsReview, eventType); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,$3,jsonb_build_object('auto_score',$4::numeric,'manual_review_count',$5::int))`, schoolID, attemptID, eventType, result.Total, result.NeedsReview); err != nil {
		return result, err
	}
	return result, nil
}

// finalizeExpiredAttempt grades one attempt whose deadline has passed, using
// only server-saved answers. Another request holding the row lock wins; this
// call then does nothing.
func finalizeExpiredAttempt(ctx context.Context, db *pgxpool.Pool, schoolID, attemptID string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var status string
	var expired bool
	err = tx.QueryRow(ctx, `SELECT status,`+attemptExpiredSQL+` FROM assessment_attempts WHERE id=$1 AND school_id=$2 FOR UPDATE SKIP LOCKED`, attemptID, schoolID).Scan(&status, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "in_progress" || !expired {
		return nil
	}
	if _, err = finalizeAttempt(ctx, tx, schoolID, attemptID, nil, nil, "deadline_submission"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// finalizeExpiredAttempts grades a batch of abandoned attempts, so a student
// who closed the tab still gets a result. Returns how many were examined.
func finalizeExpiredAttempts(ctx context.Context, db *pgxpool.Pool) (int, error) {
	rows, err := db.Query(ctx, `SELECT id::text,school_id::text FROM assessment_attempts WHERE status='in_progress' AND `+attemptExpiredSQL+` ORDER BY deadline_at LIMIT 200`)
	if err != nil {
		return 0, err
	}
	type target struct{ attemptID, schoolID string }
	targets := []target{}
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.attemptID, &item.schoolID); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	var firstErr error
	for _, item := range targets {
		if err := finalizeExpiredAttempt(ctx, db, item.schoolID, item.attemptID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return len(targets), firstErr
}

func bytesTrimJSON(raw json.RawMessage) []byte { return []byte(strings.TrimSpace(string(raw))) }

func gradeAnswer(kind string, key, answer []byte) (bool, bool) {
	switch kind {
	case "single_choice", "multiple_choice":
		var expected, selected []string
		if json.Unmarshal(key, &expected) != nil {
			return false, false
		}
		if json.Unmarshal(answer, &selected) != nil {
			var one string
			if json.Unmarshal(answer, &one) != nil {
				return false, false
			}
			selected = []string{one}
		}
		sort.Strings(expected)
		sort.Strings(selected)
		if len(expected) != len(selected) {
			return false, false
		}
		for i := range expected {
			if expected[i] != selected[i] {
				return false, false
			}
		}
		return true, false
	case "numeric":
		if string(answer) == "null" {
			return false, false
		}
		var expected struct {
			Value     float64 `json:"value"`
			Tolerance float64 `json:"tolerance"`
		}
		var got float64
		if json.Unmarshal(key, &expected) != nil {
			return false, false
		}
		if json.Unmarshal(answer, &got) != nil {
			var text string
			if json.Unmarshal(answer, &text) != nil {
				return false, false
			}
			parsed, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if err != nil {
				return false, false
			}
			got = parsed
		}
		return !math.IsNaN(got) && !math.IsInf(got, 0) && math.Abs(got-expected.Value) <= expected.Tolerance, false
	case "text":
		return false, string(answer) != "null"
	default:
		return false, false
	}
}
