#!/usr/bin/env bash
#
# Wantastic one-command installer.
#
# Pulls the all-in-one image (postgres + redis + nginx + Let's Encrypt +
# WireGuard core) and starts it. On first boot the container exposes a
# web setup wizard on :443 (self-signed cert) so you can configure the
# domain, super-admin, Let's Encrypt email, and toggle the firewall —
# all from a browser.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/WantasticApp/WantasticCore/main/scripts/install.sh | sudo bash
# or, if you've cloned the repo:
#   sudo ./scripts/install.sh
#
# Optional environment variables:
#   WANTASTIC_IMAGE       image to pull (default: ghcr.io/wantastic-app/wantastic:latest)
#   WANTASTIC_VOLUME      named docker volume for persisted data (default: wantastic-data)
#   WANTASTIC_NAME        container name (default: wantastic)

set -euo pipefail

readonly IMAGE="${WANTASTIC_IMAGE:-ghcr.io/wantastic-app/wantastic:latest}"
readonly VOLUME="${WANTASTIC_VOLUME:-wantastic-data}"
readonly NAME="${WANTASTIC_NAME:-wantastic}"

LOG_PREFIX="\033[1;32m[wantastic]\033[0m"
log()  { printf "%b %s\n" "$LOG_PREFIX" "$*"; }
die()  { printf "\033[1;31m[wantastic] ERROR:\033[0m %s\n" "$*" >&2; exit 1; }

require_root() {
    [ "$(id -u)" -eq 0 ] || die "Run as root (sudo bash) — needed to install Docker and bind :443."
}

ensure_docker() {
    if command -v docker >/dev/null 2>&1; then
        log "Docker already installed."
        return
    fi
    log "Installing Docker via the official convenience script..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker || true
}

stop_existing() {
    if docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
        log "Stopping existing '$NAME' container..."
        docker rm -f "$NAME" >/dev/null
    fi
}

pull_and_run() {
    log "Pulling $IMAGE ..."
    docker pull "$IMAGE"

    log "Starting container '$NAME' (data volume: $VOLUME)..."
    docker run -d \
        --name "$NAME" \
        --restart unless-stopped \
        --cap-add NET_ADMIN \
        -p 80:80 \
        -p 443:443 \
        -p 8291:8291 \
        -p 51820:51820/udp \
        -v "$VOLUME":/var/lib/wantastic \
        "$IMAGE" >/dev/null
}

print_done() {
    local ip
    ip="$(curl -fsSL --max-time 3 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')"
    cat <<EOF

  ╔════════════════════════════════════════════════════════════════╗
  ║                  Wantastic is running                          ║
  ╠════════════════════════════════════════════════════════════════╣
  ║                                                                ║
  ║   1) Open in your browser:                                     ║
  ║         https://${ip}/                                         ║
  ║      Browser will warn — the cert is self-signed until         ║
  ║      Let's Encrypt issues for your domain.                     ║
  ║                                                                ║
  ║   2) Fill in the setup form:                                   ║
  ║         - domain (e.g. example.com)                            ║
  ║         - super-admin email + password                         ║
  ║         - Let's Encrypt email (enables auto-issue + renew)     ║
  ║         - firewall toggle (default: on)                        ║
  ║         - optional SMTP + Anthropic API key                    ║
  ║                                                                ║
  ║   3) Add these DNS A records → ${ip}:                          ║
  ║         <console-host>.<domain>                                ║
  ║         winbox.<domain>                                        ║
  ║         wg.<domain>                                            ║
  ║                                                                ║
  ║   Day-2 ops:                                                   ║
  ║      docker logs -f $NAME                                      ║
  ║      docker exec -it $NAME /bin/bash                           ║
  ║      docker pull $IMAGE && docker rm -f $NAME && <re-run>      ║
  ║                                                                ║
  ╚════════════════════════════════════════════════════════════════╝

EOF
}

main() {
    require_root
    ensure_docker
    stop_existing
    pull_and_run
    print_done
}

main "$@"
