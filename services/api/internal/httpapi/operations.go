package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const maxTeachingResourceBytes = 15 << 20

var errTeacherClassNotAssigned = errors.New("teacher class is not assigned")

func (a *API) registerOperationsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/finance/entries", a.admin(a.listFinanceEntries))
	mux.HandleFunc("POST /api/v1/finance/entries", a.admin(a.createFinanceEntry))
	mux.HandleFunc("GET /api/v1/finance/summary", a.admin(a.financeSummary))
	mux.HandleFunc("GET /api/v1/teacher/lesson-plans", a.teacher(a.listLessonPlans))
	mux.HandleFunc("POST /api/v1/teacher/lesson-plans", a.teacher(a.saveLessonPlan))
	mux.HandleFunc("GET /api/v1/teacher/lesson-plans/{planID}", a.teacher(a.getLessonPlan))
	mux.HandleFunc("PUT /api/v1/teacher/lesson-plans/{planID}", a.teacher(a.saveLessonPlan))
	mux.HandleFunc("GET /api/v1/lesson-plans", a.admin(a.listSchoolLessonPlans))
	mux.HandleFunc("PATCH /api/v1/lesson-plans/{planID}/review", a.admin(a.reviewLessonPlan))
	mux.HandleFunc("GET /api/v1/teacher/classes/{classID}/resources", a.teacher(a.listClassResources))
	mux.HandleFunc("POST /api/v1/teacher/classes/{classID}/resources", a.teacher(a.uploadClassResource))
	mux.HandleFunc("GET /api/v1/student/resources", a.student(a.listStudentResources))
	mux.HandleFunc("GET /api/v1/resources/{resourceID}/download", a.authenticated(a.downloadResource))
}

func (a *API) authenticated(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
			return
		}
		id, err := a.auth.Identity(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
			} else {
				a.serverError(w, r, err)
			}
			return
		}
		if !hasRole(id.Roles, "school_admin") && !hasRole(id.Roles, "teacher") && !hasRole(id.Roles, "student") {
			writeError(w, 403, "FORBIDDEN", "This account cannot access teaching resources.", nil)
			return
		}
		next(w, r, id)
	}
}

type financeEntry struct {
	ID           string    `json:"id"`
	EntryType    string    `json:"entry_type"`
	Category     string    `json:"category"`
	Description  string    `json:"description"`
	AmountMinor  int64     `json:"amount_minor"`
	CurrencyCode string    `json:"currency_code"`
	StudentID    *string   `json:"student_id"`
	StudentName  string    `json:"student_name"`
	StaffUserID  *string   `json:"staff_user_id"`
	StaffName    string    `json:"staff_name"`
	DueOn        *string   `json:"due_on"`
	PaidOn       *string   `json:"paid_on"`
	Status       string    `json:"status"`
	Reference    string    `json:"reference"`
	CreatedAt    time.Time `json:"created_at"`
}

