package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/furii/school-os/services/api/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cookieName = "furii_session"

type API struct {
	auth           *auth.Service
	db             *pgxpool.Pool
	secure         bool
	sameSite       http.SameSite
	ttl            time.Duration
	log            *slog.Logger
	allowedOrigins []string
	trustProxy     bool
	limits         rateLimits
	limiter        *rateLimiter
}

// New builds the HTTP handler. Environment settings:
//   - CORS_ALLOWED_ORIGINS: exact comma-separated browser origins allowed to
//     call the API from another host (the Vercel origin, even behind its
//     /api proxy, because the proxy forwards the browser's Origin header).
//   - COOKIE_SAMESITE: lax, strict, or none. Defaults to none when cookies are
//     Secure (required while the frontend calls the API cross-site) and lax
//     otherwise. Use lax once browsers reach the API through the same-origin
//     /api proxy.
//   - TRUST_PROXY_HEADERS: true when a reverse proxy (Render, Vercel) sets
//     X-Forwarded-For, so rate limits use the real client address.
func New(a *auth.Service, db *pgxpool.Pool, secure bool, ttl time.Duration, log *slog.Logger) http.Handler {
	return newAPI(a, db, secure, ttl, log, defaultRateLimits()).routes()
}

func newAPI(a *auth.Service, db *pgxpool.Pool, secure bool, ttl time.Duration, log *slog.Logger, limits rateLimits) *API {
	var allowedOrigins []string
	for _, origin := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowedOrigins = append(allowedOrigins, origin)
		}
	}
	sameSite, err := parseSameSite(os.Getenv("COOKIE_SAMESITE"), secure)
	if err != nil {
		log.Warn("ignoring invalid COOKIE_SAMESITE", "error", err)
	}
	trustProxy, _ := strconv.ParseBool(os.Getenv("TRUST_PROXY_HEADERS"))
	return &API{auth: a, db: db, secure: secure, sameSite: sameSite, ttl: ttl, log: log, allowedOrigins: allowedOrigins, trustProxy: trustProxy, limits: limits, limiter: newRateLimiter()}
}

func (a *API) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("GET /api/v1/me", a.me)
	a.registerSchoolRoutes(mux)
	a.registerPrepRoutes(mux)
	a.registerStudentRecordRoutes(mux)
	a.registerAttendanceRoutes(mux)
	a.registerMarkRoutes(mux)
	a.registerReportRoutes(mux)
	a.registerParentRoutes(mux)
	return a.cors(mux)
}

// parseSameSite turns COOKIE_SAMESITE into a cookie mode. SameSite=None is
// only valid on Secure cookies, so it falls back to Lax without them.
func parseSameSite(raw string, secure bool) (http.SameSite, error) {
	fallback := http.SameSiteLaxMode
	if secure {
		fallback = http.SameSiteNoneMode
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return fallback, nil
	case "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		if !secure {
			return http.SameSiteLaxMode, errors.New("COOKIE_SAMESITE=none requires COOKIE_SECURE=true")
		}
		return http.SameSiteNoneMode, nil
	default:
		return fallback, errors.New("COOKIE_SAMESITE must be lax, strict, or none")
	}
}

