package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RotateSession replaces the bearer credential without extending the original
// absolute expiry. A conditional update makes concurrent refreshes single-use.
func (s *AuthService) RotateSession(ctx context.Context, session Session) (string, time.Time, error) {
	if _, _, err := s.config(); err != nil {
		return "", time.Time{}, err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	var expiry time.Time
	err := s.Store.pool.QueryRow(ctx, `UPDATE app.auth_sessions AS s SET token_hash=$1, last_seen_at=now()
 WHERE s.id=$2::uuid AND s.user_id=$3::uuid AND s.token_hash=$4
 AND s.revoked_at IS NULL AND s.expires_at>now() AND s.absolute_expires_at>now()
 AND s.auth_state='active'
 AND s.security_version=(SELECT u.security_version FROM app.users u WHERE u.id=s.user_id AND u.status='active')
 RETURNING s.expires_at`, sessionTokenHash(token), session.ID, session.UserID, session.TokenHash).Scan(&expiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, ErrUnauthenticated
	}
	if err != nil {
		return "", time.Time{}, fmt.Errorf("rotate session: %w", err)
	}
	return token, expiry, nil
}
