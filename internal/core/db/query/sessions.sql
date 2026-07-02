-- name: CreateSession :one
INSERT INTO sessions (id, token_hash, user_id, created_at, expires_at)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT sqlc.embed(sessions), sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = ?;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at < ?;
