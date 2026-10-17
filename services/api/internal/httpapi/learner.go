package httpapi

import (
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
)

const studentInviteTTL = 7 * 24 * time.Hour

func (a *API) registerLearnerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{studentID}/invitation", a.admin(a.createStudentInvitation))
	mux.HandleFunc("POST /api/v1/student-invitations/accept", a.acceptStudentInvitation)
	mux.HandleFunc("GET /api/v1/student/assessments", a.student(a.listStudentAssessments))
	mux.HandleFunc("GET /api/v1/student/assessments/{assignmentID}", a.student(a.getStudentAssessment))
	mux.HandleFunc("POST /api/v1/student/assessments/{assignmentID}/start", a.student(a.startStudentAssessment))
	mux.HandleFunc("PUT /api/v1/student/attempts/{attemptID}/answers/{questionID}", a.student(a.saveStudentAnswer))
	mux.HandleFunc("POST /api/v1/student/attempts/{attemptID}/submit", a.student(a.submitStudentAssessment))
}

func (a *API) student(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
			return
		}
		identity, err := a.auth.Identity(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication is required.", nil)
			} else {
				a.serverError(w, r, err)
			}
			return
		}
		if !hasRole(identity.Roles, "student") {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "This action requires a student account.", nil)
			return
		}
		next(w, r, identity)
	}
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
	rows, err := a.db.Query(r.Context(), `SELECT aa.id::text,a.title,a.instructions,c.name,c.grade_level,aa.assigned_at::text,COALESCE(aa.due_at::text,''),COALESCE(at.status,'not_started'),COALESCE(at.id::text,'') FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id JOIN classes c ON c.id=aa.class_id AND c.school_id=aa.school_id JOIN enrollments e ON e.class_id=c.id AND e.school_id=c.school_id AND e.student_id=$2 AND e.status='active' JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id AND s.status='active' LEFT JOIN assessment_attempts at ON at.assignment_id=aa.id AND at.student_id=$2 WHERE aa.school_id=$1 AND a.status='published' ORDER BY aa.assigned_at DESC LIMIT 100`, id.SchoolID, studentID)
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

func (a *API) studentAssignment(w http.ResponseWriter, r *http.Request, id auth.Identity) (string, string) {
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return "", ""
	}
	if err != nil {
		a.serverError(w, r, err)
		return "", ""
	}
	var title, instructions string
	err = a.db.QueryRow(r.Context(), `SELECT a.title,a.instructions FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id JOIN enrollments e ON e.class_id=aa.class_id AND e.school_id=aa.school_id AND e.student_id=$3 AND e.status='active' WHERE aa.id=$1 AND aa.school_id=$2 AND a.status='published'`, r.PathValue("assignmentID"), id.SchoolID, studentID).Scan(&title, &instructions)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "This assessment is not assigned to your active class.", nil)
		return "", ""
	}
	if err != nil {
		a.serverError(w, r, err)
		return "", ""
	}
	return studentID, title + "\x00" + instructions
}

