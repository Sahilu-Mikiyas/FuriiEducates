package httpapi

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// limitRule allows at most limit events per window.
type limitRule struct {
	limit  int
	window time.Duration
}

// rateLimits groups the API's limits. Per-address limits are generous because
// a whole school, or a computer lab sitting an exam, often shares one public
// address. Brute-force protection comes from the per-account failure limit.
type rateLimits struct {
	loginPerIP              limitRule
	loginFailuresPerAccount limitRule
	tokenRedemptionsPerIP   limitRule
	demoRequestsPerIP       limitRule
	passwordFailuresPerUser limitRule
}

func defaultRateLimits() rateLimits {
	return rateLimits{
		loginPerIP:              limitRule{limit: 120, window: time.Minute},
		loginFailuresPerAccount: limitRule{limit: 10, window: 15 * time.Minute},
		tokenRedemptionsPerIP:   limitRule{limit: 30, window: 10 * time.Minute},
		demoRequestsPerIP:       limitRule{limit: 10, window: time.Hour},
		passwordFailuresPerUser: limitRule{limit: 10, window: 15 * time.Minute},
	}
}

// rateLimiter is an in-memory fixed-window counter. It is per process, which
// is sufficient for a single API instance; a shared store is needed before
// running several instances.
type rateLimiter struct {
	mu        sync.Mutex
	windows   map[string]*limitWindow
	lastSweep time.Time
}

type limitWindow struct {
	start  time.Time
	count  int
	window time.Duration
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{windows: map[string]*limitWindow{}}
}

// hit records one event for key and reports whether it is within the rule.
func (l *rateLimiter) hit(key string, rule limitRule, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweep(now)
	current := l.current(key, rule, now)
	current.count++
	if current.count > rule.limit {
		return false, current.start.Add(rule.window).Sub(now)
	}
	return true, 0
}

// blocked reports whether key has already used up the rule, without
// recording an event.
func (l *rateLimiter) blocked(key string, rule limitRule, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.current(key, rule, now)
	if current.count >= rule.limit {
		return true, current.start.Add(rule.window).Sub(now)
	}
	return false, 0
}

func (l *rateLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, key)
}

func (l *rateLimiter) current(key string, rule limitRule, now time.Time) *limitWindow {
	current, ok := l.windows[key]
	if !ok || !now.Before(current.start.Add(rule.window)) {
		current = &limitWindow{start: now, window: rule.window}
		l.windows[key] = current
	}
	return current
}

// sweep drops expired windows at most once a minute so memory stays bounded.
func (l *rateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for key, current := range l.windows {
		if !now.Before(current.start.Add(current.window)) {
			delete(l.windows, key)
		}
	}
}

// limitByIP rejects a request once its client address exceeds the rule.
func (a *API) limitByIP(bucket string, rule limitRule, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ok, retry := a.limiter.hit(bucket+":"+a.clientIP(r), rule, time.Now()); !ok {
			writeRateLimited(w, retry)
			return
		}
		next(w, r)
	}
}

func writeRateLimited(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many attempts. Wait a few minutes and try again.", nil)
}