func (a *API) listFinanceEntries(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT f.id::text,f.entry_type,f.category,f.description,f.amount_minor,f.currency_code,f.student_id::text,COALESCE(s.legal_name,''),f.staff_user_id::text,COALESCE(u.display_name,''),f.due_on::text,f.paid_on::text,f.status,f.reference,f.created_at FROM finance_entries f LEFT JOIN students s ON s.id=f.student_id AND s.school_id=f.school_id LEFT JOIN users u ON u.id=f.staff_user_id AND u.school_id=f.school_id WHERE f.school_id=$1 ORDER BY f.created_at DESC LIMIT 500`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []financeEntry{}
	for rows.Next() {
		var x financeEntry
		if err = rows.Scan(&x.ID, &x.EntryType, &x.Category, &x.Description, &x.AmountMinor, &x.CurrencyCode, &x.StudentID, &x.StudentName, &x.StaffUserID, &x.StaffName, &x.DueOn, &x.PaidOn, &x.Status, &x.Reference, &x.CreatedAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) createFinanceEntry(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		EntryType    string  `json:"entry_type"`
		Category     string  `json:"category"`
		Description  string  `json:"description"`
		AmountMinor  int64   `json:"amount_minor"`
		CurrencyCode string  `json:"currency_code"`
		StudentID    *string `json:"student_id"`
		StaffUserID  *string `json:"staff_user_id"`
		DueOn        *string `json:"due_on"`
		PaidOn       *string `json:"paid_on"`
		Status       string  `json:"status"`
		Reference    string  `json:"reference"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid finance entry.", nil)
		return
	}
	req.EntryType = strings.TrimSpace(req.EntryType)
	req.Category = strings.TrimSpace(req.Category)
	req.Description = strings.TrimSpace(req.Description)
	req.CurrencyCode = strings.ToUpper(strings.TrimSpace(req.CurrencyCode))
	req.Status = strings.TrimSpace(req.Status)
	req.Reference = strings.TrimSpace(req.Reference)
	validType := map[string]bool{"tuition_charge": true, "payment": true, "expense": true, "salary": true}
	if !validType[req.EntryType] || req.Category == "" || len(req.Category) > 80 || req.Description == "" || len(req.Description) > 500 || req.AmountMinor <= 0 || req.AmountMinor > 1_000_000_000_000 || (req.CurrencyCode != "ETB" && req.CurrencyCode != "USD" && req.CurrencyCode != "EUR") || (req.Status != "recorded" && req.Status != "paid" && req.Status != "void") || (req.EntryType == "salary" && req.StaffUserID == nil) || ((req.EntryType == "tuition_charge" || req.EntryType == "payment") && req.StudentID == nil) {
		writeError(w, 400, "VALIDATION_ERROR", "Provide a valid type, category, description, positive amount in minor units, currency, and the related student or staff member.", nil)
		return
	}
	if req.DueOn != nil {
		if _, err := time.Parse("2006-01-02", *req.DueOn); err != nil {
			writeError(w, 400, "VALIDATION_ERROR", "Due date must use YYYY-MM-DD.", nil)
			return
		}
	}
	if req.PaidOn != nil {
		if _, err := time.Parse("2006-01-02", *req.PaidOn); err != nil {
			writeError(w, 400, "VALIDATION_ERROR", "Payment date must use YYYY-MM-DD.", nil)
			return
		}
	}
	var entryID string
	err := a.db.QueryRow(r.Context(), `INSERT INTO finance_entries(school_id,entry_type,category,description,amount_minor,currency_code,student_id,staff_user_id,due_on,paid_on,status,reference,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id::text`, id.SchoolID, req.EntryType, req.Category, req.Description, req.AmountMinor, req.CurrencyCode, req.StudentID, req.StaffUserID, req.DueOn, req.PaidOn, req.Status, req.Reference, id.UserID).Scan(&entryID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'finance.entry_created','finance_entry',$3,jsonb_build_object('entry_type',$4::text,'amount_minor',$5::bigint,'currency_code',$6::text))`, id.SchoolID, id.UserID, entryID, req.EntryType, req.AmountMinor, req.CurrencyCode)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": entryID}})
}

func (a *API) financeSummary(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var s struct {
		TuitionBilled      int64 `json:"tuition_billed_minor"`
		PaymentsRecorded   int64 `json:"payments_recorded_minor"`
		ExpensesRecorded   int64 `json:"expenses_recorded_minor"`
		SalariesRecorded   int64 `json:"salaries_recorded_minor"`
		OutstandingCharges int64 `json:"outstanding_charges_minor"`
	}
	err := a.db.QueryRow(r.Context(), `SELECT COALESCE(sum(amount_minor) FILTER(WHERE entry_type='tuition_charge' AND status<>'void'),0),COALESCE(sum(amount_minor) FILTER(WHERE entry_type='payment' AND status<>'void'),0),COALESCE(sum(amount_minor) FILTER(WHERE entry_type='expense' AND status<>'void'),0),COALESCE(sum(amount_minor) FILTER(WHERE entry_type='salary' AND status<>'void'),0),GREATEST(0,COALESCE(sum(amount_minor) FILTER(WHERE entry_type='tuition_charge' AND status<>'void'),0)-COALESCE(sum(amount_minor) FILTER(WHERE entry_type='payment' AND status<>'void'),0)) FROM finance_entries WHERE school_id=$1 AND currency_code='ETB'`, id.SchoolID).Scan(&s.TuitionBilled, &s.PaymentsRecorded, &s.ExpensesRecorded, &s.SalariesRecorded, &s.OutstandingCharges)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": s})
}

func (a *API) listLessonPlans(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT id::text,class_id::text,subject,lesson_date::text,duration_minutes,topic,objectives,prior_knowledge,materials,lesson_phases,differentiation,assessment,homework,reflection,status,created_at,updated_at FROM lesson_plans WHERE school_id=$1 AND teacher_id=$2 ORDER BY lesson_date DESC,updated_at DESC LIMIT 200`, id.SchoolID, id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	plans := []map[string]any{}
	for rows.Next() {
		var pid, cid, date, subject, topic, objectives, prior, materials, diff, assess, homework, reflection, status string
		var duration int
		var phases []byte
		var created, updated time.Time
		if err = rows.Scan(&pid, &cid, &subject, &date, &duration, &topic, &objectives, &prior, &materials, &phases, &diff, &assess, &homework, &reflection, &status, &created, &updated); err != nil {
			a.serverError(w, r, err)
			return
		}
		var decoded any
		_ = json.Unmarshal(phases, &decoded)
		plans = append(plans, lessonPlanMap(pid, cid, subject, date, duration, topic, objectives, prior, materials, decoded, diff, assess, homework, reflection, status, created, updated))
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": plans})
}

