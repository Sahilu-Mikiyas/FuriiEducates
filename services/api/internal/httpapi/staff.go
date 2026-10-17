package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const staffInviteTTL = 7 * 24 * time.Hour

func (a *API) registerStaffRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/staff", a.admin(a.listStaff))
	mux.HandleFunc("GET /api/v1/staff/invitations", a.admin(a.listStaffInvitations))
	mux.HandleFunc("POST /api/v1/staff/invitations", a.admin(a.createStaffInvitation))
	mux.HandleFunc("POST /api/v1/staff/invitations/{invitationID}/revoke", a.admin(a.revokeStaffInvitation))
	mux.HandleFunc("PATCH /api/v1/staff/{staffID}", a.admin(a.updateStaffStatus))
	mux.HandleFunc("POST /api/v1/staff/invitations/accept", a.acceptStaffInvitation)
	mux.HandleFunc("POST /api/v1/classes/{classID}/teachers", a.admin(a.assignTeacher))
	mux.HandleFunc("GET /api/v1/teacher/classes", a.teacher(a.listTeacherClasses))
	mux.HandleFunc("GET /api/v1/teacher/classes/{classID}/students", a.teacher(a.listTeacherClassStudents))
}

func (a *API) teacher(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
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
		if !hasRole(identity.Roles, "teacher") {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "This action requires a teacher account.", nil)
			return
		}
		next(w, r, identity)
	}
}

func (a *API) listStaff(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT u.id::text, u.email, u.display_name, u.status,
		       COALESCE(array_agg(ct.class_id::text ORDER BY ct.assigned_at) FILTER (WHERE ct.class_id IS NOT NULL), '{}')
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id AND ur.role = 'teacher'
		LEFT JOIN class_teachers ct ON ct.teacher_id = u.id AND ct.school_id = u.school_id
		WHERE u.school_id = $1
		GROUP BY u.id
		ORDER BY lower(u.display_name), lower(u.email)
		LIMIT 500`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()

	type staffMember struct {
		ID          string   `json:"id"`
		Email       string   `json:"email"`
		DisplayName string   `json:"display_name"`
		Status      string   `json:"status"`
		ClassIDs    []string `json:"class_ids"`
	}
	items := []staffMember{}
	for rows.Next() {
		var item staffMember
		if err := rows.Scan(&item.ID, &item.Email, &item.DisplayName, &item.Status, &item.ClassIDs); err != nil {
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

func (a *API) listStaffInvitations(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT id::text, email, role, created_at, expires_at
		FROM staff_invitations
		WHERE school_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		ORDER BY created_at DESC LIMIT 200`, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var id, email, role string
		var created, expires time.Time
		if err := rows.Scan(&id, &email, &role, &created, &expires); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{
			"id": id, "email": email, "role": role,
			"created_at": created.UTC().Format(time.RFC3339),
			"expires_at": expires.UTC().Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (a *API) createStaffInvitation(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	parsedEmail, err := mail.ParseAddress(email)
	if err != nil || parsedEmail.Address != email || len(email) > 254 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter a valid email address.", map[string]string{"email": "Invalid email address."})
		return
	}
	var existing bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users u WHERE u.school_id=$1 AND lower(u.email)=lower($2))`, id.SchoolID, email).Scan(&existing); err != nil {
		a.serverError(w, r, err)
		return
	}
	if existing {
		writeError(w, http.StatusConflict, "ACCOUNT_EXISTS", "An account already uses that email.", map[string]string{"email": "Already in use."})
		return
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		a.serverError(w, r, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(staffInviteTTL)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var invitationID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO staff_invitations (school_id,email,role,token_hash,created_by,expires_at)
		VALUES ($1,$2,'teacher',$3,$4,$5) RETURNING id::text`, id.SchoolID, email, tokenHash[:], id.UserID, expiresAt).Scan(&invitationID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "INVITATION_EXISTS", "A teacher account or pending invitation already uses that email.", map[string]string{"email": "Already in use."})
			return
		}
		a.serverError(w, r, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'staff.invited','staff_invitation',$3,jsonb_build_object('email',$4::text,'role','teacher'))`, id.SchoolID, id.UserID, invitationID, email)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{
		"id": invitationID, "email": email, "role": "teacher", "token": token,
		"expires_at": expiresAt.Format(time.RFC3339),
	}})
}

func (a *API) revokeStaffInvitation(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	invitationID := r.PathValue("invitationID")
	tag, err := a.db.Exec(r.Context(), `UPDATE staff_invitations SET revoked_at=now() WHERE id=$1 AND school_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL`, invitationID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Pending invitation was not found.", nil)
		return
	}
	_, err = a.db.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id) VALUES($1,$2,'staff.invitation_revoked','staff_invitation',$3)`, id.SchoolID, id.UserID, invitationID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"revoked": true}})
}

