package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *API) admin(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
			return
		}
		identity, err := a.auth.Identity(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
			} else {
				a.serverError(w, r, err)
			}
			return
		}
		if !hasRole(identity.Roles, "school_admin") {
			writeError(w, 403, "FORBIDDEN", "This action requires a school administrator.", nil)
			return
		}
		next(w, r, identity)
	}
}

func hasRole(roles []string, expected string) bool {
	for _, role := range roles {
		if role == expected {
			return true
		}
	}
	return false
}

func (a *API) registerSchoolRoutes(mux *http.ServeMux) {
	a.registerAcademicRoutes(mux)
	a.registerCurriculumRoutes(mux)
	mux.HandleFunc("GET /api/v1/school", a.admin(a.getSchool))
	mux.HandleFunc("PATCH /api/v1/school", a.admin(a.updateSchool))
	mux.HandleFunc("GET /api/v1/academic-years", a.admin(a.listAcademicYears))
	mux.HandleFunc("POST /api/v1/academic-years", a.admin(a.createAcademicYear))
	mux.HandleFunc("GET /api/v1/classes", a.admin(a.listClasses))
	mux.HandleFunc("POST /api/v1/classes", a.admin(a.createClass))
	mux.HandleFunc("GET /api/v1/students", a.admin(a.listStudents))
	mux.HandleFunc("POST /api/v1/students", a.admin(a.createStudent))
	mux.HandleFunc("GET /api/v1/students/{studentID}", a.admin(a.getStudent))
	mux.HandleFunc("PATCH /api/v1/students/{studentID}", a.admin(a.updateStudent))
}

func (a *API) getSchool(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var school struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Timezone        string `json:"timezone"`
		DefaultLanguage string `json:"default_language"`
	}
	err := a.db.QueryRow(r.Context(), `SELECT id::text,name,timezone,default_language FROM schools WHERE id=$1`, id.SchoolID).Scan(&school.ID, &school.Name, &school.Timezone, &school.DefaultLanguage)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": school})
}

func (a *API) updateSchool(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name            string `json:"name"`
		Timezone        string `json:"timezone"`
		DefaultLanguage string `json:"default_language"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Timezone = strings.TrimSpace(req.Timezone)
	req.DefaultLanguage = strings.TrimSpace(req.DefaultLanguage)
	if req.Name == "" || len(req.Name) > 200 || req.Timezone == "" || len(req.DefaultLanguage) < 2 || len(req.DefaultLanguage) > 16 {
		writeError(w, 400, "VALIDATION_ERROR", "School name, timezone, and language are required.", nil)
		return
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		writeError(w, 400, "VALIDATION_ERROR", "Timezone must be a valid IANA timezone.", map[string]string{"timezone": "Unknown timezone."})
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var school struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Timezone        string `json:"timezone"`
		DefaultLanguage string `json:"default_language"`
	}
	err = tx.QueryRow(r.Context(), `UPDATE schools SET name=$2,timezone=$3,default_language=$4 WHERE id=$1 RETURNING id::text,name,timezone,default_language`, id.SchoolID, req.Name, req.Timezone, req.DefaultLanguage).Scan(&school.ID, &school.Name, &school.Timezone, &school.DefaultLanguage)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'school.updated','school',$1,jsonb_build_object('name',$3::text,'timezone',$4::text,'default_language',$5::text))`, id.SchoolID, id.UserID, req.Name, req.Timezone, req.DefaultLanguage)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": school})
}

