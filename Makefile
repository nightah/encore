# Encore developer tasks. POSIX make compatible; run `make` for help.

.POSIX:
.SUFFIXES:

GO       = go
PNPM     = pnpm
DOCKER   = docker
GOLANGCI = golangci-lint
NLU_FLAGS =
SIM_FLAGS =
BIN      = bin/encore
IMAGE    = encore:dev

# make dev: the Go server (on :8080, rebuilt and restarted by air when its sources change; see .air.toml) and the Vite dev server, which proxies /api to it. Override on the command line, e.g. to reach it through a reverse proxy: make dev DEV_HOST=0.0.0.0 DEV_HOSTS=dev.example.net
DEV_DATA_DIR = $(HOME)/.local/share/encore-dev
# make dev records its air and vite PIDs here, so a second make dev can find them.
DEV_PIDS     = $(DEV_DATA_DIR)/dev.pids
DEV_HOST     = localhost
DEV_PORT     = 3000
DEV_HOSTS    =
DEV_PROXIES  =
# The skill the dev server deploys, so it doesn't adopt the skill another instance deploys as "Encore" (e.g. Encore DEV).
DEV_SKILL_NAME =

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

icons: ## Render the Alexa skill icons (108 and 512 px PNG) from docs/brand/logo.svg and the web app's maskable icon from docs/brand/mark-maskable.svg; needs rsvg-convert
	rsvg-convert -w 108 -h 108 docs/brand/logo.svg -o internal/smapi/skillpkg/assets/icon-108.png
	rsvg-convert -w 512 -h 512 docs/brand/logo.svg -o internal/smapi/skillpkg/assets/icon-512.png
	rsvg-convert -w 512 -h 512 docs/brand/mark-maskable.svg -o web/public/icon-maskable-512.png

generate: ## Regenerate Kodi JSON-RPC and SMAPI clients, the OpenAPI document and the docs reference pages (which docs builds also regenerate)
	$(GO) generate ./...

lint: lint-go lint-web lint-repo ## Lint Go (all three modules), web, and repository-wide files

lint-go: ## Lint Go (root, tools and benchmarks modules) and interaction models
	$(GOLANGCI) run ./...
	cd tools && $(GOLANGCI) run --config ../.golangci.yml ./...
	cd benchmarks && $(GOLANGCI) run --config ../.golangci.yml ./...
	$(GO) run ./cmd/encore gen lint

