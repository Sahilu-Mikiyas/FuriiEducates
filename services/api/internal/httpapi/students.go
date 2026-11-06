package httpapi

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

const (
	maxStudentImportRows  = 3000
	maxStudentImportBytes = 4 << 20
	maxStudentPageSize    = 100
	// The list without paging feeds pickers across the app, so it stays complete
	// for any realistic school.
	maxStudentPickerRows = 5000
)

var (
	phonePattern      = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,18}[0-9]$`)
	guardianRelations = []string{"mother", "father", "guardian", "other"}
)

func (a *API) registerStudentRecordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/students/{studentID}/profile", a.staffMember(a.getStudentProfile))
	mux.HandleFunc("PUT /api/v1/students/{studentID}/profile", a.admin(a.updateStudentProfile))
	mux.HandleFunc("POST /api/v1/students/{studentID}/guardians", a.admin(a.createGuardian))
	mux.HandleFunc("PUT /api/v1/students/{studentID}/guardians/{guardianID}", a.admin(a.updateGuardian))
	mux.HandleFunc("DELETE /api/v1/students/{studentID}/guardians/{guardianID}", a.admin(a.deleteGuardian))
	mux.HandleFunc("POST /api/v1/students/{studentID}/move", a.admin(a.moveStudent))
	mux.HandleFunc("POST /api/v1/students/import", a.admin(a.importStudents))
}

// canSeeStudent allows an administrator, or a teacher of the student's current
// class. A profile holds medical notes and family phone numbers.
func (a *API) canSeeStudent(ctx context.Context, id auth.Identity, studentID string) (bool, error) {
	var allowed bool
	err := a.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM students s WHERE s.id=$1 AND s.school_id=$2 AND ($3
		OR EXISTS(SELECT 1 FROM enrollments e JOIN class_teachers ct ON ct.class_id=e.class_id AND ct.school_id=e.school_id WHERE e.student_id=s.id AND e.school_id=s.school_id AND e.status='active' AND ct.teacher_id=$4)))`,
		studentID, id.SchoolID, hasRole(id.Roles, "school_admin"), id.UserID).Scan(&allowed)
	return allowed, err
}

type guardianRow struct {
	ID           string `json:"id"`
	FullName     string `json:"full_name"`
	Relationship string `json:"relationship"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	IsPrimary    bool   `json:"is_primary"`
	HasAccount   bool   `json:"has_account"`
	Pending      bool   `json:"invite_pending"`
}

func (a *API) getStudentProfile(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID := r.PathValue("studentID")
	allowed, err := a.canSeeStudent(r.Context(), id, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !allowed {
		writeError(w, 404, "NOT_FOUND", "Student not found.", nil)
		return
	}
	var profile struct {
		ID        string `json:"id"`
		Number    string `json:"student_number"`
		Name      string `json:"legal_name"`
		Birth     string `json:"date_of_birth"`
		Gender    string `json:"gender"`
		Language  string `json:"preferred_language"`
		Address   string `json:"address"`
		Medical   string `json:"medical_notes"`
		Stream    string `json:"stream"`
		Status    string `json:"status"`
		Account   bool   `json:"has_account"`
		ClassID   string `json:"class_id"`
		ClassName string `json:"class_name"`
		Grade     string `json:"grade_level"`
	}
	err = a.db.QueryRow(r.Context(), `SELECT s.id::text,s.student_number,s.legal_name,COALESCE(s.date_of_birth::text,''),COALESCE(s.gender,''),COALESCE(s.preferred_language,''),COALESCE(s.address,''),COALESCE(s.medical_notes,''),COALESCE(s.stream,''),s.status,s.user_id IS NOT NULL,
		COALESCE(c.id::text,''),COALESCE(c.name,''),COALESCE(c.grade_level,'')
		FROM students s LEFT JOIN LATERAL (SELECT e.class_id FROM enrollments e WHERE e.student_id=s.id AND e.school_id=s.school_id AND e.status='active' ORDER BY e.enrolled_at DESC LIMIT 1) current ON TRUE
		LEFT JOIN classes c ON c.id=current.class_id
		WHERE s.id=$1 AND s.school_id=$2`, studentID, id.SchoolID).Scan(&profile.ID, &profile.Number, &profile.Name, &profile.Birth, &profile.Gender, &profile.Language, &profile.Address, &profile.Medical, &profile.Stream, &profile.Status, &profile.Account, &profile.ClassID, &profile.ClassName, &profile.Grade)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT g.id::text,g.full_name,g.relationship,g.phone,COALESCE(g.email,''),g.is_primary,g.user_id IS NOT NULL,
		EXISTS(SELECT 1 FROM guardian_invitations i WHERE i.guardian_id=g.id AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now())
		FROM guardians g WHERE g.student_id=$1 AND g.school_id=$2 ORDER BY g.is_primary DESC,g.created_at`, studentID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	guardians := []guardianRow{}
	for rows.Next() {
		var x guardianRow
		if err = rows.Scan(&x.ID, &x.FullName, &x.Relationship, &x.Phone, &x.Email, &x.IsPrimary, &x.HasAccount, &x.Pending); err != nil {
			a.serverError(w, r, err)
			return
		}
		guardians = append(guardians, x)
	}
	if err = rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	rows.Close()
	summary, err := a.attendanceSummary(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"student": profile, "guardians": guardians, "attendance": summary}})
}

