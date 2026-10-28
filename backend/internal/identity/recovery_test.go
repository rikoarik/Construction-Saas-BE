package identity

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func recoveryService(t *testing.T, store *Store) *AuthService {
	t.Helper()
	return &AuthService{
		Store:      store,
		Passwords:  PasswordHasher{},
		MFAKey:     testMFAKey(),
		EmailKey:   testMFAKey(),
		PublicURL:  "https://app.example.test",
		SessionTTL: time.Hour,
	}
}

func latestDeliveryToken(t *testing.T, service *AuthService, userID, template string) string {
	t.Helper()
	var ciphertext, nonce []byte
	err := service.Store.pool.QueryRow(context.Background(), `
		SELECT token_ciphertext, token_nonce FROM app.auth_email_deliveries
		WHERE user_id=$1::uuid AND template=$2 AND delivered_at IS NULL
		ORDER BY created_at DESC LIMIT 1`, userID, template).Scan(&ciphertext, &nonce)
	if err != nil {
		t.Fatalf("load delivery: %v", err)
	}
	box, err := service.emailBox()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := box.Decrypt(ciphertext, nonce, emailTokenAAD(template, userID))
	if err != nil {
		t.Fatalf("decrypt delivery: %v", err)
	}
	return string(raw)
}

func TestPasswordResetRevokesSessionsAndConsumesToken(t *testing.T) {
	store, user := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	session, err := store.CreateSession(ctx, user.ID, sessionTokenHash("reset-session"), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RequestPasswordReset(ctx, user.Email, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token := latestDeliveryToken(t, service, user.ID, PurposePasswordReset)

	var outboxCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.outbox_events WHERE event_type='email.delivery' AND payload ? 'delivery_id'`).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if outboxCount == 0 {
		t.Fatal("password reset did not enqueue an email delivery")
	}

	next := "a completely new password"
	if err := service.ResetPassword(ctx, token, next, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetActiveSessionByTokenHash(ctx, session.TokenHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session survived reset: %v", err)
	}
	if err := service.ResetPassword(ctx, token, "another new password value", ClientInfo{}); !errors.Is(err, ErrAuthTokenInvalid) {
		t.Fatalf("token replay = %v", err)
	}
	user2, err := store.GetUserByNormalizedEmail(ctx, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := service.Passwords.Verify(user2.PasswordHash, next)
	if err != nil || !valid {
		t.Fatalf("new password does not verify: valid=%v err=%v", valid, err)
	}
	if user2.SecurityVersion <= user.SecurityVersion {
		t.Fatalf("security version did not advance: %d -> %d", user.SecurityVersion, user2.SecurityVersion)
	}
}

func TestPasswordResetUnknownAccountIsIndistinguishable(t *testing.T) {
	store, _ := integrationIdentity(t)
	service := recoveryService(t, store)
	if err := service.RequestPasswordReset(context.Background(), "missing@example.invalid", ClientInfo{}); err != nil {
		t.Fatalf("unknown account = %v", err)
	}
}

func TestExpiredAndConcurrentResetTokens(t *testing.T) {
	store, user := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()

	expired := "expired-token-value-" + user.ID
	if _, err := store.pool.Exec(ctx, `INSERT INTO app.auth_tokens (user_id,purpose,token_hash,expires_at)
		VALUES ($1::uuid,'password_reset',$2,now()-interval '1 minute')`, user.ID, tokenDigest(expired)); err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, expired, "a sufficiently long password", ClientInfo{}); !errors.Is(err, ErrAuthTokenInvalid) {
		t.Fatalf("expired token = %v", err)
	}

	if err := service.RequestPasswordReset(ctx, user.Email, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token := latestDeliveryToken(t, service, user.ID, PurposePasswordReset)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- service.ResetPassword(ctx, token, "a sufficiently long password", ClientInfo{})
		}()
	}
	wg.Wait()
	close(results)
	success, invalid := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrAuthTokenInvalid):
			invalid++
		default:
			t.Fatalf("concurrent reset error: %v", err)
		}
	}
	if success != 1 || invalid != 1 {
		t.Fatalf("concurrent reset success=%d invalid=%d", success, invalid)
	}
}

func TestEmailVerificationSingleUse(t *testing.T) {
	store, user := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	if err := service.ResendVerification(ctx, user.Email, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	token := latestDeliveryToken(t, service, user.ID, PurposeEmailVerification)
	if err := service.VerifyEmail(ctx, token, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	var verified *time.Time
	if err := store.pool.QueryRow(ctx, `SELECT email_verified_at FROM app.users WHERE id=$1::uuid`, user.ID).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if verified == nil {
		t.Fatal("email_verified_at was not set")
	}
	if err := service.VerifyEmail(ctx, token, ClientInfo{}); !errors.Is(err, ErrAuthTokenInvalid) {
		t.Fatalf("verification replay = %v", err)
	}
	// An already verified account must not receive another message.
	if err := service.ResendVerification(ctx, user.Email, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.auth_email_deliveries
		WHERE user_id=$1::uuid AND template='email_verification' AND delivered_at IS NULL`, user.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending verification deliveries = %d, want the original one", pending)
	}
}