func (a *API) getStudentAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, combined := a.studentAssignment(w, r, id)
	if studentID == "" {
		return
	}
	title, instructions, _ := strings.Cut(combined, "\x00")
	var attemptID, status string
	var deadlineAt *string
	var score, maxScore *float64
	var manualReview int
	var resultsReleased bool
	var finalScore, finalMax *float64
	err := a.db.QueryRow(r.Context(), `SELECT id::text,status,score::float8,max_score::float8,(SELECT count(*)::int FROM assessment_answers WHERE attempt_id=assessment_attempts.id AND is_correct IS NULL AND answer IS NOT NULL AND answer <> 'null'::jsonb AND reviewed_at IS NULL),results_released,final_score::float8,final_max_score::float8,deadline_at::text FROM assessment_attempts WHERE assignment_id=$1 AND student_id=$2`, r.PathValue("assignmentID"), studentID).Scan(&attemptID, &status, &score, &maxScore, &manualReview, &resultsReleased, &finalScore, &finalMax, &deadlineAt)
	if errors.Is(err, pgx.ErrNoRows) {
		attemptID = ""
		status = "not_started"
		resultsReleased = false
	} else if err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT q.id::text,COALESCE(snapshot.question_snapshot->>'question_type',q.question_type),COALESCE(snapshot.question_snapshot->'prompt',q.prompt),COALESCE(snapshot.question_snapshot->'options',q.options),COALESCE(snapshot.points::text,ai.points::text),answers.answer,CASE WHEN at.results_released THEN answers.awarded_points::text ELSE '' END,CASE WHEN at.results_released THEN answers.feedback ELSE '' END,COALESCE(answers.revision,0),COALESCE(answers.flagged,FALSE) FROM assessment_items ai JOIN questions q ON q.id=ai.question_id AND q.school_id=ai.school_id LEFT JOIN assessment_attempts at ON at.assignment_id=$1 AND at.student_id=$3 LEFT JOIN assessment_attempt_items snapshot ON snapshot.attempt_id=at.id AND snapshot.question_id=q.id LEFT JOIN assessment_answers answers ON answers.attempt_id=at.id AND answers.question_id=q.id WHERE ai.assessment_id=(SELECT assessment_id FROM assessment_assignments WHERE id=$1 AND school_id=$2) ORDER BY COALESCE(snapshot.position,ai.position)`, r.PathValue("assignmentID"), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
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
	}
	questions := []assessmentQuestion{}
	for rows.Next() {
		var q assessmentQuestion
		if err = rows.Scan(&q.ID, &q.Type, &q.Prompt, &q.Options, &q.Points, &q.Answer, &q.Awarded, &q.Feedback, &q.Revision, &q.Flagged); err != nil {
			a.serverError(w, r, err)
			return
		}
		questions = append(questions, q)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"assignment_id": r.PathValue("assignmentID"), "title": title, "instructions": instructions, "status": status, "attempt_id": attemptID, "deadline_at": deadlineAt, "score": score, "max_score": maxScore, "results_released": resultsReleased, "final_score": finalScore, "final_max_score": finalMax, "manual_review_count": manualReview, "questions": questions}})
}

func (a *API) startStudentAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
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
	var alreadyStarted bool
	if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM assessment_attempts WHERE assignment_id=$1 AND student_id=$2)`, r.PathValue("assignmentID"), studentID).Scan(&alreadyStarted); err != nil {
		a.serverError(w, r, err)
		return
	}
	var attemptID, status, deadline string
	err = tx.QueryRow(r.Context(), `INSERT INTO assessment_attempts(school_id,assignment_id,student_id,max_score,deadline_at) SELECT aa.school_id,aa.id,$3,(SELECT COALESCE(sum(ai.points),0) FROM assessment_items ai JOIN questions q ON q.id=ai.question_id AND q.school_id=ai.school_id WHERE ai.assessment_id=aa.assessment_id AND q.question_type<>'text'),CASE WHEN aa.due_at IS NOT NULL THEN LEAST(now()+make_interval(mins=>a.duration_minutes),aa.due_at) ELSE now()+make_interval(mins=>a.duration_minutes) END FROM assessment_assignments aa JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id JOIN enrollments e ON e.class_id=aa.class_id AND e.school_id=aa.school_id AND e.student_id=$3 AND e.status='active' WHERE aa.id=$1 AND aa.school_id=$2 AND a.status='published' AND (aa.due_at IS NULL OR aa.due_at>now()) ON CONFLICT(assignment_id,student_id) DO UPDATE SET assignment_id=EXCLUDED.assignment_id RETURNING id::text,status,deadline_at::text`, r.PathValue("assignmentID"), id.SchoolID, studentID).Scan(&attemptID, &status, &deadline)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "This assessment is not assigned to your active class.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status == "in_progress" {
		_, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_items(attempt_id,school_id,question_id,position,points,question_snapshot) SELECT at.id,at.school_id,ai.question_id,ai.position,ai.points,jsonb_build_object('question_type',q.question_type,'prompt',q.prompt,'options',q.options,'answer_key',q.answer_key,'explanation',q.explanation) FROM assessment_attempts at JOIN assessment_assignments aa ON aa.id=at.assignment_id AND aa.school_id=at.school_id JOIN assessment_items ai ON ai.assessment_id=aa.assessment_id AND ai.school_id=aa.school_id JOIN questions q ON q.id=ai.question_id AND q.school_id=ai.school_id WHERE at.id=$1 ON CONFLICT(attempt_id,question_id) DO NOTHING`, attemptID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if !alreadyStarted {
			_, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,'started',jsonb_build_object('deadline_at',$3::text))`, id.SchoolID, attemptID, deadline)
			if err != nil {
				a.serverError(w, r, err)
				return
			}
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"attempt_id": attemptID, "status": status, "deadline_at": deadline})
}

