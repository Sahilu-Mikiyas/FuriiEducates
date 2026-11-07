package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The edit, archive and remove actions a school needs in its first weeks.
func (a *API) registerRecordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /api/v1/academic-years/{yearID}", a.admin(a.updateAcademicYear))
	mux.HandleFunc("DELETE /api/v1/academic-years/{yearID}", a.admin(a.deleteAcademicYear))
	mux.HandleFunc("PATCH /api/v1/classes/{classID}", a.admin(a.updateClass))
	mux.HandleFunc("DELETE /api/v1/classes/{classID}", a.admin(a.deleteClass))
	mux.HandleFunc("DELETE /api/v1/classes/{classID}/teachers/{teacherID}", a.admin(a.unassignTeacher))
	mux.HandleFunc("POST /api/v1/finance/entries/{entryID}/void", a.financeOffice(a.voidFinanceEntry))
	mux.HandleFunc("DELETE /api/v1/resources/{resourceID}", a.staffMember(a.deleteResource))
	mux.HandleFunc("POST /api/v1/academic-records/{recordID}/verification", a.records(a.verifyAcademicRecord))
	mux.HandleFunc("DELETE /api/v1/academic-records/{recordID}", a.records(a.deleteAcademicRecord))
	mux.HandleFunc("POST /api/v1/external-exams/{examID}/verification", a.records(a.verifyExternalExam))
	mux.HandleFunc("DELETE /api/v1/external-exams/{examID}", a.records(a.deleteExternalExam))
	mux.HandleFunc("POST /api/v1/questions/{questionID}/archive", a.admin(a.archiveQuestion))
}

func audit(ctx context.Context, tx pgx.Tx, id auth.Identity, action, entityType, entityID string, changes map[string]any) error {
	if changes == nil {
		changes = map[string]any{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,$3,$4,$5,$6)`, id.SchoolID, id.UserID, action, entityType, entityID, changes)
	return err
}

// runInTx runs work in a transaction and writes the error response for anything
// it returns that was not already answered.
func (a *API) runInTx(w http.ResponseWriter, r *http.Request, work func(tx pgx.Tx) (bool, error)) bool {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return false
	}
	defer tx.Rollback(context.Background())
	answered, err := work(tx)
	if err != nil {
		a.databaseError(w, r, err)
		return false
	}
	if answered {
		return false
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return false
	}
	return true
}

func (a *API) updateAcademicYear(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name     string `json:"name"`
		StartsOn string `json:"starts_on"`
		EndsOn   string `json:"ends_on"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	start, errStart := time.Parse("2006-01-02", req.StartsOn)
	end, errEnd := time.Parse("2006-01-02", req.EndsOn)
	if req.Name == "" || len(req.Name) > 100 || errStart != nil || errEnd != nil || !end.After(start) {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a name and valid dates with the end after the start.", nil)
		return
	}
	yearID := r.PathValue("yearID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		// Terms have to stay inside their year, or attendance would fall between them.
		var outside bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM academic_terms WHERE academic_year_id=$1 AND (starts_on<$2::date OR ends_on>$3::date))`, yearID, req.StartsOn, req.EndsOn).Scan(&outside); err != nil {
			return false, err
		}
		if outside {
			writeError(w, 409, "TERMS_OUTSIDE_YEAR", "A term would fall outside these dates. Change the term first.", nil)
			return true, nil
		}
		var found string
		if err := tx.QueryRow(r.Context(), `UPDATE academic_years SET name=$3,starts_on=$4::date,ends_on=$5::date WHERE id=$1 AND school_id=$2 RETURNING id::text`, yearID, id.SchoolID, req.Name, req.StartsOn, req.EndsOn).Scan(&found); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "academic_year.updated", "academic_year", yearID, map[string]any{"name": req.Name, "starts_on": req.StartsOn, "ends_on": req.EndsOn})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": yearID}})
	}
}

func (a *API) deleteAcademicYear(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	yearID := r.PathValue("yearID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var inUse bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM classes WHERE academic_year_id=$1) OR EXISTS(SELECT 1 FROM academic_terms WHERE academic_year_id=$1) FROM academic_years WHERE id=$1 AND school_id=$2`, yearID, id.SchoolID).Scan(&inUse); err != nil {
			return false, err
		}
		if inUse {
			writeError(w, 409, "YEAR_IN_USE", "This year has classes or terms, so it cannot be deleted.", nil)
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM academic_years WHERE id=$1 AND school_id=$2`, yearID, id.SchoolID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "academic_year.deleted", "academic_year", yearID, nil)
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": yearID}})
	}
}

func (a *API) updateClass(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name       string `json:"name"`
		GradeLevel string `json:"grade_level"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name, req.GradeLevel = strings.TrimSpace(req.Name), strings.TrimSpace(req.GradeLevel)
	if req.Name == "" || len(req.Name) > 100 || req.GradeLevel == "" || len(req.GradeLevel) > 40 {
		writeError(w, 400, "VALIDATION_ERROR", "Class name and grade level are required.", nil)
		return
	}
	classID := r.PathValue("classID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var found string
		if err := tx.QueryRow(r.Context(), `UPDATE classes SET name=$3,grade_level=$4 WHERE id=$1 AND school_id=$2 RETURNING id::text`, classID, id.SchoolID, req.Name, req.GradeLevel).Scan(&found); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "class.updated", "class", classID, map[string]any{"name": req.Name, "grade_level": req.GradeLevel})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": classID}})
	}
}

