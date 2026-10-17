package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *API) registerAssessmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/assessments", a.admin(a.listAssessments))
	mux.HandleFunc("POST /api/v1/assessments", a.admin(a.createAssessment))
	mux.HandleFunc("POST /api/v1/assessments/{assessmentID}/publish", a.admin(a.publishAssessment))
	mux.HandleFunc("POST /api/v1/assessments/{assessmentID}/assignments", a.admin(a.assignAssessment))
}

func (a *API) listAssessments(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT a.id::text,a.title,a.instructions,a.status,a.created_at::text,a.duration_minutes,
		       (SELECT count(*)::int FROM assessment_items ai WHERE ai.assessment_id=a.id),
		       COALESCE((SELECT array_agg(ai.question_id::text ORDER BY ai.position) FROM assessment_items ai WHERE ai.assessment_id=a.id),'{}'),
		       COALESCE((SELECT array_agg(aa.class_id::text ORDER BY aa.assigned_at) FROM assessment_assignments aa WHERE aa.assessment_id=a.id),'{}')
		FROM assessments a WHERE a.school_id=$1
		ORDER BY a.created_at DESC LIMIT 200`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type item struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		Instructions     string   `json:"instructions"`
		Status           string   `json:"status"`
		CreatedAt        string   `json:"created_at"`
		DurationMinutes  int      `json:"duration_minutes"`
		QuestionCount    int      `json:"question_count"`
		QuestionIDs      []string `json:"question_ids"`
		AssignedClassIDs []string `json:"assigned_class_ids"`
	}
	items := []item{}
	for rows.Next() {
		var x item
		if err := rows.Scan(&x.ID, &x.Title, &x.Instructions, &x.Status, &x.CreatedAt, &x.DurationMinutes, &x.QuestionCount, &x.QuestionIDs, &x.AssignedClassIDs); err != nil {
			a.serverError(w, r, err)
			return
		}
		if x.QuestionIDs == nil {
			x.QuestionIDs = []string{}
		}
		if x.AssignedClassIDs == nil {
			x.AssignedClassIDs = []string{}
		}
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (a *API) createAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Title           string   `json:"title"`
		Instructions    string   `json:"instructions"`
		QuestionIDs     []string `json:"question_ids"`
		DurationMinutes int      `json:"duration_minutes"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Instructions = strings.TrimSpace(req.Instructions)
	if req.Title == "" || len(req.Title) > 200 || len(req.Instructions) > 5000 || len(req.QuestionIDs) == 0 || len(req.QuestionIDs) > 100 || req.DurationMinutes < 5 || req.DurationMinutes > 240 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter a title, choose a duration from 5 to 240 minutes, keep instructions under 5,000 characters, and select 1 to 100 questions.", nil)
		return
	}
	seen := map[string]bool{}
	for _, questionID := range req.QuestionIDs {
		if questionID == "" || seen[questionID] {
			writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Each selected question must be unique.", nil)
			return
		}
		seen[questionID] = true
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var assessmentID string
	err = tx.QueryRow(r.Context(), `INSERT INTO assessments(school_id,title,instructions,duration_minutes,created_by) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, id.SchoolID, req.Title, req.Instructions, req.DurationMinutes, id.UserID).Scan(&assessmentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	for index, questionID := range req.QuestionIDs {
		var status string
		err = tx.QueryRow(r.Context(), `SELECT status FROM questions WHERE id=$1 AND school_id=$2`, questionID, id.SchoolID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "QUESTION_UNAVAILABLE", "A selected question is not available in this school.", nil)
			return
		}
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if status != "published" {
			writeError(w, http.StatusBadRequest, "QUESTION_UNAVAILABLE", "Assessments can only include published questions.", nil)
			return
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO assessment_items(assessment_id,school_id,question_id,position,points) VALUES($1,$2,$3,$4,1)`, assessmentID, id.SchoolID, questionID, index+1); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.created','assessment',$3,jsonb_build_object('title',$4::text,'question_count',$5::int))`, id.SchoolID, id.UserID, assessmentID, req.Title, len(req.QuestionIDs)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"id": assessmentID, "title": req.Title, "status": "draft", "duration_minutes": req.DurationMinutes, "question_count": len(req.QuestionIDs)}})
}

func (a *API) publishAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	assessmentID := r.PathValue("assessmentID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	err = tx.QueryRow(r.Context(), `SELECT status FROM assessments WHERE id=$1 AND school_id=$2 FOR UPDATE`, assessmentID, id.SchoolID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Assessment not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "draft" {
		writeError(w, http.StatusConflict, "CONFLICT", "Only draft assessments can be published.", nil)
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM assessment_items WHERE assessment_id=$1`, assessmentID).Scan(&count); err != nil {
		a.serverError(w, r, err)
		return
	}
	if count == 0 {
		writeError(w, http.StatusConflict, "ASSESSMENT_EMPTY", "Add at least one question before publishing.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE assessments SET status='published',updated_at=now() WHERE id=$1 AND school_id=$2`, assessmentID, id.SchoolID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id) VALUES($1,$2,'assessment.published','assessment',$3)`, id.SchoolID, id.UserID, assessmentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"id": assessmentID, "status": "published"}})
}

func (a *API) assignAssessment(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ClassID string `json:"class_id"`
	}
	if !decode(w, r, &req) || strings.TrimSpace(req.ClassID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	assessmentID := r.PathValue("assessmentID")
	var status string
	err := a.db.QueryRow(r.Context(), `SELECT status FROM assessments WHERE id=$1 AND school_id=$2`, assessmentID, id.SchoolID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Assessment not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "published" {
		writeError(w, http.StatusConflict, "ASSESSMENT_NOT_PUBLISHED", "Publish the assessment before assigning it.", nil)
		return
	}
	var assignmentID string
	err = a.db.QueryRow(r.Context(), `INSERT INTO assessment_assignments(school_id,assessment_id,class_id,assigned_by) SELECT $1,a.id,c.id,$4 FROM assessments a JOIN classes c ON c.id=$3 AND c.school_id=$1 WHERE a.id=$2 AND a.school_id=$1 RETURNING id::text`, id.SchoolID, assessmentID, req.ClassID, id.UserID).Scan(&assignmentID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "ALREADY_ASSIGNED", "This assessment is already assigned to that class.", nil)
			return
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Class was not found in this school.", nil)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "Class was not found in this school.", nil)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if _, err = a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.assigned','assessment_assignment',$3,jsonb_build_object('assessment_id',$4::text,'class_id',$5::text))`, id.SchoolID, id.UserID, assignmentID, assessmentID, req.ClassID); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"id": assignmentID, "assessment_id": assessmentID, "class_id": req.ClassID}})
}
