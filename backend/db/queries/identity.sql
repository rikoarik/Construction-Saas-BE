-- name: GetUserByNormalizedEmail :one
SELECT id, email, email_normalized, password_hash, status, created_at, updated_at
FROM app.users
WHERE email_normalized = $1;

-- name: CreateUser :one
INSERT INTO app.users (email, email_normalized, password_hash, status)
VALUES ($1, $2, $3, $4)
RETURNING id, email, email_normalized, password_hash, status, created_at, updated_at;

-- name: CreateAuthSession :one
INSERT INTO app.auth_sessions (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at;

-- name: GetActiveAuthSessionByTokenHash :one
SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
FROM app.auth_sessions
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: RevokeAuthSession :execrows
UPDATE app.auth_sessions
SET revoked_at = now()
WHERE id = $1
  AND user_id = $2
  AND revoked_at IS NULL;
