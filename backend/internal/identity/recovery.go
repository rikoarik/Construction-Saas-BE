package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	PurposePasswordReset     = "password_reset"
	PurposeEmailVerification = "email_verification"

	passwordResetTTL     = 30 * time.Minute
	emailVerificationTTL = 24 * time.Hour
)

var (
	ErrAuthTokenInvalid = errors.New("authentication token is invalid or expired")
	ErrEmailUnavailable = errors.New("email delivery encryption is not configured")
)

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) emailBox() (*SecretBox, error) {
	key := s.EmailKey
	if len(key) == 0 {
		// Reuse the MFA key only when an explicit email key is absent. The AAD
		// still separates the two secret domains.
		key = s.MFAKey
	}
	if len(key) != 32 {
		return nil, ErrEmailUnavailable
	}
	return NewSecretBox(key)
}

func emailTokenAAD(template, userID string) string {
	return "auth-email-v1:" + template + ":" + userID
}

// issueEmailToken commits the token hash, the encrypted delivery secret, and
// the outbox event in one transaction. The raw token never touches the
// database and never appears in the outbox payload.
func (s *AuthService) issueEmailToken(ctx context.Context, userID, recipient, template, purpose string, ttl time.Duration, client ClientInfo) error {
	publicURL, err := s.frontendURL()
	if err != nil {
		return err
	}
	token, err := randomToken(32)
	if err != nil {
		return err
	}
	box, err := s.emailBox()
	if err != nil {
		return err
	}
	ciphertext, nonce, err := box.Encrypt([]byte(token), emailTokenAAD(template, userID))
	if err != nil {
		return err
	}
	expires := time.Now().Add(ttl)
	ipHash, _ := s.metadataHashes(client)
	_ = publicURL

	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE app.auth_tokens SET consumed_at=now()
		WHERE user_id=$1::uuid AND purpose=$2 AND consumed_at IS NULL`, userID, purpose); err != nil {
		return fmt.Errorf("revoke previous tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.auth_tokens
		(user_id, purpose, token_hash, expires_at, requested_ip_hash)
		VALUES ($1::uuid,$2,$3,$4,NULLIF($5,''))`,
		userID, purpose, tokenDigest(token), expires, ipHash); err != nil {
		return fmt.Errorf("create auth token: %w", err)
	}
	var deliveryID string
	err = tx.QueryRow(ctx, `INSERT INTO app.auth_email_deliveries
		(user_id, recipient_email, template, token_ciphertext, token_nonce, link_expires_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6) RETURNING id::text`,
		userID, strings.TrimSpace(recipient), template, ciphertext, nonce, expires).Scan(&deliveryID)
	if err != nil {
		return fmt.Errorf("create email delivery: %w", err)
	}
	payload, err := json.Marshal(map[string]string{"delivery_id": deliveryID})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.outbox_events (event_type, payload, idempotency_key)
		VALUES ('email.delivery', $1::jsonb, $2)`,
		payload, "email.delivery:"+deliveryID); err != nil {
		return fmt.Errorf("enqueue email delivery: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *AuthService) frontendURL() (string, error) {
	value := strings.TrimRight(strings.TrimSpace(s.PublicURL), "/")
	if value == "" {
		return "", errors.New("public frontend URL is not configured")
	}
	return value, nil
}

// RequestPasswordReset always reports success so account existence is not
// disclosed. Rate limits are keyed by the submitted identity and client IP.
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string, client ClientInfo) error {
	if err := s.enforceRecoveryLimit(ctx, "password_reset", email, client, 5, 10); err != nil {
		return err
	}
	user, err := s.Store.GetUserByNormalizedEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load reset account: %w", err)
	}
	if user.Status != "active" {
		return nil
	}
	if err := s.issueEmailToken(ctx, user.ID, user.Email, PurposePasswordReset, PurposePasswordReset, passwordResetTTL, client); err != nil {
		s.logger().ErrorContext(ctx, "queue password reset email", "error", err.Error())
	}
	s.recordEvent(ctx, &user.ID, "auth.password_reset_requested", client, nil)
	return nil
}

// ResetPassword consumes the token atomically, changes the credential, bumps
// the security version, and revokes every session and outstanding reset token.
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string, client ClientInfo) error {
	if !ValidNewPassword(newPassword) {
		return ErrPasswordPolicy
	}
	if len(token) < 20 || len(token) > 200 {
		return ErrAuthTokenInvalid
	}
	hash, err := s.Passwords.Hash(newPassword)
	if err != nil {
		return err
	}
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `UPDATE app.auth_tokens
		SET consumed_at=now()
		WHERE purpose='password_reset' AND token_hash=$1
		  AND consumed_at IS NULL AND expires_at>now()
		RETURNING user_id::text`, tokenDigest(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("consume reset token: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE app.users
		SET password_hash=$2, security_version=security_version+1, updated_at=now()
		WHERE id=$1::uuid AND status='active'`, userID, hash); err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE app.auth_sessions SET revoked_at=now()
		WHERE user_id=$1::uuid AND revoked_at IS NULL`, userID); err != nil {
		return fmt.Errorf("revoke sessions after reset: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE app.auth_tokens SET consumed_at=now()
		WHERE user_id=$1::uuid AND purpose='password_reset' AND consumed_at IS NULL`, userID); err != nil {
		return fmt.Errorf("consume sibling reset tokens: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,metadata)
		VALUES ($1::uuid,'auth.password_reset_completed','{}'::jsonb)`, userID); err != nil {
		return fmt.Errorf("record password reset: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *AuthService) VerifyEmail(ctx context.Context, token string, client ClientInfo) error {
	if len(token) < 20 || len(token) > 200 {
		return ErrAuthTokenInvalid
	}
	tx, err := s.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `UPDATE app.auth_tokens
		SET consumed_at=now()
		WHERE purpose='email_verification' AND token_hash=$1
		  AND consumed_at IS NULL AND expires_at>now()
		RETURNING user_id::text`, tokenDigest(token)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAuthTokenInvalid
	}
	if err != nil {
		return fmt.Errorf("consume verification token: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE app.users
		SET email_verified_at=COALESCE(email_verified_at, now()), updated_at=now()
		WHERE id=$1::uuid`, userID); err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *AuthService) ResendVerification(ctx context.Context, email string, client ClientInfo) error {
	if err := s.enforceRecoveryLimit(ctx, "email_verification", email, client, 3, 5); err != nil {
		return err
	}
	user, err := s.Store.GetUserByNormalizedEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if user.Status != "active" || user.EmailVerifiedAt != nil {
		return nil
	}
	if err := s.issueEmailToken(ctx, user.ID, user.Email, PurposeEmailVerification, PurposeEmailVerification, emailVerificationTTL, client); err != nil {
		s.logger().ErrorContext(ctx, "queue verification email", "error", err.Error())
	}
	return nil
}

func (s *AuthService) enforceRecoveryLimit(ctx context.Context, scope, email string, client ClientInfo, identityLimit, ipLimit int) error {
	if s.RateLimiter == nil {
		return nil
	}
	now := time.Now()
	for _, check := range []struct {
		name  string
		key   string
		limit int
	}{
		{scope + ".identity", NormalizeEmail(email), identityLimit},
		{scope + ".ip", client.IP, ipLimit},
	} {
		if check.key == "" {
			continue
		}
		allowed, retry, err := s.RateLimiter.Allow(ctx, check.name, check.key, check.limit, 15*time.Minute, now)
		if err != nil {
			return fmt.Errorf("check recovery rate limit: %w", err)
		}
		if !allowed {
			return &RateLimitError{RetryAfter: retry}
		}
	}
	return nil
}
