package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type TOTPFactor struct {
	ID                  string
	UserID              string
	SecretCiphertext    []byte
	SecretNonce         []byte
	Status              string
	EnrollmentExpiresAt *time.Time
	LastUsedStep        *int64
	FailedAttempts      int
	LockedUntil         *time.Time
}

func scanTOTPFactor(row pgx.Row) (TOTPFactor, error) {
	var factor TOTPFactor
	err := row.Scan(&factor.ID, &factor.UserID, &factor.SecretCiphertext, &factor.SecretNonce,
		&factor.Status, &factor.EnrollmentExpiresAt, &factor.LastUsedStep,
		&factor.FailedAttempts, &factor.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return TOTPFactor{}, ErrNotFound
	}
	if err != nil {
		return TOTPFactor{}, fmt.Errorf("load TOTP factor: %w", err)
	}
	return factor, nil
}

func (s *Store) HasActiveTOTPFactor(ctx context.Context, userID string) (bool, error) {
	var active bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM app.mfa_totp_factors f
		JOIN app.users u ON u.id = f.user_id
		WHERE f.user_id = $1::uuid AND f.status = 'active' AND u.status = 'active'
	)`, userID).Scan(&active)
	if err != nil {
		return false, fmt.Errorf("check active TOTP factor: %w", err)
	}
	return active, nil
}

func (s *Store) PendingTOTPFactor(ctx context.Context, userID string) (TOTPFactor, error) {
	return scanTOTPFactor(s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, secret_ciphertext, secret_nonce, status,
		       enrollment_expires_at, last_used_step, failed_attempts, locked_until
		FROM app.mfa_totp_factors
		WHERE user_id = $1::uuid AND status = 'pending'`, userID))
}

func (s *Store) ActiveTOTPFactor(ctx context.Context, userID string) (TOTPFactor, error) {
	return scanTOTPFactor(s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, secret_ciphertext, secret_nonce, status,
		       enrollment_expires_at, last_used_step, failed_attempts, locked_until
		FROM app.mfa_totp_factors
		WHERE user_id = $1::uuid AND status = 'active'`, userID))
}

func (s *Store) CreatePendingTOTPFactor(ctx context.Context, userID string, ciphertext, nonce []byte, expiresAt time.Time) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE app.mfa_totp_factors
		SET status='revoked', updated_at=now()
		WHERE user_id=$1::uuid AND status='pending'`, userID); err != nil {
		return "", fmt.Errorf("revoke stale TOTP enrollment: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO app.mfa_totp_factors
			(user_id, secret_ciphertext, secret_nonce, status, enrollment_expires_at)
		SELECT u.id, $2, $3, 'pending', $4
		FROM app.users u
		WHERE u.id = $1::uuid AND u.status = 'active'
		  AND NOT EXISTS (
		      SELECT 1 FROM app.mfa_totp_factors f
		      WHERE f.user_id = u.id AND f.status = 'active'
		  )
		RETURNING id::text`, userID, ciphertext, nonce, expiresAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMFAAlreadyEnrolled
	}
	if err != nil {
		return "", fmt.Errorf("create TOTP enrollment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) RecordTOTPFailure(ctx context.Context, factorID, userID string) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE app.mfa_totp_factors
		SET failed_attempts = failed_attempts + 1,
		    locked_until = CASE WHEN failed_attempts + 1 >= 5 THEN now() + interval '5 minutes' ELSE locked_until END,
		    updated_at = now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status IN ('pending','active')
		RETURNING failed_attempts`, factorID, userID).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("record TOTP failure: %w", err)
	}
	return attempts, nil
}

func (s *Store) RecordSessionMFAFailure(ctx context.Context, session Session) (int, error) {
	var attempts int
	err := s.pool.QueryRow(ctx, `UPDATE app.auth_sessions
		SET mfa_failed_attempts = mfa_failed_attempts + 1,
		    revoked_at = CASE WHEN mfa_failed_attempts + 1 >= 5 THEN now() ELSE revoked_at END,
		    last_seen_at = now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND token_hash=$3
		  AND auth_state='mfa_pending' AND revoked_at IS NULL
		RETURNING mfa_failed_attempts`, session.ID, session.UserID, session.TokenHash).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnauthenticated
	}
	if err != nil {
		return 0, fmt.Errorf("record MFA session failure: %w", err)
	}
	return attempts, nil
}

func (s *Store) lockPendingMFASession(ctx context.Context, tx pgx.Tx, session Session) (int64, error) {
	var securityVersion int64
	err := tx.QueryRow(ctx, `SELECT u.security_version
		FROM app.auth_sessions s JOIN app.users u ON u.id=s.user_id
		WHERE s.id=$1::uuid AND s.user_id=$2::uuid AND s.token_hash=$3
		  AND s.auth_state='mfa_pending' AND s.revoked_at IS NULL AND s.expires_at>now()
		  AND u.status='active'
		FOR UPDATE OF s, u`, session.ID, session.UserID, session.TokenHash).Scan(&securityVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUnauthenticated
	}
	if err != nil {
		return 0, fmt.Errorf("lock MFA session: %w", err)
	}
	if securityVersion != session.SecurityVersion {
		return 0, ErrUnauthenticated
	}
	if allowed, err := platformGrantTx(ctx, tx, session.UserID); err != nil {
		return 0, err
	} else if !allowed {
		return 0, ErrForbidden
	}
	return securityVersion, nil
}

