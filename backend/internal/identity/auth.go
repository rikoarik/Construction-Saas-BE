package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	SessionCookieName = "session"
	SessionTTL        = 12 * time.Hour
)

var ErrUnauthenticated = errors.New("authentication required")

// AuthService owns password login and the opaque-cookie session lifecycle.
type AuthService struct {
	Store      *Store
	Passwords  PasswordHasher
	Logger     *slog.Logger
	CookieName string
	SessionTTL time.Duration
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

func (s *AuthService) Login(ctx context.Context, credentials Credentials) (string, time.Time, error) {
	_, ttl, err := s.config()
	if err != nil {
		return "", time.Time{}, err
	}
	if credentials.Email == "" || credentials.Password == "" || len(credentials.Password) > maxPasswordBytes {
		return "", time.Time{}, errors.New("email and password are required")
	}
	user, err := s.Store.GetUserByNormalizedEmail(ctx, credentials.Email)
	if err != nil {
		s.logLoginFailure(ctx)
		return "", time.Time{}, ErrUnauthenticated
	}
	if user.Status != "active" {
		s.logLoginFailure(ctx)
		return "", time.Time{}, ErrUnauthenticated
	}
	valid, err := s.Passwords.Verify(user.PasswordHash, credentials.Password)
	if err != nil || !valid {
		s.logLoginFailure(ctx)
		return "", time.Time{}, ErrUnauthenticated
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	expires := time.Now().Add(ttl)
	if _, err := s.Store.CreateSession(ctx, user.ID, sessionTokenHash(token), expires); err != nil {
		return "", time.Time{}, fmt.Errorf("create login session: %w", err)
	}
	return token, expires, nil
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
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: token, Path: "/", Expires: expires,
		MaxAge: int(time.Until(expires).Seconds()), HttpOnly: true, Secure: secure,
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

func (s *AuthService) logLoginFailure(ctx context.Context) {
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "authentication failed")
}
