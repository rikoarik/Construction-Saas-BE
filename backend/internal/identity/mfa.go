package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrMFARequired        = errors.New("MFA is required")
	ErrMFAInvalidCode     = errors.New("invalid MFA code")
	ErrMFAAlreadyEnrolled = errors.New("MFA is already enrolled")
	ErrMFANotEnrolled     = errors.New("MFA is not enrolled")
	ErrMFAExpired         = errors.New("MFA enrollment or challenge expired")
	ErrMFAReplay          = errors.New("MFA code was already used")
	ErrMFALocked          = errors.New("MFA verification is temporarily locked")
	ErrForbidden          = errors.New("forbidden")
)

const (
	mfaRecoveryCodeCount = 10
	mfaMaxFailures       = 5
)

type MFAEnrollment struct {
	Secret     string    `json:"secret"`
	OTPAuthURL string    `json:"otpauth_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type MFAResult struct {
	Token         string    `json:"-"`
	ExpiresAt     time.Time `json:"expires_at"`
	RecoveryCodes []string  `json:"recovery_codes,omitempty"`
}

func (s *AuthService) requirePendingPlatformSession(ctx context.Context, session Session) error {
	if session.AuthState != "mfa_pending" {
		return ErrMFARequired
	}
	if session.ExpiresAt.Before(time.Now()) || session.RevokedAt != nil {
		return ErrMFAExpired
	}
	allowed, err := s.Store.IsPlatformAdmin(ctx, session.UserID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

// StartMFAEnrollment creates a non-active TOTP enrollment. The secret is only
// returned once and is encrypted at rest with the configured AES-GCM key.
func (s *AuthService) StartMFAEnrollment(ctx context.Context, session Session, client ClientInfo) (MFAEnrollment, error) {
	if err := s.requirePendingPlatformSession(ctx, session); err != nil {
		return MFAEnrollment{}, err
	}
	box, err := s.secretBox()
	if err != nil {
		return MFAEnrollment{}, err
	}
	active, err := s.Store.HasActiveTOTPFactor(ctx, session.UserID)
	if err != nil {
		return MFAEnrollment{}, err
	}
	if active {
		return MFAEnrollment{}, ErrMFAAlreadyEnrolled
	}
	secret, encoded, err := NewTOTPSecret()
	if err != nil {
		return MFAEnrollment{}, err
	}
	ciphertext, nonce, err := box.Encrypt(secret, totpAAD(session.UserID))
	if err != nil {
		return MFAEnrollment{}, err
	}
	now := time.Now()
	expires := now.Add(s.pendingTTL())
	if expires.After(session.ExpiresAt) {
		expires = session.ExpiresAt
	}
	if _, err := s.Store.CreatePendingTOTPFactor(ctx, session.UserID, ciphertext, nonce, expires); err != nil {
		return MFAEnrollment{}, err
	}
	account, err := s.Store.Account(ctx, session.UserID)
	if err != nil {
		return MFAEnrollment{}, err
	}
	issuer := s.MFAIssuer
	if issuer == "" {
		issuer = DefaultMFAIssuer
	}
	s.recordEvent(ctx, &session.UserID, "auth.mfa_enrollment_started", client, nil)
	return MFAEnrollment{
		Secret:     encoded,
		OTPAuthURL: totpProvisioningURI(issuer, account.Email, encoded),
		ExpiresAt:  expires,
	}, nil
}

// ConfirmMFAEnrollment activates the factor only after the first valid code and
// rotates the pending session into a fully verified session.
func (s *AuthService) ConfirmMFAEnrollment(ctx context.Context, session Session, code string, client ClientInfo) (MFAResult, error) {
	if err := s.requirePendingPlatformSession(ctx, session); err != nil {
		return MFAResult{}, err
	}
	box, err := s.secretBox()
	if err != nil {
		return MFAResult{}, err
	}
	factor, err := s.Store.PendingTOTPFactor(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return MFAResult{}, ErrMFANotEnrolled
		}
		return MFAResult{}, err
	}
	if factor.EnrollmentExpiresAt == nil || !factor.EnrollmentExpiresAt.After(time.Now()) {
		return MFAResult{}, ErrMFAExpired
	}
	secret, err := box.Decrypt(factor.SecretCiphertext, factor.SecretNonce, totpAAD(session.UserID))
	if err != nil {
		return MFAResult{}, err
	}
	step, valid := VerifyTOTP(secret, code, time.Now())
	if !valid {
		s.failMFA(ctx, session, factor.ID, client, "enrollment")
		return MFAResult{}, ErrMFAInvalidCode
	}
	recoveryCodes, recoveryHashes, err := s.newRecoveryCodes()
	if err != nil {
		return MFAResult{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return MFAResult{}, err
	}
	ipHash, uaHash := s.metadataHashes(client)
	expiry, err := s.Store.CompleteMFAEnrollment(ctx, session, factor.ID, step, sessionTokenHash(token), recoveryHashes, ipHash, uaHash)
	if err != nil {
		return MFAResult{}, err
	}
	return MFAResult{Token: token, ExpiresAt: expiry, RecoveryCodes: recoveryCodes}, nil
}

// VerifyMFAChallenge completes the TOTP challenge for an existing active factor.
func (s *AuthService) VerifyMFAChallenge(ctx context.Context, session Session, code string, client ClientInfo) (MFAResult, error) {
	if err := s.requirePendingPlatformSession(ctx, session); err != nil {
		return MFAResult{}, err
	}
	box, err := s.secretBox()
	if err != nil {
		return MFAResult{}, err
	}
	factor, err := s.Store.ActiveTOTPFactor(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return MFAResult{}, ErrMFANotEnrolled
		}
		return MFAResult{}, err
	}
	if factor.LockedUntil != nil && factor.LockedUntil.After(time.Now()) {
		return MFAResult{}, ErrMFALocked
	}
	secret, err := box.Decrypt(factor.SecretCiphertext, factor.SecretNonce, totpAAD(session.UserID))
	if err != nil {
		return MFAResult{}, err
	}
	step, valid := VerifyTOTP(secret, code, time.Now())
	if !valid {
		s.failMFA(ctx, session, factor.ID, client, "totp")
		return MFAResult{}, ErrMFAInvalidCode
	}
	if factor.LastUsedStep != nil && step <= *factor.LastUsedStep {
		s.failMFA(ctx, session, factor.ID, client, "replay")
		return MFAResult{}, ErrMFAReplay
	}
	token, err := randomToken(32)
	if err != nil {
		return MFAResult{}, err
	}
	ipHash, uaHash := s.metadataHashes(client)
	expiry, err := s.Store.CompleteMFAChallenge(ctx, session, factor.ID, step, sessionTokenHash(token), ipHash, uaHash)
	if err != nil {
		if errors.Is(err, ErrMFAReplay) {
			s.failMFA(ctx, session, factor.ID, client, "replay")
		}
		return MFAResult{}, err
	}
	return MFAResult{Token: token, ExpiresAt: expiry}, nil
}

func (s *AuthService) VerifyMFARecovery(ctx context.Context, session Session, code string, client ClientInfo) (MFAResult, error) {
	if err := s.requirePendingPlatformSession(ctx, session); err != nil {
		return MFAResult{}, err
	}
	if len(s.MFAKey) != 32 {
		return MFAResult{}, ErrMFAUnavailable
	}
	normalized := normalizeRecoveryCode(code)
	if normalized == "" {
		return MFAResult{}, ErrMFAInvalidCode
	}
	token, err := randomToken(32)
	if err != nil {
		return MFAResult{}, err
	}
	ipHash, uaHash := s.metadataHashes(client)
	expiry, err := s.Store.CompleteMFARecovery(ctx, session, recoveryCodeHash(s.recoveryPepper(), normalized), sessionTokenHash(token), ipHash, uaHash)
	if err != nil {
		if errors.Is(err, ErrMFAInvalidCode) {
			if _, failureErr := s.Store.RecordSessionMFAFailure(ctx, session); failureErr != nil && !errors.Is(failureErr, ErrUnauthenticated) {
				return MFAResult{}, failureErr
			}
			s.recordEvent(ctx, &session.UserID, "auth.mfa_recovery_failed", client, nil)
		}
		return MFAResult{}, err
	}
	return MFAResult{Token: token, ExpiresAt: expiry}, nil
}

func (s *AuthService) failMFA(ctx context.Context, session Session, factorID string, client ClientInfo, stage string) {
	_, _ = s.Store.RecordTOTPFailure(ctx, factorID, session.UserID)
	_, _ = s.Store.RecordSessionMFAFailure(ctx, session)
	s.recordEvent(ctx, &session.UserID, "auth.mfa_failed", client, map[string]any{"stage": stage})
}

func (s *AuthService) newRecoveryCodes() ([]string, []string, error) {
	codes := make([]string, 0, mfaRecoveryCodeCount)
	hashes := make([]string, 0, mfaRecoveryCodeCount)
	pepper := s.recoveryPepper()
	for i := 0; i < mfaRecoveryCodeCount; i++ {
		raw, err := randomBytes(10)
		if err != nil {
			return nil, nil, err
		}
		encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
		code := strings.Join([]string{encoded[0:4], encoded[4:8], encoded[8:12], encoded[12:16]}, "-")
		codes = append(codes, code)
		hashes = append(hashes, recoveryCodeHash(pepper, normalizeRecoveryCode(code)))
	}
	return codes, hashes, nil
}

func (s *AuthService) recoveryPepper() []byte {
	mac := hmac.New(sha256.New, s.MFAKey)
	_, _ = mac.Write([]byte("mfa-recovery-codes-v1"))
	return mac.Sum(nil)
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.ReplaceAll(code, "-", "")
	code = strings.ReplaceAll(code, " ", "")
	return code
}

func totpAAD(userID string) string {
	return fmt.Sprintf("mfa-totp-v1:%s", userID)
}

// PlatformSession authorizes the privileged control plane only for an active
// platform role with a verified MFA session. It never grants tenant access.
func (s *AuthService) PlatformSession(ctx context.Context, session Session) error {
	if session.AuthState != "active" || session.MFAVerifiedAt == nil {
		return ErrMFARequired
	}
	allowed, err := s.Store.IsPlatformAdmin(ctx, session.UserID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	capabilities, err := s.Store.PlatformCapabilities(ctx, session.UserID)
	if err != nil {
		return err
	}
	for _, capability := range capabilities {
		if capability == "platform.access" {
			return nil
		}
	}
	return ErrForbidden
}
