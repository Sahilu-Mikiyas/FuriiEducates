package httpapi

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/furii/school-os/services/api/internal/auth"
)

var academicCSVHeaders = []string{"student_number", "school_name", "academic_year_label", "grade_level", "subject_name", "score", "maximum_score", "grade_label", "source_type"}

type academicImportRow struct {
	StudentNumber     string   `json:"student_number"`
	SchoolName        string   `json:"school_name"`
	AcademicYearLabel string   `json:"academic_year_label"`
	GradeLevel        string   `json:"grade_level"`
	SubjectName       string   `json:"subject_name"`
	Score             *float64 `json:"score"`
	MaximumScore      *float64 `json:"maximum_score"`
	GradeLabel        string   `json:"grade_label"`
	SourceType        string   `json:"source_type"`
}

type importRowError struct {
	Row    int      `json:"row"`
	Errors []string `json:"errors"`
}

func (a *API) previewAcademicImport(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	reader := csv.NewReader(r.Body)
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		writeError(w, 400, "VALIDATION_ERROR", "Upload a CSV with the required header row.", nil)
		return
	}
	for i := range header {
		header[i] = strings.ToLower(strings.TrimSpace(header[i]))
	}
	if len(header) != len(academicCSVHeaders) {
		writeError(w, 400, "VALIDATION_ERROR", "CSV headers must be student_number, school_name, academic_year_label, grade_level, subject_name, score, maximum_score, grade_label, source_type in that order.", nil)
		return
	}
	for i, name := range academicCSVHeaders {
		if header[i] != name {
			writeError(w, 400, "VALIDATION_ERROR", "CSV headers must be student_number, school_name, academic_year_label, grade_level, subject_name, score, maximum_score, grade_label, source_type in that order.", nil)
			return
		}
	}
	valid := []academicImportRow{}
	rowErrors := []importRowError{}
	seen := map[string]bool{}
	total := 0
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			writeError(w, 400, "VALIDATION_ERROR", "CSV contains a malformed row.", nil)
			return
		}
		total++
		if total > 1000 {
			writeError(w, 413, "PAYLOAD_TOO_LARGE", "CSV may contain at most 1,000 records.", nil)
			return
		}
		if len(record) != len(academicCSVHeaders) {
			rowErrors = append(rowErrors, importRowError{total + 1, []string{"Expected 9 columns."}})
			continue
		}
		row := academicImportRow{StudentNumber: strings.TrimSpace(record[0]), SchoolName: strings.TrimSpace(record[1]), AcademicYearLabel: strings.TrimSpace(record[2]), GradeLevel: strings.TrimSpace(record[3]), SubjectName: strings.TrimSpace(record[4]), GradeLabel: strings.TrimSpace(record[7]), SourceType: strings.TrimSpace(record[8])}
		issues := []string{}
		if row.StudentNumber == "" {
			issues = append(issues, "student_number is required.")
		}
		if row.SchoolName == "" {
			issues = append(issues, "school_name is required.")
		}
		if row.AcademicYearLabel == "" {
			issues = append(issues, "academic_year_label is required.")
		}
		if row.GradeLevel == "" {
			issues = append(issues, "grade_level is required.")
		}
		if row.SubjectName == "" {
			issues = append(issues, "subject_name is required.")
		}
		var scoreErr, maxErr error
		row.Score, scoreErr = parseOptionalCSVNumber(record[5])
		row.MaximumScore, maxErr = parseOptionalCSVNumber(record[6])
		if scoreErr != nil {
			issues = append(issues, "score must be a nonnegative number.")
		}
		if maxErr != nil || row.MaximumScore != nil && *row.MaximumScore <= 0 {
			issues = append(issues, "maximum_score must be greater than zero.")
		}
		if row.Score != nil && row.MaximumScore != nil && *row.Score > *row.MaximumScore {
			issues = append(issues, "score cannot exceed maximum_score.")
		}
		if row.SourceType != "school_import" && row.SourceType != "staff_entry" && row.SourceType != "verified_document" {
			issues = append(issues, "source_type must be school_import, staff_entry, or verified_document.")
		}
		key := strings.ToLower(row.StudentNumber) + "\x00" + strings.ToLower(row.AcademicYearLabel) + "\x00" + strings.ToLower(row.GradeLevel) + "\x00" + strings.ToLower(row.SubjectName)
		if seen[key] {
			issues = append(issues, "Duplicate student/year/grade/subject row in this file.")
		}
		seen[key] = true
		if row.StudentNumber != "" {
			var exists bool
			qerr := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM students WHERE school_id=$1 AND lower(student_number)=lower($2))`, id.SchoolID, row.StudentNumber).Scan(&exists)
			if qerr != nil {
				a.serverError(w, r, qerr)
				return
			}
			if !exists {
				issues = append(issues, "Student number is not enrolled in this school.")
			}
		}
		if len(issues) > 0 {
			rowErrors = append(rowErrors, importRowError{total + 1, issues})
		} else {
			valid = append(valid, row)
		}
	}
	if total == 0 {
		writeError(w, 400, "VALIDATION_ERROR", "CSV has no data rows.", nil)
		return
	}
	payload, err := json.Marshal(valid)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	errorsJSON, err := json.Marshal(rowErrors)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var jobID string
	err = a.db.QueryRow(r.Context(), `INSERT INTO import_jobs(school_id,requested_by,payload,row_errors,row_count) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, id.SchoolID, id.UserID, payload, errorsJSON, total).Scan(&jobID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"id": jobID, "status": "previewed", "row_count": total, "valid_rows": len(valid), "error_rows": len(rowErrors), "row_errors": rowErrors, "can_commit": len(rowErrors) == 0}})
}