func (a *API) updateStudentProfile(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Name     string `json:"legal_name"`
		Birth    string `json:"date_of_birth"`
		Gender   string `json:"gender"`
		Language string `json:"preferred_language"`
		Address  string `json:"address"`
		Medical  string `json:"medical_notes"`
		Status   string `json:"status"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Name, req.Birth, req.Address, req.Medical = strings.TrimSpace(req.Name), strings.TrimSpace(req.Birth), strings.TrimSpace(req.Address), strings.TrimSpace(req.Medical)
	var birth *time.Time
	if req.Birth != "" {
		parsed, err := time.Parse("2006-01-02", req.Birth)
		if err != nil || parsed.After(time.Now()) {
			writeError(w, 400, "VALIDATION_ERROR", "The date of birth is not valid.", nil)
			return
		}
		birth = &parsed
	}
	if req.Name == "" || len(req.Name) > 200 || (req.Gender != "" && req.Gender != "female" && req.Gender != "male") || (req.Status != "active" && req.Status != "inactive") || len(req.Address) > 500 || len(req.Medical) > 2000 {
		writeError(w, 400, "VALIDATION_ERROR", "Check the name, gender and status, and keep notes under 2,000 characters.", nil)
		return
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	studentID := r.PathValue("studentID")
	tag, err := tx.Exec(r.Context(), `UPDATE students SET legal_name=$3,date_of_birth=$4,gender=NULLIF($5,''),preferred_language=NULLIF($6,''),address=NULLIF($7,''),medical_notes=NULLIF($8,''),status=$9 WHERE id=$1 AND school_id=$2`, studentID, id.SchoolID, req.Name, birth, req.Gender, req.Language, req.Address, req.Medical, req.Status)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "Student not found.", nil)
		return
	}
	if err = syncStudentLogin(r.Context(), tx, id.SchoolID, studentID, req.Status); err != nil {
		a.serverError(w, r, err)
		return
	}
	// The log records that the profile changed, not its medical details.
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.profile_updated','student',$3,jsonb_build_object('status',$4::text))`, id.SchoolID, id.UserID, studentID, req.Status); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": studentID}})
}

