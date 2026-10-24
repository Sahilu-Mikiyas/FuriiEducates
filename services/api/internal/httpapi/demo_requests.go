package httpapi

import (
	"context"
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
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
	SchoolName string    `json:"school_name"`
	Role       string    `json:"role"`
	SchoolSize string    `json:"school_size"`
	Message    string    `json:"message"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

var demoRequestIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func (a *API) registerDemoRequestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/demo-requests", a.createDemoRequest)
	mux.HandleFunc("GET /api/v1/platform/demo-requests", a.admin(a.demoRequestAdmin(a.listDemoRequests)))
	mux.HandleFunc("PATCH /api/v1/platform/demo-requests/{requestID}", a.admin(a.demoRequestAdmin(a.updateDemoRequest)))
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
		Name           string `json:"name"`
		Email          string `json:"email"`
		SchoolName     string `json:"school_name"`
		Role           string `json:"role"`
		SchoolSize     string `json:"school_size"`
		Message        string `json:"message"`
		ContactConsent bool   `json:"contact_consent"`
		Website        string `json:"website"`
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
	req.Message = strings.TrimSpace(req.Message)
	parsedEmail, emailErr := mail.ParseAddress(req.Email)
	if len(req.Name) < 2 || len(req.Name) > 120 ||
		emailErr != nil || parsedEmail.Address != req.Email || len(req.Email) > 254 ||
		len(req.SchoolName) < 2 || len(req.SchoolName) > 180 ||
		len(req.Role) < 2 || len(req.Role) > 80 ||
		!validDemoSchoolSize(req.SchoolSize) || len(req.Message) > 3000 ||
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
	_, err = tx.Exec(ctx, "INSERT INTO public.demo_requests (name, email, school_name, role, school_size, message, contact_consent) VALUES ($1,$2,$3,$4,$5,$6,$7)",
		req.Name, req.Email, req.SchoolName, req.Role, req.SchoolSize, req.Message, req.ContactConsent)
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
	query := "SELECT id::text, name, email, school_name, role, school_size, message, status, created_at, updated_at FROM public.demo_requests"
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
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.SchoolName, &item.Role, &item.SchoolSize, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
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
	err := a.db.QueryRow(r.Context(), "UPDATE public.demo_requests SET status=$2, updated_at=now() WHERE id=$1::uuid RETURNING id::text, name, email, school_name, role, school_size, message, status, created_at, updated_at",
		requestID, req.Status).Scan(&item.ID, &item.Name, &item.Email, &item.SchoolName, &item.Role, &item.SchoolSize, &item.Message, &item.Status, &item.CreatedAt, &item.UpdatedAt)
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

func validDemoRequestStatus(status string) bool {
	switch status {
	case "new", "contacted", "closed":
		return true
	default:
		return false
	}
}
