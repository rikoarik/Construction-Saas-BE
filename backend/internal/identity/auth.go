package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName = "session"
	SessionTTL        = 12 * time.Hour
	PendingMFATTL     = 10 * time.Minute
	DefaultMFAIssuer  = "Construction SaaS"
)

var ErrUnauthenticated = errors.New("authentication required")

type ClientInfo struct {
	IP        string
	UserAgent string
}

type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Next      string
}

const (
	LoginNextAuthenticated         = "authenticated"
	LoginNextMFARequired           = "mfa_required"
	LoginNextMFAEnrollmentRequired = "mfa_enrollment_required"
)

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "authentication rate limit exceeded" }

var dummyPassword struct {
	sync.Once
	hash string
	err  error
}

func dummyPasswordHash(hasher PasswordHasher) (string, error) {
	dummyPassword.Do(func() {
		// An unpredictable process-local password avoids an embedded credential.
		var secret [32]byte
		if _, dummyPassword.err = rand.Read(secret[:]); dummyPassword.err == nil {
			dummyPassword.hash, dummyPassword.err = hasher.Hash(base64.RawURLEncoding.EncodeToString(secret[:]))
		}
	})
	return dummyPassword.hash, dummyPassword.err
}

// AuthService owns password login and the opaque-cookie session lifecycle.
type AuthService struct {
	Store       *Store
	Passwords   PasswordHasher
	Logger      *slog.Logger
	CookieName  string
	SessionTTL  time.Duration
	PendingTTL  time.Duration
	MFAKey      []byte
	EmailKey    []byte
	MFAIssuer   string
	PublicURL   string
	MetadataKey []byte
	RateLimiter *AuthRateLimiter
}

type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *AuthService) config() (string, time.Duration, error) {
	if s.Store == nil {
		return "", 0, errors.New("identity store is required")
	}
	name := s.CookieName
	if name == "" {
		name = SessionCookieName
	}
	ttl := s.SessionTTL
	if ttl == 0 {
		ttl = SessionTTL
	}
	if ttl <= 0 || ttl > 30*24*time.Hour {
		return "", 0, errors.New("session lifetime must be positive and at most 30 days")
	}
	return name, ttl, nil
}

func (s *AuthService) pendingTTL() time.Duration {
	if s.PendingTTL > 0 && s.PendingTTL <= 15*time.Minute {
		return s.PendingTTL
	}
	return PendingMFATTL
}

func (s *AuthService) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *AuthService) secretBox() (*SecretBox, error) {
	if len(s.MFAKey) == 0 {
		return nil, ErrMFAUnavailable
	}
	return NewSecretBox(s.MFAKey)
}

// Login preserves the original service contract for callers that only handle
// fully authenticated password sessions.
func (s *AuthService) Login(ctx context.Context, credentials Credentials) (string, time.Time, error) {
	result, err := s.LoginWithClient(ctx, credentials, ClientInfo{})
	if err != nil {
		return "", time.Time{}, err
	}
	if result.Next != LoginNextAuthenticated {
		return "", time.Time{}, ErrMFARequired
	}
	return result.Token, result.ExpiresAt, nil
}

func (s *AuthService) LoginWithClient(ctx context.Context, credentials Credentials, client ClientInfo) (LoginResult, error) {
	_, ttl, err := s.config()
	if err != nil {
		return LoginResult{}, err
	}
	if credentials.Email == "" || credentials.Password == "" || len(credentials.Password) > maxPasswordBytes {
		return LoginResult{}, ErrUnauthenticated
	}
	if err := s.enforceLoginLimit(ctx, credentials.Email, client); err != nil {
		return LoginResult{}, err
	}
	user, err := s.Store.GetUserByNormalizedEmail(ctx, credentials.Email)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return LoginResult{}, fmt.Errorf("load login account: %w", err)
	}
	found := err == nil
	hash := user.PasswordHash
	if !found || user.Status != "active" {
		hash, err = dummyPasswordHash(s.Passwords)
		if err != nil {
			return LoginResult{}, fmt.Errorf("prepare password verification: %w", err)
		}
	}
	valid, err := s.Passwords.Verify(hash, credentials.Password)
	if err != nil || !valid || !found || user.Status != "active" {
		s.logLoginFailure(ctx)
		s.recordEvent(ctx, userIDOrNil(found, user.ID), "auth.login_failed", client, nil)
		return LoginResult{}, ErrUnauthenticated
	}

	platform, err := s.Store.IsPlatformAdmin(ctx, user.ID)
	if err != nil {
		return LoginResult{}, fmt.Errorf("check platform access: %w", err)
	}
	if platform {
		if len(s.MFAKey) == 0 {
			s.recordEvent(ctx, &user.ID, "auth.mfa_unavailable", client, nil)
			return LoginResult{}, ErrMFAUnavailable
		}
		enrolled, err := s.Store.HasActiveTOTPFactor(ctx, user.ID)
		if err != nil {
			return LoginResult{}, err
		}
		now := time.Now()
		pendingExpiry := now.Add(s.pendingTTL())
		absoluteExpiry := now.Add(ttl)
		if pendingExpiry.After(absoluteExpiry) {
			pendingExpiry = absoluteExpiry
		}
		token, err := randomToken(32)
		if err != nil {
			return LoginResult{}, fmt.Errorf("generate MFA session token: %w", err)
		}
		if _, err := s.Store.CreatePendingMFASessionForPassword(ctx, user.ID, user.PasswordHash, sessionTokenHash(token), pendingExpiry, absoluteExpiry); err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				return LoginResult{}, ErrUnauthenticated
			}
			return LoginResult{}, fmt.Errorf("create MFA pending session: %w", err)
		}
		next := LoginNextMFAEnrollmentRequired
		if enrolled {
			next = LoginNextMFARequired
		}
		s.recordEvent(ctx, &user.ID, "auth.mfa_challenge_started", client, map[string]any{"enrolled": enrolled})
		return LoginResult{Token: token, ExpiresAt: pendingExpiry, Next: next}, nil
	}

	token, err := randomToken(32)
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	expires := time.Now().Add(ttl)
	if err := s.Store.CreateSessionForPassword(ctx, user.ID, user.PasswordHash, sessionTokenHash(token), expires); err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			return LoginResult{}, ErrUnauthenticated
		}
		return LoginResult{}, fmt.Errorf("create login session: %w", err)
	}
	s.recordEvent(ctx, &user.ID, "auth.login_succeeded", client, nil)
	return LoginResult{Token: token, ExpiresAt: expires, Next: LoginNextAuthenticated}, nil
}

