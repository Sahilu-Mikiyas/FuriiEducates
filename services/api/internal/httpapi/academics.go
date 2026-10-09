package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

type academicRecordInput struct {
	SchoolName        string   `json:"school_name"`
	AcademicYearLabel string   `json:"academic_year_label"`
	GradeLevel        string   `json:"grade_level"`
	SubjectName       string   `json:"subject_name"`
	Score             *float64 `json:"score"`
	MaximumScore      *float64 `json:"maximum_score"`
	GradeLabel        string   `json:"grade_label"`
	GradingScaleID    string   `json:"grading_scale_id"`
	SourceType        string   `json:"source_type"`
}

func (a *API) registerAcademicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/students/{studentID}/academic-records", a.admin(a.listAcademicRecords))
	mux.HandleFunc("POST /api/v1/students/{studentID}/academic-records", a.admin(a.createAcademicRecord))
	mux.HandleFunc("GET /api/v1/students/{studentID}/external-exams", a.admin(a.listExternalExams))
	mux.HandleFunc("POST /api/v1/students/{studentID}/external-exams", a.admin(a.createExternalExam))
	mux.HandleFunc("POST /api/v1/import-jobs/preview", a.admin(a.previewAcademicImport))
	mux.HandleFunc("GET /api/v1/import-jobs/{jobID}", a.admin(a.getImportJob))
	mux.HandleFunc("POST /api/v1/import-jobs/{jobID}/commit", a.admin(a.commitImportJob))
}

func (a *API) listAcademicRecords(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID := r.PathValue("studentID")
	if err := a.ensureStudent(r.Context(), id.SchoolID, studentID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT ar.id::text,ar.school_name,ar.academic_year_label,ar.grade_level,ar.subject_name,COALESCE(ar.score::text,''),COALESCE(ar.maximum_score::text,''),COALESCE(ar.grade_label,''),COALESCE(gs.name,''),ar.source_type,ar.verification_status,ar.recorded_at FROM academic_records ar LEFT JOIN grading_scales gs ON gs.id=ar.grading_scale_id WHERE ar.student_id=$1 ORDER BY ar.recorded_at DESC LIMIT 200`, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var item struct {
			ID, SchoolName, AcademicYearLabel, GradeLevel, SubjectName, Score, MaximumScore, GradeLabel, GradingScaleName, SourceType, VerificationStatus string
			RecordedAt                                                                                                                                    time.Time
		}
		if err := rows.Scan(&item.ID, &item.SchoolName, &item.AcademicYearLabel, &item.GradeLevel, &item.SubjectName, &item.Score, &item.MaximumScore, &item.GradeLabel, &item.GradingScaleName, &item.SourceType, &item.VerificationStatus, &item.RecordedAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": item.ID, "school_name": item.SchoolName, "academic_year_label": item.AcademicYearLabel, "grade_level": item.GradeLevel, "subject_name": item.SubjectName, "score": numberOrNil(item.Score), "maximum_score": numberOrNil(item.MaximumScore), "grade_label": item.GradeLabel, "grading_scale_name": item.GradingScaleName, "source_type": item.SourceType, "verification_status": item.VerificationStatus, "recorded_at": item.RecordedAt.UTC().Format(time.RFC3339)})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createAcademicRecord(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req academicRecordInput
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if err := validateAcademicRecord(req); err != "" {
		writeError(w, 400, "VALIDATION_ERROR", err, nil)
		return
	}
	studentID := r.PathValue("studentID")
	if err := a.ensureStudent(r.Context(), id.SchoolID, studentID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if req.GradingScaleID != "" {
		var found bool
		if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM grading_scales WHERE id=$1 AND (school_id=$2 OR school_id IS NULL))`, req.GradingScaleID, id.SchoolID).Scan(&found); err != nil {
			a.serverError(w, r, err)
			return
		}
		if !found {
			writeError(w, 400, "VALIDATION_ERROR", "The grading scale is not available to this school.", map[string]string{"grading_scale_id": "Unknown grading scale."})
			return
		}
	}
	var recordID string
	err := a.db.QueryRow(r.Context(), `WITH created AS (INSERT INTO academic_records(student_id,school_name,academic_year_label,grade_level,subject_name,score,maximum_score,grade_label,grading_scale_id,source_type) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,'')::uuid,$10) RETURNING id) INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) SELECT $11,$12,'academic_record.created','academic_record',id,jsonb_build_object('student_id',$1::text,'subject_name',$5::text,'academic_year_label',$3::text) FROM created RETURNING entity_id::text`, studentID, strings.TrimSpace(req.SchoolName), strings.TrimSpace(req.AcademicYearLabel), strings.TrimSpace(req.GradeLevel), strings.TrimSpace(req.SubjectName), req.Score, req.MaximumScore, strings.TrimSpace(req.GradeLabel), req.GradingScaleID, req.SourceType, id.SchoolID, id.UserID).Scan(&recordID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": recordID, "verification_status": "unverified"}})
}

