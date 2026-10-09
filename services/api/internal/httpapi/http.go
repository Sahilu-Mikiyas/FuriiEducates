package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cookieName = "furii_session"

type API struct {
	auth   *auth.Service
	db     *pgxpool.Pool
	secure bool
	ttl    time.Duration
	log    *slog.Logger
}

func New(a *auth.Service, db *pgxpool.Pool, secure bool, ttl time.Duration, log *slog.Logger) http.Handler {
	x := &API{auth: a, db: db, secure: secure, ttl: ttl, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", x.health)
	mux.HandleFunc("POST /api/v1/auth/login", x.login)
	mux.HandleFunc("POST /api/v1/auth/logout", x.logout)
	mux.HandleFunc("GET /api/v1/me", x.me)
	x.registerSchoolRoutes(mux)
	return mux
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Ping(r.Context()); err != nil {
		writeError(w, 503, "SERVICE_UNAVAILABLE", "Service temporarily unavailable.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "ok"}})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) || strings.TrimSpace(req.Email) == "" || req.Password == "" {
		writeError(w, 400, "VALIDATION_ERROR", "Email and password are required.", nil)
		return
	}
	token, identity, err := a.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if err == auth.ErrUnauthenticated {
			writeError(w, 401, "UNAUTHENTICATED", "Email or password is incorrect.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	seconds := int(a.ttl.Seconds())
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(a.ttl), MaxAge: seconds})
	writeJSON(w, 200, map[string]any{"data": identity})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := a.auth.Logout(r.Context(), c.Value); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	writeJSON(w, 200, map[string]any{"data": map[string]bool{"logged_out": true}})
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
		return
	}
	identity, err := a.auth.Identity(r.Context(), c.Value)
	if err != nil {
		if err == auth.ErrUnauthenticated {
			writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	writeJSON(w, 200, map[string]any{"data": identity})
}

func (a *API) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.log.Error("request failed", "path", r.URL.Path, "error", err)
	writeError(w, 500, "INTERNAL_ERROR", "The request could not be completed.", nil)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return false
	}
	var trailing any
	return dec.Decode(&trailing) == io.EOF
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "fields": fields}})
}