type guardianInput struct {
	FullName     string `json:"full_name"`
	Relationship string `json:"relationship"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	IsPrimary    bool   `json:"is_primary"`
}

func (g *guardianInput) normalize() string {
	g.FullName, g.Phone, g.Email = strings.TrimSpace(g.FullName), strings.TrimSpace(g.Phone), strings.TrimSpace(g.Email)
	switch {
	case g.FullName == "" || len(g.FullName) > 200:
		return "Enter the guardian's name."
	case !slices.Contains(guardianRelations, g.Relationship):
		return "Choose the guardian's relationship to the student."
	case !phonePattern.MatchString(g.Phone):
		return "Enter a phone number with digits, for example 0911 234 567."
	case g.Email != "" && (len(g.Email) > 254 || !strings.Contains(g.Email, "@")):
		return "The email address is not valid."
	}
	return ""
}

func (a *API) createGuardian(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var g guardianInput
	if !decode(w, r, &g) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := g.normalize(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	studentID := r.PathValue("studentID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var locked string
	if err = tx.QueryRow(r.Context(), `SELECT id::text FROM students WHERE id=$1 AND school_id=$2 FOR UPDATE`, studentID, id.SchoolID).Scan(&locked); err != nil {
		a.databaseError(w, r, err)
		return
	}
	guardianID, err := insertGuardian(r.Context(), tx, id.SchoolID, studentID, g)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = auditGuardian(r.Context(), tx, id, "guardian.created", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"id": guardianID}})
}

// insertGuardian makes the student's first guardian the primary contact, so an
// absence always has someone to call.
func insertGuardian(ctx context.Context, tx pgx.Tx, schoolID, studentID string, g guardianInput) (string, error) {
	var existing int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM guardians WHERE student_id=$1 AND school_id=$2`, studentID, schoolID).Scan(&existing); err != nil {
		return "", err
	}
	primary := g.IsPrimary || existing == 0
	if primary {
		if _, err := tx.Exec(ctx, `UPDATE guardians SET is_primary=false WHERE student_id=$1 AND school_id=$2 AND is_primary`, studentID, schoolID); err != nil {
			return "", err
		}
	}
	var guardianID string
	err := tx.QueryRow(ctx, `INSERT INTO guardians(school_id,student_id,full_name,relationship,phone,email,is_primary) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),$7) RETURNING id::text`, schoolID, studentID, g.FullName, g.Relationship, g.Phone, g.Email, primary).Scan(&guardianID)
	return guardianID, err
}