// deleteClass removes a class created by mistake. A class with any students,
// subjects, attendance or reports keeps them, because several of those would
// otherwise be deleted with it.
func (a *API) deleteClass(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var inUse bool
		err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM enrollments WHERE class_id=$1) OR EXISTS(SELECT 1 FROM class_subjects WHERE class_id=$1)
			OR EXISTS(SELECT 1 FROM attendance_records WHERE class_id=$1) OR EXISTS(SELECT 1 FROM report_publications WHERE class_id=$1)
			OR EXISTS(SELECT 1 FROM report_notes WHERE class_id=$1) OR EXISTS(SELECT 1 FROM assessment_assignments WHERE class_id=$1)
			FROM classes WHERE id=$1 AND school_id=$2`, classID, id.SchoolID).Scan(&inUse)
		if err != nil {
			return false, err
		}
		if inUse {
			writeError(w, 409, "CLASS_IN_USE", "This class has students, subjects or work recorded, so it cannot be deleted. Rename it instead.", nil)
			return true, nil
		}
		if _, err = tx.Exec(r.Context(), `DELETE FROM classes WHERE id=$1 AND school_id=$2`, classID, id.SchoolID); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23503" {
				writeError(w, 409, "CLASS_IN_USE", "This class has lesson plans, files or other records, so it cannot be deleted. Rename it instead.", nil)
				return true, nil
			}
			return false, err
		}
		return false, audit(r.Context(), tx, id, "class.deleted", "class", classID, nil)
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": classID}})
	}
}

// unassignTeacher takes a teacher off a class. Subjects they taught there go
// back to any teacher of the class, so their marks can still be entered.
func (a *API) unassignTeacher(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID, teacherID := r.PathValue("classID"), r.PathValue("teacherID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		tag, err := tx.Exec(r.Context(), `DELETE FROM class_teachers WHERE class_id=$1 AND teacher_id=$2 AND school_id=$3`, classID, teacherID, id.SchoolID)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 0 {
			writeError(w, 404, "NOT_FOUND", "That teacher is not assigned to this class.", nil)
			return true, nil
		}
		if _, err = tx.Exec(r.Context(), `UPDATE class_subjects SET teacher_id=NULL WHERE class_id=$1 AND teacher_id=$2`, classID, teacherID); err != nil {
			return false, err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE classes SET homeroom_teacher_id=NULL WHERE id=$1 AND homeroom_teacher_id=$2`, classID, teacherID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "class.teacher_removed", "class", classID, map[string]any{"teacher_id": teacherID})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"class_id": classID, "teacher_id": teacherID}})
	}
}

// voidFinanceEntry cancels an entry made in error. It stays in the ledger, marked
// void and left out of every total, with the reason in the audit log.
func (a *API) voidFinanceEntry(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if len(req.Reason) < 3 || len(req.Reason) > 300 {
		writeError(w, 400, "VALIDATION_ERROR", "Say why this entry is being voided.", nil)
		return
	}
	entryID := r.PathValue("entryID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT status FROM finance_entries WHERE id=$1 AND school_id=$2 FOR UPDATE`, entryID, id.SchoolID).Scan(&status); err != nil {
			return false, err
		}
		if status == "void" {
			writeError(w, 409, "ALREADY_VOID", "This entry is already void.", nil)
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `UPDATE finance_entries SET status='void' WHERE id=$1`, entryID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "finance.voided", "finance_entry", entryID, map[string]any{"reason": req.Reason, "was": status})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": entryID, "status": "void"}})
	}
}