func (a *API) saveStudentAnswer(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Answer    json.RawMessage `json:"answer"`
		Revision  int             `json:"revision"`
		RequestID string          `json:"idempotency_key"`
		Flagged   bool            `json:"flagged"`
	}
	if !decode(w, r, &req) || len(req.Answer) == 0 || !json.Valid(req.Answer) || req.Revision < 0 || len(req.RequestID) > 128 {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a valid JSON answer.", nil)
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
	defer tx.Rollback(r.Context())
	var status string
	var expired bool
	err = tx.QueryRow(r.Context(), `SELECT status,now()>=deadline_at FROM assessment_attempts WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, r.PathValue("attemptID"), id.SchoolID, studentID).Scan(&status, &expired)
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
	err = tx.QueryRow(r.Context(), `SELECT revision,COALESCE(idempotency_key,'') FROM assessment_answers WHERE attempt_id=$1 AND question_id=$2`, r.PathValue("attemptID"), r.PathValue("questionID")).Scan(&currentRevision, &currentRequest)
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
		writeError(w, 409, "STALE_REVISION", "A newer answer is already saved. Reload the assessment to reconcile it.", map[string]int{"current_revision": currentRevision})
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO assessment_answers(attempt_id,school_id,question_id,answer,revision,idempotency_key,flagged,saved_at) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7,now()) ON CONFLICT(attempt_id,question_id) DO UPDATE SET answer=EXCLUDED.answer,revision=EXCLUDED.revision,idempotency_key=EXCLUDED.idempotency_key,flagged=EXCLUDED.flagged,saved_at=now() WHERE assessment_answers.is_correct IS NULL AND EXCLUDED.revision>assessment_answers.revision`, r.PathValue("attemptID"), id.SchoolID, r.PathValue("questionID"), []byte(req.Answer), req.Revision, req.RequestID, req.Flagged)
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
	defer tx.Rollback(r.Context())
	attemptID := r.PathValue("attemptID")
	var status string
	var expired bool
	err = tx.QueryRow(r.Context(), `SELECT status,now()>=deadline_at FROM assessment_attempts WHERE id=$1 AND school_id=$2 AND student_id=$3 FOR UPDATE`, attemptID, id.SchoolID, studentID).Scan(&status, &expired)
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
	stored, queryErr := tx.Query(r.Context(), `SELECT question_id::text,answer FROM assessment_answers WHERE attempt_id=$1 AND answer IS NOT NULL`, attemptID)
	if queryErr != nil {
		a.serverError(w, r, queryErr)
		return
	}
	savedAnswers := map[string]json.RawMessage{}
	for stored.Next() {
		var qid string
		var answer json.RawMessage
		if queryErr = stored.Scan(&qid, &answer); queryErr != nil {
			stored.Close()
			a.serverError(w, r, queryErr)
			return
		}
		savedAnswers[qid] = answer
	}
	if queryErr = stored.Err(); queryErr != nil {
		stored.Close()
		a.serverError(w, r, queryErr)
		return
	}
	stored.Close()
	if expired {
		req.Answers = savedAnswers
	} else {
		if req.Answers == nil {
			req.Answers = map[string]json.RawMessage{}
		}
		for questionID, answer := range req.Answers {
			savedAnswers[questionID] = answer
		}
		req.Answers = savedAnswers
	}
	rows, err := tx.Query(r.Context(), `SELECT question_id::text,question_snapshot->>'question_type',question_snapshot->'answer_key',points::float8 FROM assessment_attempt_items WHERE attempt_id=$1 AND school_id=$2 ORDER BY position`, attemptID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
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
			a.serverError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	valid := map[string]bool{}
	for _, item := range items {
		valid[item.id] = true
	}
	for questionID := range req.Answers {
		if !valid[questionID] {
			writeError(w, 400, "INVALID_ANSWER", "Submission contains a question outside this assessment.", nil)
			return
		}
	}
	var total, max float64
	var needsReview int
	for _, item := range items {
		answer := bytesTrimJSON(req.Answers[item.id])
		if len(answer) == 0 {
			answer = []byte("null")
		}
		if !json.Valid(answer) {
			writeError(w, 400, "INVALID_ANSWER", "An answer is not valid JSON.", nil)
			return
		}
		correct, manual := gradeAnswer(item.kind, item.key, answer)
		if manual {
			needsReview++
		}
		awarded := 0.0
		if correct {
			awarded = item.points
		}
		if !manual {
			max += item.points
		}
		total += awarded
		var isCorrect any
		if !manual {
			isCorrect = correct
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_answers(attempt_id,school_id,question_id,answer,is_correct,awarded_points,saved_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(attempt_id,question_id) DO UPDATE SET answer=EXCLUDED.answer,is_correct=EXCLUDED.is_correct,awarded_points=EXCLUDED.awarded_points,saved_at=now()`, attemptID, id.SchoolID, item.id, answer, isCorrect, awarded); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	var finalMax float64
	if err = tx.QueryRow(r.Context(), `SELECT COALESCE(sum(ai.points),0)::float8 FROM assessment_items ai WHERE ai.assessment_id=(SELECT aa.assessment_id FROM assessment_assignments aa JOIN assessment_attempts at ON at.assignment_id=aa.id WHERE at.id=$1)`, attemptID).Scan(&finalMax); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessment_attempts SET status='submitted',submitted_at=now(),score=$2,max_score=$3,results_released=($4=0),final_score=CASE WHEN $4=0 THEN $2 ELSE NULL END,final_max_score=CASE WHEN $4=0 THEN $5 ELSE NULL END WHERE id=$1`, attemptID, total, max, needsReview, finalMax); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = updateLearningIntelligence(r.Context(), tx, id.SchoolID, attemptID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.submitted','assessment_attempt',$3,jsonb_build_object('auto_score',$4::numeric,'auto_max_score',$5::numeric,'manual_review_count',$6::int))`, id.SchoolID, id.UserID, attemptID, total, max, needsReview); err != nil {
		a.serverError(w, r, err)
		return
	}
	eventType := "submitted"
	if expired {
		eventType = "deadline_submission"
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_attempt_events(school_id,attempt_id,event_type,event_data) VALUES($1,$2,$3,jsonb_build_object('auto_score',$4::numeric,'manual_review_count',$5::int))`, id.SchoolID, attemptID, eventType, total, needsReview); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	var finalScore, finalMaxScore any
	if needsReview == 0 {
		finalScore, finalMaxScore = total, finalMax
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"attempt_id": attemptID, "status": "submitted", "auto_score": total, "auto_max_score": max, "manual_review_count": needsReview, "results_released": needsReview == 0, "final_score": finalScore, "final_max_score": finalMaxScore, "deadline_submission": expired}})
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
