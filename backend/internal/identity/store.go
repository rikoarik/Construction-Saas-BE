package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("identity record not found")

// User contains the persisted user record; PasswordHash must contain only a
// password hash, never a plaintext password.
type User struct {
	ID              string
	Email           string
	EmailNormalized string
	PasswordHash    string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Session only accepts token hashes; the raw opaque token is not persisted here.
type Session struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &Store{pool: pool}, nil
}

// NormalizeEmail trims surrounding whitespace and lowercases for uniqueness.
// It does not perform provider-specific alias rewriting.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, status string) (User, error) {
	normalized := NormalizeEmail(email)
	if normalized == "" || passwordHash == "" || status == "" {
		return User{}, errors.New("email, password hash, and status are required")
	}
	var user User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO app.users (email, email_normalized, password_hash, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, email, email_normalized, password_hash, status, created_at, updated_at`,
		strings.TrimSpace(email), normalized, passwordHash, status).Scan(
		&user.ID, &user.Email, &user.EmailNormalized, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *Store) GetUserByNormalizedEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, email, email_normalized, password_hash, status, created_at, updated_at
		FROM app.users WHERE email_normalized = $1`, NormalizeEmail(email)).Scan(
		&user.ID, &user.Email, &user.EmailNormalized, &user.PasswordHash, &user.Status, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (Session, error) {
	if userID == "" || tokenHash == "" || !expiresAt.After(time.Now()) {
		return Session{}, errors.New("user ID, token hash, and future expiry are required")
	}
	var session Session
	err := s.pool.QueryRow(ctx, `
		INSERT INTO app.auth_sessions (user_id, token_hash, expires_at)
		VALUES ($1::uuid, $2, $3)
		RETURNING id::text, user_id::text, token_hash, expires_at, revoked_at, created_at`,
		userID, tokenHash, expiresAt).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.RevokedAt, &session.CreatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (s *Store) GetActiveSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, token_hash, expires_at, revoked_at, created_at
		FROM app.auth_sessions
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt, &session.RevokedAt, &session.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get active session: %w", err)
	}
	return session, nil
}

func (s *Store) RevokeSession(ctx context.Context, userID, sessionID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE app.auth_sessions SET revoked_at = now()
		WHERE id = $1::uuid AND user_id = $2::uuid AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}
	return result.RowsAffected() == 1, nil
}