func validateAcademicRecord(req academicRecordInput) string {
	if strings.TrimSpace(req.SchoolName) == "" || strings.TrimSpace(req.AcademicYearLabel) == "" || strings.TrimSpace(req.GradeLevel) == "" || strings.TrimSpace(req.SubjectName) == "" {
		return "School, academic year, grade, and subject are required."
	}
	if len(req.SchoolName) > 200 || len(req.AcademicYearLabel) > 100 || len(req.GradeLevel) > 40 || len(req.SubjectName) > 120 || len(req.GradeLabel) > 40 {
		return "One or more fields exceed the supported length."
	}
	if req.SourceType != "school_import" && req.SourceType != "staff_entry" && req.SourceType != "verified_document" {
		return "Source type must be school_import, staff_entry, or verified_document."
	}
	if req.Score != nil && *req.Score < 0 {
		return "Score cannot be negative."
	}
	if req.MaximumScore != nil && *req.MaximumScore <= 0 {
		return "Maximum score must be greater than zero."
	}
	if req.Score != nil && req.MaximumScore != nil && *req.Score > *req.MaximumScore {
		return "Score cannot exceed maximum score."
	}
	return ""
}

func (a *API) listExternalExams(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID := r.PathValue("studentID")
	if err := a.ensureStudent(r.Context(), id.SchoolID, studentID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT e.id::text,e.exam_name,COALESCE(e.exam_provider,''),COALESCE(e.exam_date::text,''),COALESCE(e.subject_or_section,''),COALESCE(e.score::text,''),COALESCE(e.maximum_score::text,''),COALESCE(e.score_label,''),e.verification_status,e.source_metadata,COALESCE((SELECT jsonb_agg(jsonb_build_object('section_name',b.section_name,'score',b.score,'maximum_score',b.maximum_score,'score_label',b.score_label,'source_provided',b.source_provided) ORDER BY b.section_name) FROM external_exam_breakdowns b WHERE b.external_exam_id=e.id),'[]'::jsonb) FROM external_exams e WHERE e.student_id=$1 ORDER BY e.exam_date DESC NULLS LAST,e.created_at DESC LIMIT 100`, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, provider, examDate, subject, score, maxScore, label, verification string
		var metadata, breakdowns []byte
		if err := rows.Scan(&id, &name, &provider, &examDate, &subject, &score, &maxScore, &label, &verification, &metadata, &breakdowns); err != nil {
			a.serverError(w, r, err)
			return
		}
		var source any = map[string]any{}
		_ = json.Unmarshal(metadata, &source)
		var sections any = []any{}
		_ = json.Unmarshal(breakdowns, &sections)
		items = append(items, map[string]any{"id": id, "exam_name": name, "exam_provider": provider, "exam_date": examDate, "subject_or_section": subject, "score": numberOrNil(score), "maximum_score": numberOrNil(maxScore), "score_label": label, "verification_status": verification, "source_metadata": source, "breakdowns": sections})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createExternalExam(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ExamName         string         `json:"exam_name"`
		ExamProvider     string         `json:"exam_provider"`
		ExamDate         string         `json:"exam_date"`
		SubjectOrSection string         `json:"subject_or_section"`
		Score            *float64       `json:"score"`
		MaximumScore     *float64       `json:"maximum_score"`
		ScoreLabel       string         `json:"score_label"`
		SourceMetadata   map[string]any `json:"source_metadata"`
		Breakdowns       []struct {
			SectionName    string   `json:"section_name"`
			Score          *float64 `json:"score"`
			MaximumScore   *float64 `json:"maximum_score"`
			ScoreLabel     string   `json:"score_label"`
			SourceProvided *bool    `json:"source_provided"`
		} `json:"breakdowns"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.ExamName = strings.TrimSpace(req.ExamName)
	if req.ExamName == "" || len(req.ExamName) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Exam name is required and must be at most 200 characters.", nil)
		return
	}
	if req.Score != nil && *req.Score < 0 || req.MaximumScore != nil && *req.MaximumScore <= 0 || req.Score != nil && req.MaximumScore != nil && *req.Score > *req.MaximumScore {
		writeError(w, 400, "VALIDATION_ERROR", "Exam scores must be nonnegative and within the maximum score.", nil)
		return
	}
	var examDate any
	if req.ExamDate != "" {
		parsed, err := time.Parse("2006-01-02", req.ExamDate)
		if err != nil {
			writeError(w, 400, "VALIDATION_ERROR", "Exam date must use YYYY-MM-DD.", nil)
			return
		}
		examDate = parsed
	}
	studentID := r.PathValue("studentID")
	if err := a.ensureStudent(r.Context(), id.SchoolID, studentID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	for _, part := range req.Breakdowns {
		if strings.TrimSpace(part.SectionName) == "" || part.Score != nil && *part.Score < 0 || part.MaximumScore != nil && *part.MaximumScore <= 0 || part.Score != nil && part.MaximumScore != nil && *part.Score > *part.MaximumScore {
			writeError(w, 400, "VALIDATION_ERROR", "Each exam breakdown needs a section name and valid scores.", nil)
			return
		}
	}
	metadata, err := json.Marshal(req.SourceMetadata)
	if err != nil {
		writeError(w, 400, "VALIDATION_ERROR", "Source metadata is invalid.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var examID string
	err = tx.QueryRow(r.Context(), `INSERT INTO external_exams(student_id,exam_name,exam_provider,exam_date,subject_or_section,score,maximum_score,score_label,source_metadata) VALUES($1,$2,NULLIF($3,''),$4,NULLIF($5,''),$6,$7,NULLIF($8,''),$9) RETURNING id::text`, studentID, req.ExamName, strings.TrimSpace(req.ExamProvider), examDate, strings.TrimSpace(req.SubjectOrSection), req.Score, req.MaximumScore, strings.TrimSpace(req.ScoreLabel), metadata).Scan(&examID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	for _, part := range req.Breakdowns {
		provided := true
		if part.SourceProvided != nil {
			provided = *part.SourceProvided
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO external_exam_breakdowns(external_exam_id,section_name,score,maximum_score,score_label,source_provided) VALUES($1,$2,$3,$4,NULLIF($5,''),$6)`, examID, strings.TrimSpace(part.SectionName), part.Score, part.MaximumScore, strings.TrimSpace(part.ScoreLabel), provided); err != nil {
			a.databaseError(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'external_exam.created','external_exam',$3,jsonb_build_object('student_id',$4::text,'exam_name',$5::text))`, id.SchoolID, id.UserID, examID, studentID, req.ExamName); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": examID, "verification_status": "unverified"}})
}

func (a *API) ensureStudent(ctx context.Context, schoolID, studentID string) error {
	var found bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students WHERE id=$1 AND school_id=$2)`, studentID, schoolID).Scan(&found)
	if err != nil {
		return err
	}
	if !found {
		return pgx.ErrNoRows
	}
	return nil
}

func numberOrNil(raw string) any {
	if raw == "" {
		return nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return value
}
