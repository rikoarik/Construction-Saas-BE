package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("identity record not found")

// User contains the persisted user record. PasswordHash must contain only a
// password hash, never a plaintext password.
type User struct {
	ID              string
	Email           string
	EmailNormalized string
	PasswordHash    string
	Status          string
	EmailVerifiedAt *time.Time
	SecurityVersion int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Session only accepts token hashes; the raw opaque token is not persisted here.
// ExpiresAt is the current lease expiry: short for mfa_pending sessions and the
// absolute expiry for active sessions. AbsoluteExpiresAt bounds every rotation.
type Session struct {
	ID                string
	UserID            string
	TokenHash         string
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
	AuthState         string
	MFAVerifiedAt     *time.Time
	SecurityVersion   int64
	MFAFailedAttempts int
	RevokedAt         *time.Time
	CreatedAt         time.Time
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin create user: %w", err)
	}
	defer tx.Rollback(ctx)

	var user User
	err = tx.QueryRow(ctx, `
		INSERT INTO app.users (email, email_normalized, password_hash, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, email, email_normalized, status, email_verified_at,
		          security_version, created_at, updated_at`,
		strings.TrimSpace(email), normalized, passwordHash, status).Scan(
		&user.ID, &user.Email, &user.EmailNormalized, &user.Status,
		&user.EmailVerifiedAt, &user.SecurityVersion, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	// Write credentials explicitly in the same transaction instead of relying
	// on the compatibility trigger, whose effects are not visible to the
	// statement that fired it.
	user.PasswordHash = passwordHash
	if _, err := tx.Exec(ctx, `
		INSERT INTO app.user_credentials (user_id, password_hash, updated_at)
		VALUES ($1::uuid, $2, now())
		ON CONFLICT (user_id) DO UPDATE
		SET password_hash = EXCLUDED.password_hash, updated_at = now()`,
		user.ID, passwordHash); err != nil {
		return User{}, fmt.Errorf("create user credentials: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit create user: %w", err)
	}
	return user, nil
}

func (s *Store) GetUserByNormalizedEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id::text, u.email, u.email_normalized, c.password_hash, u.status,
		       u.email_verified_at, u.security_version, u.created_at, u.updated_at
		FROM app.users u JOIN app.user_credentials c ON c.user_id = u.id
		WHERE u.email_normalized = $1`, NormalizeEmail(email)).Scan(
		&user.ID, &user.Email, &user.EmailNormalized, &user.PasswordHash, &user.Status,
		&user.EmailVerifiedAt, &user.SecurityVersion, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (Session, error) {
	return s.createSession(ctx, userID, "", tokenHash, expiresAt, expiresAt, "active", nil)
}

func (s *Store) createSession(ctx context.Context, userID, passwordHash, tokenHash string, expiresAt, absoluteExpiresAt time.Time, state string, mfaVerifiedAt *time.Time) (Session, error) {
	if userID == "" || tokenHash == "" || !expiresAt.After(time.Now()) || !absoluteExpiresAt.After(time.Now()) {
		return Session{}, errors.New("user ID, token hash, and future expiry are required")
	}
	if absoluteExpiresAt.Before(expiresAt) {
		return Session{}, errors.New("absolute session expiry cannot precede lease expiry")
	}
	if state != "active" && state != "mfa_pending" {
		return Session{}, errors.New("invalid session state")
	}
	var session Session
	err := s.pool.QueryRow(ctx, `
		INSERT INTO app.auth_sessions
			(user_id, token_hash, expires_at, absolute_expires_at, auth_state, mfa_verified_at, security_version)
		SELECT u.id, $2, $3, $4, $5, $6, u.security_version
		FROM app.users u
		WHERE u.id = $1::uuid AND u.status = 'active'
		  AND ($7 = '' OR EXISTS (
		      SELECT 1 FROM app.user_credentials c
		      WHERE c.user_id = u.id AND c.password_hash = $7
		  ))
		RETURNING id::text, user_id::text, token_hash, expires_at, absolute_expires_at,
		          auth_state, mfa_verified_at, security_version, mfa_failed_attempts,
		          revoked_at, created_at`,
		userID, tokenHash, expiresAt, absoluteExpiresAt, state, mfaVerifiedAt, passwordHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt,
		&session.AbsoluteExpiresAt, &session.AuthState, &session.MFAVerifiedAt,
		&session.SecurityVersion, &session.MFAFailedAttempts, &session.RevokedAt, &session.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (s *Store) GetActiveSessionByTokenHash(ctx context.Context, tokenHash string) (Session, error) {
	var session Session
	err := s.pool.QueryRow(ctx, `
		SELECT s.id::text, s.user_id::text, s.token_hash, s.expires_at,
		       s.absolute_expires_at, s.auth_state, s.mfa_verified_at,
		       s.security_version, s.mfa_failed_attempts, s.revoked_at, s.created_at
		FROM app.auth_sessions s
		JOIN app.users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
		  AND u.status = 'active' AND u.security_version = s.security_version`, tokenHash).Scan(
		&session.ID, &session.UserID, &session.TokenHash, &session.ExpiresAt,
		&session.AbsoluteExpiresAt, &session.AuthState, &session.MFAVerifiedAt,
		&session.SecurityVersion, &session.MFAFailedAttempts, &session.RevokedAt, &session.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get active session: %w", err)
	}
	return session, nil
}

// IsPlatformAdmin checks current platform role assignments (and the legacy
// grant table during transition) plus active account status on every request.
func (s *Store) IsPlatformAdmin(ctx context.Context, userID string) (bool, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM app.users u
		WHERE u.id = $1::uuid AND u.status = 'active'
		  AND (
		    EXISTS (
		      SELECT 1 FROM app.platform_role_assignments a
		      JOIN app.platform_role_capabilities rc ON rc.role_code = a.role_code
		      WHERE a.user_id = u.id AND a.revoked_at IS NULL
		        AND rc.capability_code = 'platform.access'
		    )
		    OR EXISTS (SELECT 1 FROM app.platform_admins pa WHERE pa.user_id = u.id)
		  )
	)`, userID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check platform admin: %w", err)
	}
	return allowed, nil
}

