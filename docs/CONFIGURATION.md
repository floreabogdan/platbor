# Configuration

Platbor is configured from three sources, in increasing precedence:

1. **Built-in defaults** — a working, zero-config single-binary instance.
2. **A YAML file** — optional, passed with `--config /path/to/platbor.yaml`.
3. **Environment variables** — `PLATBOR_*`, highest precedence.

Zero config is a supported mode: with no file and no env, Platbor stores
everything under `./platbor-data`, listens on `:8080`, uses SQLite and the
filesystem blob store, and prints a generated admin password once at first start.
Set only what you need to change.

A commented starting point lives at
[`configs/platbor.example.yaml`](../configs/platbor.example.yaml).

## Reference

Every key below shows its YAML path, its `PLATBOR_*` environment variable, and its
default. An empty environment variable is treated as unset (it does not blank out
a real default).

### Server

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `addr` | `PLATBOR_ADDR` | `:8080` | `host:port` to listen on. |
| `dataDir` | `PLATBOR_DATA_DIR` | `platbor-data` | Holds the SQLite DB and, for the `fs` blob driver, the blobs. In-progress uploads always stage here (`{dataDir}/uploads`). |
| `shutdownTimeout` | `PLATBOR_SHUTDOWN_TIMEOUT` | `15s` | Grace window for draining in-flight requests on shutdown. |

### Database

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `database.driver` | `PLATBOR_DB_DRIVER` | `sqlite` | `sqlite` (zero-config) or `postgres`. |
| `database.dsn` | `PLATBOR_DB_DSN` | *(empty)* | SQLite uses `{dataDir}/platbor.db` when empty; `postgres` requires an explicit DSN. |

### Blob store

The content-addressable store for all binary content (image layers, package
tarballs, module archives). `fs` is the zero-config default; `s3` targets any
S3-compatible object store (AWS S3, MinIO, Cloudflare R2) and is the right choice
for large artifacts.

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `blob.driver` | `PLATBOR_BLOB_DRIVER` | `fs` | `fs` or `s3`. |
| `blob.s3.endpoint` | `PLATBOR_S3_ENDPOINT` | *(empty)* | `host[:port]` of the S3 API. Required for `s3`. |
| `blob.s3.bucket` | `PLATBOR_S3_BUCKET` | *(empty)* | Bucket for blobs; created on first run if absent and permitted. Required for `s3`. |
| `blob.s3.region` | `PLATBOR_S3_REGION` | *(empty)* | Region (optional for MinIO). |
| `blob.s3.accessKeyId` | `PLATBOR_S3_ACCESS_KEY_ID` | *(empty)* | Access key. When both key and secret are empty, ambient credentials (env, IAM role) are used. |
| `blob.s3.secretAccessKey` | `PLATBOR_S3_SECRET_ACCESS_KEY` | *(empty)* | Secret key. |
| `blob.s3.useSSL` | `PLATBOR_S3_USE_SSL` | `false` | HTTPS to the endpoint. |
| `blob.s3.prefix` | `PLATBOR_S3_PREFIX` | *(empty)* | Optional key prefix within the bucket. |

**Note on multi-node deploys:** committed blobs live in the shared bucket, but an
in-progress *resumable* upload stages on local disk, so it must be resumed on the
node that started it. Put resumable-upload clients behind sticky sessions, or
accept that a mid-upload node change restarts that upload.

### Maintenance (optional scheduled jobs)

Both are **off by default** — Platbor never deletes on a timer unless you ask it
to — and both remain available on demand via the admin API (`POST
/api/v1/registry/gc`, `POST /api/v1/registry/retention`) regardless. When several
instances share one backend, enable these on a **single** instance to avoid
redundant concurrent runs.

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `maintenance.gcInterval` | `PLATBOR_GC_INTERVAL` | `0` (disabled) | Interval for automatic garbage collection (unreferenced-blob sweep). Must be ≥ `1m` when set. |
| `maintenance.retentionInterval` | `PLATBOR_RETENTION_INTERVAL` | `0` (disabled) | Interval for automatic retention (keep-last-N pruning). Must be ≥ `1m` when set. |

### Auth

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `auth.adminUsername` | `PLATBOR_ADMIN_USERNAME` | `admin` | Instance admin created on first run. |
| `auth.adminPassword` | `PLATBOR_ADMIN_PASSWORD` | *(empty)* | First-run admin password. When empty, a random one is generated and logged **once** at startup. |
| `auth.cookieSecure` | `PLATBOR_COOKIE_SECURE` | `false` | Set the `Secure` flag on the session cookie. Enable when serving over HTTPS (directly or behind a TLS-terminating proxy). |
| `auth.ociBearer` | `PLATBOR_OCI_BEARER` | `false` | Opt into the OCI bearer-token flow (`/v2` challenges toward `/v2/token`). HTTP Basic (password or PAT) keeps working either way. |

### Logging

| YAML | Env | Default | Notes |
|------|-----|---------|-------|
| `log.level` | `PLATBOR_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `log.format` | `PLATBOR_LOG_FORMAT` | `text` | `text` (human) or `json` (log pipelines). |

## Health & readiness

Platbor exposes two unauthenticated operational endpoints for orchestrators:

- `GET /healthz` — **liveness**: 200 as long as the process can serve. It checks
  no dependencies, so a transient database blip never triggers a restart loop.
- `GET /readyz` — **readiness**: probes the metadata database and the blob store
  and returns `200 {"status":"ready", ...}` when both are reachable, or
  `503 {"status":"unready", "checks":{...}}` when either is not — so a load
  balancer stops routing to an instance that cannot serve.

## Examples

**Zero config** (dev / small self-host):

```bash
platbor
```

**S3-backed, HTTPS-fronted, with nightly maintenance** (env form):

```bash
PLATBOR_ADDR=:8080 \
PLATBOR_COOKIE_SECURE=true \
PLATBOR_BLOB_DRIVER=s3 \
PLATBOR_S3_ENDPOINT=s3.amazonaws.com \
PLATBOR_S3_BUCKET=platbor-blobs \
PLATBOR_S3_REGION=us-east-1 \
PLATBOR_S3_ACCESS_KEY_ID=... \
PLATBOR_S3_SECRET_ACCESS_KEY=... \
PLATBOR_S3_USE_SSL=true \
PLATBOR_GC_INTERVAL=24h \
PLATBOR_RETENTION_INTERVAL=24h \
platbor
```

**Postgres + MinIO** via a config file (`platbor --config platbor.yaml`): see
[`configs/platbor.example.yaml`](../configs/platbor.example.yaml).