lint-repo: ## Repository-wide checks, as CI runs them: editorconfig, typos, secrets, sorted language files, shell scripts and the image scripts', MSW worker's, mutation go wrapper and shards', next version, release commit, release version and Go coverage scripts' tests, Dockerfiles, workflows and Go vulnerabilities
	$(GO) tool -modfile=tools/go.mod actionlint
	git ls-files -z | xargs -0 $(GO) tool -modfile=tools/go.mod editorconfig-checker
	$(GO) tool -modfile=tools/go.mod govulncheck ./...
	hadolint Dockerfile Dockerfile.goreleaser
	shellcheck --format=gcc docker/root/etc/s6-overlay/s6-rc.d/encore/run docker/root/etc/s6-overlay/scripts/adduser docker/root/etc/s6-overlay/scripts/encoresetup .github/scripts/*.sh
	trufflehog --fail --log-level=-1 filesystem -x .trufflehog .
	typos --format brief
	yamllint --strict .
	zizmor --no-progress .github/workflows
	.github/scripts/go-coverage_test.sh
	.github/scripts/image-delete-untagged_test.sh
	.github/scripts/image-superseded_test.sh
	.github/scripts/image-tag_test.sh
	.github/scripts/mutation-go_test.sh
	.github/scripts/mutation-shard_test.sh
	.github/scripts/next-version_test.sh
	.github/scripts/release-commit_test.sh
	.github/scripts/release-version_test.sh

lint-web: web-install ## Lint and typecheck web
	$(PNPM) -C web lint
	$(PNPM) -C web typecheck

test: test-go test-tools test-web ## Run Go, tools and web unit tests

test-go: ## Run Go tests with the race detector
	$(GO) test -race ./...

test-tools: ## Run tools module tests
	cd tools && $(GO) test ./...

test-benchmarks: ## Run the benchmarks module's parity tests: the in-tree code against bleve, go-edlib, pquerna/otp and the phonetic libraries
	cd benchmarks && $(GO) test ./...

bench: ## Run the benchmarks module's comparisons once and summarise them with benchstat (see benchmarks/README.md to reproduce the published numbers)
	cd benchmarks && $(GO) test -run '^$$' -bench . -benchtime 100x ./retrieval > bench-retrieval.txt
	cd benchmarks && for p in editdistance totp phonetic; do $(GO) test -run '^$$' -bench . ./$$p > bench-$$p.txt || exit 1; done
	cd benchmarks && for p in retrieval editdistance totp phonetic; do $(GO) tool benchstat -col /impl bench-$$p.txt || exit 1; done

nlu-deploy: ## Deploy these sources' model to the skill kept for nlu and nlu-open, named "Encore NLU CI" (same credentials as nlu; NLU_INVOCATION is encore or kodi, the one your own skill doesn't use)
	$(GO) run ./cmd/encore gen deploy --invocation=$(NLU_INVOCATION) $(NLU_FLAGS)

nlu: ## Profile testdata/nlu's utterances against the deployed skill with SMAPI's profileNlu (needs SMAPI_CLIENT_ID, SMAPI_CLIENT_SECRET, SMAPI_REFRESH_TOKEN and SMAPI_SKILL_ID; NLU_FLAGS adds flags, e.g. NLU_FLAGS='-l nl-NL -v')
	$(GO) run ./cmd/encore gen nlu $(NLU_FLAGS)

nlu-open: ## Say each locale's launch phrases to Amazon's simulator against the deployed skill and report which locales it refuses (same credentials as nlu; NLU_FLAGS adds flags, e.g. NLU_FLAGS='--invocation kodi', or NLU_INVOCATION=kodi as for nlu-deploy)
	$(GO) run ./cmd/encore gen open $(if $(filter kodi,$(NLU_INVOCATION)),--invocation kodi) $(NLU_FLAGS)

simulate: ## Say utterances, or a cases file, to Amazon's simulator and show or check what Alexa heard and the skill answered, by default with make dev's skill, as Encore in DEV_DATA_DIR reaches it (ENCORE_DATA_DIR picks another); SIM_FLAGS holds the flags and quoted utterances, e.g. SIM_FLAGS='-f cases.yaml'. It really controls your Kodi instances
	ENCORE_DATA_DIR='$(or $(ENCORE_DATA_DIR),$(DEV_DATA_DIR))' $(GO) run ./cmd/encore gen simulate $(SIM_FLAGS)

test-web: web-browser ## Run web unit tests (in headless Chromium)
	$(PNPM) -C web test

test-contract: ## Run contract tests (needs a real Kodi, MinIO and WebDAV server; see the development docs)
	$(GO) test -tags contract -count=1 ./internal/backend/kodi/... ./internal/blobstore/... ./internal/app/

web-install: ## Install web dependencies (frozen lockfile)
	$(PNPM) -C web install --frozen-lockfile

web-browser: web-install ## Install the Chromium the web tests run in (skips the download once installed)
	$(PNPM) -C web exec playwright install chromium

web-build: web-install ## Build the SPA into internal/server/dist
	$(PNPM) -C web build

build: web-build ## Build web then the Encore binary (with UI) into bin/
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/encore

build-noui: ## Build the Encore binary without the embedded UI
	CGO_ENABLED=0 $(GO) build -tags noui -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/encore

run: ## Run the server locally with debug logging
	ENCORE_LOG_LEVEL=debug $(GO) run ./cmd/encore serve

# dev_find sets pids to what is already running against DEV_DATA_DIR, and found to a line about each: make dev's air and vite, by the PIDs it recorded, and the Encore holding the data directory's lock, with the air that started it. Each PID is checked against its command, as a PID may have been reused. Recipes run with sh -e, so a missing file or a process already gone must not fail it.
define dev_find
pids=; found=; \
for p in $$(cat '$(DEV_PIDS)' 2>/dev/null || true); do \
	what=$$(ps -o args= -p "$$p" 2>/dev/null | grep -owE 'air|vite' | head -1); \
	if [ -n "$$what" ]; then pids="$$pids $$p"; found="$$found\nencore: make dev's $$what (pid $$p) is running for $(DEV_DATA_DIR)"; fi; \
done; \
p=$$(cat '$(DEV_DATA_DIR)/encore.lock' 2>/dev/null || true); \
if [ -n "$$p" ] && ps -o args= -p "$$p" 2>/dev/null | grep -qw encore-dev; then \
	pp=$$(ps -o ppid= -p "$$p" 2>/dev/null | tr -d ' '); pids="$$pids $$p"; \
	found="$$found\nencore: Encore (pid $$p, started by pid $$pp) is using $(DEV_DATA_DIR)"; \
	if ps -o args= -p "$$pp" 2>/dev/null | grep -qw air; then case " $$pids " in *" $$pp "*) ;; *) pids="$$pids $$pp";; esac; fi; \
fi
endef

dev: web-install ## Run the Go server (rebuilt and restarted by air on changes) and the Vite dev server together; Ctrl-C or closing the terminal stops both. Refuses to start while another is running against DEV_DATA_DIR, and says how to stop it
	@$(dev_find); \
	if [ -n "$$found" ]; then printf '%b\n' "$${found#\\n}" "encore: stop them with: kill$$pids (or use another DEV_DATA_DIR)" >&2; exit 1; fi
	@echo "encore: data in $(DEV_DATA_DIR); UI at http://$(DEV_HOST):$(DEV_PORT)"
	@# Background jobs ignore SIGINT in a non-interactive shell, so the trap stops both explicitly, and on HUP too, as closing the terminal sends: without it air outlives the terminal and keeps restarting Encore. air and vite run without their go tool and pnpm wrappers so their PIDs are the ones signalled; air stops the server gracefully.
	@air=$$($(GO) tool -modfile=tools/go.mod -n air) || exit 1; \
	mkdir -p '$(DEV_DATA_DIR)'; \
	ENCORE_DATA_DIR='$(DEV_DATA_DIR)' $(if $(DEV_PROXIES),ENCORE_SERVER_TRUSTED_PROXIES='$(DEV_PROXIES)') $(if $(DEV_SKILL_NAME),ENCORE_ALEXA_SKILL_NAME='$(DEV_SKILL_NAME)') "$$air" -c .air.toml & srv=$$!; \
	(cd web && VITE_ALLOWED_HOSTS='$(DEV_HOSTS)' exec ./node_modules/.bin/vite --host '$(DEV_HOST)' --port '$(DEV_PORT)') & ui=$$!; \
	echo "$$srv $$ui" > '$(DEV_PIDS)'; \
	stop() { kill $$srv $$ui 2>/dev/null || true; wait || true; rm -f '$(DEV_PIDS)'; }; \
	trap 'stop; exit 0' INT TERM HUP; \
	wait; stop

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
	rm -rf bin dist coverage.out benchmarks/bench-*.txt web/playwright-report web/test-results docs/dist docs/.astro
	find internal/server/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +

.PHONY: help version patch minor major icons generate lint lint-go lint-repo lint-web test test-go test-tools test-benchmarks bench nlu-deploy nlu nlu-open simulate test-web \
	test-contract web-install web-browser web-build build build-noui run dev web-dev docs-install docs docs-dev \
	docker clean