func auditGuardian(ctx context.Context, tx pgx.Tx, id auth.Identity, action, studentID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,$3,'student',$4,'{}'::jsonb)`, id.SchoolID, id.UserID, action, studentID)
	return err
}

func (a *API) updateGuardian(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var g guardianInput
	if !decode(w, r, &g) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	if message := g.normalize(); message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	studentID, guardianID := r.PathValue("studentID"), r.PathValue("guardianID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var wasPrimary bool
	err = tx.QueryRow(r.Context(), `SELECT is_primary FROM guardians WHERE id=$1 AND student_id=$2 AND school_id=$3 FOR UPDATE`, guardianID, studentID, id.SchoolID).Scan(&wasPrimary)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if g.IsPrimary && !wasPrimary {
		if _, err = tx.Exec(r.Context(), `UPDATE guardians SET is_primary=false WHERE student_id=$1 AND school_id=$2 AND is_primary`, studentID, id.SchoolID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	// The primary contact stays primary until another guardian takes over.
	if _, err = tx.Exec(r.Context(), `UPDATE guardians SET full_name=$2,relationship=$3,phone=$4,email=NULLIF($5,''),is_primary=$6 WHERE id=$1`, guardianID, g.FullName, g.Relationship, g.Phone, g.Email, g.IsPrimary || wasPrimary); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = auditGuardian(r.Context(), tx, id, "guardian.updated", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": guardianID}})
}

func (a *API) deleteGuardian(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, guardianID := r.PathValue("studentID"), r.PathValue("guardianID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var wasPrimary bool
	err = tx.QueryRow(r.Context(), `DELETE FROM guardians WHERE id=$1 AND student_id=$2 AND school_id=$3 RETURNING is_primary`, guardianID, studentID, id.SchoolID).Scan(&wasPrimary)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if wasPrimary {
		if _, err = tx.Exec(r.Context(), `UPDATE guardians SET is_primary=true WHERE id=(SELECT id FROM guardians WHERE student_id=$1 AND school_id=$2 ORDER BY created_at LIMIT 1)`, studentID, id.SchoolID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if err = auditGuardian(r.Context(), tx, id, "guardian.deleted", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": guardianID}})
}

// moveStudent changes a student's class. Their attendance and results stay with
// the class they were earned in.
func (a *API) moveStudent(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		ClassID string `json:"class_id"`
	}
	if !decode(w, r, &req) || !uuidPattern.MatchString(req.ClassID) {
		writeError(w, 400, "VALIDATION_ERROR", "Choose a class.", nil)
		return
	}
	studentID := r.PathValue("studentID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var locked string
	if err = tx.QueryRow(r.Context(), `SELECT id::text FROM students WHERE id=$1 AND school_id=$2 FOR UPDATE`, studentID, id.SchoolID).Scan(&locked); err != nil {
		a.databaseError(w, r, err)
		return
	}
	var classExists, alreadyThere bool
	err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM classes WHERE id=$1 AND school_id=$2),EXISTS(SELECT 1 FROM enrollments WHERE class_id=$1 AND student_id=$3 AND status='active')`, req.ClassID, id.SchoolID, studentID).Scan(&classExists, &alreadyThere)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !classExists {
		writeError(w, 400, "VALIDATION_ERROR", "That class is not in this school.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE enrollments SET status='ended',ended_at=GREATEST(CURRENT_DATE,enrolled_at) WHERE student_id=$1 AND school_id=$2 AND status='active' AND class_id<>$3`, studentID, id.SchoolID, req.ClassID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !alreadyThere {
		if _, err = tx.Exec(r.Context(), `INSERT INTO enrollments(school_id,student_id,class_id) VALUES($1,$2,$3)`, id.SchoolID, studentID, req.ClassID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.moved','student',$3,jsonb_build_object('class_id',$4::text))`, id.SchoolID, id.UserID, studentID, req.ClassID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": studentID, "class_id": req.ClassID}})
}

// ethiopianDateToISO converts an Ethiopian date written YYYY-MM-DD. Schools keep
// birth dates in the Ethiopian calendar, so imports must accept it.
func ethiopianDateToISO(value string) (string, bool) {
	parts := strings.Split(value, "-")
	if len(parts) != 3 {
		return "", false
	}
	year, errYear := strconv.Atoi(parts[0])
	month, errMonth := strconv.Atoi(parts[1])
	day, errDay := strconv.Atoi(parts[2])
	if errYear != nil || errMonth != nil || errDay != nil || year < 1800 || year > 2300 || month < 1 || month > 13 || day < 1 {
		return "", false
	}
	limit := 30
	if month == 13 {
		limit = 5
		if year%4 == 3 {
			limit = 6
		}
	}
	if day > limit {
		return "", false
	}
	const ethiopianEpoch = 1724221
	julianDay := ethiopianEpoch + 365*(year-1) + year/4 + 30*(month-1) + day - 1
	// The Julian day number of 1970-01-01 is 2440588.
	return time.Unix(int64(julianDay-2440588)*86400, 0).UTC().Format("2006-01-02"), true
}

type studentImportRow struct {
	row                     int
	number, name, birth     string
	gender, stream          string
	grade, section, classID string
	guardian                *guardianInput
}

// parseStudentCSV reads the student import format. The header names the columns,
// so their order does not matter and optional columns can be left out.
func parseStudentCSV(body io.Reader) ([]studentImportRow, []map[string]any, string) {
	reader := csv.NewReader(body)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, nil, "The file is empty or is not a CSV."
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(name, "\xef\xbb\xbf")))] = i
	}
	for _, required := range []string{"student_number", "full_name"} {
		if _, ok := columns[required]; !ok {
			return nil, nil, "The file is missing the " + required + " column."
		}
	}
	rows := []studentImportRow{}
	rowErrors := []map[string]any{}
	seen := map[string]int{}
	for line := 2; ; line++ {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, rowErrors, ""
		}
		if line-1 > maxStudentImportRows {
			return nil, nil, fmt.Sprintf("Import up to %d students at a time.", maxStudentImportRows)
		}
		fail := func(message string) { rowErrors = append(rowErrors, map[string]any{"row": line, "error": message}) }
		if err != nil {
			fail("This row could not be read.")
			continue
		}
		cell := func(name string) string {
			if i, ok := columns[name]; ok && i < len(record) {
				return strings.TrimSpace(record[i])
			}
			return ""
		}
		x := studentImportRow{row: line, number: cell("student_number"), name: cell("full_name"), gender: strings.ToLower(cell("gender")), stream: strings.ToLower(cell("stream")), grade: cell("grade"), section: cell("section")}
		switch {
		case x.number == "" || len(x.number) > 80:
			fail("Enter a student number.")
			continue
		case x.name == "" || len(x.name) > 200:
			fail("Enter the student's name.")
			continue
		case x.gender != "" && x.gender != "female" && x.gender != "male":
			fail("Gender must be female or male.")
			continue
		case x.stream != "" && x.stream != "natural" && x.stream != "social":
			fail("Stream must be natural or social.")
			continue
		case (x.grade == "") != (x.section == ""):
			fail("Fill in both grade and section, or neither.")
			continue
		}
		if first, repeated := seen[strings.ToLower(x.number)]; repeated {
			fail(fmt.Sprintf("Student number %s is also on row %d.", x.number, first))
			continue
		}
		seen[strings.ToLower(x.number)] = line
		if value := cell("date_of_birth"); value != "" {
			parsed, err := time.Parse("2006-01-02", value)
			if err != nil || parsed.After(time.Now()) || parsed.Year() < 1900 {
				fail("date_of_birth must be a past date written YYYY-MM-DD.")
				continue
			}
			x.birth = value
		} else if value := cell("date_of_birth_ec"); value != "" {
			iso, ok := ethiopianDateToISO(value)
			if !ok || iso > time.Now().Format("2006-01-02") {
				fail("date_of_birth_ec must be a past Ethiopian date written YYYY-MM-DD.")
				continue
			}
			x.birth = iso
		}
		if cell("guardian_name") != "" || cell("guardian_phone") != "" {
			relationship := strings.ToLower(cell("guardian_relationship"))
			if relationship == "" {
				relationship = "guardian"
			}
			guardian := guardianInput{FullName: cell("guardian_name"), Relationship: relationship, Phone: cell("guardian_phone"), IsPrimary: true}
			if message := guardian.normalize(); message != "" {
				fail(message)
				continue
			}
			x.guardian = &guardian
		}
		rows = append(rows, x)
	}
}

