package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CSRFTokenPath     = "/api/v1/auth/csrf"
	CSRFHeader        = "X-CSRF-Token"
	PreAuthCSRFCookie = "csrf_preauth"
)

type SessionResolver interface {
	ResolveSessionID(*http.Request) (string, error)
}

type sessionResolverFunc func(*http.Request) (string, error)

func (f sessionResolverFunc) ResolveSessionID(r *http.Request) (string, error) { return f(r) }

func NewCSRFSecret() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func csrfValue(secret []byte, sessionID string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func CSRFMiddleware(sessionCookie string, secret []byte, resolver SessionResolver, trustedOrigins map[string]struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == CSRFTokenPath && r.Method == http.MethodGet {
			if len(secret) < 32 {
				WriteError(w, r, &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR"})
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Add("Vary", "Cookie")
			// A live session gets a token bound to the session ID and current
			// bearer hash. Anonymous callers get a double-submit token that
			// protects pre-authentication mutations such as login.
			if sessionCookie != "" && resolver != nil {
				if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
					if sessionID, err := resolver.ResolveSessionID(r); err == nil && sessionID != "" {
						_, _ = w.Write(mustJSON(map[string]string{"csrf_token": csrfValue(secret, sessionID)}))
						return
					}
				}
			}
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				WriteError(w, r, &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR"})
				return
			}
			token := base64.RawURLEncoding.EncodeToString(raw)
			http.SetCookie(w, &http.Cookie{
				Name: PreAuthCSRFCookie, Value: token, Path: "/",
				HttpOnly: false, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode,
			})
			_, _ = w.Write(mustJSON(map[string]string{"csrf_token": token}))
			return
		}
		if isUnsafeMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/v1/") && !sameOriginRequest(r, trustedOrigins) {
			WriteError(w, r, &Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
			return
		}
		if isUnsafeMethod(r.Method) && strings.HasPrefix(r.URL.Path, "/api/v1/") {
			if len(secret) < 32 {
				WriteError(w, r, &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR"})
				return
			}
			provided := r.Header.Get(CSRFHeader)
			bound := false
			if sessionCookie != "" && resolver != nil {
				if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
					if sessionID, err := resolver.ResolveSessionID(r); err == nil && sessionID != "" {
						bound = true
						if !validCSRFToken(secret, sessionID, provided) {
							WriteError(w, r, &Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
							return
						}
					}
				}
			}
			if !bound {
				cookie, err := r.Cookie(PreAuthCSRFCookie)
				if err != nil || cookie.Value == "" || !constantTimeEqual(cookie.Value, provided) {
					WriteError(w, r, &Error{Status: http.StatusForbidden, Code: "FORBIDDEN"})
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return hmac.Equal([]byte(a), []byte(b))
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func sameOriginRequest(r *http.Request, allowed map[string]struct{}) bool {
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		return false
	}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		_, ok := allowed[origin]
		return ok
	}
	if referer := strings.TrimSpace(r.Header.Get("Referer")); referer != "" {
		parsed, err := url.Parse(referer)
		if err != nil {
			return false
		}
		_, ok := allowed[parsed.Scheme+"://"+parsed.Host]
		return ok
	}
	return false
}

func validCSRFToken(secret []byte, sessionID, provided string) bool {
	if provided == "" {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(csrfValue(secret, sessionID))
	if err != nil {
		return false
	}
	actual, err := base64.RawURLEncoding.DecodeString(provided)
	if err != nil || len(actual) != len(expected) {
		return false
	}
	return hmac.Equal(expected, actual)
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

type RateLimitConfig struct {
	Capacity int
	Window   time.Duration
	MaxKeys  int
}

type rateBucket struct {
	tokens float64
	at     time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	capacity float64
	rate     float64
	maxKeys  int
	window   time.Duration
}

func NewRateLimiter(config RateLimitConfig) (*RateLimiter, error) {
	if config.Capacity < 1 || config.Window <= 0 || config.MaxKeys < 1 {
		return nil, errors.New("rate limit capacity, window, and max keys must be positive")
	}
	return &RateLimiter{buckets: make(map[string]*rateBucket), capacity: float64(config.Capacity), rate: float64(config.Capacity) / config.Window.Seconds(), maxKeys: config.MaxKeys, window: config.Window}, nil
}

func (l *RateLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	current := l.buckets[key]
	if current == nil {
		l.evictExpired(now)
		if len(l.buckets) >= l.maxKeys {
			return false, time.Second
		}
		current = &rateBucket{tokens: l.capacity, at: now}
		l.buckets[key] = current
	}
	current.tokens += now.Sub(current.at).Seconds() * l.rate
	if current.tokens > l.capacity {
		current.tokens = l.capacity
	}
	current.at = now
	if current.tokens < 1 {
		retry := time.Duration((1 - current.tokens) / l.rate * float64(time.Second))
		if retry < time.Second {
			retry = time.Second
		}
		return false, retry
	}
	current.tokens--
	return true, 0
}

func (l *RateLimiter) evictExpired(now time.Time) {
	for key, value := range l.buckets {
		if now.Sub(value.at) > l.window*2 {
			delete(l.buckets, key)
		}
	}
}

func (l *RateLimiter) Middleware(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if l == nil {
			WriteError(w, r, &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR"})
			return
		}
		allowed, retry := l.Allow(route + "|" + peerIP(r.RemoteAddr))
		if !allowed {
			seconds := int(retry.Round(time.Second).Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			WriteError(w, r, &Error{Status: http.StatusTooManyRequests, Code: "RATE_LIMITED"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func peerIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func SecurityHeaders(https bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if https {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