func lessonPlanMap(pid, cid, subject, date string, duration int, topic, objectives, prior, materials string, phases any, diff, assess, homework, reflection, status string, created, updated time.Time) map[string]any {
	return map[string]any{"id": pid, "class_id": cid, "subject": subject, "lesson_date": date, "duration_minutes": duration, "topic": topic, "objectives": objectives, "prior_knowledge": prior, "materials": materials, "lesson_phases": phases, "differentiation": diff, "assessment": assess, "homework": homework, "reflection": reflection, "status": status, "created_at": created, "updated_at": updated}
}

func (a *API) saveLessonPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ClassID         string `json:"class_id"`
		Subject         string `json:"subject"`
		LessonDate      string `json:"lesson_date"`
		DurationMinutes int    `json:"duration_minutes"`
		Topic           string `json:"topic"`
		Objectives      string `json:"objectives"`
		PriorKnowledge  string `json:"prior_knowledge"`
		Materials       string `json:"materials"`
		LessonPhases    []struct {
			Phase      string `json:"phase"`
			Minutes    int    `json:"minutes"`
			Activities string `json:"activities"`
		} `json:"lesson_phases"`
		Differentiation string `json:"differentiation"`
		Assessment      string `json:"assessment"`
		Homework        string `json:"homework"`
		Reflection      string `json:"reflection"`
		Status          string `json:"status"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid lesson plan.", nil)
		return
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Topic) == "" || strings.TrimSpace(req.Objectives) == "" || req.DurationMinutes < 5 || req.DurationMinutes > 480 || len(req.LessonPhases) > 12 || len(req.Topic) > 200 || len(req.Objectives) > 5000 {
		writeError(w, 400, "VALIDATION_ERROR", "Subject, topic, learning objectives, valid lesson duration, and up to 12 lesson phases are required.", nil)
		return
	}
	if _, err := time.Parse("2006-01-02", req.LessonDate); err != nil {
		writeError(w, 400, "VALIDATION_ERROR", "Lesson date must use YYYY-MM-DD.", nil)
		return
	}
	if req.Status == "" {
		req.Status = "draft"
	}
	if req.Status != "draft" && req.Status != "submitted" {
		writeError(w, 400, "VALIDATION_ERROR", "A teacher may save a draft or submit a plan for review.", nil)
		return
	}
	for _, p := range req.LessonPhases {
		if p.Minutes < 0 || p.Minutes > req.DurationMinutes || len(p.Activities) > 2000 {
			writeError(w, 400, "VALIDATION_ERROR", "Lesson phase time or activities are invalid.", nil)
			return
		}
	}
	if err := a.ensureTeacherClass(r.Context(), id, req.ClassID); err != nil {
		if errors.Is(err, errTeacherClassNotAssigned) {
			writeError(w, 403, "FORBIDDEN", "You can only plan lessons for a class assigned to you.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	phaseJSON, err := json.Marshal(req.LessonPhases)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	planID := r.PathValue("planID")
	if planID == "" {
		err = a.db.QueryRow(r.Context(), `INSERT INTO lesson_plans(school_id,class_id,teacher_id,subject,lesson_date,duration_minutes,topic,objectives,prior_knowledge,materials,lesson_phases,differentiation,assessment,homework,reflection,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id::text`, id.SchoolID, req.ClassID, id.UserID, strings.TrimSpace(req.Subject), req.LessonDate, req.DurationMinutes, strings.TrimSpace(req.Topic), strings.TrimSpace(req.Objectives), req.PriorKnowledge, req.Materials, phaseJSON, req.Differentiation, req.Assessment, req.Homework, req.Reflection, req.Status).Scan(&planID)
	} else {
		tag, e := a.db.Exec(r.Context(), `UPDATE lesson_plans SET class_id=$3,subject=$4,lesson_date=$5,duration_minutes=$6,topic=$7,objectives=$8,prior_knowledge=$9,materials=$10,lesson_phases=$11,differentiation=$12,assessment=$13,homework=$14,reflection=$15,status=$16,updated_at=now(),reviewed_by=NULL,reviewed_at=NULL WHERE id=$1 AND school_id=$2 AND teacher_id=$17 AND status IN ('draft','needs_revision')`, planID, id.SchoolID, req.ClassID, strings.TrimSpace(req.Subject), req.LessonDate, req.DurationMinutes, strings.TrimSpace(req.Topic), strings.TrimSpace(req.Objectives), req.PriorKnowledge, req.Materials, phaseJSON, req.Differentiation, req.Assessment, req.Homework, req.Reflection, req.Status, id.UserID)
		err = e
		if err == nil && tag.RowsAffected() == 0 {
			writeError(w, 404, "NOT_FOUND", "Editable lesson plan not found.", nil)
			return
		}
	}
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": planID, "status": req.Status}})
}

func (a *API) ensureTeacherClass(ctx context.Context, id auth.Identity, classID string) error {
	var allowed bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND class_id=$2 AND teacher_id=$3)`, id.SchoolID, classID, id.UserID).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errTeacherClassNotAssigned
	}
	return nil
}

