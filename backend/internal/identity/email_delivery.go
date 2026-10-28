package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"construction-saas/backend/internal/email"
	"construction-saas/backend/internal/outbox"
)

// EmailDeliveryHandler turns durable outbox events into SMTP delivery attempts.
// It is safe to retry: a delivered row is a no-op and the Message-ID is stable
// per delivery identifier, though SMTP itself does not guarantee exactly-once
// delivery.
type EmailDeliveryHandler struct {
	Store     *Store
	Box       *SecretBox
	Sender    email.Sender
	PublicURL string
	Logger    *slog.Logger
	Now       func() time.Time
}

func (h *EmailDeliveryHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *EmailDeliveryHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *EmailDeliveryHandler) Handle(ctx context.Context, event outbox.Event) error {
	if h.Store == nil || h.Box == nil || h.Sender == nil {
		return errors.New("email delivery handler is not configured")
	}
	if event.Type != "email.delivery" {
		return fmt.Errorf("unsupported outbox event type %q", event.Type)
	}
	var payload struct {
		DeliveryID string `json:"delivery_id"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload.DeliveryID == "" {
		return errors.New("invalid email delivery payload")
	}
	var (
		recipient   string
		userID      *string
		template    string
		ciphertext  []byte
		nonce       []byte
		deliveredAt *time.Time
		expiresAt   time.Time
	)
	err := h.Store.pool.QueryRow(ctx, `
		SELECT recipient_email, user_id::text, template, token_ciphertext, token_nonce, delivered_at, link_expires_at
		FROM app.auth_email_deliveries WHERE id=$1::uuid`, payload.DeliveryID).Scan(
		&recipient, &userID, &template, &ciphertext, &nonce, &deliveredAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("email delivery record not found")
	}
	if err != nil {
		return fmt.Errorf("load email delivery: %w", err)
	}
	if deliveredAt != nil {
		return nil
	}
	if !expiresAt.After(h.now()) {
		// The link is already dead; discard the encrypted secret instead of
		// retrying forever.
		return h.clearSecret(ctx, payload.DeliveryID)
	}
	if template == "company_invitation" {
		var managed bool
		if err := h.Store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app.company_invitations WHERE delivery_id=$1::uuid)`, payload.DeliveryID).Scan(&managed); err != nil {
			return err
		}
		if managed {
			var valid bool
			err := h.Store.pool.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM app.company_invitations i JOIN app.companies c ON c.id=i.company_id
				WHERE i.delivery_id=$1::uuid AND i.accepted_at IS NULL AND i.revoked_at IS NULL
				AND i.expires_at>now() AND c.status IN ('active','provisioning')
			)`, payload.DeliveryID).Scan(&valid)
			if err != nil {
				return err
			}
			if !valid {
				return h.clearSecret(ctx, payload.DeliveryID)
			}
		}
	}
	aadUserID := ""
	if userID != nil {
		aadUserID = *userID
	}
	raw, err := h.Box.Decrypt(ciphertext, nonce, emailTokenAAD(template, aadUserID))
	if err != nil {
		return errors.New("email delivery secret could not be decrypted")
	}
	link, err := deliveryLink(h.publicURL(), template, string(raw))
	if err != nil {
		return err
	}
	subject, body := deliveryContent(template, link)
	message := email.Message{
		To: recipient, Subject: subject, Body: body, HTMLBody: deliveryHTML(template, link),
		MessageID: "<" + payload.DeliveryID + "@construction-saas.invalid>",
	}
	if err := h.Sender.Send(ctx, message); err != nil {
		// The caller retries through the outbox; never include the recipient.
		return fmt.Errorf("send %s email: %w", template, err)
	}
	if _, err := h.Store.pool.Exec(ctx, `
		UPDATE app.auth_email_deliveries
		SET delivered_at=now(), token_ciphertext=NULL, token_nonce=NULL
		WHERE id=$1::uuid AND delivered_at IS NULL`, payload.DeliveryID); err != nil {
		return fmt.Errorf("mark email delivered: %w", err)
	}
	h.logger().InfoContext(ctx, "auth email delivered",
		slog.Int64("event_id", event.ID), slog.String("delivery_id", payload.DeliveryID),
		slog.String("template", template))
	return nil
}

func (h *EmailDeliveryHandler) clearSecret(ctx context.Context, deliveryID string) error {
	_, err := h.Store.pool.Exec(ctx, `
		UPDATE app.auth_email_deliveries
		SET delivered_at=now(), token_ciphertext=NULL, token_nonce=NULL
		WHERE id=$1::uuid AND delivered_at IS NULL`, deliveryID)
	return err
}

func (h *EmailDeliveryHandler) publicURL() string {
	return strings.TrimRight(strings.TrimSpace(h.PublicURL), "/")
}

func deliveryLink(publicURL, template, token string) (string, error) {
	if publicURL == "" {
		return "", errors.New("public frontend URL is not configured")
	}
	var path string
	switch template {
	case PurposePasswordReset:
		path = "/reset-password"
	case PurposeEmailVerification:
		path = "/verify-email"
	case "company_invitation":
		path = "/invitations/accept"
	default:
		return "", fmt.Errorf("unsupported email template %q", template)
	}
	return publicURL + path + "?token=" + url.QueryEscape(token), nil
}

func deliveryContent(template, link string) (string, string) {
	switch template {
	case PurposePasswordReset:
		return "Atur ulang kata sandi Dexotic Groundwork", "Kami menerima permintaan untuk mengatur ulang kata sandi akun Anda. Buka tautan berikut untuk memilih kata sandi baru (berlaku 30 menit, hanya sekali pakai):\n\n" + link + "\n\nJika Anda tidak meminta ini, abaikan email ini. Kata sandi Anda tidak berubah."
	case PurposeEmailVerification:
		return "Verifikasi email Dexotic Groundwork", "Verifikasi alamat email Anda melalui tautan berikut (berlaku 24 jam, hanya sekali pakai):\n\n" + link + "\n\nJika Anda tidak mengenali permintaan ini, abaikan email ini."
	case "company_invitation":
		return "Undangan ke Dexotic Groundwork", "Anda diundang bergabung ke ruang kerja perusahaan. Terima undangan melalui tautan berikut:\n\n" + link + "\n\nJika Anda tidak menunggu undangan ini, abaikan email ini."
	default:
		return "", ""
	}
}

// deliveryHTML uses only static copy plus an escaped, first-party link.
// Keep the raw URL visible so recipients can inspect the destination.
func deliveryHTML(template, link string) string {
	var title, label, detail, action, note string
	switch template {
	case PurposePasswordReset:
		title, label, detail, action, note = "Atur ulang kata sandi", "KEAMANAN AKUN", "Tautan ini berlaku 30 menit dan hanya dapat digunakan sekali.", "Atur ulang kata sandi", "Jika Anda tidak meminta perubahan ini, abaikan email ini. Kata sandi Anda tidak berubah."
	case PurposeEmailVerification:
		title, label, detail, action, note = "Verifikasi alamat email", "VERIFIKASI AKUN", "Tautan ini berlaku 24 jam dan hanya dapat digunakan sekali.", "Verifikasi email", "Jika Anda tidak mengenali permintaan ini, abaikan email ini."
	case "company_invitation":
		title, label, detail, action, note = "Undangan ruang kerja", "UNDANGAN", "Gunakan tautan berikut untuk menerima undangan Anda.", "Terima undangan", "Jika Anda tidak menunggu undangan ini, abaikan email ini."
	default:
		return ""
	}
	safeLink := html.EscapeString(link)
	return `<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"></head>` +
		`<body style="margin:0;padding:0;background:#f1f2eb;color:#20271f;font-family:Arial,Helvetica,sans-serif">` +
		`<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="background:#f1f2eb"><tr><td align="center" style="padding:32px 16px">` +
		`<table role="presentation" cellpadding="0" cellspacing="0" width="100%" style="max-width:560px;background:#ffffff;border:1px solid #d5dccb"><tr><td style="padding:24px 32px;border-bottom:3px solid #6b8355;font-size:16px;font-weight:700;letter-spacing:-.02em">Dexotic Groundwork</td></tr>` +
		`<tr><td style="padding:36px 32px 24px"><p style="margin:0 0 16px;color:#526746;font-size:12px;font-weight:700;letter-spacing:.1em">` + label + `</p>` +
		`<h1 style="margin:0 0 16px;font-size:28px;line-height:1.2;font-weight:700">` + title + `</h1>` +
		`<p style="margin:0 0 24px;color:#4b5548;font-size:16px;line-height:1.6">` + detail + `</p>` +
		`<table role="presentation" cellpadding="0" cellspacing="0"><tr><td bgcolor="#283a29" style="border-radius:4px"><a href="` + safeLink + `" style="display:inline-block;padding:14px 22px;color:#ffffff;font-size:15px;font-weight:700;text-decoration:none">` + action + `</a></td></tr></table>` +
		`<p style="margin:28px 0 8px;color:#4b5548;font-size:13px;line-height:1.5">Jika tombol tidak berfungsi, salin tautan ini ke browser:</p>` +
		`<p style="margin:0;word-break:break-all;font-size:13px;line-height:1.5"><a href="` + safeLink + `" style="color:#315a35">` + safeLink + `</a></p></td></tr>` +
		`<tr><td style="padding:20px 32px;border-top:1px solid #d5dccb;color:#586454;font-size:13px;line-height:1.5">` + note + `</td></tr></table>` +
		`<p style="margin:16px 0;color:#586454;font-size:12px">Dexotic Groundwork</p></td></tr></table></body></html>`
}