func (s *AuthService) enforceLoginLimit(ctx context.Context, email string, client ClientInfo) error {
	if s.RateLimiter == nil {
		return nil
	}
	now := time.Now()
	checks := []struct {
		scope string
		key   string
		limit int
	}{
		{"login.identity", NormalizeEmail(email), 10},
		{"login.ip", client.IP, 60},
	}
	for _, check := range checks {
		if check.key == "" {
			continue
		}
		allowed, retry, err := s.RateLimiter.Allow(ctx, check.scope, check.key, check.limit, 15*time.Minute, now)
		if err != nil {
			return fmt.Errorf("check login rate limit: %w", err)
		}
		if !allowed {
			return &RateLimitError{RetryAfter: retry}
		}
	}
	return nil
}

func (s *AuthService) Logout(ctx context.Context, userID, token string) error {
	if _, _, err := s.config(); err != nil {
		return err
	}
	if userID == "" || token == "" {
		return ErrUnauthenticated
	}
	session, err := s.Store.GetActiveSessionByTokenHash(ctx, sessionTokenHash(token))
	if err != nil || session.UserID != userID {
		return ErrUnauthenticated
	}
	if _, err := s.Store.RevokeSession(ctx, userID, session.ID); err != nil {
		return fmt.Errorf("revoke login session: %w", err)
	}
	return nil
}

func (s *AuthService) SetSessionCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	name := s.CookieName
	if name == "" {
		name = SessionCookieName
	}
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: token, Path: "/", Expires: expires,
		MaxAge: maxAge, HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *AuthService) ClearSessionCookie(w http.ResponseWriter, secure bool) {
	name := s.CookieName
	if name == "" {
		name = SessionCookieName
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (Session, error) {
	if token == "" || s.Store == nil {
		return Session{}, ErrUnauthenticated
	}
	session, err := s.Store.GetActiveSessionByTokenHash(ctx, sessionTokenHash(token))
	if err != nil {
		return Session{}, ErrUnauthenticated
	}
	return session, nil
}

func (s *AuthService) metadataHashes(client ClientInfo) (string, string) {
	hash := func(value string) string {
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		if len(s.MetadataKey) > 0 {
			mac := hmac.New(sha256.New, s.MetadataKey)
			_, _ = mac.Write([]byte(value))
			return hex.EncodeToString(mac.Sum(nil))
		}
		sum := sha256.Sum256([]byte(value))
		return hex.EncodeToString(sum[:])
	}
	return hash(client.IP), hash(client.UserAgent)
}

func (s *AuthService) recordEvent(ctx context.Context, userID *string, eventType string, client ClientInfo, metadata map[string]any) {
	ipHash, uaHash := s.metadataHashes(client)
	if err := s.Store.RecordSecurityEvent(ctx, userID, eventType, ipHash, uaHash, metadata); err != nil {
		s.logger().ErrorContext(ctx, "record security event", slog.String("error", err.Error()))
	}
}

func userIDOrNil(found bool, id string) *string {
	if !found || id == "" {
		return nil
	}
	return &id
}

func (s *AuthService) logLoginFailure(ctx context.Context) {
	s.logger().WarnContext(ctx, "authentication failed")
}
