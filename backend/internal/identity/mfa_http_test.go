package identity

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func testMFAKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func mfaService(t *testing.T, store *Store) (*AuthService, *http.ServeMux) {
	t.Helper()
	service := &AuthService{Store: store, MFAKey: testMFAKey(), SessionTTL: time.Hour}
	mux := http.NewServeMux()
	RegisterAuthRoutes(mux, service, false)
	return service, mux
}

func setPassword(t *testing.T, store *Store, userID, password string) {
	t.Helper()
	hash, err := (PasswordHasher{}).Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(),
		`UPDATE app.users SET password_hash=$2 WHERE id=$1::uuid`, userID, hash); err != nil {
		t.Fatal(err)
	}
}

func createActiveFactor(t *testing.T, store *Store, key []byte, userID string) string {
	t.Helper()
	secret, _, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, nonce, err := box.Encrypt(secret, totpAAD(userID))
	if err != nil {
		t.Fatal(err)
	}
	var factorID string
	err = store.pool.QueryRow(context.Background(), `INSERT INTO app.mfa_totp_factors
		(user_id, secret_ciphertext, secret_nonce, status, activated_at)
		VALUES ($1::uuid, $2, $3, 'active', now()) RETURNING id::text`,
		userID, ciphertext, nonce).Scan(&factorID)
	if err != nil {
		t.Fatal(err)
	}
	return factorID
}

