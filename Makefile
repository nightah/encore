# Encore developer tasks. POSIX make compatible; run `make` for help.

.POSIX:
.SUFFIXES:

GO       = go
PNPM     = pnpm
DOCKER   = docker
GOLANGCI = golangci-lint
BIN      = bin/encore
IMAGE    = encore:dev

# make dev: the Go server (on :8080, rebuilt and restarted by air when its sources change; see .air.toml) and the Vite dev server, which proxies /api to it. Override on the command line, e.g. to reach it through a reverse proxy: make dev DEV_HOST=0.0.0.0 DEV_HOSTS=dev.example.net
DEV_DATA_DIR = $(HOME)/.local/share/encore-dev
DEV_HOST     = localhost
DEV_PORT     = 3000
DEV_HOSTS    =
DEV_PROXIES  =

# Personal overrides of the settings above (DEV_HOST, DEV_HOSTS and so on) go in .local.mk, which git ignores; command-line values still win.
-include .local.mk

PKG      = github.com/nightah/encore/internal/buildinfo
VERSION  = dev
LDFLAGS  = -s -w -X $(PKG).Version=$(VERSION)

help: ## Show this help (default)
	@echo 'Usage: make <target>'
	@echo
	@grep -E '^[a-zA-Z_-]+:.*## ' Makefile | \
		awk 'BEGIN { FS = ":.*## " } { printf "  %-14s %s\n", $$1, $$2 }'

version: ## Commit "release: vX.Y.Z" on a release branch (made from origin/main unless you're on one), stamping the next version into web/ and docs/package.json: make version patch (or minor, major), or name it with VERSION=1.2.3
	@bump="$(VERSION)"; for goal in $(MAKECMDGOALS); do case "$$goal" in patch|minor|major) bump="$$goal" ;; esac; done; \
	if [ "$$bump" = dev ]; then echo "usage: make version patch|minor|major, or make version VERSION=X.Y.Z" >&2; exit 1; fi; \
	PNPM="$(PNPM)" .github/scripts/release-version.sh "$$bump"

# The bump in "make version patch" is a goal of its own (GNU make's MAKECMDGOALS); version reads it, and alone it does nothing but say so.
patch minor major:
	@case " $(MAKECMDGOALS) " in *" version "*) ;; *) echo "make $@: did you mean make version $@?" >&2; exit 1 ;; esac

icons: ## Render the Alexa skill icons (108 and 512 px PNG) from docs/brand/logo.svg; needs rsvg-convert
	rsvg-convert -w 108 -h 108 docs/brand/logo.svg -o internal/smapi/skillpkg/assets/icon-108.png
	rsvg-convert -w 512 -h 512 docs/brand/logo.svg -o internal/smapi/skillpkg/assets/icon-512.png

generate: ## Regenerate Kodi JSON-RPC and SMAPI clients, the OpenAPI document and the docs reference pages (which docs builds also regenerate)
	$(GO) generate ./...

lint: lint-go lint-web lint-repo ## Lint Go (both modules), web, and repository-wide files

lint-go: ## Lint Go (root + tools module) and interaction models
	$(GOLANGCI) run ./...
	cd tools && $(GOLANGCI) run --config ../.golangci.yml ./...
	$(GO) run ./cmd/encore gen lint

lint-repo: ## Repository-wide checks, as CI runs them: editorconfig, typos, secrets, sorted language files, shell scripts and the image tag, next version and release version scripts' tests, Dockerfiles, workflows and Go vulnerabilities
	$(GO) tool -modfile=tools/go.mod actionlint
	git ls-files -z | xargs -0 $(GO) tool -modfile=tools/go.mod editorconfig-checker
	$(GO) tool -modfile=tools/go.mod govulncheck ./...
	hadolint Dockerfile Dockerfile.goreleaser
	shellcheck --format=gcc docker/root/etc/s6-overlay/s6-rc.d/encore/run docker/root/etc/s6-overlay/scripts/adduser docker/root/etc/s6-overlay/scripts/encoresetup .github/scripts/*.sh
	trufflehog --fail --log-level=-1 filesystem -x .trufflehog .
	typos --format brief
	yamllint --strict .
	zizmor --no-progress .github/workflows
	.github/scripts/image-tag_test.sh
	.github/scripts/next-version_test.sh
	.github/scripts/release-version_test.sh

lint-web: web-install ## Lint and typecheck web
	$(PNPM) -C web lint
	$(PNPM) -C web typecheck

test: test-go test-tools test-web ## Run Go, tools and web unit tests

test-go: ## Run Go tests with the race detector
	$(GO) test -race ./...

test-tools: ## Run tools module tests
	cd tools && $(GO) test ./...

test-web: web-install ## Run web unit tests
	$(PNPM) -C web test

test-contract: ## Run contract tests (needs a real Kodi and MinIO; see the development docs)
	$(GO) test -tags contract -count=1 ./internal/backend/kodi/... ./internal/blobstore/... ./internal/app/

web-install: ## Install web dependencies (frozen lockfile)
	$(PNPM) -C web install --frozen-lockfile

web-build: web-install ## Build the SPA into internal/server/dist
	$(PNPM) -C web build

build: web-build ## Build web then the Encore binary (with UI) into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/encore

build-noui: ## Build the Encore binary without the embedded UI
	CGO_ENABLED=0 $(GO) build -tags noui -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/encore

run: ## Run the server locally with debug logging
	ENCORE_LOG_LEVEL=debug $(GO) run ./cmd/encore serve

dev: web-install ## Run the Go server (rebuilt and restarted by air on changes) and the Vite dev server together; Ctrl-C stops both
	@echo "encore: data in $(DEV_DATA_DIR); UI at http://$(DEV_HOST):$(DEV_PORT)"
	@# Background jobs ignore SIGINT in a non-interactive shell, so the trap stops both explicitly. air and vite run without their go tool and pnpm wrappers so their PIDs are the ones signalled; air stops the server gracefully.
	@air=$$($(GO) tool -modfile=tools/go.mod -n air) || exit 1; \
	ENCORE_DATA_DIR='$(DEV_DATA_DIR)' $(if $(DEV_PROXIES),ENCORE_SERVER_TRUSTED_PROXIES='$(DEV_PROXIES)') "$$air" -c .air.toml & srv=$$!; \
	(cd web && VITE_ALLOWED_HOSTS='$(DEV_HOSTS)' exec ./node_modules/.bin/vite --host '$(DEV_HOST)' --port '$(DEV_PORT)') & ui=$$!; \
	trap 'kill $$srv $$ui 2>/dev/null; wait; exit 0' INT TERM; \
	wait

web-dev: ## Start the Vite dev server
	$(PNPM) -C web dev

docs-install: ## Install docs site dependencies (frozen lockfile)
	$(PNPM) -C docs install --frozen-lockfile

docs: docs-install ## Build the documentation site into docs/dist (checks links)
	$(PNPM) -C docs build

docs-dev: docs-install ## Start the documentation site's dev server
	$(PNPM) -C docs dev

docker: ## Build a local container image (encore:dev)
	$(DOCKER) build --build-arg version=$(VERSION) -t $(IMAGE) .

clean: ## Remove build outputs (keeps internal/server/dist/.gitkeep)
	rm -rf bin dist coverage.out web/playwright-report web/test-results docs/dist docs/.astro
	find internal/server/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

.PHONY: help version patch minor major icons generate lint lint-go lint-repo lint-web test test-go test-tools test-web \
	test-contract web-install web-build build build-noui run dev web-dev docs-install docs docs-dev \
	docker clean
