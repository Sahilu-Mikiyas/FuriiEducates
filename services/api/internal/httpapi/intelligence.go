package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const masteryAlgorithmVersion = "beta-weighted-v1"

func (a *API) registerIntelligenceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/student/learning-profile", a.student(a.getStudentLearningProfile))
	mux.HandleFunc("POST /api/v1/student/recommendations/{recommendationID}/accept", a.student(a.acceptRecommendation))
	mux.HandleFunc("POST /api/v1/student/recommendations/{recommendationID}/complete", a.student(a.completeRecommendation))
	mux.HandleFunc("GET /api/v1/teacher/classes/{classID}/learning-profile", a.teacher(a.getTeacherLearningProfile))
}

func updateLearningIntelligence(ctx context.Context, tx pgx.Tx, schoolID, attemptID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO skill_evidence(school_id,student_id,skill_id,attempt_id,question_id,correctness,evidence_weight)
		SELECT at.school_id,at.student_id,qst.skill_id,at.id,ans.question_id,
		       CASE WHEN ans.is_correct THEN 1 ELSE 0 END,qst.weight
		FROM assessment_attempts at
		JOIN assessment_answers ans ON ans.attempt_id=at.id AND ans.school_id=at.school_id
		JOIN question_skill_tags qst ON qst.question_id=ans.question_id
		WHERE at.id=$1 AND at.school_id=$2 AND at.status='submitted' AND ans.is_correct IS NOT NULL
		ON CONFLICT (attempt_id,question_id,skill_id) DO NOTHING`, attemptID, schoolID)
	if err != nil {
		return err
	}
	var studentID string
	if err := tx.QueryRow(ctx, `SELECT student_id::text FROM assessment_attempts WHERE id=$1 AND school_id=$2`, attemptID, schoolID).Scan(&studentID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `
		SELECT e.skill_id::text,n.title,count(*)::int,sum(e.evidence_weight)::float8,
		       sum(e.correctness*e.evidence_weight)::float8
		FROM skill_evidence e JOIN curriculum_nodes n ON n.id=e.skill_id
		WHERE e.school_id=$1 AND e.student_id=$2
		GROUP BY e.skill_id,n.title`, schoolID, studentID)
	if err != nil {
		return err
	}
	type evidenceSummary struct {
		skillID string
		title   string
		count   int
		weight  float64
		correct float64
	}
	var summaries []evidenceSummary
	for rows.Next() {
		var item evidenceSummary
		if err = rows.Scan(&item.skillID, &item.title, &item.count, &item.weight, &item.correct); err != nil {
			rows.Close()
			return err
		}
		summaries = append(summaries, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range summaries {
		mastery := (2.0 + item.correct) / (4.0 + item.weight) * 100
		confidence := float64(item.count) * 10
		if confidence > 100 {
			confidence = 100
		}
		if _, err = tx.Exec(ctx, `INSERT INTO mastery_snapshots(school_id,student_id,skill_id,estimated_mastery,confidence,evidence_count,algorithm_version) VALUES($1,$2,$3,$4,$5,$6,$7)`, schoolID, studentID, item.skillID, mastery, confidence, item.count, masteryAlgorithmVersion); err != nil {
			return err
		}
		if item.count >= 5 && mastery < 65 {
			priority := "medium"
			if mastery < 40 && confidence >= 50 {
				priority = "high"
			}
			evidence, _ := json.Marshal(map[string]any{"estimated_mastery": mastery, "confidence": confidence, "evidence_count": item.count, "weighted_responses": item.weight})
			action, _ := json.Marshal(map[string]any{"title": "Practice this skill and reassess", "description": "Complete teacher-approved practice for this skill, then take a fresh assessment to measure progress.", "target_mastery": 65})
			_, err = tx.Exec(ctx, `INSERT INTO recommendations(school_id,student_id,skill_id,reason_code,evidence_summary,recommended_action,priority,algorithm_version,baseline_mastery) VALUES($1,$2,$3,'skill_gap',$4,$5,$6,$7,$8) ON CONFLICT (student_id,skill_id) WHERE status IN ('pending','accepted','in_progress') DO UPDATE SET evidence_summary=EXCLUDED.evidence_summary,recommended_action=EXCLUDED.recommended_action,priority=EXCLUDED.priority,algorithm_version=EXCLUDED.algorithm_version`, schoolID, studentID, item.skillID, evidence, action, priority, masteryAlgorithmVersion, mastery)
			if err != nil {
				return err
			}
		} else if _, err = tx.Exec(ctx, `UPDATE recommendations SET status='superseded' WHERE school_id=$1 AND student_id=$2 AND skill_id=$3 AND status IN ('pending','accepted','in_progress')`, schoolID, studentID, item.skillID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO intervention_outcomes(school_id,recommendation_id,post_attempt_id,pre_mastery,post_mastery,change_points,outcome_summary)
			SELECT r.school_id,r.id,$3,r.baseline_mastery,$4,$4-r.baseline_mastery,
			       jsonb_build_object('skill_title',$5::text,'algorithm_version',$6::text,'evidence_count',$7::int)
			FROM recommendations r WHERE r.school_id=$1 AND r.student_id=$2 AND r.skill_id=$8 AND r.status='completed'
			AND r.completed_at < now() AND EXISTS(SELECT 1 FROM skill_evidence e WHERE e.attempt_id=$3 AND e.student_id=r.student_id AND e.skill_id=r.skill_id)
			AND NOT EXISTS(SELECT 1 FROM intervention_outcomes o WHERE o.recommendation_id=r.id)`, schoolID, studentID, attemptID, mastery, item.title, masteryAlgorithmVersion, item.count, item.skillID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *API) getStudentLearningProfile(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT DISTINCT ON(ms.skill_id) ms.skill_id::text,n.title,ms.estimated_mastery::float8,ms.confidence::float8,ms.evidence_count,ms.algorithm_version,ms.calculated_at::text FROM mastery_snapshots ms JOIN curriculum_nodes n ON n.id=ms.skill_id WHERE ms.school_id=$1 AND ms.student_id=$2 ORDER BY ms.skill_id,ms.calculated_at DESC`, id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type mastery struct {
		SkillID        string  `json:"skill_id"`
		Title          string  `json:"title"`
		Estimate       float64 `json:"estimated_mastery"`
		Confidence     float64 `json:"confidence"`
		Evidence       int     `json:"evidence_count"`
		Algorithm      string  `json:"algorithm_version"`
		CalculatedAt   string  `json:"calculated_at"`
		ConfidenceBand string  `json:"confidence_band"`
	}
	items := []mastery{}
	for rows.Next() {
		var x mastery
		if err = rows.Scan(&x.SkillID, &x.Title, &x.Estimate, &x.Confidence, &x.Evidence, &x.Algorithm, &x.CalculatedAt); err != nil {
			rows.Close()
			a.serverError(w, r, err)
			return
		}
		x.ConfidenceBand = confidenceBand(x.Evidence)
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	recs, err := a.db.Query(r.Context(), `SELECT r.id::text,r.skill_id::text,n.title,r.reason_code,r.evidence_summary,r.recommended_action,r.priority,r.status,r.algorithm_version,r.created_at::text,COALESCE(o.pre_mastery::float8,r.baseline_mastery::float8),o.post_mastery::float8,o.change_points::float8 FROM recommendations r JOIN curriculum_nodes n ON n.id=r.skill_id LEFT JOIN intervention_outcomes o ON o.recommendation_id=r.id WHERE r.school_id=$1 AND r.student_id=$2 AND r.status NOT IN ('superseded','dismissed') ORDER BY CASE r.status WHEN 'pending' THEN 0 WHEN 'accepted' THEN 1 WHEN 'in_progress' THEN 2 ELSE 3 END,r.created_at DESC LIMIT 100`, id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer recs.Close()
	type recommendation struct {
		ID        string          `json:"id"`
		SkillID   string          `json:"skill_id"`
		Skill     string          `json:"skill_title"`
		Reason    string          `json:"reason_code"`
		Evidence  json.RawMessage `json:"evidence_summary"`
		Action    json.RawMessage `json:"recommended_action"`
		Priority  string          `json:"priority"`
		Status    string          `json:"status"`
		Algorithm string          `json:"algorithm_version"`
		CreatedAt string          `json:"created_at"`
		Pre       *float64        `json:"pre_mastery"`
		Post      *float64        `json:"post_mastery"`
		Change    *float64        `json:"change_points"`
	}
	recommendations := []recommendation{}
	for recs.Next() {
		var x recommendation
		if err = recs.Scan(&x.ID, &x.SkillID, &x.Skill, &x.Reason, &x.Evidence, &x.Action, &x.Priority, &x.Status, &x.Algorithm, &x.CreatedAt, &x.Pre, &x.Post, &x.Change); err != nil {
			a.serverError(w, r, err)
			return
		}
		recommendations = append(recommendations, x)
	}
	if err = recs.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"algorithm_version": masteryAlgorithmVersion, "skills": items, "recommendations": recommendations}})
}

func confidenceBand(evidence int) string {
	if evidence < 5 {
		return "low"
	}
	if evidence < 10 {
		return "provisional"
	}
	return "stable"
}

func (a *API) changeRecommendation(w http.ResponseWriter, r *http.Request, id auth.Identity, complete bool) {
	studentID, err := a.learnerStudent(r, id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 403, "STUDENT_PROFILE_UNAVAILABLE", "No active student profile is linked to this account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	recommendationID := r.PathValue("recommendationID")
	var status string
	err = a.db.QueryRow(r.Context(), `SELECT status FROM recommendations WHERE id=$1 AND school_id=$2 AND student_id=$3`, recommendationID, id.SchoolID, studentID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Recommendation not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if complete {
		if status != "accepted" && status != "in_progress" {
			writeError(w, 409, "INVALID_STATUS", "Accept the recommendation before marking it complete.", nil)
			return
		}
		_, err = a.db.Exec(r.Context(), `UPDATE recommendations SET status='completed',completed_at=now() WHERE id=$1 AND school_id=$2 AND student_id=$3`, recommendationID, id.SchoolID, studentID)
	} else {
		if status != "pending" {
			writeError(w, 409, "INVALID_STATUS", "Only pending recommendations can be accepted.", nil)
			return
		}
		_, err = a.db.Exec(r.Context(), `UPDATE recommendations SET status='accepted',accepted_at=now() WHERE id=$1 AND school_id=$2 AND student_id=$3`, recommendationID, id.SchoolID, studentID)
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": recommendationID, "status": map[bool]string{true: "completed", false: "accepted"}[complete]}})
}

func (a *API) acceptRecommendation(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.changeRecommendation(w, r, id, false)
}
func (a *API) completeRecommendation(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.changeRecommendation(w, r, id, true)
}

func (a *API) getTeacherLearningProfile(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	var assigned bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND teacher_id=$2 AND class_id=$3)`, id.SchoolID, id.UserID, classID).Scan(&assigned); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !assigned {
		writeError(w, 403, "FORBIDDEN", "You are not assigned to this class.", nil)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(latest.skill_id::text,''),COALESCE(n.title,''),COALESCE(latest.estimated_mastery::float8,0),COALESCE(latest.confidence::float8,0),COALESCE(latest.evidence_count,0),COALESCE(active_recs.count,0) FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id LEFT JOIN LATERAL(SELECT DISTINCT ON(ms.skill_id) ms.skill_id,ms.estimated_mastery,ms.confidence,ms.evidence_count FROM mastery_snapshots ms WHERE ms.student_id=s.id AND ms.school_id=s.school_id ORDER BY ms.skill_id,ms.calculated_at DESC) latest ON TRUE LEFT JOIN curriculum_nodes n ON n.id=latest.skill_id LEFT JOIN LATERAL(SELECT count(*)::int count FROM recommendations r WHERE r.student_id=s.id AND r.school_id=s.school_id AND r.status IN ('pending','accepted','in_progress')) active_recs ON TRUE WHERE e.school_id=$1 AND e.class_id=$2 AND e.status='active' AND s.status='active' ORDER BY s.legal_name LIMIT 500`, id.SchoolID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	type studentProfile struct {
		StudentID             string  `json:"student_id"`
		Number                string  `json:"student_number"`
		Name                  string  `json:"student_name"`
		SkillID               string  `json:"skill_id"`
		Skill                 string  `json:"skill_title"`
		Mastery               float64 `json:"estimated_mastery"`
		Confidence            float64 `json:"confidence"`
		Evidence              int     `json:"evidence_count"`
		ConfidenceBand        string  `json:"confidence_band"`
		ActiveRecommendations int     `json:"active_recommendations"`
	}
	items := []studentProfile{}
	for rows.Next() {
		var x studentProfile
		if err = rows.Scan(&x.StudentID, &x.Number, &x.Name, &x.SkillID, &x.Skill, &x.Mastery, &x.Confidence, &x.Evidence, &x.ActiveRecommendations); err != nil {
			a.serverError(w, r, err)
			return
		}
		x.ConfidenceBand = confidenceBand(x.Evidence)
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}