func platformGrantTx(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM app.users u WHERE u.id=$1::uuid AND u.status='active'
		AND (
		  EXISTS (
		    SELECT 1 FROM app.platform_role_assignments a
		    JOIN app.platform_role_capabilities rc ON rc.role_code=a.role_code
		    WHERE a.user_id=u.id AND a.revoked_at IS NULL AND rc.capability_code='platform.access'
		  )
		  OR EXISTS (SELECT 1 FROM app.platform_admins pa WHERE pa.user_id=u.id)
		)
	)`, userID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("check platform grant: %w", err)
	}
	return allowed, nil
}

func (s *Store) activateMFASessionTx(ctx context.Context, tx pgx.Tx, session Session, tokenHash string) (time.Time, error) {
	var expiry time.Time
	err := tx.QueryRow(ctx, `UPDATE app.auth_sessions
		SET auth_state='active', mfa_verified_at=now(), expires_at=absolute_expires_at,
		    token_hash=$4, mfa_failed_attempts=0, last_seen_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND token_hash=$3
		  AND auth_state='mfa_pending' AND revoked_at IS NULL AND expires_at>now()
		RETURNING expires_at`, session.ID, session.UserID, session.TokenHash, tokenHash).Scan(&expiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrUnauthenticated
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("activate MFA session: %w", err)
	}
	return expiry, nil
}

func (s *Store) CompleteMFAEnrollment(ctx context.Context, session Session, factorID string, step int64, tokenHash string, recoveryHashes []string, ipHash, userAgentHash string) (time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := s.lockPendingMFASession(ctx, tx, session); err != nil {
		return time.Time{}, err
	}
	var factorUser string
	err = tx.QueryRow(ctx, `UPDATE app.mfa_totp_factors
		SET status='active', activated_at=now(), last_used_step=$3, failed_attempts=0,
		    locked_until=NULL, enrollment_expires_at=NULL, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status='pending'
		  AND enrollment_expires_at>now()
		RETURNING user_id::text`, factorID, session.UserID, step).Scan(&factorUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrMFAExpired
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("activate TOTP factor: %w", err)
	}
	for _, hash := range recoveryHashes {
		if _, err := tx.Exec(ctx, `INSERT INTO app.mfa_recovery_codes (user_id, factor_id, code_hash)
			VALUES ($1::uuid, $2::uuid, $3)`, session.UserID, factorID, hash); err != nil {
			return time.Time{}, fmt.Errorf("store recovery code: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE app.mfa_totp_factors SET status='revoked', updated_at=now()
		WHERE user_id=$1::uuid AND status='pending' AND id<>$2::uuid`, session.UserID, factorID); err != nil {
		return time.Time{}, fmt.Errorf("revoke stale enrollment: %w", err)
	}
	expiry, err := s.activateMFASessionTx(ctx, tx, session, tokenHash)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,ip_hash,user_agent_hash,metadata)
		VALUES ($1::uuid,'auth.mfa_enrolled',NULLIF($2,''),NULLIF($3,''),jsonb_build_object('factor_id',$4::text))`,
		session.UserID, ipHash, userAgentHash, factorID); err != nil {
		return time.Time{}, fmt.Errorf("record MFA enrollment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expiry, nil
}

func (s *Store) CompleteMFAChallenge(ctx context.Context, session Session, factorID string, step int64, tokenHash string, ipHash, userAgentHash string) (time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := s.lockPendingMFASession(ctx, tx, session); err != nil {
		return time.Time{}, err
	}
	var factorUser string
	err = tx.QueryRow(ctx, `UPDATE app.mfa_totp_factors
		SET last_used_step=$3, failed_attempts=0, locked_until=NULL, updated_at=now()
		WHERE id=$1::uuid AND user_id=$2::uuid AND status='active'
		  AND (last_used_step IS NULL OR last_used_step < $3)
		  AND (locked_until IS NULL OR locked_until <= now())
		RETURNING user_id::text`, factorID, session.UserID, step).Scan(&factorUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrMFAReplay
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("consume TOTP step: %w", err)
	}
	expiry, err := s.activateMFASessionTx(ctx, tx, session, tokenHash)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,ip_hash,user_agent_hash,metadata)
		VALUES ($1::uuid,'auth.mfa_verified',NULLIF($2,''),NULLIF($3,''),'{}'::jsonb)`,
		session.UserID, ipHash, userAgentHash); err != nil {
		return time.Time{}, fmt.Errorf("record MFA verification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expiry, nil
}

func (s *Store) CompleteMFARecovery(ctx context.Context, session Session, codeHash, tokenHash, ipHash, userAgentHash string) (time.Time, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return time.Time{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := s.lockPendingMFASession(ctx, tx, session); err != nil {
		return time.Time{}, err
	}
	var factorID string
	err = tx.QueryRow(ctx, `UPDATE app.mfa_recovery_codes rc
		SET used_at=now()
		WHERE rc.user_id=$1::uuid AND rc.code_hash=$2 AND rc.used_at IS NULL
		  AND EXISTS (
		    SELECT 1 FROM app.mfa_totp_factors f
		    WHERE f.id=rc.factor_id AND f.user_id=rc.user_id AND f.status='active'
		  )
		RETURNING rc.factor_id::text`, session.UserID, codeHash).Scan(&factorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrMFAInvalidCode
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("consume recovery code: %w", err)
	}
	expiry, err := s.activateMFASessionTx(ctx, tx, session, tokenHash)
	if err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app.security_events (user_id,event_type,ip_hash,user_agent_hash,metadata)
		VALUES ($1::uuid,'auth.mfa_recovery_used',NULLIF($2,''),NULLIF($3,''),jsonb_build_object('factor_id',$4::text))`,
		session.UserID, ipHash, userAgentHash, factorID); err != nil {
		return time.Time{}, fmt.Errorf("record recovery use: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, err
	}
	return expiry, nil
}