func parseOptionalCSVNumber(value string) (*float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, err
	}
	if parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed > 99999.999 {
		return nil, errors.New("score is outside the supported range")
	}
	return &parsed, nil
}

func (a *API) getImportJob(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var job struct {
		ID, Status      string
		RowCount        int
		Payload, Errors []byte
	}
	err := a.db.QueryRow(r.Context(), `SELECT id::text,status,row_count,payload,row_errors FROM import_jobs WHERE id=$1 AND school_id=$2`, r.PathValue("jobID"), id.SchoolID).Scan(&job.ID, &job.Status, &job.RowCount, &job.Payload, &job.Errors)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	var rows []academicImportRow
	var rowErrors []importRowError
	_ = json.Unmarshal(job.Payload, &rows)
	_ = json.Unmarshal(job.Errors, &rowErrors)
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": job.ID, "status": job.Status, "row_count": job.RowCount, "valid_rows": len(rows), "error_rows": len(rowErrors), "row_errors": rowErrors, "can_commit": job.Status == "previewed" && len(rowErrors) == 0}})
}

func (a *API) commitImportJob(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var job struct {
		Status          string
		Payload, Errors []byte
	}
	err = tx.QueryRow(r.Context(), `SELECT status,payload,row_errors FROM import_jobs WHERE id=$1 AND school_id=$2 FOR UPDATE`, r.PathValue("jobID"), id.SchoolID).Scan(&job.Status, &job.Payload, &job.Errors)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if job.Status != "previewed" {
		writeError(w, 409, "CONFLICT", "This import job has already been committed.", nil)
		return
	}
	var rowErrors []importRowError
	if err := json.Unmarshal(job.Errors, &rowErrors); err != nil {
		a.serverError(w, r, err)
		return
	}
	if len(rowErrors) > 0 {
		writeError(w, 409, "IMPORT_HAS_ERRORS", "Correct the CSV errors and preview a clean file before committing.", nil)
		return
	}
	var rows []academicImportRow
	if err := json.Unmarshal(job.Payload, &rows); err != nil {
		a.serverError(w, r, err)
		return
	}
	for _, row := range rows {
		tag, execErr := tx.Exec(r.Context(), `INSERT INTO academic_records(student_id,school_name,academic_year_label,grade_level,subject_name,score,maximum_score,grade_label,source_type) SELECT id,$3,$4,$5,$6,$7,$8,NULLIF($9,''),$10 FROM students WHERE school_id=$1 AND lower(student_number)=lower($2)`, id.SchoolID, row.StudentNumber, row.SchoolName, row.AcademicYearLabel, row.GradeLevel, row.SubjectName, row.Score, row.MaximumScore, row.GradeLabel, row.SourceType)
		err = execErr
		if err != nil {
			a.databaseError(w, r, err)
			return
		}
		if tag.RowsAffected() != 1 {
			writeError(w, 409, "CONFLICT", "A student in this import is no longer enrolled.", nil)
			return
		}
	}
	var jobID string
	if err := tx.QueryRow(r.Context(), `UPDATE import_jobs SET status='committed',committed_at=now() WHERE id=$1 RETURNING id::text`, r.PathValue("jobID")).Scan(&jobID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'academic_import.committed','import_job',$3,jsonb_build_object('record_count',$4::int))`, id.SchoolID, id.UserID, jobID, len(rows)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"id": jobID, "status": "committed", "imported_rows": len(rows)}})
}
