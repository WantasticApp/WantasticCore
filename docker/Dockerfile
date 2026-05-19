# syntax=docker/dockerfile:1.7
#
# Wantastic — all-in-one image.
#
#   Builds a single container that runs postgres + redis + wantastic-core +
#   nginx (reverse-proxy with Let's Encrypt) + an iptables firewall. The
#   setup wizard on first boot configures all of them; data persists in
#   /var/lib/wantastic (mount this as a volume).
#
# Quick start:
#   docker build -t wantastic:latest -f docker/Dockerfile .
#   docker run -d --name wantastic \
#     --cap-add NET_ADMIN \
#     -p 80:80 -p 443:443 -p 8291:8291 -p 51820:51820/udp \
#     -v wantastic-data:/var/lib/wantastic \
#     wantastic:latest
#   # Open https://<host>/  (self-signed cert until LE issues for your domain)

# ───────── Stage 1a: build the SPA (portal frontend) ─────────
# The Go binary embeds internal/portalsrv/app/dist/* via //go:embed. The
# dist tree is gitignored (it's a build artifact), so CI checkouts don't
# have it — we have to produce it here before `go build` runs.
#
# --platform=$BUILDPLATFORM pins this stage to the runner's native arch
# regardless of what we're targeting downstream. The SPA output is
# pure JS/CSS/HTML — platform-independent — so building it twice (once
# per target) under QEMU emulation would waste ~9 min per multi-arch run.
#
# Copying the whole app/ directory in one shot (instead of staging the
# lockfile separately) is required because the package ships a
# pnpm-workspace.yaml that uses pnpm 10's onlyBuiltDependencies setting;
# pnpm errors with "packages field missing or empty" if it sees that
# file without the rest of the project alongside it.
FROM --platform=$BUILDPLATFORM node:20-alpine AS spa
WORKDIR /src
# Match the local toolchain: the app's pnpm-workspace.yaml uses
# onlyBuiltDependencies (a pnpm 10 feature). pnpm 9 chokes on that file
# with "packages field missing or empty".
RUN corepack enable && corepack prepare pnpm@10.29.3 --activate
COPY internal/portalsrv/app/ ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile && pnpm build

# ───────── Stage 1b: build wantastic-core ─────────
# Same trick — run the compiler on the BUILDPLATFORM and cross-compile
# to TARGETPLATFORM via GOARCH/GOOS. CGO_ENABLED=0 means we don't need
# the target's libc, so cross-compilation is free. This replaces a
# QEMU-emulated `go build` (15–30 min for arm64 on an x86 runner) with
# a native one (≈2 min).
FROM --platform=$BUILDPLATFORM golang:alpine AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
# Drop in the freshly-built SPA so the //go:embed directive has files to read.
COPY --from=spa /src/dist/ ./internal/portalsrv/app/dist/
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags='-s -w' -o /out/wantastic-core ./cmd/wantastic-core

# ───────── Stage 2: runtime ─────────
# Alpine base — small footprint, all the daemons are first-class packages.
FROM alpine:3.20

LABEL org.opencontainers.image.title="wantastic" \
      org.opencontainers.image.source="https://github.com/wantastic-app/WantasticCore" \
      org.opencontainers.image.description="All-in-one Wantastic node: core + portal + adminbot + postgres + redis + nginx + LE."

# Preinstall every daemon and tool. Nothing else is installed at runtime —
# the image is fully self-contained.
RUN apk add --no-cache \
        s6-overlay \
        postgresql16 postgresql16-contrib postgresql16-client \
        redis \
        nginx \
        certbot certbot-nginx \
        openssl \
        iptables ip6tables \
        ca-certificates \
        tzdata \
        curl \
        bash \
        su-exec \
        tini \
    && rm -rf /var/cache/apk/*

# Persisted data lives here. Mount as a volume.
#   /var/lib/wantastic/config     → config.yaml + TLS certs
#   /var/lib/wantastic/postgres   → PG data directory
#   /var/lib/wantastic/redis      → redis AOF/RDB
#   /var/lib/wantastic/letsencrypt → certbot state
#   /var/lib/wantastic/logs       → s6 / nginx access logs
RUN mkdir -p \
        /var/lib/wantastic/config \
        /var/lib/wantastic/postgres \
        /var/lib/wantastic/redis \
        /var/lib/wantastic/letsencrypt \
        /var/lib/wantastic/logs \
    && chown -R postgres:postgres /var/lib/wantastic/postgres \
    && chown -R redis:redis /var/lib/wantastic/redis

# Copy the rootfs overlay (s6 services, nginx template, scripts).
COPY docker/rootfs/ /

# Make every shipped script executable.
RUN chmod +x /usr/local/bin/*.sh /etc/s6-overlay/s6-rc.d/*/run /etc/s6-overlay/s6-rc.d/*/up 2>/dev/null || true

# Drop in the wantastic-core binary.
COPY --from=builder /out/wantastic-core /usr/local/bin/wantastic-core

# Symlink the config volume into the path the binary expects.
RUN ln -sf /var/lib/wantastic/config /etc/wantastic

# Ports (all exposed via host firewall in the operator's network):
#   80   — HTTP (LE renewal + HTTPS redirect)
#   443  — HTTPS (portal + console + setup wizard)
#   8291 — Winbox multiplexer
#   51820/udp — WireGuard
EXPOSE 80 443 8291
EXPOSE 51820/udp

VOLUME ["/var/lib/wantastic"]

# s6-overlay takes PID 1. It supervises postgres → redis → wantastic-core
# (and nginx after the setup wizard has written /etc/nginx/conf.d/site.conf).
ENV S6_KEEP_ENV=1 \
    S6_BEHAVIOUR_IF_STAGE2_FAILS=2 \
    S6_VERBOSITY=1 \
    WANTASTIC_WEB_SETUP=1

ENTRYPOINT ["/init"]
