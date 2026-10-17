package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

func (a *API) registerReviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/teacher/reviews", a.teacher(a.listTeacherReviews))
	mux.HandleFunc("PUT /api/v1/teacher/attempts/{attemptID}/answers/{questionID}", a.teacher(a.gradeTeacherAnswer))
}

func (a *API) listTeacherReviews(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := strings.TrimSpace(r.URL.Query().Get("class_id"))
	if classID == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose one of your assigned classes.", nil)
		return
	}
	var assigned bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND teacher_id=$2 AND class_id=$3)`, id.SchoolID, id.UserID, classID).Scan(&assigned); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !assigned {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You are not assigned to this class.", nil)
		return
	}
	rows, err := a.db.Query(r.Context(), `
		SELECT at.id::text,item.question_id::text,aa.id::text,a.title,
		       s.id::text,s.student_number,s.legal_name,
		       item.question_snapshot->'prompt',item.question_snapshot->'answer_key',ans.answer,item.points::float8,
		       COALESCE(ans.feedback,''),COALESCE(ans.reviewed_at::text,'')
		FROM assessment_attempts at
		JOIN assessment_assignments aa ON aa.id=at.assignment_id AND aa.school_id=at.school_id
		JOIN assessments a ON a.id=aa.assessment_id AND a.school_id=aa.school_id
		JOIN class_teachers ct ON ct.class_id=aa.class_id AND ct.school_id=aa.school_id AND ct.teacher_id=$2
		JOIN students s ON s.id=at.student_id AND s.school_id=at.school_id
		JOIN assessment_attempt_items item ON item.attempt_id=at.id AND item.school_id=at.school_id AND item.question_snapshot->>'question_type'='text'
		JOIN assessment_answers ans ON ans.attempt_id=at.id AND ans.question_id=item.question_id
		WHERE at.school_id=$1 AND aa.class_id=$3 AND at.status='submitted' AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL
		ORDER BY at.submitted_at,s.legal_name,item.position LIMIT 500`, id.SchoolID, id.UserID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type review struct {
		AttemptID    string          `json:"attempt_id"`
		QuestionID   string          `json:"question_id"`
		AssignmentID string          `json:"assignment_id"`
		Assessment   string          `json:"assessment_title"`
		StudentID    string          `json:"student_id"`
		StudentNo    string          `json:"student_number"`
		StudentName  string          `json:"student_name"`
		Prompt       json.RawMessage `json:"prompt"`
		Guide        json.RawMessage `json:"grading_guide,omitempty"`
		Answer       json.RawMessage `json:"answer"`
		MaxPoints    float64         `json:"max_points"`
		Feedback     string          `json:"feedback"`
		ReviewedAt   string          `json:"reviewed_at,omitempty"`
	}
	items := []review{}
	for rows.Next() {
		var item review
		if err := rows.Scan(&item.AttemptID, &item.QuestionID, &item.AssignmentID, &item.Assessment,
			&item.StudentID, &item.StudentNo, &item.StudentName, &item.Prompt, &item.Guide,
			&item.Answer, &item.MaxPoints, &item.Feedback, &item.ReviewedAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (a *API) gradeTeacherAnswer(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Points   *float64 `json:"points"`
		Feedback string   `json:"feedback"`
	}
	if !decode(w, r, &req) || req.Points == nil || math.IsNaN(*req.Points) || math.IsInf(*req.Points, 0) || *req.Points < 0 || len(req.Feedback) > 5000 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter a score from zero to the question's maximum and feedback under 5,000 characters.", nil)
		return
	}
	attemptID, questionID := r.PathValue("attemptID"), r.PathValue("questionID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var status string
	err = tx.QueryRow(r.Context(), `SELECT status FROM assessment_attempts WHERE id=$1 AND school_id=$2 FOR UPDATE`, attemptID, id.SchoolID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Assessment attempt not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "submitted" {
		writeError(w, http.StatusConflict, "ATTEMPT_NOT_SUBMITTED", "Only submitted assessment answers can be graded.", nil)
		return
	}
	var maxPoints float64
	err = tx.QueryRow(r.Context(), `
		SELECT item.points::float8 FROM assessment_attempts at
		JOIN assessment_assignments aa ON aa.id=at.assignment_id AND aa.school_id=at.school_id
		JOIN class_teachers ct ON ct.class_id=aa.class_id AND ct.school_id=aa.school_id AND ct.teacher_id=$3
		JOIN assessment_attempt_items item ON item.attempt_id=at.id AND item.school_id=at.school_id AND item.question_id=$4 AND item.question_snapshot->>'question_type'='text'
		JOIN assessment_answers ans ON ans.attempt_id=at.id AND ans.question_id=item.question_id AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb
		WHERE at.id=$1 AND at.school_id=$2`, attemptID, id.SchoolID, id.UserID, questionID).Scan(&maxPoints)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Written response was not found in a class assigned to you.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if *req.Points > maxPoints {
		writeError(w, http.StatusBadRequest, "INVALID_SCORE", "The score cannot exceed the question's maximum points.", nil)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE assessment_answers SET awarded_points=$3,feedback=$4,reviewed_by=$5,reviewed_at=now(),is_correct=NULL WHERE attempt_id=$1 AND question_id=$2`, attemptID, questionID, *req.Points, strings.TrimSpace(req.Feedback), id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var pending int
	if err = tx.QueryRow(r.Context(), `SELECT count(*)::int FROM assessment_answers ans JOIN questions q ON q.id=ans.question_id AND q.school_id=ans.school_id WHERE ans.attempt_id=$1 AND q.question_type='text' AND ans.answer IS NOT NULL AND ans.answer <> 'null'::jsonb AND ans.reviewed_at IS NULL`, attemptID).Scan(&pending); err != nil {
		a.serverError(w, r, err)
		return
	}
	var released bool
	var finalScore, finalMax *float64
	if pending == 0 {
		err = tx.QueryRow(r.Context(), `
			UPDATE assessment_attempts at SET results_released=TRUE,
				final_score=(SELECT COALESCE(sum(awarded_points),0) FROM assessment_answers WHERE attempt_id=at.id),
				final_max_score=(SELECT COALESCE(sum(ai.points),0) FROM assessment_items ai WHERE ai.assessment_id=(SELECT assessment_id FROM assessment_assignments WHERE id=at.assignment_id))
			WHERE at.id=$1 RETURNING results_released,final_score::float8,final_max_score::float8`, attemptID).Scan(&released, &finalScore, &finalMax)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'assessment.answer_graded','assessment_attempt',$3,jsonb_build_object('question_id',$4::text,'points',$5::numeric,'results_released',$6::boolean))`, id.SchoolID, id.UserID, attemptID, questionID, *req.Points, pending == 0); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"saved": true, "pending_review_count": pending, "results_released": released, "final_score": finalScore, "final_max_score": finalMax}})
}