// saveImportedStudents writes a checked file with one statement per table, so a
// whole school's file stays quick when the database is far from the API.
func saveImportedStudents(ctx context.Context, tx pgx.Tx, schoolID string, rows []studentImportRow) error {
	var numbers, names, births, genders, streams []string
	for _, x := range rows {
		numbers, names, births = append(numbers, x.number), append(names, x.name), append(births, x.birth)
		genders, streams = append(genders, x.gender), append(streams, x.stream)
	}
	created, err := tx.Query(ctx, `INSERT INTO students(school_id,student_number,legal_name,date_of_birth,gender,stream)
		SELECT $1,x.number,x.name,NULLIF(x.birth,'')::date,NULLIF(x.gender,''),NULLIF(x.stream,'') FROM unnest($2::text[],$3::text[],$4::text[],$5::text[],$6::text[]) AS x(number,name,birth,gender,stream)
		RETURNING student_number,id::text`, schoolID, numbers, names, births, genders, streams)
	if err != nil {
		return err
	}
	studentIDs := map[string]string{}
	for created.Next() {
		var number, studentID string
		if err = created.Scan(&number, &studentID); err != nil {
			created.Close()
			return err
		}
		studentIDs[number] = studentID
	}
	created.Close()
	if err = created.Err(); err != nil {
		return err
	}
	var enrolled, classIDs, guarded, guardianNames, relationships, phones []string
	for _, x := range rows {
		if x.classID != "" {
			enrolled, classIDs = append(enrolled, studentIDs[x.number]), append(classIDs, x.classID)
		}
		if x.guardian != nil {
			guarded, guardianNames = append(guarded, studentIDs[x.number]), append(guardianNames, x.guardian.FullName)
			relationships, phones = append(relationships, x.guardian.Relationship), append(phones, x.guardian.Phone)
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO enrollments(school_id,student_id,class_id) SELECT $1,x.student_id,x.class_id FROM unnest($2::uuid[],$3::uuid[]) AS x(student_id,class_id)`, schoolID, enrolled, classIDs); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO guardians(school_id,student_id,full_name,relationship,phone,is_primary) SELECT $1,x.student_id,x.full_name,x.relationship,x.phone,true FROM unnest($2::uuid[],$3::text[],$4::text[],$5::text[]) AS x(student_id,full_name,relationship,phone)`, schoolID, guarded, guardianNames, relationships, phones)
	return err
}

// importStudents adds students from a CSV. Any bad row stops the whole import,
// so a school never ends up with half a file. A likely duplicate also stops it
// unless the school says to continue.
func (a *API) importStudents(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, rowErrors, message := parseStudentCSV(http.MaxBytesReader(w, r.Body, maxStudentImportBytes))
	if message != "" {
		writeError(w, 400, "VALIDATION_ERROR", message, nil)
		return
	}
	if len(rowErrors) == 0 && len(rows) == 0 {
		writeError(w, 400, "VALIDATION_ERROR", "The file has no students.", nil)
		return
	}
	allowDuplicates := r.URL.Query().Get("allow_duplicates") == "true"
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	// Classes are matched in the newest academic year that has that grade and section.
	classRows, err := tx.Query(r.Context(), `SELECT DISTINCT ON (lower(c.grade_level),lower(c.name)) lower(c.grade_level),lower(c.name),c.id::text FROM classes c JOIN academic_years y ON y.id=c.academic_year_id WHERE c.school_id=$1 ORDER BY lower(c.grade_level),lower(c.name),y.starts_on DESC`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	classes := map[string]string{}
	for classRows.Next() {
		var grade, name, classID string
		if err = classRows.Scan(&grade, &name, &classID); err != nil {
			classRows.Close()
			a.serverError(w, r, err)
			return
		}
		classes[grade+"|"+name] = classID
	}
	classRows.Close()
	if err = classRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	existingRows, err := tx.Query(r.Context(), `SELECT lower(student_number),student_number,lower(legal_name),COALESCE(date_of_birth::text,'') FROM students WHERE school_id=$1`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	numbers := map[string]bool{}
	people := map[string]string{}
	for existingRows.Next() {
		var lowerNumber, number, name, birth string
		if err = existingRows.Scan(&lowerNumber, &number, &name, &birth); err != nil {
			existingRows.Close()
			a.serverError(w, r, err)
			return
		}
		numbers[lowerNumber] = true
		people[name+"|"+birth] = number
	}
	existingRows.Close()
	if err = existingRows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	for i := range rows {
		x := &rows[i]
		fail := func(message string) { rowErrors = append(rowErrors, map[string]any{"row": x.row, "error": message}) }
		if x.grade != "" {
			classID, found := classes[strings.ToLower(x.grade)+"|"+strings.ToLower(x.section)]
			if !found {
				fail(fmt.Sprintf("No class %s, section %s. Create it first.", x.grade, x.section))
				continue
			}
			x.classID = classID
		}
		if numbers[strings.ToLower(x.number)] {
			fail(fmt.Sprintf("Student number %s already exists.", x.number))
			continue
		}
		if number, likely := people[strings.ToLower(x.name)+"|"+x.birth]; likely && !allowDuplicates {
			fail(fmt.Sprintf("%s may already be enrolled as %s.", x.name, number))
		}
	}
	if len(rowErrors) > 0 {
		slices.SortFunc(rowErrors, func(p, q map[string]any) int { return p["row"].(int) - q["row"].(int) })
		writeJSON(w, 422, map[string]any{"error": map[string]any{"code": "IMPORT_HAS_ERRORS", "message": "Fix the listed rows and import the file again. Nothing was saved.", "row_errors": rowErrors}})
		return
	}
	if err = saveImportedStudents(r.Context(), tx, id.SchoolID, rows); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'student.imported','school',$1,jsonb_build_object('count',$3::int))`, id.SchoolID, id.UserID, len(rows)); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]int{"imported": len(rows)}})
}
