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
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *API) registerParentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/students/{studentID}/guardians/{guardianID}/invitation", a.admin(a.inviteGuardian))
	mux.HandleFunc("POST /api/v1/students/{studentID}/guardians/{guardianID}/password-reset", a.admin(a.resetGuardianPassword))
	mux.HandleFunc("DELETE /api/v1/students/{studentID}/guardians/{guardianID}/account", a.admin(a.removeGuardianAccess))
	mux.HandleFunc("POST /api/v1/guardian-invitations/accept", a.limitByIP("token-redeem", a.limits.tokenRedemptionsPerIP, a.acceptGuardianInvitation))
	mux.HandleFunc("GET /api/v1/parent/children", a.parent(a.listChildren))
	mux.HandleFunc("GET /api/v1/parent/children/{studentID}", a.parent(a.getChild))
}

func (a *API) parent(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This action requires a parent account.", "parent")
}

// inviteGuardian gives a guardian access to the parent portal. Someone who
// already has an account in the school is linked straight away, so a parent of
// two children has one account. Anyone else gets a one-time invitation link.
func (a *API) inviteGuardian(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var req struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || len(email) > 254 {
		writeError(w, 400, "VALIDATION_ERROR", "Enter a valid email address.", nil)
		return
	}
	studentID, guardianID := r.PathValue("studentID"), r.PathValue("guardianID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var linked *string
	err = tx.QueryRow(r.Context(), `SELECT user_id::text FROM guardians WHERE id=$1 AND student_id=$2 AND school_id=$3 FOR UPDATE`, guardianID, studentID, id.SchoolID).Scan(&linked)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if linked != nil {
		writeError(w, 409, "ALREADY_LINKED", "This guardian already has a parent account.", nil)
		return
	}
	var userID, status string
	var isStudent bool
	err = tx.QueryRow(r.Context(), `SELECT u.id::text,u.status,EXISTS(SELECT 1 FROM user_roles ur WHERE ur.user_id=u.id AND ur.role='student') FROM users u WHERE u.school_id=$1 AND lower(u.email)=$2`, id.SchoolID, email).Scan(&userID, &status, &isStudent)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.serverError(w, r, err)
		return
	}
	if userID != "" {
		a.linkExistingGuardian(w, r, id, tx, studentID, guardianID, userID, status, isStudent)
		return
	}
	elsewhere, err := emailInUse(r.Context(), tx, email)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if elsewhere {
		writeEmailInUse(w)
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		a.serverError(w, r, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(studentInviteTTL)
	if _, err = tx.Exec(r.Context(), `UPDATE guardian_invitations SET revoked_at=now() WHERE guardian_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, guardianID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO guardian_invitations(school_id,guardian_id,email,token_hash,created_by,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, id.SchoolID, guardianID, email, tokenHash[:], id.UserID, expires); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = auditGuardian(r.Context(), tx, id, "guardian.invited", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]any{"linked": false, "email": email, "token": token, "expires_at": expires.Format(time.RFC3339)}})
}

func (a *API) linkExistingGuardian(w http.ResponseWriter, r *http.Request, id auth.Identity, tx pgx.Tx, studentID, guardianID, userID, status string, isStudent bool) {
	if status != "active" {
		writeError(w, 409, "ACCOUNT_DISABLED", "The account with this email is disabled.", nil)
		return
	}
	// A student's account must not gain a view of other students' records.
	if isStudent {
		writeError(w, 409, "ACCOUNT_EXISTS", "A student account already uses this email.", nil)
		return
	}
	var already bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM guardians WHERE student_id=$1 AND user_id=$2)`, studentID, userID).Scan(&already); err != nil {
		a.serverError(w, r, err)
		return
	}
	if already {
		writeError(w, 409, "ALREADY_LINKED", "This account is already linked to this student through another guardian.", nil)
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,'parent') ON CONFLICT DO NOTHING`, userID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE guardians SET user_id=$1 WHERE id=$2`, userID, guardianID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE guardian_invitations SET revoked_at=now() WHERE guardian_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, guardianID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := auditGuardian(r.Context(), tx, id, "guardian.linked", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"linked": true}})
}

