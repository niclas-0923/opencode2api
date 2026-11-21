FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 \
    GOOS="${TARGETOS:-linux}" \
    GOARCH="${TARGETARCH:-amd64}" \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/opencode2api ./cmd/opencode2api

FROM alpine:3.22

RUN apk add --no-cache ca-certificates su-exec tzdata \
    && addgroup -S opencode2api \
    && adduser -S -G opencode2api -h /var/lib/opencode2api opencode2api \
    && mkdir -p /app /var/lib/opencode2api \
    && chown -R opencode2api:opencode2api /app /var/lib/opencode2api

COPY --from=builder /out/opencode2api /usr/local/bin/opencode2api
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/docker-entrypoint
COPY --chown=opencode2api:opencode2api config.example.json /app/config.example.json
COPY LICENSE /usr/share/licenses/opencode2api/LICENSE
LABEL org.opencontainers.image.licenses="GPL-3.0-or-later"

# LISTEN_ADDRESS is intentionally unset: config.json is authoritative. Export
# it to override the configured listen address.
ENV CONFIG_PATH=/var/lib/opencode2api/config.json \
    CONFIG_SEED_PATH= \
    LISTEN_ADDRESS= \
    STATE_DIR=/var/lib/opencode2api

WORKDIR /app

EXPOSE 8080

# Probe the address the entrypoint was told to bind. When LISTEN_ADDRESS is
# unset, config.json is authoritative and the seeded config binds 0.0.0.0:8080,
# so the probe uses loopback; override with LISTEN_ADDRESS when the file binds
# another port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD probe="${LISTEN_ADDRESS:-127.0.0.1:8080}"; \
        case "$probe" in \
            0.0.0.0:*) probe="127.0.0.1:${probe#0.0.0.0:}" ;; \
            \[::\]:*) probe="[::1]:${probe#\[::\]:}" ;; \
            :*) probe="127.0.0.1${probe}" ;; \
        esac; \
        wget -q -O /dev/null "http://${probe}/healthz" || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