func grantPlatform(t *testing.T, store *Store, userID string) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`INSERT INTO app.platform_admins (user_id) VALUES ($1::uuid)`, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM app.platform_admins WHERE user_id=$1::uuid`, userID)
	})
}

func callJSON(mux *http.ServeMux, method, path string, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)
	return res
}

func loginPending(t *testing.T, mux *http.ServeMux, email, password string) *http.Cookie {
	t.Helper()
	payload, _ := json.Marshal(Credentials{Email: email, Password: password})
	res := callJSON(mux, http.MethodPost, "/api/v1/auth/login", string(payload), nil)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", res.Code, res.Body.String())
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d", len(cookies))
	}
	return cookies[0]
}

func decodeTOTP(t *testing.T, encoded string) []byte {
	t.Helper()
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	return secret
}

func TestPlatformLoginStaysPendingUntilMFA(t *testing.T) {
	store, user := integrationIdentity(t)
	grantPlatform(t, store, user.ID)
	password := "a sufficiently long password"
	setPassword(t, store, user.ID, password)
	_, mux := mfaService(t, store)

	payload, _ := json.Marshal(Credentials{Email: user.Email, Password: password})
	res := callJSON(mux, http.MethodPost, "/api/v1/auth/login", string(payload), nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), LoginNextMFAEnrollmentRequired) {
		t.Fatalf("login = %d %s", res.Code, res.Body.String())
	}
	cookie := res.Result().Cookies()[0]

	for _, path := range []string{"/api/v1/auth/me", "/api/v1/auth/sessions", "/api/v1/auth/companies"} {
		pending := callJSON(mux, http.MethodGet, path, "", cookie)
		if pending.Code != http.StatusForbidden || !strings.Contains(pending.Body.String(), "MFA_REQUIRED") {
			t.Fatalf("%s with pending session = %d %s", path, pending.Code, pending.Body.String())
		}
	}
	platform := callJSON(mux, http.MethodGet, "/api/v1/platform/session", "", cookie)
	if platform.Code != http.StatusForbidden {
		t.Fatalf("platform with pending session = %d %s", platform.Code, platform.Body.String())
	}
}

func TestMFAEnrollmentChallengeAndReplay(t *testing.T) {
	store, user := integrationIdentity(t)
	grantPlatform(t, store, user.ID)
	password := "a sufficiently long password"
	setPassword(t, store, user.ID, password)
	_, mux := mfaService(t, store)
	pending := loginPending(t, mux, user.Email, password)

	start := callJSON(mux, http.MethodPost, "/api/v1/auth/mfa/enrollment", "", pending)
	if start.Code != http.StatusOK {
		t.Fatalf("start enrollment = %d %s", start.Code, start.Body.String())
	}
	var enrollment MFAEnrollment
	if err := json.Unmarshal(start.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	secret := decodeTOTP(t, enrollment.Secret)
	code := totpCode(secret, totpStep(time.Now()))

	wrong := callJSON(mux, http.MethodPost, "/api/v1/auth/mfa/enrollment/confirm", `{"code":"000000"}`, pending)
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), "INVALID_MFA_CODE") {
		t.Fatalf("wrong enrollment code = %d %s", wrong.Code, wrong.Body.String())
	}

	confirm := callJSON(mux, http.MethodPost, "/api/v1/auth/mfa/enrollment/confirm", `{"code":"`+code+`"}`, pending)
	if confirm.Code != http.StatusOK {
		t.Fatalf("confirm = %d %s", confirm.Code, confirm.Body.String())
	}
	var result MFAResult
	if err := json.Unmarshal(confirm.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.RecoveryCodes) != mfaRecoveryCodeCount {
		t.Fatalf("recovery codes = %d", len(result.RecoveryCodes))
	}
	rotated := confirm.Result().Cookies()
	if len(rotated) != 1 || rotated[0].Value == pending.Value {
		t.Fatalf("session token was not rotated after MFA")
	}
	if stale := callJSON(mux, http.MethodGet, "/api/v1/auth/me", "", pending); stale.Code != http.StatusUnauthorized {
		t.Fatalf("old pending token = %d", stale.Code)
	}
	if platform := callJSON(mux, http.MethodGet, "/api/v1/platform/session", "", rotated[0]); platform.Code != http.StatusOK {
		t.Fatalf("platform after MFA = %d %s", platform.Code, platform.Body.String())
	}

	// A second login creates a fresh pending session; replaying the same TOTP
	// timestep must be rejected even though the code is still inside the
	// acceptance window.
	second := loginPending(t, mux, user.Email, password)
	replay := callJSON(mux, http.MethodPost, "/api/v1/auth/mfa/challenge", `{"code":"`+code+`"}`, second)
	if replay.Code != http.StatusUnauthorized || !strings.Contains(replay.Body.String(), "MFA_CODE_REPLAYED") {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body.String())
	}
}

func TestMFARecoveryCodeConsumedOnceConcurrently(t *testing.T) {
	store, user := integrationIdentity(t)
	grantPlatform(t, store, user.ID)
	service, _ := mfaService(t, store)
	ctx := context.Background()

	codes, hashes, err := service.newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	factorID := createActiveFactor(t, store, service.MFAKey, user.ID)
	for _, hash := range hashes {
		if _, err := store.pool.Exec(ctx, `INSERT INTO app.mfa_recovery_codes (user_id, factor_id, code_hash)
			VALUES ($1::uuid,$2::uuid,$3)`, user.ID, factorID, hash); err != nil {
			t.Fatal(err)
		}
	}
	sessions := make([]Session, 2)
	for i := range sessions {
		sessions[i], err = store.CreatePendingMFASessionForPassword(ctx, user.ID, user.PasswordHash,
			sessionTokenHash("recovery-race-"+string(rune('a'+i))), time.Now().Add(5*time.Minute), time.Now().Add(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range sessions {
		wg.Add(1)
		go func(session Session) {
			defer wg.Done()
			_, err := service.VerifyMFARecovery(ctx, session, codes[0], ClientInfo{})
			results <- err
		}(sessions[i])
	}
	wg.Wait()
	close(results)
	success, rejected := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errorsIsMFAInvalid(err):
			rejected++
		default:
			t.Fatalf("unexpected recovery error: %v", err)
		}
	}
	if success != 1 || rejected != 1 {
		t.Fatalf("recovery success=%d rejected=%d", success, rejected)
	}
}

func errorsIsMFAInvalid(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "invalid MFA code") ||
		strings.Contains(err.Error(), "authentication required"))
}

func TestMFAFailedAttemptsRevokePendingSession(t *testing.T) {
	store, user := integrationIdentity(t)
	grantPlatform(t, store, user.ID)
	password := "a sufficiently long password"
	setPassword(t, store, user.ID, password)
	createActiveFactor(t, store, testMFAKey(), user.ID)
	_, mux := mfaService(t, store)
	pending := loginPending(t, mux, user.Email, password)
	for i := 0; i < mfaMaxFailures; i++ {
		res := callJSON(mux, http.MethodPost, "/api/v1/auth/mfa/challenge", `{"code":"000000"}`, pending)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s", i+1, res.Code, res.Body.String())
		}
	}
	// The fifth failure revokes the pending session, so a later valid request
	// is unauthenticated rather than merely rate-limited.
	if res := callJSON(mux, http.MethodGet, "/api/v1/auth/me", "", pending); res.Code != http.StatusUnauthorized {
		t.Fatalf("revoked pending session = %d", res.Code)
	}
}