// PlatformCapabilities returns current effective platform capabilities. A
// platform grant never implies membership or tenant capabilities.
func (s *Store) PlatformCapabilities(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT capability_code FROM (
			SELECT rc.capability_code
			FROM app.platform_role_assignments a
			JOIN app.platform_role_capabilities rc ON rc.role_code = a.role_code
			WHERE a.user_id = $1::uuid AND a.revoked_at IS NULL
			UNION ALL
			SELECT unnest(ARRAY['platform.access', 'platform.roles.manage'])
			WHERE EXISTS (SELECT 1 FROM app.platform_admins pa WHERE pa.user_id = $1::uuid)
		) grants
		WHERE EXISTS (
			SELECT 1 FROM app.users u WHERE u.id = $1::uuid AND u.status = 'active'
		)
		ORDER BY capability_code`, userID)
	if err != nil {
		return nil, fmt.Errorf("load platform capabilities: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		result = append(result, capability)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// TenantCapabilities evaluates a live active membership against the shared
// capability mapping. It never consults platform grants.
func (s *Store) TenantCapabilities(ctx context.Context, userID, companyID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT rc.capability_code
		FROM app.company_memberships m
		JOIN app.companies c ON c.id = m.company_id
		JOIN app.users u ON u.id = m.user_id
		JOIN app.tenant_role_capabilities rc ON rc.role = m.role
		WHERE m.user_id = $1::uuid AND m.company_id = $2::uuid
		  AND m.status = 'active' AND c.status = 'active' AND u.status = 'active'
		ORDER BY rc.capability_code`, userID, companyID)
	if err != nil {
		return nil, fmt.Errorf("load tenant capabilities: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		result = append(result, capability)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// RevokeAllSessions invalidates all currently active sessions for an account.
func (s *Store) RevokeAllSessions(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE app.auth_sessions SET revoked_at = now()
		WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID)
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
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

// RecordSecurityEvent stores a sanitized, append-only authentication event.
// Callers must pass hashes rather than raw IP addresses or user agents.
func (s *Store) RecordSecurityEvent(ctx context.Context, userID *string, eventType, ipHash, userAgentHash string, metadata map[string]any) error {
	if eventType == "" {
		return errors.New("security event type is required")
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode security event metadata: %w", err)
	}
	var uid any
	if userID != nil && *userID != "" {
		uid = *userID
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app.security_events (user_id, event_type, ip_hash, user_agent_hash, metadata)
		VALUES ($1::uuid, $2, NULLIF($3, ''), NULLIF($4, ''), $5::jsonb)`,
		uid, eventType, ipHash, userAgentHash, encoded)
	if err != nil {
		return fmt.Errorf("record security event: %w", err)
	}
	return nil
}
