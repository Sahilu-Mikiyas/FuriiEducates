package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5"
)

type demoRequest struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Email            string    `json:"email"`
	SchoolName       string    `json:"school_name"`
	Role             string    `json:"role"`
	SchoolSize       string    `json:"school_size"`
	Timezone         string    `json:"timezone"`
	Region           string    `json:"region"`
	DefaultLanguage  string    `json:"default_language"`
	Priorities       []string  `json:"priorities"`
	PreparedSchoolID string    `json:"prepared_school_id,omitempty"`
	Message          string    `json:"message"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

var demoRequestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func (a *API) registerDemoRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/demo-requests", a.createDemoRequest)
	mux.HandleFunc("GET /api/v1/platform/demo-requests", a.admin(a.demoRequestAdmin(a.listDemoRequests)))
	mux.HandleFunc("PATCH /api/v1/platform/demo-requests/{requestID}", a.admin(a.demoRequestAdmin(a.updateDemoRequest)))
	mux.HandleFunc("POST /api/v1/platform/demo-requests/{requestID}/prepare", a.admin(a.demoRequestAdmin(a.prepareSchoolFromRequest)))
}

func (a *API) demoRequestAdmin(next func(http.ResponseWriter, *http.Request, auth.Identity)) func(http.ResponseWriter, *http.Request, auth.Identity) {
	return func(w http.ResponseWriter, r *http.Request, identity auth.Identity) {
		var email string
		if err := a.db.QueryRow(r.Context(), "SELECT email FROM users WHERE id=$1", identity.UserID).Scan(&email); err != nil {
			a.databaseError(w, r, err)
			return
		}
		for _, candidate := range strings.Split(os.Getenv("DEMO_REQUEST_ADMIN_EMAILS"), ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" && strings.EqualFold(candidate, email) {
				next(w, r, identity)
				return
			}
		}
		writeError(w, http.StatusForbidden, "FORBIDDEN", "This inbox is restricted to configured FURII product administrators.", nil)
	}
}

func (a *API) createDemoRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string   `json:"name"`
		Email           string   `json:"email"`
		SchoolName      string   `json:"school_name"`
		Role            string   `json:"role"`
		SchoolSize      string   `json:"school_size"`
		Timezone        string   `json:"timezone"`
		Region          string   `json:"region"`
		DefaultLanguage string   `json:"default_language"`
		Priorities      []string `json:"priorities"`
		Message         string   `json:"message"`
		ContactConsent  bool     `json:"contact_consent"`
		Website         string   `json:"website"`
	}
	if !decode(w, r, &req) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Enter the required details and try again.", nil)
		return
	}
	if strings.TrimSpace(req.Website) != "" {
		writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]bool{"submitted": true}})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)
	req.SchoolName = strings.TrimSpace(req.SchoolName)
	req.Role = strings.TrimSpace(req.Role)
	req.SchoolSize = strings.TrimSpace(req.SchoolSize)
	req.Timezone = strings.TrimSpace(req.Timezone)
	req.Region = strings.TrimSpace(req.Region)
	req.DefaultLanguage = strings.TrimSpace(req.DefaultLanguage)
	req.Message = strings.TrimSpace(req.Message)
	parsedEmail, emailErr := mail.ParseAddress(req.Email)
	if len(req.Name) < 2 || len(req.Name) > 120 ||
		emailErr != nil || parsedEmail.Address != req.Email || len(req.Email) > 254 ||
		len(req.SchoolName) < 2 || len(req.SchoolName) > 180 ||
		len(req.Role) < 2 || len(req.Role) > 80 ||
		!validDemoSchoolSize(req.SchoolSize) || req.Timezone == "" || len(req.Timezone) > 100 ||
		len(req.Region) < 2 || len(req.Region) > 80 || len(req.DefaultLanguage) < 2 || len(req.DefaultLanguage) > 16 ||
		!validSetupPriorities(req.Priorities) || len(req.Message) > 3000 ||
		!req.ContactConsent {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Check the required fields and consent before submitting.", nil)
		return
	}

	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())
	normalizedEmail := strings.ToLower(req.Email)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", normalizedEmail); err != nil {
		a.databaseError(w, r, err)
		return
	}
	var recent bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM public.demo_requests WHERE lower(email)=lower($1) AND created_at > now() - interval '10 minutes')", req.Email).Scan(&recent); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if recent {
		writeError(w, http.StatusTooManyRequests, "REQUEST_RECENTLY_SUBMITTED", "A request from this email was just received. Please wait before submitting again.", nil)
		return
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a valid school timezone.", nil)
		return
	}
	_, err = tx.Exec(ctx, "INSERT INTO public.demo_requests (name,email,school_name,role,school_size,timezone,region,default_language,priorities,message,contact_consent) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)",
		req.Name, req.Email, req.SchoolName, req.Role, req.SchoolSize, req.Timezone, req.Region, req.DefaultLanguage, req.Priorities, req.Message, req.ContactConsent)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]bool{"submitted": true}})
}

func validSetupPriorities(items []string) bool {
	if len(items) < 1 || len(items) > 7 {
		return false
	}
	allowed := map[string]bool{"student-records": true, "curriculum": true, "assessment": true, "learning-progress": true, "staff-coordination": true, "academic-history": true, "family-communication": true}
	seen := map[string]bool{}
	for _, item := range items {
		if !allowed[item] || seen[item] {
			return false
		}
		seen[item] = true
	}
	return true
}

func validDemoSchoolSize(size string) bool {
	switch size {
	case "1-100", "101-500", "501-1500", "1501+":
		return true
	default:
		return false
	}
}

func (a *API) listDemoRequests(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && !validDemoRequestStatus(status) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a valid request status.", nil)
		return
	}
	query := "SELECT id::text,name,email,school_name,role,school_size,message,status,created_at,updated_at,timezone,region,default_language,priorities,COALESCE(prepared_school_id::text,\x27\x27) FROM public.demo_requests"
	args := []any{}
	if status != "" {
		query += " WHERE status=$1"
		args = append(args, status)
	}
	query += " ORDER BY created_at DESC LIMIT 100"
	rows, err := a.db.Query(r.Context(), query, args...)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	defer rows.Close()
	requests := []demoRequest{}
	for rows.Next() {
		var item demoRequest
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.SchoolName, &item.Role, &item.SchoolSize, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.Timezone, &item.Region, &item.DefaultLanguage, &item.Priorities, &item.PreparedSchoolID); err != nil {
			a.databaseError(w, r, err)
			return
		}
		requests = append(requests, item)
	}
	if err := rows.Err(); err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": requests})
}

func (a *API) updateDemoRequest(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	requestID := r.PathValue("requestID")
	if !demoRequestIDPattern.MatchString(requestID) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a valid walkthrough request.", nil)
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) || !validDemoRequestStatus(req.Status) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose new, contacted, or closed as the request status.", nil)
		return
	}
	var item demoRequest
	err := a.db.QueryRow(r.Context(), "UPDATE public.demo_requests SET status=$2, updated_at=now() WHERE id=$1::uuid RETURNING id::text,name,email,school_name,role,school_size,message,status,created_at,updated_at,timezone,region,default_language,priorities,COALESCE(prepared_school_id::text,\x27\x27)",
		requestID, req.Status).Scan(&item.ID, &item.Name, &item.Email, &item.SchoolName, &item.Role, &item.SchoolSize, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.Timezone, &item.Region, &item.DefaultLanguage, &item.Priorities, &item.PreparedSchoolID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "Walkthrough request was not found.", nil)
		return
	}
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": item})
}

func (a *API) prepareSchoolFromRequest(w http.ResponseWriter, r *http.Request, _ auth.Identity) {
	requestID := r.PathValue("requestID")
	if !demoRequestIDPattern.MatchString(requestID) {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Choose a valid school request.", nil)
		return
	}
	ctx := r.Context()
	tx, err := a.db.Begin(ctx)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	defer tx.Rollback(context.Background())

	var item demoRequest
	query := "SELECT id::text,name,email,school_name,role,school_size,message,status,created_at,updated_at,timezone,region,default_language,priorities,COALESCE(prepared_school_id::text,'') FROM public.demo_requests WHERE id=$1::uuid FOR UPDATE"
	err = tx.QueryRow(ctx, query, requestID).Scan(&item.ID, &item.Name, &item.Email, &item.SchoolName, &item.Role, &item.SchoolSize, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt, &item.Timezone, &item.Region, &item.DefaultLanguage, &item.Priorities, &item.PreparedSchoolID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "School setup request was not found.", nil)
		return
	}
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if item.Status == "closed" {
		writeError(w, http.StatusConflict, "REQUEST_CLOSED", "Reopen this request before preparing a school.", nil)
		return
	}
	if item.PreparedSchoolID != "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			a.serverError(w, r, err)
			return
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		tokenHash := sha256.Sum256([]byte(token))
		expires := time.Now().UTC().Add(staffInviteTTL)
		var invitationID string
		err = tx.QueryRow(ctx, "UPDATE staff_invitations SET token_hash=$2,created_at=now(),expires_at=$3 WHERE school_id=$1 AND role='school_admin' AND accepted_at IS NULL AND revoked_at IS NULL RETURNING id::text", item.PreparedSchoolID, tokenHash[:], expires).Scan(&invitationID)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "INVITATION_NOT_PENDING", "The school is already active or has no pending administrator invitation to renew.", nil)
			return
		}
		if err != nil {
			a.databaseError(w, r, err)
			return
		}
		if _, err = tx.Exec(ctx, "UPDATE demo_requests SET status='contacted',updated_at=now() WHERE id=$1::uuid", item.ID); err != nil {
			a.databaseError(w, r, err)
			return
		}
		if err = tx.Commit(ctx); err != nil {
			a.databaseError(w, r, err)
			return
		}
		item.Status = "contacted"
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"request": item, "invitation_token": token, "expires_at": expires.Format(time.RFC3339)}})
		return
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE lower(email)=lower($1)) OR EXISTS(SELECT 1 FROM staff_invitations WHERE lower(email)=lower($1) AND role='school_admin' AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now())", item.Email).Scan(&exists); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "ACCOUNT_EXISTS", "This email already has an account or a pending school administrator invitation.", nil)
		return
	}
	var schoolID string
	err = tx.QueryRow(ctx, "INSERT INTO schools(name,timezone,default_language) VALUES($1,$2,$3) RETURNING id::text", item.SchoolName, item.Timezone, item.DefaultLanguage).Scan(&schoolID)
	if err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO school_setup_profiles(school_id,learner_count_band,region,priorities,source_request_id) VALUES($1,$2,$3,$4,$5::uuid)", schoolID, item.SchoolSize, item.Region, item.Priorities, item.ID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		a.serverError(w, r, err)
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))
	expires := time.Now().UTC().Add(staffInviteTTL)
	if _, err = tx.Exec(ctx, "INSERT INTO staff_invitations(school_id,email,role,token_hash,created_by,expires_at) VALUES($1,$2,'school_admin',$3,NULL,$4)", schoolID, item.Email, tokenHash[:], expires); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(ctx, "UPDATE demo_requests SET prepared_school_id=$2,status='contacted',updated_at=now() WHERE id=$1::uuid", item.ID, schoolID); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(school_id,action,entity_type,entity_id,changes) VALUES($1,'school.onboarding_prepared','school',$1,jsonb_build_object('request_id',$2::text,'learner_count_band',$3::text,'priorities',$4))", schoolID, item.ID, item.SchoolSize, item.Priorities); err != nil {
		a.databaseError(w, r, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		a.databaseError(w, r, err)
		return
	}
	item.PreparedSchoolID = schoolID
	item.Status = "contacted"
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]any{"request": item, "invitation_token": token, "expires_at": expires.Format(time.RFC3339)}})
}

func validDemoRequestStatus(status string) bool {
	switch status {
	case "new", "contacted", "closed":
		return true
	default:
		return false
	}
}
