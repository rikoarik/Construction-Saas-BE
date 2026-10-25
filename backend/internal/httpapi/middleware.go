package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

type requestIDKey struct{}

// RequestID returns the identifier assigned by RequestIDMiddleware.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// RequestIDMiddleware assigns a fresh request identifier and exposes it in the
// response header. Client-supplied IDs are not trusted or reused.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			http.Error(w, "request initialization failed", http.StatusInternalServerError)
			return
		}
		id := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

// LoggingMiddleware logs request metadata without query strings or bodies.
func LoggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		wrapped := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.InfoContext(r.Context(), "http request",
			slog.String("request_id", RequestID(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(started)),
		)
	})
}

// Error is a stable machine-readable API error.
type Error struct {
	Status int            `json:"-"`
	Code   string         `json:"code"`
	Params map[string]any `json:"params,omitempty"`
}

var ErrUnauthenticated = &Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED"}

func (e *Error) Error() string { return e.Code }

// WriteError emits the contract error envelope and request correlation ID.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status < 400 || apiErr.Status > 599 || !validErrorCode(apiErr.Code) {
		apiErr = &Error{Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR"}
	}

	w.Header().Set("Content-Type", "application/json")
	if id := RequestID(r.Context()); id != "" {
		w.Header().Set("X-Request-ID", id)
	}
	w.WriteHeader(apiErr.Status)
	_ = json.NewEncoder(w).Encode(apiErr)
}

func validErrorCode(code string) bool {
	if code == "" {
		return false
	}
	for _, r := range code {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// ErrorCode maps internal errors to stable API codes without leaking details.
func ErrorCode(err error) string {
	var apiErr *Error
	if errors.As(err, &apiErr) && validErrorCode(apiErr.Code) {
		return apiErr.Code
	}
	return "INTERNAL_ERROR"
}

// ValidateErrorCode reports whether code is a valid stable machine-readable code.
func ValidateErrorCode(code string) bool {
	return validErrorCode(code)
}
