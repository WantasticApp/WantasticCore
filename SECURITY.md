# Security Policy

## Supported versions

Only the latest release receives security fixes. Older versions are
unsupported — upgrade.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security reports.

Email **security@wantastic.app** with:

- A description of the issue
- Steps to reproduce (or a proof-of-concept)
- Affected version(s) / commit SHA
- Any mitigations you've already tried

We'll acknowledge within 72 hours and aim to have a fix out within 30 days
for high-severity issues. Coordinated disclosure is welcome — we'll credit
you in the release notes unless you'd rather stay anonymous.

## Out of scope

These are by design, not vulnerabilities:

- The first-run setup wizard binds an HTTPS listener with a self-signed
  certificate. The operator is expected to either (a) enable Let's
  Encrypt in the wizard, or (b) close the setup port after first boot.
- The in-container firewall (`WANTASTIC_FIREWALL=1`) is best-effort, not a
  substitute for a host firewall / cloud security group.
- Public sign-up is intentionally not supported — only admin-created
  tenants exist. "Anyone can register" is not a goal.

## Running the vuln checkers

```bash
make vulncheck       # Go: govulncheck against ./...
make vulncheck-web   # Web: pnpm audit (separates --prod from dev tooling)
```

`make vulncheck-web` reports two sections:

- **Production deps** — these ship in the binary's embedded SPA bundle. We
  keep this at zero advisories. If your audit reports anything here,
  open an issue.
- **Dev deps** — build tooling (vite, esbuild, postcss, sass, svelte-check,
  vitest, the Svelte 3 compiler). These run only on a contributor's
  machine during `pnpm run build`; nothing they touch reaches a deployed
  binary. There are persistent advisories here because the Svelte 3
  build chain is pinned (a Svelte 5 upgrade is a separate, larger
  refactor). We track them but don't gate releases on them.

[`.github/dependabot.yml`](.github/dependabot.yml) holds back the Svelte 3
and Vite 4 ecosystems from auto-bump PRs for exactly this reason. Patch-
level bumps for every other package land in a single weekly grouped PR.

CI runs `make vulncheck` on every release tag.

## What's currently green

- `make vulncheck` (Go) — **0 vulnerabilities**.
- `make vulncheck-web` (production deps) — **0 vulnerabilities**.

## What's currently yellow (dev-only)

- ~11 advisories in the SPA's build tooling (vite, vitest, svelte
  compiler). Not exploitable in the deployed binary; tracked in the
  Dependabot config so they don't generate PR noise. The fix is a Svelte
  3 → 5 + Vite 4 → 5 migration; that's planned but separate.



















