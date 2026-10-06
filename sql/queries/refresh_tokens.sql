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
