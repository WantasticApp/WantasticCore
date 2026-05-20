<div align="center">

# Wantastic

**A self-hosted WireGuard mesh with a browser admin portal, all in one container.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg?style=flat-square)](LICENSE)
[![Release](https://img.shields.io/github/v/release/WantasticApp/WantasticCore?style=flat-square&logo=github)](https://github.com/WantasticApp/WantasticCore/releases)
[![Docker Image](https://img.shields.io/badge/ghcr.io-wantastic-2496ED?style=flat-square&logo=docker)](https://github.com/WantasticApp/WantasticCore/pkgs/container/wantastic)
[![Discord](https://img.shields.io/badge/Discord-join-5865F2?style=flat-square&logo=discord&logoColor=white)](https://discord.gg/jVVX68F6b)
[![Live Demo](https://img.shields.io/badge/demo-console.wantastic.app-7c6af7?style=flat-square&logo=vercel&logoColor=white)](https://console.wantastic.app)

[**Try the demo →**](https://console.wantastic.app)  ·  [**Join Discord →**](https://discord.gg/jVVX68F6b)

</div>

---

## Why Wantastic

The overlay-VPN space is crowded — Tailscale, Headscale, NetBird,
wg-easy. They all stop at "your devices can reach each other." Wantastic
keeps going.

**1. You actually *work* on the devices from the browser.** No other
self-hosted overlay ships:

- **Winbox proxy with credential re-encryption** — manage MikroTik
  routers in your browser. The real device password never leaves the
  server (ECSRP-5 bridge in the middle), so handing out access doesn't
  mean handing out the keys.
- **WebSSH over the overlay** — terminal in a tab, traffic routed
  through the WireGuard tunnel. No SSH client to install per laptop.
- **WebProxy to any peer's HTTP/HTTPS** — open a printer's admin page,
  a NAS dashboard, or a router LAN-only UI from the portal, without
  port-forwards or split DNS.

**2. One container, zero glue work.** Postgres, Redis, nginx, certbot,
iptables, and the app all run together under `s6-overlay`. Tailscale
needs their SaaS; Headscale needs you to wire up the UI, certs, and
database yourself. `docker run` here, you're done.

**3. A web wizard that finishes the deploy.** Other tools give you a
binary. Wantastic gives you a form: domain, admin, Let's Encrypt email,
submit. It issues the cert, writes the nginx config, applies the
firewall, and prints the DNS records. First-run takes about a minute.

**4. An in-portal AI assistant that can *act*.** Copilot (Claude-backed,
your API key) has scoped tool calls — "create a tenant", "ping the
office router", "show me last hour's traffic" — gated by role. It's not
a chatbot bolted on; it touches the same in-process services the UI does.

Multi-tenant subnet isolation, TOTP/WhatsApp 2FA, OAuth2 device flow,
admin-managed accounts (no public sign-up) round it out.

## Quick start

```bash
docker run -d --name wantastic \
  --cap-add NET_ADMIN --restart unless-stopped \
  -p 80:80 -p 443:443 -p 8291:8291 -p 51820:51820/udp \
  -v wantastic-data:/var/lib/wantastic \
  ghcr.io/wantastic-app/wantastic:latest
```

Then open `https://<host>/` — the setup wizard takes you through domain,
admin account, and optional Let's Encrypt in about a minute.

## Watch it work

<details open>
<summary><b>Add a peer with the native WireGuard client</b></summary>

https://github.com/WantasticApp/WantasticCore/raw/main/docs/guide/add-with-native-wg-client.mp4

</details>

<details>
<summary><b>WUSP in action</b> — live device push, port scan, browser SSH</summary>

https://github.com/WantasticApp/WantasticCore/raw/main/docs/guide/wusp.mp4

</details>

## Build from source

```bash
make build && ./bin/wantastic-core
```

Docs in [`docker/README.md`](docker/README.md) for container internals,
[`docs/`](docs/) for protocol notes.

## Contributing

Issues and PRs welcome. For anything bigger than a typo, open an issue
first so we can talk through the approach.

## License

MIT — see [LICENSE](LICENSE).

---

<div align="center">

If Wantastic solves a problem for you, **leave a star** ⭐ — it's how we
decide what to build next.

</div>
