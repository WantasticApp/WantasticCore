.PHONY: help build build-prod run test vet vulncheck image image-run image-logs image-shell image-stop fmt clean

BINARY  := wantastic-core
BUILDD  := bin
MAIN    := ./cmd/wantastic-core
IMAGE   := wantastic:latest
DOCKER  := docker

.DEFAULT_GOAL := help

help:
	@echo "WantasticCore — build & run targets:"
	@echo ""
	@echo "  Dev build:"
	@echo "    make build         Build dev binary -> $(BUILDD)/$(BINARY)"
	@echo "    make build-prod    Stripped optimized build -> $(BUILDD)/prod/$(BINARY)"
	@echo "    make run           Build and run with ./config.yaml"
	@echo ""
	@echo "  Quality:"
	@echo "    make test          Run unit tests"
	@echo "    make vet           Run go vet"
	@echo "    make vulncheck     Run govulncheck (requires \`go install golang.org/x/vuln/cmd/govulncheck@latest\`)"
	@echo "    make fmt           Run go fmt across the repo"
	@echo ""
	@echo "  All-in-one image (postgres + redis + nginx + LE + core):"
	@echo "    make image         Build the container image as $(IMAGE)"
	@echo "    make image-run     Run the image with sane defaults"
	@echo "    make image-logs    Tail container logs"
	@echo "    make image-shell   Open a shell inside the running container"
	@echo "    make image-stop    Stop and remove the container"
	@echo ""
	@echo "    make clean         Remove $(BUILDD)/"

build:
	@mkdir -p $(BUILDD)
	@go build -o $(BUILDD)/$(BINARY) $(MAIN)
	@echo "  built  $(BUILDD)/$(BINARY)"

build-prod:
	@mkdir -p $(BUILDD)/prod
	@go build -ldflags="-s -w" -trimpath -o $(BUILDD)/prod/$(BINARY) $(MAIN)
	@echo "  built  $(BUILDD)/prod/$(BINARY)"

run: build
	@$(BUILDD)/$(BINARY) --config config.yaml

test:
	@go test ./...

vet:
	@go vet ./...

vulncheck:
	@command -v govulncheck >/dev/null || { echo "govulncheck not installed — run: go install golang.org/x/vuln/cmd/govulncheck@latest"; exit 1; }
	@govulncheck ./...

vulncheck-web:
	@echo "== Production deps (these ship in the binary) =="
	@cd internal/portalsrv/app && pnpm audit --prod || true
	@echo ""
	@echo "== Including dev deps (build tooling only — not shipped) =="
	@cd internal/portalsrv/app && pnpm audit || true

fmt:
	@go fmt ./...

# -----------------------------------------------------------------------------
# All-in-one image — postgres + redis + nginx + Let's Encrypt + firewall + core
# -----------------------------------------------------------------------------

image:
	@$(DOCKER) build -t $(IMAGE) -f docker/Dockerfile .

image-run:
	@$(DOCKER) run -d --name wantastic \
		--cap-add NET_ADMIN \
		-p 80:80 -p 443:443 -p 8291:8291 -p 51820:51820/udp \
		-v wantastic-data:/var/lib/wantastic \
		$(IMAGE)
	@echo ""
	@echo "  Container started. Open https://<host>/ to run the setup wizard."

image-logs:
	@$(DOCKER) logs -f wantastic

image-shell:
	@$(DOCKER) exec -it wantastic /bin/bash

image-stop:
	@-$(DOCKER) rm -f wantastic

clean:
	@rm -rf $(BUILDD)
