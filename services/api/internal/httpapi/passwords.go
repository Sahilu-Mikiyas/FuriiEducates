package httpapi

import (
	"context"
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
)

const passwordResetTTL = 24 * time.Hour

func (a *API) registerPasswordRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/password", a.signedIn(a.changePassword))
	mux.HandleFunc("POST /api/v1/auth/password-reset/complete", a.limitByIP("token-redeem", a.limits.tokenRedemptionsPerIP, a.completePasswordReset))
	mux.HandleFunc("POST /api/v1/staff/{staffID}/password-reset", a.admin(a.createStaffPasswordReset))
	mux.HandleFunc("POST /api/v1/students/{studentID}/password-reset", a.admin(a.createStudentPasswordReset))
	mux.HandleFunc("POST /api/v1/platform/password-resets", a.admin(a.demoRequestAdmin(a.createOperatorPasswordReset)))
}

func validNewPassword(password string) bool {
	return len(password) >= 12 && len(password) <= 1024
}

// changePassword lets a signed-in user replace their password. Every other
// session for the account is revoked; the current one stays signed in.
func (a *API) changePassword(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if !decode(w, r, &req) || req.CurrentPassword == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter your current password and a new password.", nil)
		return
	}
	if !validNewPassword(req.NewPassword) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "The new password must contain at least 12 characters.", map[string]string{"new_password": "Use at least 12 characters."})
		return
	}
	failureKey := "password-user:" + id.UserID
	if blocked, retry := a.limiter.blocked(failureKey, a.limits.passwordFailuresPerUser, time.Now()); blocked {
		writeRateLimited(w, retry)
		return
	}
	matches, err := a.auth.CheckPassword(r.Context(), id.UserID, req.CurrentPassword)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if !matches {
		a.limiter.hit(failureKey, a.limits.passwordFailuresPerUser, time.Now())
		writeError(w, http.StatusBadRequest, "INVALID_CURRENT_PASSWORD", "Your current password is incorrect.", map[string]string{"current_password": "Incorrect password."})
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var currentDigest []byte
	if cookie, err := r.Cookie(cookieName); err == nil {
		currentDigest = a.auth.TokenDigest(cookie.Value)
	}
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(r.Context(), `UPDATE users SET password_hash=$2 WHERE id=$1`, id.UserID, hash); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL AND token_hash IS DISTINCT FROM $2`, id.UserID, currentDigest); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE password_reset_tokens SET revoked_at=now() WHERE user_id=$1 AND used_at IS NULL AND revoked_at IS NULL`, id.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id) VALUES($1,$2,'user.password_changed','user',$2)`, id.SchoolID, id.UserID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	a.limiter.reset(failureKey)
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]bool{"changed": true}})
}

// issuePasswordReset revokes the user's earlier unused links and stores a new
// one. Only the token's SHA-256 digest is kept; the token is returned once.
func issuePasswordReset(ctx context.Context, tx pgx.Tx, schoolID, userID, createdBy string) (string, time.Time, error) {
	if _, err := tx.Exec(ctx, `UPDATE password_reset_tokens SET revoked_at=now() WHERE user_id=$1 AND used_at IS NULL AND revoked_at IS NULL`, userID); err != nil {
		return "", time.Time{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(passwordResetTTL)
	if _, err := tx.Exec(ctx, `INSERT INTO password_reset_tokens(school_id,user_id,token_hash,created_by,expires_at) VALUES($1,$2,$3,$4,$5)`, schoolID, userID, digest[:], createdBy, expires); err != nil {
		return "", time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs(school_id,actor_user_id,action,entity_type,entity_id) VALUES($1,$2,'user.password_reset_issued','user',$3)`, schoolID, createdBy, userID); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (a *API) writePasswordReset(w http.ResponseWriter, r *http.Request, schoolID, userID, email, createdBy string) {
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	token, expires, err := issuePasswordReset(r.Context(), tx, schoolID, userID, createdBy)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	emailed, err := a.queueAccountEmail(r.Context(), tx, schoolID, "account.password_reset", email, "#reset="+token, expires)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"email": email, "token": token, "expires_at": expires.Format(time.RFC3339), "emailed": emailed}})
}