func (a *API) getLessonPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var p map[string]any
	pid := r.PathValue("planID")
	var cid, subject, date, topic, objectives, prior, materials, diff, assess, homework, reflection, status string
	var duration int
	var phases []byte
	var created, updated time.Time
	err := a.db.QueryRow(r.Context(), `SELECT id::text,class_id::text,subject,lesson_date::text,duration_minutes,topic,objectives,prior_knowledge,materials,lesson_phases,differentiation,assessment,homework,reflection,status,created_at,updated_at FROM lesson_plans WHERE id=$1 AND school_id=$2 AND teacher_id=$3`, pid, id.SchoolID, id.UserID).Scan(&pid, &cid, &subject, &date, &duration, &topic, &objectives, &prior, &materials, &phases, &diff, &assess, &homework, &reflection, &status, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Lesson plan not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var decoded any
	_ = json.Unmarshal(phases, &decoded)
	p = lessonPlanMap(pid, cid, subject, date, duration, topic, objectives, prior, materials, decoded, diff, assess, homework, reflection, status, created, updated)
	writeJSON(w, 200, map[string]any{"data": p})
}

func (a *API) listSchoolLessonPlans(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `SELECT lp.id::text,lp.class_id::text,lp.subject,lp.lesson_date::text,lp.duration_minutes,lp.topic,lp.objectives,lp.prior_knowledge,lp.materials,lp.lesson_phases,lp.differentiation,lp.assessment,lp.homework,lp.reflection,lp.status,lp.created_at,lp.updated_at,u.display_name,c.name,c.grade_level FROM lesson_plans lp JOIN users u ON u.id=lp.teacher_id AND u.school_id=lp.school_id JOIN classes c ON c.id=lp.class_id AND c.school_id=lp.school_id WHERE lp.school_id=$1 ORDER BY lp.lesson_date DESC,lp.updated_at DESC LIMIT 500`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var pid, cid, date, subject, topic, objectives, prior, materials, diff, assess, homework, reflection, status string
		var duration int
		var phases []byte
		var created, updated time.Time
		var teacherName, className, grade string
		if err = rows.Scan(&pid, &cid, &subject, &date, &duration, &topic, &objectives, &prior, &materials, &phases, &diff, &assess, &homework, &reflection, &status, &created, &updated, &teacherName, &className, &grade); err != nil {
			a.serverError(w, r, err)
			return
		}
		var decoded any
		_ = json.Unmarshal(phases, &decoded)
		plan := lessonPlanMap(pid, cid, subject, date, duration, topic, objectives, prior, materials, decoded, diff, assess, homework, reflection, status, created, updated)
		plan["teacher_name"], plan["class_name"], plan["grade_level"] = teacherName, className, grade
		items = append(items, plan)
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) reviewLessonPlan(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) || (req.Status != "approved" && req.Status != "needs_revision") {
		writeError(w, 400, "VALIDATION_ERROR", "Status must be approved or needs_revision.", nil)
		return
	}
	tag, err := a.db.Exec(r.Context(), `UPDATE lesson_plans SET status=$3,reviewed_by=$4,reviewed_at=now(),updated_at=now() WHERE id=$1 AND school_id=$2 AND status='submitted'`, r.PathValue("planID"), id.SchoolID, req.Status, id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "Submitted lesson plan not found.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"status": req.Status}})
}

