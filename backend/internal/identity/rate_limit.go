package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthRateLimiter stores fixed-window counters in PostgreSQL so limits are
// shared by every API instance. Keys are HMACed before persistence.
type AuthRateLimiter struct {
	pool *pgxpool.Pool
	key  []byte
}

func NewAuthRateLimiter(pool *pgxpool.Pool, key []byte) (*AuthRateLimiter, error) {
	if pool == nil {
		return nil, errors.New("PostgreSQL pool is required")
	}
	return &AuthRateLimiter{pool: pool, key: append([]byte(nil), key...)}, nil
}

func (l *AuthRateLimiter) keyHash(value string) string {
	if len(l.key) > 0 {
		mac := hmac.New(sha256.New, l.key)
		_, _ = mac.Write([]byte(value))
		return hex.EncodeToString(mac.Sum(nil))
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (l *AuthRateLimiter) Allow(ctx context.Context, scope, key string, limit int, window time.Duration, now time.Time) (bool, time.Duration, error) {
	if l == nil || l.pool == nil || scope == "" || key == "" || limit < 1 || window <= 0 {
		return false, 0, errors.New("invalid rate limit request")
	}
	windowStart := now.UTC().Truncate(window)
	var attempts int
	err := l.pool.QueryRow(ctx, `
		INSERT INTO app.auth_rate_limits (scope, key_hash, window_start, attempts)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (scope, key_hash, window_start)
		DO UPDATE SET attempts = app.auth_rate_limits.attempts + 1
		RETURNING attempts`, scope, l.keyHash(key), windowStart).Scan(&attempts)
	if err != nil {
		return false, 0, fmt.Errorf("increment auth rate limit: %w", err)
	}
	if attempts <= limit {
		_, _ = l.pool.Exec(ctx, `DELETE FROM app.auth_rate_limits WHERE window_start < $1`,
			now.UTC().Add(-24*time.Hour).Truncate(window))
		return true, 0, nil
	}
	retry := windowStart.Add(window).Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return false, retry, nil
}