// createStaffPasswordReset issues a reset link for a teacher or another
// administrator in the same school.
func (a *API) createStaffPasswordReset(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	staffID := r.PathValue("staffID")
	if staffID == id.UserID {
		writeError(w, http.StatusBadRequest, "USE_CHANGE_PASSWORD", "Use Change password for your own account.", nil)
		return
	}
	var email, status string
	err := a.db.QueryRow(r.Context(), `
		SELECT COALESCE(u.email,''), u.status FROM users u
		WHERE u.id=$1 AND u.school_id=$2 AND EXISTS (
			SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role IN ('teacher','school_admin')
		)`, staffID, id.SchoolID).Scan(&email, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Staff account was not found in this school.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" {
		writeError(w, http.StatusConflict, "ACCOUNT_DISABLED", "Enable this account before issuing a password reset link.", nil)
		return
	}
	a.writePasswordReset(w, r, id.SchoolID, staffID, email, id.UserID)
}

// createStudentPasswordReset issues a reset link for a student's learner account.
func (a *API) createStudentPasswordReset(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var userID *string
	err := a.db.QueryRow(r.Context(), `SELECT user_id::text FROM students WHERE id=$1 AND school_id=$2`, r.PathValue("studentID"), id.SchoolID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Student was not found in this school.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if userID == nil {
		writeError(w, http.StatusConflict, "NO_LEARNER_ACCOUNT", "This student has not activated a learner account. Create an invitation instead.", nil)
		return
	}
	var email, status string
	if err = a.db.QueryRow(r.Context(), `SELECT COALESCE(email,''), status FROM users WHERE id=$1`, *userID).Scan(&email, &status); err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" {
		writeError(w, http.StatusConflict, "ACCOUNT_DISABLED", "This learner account is disabled.", nil)
		return
	}
	a.writePasswordReset(w, r, id.SchoolID, *userID, email, id.UserID)
}

// createOperatorPasswordReset lets a FURII platform operator recover a school
// administrator who is locked out of their own school.
func (a *API) createOperatorPasswordReset(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter the administrator's email address.", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if parsed, err := mail.ParseAddress(email); err != nil || parsed.Address != email {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter a valid email address.", nil)
		return
	}
	var userID, schoolID, status string
	err := a.db.QueryRow(r.Context(), `
		SELECT u.id::text, u.school_id::text, u.status FROM users u
		JOIN user_roles ur ON ur.user_id=u.id AND ur.role='school_admin'
		WHERE lower(u.email)=$1`, email).Scan(&userID, &schoolID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "No school administrator uses that email.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" {
		writeError(w, http.StatusConflict, "ACCOUNT_DISABLED", "This administrator account is disabled.", nil)
		return
	}
	a.writePasswordReset(w, r, schoolID, userID, email, id.UserID)
}

// completePasswordReset redeems a one-time link, sets the new password, and
// signs the account out everywhere.
func (a *API) completePasswordReset(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &req) || len(strings.TrimSpace(req.Token)) < 32 || len(req.Token) > 128 {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "This reset link is incomplete.", nil)
		return
	}
	if !validNewPassword(req.NewPassword) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "The new password must contain at least 12 characters.", map[string]string{"new_password": "Use at least 12 characters."})
		return
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(req.Token)))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var tokenID, userID, schoolID string
	var expires time.Time
	err = tx.QueryRow(r.Context(), `SELECT id::text,user_id::text,school_id::text,expires_at FROM password_reset_tokens WHERE token_hash=$1 AND used_at IS NULL AND revoked_at IS NULL FOR UPDATE`, digest[:]).Scan(&tokenID, &userID, &schoolID, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !expires.After(time.Now())) {
		writeError(w, http.StatusBadRequest, "RESET_INVALID", "This reset link is invalid, expired, or already used. Ask your school administrator for a new one.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var email, status string
	if err = tx.QueryRow(r.Context(), `SELECT COALESCE(email,''), status FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&email, &status); err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" {
		writeError(w, http.StatusBadRequest, "RESET_INVALID", "This reset link is invalid, expired, or already used. Ask your school administrator for a new one.", nil)
		return
	}
	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	statements := []struct {
		sql  string
		args []any
	}{
		{`UPDATE users SET password_hash=$2 WHERE id=$1`, []any{userID, hash}},
		{`UPDATE password_reset_tokens SET used_at=now() WHERE id=$1`, []any{tokenID}},
		{`UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, []any{userID}},
		{`INSERT INTO audit_logs(school_id,action,entity_type,entity_id) VALUES($1,'user.password_reset_completed','user',$2)`, []any{schoolID, userID}},
	}
	for _, statement := range statements {
		if _, err = tx.Exec(r.Context(), statement.sql, statement.args...); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"email": email}})
}
