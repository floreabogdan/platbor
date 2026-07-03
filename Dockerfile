# Platbor — single-binary developer platform.
#
# Three stages: build the SPA, compile a static binary with the SPA embedded,
# then ship it on a minimal distroless base. The result is one self-contained
# image with no runtime dependencies — SQLite is pure Go (modernc.org/sqlite),
# so CGO stays off and the binary is fully static.
#
#   docker build -t platbor .
#   docker run --rm -p 8080:8080 -v platbor-data:/data platbor

# --- Stage 1: build the React SPA into web/dist ---
FROM node:22-alpine AS web
WORKDIR /web
# Install against the lockfile first so this layer caches until deps change.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Stage 2: compile the static Go binary with the SPA embedded ---
FROM golang:1.26-alpine AS build
WORKDIR /src
# Download modules first for layer caching, then bring in the source.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The committed web/dist placeholder is ignored (.dockerignore); drop in the
# freshly built SPA so //go:embed all:dist picks up the real assets.
COPY --from=web /web/dist ./web/dist
# Create the data directory here so we can hand it to the scratch-like runtime
# already owned by the non-root user (distroless has no shell to mkdir/chown).
RUN mkdir -p /data
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/platbor ./cmd/platbor

# --- Stage 3: minimal runtime ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/platbor /platbor
COPY --from=build --chown=65532:65532 /data /data
# Zero-config defaults: listen on :8080, persist under the mounted volume.
ENV PLATBOR_ADDR=:8080 \
    PLATBOR_DATA_DIR=/data
EXPOSE 8080
VOLUME ["/data"]
# 65532 is distroless's non-root user; it owns /data so SQLite and the blob
# store can write. Liveness/readiness are HTTP probes (/healthz, /readyz) —
# distroless ships no shell, so there is no in-container HEALTHCHECK.
USER 65532:65532
ENTRYPOINT ["/platbor"]