// acceptGuardianInvitation creates the parent's account. Every invitation waiting
// for the same email is accepted with it, so a parent invited for two children
// before signing up ends up with one account linked to both.
func (a *API) acceptGuardianInvitation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}
	if !decode(w, r, &req) {
		writeError(w, 400, "VALIDATION_ERROR", "Invalid JSON request.", nil)
		return
	}
	req.Token, req.DisplayName = strings.TrimSpace(req.Token), strings.TrimSpace(req.DisplayName)
	if len(req.Token) < 32 || len(req.Token) > 128 || req.DisplayName == "" || len(req.DisplayName) > 200 {
		writeError(w, 400, "VALIDATION_ERROR", "Invitation and display name are required.", nil)
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 1024 {
		writeError(w, 400, "VALIDATION_ERROR", "Password must contain at least 12 characters.", nil)
		return
	}
	tokenHash := sha256.Sum256([]byte(req.Token))
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	var schoolID, email string
	var expires time.Time
	err = tx.QueryRow(r.Context(), `SELECT school_id::text,email,expires_at FROM guardian_invitations WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL FOR UPDATE`, tokenHash[:]).Scan(&schoolID, &email, &expires)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !expires.After(time.Now())) {
		writeError(w, 400, "INVITATION_INVALID", "This invitation is invalid, expired, or already used.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	var userID string
	err = tx.QueryRow(r.Context(), `INSERT INTO users(school_id,email,password_hash,display_name) VALUES($1,$2,$3,$4) RETURNING id::text`, schoolID, email, passwordHash, req.DisplayName).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, 409, "ACCOUNT_EXISTS", "An account already uses this email. Ask the school to link it.", nil)
			return
		}
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO user_roles(user_id,role) VALUES($1,'parent')`, userID); err != nil {
		a.serverError(w, r, err)
		return
	}
	const waiting = `school_id=$1 AND lower(email)=lower($2) AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now()`
	if _, err = tx.Exec(r.Context(), `UPDATE guardians SET user_id=$3 WHERE school_id=$1 AND user_id IS NULL AND id IN (SELECT guardian_id FROM guardian_invitations WHERE `+waiting+`)`, schoolID, email, userID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE guardian_invitations SET accepted_at=now() WHERE `+waiting, schoolID, email); err != nil {
		a.serverError(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_logs(school_id,action,entity_type,entity_id,changes) VALUES($1,'guardian.invitation_accepted','user',$2,jsonb_build_object('email',$3::text))`, schoolID, userID, email); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": map[string]string{"email": email}})
}

func (a *API) resetGuardianPassword(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	var userID, email, status string
	err := a.db.QueryRow(r.Context(), `SELECT u.id::text,COALESCE(u.email,''),u.status FROM guardians g JOIN users u ON u.id=g.user_id WHERE g.id=$1 AND g.student_id=$2 AND g.school_id=$3`, r.PathValue("guardianID"), r.PathValue("studentID"), id.SchoolID).Scan(&userID, &email, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "This guardian has no parent account.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if status != "active" {
		writeError(w, 409, "ACCOUNT_DISABLED", "This account is disabled.", nil)
		return
	}
	a.writePasswordReset(w, r, id.SchoolID, userID, email, id.UserID)
}

// removeGuardianAccess unlinks a parent from one child, for example after a
// change in custody. It also cancels any invitation still waiting.
func (a *API) removeGuardianAccess(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID, guardianID := r.PathValue("studentID"), r.PathValue("guardianID")
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	tag, err := tx.Exec(r.Context(), `UPDATE guardians SET user_id=NULL WHERE id=$1 AND student_id=$2 AND school_id=$3`, guardianID, studentID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "NOT_FOUND", "Guardian not found.", nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE guardian_invitations SET revoked_at=now() WHERE guardian_id=$1 AND accepted_at IS NULL AND revoked_at IS NULL`, guardianID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = auditGuardian(r.Context(), tx, id, "guardian.access_removed", studentID); err != nil {
		a.serverError(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"id": guardianID}})
}

type childRow struct {
	ID        string `json:"id"`
	Number    string `json:"student_number"`
	Name      string `json:"legal_name"`
	Status    string `json:"status"`
	Stream    string `json:"stream"`
	ClassName string `json:"class_name"`
	Grade     string `json:"grade_level"`
}

// A parent sees a child only through their own guardian link.
const childSelect = `SELECT DISTINCT s.id::text,s.student_number,s.legal_name,s.status,COALESCE(s.stream,''),COALESCE(c.name,''),COALESCE(c.grade_level,'')
	FROM guardians g JOIN students s ON s.id=g.student_id
	LEFT JOIN LATERAL (SELECT e.class_id FROM enrollments e WHERE e.student_id=s.id AND e.status='active' ORDER BY e.enrolled_at DESC LIMIT 1) current ON TRUE
	LEFT JOIN classes c ON c.id=current.class_id
	WHERE g.user_id=$1 AND g.school_id=$2`

