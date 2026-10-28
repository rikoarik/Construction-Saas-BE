package main

import (
	"testing"

	"construction-saas/backend/internal/identity"
)

func TestProductionCSRFRequiresSecretAndOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("CSRF_SECRET", "")
	t.Setenv("TRUSTED_ORIGINS", "")
	if _, err := securityCSRFConfig(&identity.AuthService{}); err == nil {
		t.Fatal("missing secret accepted")
	}
	t.Setenv("CSRF_SECRET", "0123456789abcdef0123456789abcdef")
	if _, err := securityCSRFConfig(&identity.AuthService{}); err == nil {
		t.Fatal("missing origins accepted")
	}
	t.Setenv("TRUSTED_ORIGINS", "https://app.example.test")
	if _, err := securityCSRFConfig(&identity.AuthService{}); err != nil {
		t.Fatal(err)
	}
}