// originAllowed reports whether a browser Origin may call the API: either the
// API's own host or one of the configured origins.
func (a *API) originAllowed(origin string, r *http.Request) bool {
	for _, candidate := range a.allowedOrigins {
		if origin == candidate {
			return true
		}
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

// cors applies the CORS policy and the cross-site request check. Browsers
// attach an Origin header to every cross-origin request and to same-origin
// unsafe requests, so rejecting unknown origins stops cross-site request
// forgery. Fetch metadata catches browsers that send a cross-site unsafe
// request without an Origin header. Requests from non-browser clients carry
// neither header and pass through to normal authentication.
func (a *API) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			if isUnsafeMethod(r.Method) && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "Cross-site requests are not allowed.", nil)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !a.originAllowed(origin, r) {
			writeError(w, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "This web origin is not allowed to access the school API.", nil)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// clientIP returns the address used for rate limiting. Behind a trusted
// proxy it is the first X-Forwarded-For entry, which the edge proxy sets
// to the connecting client.
func (a *API) clientIP(r *http.Request) string {
	if a.trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *API) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: a.sameSite, Expires: time.Now().Add(a.ttl), MaxAge: int(a.ttl.Seconds())}
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Ping(r.Context()); err != nil {
		writeError(w, 503, "SERVICE_UNAVAILABLE", "Service temporarily unavailable.", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"data": map[string]string{"status": "ok"}})
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if ok, retry := a.limiter.hit("login-ip:"+a.clientIP(r), a.limits.loginPerIP, time.Now()); !ok {
		writeRateLimited(w, retry)
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &req) || strings.TrimSpace(req.Email) == "" || req.Password == "" {
		writeError(w, 400, "VALIDATION_ERROR", "Email and password are required.", nil)
		return
	}
	accountKey := "login-account:" + strings.ToLower(strings.TrimSpace(req.Email))
	if blocked, retry := a.limiter.blocked(accountKey, a.limits.loginFailuresPerAccount, time.Now()); blocked {
		writeRateLimited(w, retry)
		return
	}
	token, identity, err := a.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthenticated) {
			a.limiter.hit(accountKey, a.limits.loginFailuresPerAccount, time.Now())
			writeError(w, 401, "UNAUTHENTICATED", "Email or password is incorrect.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	a.limiter.reset(accountKey)
	body, err := a.identityResponse(r.Context(), identity)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	http.SetCookie(w, a.sessionCookie(token))
	writeJSON(w, 200, map[string]any{"data": body})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := a.auth.Logout(r.Context(), c.Value); err != nil {
			a.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: a.secure, SameSite: a.sameSite, MaxAge: -1})
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
		if errors.Is(err, auth.ErrUnauthenticated) {
			writeError(w, 401, "UNAUTHENTICATED", "Authentication is required.", nil)
		} else {
			a.serverError(w, r, err)
		}
		return
	}
	body, err := a.identityResponse(r.Context(), identity)
	if err != nil {
		a.serverError(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": body})
}

// identityResponse is the signed-in user as returned by login and /me,
// including whether they may use platform operator pages and the school's
// default language, which the web app starts in.
func (a *API) identityResponse(ctx context.Context, identity auth.Identity) (map[string]any, error) {
	operator, err := a.isPlatformOperator(ctx, identity)
	if err != nil {
		return nil, err
	}
	var language string
	if err := a.db.QueryRow(ctx, `SELECT default_language FROM schools WHERE id=$1`, identity.SchoolID).Scan(&language); err != nil {
		return nil, err
	}
	return map[string]any{"user_id": identity.UserID, "school_id": identity.SchoolID, "display_name": identity.DisplayName, "roles": identity.Roles, "platform_operator": operator, "default_language": language}, nil
}

// isPlatformOperator reports whether a school administrator's email is listed
// in DEMO_REQUEST_ADMIN_EMAILS, which grants the FURII operator pages.
func (a *API) isPlatformOperator(ctx context.Context, identity auth.Identity) (bool, error) {
	if !hasRole(identity.Roles, "school_admin") {
		return false, nil
	}
	allowed := strings.Split(os.Getenv("DEMO_REQUEST_ADMIN_EMAILS"), ",")
	configured := false
	for _, candidate := range allowed {
		if strings.TrimSpace(candidate) != "" {
			configured = true
		}
	}
	if !configured {
		return false, nil
	}
	var email string
	if err := a.db.QueryRow(ctx, "SELECT COALESCE(email,'') FROM users WHERE id=$1", identity.UserID).Scan(&email); err != nil {
		return false, err
	}
	for _, candidate := range allowed {
		if candidate = strings.TrimSpace(candidate); candidate != "" && strings.EqualFold(candidate, email) {
			return true, nil
		}
	}
	return false, nil
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

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// requireRole resolves the session cookie into an identity, checks that the
// identity holds at least one of the allowed roles, and rejects malformed
// route identifiers before the handler queries the database.
func (a *API) requireRole(next func(http.ResponseWriter, *http.Request, auth.Identity), forbiddenMessage string, allowed ...string) http.HandlerFunc {
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
		permitted := false
		for _, role := range allowed {
			if hasRole(identity.Roles, role) {
				permitted = true
				break
			}
		}
		if !permitted {
			writeError(w, http.StatusForbidden, "FORBIDDEN", forbiddenMessage, nil)
			return
		}
		if !pathIDsValid(r) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "The requested record was not found.", nil)
			return
		}
		next(w, r, identity)
	}
}

// signedIn allows any authenticated account, whatever its role.
func (a *API) signedIn(next func(http.ResponseWriter, *http.Request, auth.Identity)) http.HandlerFunc {
	return a.requireRole(next, "This account has no active role.", "school_admin", "teacher", "student", "parent")
}

// pathIDsValid reports whether every wildcard in the matched route pattern is
// a UUID. All route identifiers in this API are UUIDs.
func pathIDsValid(r *http.Request) bool {
	for _, segment := range strings.Split(r.Pattern, "/") {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}"), "...")
		if !uuidPattern.MatchString(r.PathValue(name)) {
			return false
		}
	}
	return true
}