func (a *API) listChildren(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	rows, err := a.db.Query(r.Context(), childSelect+` ORDER BY 3`, id.UserID, id.SchoolID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	children, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (childRow, error) {
		var x childRow
		err := row.Scan(&x.ID, &x.Number, &x.Name, &x.Status, &x.Stream, &x.ClassName, &x.Grade)
		return x, err
	})
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	type childSummary struct {
		childRow
		Attendance attendanceSummaryData `json:"attendance"`
		LatestTerm string                `json:"latest_term"`
		Average    *float64              `json:"latest_average"`
		Rank       *int                  `json:"latest_rank"`
		Ranked     int                   `json:"students_ranked"`
	}
	items := make([]childSummary, 0, len(children))
	for _, child := range children {
		x := childSummary{childRow: child}
		if x.Attendance, err = a.attendanceSummary(r.Context(), id.SchoolID, child.ID); err != nil {
			a.serverError(w, r, err)
			return
		}
		cards, err := a.cardsForStudent(r.Context(), id.SchoolID, child.ID)
		if err != nil {
			a.serverError(w, r, err)
			return
		}
		if len(cards) > 0 {
			x.LatestTerm, x.Average, x.Rank, x.Ranked = cards[0].TermName, cards[0].Average, cards[0].Rank, cards[0].Ranked
		}
		items = append(items, x)
	}
	writeJSON(w, 200, map[string]any{"data": items})
}

type feeEntry struct {
	Type        string  `json:"entry_type"`
	Category    string  `json:"category"`
	Description string  `json:"description"`
	Amount      int64   `json:"amount_minor"`
	Currency    string  `json:"currency_code"`
	DueOn       *string `json:"due_on"`
	PaidOn      *string `json:"paid_on"`
	Recorded    string  `json:"recorded_on"`
}

type feeBalance struct {
	Currency string `json:"currency_code"`
	Charged  int64  `json:"charged_minor"`
	Paid     int64  `json:"paid_minor"`
	Due      int64  `json:"due_minor"`
}

// loadChildFees reads a student's fees. Voided entries do not count, and the sum
// is the one the school's own finance screen uses.
func (a *API) loadChildFees(ctx context.Context, schoolID, studentID string) (map[string]any, error) {
	rows, err := a.db.Query(ctx, `SELECT currency_code,COALESCE(sum(amount_minor) FILTER (WHERE entry_type='tuition_charge'),0)::bigint,COALESCE(sum(amount_minor) FILTER (WHERE entry_type='payment'),0)::bigint
		FROM finance_entries WHERE school_id=$1 AND student_id=$2 AND status<>'void' AND entry_type IN ('tuition_charge','payment') GROUP BY currency_code ORDER BY currency_code`, schoolID, studentID)
	if err != nil {
		return nil, err
	}
	balances, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (feeBalance, error) {
		var x feeBalance
		err := row.Scan(&x.Currency, &x.Charged, &x.Paid)
		x.Due = max(0, x.Charged-x.Paid)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	rows, err = a.db.Query(ctx, `SELECT entry_type,category,description,amount_minor,currency_code,due_on::text,paid_on::text,created_at::date::text
		FROM finance_entries WHERE school_id=$1 AND student_id=$2 AND status<>'void' AND entry_type IN ('tuition_charge','payment') ORDER BY created_at DESC LIMIT 50`, schoolID, studentID)
	if err != nil {
		return nil, err
	}
	entries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (feeEntry, error) {
		var x feeEntry
		err := row.Scan(&x.Type, &x.Category, &x.Description, &x.Amount, &x.Currency, &x.DueOn, &x.PaidOn, &x.Recorded)
		return x, err
	})
	if err != nil {
		return nil, err
	}
	if balances == nil {
		balances = []feeBalance{}
	}
	if entries == nil {
		entries = []feeEntry{}
	}
	return map[string]any{"balances": balances, "entries": entries}, nil
}

func (a *API) getChild(w http.ResponseWriter, r *http.Request, id auth.Identity) {
	studentID := r.PathValue("studentID")
	var child childRow
	err := a.db.QueryRow(r.Context(), childSelect+` AND s.id=$3`, id.UserID, id.SchoolID, studentID).Scan(&child.ID, &child.Number, &child.Name, &child.Status, &child.Stream, &child.ClassName, &child.Grade)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "NOT_FOUND", "Student not found.", nil)
		return
	}
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	attendance, err := a.attendanceSummary(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	cards, err := a.cardsForStudent(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	fees, err := a.loadChildFees(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	// Exam readiness applies once a student has chosen a national exam stream.
	var readiness map[string]any
	if child.Stream != "" {
		if readiness, err = a.studentReadiness(r.Context(), id.SchoolID, studentID, child.Stream); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	plan, err := a.buildLearningPlan(r.Context(), id.SchoolID, studentID)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]any{"student": child, "attendance": attendance, "report_cards": cards, "fees": fees, "readiness": readiness, "learning_plan": plan}})
}
