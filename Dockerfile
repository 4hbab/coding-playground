# syntax=docker/dockerfile:1

# Base images are pinned by digest (tag in the name) so every build uses exactly
# the same image. Versions follow the 7-day upgrade cooldown.

# ---- Build: compile a static binary ----
FROM golang:1.25.14-alpine3.24@sha256:1ae0735f00daffa3aaf1363a5184c0d2dc55c78e3db4ec70241cdac97bf84b59 AS build
WORKDIR /src

# Dependencies first, so this layer is cached until go.mod/go.sum change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal
# The SQLite driver is pure Go, so CGO can be off and the binary has no libc dependency
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/playground ./cmd/server

# ---- Run: small image with the binary, templates and static files ----
FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN addgroup -S app && adduser -S -G app -h /app app \
    && mkdir -p /data && chown app:app /data

WORKDIR /app
COPY --from=build /out/playground ./playground
COPY web ./web

ENV PORT=8080 \
    DB_PATH=/data/playground.db \
    LOG_LEVEL=info \
    LOG_FORMAT=json

USER app
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=60s \
    CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/playground"]
