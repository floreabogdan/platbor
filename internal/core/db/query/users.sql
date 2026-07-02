-- name: CreateUser :one
INSERT INTO users (id, username, email, password_hash, is_admin, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ?;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;
