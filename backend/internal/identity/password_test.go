package identity

import (
	"context"
	"strings"
	"testing"
)

func TestArgon2idHashAndVerify(t *testing.T) {
	hasher := PasswordHasher{}
	first, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("separate hashes reused the same salt")
	}
	if !strings.HasPrefix(first, "$argon2id$v=19$") {
		t.Fatalf("hash does not identify Argon2id: %q", first)
	}
	for _, tc := range []struct {
		password string
		want     bool
	}{
		{"correct horse battery staple", true},
		{"incorrect horse battery staple", false},
	} {
		got, err := hasher.Verify(first, tc.password)
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("Verify(..., %q) = %v, want %v", tc.password, got, tc.want)
		}
	}
}

func TestArgon2idRejectsMalformedAndExcessiveHashes(t *testing.T) {
	hasher := PasswordHasher{}
	for _, encoded := range []string{
		"not-a-hash",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=999999999,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$YWJjZGVmZ2hpamtsbW5vcA",
		"$argon2id$v=19$m=19456,t=2,p=1$bad!$bad!",
	} {
		if _, err := hasher.Verify(encoded, "some password"); err == nil {
			t.Errorf("Verify accepted malformed hash %q", encoded)
		}
	}
}

func TestArgon2idRejectsEmptyAndOversizePasswords(t *testing.T) {
	hasher := PasswordHasher{}
	if _, err := hasher.Hash(""); err == nil {
		t.Fatal("Hash accepted empty password")
	}
	if _, err := hasher.Verify("invalid", strings.Repeat("x", maxPasswordBytes+1)); err == nil {
		t.Fatal("Verify accepted oversized password")
	}
}

func TestSessionTokenHashDoesNotReturnRawToken(t *testing.T) {
	token := "opaque-session-token"
	hashed := sessionTokenHash(token)
	if hashed == token || len(hashed) != 64 {
		t.Fatalf("token hash has invalid shape %q", hashed)
	}
}

func TestAuthServiceRejectsMissingStore(t *testing.T) {
	if _, _, err := (&AuthService{}).Login(context.Background(), Credentials{Email: "a@example.com", Password: "pass"}); err == nil {
		t.Fatal("Login with missing store succeeded")
	}
}
