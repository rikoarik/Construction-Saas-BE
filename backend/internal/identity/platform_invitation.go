package identity

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// QueuePlatformInvitation must run inside the caller's company transaction. Only
// the delivery identifier (never the raw token) is placed in the outbox.
func (s *AuthService) QueuePlatformInvitation(ctx context.Context, tx pgx.Tx, actorID, companyID, email string) (Invitation, error) {
	email = strings.TrimSpace(email)
	normalized := NormalizeEmail(email)
	if _, err := s.frontendURL(); err != nil {
		return Invitation{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return Invitation{}, err
	}
	box, err := s.emailBox()
	if err != nil {
		return Invitation{}, err
	}
	ciphertext, nonce, err := box.Encrypt([]byte(token), emailTokenAAD("company_invitation", ""))
	if err != nil {
		return Invitation{}, err
	}
	expires := time.Now().Add(invitationTTL)
	var i Invitation
	err = tx.QueryRow(ctx, `INSERT INTO app.company_invitations (company_id,email,email_normalized,token_hash,role,expires_at,created_by)
 VALUES ($1::uuid,$2,$3,$4,'admin',$5,$6::uuid) RETURNING id::text,email,role,expires_at,created_at`, companyID, email, normalized, tokenDigest(token), expires, actorID).Scan(&i.ID, &i.Email, &i.Role, &i.ExpiresAt, &i.CreatedAt)
	if err != nil {
		return Invitation{}, err
	}
	var deliveryID string
	err = tx.QueryRow(ctx, `INSERT INTO app.auth_email_deliveries (recipient_email,template,token_ciphertext,token_nonce,link_expires_at)
 VALUES ($1,'company_invitation',$2,$3,$4) RETURNING id::text`, email, ciphertext, nonce, expires).Scan(&deliveryID)
	if err != nil {
		return Invitation{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE app.company_invitations SET delivery_id=$2::uuid WHERE id=$1::uuid`, i.ID, deliveryID); err != nil {
		return Invitation{}, err
	}
	payload, _ := json.Marshal(map[string]string{"delivery_id": deliveryID, "company_id": companyID})
	_, err = tx.Exec(ctx, `INSERT INTO app.outbox_events (company_id,event_type,payload,idempotency_key) VALUES ($1::uuid,'email.delivery',$2::jsonb,$3)`, companyID, payload, "email.delivery:"+deliveryID)
	return i, err
}
