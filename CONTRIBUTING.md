# Contributing to Wantastic

Thanks for your interest. A few notes to make the round-trip smooth.

## Before you start

Open an issue before you start anything larger than a typo or a small bug fix.
We'd rather agree on direction up-front than have you ship a 1,000-line PR
that we'd have asked you to architect differently.

## Local setup

```bash
git clone https://github.com/WantasticApp/WantasticCore.git
cd WantasticCore
make build        # → bin/wantastic-core
make test         # unit tests
make vet          # go vet
make vulncheck    # govulncheck (install: go install golang.org/x/vuln/cmd/govulncheck@latest)
make image        # build the all-in-one container
make image-run    # run it on localhost
```

The frontend lives in `internal/portalsrv/app/` (Svelte + Vite). To rebuild
after editing Svelte sources:

```bash
cd internal/portalsrv/app
pnpm install   # first time only
pnpm run build
cd ../../..
make build     # re-embed dist/ into the Go binary
```

## What we'll accept

- **Bug fixes** — always welcome. Include a regression test if you can.
- **Performance improvements** — please include a benchmark and a profile.
- **New protocol adapters** (RouterOS, Cisco, OpenWrt, etc.) — sure, but
  scope them tightly and put them behind a config toggle.
- **UI polish** — keep it consistent with the existing window-chrome
  pattern in `internal/portalsrv/app/src/apps/Peers.svelte` (the reference
  app). Don't introduce new design systems.
- **Documentation** — `README.md`, `docker/README.md`, and the WUSP white
  paper in `docs/` are fair game.

## What we'll push back on

- **New external SaaS dependencies** in the hot path (analytics, telemetry,
  feature flags, etc.). The project is intentionally self-hosted.
- **gRPC, microservices, "service mesh" patterns** — all in-process method
  calls now. Don't add inter-service network hops.
- **Public sign-up flows** — admin-managed tenants only by design.
- **Multi-instance HA** — out of scope for this codebase. If you need it,
  fork and run it.

## Commit + PR style

- One logical change per PR. Split refactors from feature work.
- Commit messages: `<area>: <imperative summary>` (e.g. `webproxy: handle CORS preflight`).
- PR description: what changed, why, what you tested, screenshots if UI.
- Run `make vet`, `make test`, and `make image` before pushing.

## License

By contributing, you agree your code is offered under the project's
[MIT License](LICENSE).
