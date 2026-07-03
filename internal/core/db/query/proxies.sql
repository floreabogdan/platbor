-- name: CreateProxy :exec
INSERT INTO registry_proxies (project_id, upstream_url, username, password, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetProxyByProjectID :one
SELECT * FROM registry_proxies WHERE project_id = ?;

-- name: ListProxies :many
SELECT * FROM registry_proxies;