type resourceMeta struct {
	ID          string    `json:"id"`
	ClassID     string    `json:"class_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Subject     string    `json:"subject"`
	FileName    string    `json:"file_name"`
	MediaType   string    `json:"media_type"`
	FileSize    int64     `json:"file_size"`
	CreatedAt   time.Time `json:"created_at"`
}

func (a *API) uploadClassResource(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if err := a.ensureTeacherClass(r.Context(), id, classID); err != nil {
		if errors.Is(err, errTeacherClassNotAssigned) {
			writeError(w, 403, "FORBIDDEN", "You can only share files with a class assigned to you.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTeachingResourceBytes+(1<<20))
	if err := r.ParseMultipartForm(maxTeachingResourceBytes); err != nil {
		writeError(w, 413, "FILE_TOO_LARGE", "Files must be 15 MB or smaller.", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a resource file to upload.", nil)
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxTeachingResourceBytes+1))
	if err != nil || len(content) == 0 || len(content) > maxTeachingResourceBytes {
		writeError(w, 413, "FILE_TOO_LARGE", "Files must be between 1 byte and 15 MB.", nil)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		title = filepath.Base(header.Filename)
	}
	if len(title) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Title must be 200 characters or fewer.", nil)
		return
	}
	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	name := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if len(name) > 255 {
		name = name[len(name)-255:]
	}
	var resourceID string
	err = a.db.QueryRow(r.Context(), `INSERT INTO teaching_resources(school_id,class_id,uploaded_by,title,description,subject,file_name,media_type,file_size,file_data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text`, id.SchoolID, classID, id.UserID, title, strings.TrimSpace(r.FormValue("description")), strings.TrimSpace(r.FormValue("subject")), name, mediaType, len(content), content).Scan(&resourceID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": resourceID}})
}

func (a *API) listClassResources(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	if !hasRole(id.Roles, "school_admin") {
		var allowed bool
		err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND class_id=$2 AND teacher_id=$3)`, id.SchoolID, classID, id.UserID).Scan(&allowed)
		if err != nil || !allowed {
			if err != nil {
				a.serverError(w, r, err)
				return
			}
			writeError(w, 403, "FORBIDDEN", "This class is not assigned to your account.", nil)
			return
		}
	}
	a.writeResources(w, r, id, "r.class_id=$2", classID)
}
func (a *API) listStudentResources(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	a.writeResources(w, r, id, `r.class_id IN(SELECT e.class_id FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id WHERE s.user_id=$2 AND s.school_id=$1 AND e.status='active')`, id.UserID)
}
func (a *API) writeResources(w http.ResponseWriter, r *http.Request, id auth.Identity, where string, arg any) {
	query := `SELECT r.id::text,r.class_id::text,r.title,r.description,r.subject,r.file_name,r.media_type,r.file_size,r.created_at FROM teaching_resources r WHERE r.school_id=$1 AND ` + where + ` ORDER BY r.created_at DESC LIMIT 500`
	rows, err := a.db.Query(r.Context(), query, id.SchoolID, arg)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []resourceMeta{}
	for rows.Next() {
		var x resourceMeta
		if err = rows.Scan(&x.ID, &x.ClassID, &x.Title, &x.Description, &x.Subject, &x.FileName, &x.MediaType, &x.FileSize, &x.CreatedAt); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, x)
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

func (a *API) downloadResource(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rid := r.PathValue("resourceID")
	var classID, name, media string
	var content []byte
	err := a.db.QueryRow(r.Context(), `SELECT class_id::text,file_name,media_type,file_data FROM teaching_resources WHERE id=$1 AND school_id=$2`, rid, id.SchoolID).Scan(&classID, &name, &media, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Resource not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var allowed bool
	if hasRole(id.Roles, "school_admin") {
		allowed = true
	} else if hasRole(id.Roles, "teacher") {
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND class_id=$2 AND teacher_id=$3)`, id.SchoolID, classID, id.UserID).Scan(&allowed)
	} else {
		err = a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM enrollments e JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id WHERE s.user_id=$1 AND s.school_id=$2 AND e.class_id=$3 AND e.status='active')`, id.UserID, id.SchoolID, classID).Scan(&allowed)
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 403, "FORBIDDEN", "This resource is not shared with your account.", nil)
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(200)
	_, _ = w.Write(content)
}
