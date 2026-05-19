<div align="center">

# Wantastic

**Self-hosted, multi-tenant WireGuard zero-trust mesh — admin portal, browser SSH, Winbox proxy, and an optional AI assistant, all in one container.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/WantasticApp/WantasticCore?style=flat-square&logo=go)](go.mod)
[![Release](https://img.shields.io/github/v/release/WantasticApp/WantasticCore?style=flat-square&logo=github)](https://github.com/WantasticApp/WantasticCore/releases)
[![Docker Image](https://img.shields.io/badge/ghcr.io-wantastic-2496ED?style=flat-square&logo=docker)](https://github.com/WantasticApp/WantasticCore/pkgs/container/wantastic)
[![GitHub Stars](https://img.shields.io/github/stars/WantasticApp/WantasticCore?style=flat-square&logo=github)](https://github.com/WantasticApp/WantasticCore/stargazers)
[![CI](https://img.shields.io/github/actions/workflow/status/WantasticApp/WantasticCore/release.yaml?style=flat-square&logo=github)](https://github.com/WantasticApp/WantasticCore/actions)

[**One-command install →**](#one-command-install) · [**Run with Docker →**](#run-with-docker) · [**Build from source →**](#build-from-source)

</div>

---

## What it is

Wantastic is a **WireGuard-based zero-trust overlay** for teams, labs, and
homelab fleets. Each tenant gets an isolated `/24` (or larger) WireGuard
network with admin-managed device caps. The whole stack — including
PostgreSQL, Redis, nginx with auto Let's Encrypt, and an iptables firewall
— ships as **one Docker image**.

- **Web admin portal** with peer management, live topology, browser SSH.
- **Winbox proxy** to reach Mikrotik routers over WireGuard without leaking real credentials.
- **WebProxy** for HTTP/HTTPS access to tenant peers from the portal.
- **Optional WhatsApp adminbot** + in-portal **Copilot** (Claude-backed).
- **One Go process** for the application code — no internal RPC.
- **One container** for the whole deploy — postgres, redis, nginx, certbot, firewall, and the core run side-by-side under `s6-overlay`.

```
┌──────────────────────── one container ────────────────────────┐
│                                                                │
│  s6-overlay (PID 1)                                            │
│    ├── postgres ──┐                                            │
│    ├── redis   ──┼─── wantastic-core (portal + adminbot)       │
│    │             │      :8001 HTTP   :8443 setup wizard        │
│    │             │      :8291 Winbox :51820/udp WireGuard      │
│    │             │                                             │
│    ├── nginx ────┴── :80 (ACME + redirect)  :443 (portal TLS)  │
│    ├── certbot-renew (12h loop, Let's Encrypt webroot)         │
│    └── firewall (iptables default-deny, allow 80/443/8291/wg)  │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

## Features

- **One-command install** — `curl | sudo bash` pulls the image and starts it.
- **Web-based first-run setup** — open a browser, fill the form, done. The wizard issues the Let's Encrypt cert, writes the nginx config, applies the firewall, and writes `config.yaml` — no SSH gymnastics.
- **Multi-tenant** — each tenant gets an isolated WireGuard subnet, one shared UDP port via `SO_REUSEPORT`.
- **Admin-managed accounts** — no public sign-up; super-admins create tenants and set device caps.
- **WebSSH** — browser-based SSH terminal multiplexed over a single WireGuard tunnel per target.
- **Winbox proxy** — ECSRP-5 re-encrypting bridge so real Mikrotik passwords never leave the portal.
- **Copilot** — role-aware in-portal assistant with scoped tool calls (Claude-backed; bring your own API key).
- **2FA** — TOTP and optional WhatsApp 2FA.
- **OAuth2 Device Authorization Grant** — Tailscale-style device login flow for agents.
- **Embedded UI** — Svelte portal compiled into the binary.
- **Auto-renewing TLS** — certbot wakes every 12h and rolls the cert through the LE webroot, then reloads nginx.
- **Default-deny firewall** — iptables ruleset baked into the image; toggle from the setup form.

## Guides

Short, narration-free walkthroughs of the two flows users hit most often.
GitHub renders the MP4s inline below; raw copies live in
[`docs/guide/`](docs/guide).

<details open>
<summary><b>Add a peer with the native WireGuard client</b> — generate a config, scan the QR, ship.</summary>

https://github.com/WantasticApp/WantasticCore/raw/main/docs/guide/add-with-native-wg-client.mp4

</details>

<details>
<summary><b>WUSP in action</b> — live device push, port scan, browser SSH over the overlay.</summary>

https://github.com/WantasticApp/WantasticCore/raw/main/docs/guide/wusp.mp4

</details>

## One-command install

Fresh Ubuntu / Debian / Rocky / Alma VM with root access:

```bash
curl -fsSL https://raw.githubusercontent.com/WantasticApp/WantasticCore/main/scripts/install.sh | sudo bash
```

What that does:

1. Installs Docker (via `get.docker.com`) if not already present.
2. Pulls `ghcr.io/wantastic-app/wantastic:latest`.
3. Runs the container with `NET_ADMIN`, ports `80/443/8291/51820`, and a `wantastic-data` volume for persistence.
4. Prints the URL to visit in your browser.

On first boot the container has no `config.yaml`. nginx terminates `:443`
with a self-signed cert and proxies to the **web setup wizard** that asks:

- **Domain** + console hostname.
- **Super-admin email + password**.
- **Let's Encrypt email** — supplying this triggers an immediate cert issuance and turns on the 12h auto-renew.
- **Firewall toggle** — default-deny iptables; allows only `80/443/8291/tcp` and `51820/udp`.
- Optional **SMTP** + **Anthropic API key**.

After submit, the wizard writes config, issues the cert, applies the
firewall, reloads nginx, and prints the DNS records you need to add.

## Run with Docker

If you'd rather drive Docker yourself:

```bash
docker run -d --name wantastic \
  --cap-add NET_ADMIN \
  --restart unless-stopped \
  -p 80:80 -p 443:443 -p 8291:8291 -p 51820:51820/udp \
  -v wantastic-data:/var/lib/wantastic \
  ghcr.io/wantastic-app/wantastic:latest
```

Then open `https://<host>/` for the setup wizard. Everything (postgres,
redis, nginx, the core) runs inside this one container; persisted data
lives under `/var/lib/wantastic` in the named volume.

Full reference: [`docker/README.md`](docker/README.md).

## Build from source

```bash
make build                                    # bin/wantastic-core
WANTASTIC_WEB_SETUP=1 ./bin/wantastic-core    # web wizard on :8443
# or
./bin/wantastic-core                          # CLI wizard (interactive TTY)
```

Three setup paths share the same code in `internal/config` + `internal/setupweb`:

1. **Web wizard** (default in the container) — HTTPS form at `:8443`, generates secrets, configures nginx + LE + firewall, prints DNS records.
2. **CLI wizard** (default when stdin is a TTY).
3. **Env-driven** (`WANTASTIC_SETUP_NONINTERACTIVE=1`) — every value read from `WANTASTIC_*` vars; used by the one-command installer.

All three write `./config.yaml` (mode `0600`) with auto-generated
`tenant.session_signing_key` and `hooks.secret_key`.

### Useful make targets

```bash
make build          # dev binary
make build-prod     # stripped optimized binary
make test           # unit tests
make vet            # go vet
make vulncheck      # govulncheck (install: go install golang.org/x/vuln/cmd/govulncheck@latest)
make fmt            # go fmt
make image          # build the all-in-one container image
make image-run      # run it locally
make image-logs     # tail container logs
make image-shell    # exec into the container
make image-stop     # stop + remove
```

### Frontend

The Svelte portal is embedded into the binary via `//go:embed` from
`internal/portalsrv/app/dist/`. To rebuild after changing Svelte sources:

```bash
cd internal/portalsrv/app
pnpm install
pnpm run build
cd ../../..
make build
```

## Security

- **Vulnerability scanning** is part of the workflow — run `make vulncheck`
  before every release. The repo's `go.mod` pins `golang.org/x/net ≥ 0.53.0`
  and `golang.org/x/image ≥ 0.38.0` to cover the known HTTP/2 and TIFF
  CVEs as of the last release. The image is rebuilt against the latest
  Go 1.25 patch on every release tag.
- **TLS** is terminated by nginx using a Let's Encrypt cert issued and
  renewed by certbot inside the container.
- **Firewall** is iptables default-deny INPUT; opt out with
  `-e WANTASTIC_FIREWALL=0` or by unchecking the toggle in the setup form.
- **No public sign-up**: tenants are admin-created only.
- **Admin-bound 2FA** is optional but recommended (TOTP).

## What's running, where (inside the container)

| Component       | Port (inside)  | Notes                                                 |
|-----------------|----------------|-------------------------------------------------------|
| nginx           | `:80`, `:443`  | ACME + portal TLS termination, WS upgrade aware.      |
| wantastic-core  | `:8001`, `:8443` | Portal HTTP + first-run wizard (HTTPS, self-signed). |
| WebSSH          | `:8081`        | Proxied through nginx on `:443`.                      |
| Winbox mux      | `:8291`        | Exposed directly (binary protocol; nginx can't help). |
| WireGuard       | `:51820/udp`   | Shared UDP, SO_REUSEPORT across tenants.              |
| PostgreSQL      | `:5432`        | Loopback only; password is generated on first boot.   |
| Redis           | `:6379`        | Loopback only.                                        |
| Prometheus      | `:9091`        | Loopback only; expose via `-p 127.0.0.1:9091:9091`.   |

Persistent state lives under `/var/lib/wantastic` (mount this as a named
volume). The all-in-one image runs **no gRPC** — the portal and adminbot
dispatch in-process.

## Project status

| Surface | Status |
|---------|--------|
| Core (WireGuard userspace, multi-tenant) | ✅ Stable |
| Web portal (peer mgmt, topology, SSH, Winbox) | ✅ Stable |
| One-command installer + web setup wizard | ✅ Stable |
| Auto Let's Encrypt + auto-renew | ✅ Stable |
| In-container firewall (iptables) | ✅ Stable |
| Admin tenant management (CRUD + role gating) | ✅ Stable |
| Copilot (Claude-backed assistant) | 🧪 Beta — requires Anthropic API key |
| AdminBot (WhatsApp) | 🧪 Beta — opt-in via config |
| Multi-instance HA | ❌ Not supported (intentional — single-process design) |

## Contributing

Issues, discussions, and PRs welcome. Before you start on something larger
than a typo fix, please open an issue first so we can confirm the direction.

```bash
git clone https://github.com/WantasticApp/WantasticCore.git
cd WantasticCore
make build && make test
```

Opinions baked in:

- **One binary**. Portal and adminbot are in-process packages, not separate services. Don't add internal RPC.
- **One container**. Postgres, redis, nginx, certbot, the firewall, and the core ship together — no external dependencies in the hot path.
- **Admin-managed tenants only**. Public sign-up was intentionally removed; new deploys provision tenants from the portal or the Copilot's `create_tenant` tool.

## License

MIT — see [LICENSE](LICENSE).

## Star history

[![Star History Chart](https://api.star-history.com/svg?repos=WantasticApp/WantasticCore&type=Date)](https://star-history.com/#WantasticApp/WantasticCore&Date)

---

<div align="center">

If Wantastic solves a real problem for you, **leave a star**. It's the
clearest signal we can use to prioritize what to build next.

</div>
