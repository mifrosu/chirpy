-- name: CreateRefreshToken :one
-- expires_at is computed in SQL so it uses the same clock and timezone as
-- created_at (and as any later comparison against NOW()).
INSERT INTO refresh_tokens (token, created_at, updated_at, user_id, expires_at)
VALUES (
    $1,
    NOW(),
    NOW(),
    $2,
    NOW() + INTERVAL '60 days'
)
RETURNING *;

-- name: GetUserFromRefreshToken :one
-- Returns the token's user only while the token is neither revoked nor expired.
SELECT users.*
FROM users
JOIN refresh_tokens ON refresh_tokens.user_id = users.id
WHERE refresh_tokens.token = $1
  AND refresh_tokens.revoked_at IS NULL
  AND refresh_tokens.expires_at > NOW();

-- name: RevokeRefreshToken :exec
-- An already-revoked token is left alone so its original revoked_at is kept.
UPDATE refresh_tokens
SET revoked_at = NOW(), updated_at = NOW()
WHERE token = $1 AND revoked_at IS NULL;
