package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"construction-saas/backend/internal/email"
	"construction-saas/backend/internal/outbox"
)

type recordingSender struct {
	messages []email.Message
	err      error
}

func (s *recordingSender) Send(_ context.Context, message email.Message) error {
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, message)
	return nil
}

func claimEmailEvent(t *testing.T, store *Store, userID string) outbox.Event {
	t.Helper()
	var event outbox.Event
	err := store.pool.QueryRow(context.Background(), `
		SELECT o.id, o.event_type, o.payload
		FROM app.outbox_events o
		JOIN app.auth_email_deliveries d ON d.id::text = o.payload->>'delivery_id'
		WHERE d.user_id=$1::uuid
		ORDER BY o.id DESC LIMIT 1`, userID).Scan(&event.ID, &event.Type, &event.Payload)
	if err != nil {
		t.Fatalf("load delivery event: %v", err)
	}
	return event
}

func TestDeliveryHTMLUsesEscapedFirstPartyLink(t *testing.T) {
	link := "https://app.example.test/reset-password?token=a&next=\"b"
	body := deliveryHTML(PurposePasswordReset, link)
	if !strings.Contains(body, `href="https://app.example.test/reset-password?token=a&amp;next=&#34;b"`) || strings.Contains(body, `next="b"`) {
		t.Fatal("reset link was not HTML-escaped")
	}
	if body := deliveryHTML("unknown", link); body != "" {
		t.Fatal("unknown template rendered an email")
	}
}

func TestEmailDeliveryHandlerRetriesWithoutRecreatingTokens(t *testing.T) {
	store, user := integrationIdentity(t)
	service := recoveryService(t, store)
	ctx := context.Background()
	if err := service.RequestPasswordReset(ctx, user.Email, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	event := claimEmailEvent(t, store, user.ID)
	box, err := service.emailBox()
	if err != nil {
		t.Fatal(err)
	}
	sender := &recordingSender{err: errors.New("smtp unavailable")}
	handler := &EmailDeliveryHandler{Store: store, Box: box, Sender: sender, PublicURL: "https://app.example.test"}

	if err := handler.Handle(ctx, event); err == nil {
		t.Fatal("delivery succeeded despite sender failure")
	}
	var pending int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.auth_email_deliveries
		WHERE user_id=$1::uuid AND delivered_at IS NULL AND token_ciphertext IS NOT NULL`, user.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("pending encrypted deliveries = %d, want 1", pending)
	}
	var tokenCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM app.auth_tokens
		WHERE user_id=$1::uuid AND purpose='password_reset' AND consumed_at IS NULL`, user.ID).Scan(&tokenCount); err != nil {
		t.Fatal(err)
	}
	if tokenCount != 1 {
		t.Fatalf("outstanding reset tokens = %d, want 1", tokenCount)
	}

	sender.err = nil
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("messages = %d", len(sender.messages))
	}
	message := sender.messages[0]
	if !strings.Contains(message.HTMLBody, "href=\"https://app.example.test/reset-password?token=") || !strings.Contains(message.HTMLBody, "Atur ulang kata sandi") {
		t.Fatal("missing reset action in HTML email")
	}
	if !strings.Contains(message.Body, "https://app.example.test/reset-password?token=") {
		t.Fatalf("message body = %s", message.Body)
	}
	if !strings.HasPrefix(message.MessageID, "<") || !strings.HasSuffix(message.MessageID, "@construction-saas.invalid>") {
		t.Fatalf("message id = %q", message.MessageID)
	}
	// A repeated event after successful delivery is a no-op: no second email
	// and no second token.
	if err := handler.Handle(ctx, event); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if len(sender.messages) != 1 {
		t.Fatalf("duplicate delivery messages = %d", len(sender.messages))
	}
	var ciphertextCleared bool
	if err := store.pool.QueryRow(ctx, `SELECT token_ciphertext IS NULL AND delivered_at IS NOT NULL
		FROM app.auth_email_deliveries WHERE user_id=$1::uuid`, user.ID).Scan(&ciphertextCleared); err != nil {
		t.Fatal(err)
	}
	if !ciphertextCleared {
		t.Fatal("delivery secret was not cleared")
	}
}