func (a *API) listAcademicYears(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT id::text,name,starts_on::text,ends_on::text FROM academic_years WHERE school_id=$1 ORDER BY starts_on DESC LIMIT 100`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	years := []map[string]string{}
	for rows.Next() {
		var x struct{ ID, Name, StartsOn, EndsOn string }
		if err := rows.Scan(&x.ID, &x.Name, &x.StartsOn, &x.EndsOn); err != nil {
			a.serverError(w, r, err)
			return
		}
		years = append(years, map[string]string{"id": x.ID, "name": x.Name, "starts_on": x.StartsOn, "ends_on": x.EndsOn})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": years})
}

func (a *API) createAcademicYear(w http.ResponseWriter, r *http.Request, id auth.Identity) {
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
	start, e1 := time.Parse("2006-01-02", req.StartsOn)
	end, e2 := time.Parse("2006-01-02", req.EndsOn)
	if req.Name == "" || len(req.Name) > 100 || e1 != nil || e2 != nil || !end.After(start) {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a name and valid dates with the end after the start.", nil)
		return
	}
	var result map[string]string
	var year struct{ ID, Name, StartsOn, EndsOn string }
	err := a.db.QueryRow(r.Context(), `INSERT INTO academic_years(school_id,name,starts_on,ends_on) VALUES($1,$2,$3,$4) RETURNING id::text,name,starts_on::text,ends_on::text`, id.SchoolID, req.Name, start, end).Scan(&year.ID, &year.Name, &year.StartsOn, &year.EndsOn)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	result = map[string]string{"id": year.ID, "name": year.Name, "starts_on": year.StartsOn, "ends_on": year.EndsOn}
	writeJSON(w, 201, map[string]any{"data": result})
}

func (a *API) listClasses(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT c.id::text,c.name,c.grade_level,c.academic_year_id::text,y.name FROM classes c JOIN academic_years y ON y.id=c.academic_year_id AND y.school_id=c.school_id WHERE c.school_id=$1 ORDER BY y.starts_on DESC,c.grade_level,c.name LIMIT 200`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var x [5]string
		if err := rows.Scan(&x[0], &x[1], &x[2], &x[3], &x[4]); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{"id": x[0], "name": x[1], "grade_level": x[2], "academic_year_id": x[3], "academic_year_name": x[4]})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createClass(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name           string `json:"name"`
		GradeLevel     string `json:"grade_level"`
		AcademicYearID string `json:"academic_year_id"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.GradeLevel = strings.TrimSpace(req.GradeLevel)
	if req.Name == "" || len(req.Name) > 100 || req.GradeLevel == "" || len(req.GradeLevel) > 40 || req.AcademicYearID == "" {
		writeError(w, 400, "VALIDATION_ERROR", "Class name, grade level, and academic year are required.", nil)
		return
	}
	var result map[string]string
	var x [4]string
	err := a.db.QueryRow(r.Context(), `INSERT INTO classes(school_id,academic_year_id,name,grade_level) VALUES($1,$2,$3,$4) RETURNING id::text,name,grade_level,academic_year_id::text`, id.SchoolID, req.AcademicYearID, req.Name, req.GradeLevel).Scan(&x[0], &x[1], &x[2], &x[3])
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	result = map[string]string{"id": x[0], "name": x[1], "grade_level": x[2], "academic_year_id": x[3]}
	writeJSON(w, 201, map[string]any{"data": result})
}

func (a *API) listStudents(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(s.preferred_language,''),s.status FROM students s WHERE s.school_id=$1 ORDER BY s.legal_name,s.student_number LIMIT 200`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var x [5]string
		if err := rows.Scan(&x[0], &x[1], &x[2], &x[3], &x[4]); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{"id": x[0], "student_number": x[1], "legal_name": x[2], "preferred_language": x[3], "status": x[4]})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createStudent(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		StudentNumber     string `json:"student_number"`
		LegalName         string `json:"legal_name"`
		DateOfBirth       string `json:"date_of_birth"`
		PreferredLanguage string `json:"preferred_language"`
		ClassID           string `json:"class_id"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.StudentNumber = strings.TrimSpace(req.StudentNumber)
	req.LegalName = strings.TrimSpace(req.LegalName)
	if req.StudentNumber == "" || len(req.StudentNumber) > 80 || req.LegalName == "" || len(req.LegalName) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Student number and legal name are required.", nil)
		return
	}
	var dob any
	if req.DateOfBirth != "" {
		parsed, err := time.Parse("2006-01-02", req.DateOfBirth)
		if err != nil {
			writeError(w, 400, "VALIDATION_ERROR", "Date of birth must use YYYY-MM-DD.", map[string]string{"date_of_birth": "Invalid date."})
			return
		}
		dob = parsed
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var student struct {
		ID                string `json:"id"`
		StudentNumber     string `json:"student_number"`
		LegalName         string `json:"legal_name"`
		PreferredLanguage string `json:"preferred_language"`
		Status            string `json:"status"`
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO students(school_id,student_number,legal_name,date_of_birth,preferred_language) VALUES($1,$2,$3,$4,NULLIF($5,'')) RETURNING id::text,student_number,legal_name,COALESCE(preferred_language,''),status`, id.SchoolID, req.StudentNumber, req.LegalName, dob, strings.TrimSpace(req.PreferredLanguage)).Scan(&student.ID, &student.StudentNumber, &student.LegalName, &student.PreferredLanguage, &student.Status)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if req.ClassID != "" {
		tag, e := tx.Exec(r.Context(), `INSERT INTO enrollments(school_id,student_id,class_id) SELECT $1,$2,c.id FROM classes c WHERE c.id=$3 AND c.school_id=$1`, id.SchoolID, student.ID, req.ClassID)
		if e != nil {
			a.databaseError(w, r, e)
			return
		}
		if tag.RowsAffected() != 1 {
			writeError(w, 400, "VALIDATION_ERROR", "Class does not belong to this school.", map[string]string{"class_id": "Unknown class."})
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.created','student',$3,jsonb_build_object('student_number',$4::text,'class_id',NULLIF($5::text,'')))`, id.SchoolID, id.UserID, student.ID, student.StudentNumber, req.ClassID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": student})
}

func (a *API) getStudent(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	student, err := a.fetchStudent(r.Context(), id.SchoolID, r.PathValue("studentID"))
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": student})
}

func (a *API) updateStudent(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		LegalName         string `json:"legal_name"`
		PreferredLanguage string `json:"preferred_language"`
		Status            string `json:"status"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.LegalName = strings.TrimSpace(req.LegalName)
	if req.LegalName == "" || len(req.LegalName) > 200 || (req.Status != "active" && req.Status != "inactive") {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a legal name and status of active or inactive.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var studentID string
	err = tx.QueryRow(r.Context(), `UPDATE students SET legal_name=$3,preferred_language=NULLIF($4,''),status=$5 WHERE id=$1 AND school_id=$2 RETURNING id::text`, r.PathValue("studentID"), id.SchoolID, req.LegalName, strings.TrimSpace(req.PreferredLanguage), req.Status).Scan(&studentID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.updated','student',$3,jsonb_build_object('legal_name',$4::text,'preferred_language',$5::text,'status',$6::text))`, id.SchoolID, id.UserID, studentID, req.LegalName, strings.TrimSpace(req.PreferredLanguage), req.Status); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	student, err := a.fetchStudent(r.Context(), id.SchoolID, r.PathValue("studentID"))
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": student})
}

func (a *API) fetchStudent(ctx context.Context, schoolID, studentID string) (map[string]any, error) {
	var item struct{ ID, StudentNumber, LegalName, DateOfBirth, PreferredLanguage, Status string }
	err := a.db.QueryRow(ctx, `SELECT id::text,student_number,legal_name,COALESCE(date_of_birth::text,''),COALESCE(preferred_language,''),status FROM students WHERE id=$1 AND school_id=$2`, studentID, schoolID).Scan(&item.ID, &item.StudentNumber, &item.LegalName, &item.DateOfBirth, &item.PreferredLanguage, &item.Status)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": item.ID, "student_number": item.StudentNumber, "legal_name": item.LegalName, "date_of_birth": item.DateOfBirth, "preferred_language": item.PreferredLanguage, "status": item.Status}, nil
}

func (a *API) databaseError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "The requested record was not found.", nil)
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			writeError(w, 409, "CONFLICT", "A record with the same identifier already exists.", nil)
			return
		}
		if pgErr.Code == "23503" || pgErr.Code == "23514" {
			writeError(w, 400, "VALIDATION_ERROR", "A referenced record is invalid or the data violates a constraint.", nil)
			return
		}
	}
	a.serverError(w, r, err)
}