// deleteResource removes a class file. The teacher who shared it or an
// administrator can remove it, and work that linked it simply loses the link.
func (a *API) deleteResource(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	resourceID := r.PathValue("resourceID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var uploader, classID, title string
		if err := tx.QueryRow(r.Context(), `SELECT uploaded_by::text,class_id::text,title FROM teaching_resources WHERE id=$1 AND school_id=$2`, resourceID, id.SchoolID).Scan(&uploader, &classID, &title); err != nil {
			return false, err
		}
		if uploader != id.UserID && !hasRole(id.Roles, "school_admin") {
			writeError(w, 403, "FORBIDDEN", "Only the teacher who shared this file, or an administrator, can remove it.", nil)
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM teaching_resources WHERE id=$1`, resourceID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "resource.deleted", "class", classID, map[string]any{"title": title})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": resourceID}})
	}
}

var verificationStatuses = map[string]bool{"unverified": true, "verified": true, "rejected": true}

// setVerification records whether a past result was checked against the source,
// such as a report card from the previous school.
func (a *API) setVerification(w http.ResponseWriter, r *http.Request, id auth.Identity, table, entityType, recordID string) {
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) || !verificationStatuses[req.Status] {
		writeError(w, 400, "VALIDATION_ERROR", "Choose verified, unverified or rejected.", nil)
		return
	}
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var found string
		err := tx.QueryRow(r.Context(), `UPDATE `+table+` x SET verification_status=$3 FROM students s WHERE x.id=$1 AND s.id=x.student_id AND s.school_id=$2 RETURNING x.id::text`, recordID, id.SchoolID, req.Status).Scan(&found)
		if err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, entityType+".verification", entityType, recordID, map[string]any{"status": req.Status})
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": recordID, "verification_status": req.Status}})
	}
}

func (a *API) removeStudentRecord(w http.ResponseWriter, r *http.Request, id auth.Identity, table, entityType, recordID string) {
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var found string
		if err := tx.QueryRow(r.Context(), `DELETE FROM `+table+` x USING students s WHERE x.id=$1 AND s.id=x.student_id AND s.school_id=$2 RETURNING x.id::text`, recordID, id.SchoolID).Scan(&found); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, entityType+".deleted", entityType, recordID, nil)
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": recordID}})
	}
}

func (a *API) verifyAcademicRecord(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.setVerification(w, r, id, "academic_records", "academic_record", r.PathValue("recordID"))
}

func (a *API) deleteAcademicRecord(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.removeStudentRecord(w, r, id, "academic_records", "academic_record", r.PathValue("recordID"))
}

func (a *API) verifyExternalExam(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.setVerification(w, r, id, "external_exams", "external_exam", r.PathValue("examID"))
}

func (a *API) deleteExternalExam(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.removeStudentRecord(w, r, id, "external_exams", "external_exam", r.PathValue("examID"))
}

// archiveQuestion takes a published question out of use: out of practice and
// out of new tests. Tests that already include it keep it, so nothing in
// progress changes.
func (a *API) archiveQuestion(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	questionID := r.PathValue("questionID")
	if a.runInTx(w, r, func(tx pgx.Tx) (bool, error) {
		var status string
		if err := tx.QueryRow(r.Context(), `SELECT status FROM questions WHERE id=$1 AND school_id=$2 FOR UPDATE`, questionID, id.SchoolID).Scan(&status); err != nil {
			return false, err
		}
		if status != "published" {
			writeError(w, 409, "QUESTION_NOT_PUBLISHED", "Only a published question can be archived.", nil)
			return true, nil
		}
		if _, err := tx.Exec(r.Context(), `UPDATE questions SET status='archived',updated_at=now() WHERE id=$1`, questionID); err != nil {
			return false, err
		}
		return false, audit(r.Context(), tx, id, "question.archived", "question", questionID, nil)
	}) {
		writeJSON(w, 200, map[string]any{"data": map[string]string{"id": questionID, "status": "archived"}})
	}
}

// syncStudentLogin follows a student's status with their account. An inactive
// student cannot sign in, and is signed out wherever they were.
func syncStudentLogin(ctx context.Context, tx pgx.Tx, schoolID, studentID, status string) error {
	var userID *string
	if err := tx.QueryRow(ctx, `SELECT user_id::text FROM students WHERE id=$1 AND school_id=$2`, studentID, schoolID).Scan(&userID); err != nil || userID == nil {
		return err
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1 AND status='active'`, *userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, *userID)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1 AND status='disabled'`, *userID)
	return err
}

// emailInUse says whether any account in any school already uses an email.
// Emails are unique across schools, so an invitation that would collide is
// refused before it is sent, not when the person tries to accept it.
func emailInUse(ctx context.Context, q reportQuerier, email string) (bool, error) {
	var used bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1))`, email).Scan(&used)
	return used, err
}

func writeEmailInUse(w http.ResponseWriter) {
	writeError(w, 409, "ACCOUNT_EXISTS", "An account already uses this email, possibly at another school. Use a different email.", map[string]string{"email": "Already in use."})
}

// slogLevel keeps routine refusals quiet. A server failure is an error and is
// also logged, with details, where it happened.
func slogLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status == http.StatusTooManyRequests:
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(body []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(body)
}

// observe gives every request an ID, logs how it went, and sets headers that
// stop browsers from caching school records or framing the API. The log names
// the route pattern, never IDs or query values, so it holds no personal data.
func (a *API) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := make([]byte, 8)
		_, _ = rand.Read(raw)
		requestID := hex.EncodeToString(raw)
		header := w.Header()
		header.Set("X-Request-ID", requestID)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			header.Set("Cache-Control", "no-store")
		}
		recorder := &statusRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		if r.URL.Path == "/healthz" {
			return
		}
		route := r.Pattern
		if route == "" {
			route = r.Method + " (no route)"
		}
		level := slogLevel(recorder.status)
		a.log.Log(r.Context(), level, "request", "route", route, "status", recorder.status, "ms", time.Since(started).Milliseconds(), "request_id", requestID)
	})
}
