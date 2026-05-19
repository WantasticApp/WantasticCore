# Wantastic — all-in-one container

One image. Postgres, Redis, nginx (with auto Let's Encrypt + 12h renew),
the WireGuard core + portal + adminbot, and an iptables firewall — all
supervised by `s6-overlay`.

## Build

```bash
make image                   # → wantastic:latest
# or
docker build -t wantastic:latest -f docker/Dockerfile .
```

## Run

```bash
docker run -d --name wantastic \
  --cap-add NET_ADMIN \
  --restart unless-stopped \
  -p 80:80 -p 443:443 -p 8291:8291 -p 51820:51820/udp \
  -v wantastic-data:/var/lib/wantastic \
  wantastic:latest
```

Open `https://<host>/` to land on the setup wizard. Browser warning is
expected on first boot — nginx serves a self-signed cert until Let's
Encrypt issues for your domain.

## Setup wizard

Fields:

| Field | Notes |
|-------|-------|
| Domain | Base domain. DNS records are derived from this. |
| Console hostname | Usually `console.<domain>` or `<domain>` itself. |
| Winbox / WireGuard hostnames | Default to `winbox.<domain>` / `wg.<domain>`. |
| Let's Encrypt email | Filling this in triggers immediate cert issuance + turns on the 12h auto-renew loop. Leave blank to stay on the self-signed bootstrap cert. |
| Firewall toggle | Default-deny iptables ruleset. Allows only `80/443/8291/tcp` and `51820/udp`. Requires `--cap-add NET_ADMIN`. |
| Super-admin email + password | Required. Becomes the first login. |
| SMTP | Optional. Falls back to local sendmail then disk spool. |
| Anthropic API key | Optional. Powers the in-portal Copilot. |

After submit the wizard:

1. Writes `/etc/wantastic/config.yaml`.
2. Renders the production nginx config from the template.
3. Runs `certbot certonly --webroot` (if LE email supplied) and reloads nginx.
4. Applies the iptables firewall (if enabled).
5. Exits the wizard server — s6 restarts wantastic-core in normal mode.

## What lives where

| Inside container | Purpose |
|------------------|---------|
| `/var/lib/wantastic/config/` | `config.yaml`, generated PG password, bootstrap TLS keypair. Symlinked from `/etc/wantastic`. |
| `/var/lib/wantastic/postgres/` | PG data dir (uid `postgres`). |
| `/var/lib/wantastic/redis/` | Redis RDB/AOF. |
| `/var/lib/wantastic/letsencrypt/` | certbot state: live certs, accounts, webroot, renewal hooks. |
| `/var/lib/wantastic/logs/` | nginx + bootstrap logs. |
| `/etc/nginx/conf.d/site.conf` | Active site config — bootstrap until wizard runs, production after. |
| `/etc/nginx/templates/*.conf*` | Bootstrap + production templates (image-baked). |
| `/etc/s6-overlay/s6-rc.d/` | Service definitions for postgres, redis, nginx, wantastic-core, firewall, certbot-renew. |
| `/usr/local/bin/firewall-apply.sh` | Re-runnable iptables script. |
| `/usr/local/bin/nginx-render.sh` | Renders the production site config. |
| `/usr/local/bin/letsencrypt-issue.sh` | One-shot `certbot certonly --webroot`. |

## Environment knobs

| Variable | Default | Effect |
|----------|---------|--------|
| `WANTASTIC_FIREWALL` | `1` | `0` to skip the iptables apply (rely on host firewall). |
| `WANTASTIC_AUTORENEW` | `1` | `0` to disable the 12h certbot renew loop. |
| `WANTASTIC_WEB_SETUP` | `1` | The image always boots into the web wizard until `config.yaml` exists. |
| `WANTASTIC_DOMAIN` | — | Pre-fills the Domain field in the wizard. |
| `WANTASTIC_CONSOLE_HOST` | — | Pre-fills Console hostname. |
| `WANTASTIC_LE_EMAIL` | — | Pre-fills Let's Encrypt email. |
| `WANTASTIC_BOOTSTRAP_ADMIN_EMAIL` / `_NAME` / `_PASSWORD` | — | Pre-fill the super-admin fields. |
| `WANTASTIC_DB_HOST` / `_PORT` / `_USER` / `_NAME` / `_PASSWORD` | `127.0.0.1` / `5432` / `wantastic` / `wantastic` / generated | Override to point at an external Postgres. |
| `WANTASTIC_REDIS_ADDR` | `127.0.0.1:6379` | Override to point at an external Redis. |
| `WANTASTIC_SETUP_NONINTERACTIVE` | — | Set to `1` to skip the form entirely — all values read from env vars. Used by CI and the curl-installer. |

## Day-2 ops

```bash
docker logs -f wantastic                       # everything (s6 + child stderr)
docker exec -it wantastic /bin/bash            # shell inside

# Inspect individual services:
docker exec wantastic s6-svstat /run/service/postgres
docker exec wantastic s6-svstat /run/service/wantastic-core
docker exec wantastic s6-svstat /run/service/certbot-renew

# Force a renew check now:
docker exec wantastic certbot renew \
  --config-dir=/var/lib/wantastic/letsencrypt \
  --webroot --webroot-path=/var/lib/wantastic/letsencrypt/webroot

# Re-apply the firewall (after editing the script):
docker exec wantastic /usr/local/bin/firewall-apply.sh

# Postgres dump:
docker exec wantastic su-exec postgres pg_dump -U wantastic wantastic > backup.sql
```

## Ports the host needs open

| Port | Why |
|------|-----|
| `80/tcp` | ACME HTTP-01 challenge + HTTPS redirect. |
| `443/tcp` | Portal + console + setup wizard. |
| `8291/tcp` | Winbox multiplexer. |
| `51820/udp` | WireGuard. |

If the in-container firewall is enabled it allowlists exactly these ports;
the host firewall (cloud security group / ufw / etc.) still needs to let
them through.
