-- name: CreateAPIToken :one
INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListAPITokensByUser :many
SELECT * FROM api_tokens
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: GetAPITokenByHash :one
SELECT sqlc.embed(api_tokens), sqlc.embed(users)
FROM api_tokens
JOIN users ON users.id = api_tokens.user_id
WHERE api_tokens.token_hash = ?;

-- name: DeleteAPIToken :execrows
DELETE FROM api_tokens WHERE id = ? AND user_id = ?;
