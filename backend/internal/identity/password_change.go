package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrPasswordPolicy = errors.New("new password does not meet policy")

// Serialize issuance with password changes so an old-password login cannot
// create a valid session after the password has changed.
func (s *Store) CreateSessionForPassword(ctx context.Context, userID, passwordHash, tokenHash string, expires time.Time) error {
	_, err := s.createSession(ctx, userID, passwordHash, tokenHash, expires, expires, "active", nil)
	return err
}

// CreatePendingMFASessionForPassword issues only a short-lived, non-privileged
// session. It becomes usable after a verified TOTP or recovery-code challenge.
func (s *Store) CreatePendingMFASessionForPassword(ctx context.Context, userID, passwordHash, tokenHash string, pendingExpires, absoluteExpires time.Time) (Session, error) {
	return s.createSession(ctx, userID, passwordHash, tokenHash, pendingExpires, absoluteExpires, "mfa_pending", nil)
}

func (s *AuthService) ChangePassword(ctx context.Context, session Session, current, next string) error {
	if !ValidNewPassword(next) || current == next {
		return ErrPasswordPolicy
	}
	if len(current) == 0 || len(current) > 1024 {
		return ErrUnauthenticated
	}
	// Hash before obtaining the account lock to avoid holding it during this work.
	newHash, err := s.Passwords.Hash(next)
	if err != nil {
		return err
	}
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT c.password_hash FROM app.users u
		JOIN app.user_credentials c ON c.user_id=u.id
		WHERE u.id=$1::uuid AND u.status='active' FOR UPDATE OF u`, session.UserID).Scan(&oldHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app.auth_sessions WHERE id=$1::uuid AND user_id=$2::uuid AND revoked_at IS NULL AND expires_at>now())`, session.ID, session.UserID).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return ErrUnauthenticated
	}
	valid, err := s.Passwords.Verify(oldHash, current)
	if err != nil || !valid {
		return ErrUnauthenticated
	}
	if _, err = tx.Exec(ctx, `UPDATE app.users
		SET password_hash=$2, security_version=security_version+1, updated_at=now()
		WHERE id=$1::uuid`, session.UserID, newHash); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE app.auth_sessions SET revoked_at=now() WHERE user_id=$1::uuid AND revoked_at IS NULL`, session.UserID); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE app.auth_tokens SET consumed_at=now()
		WHERE user_id=$1::uuid AND purpose='password_reset' AND consumed_at IS NULL`, session.UserID); err != nil {
		return fmt.Errorf("consume password reset tokens: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO app.security_events (user_id, event_type, metadata)
		VALUES ($1::uuid, 'auth.password_changed', '{}'::jsonb)`, session.UserID); err != nil {
		return fmt.Errorf("record password change: %w", err)
	}
	return tx.Commit(ctx)
}