func (a *API) updateStaffStatus(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) || (req.Status != "active" && req.Status != "disabled") {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Status must be active or disabled.", nil)
		return
	}
	staffID := r.PathValue("staffID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var email string
	err = tx.QueryRow(r.Context(), `
		UPDATE users u SET status=$3
		WHERE u.id=$1 AND u.school_id=$2 AND EXISTS (
			SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role='teacher'
		)
		RETURNING u.email`, staffID, id.SchoolID, req.Status).Scan(&email)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Teacher account was not found in this school.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if req.Status == "disabled" {
		if _, err := tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, staffID); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id,changes) VALUES($1,$2,'staff.status_changed','user',$3,jsonb_build_object('email',$4::text,'status',$5::text))`, id.SchoolID, id.UserID, staffID, email, req.Status); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"id": staffID, "email": email, "status": req.Status}})
}

func (a *API) acceptStaffInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if len(req.Token) < 32 || len(req.Token) > 128 || req.DisplayName == "" || len(req.DisplayName) > 200 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Invitation and display name are required.", nil)
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 1024 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Password must contain at least 12 characters.", map[string]string{"password": "Use at least 12 characters."})
		return
	}
	tokenHash := sha256.Sum256([]byte(req.Token))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	var invitationID, schoolID, email, role string
	var expiresAt time.Time
	err = tx.QueryRow(r.Context(), `
		SELECT id::text,school_id::text,email,role,expires_at
		FROM staff_invitations
		WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL
		FOR UPDATE`, tokenHash[:]).Scan(&invitationID, &schoolID, &email, &role, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !expiresAt.After(time.Now())) {
		writeError(w, http.StatusBadRequest, "INVITATION_INVALID", "This invitation is invalid, expired, or already used.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var userID string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(school_id,email,password_hash,display_name) VALUES($1,$2,$3,$4) RETURNING id::text`, schoolID, email, hash, req.DisplayName).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "ACCOUNT_EXISTS", "An account already uses this email. Contact the school administrator.", nil)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,$2)`, userID, role); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE staff_invitations SET accepted_at=now() WHERE id=$1`, invitationID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,action,entity_type,entity_id,changes) VALUES($1,'staff.invitation_accepted','user',$2,jsonb_build_object('email',$3::text,'role',$4::text))`, schoolID, userID, email, role); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"email": email, "role": role}})
}

func (a *API) assignTeacher(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		TeacherID string `json:"teacher_id"`
	}
	if !decode(w, r, &req) || strings.TrimSpace(req.TeacherID) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "A teacher account is required.", nil)
		return
	}
	classID := r.PathValue("classID")
	_, err := a.db.Exec(r.Context(), `
		INSERT INTO class_teachers(school_id,class_id,teacher_id)
		SELECT $1,c.id,u.id FROM classes c
		JOIN users u ON u.id=$3 AND u.school_id=$1 AND u.status='active'
		JOIN user_roles ur ON ur.user_id=u.id AND ur.role='teacher'
		WHERE c.id=$2 AND c.school_id=$1
		ON CONFLICT (class_id,teacher_id) DO NOTHING`, id.SchoolID, classID, strings.TrimSpace(req.TeacherID))
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	var assigned bool
	if err := a.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM class_teachers WHERE school_id=$1 AND class_id=$2 AND teacher_id=$3)`, id.SchoolID, classID, strings.TrimSpace(req.TeacherID)).Scan(&assigned); err != nil {
		a.serverError(w, r, err)
		return
	}
	if !assigned {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Class or active teacher account was not found in this school.", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"assigned": true}})
}

func (a *API) listTeacherClasses(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), `
		SELECT c.id::text,c.name,c.grade_level,c.academic_year_id::text,y.name
		FROM class_teachers ct
		JOIN classes c ON c.id=ct.class_id AND c.school_id=ct.school_id
		JOIN academic_years y ON y.id=c.academic_year_id AND y.school_id=c.school_id
		WHERE ct.school_id=$1 AND ct.teacher_id=$2
		ORDER BY y.starts_on DESC,c.grade_level,c.name`, id.SchoolID, id.UserID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var classID, name, grade, yearID, yearName string
		if err := rows.Scan(&classID, &name, &grade, &yearID, &yearName); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{"id": classID, "name": name, "grade_level": grade, "academic_year_id": yearID, "academic_year_name": yearName})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (a *API) listTeacherClassStudents(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	classID := r.PathValue("classID")
	rows, err := a.db.Query(r.Context(), `
		SELECT s.id::text,s.student_number,s.legal_name,COALESCE(s.preferred_language,''),s.status
		FROM class_teachers ct
		JOIN enrollments e ON e.class_id=ct.class_id AND e.school_id=ct.school_id AND e.status='active'
		JOIN students s ON s.id=e.student_id AND s.school_id=e.school_id
		WHERE ct.school_id=$1 AND ct.teacher_id=$2 AND ct.class_id=$3 AND s.status='active'
		ORDER BY s.legal_name,s.student_number LIMIT 500`, id.SchoolID, id.UserID, classID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]string{}
	for rows.Next() {
		var studentID, number, name, language, status string
		if err := rows.Scan(&studentID, &number, &name, &language, &status); err != nil {
			a.serverError(w, r, err)
			return
		}
		items = append(items, map[string]string{"id": studentID, "student_number": number, "legal_name": name, "preferred_language": language, "status": status})
	}
	if err := rows.Err(); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}
