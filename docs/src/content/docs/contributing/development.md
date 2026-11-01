---
title: Development setup
description: Run Encore from source, the make targets, generated code and the test suites.
---

## Prerequisites

- **Go**, the version in `go.mod`.
- **Node.js**, the version in the `engines` field of `web/package.json`, and **pnpm**, the version pinned under `packageManager` there (`corepack enable` picks it up).
- **golangci-lint** v2, for the Git hooks and `make lint`.
- **[hadolint](https://github.com/hadolint/hadolint), [ShellCheck](https://www.shellcheck.net), [TruffleHog](https://github.com/trufflesecurity/trufflehog), [typos](https://github.com/crate-ci/typos), [yamllint](https://yamllint.readthedocs.io) and [zizmor](https://docs.zizmor.sh)**, for the Git hooks and `make lint-repo`; install them with your package manager. The pre-commit hook first checks for these, golangci-lint and pnpm, and lists any that are missing.
- **Docker**, only for the contract tests and container builds.

## Repository layout

| Path | What lives there |
| --- | --- |
| `cmd/encore` | The CLI (`serve`, `restore`, `healthcheck`, `version`, `gen`) |
| `internal/` | Application code; `internal/server/dist` is the embedded web build |
| `web/` | The web interface: React, Vite, Biome, Vitest and Playwright |
| `docs/` | This site (Astro Starlight) |
| `tools/` | A separate Go module for the code generators and the developer tools (actionlint, air, editorconfig-checker, govulncheck), so their dependencies stay out of Encore's; run them with `go tool -modfile=tools/go.mod …` |
| `benchmarks/` | A separate Go module comparing Encore's own search index, edit distances, TOTP and phonetic encoders with the libraries they replaced, so those libraries stay out of Encore's `go.mod`; see [Dependencies and benchmarks](/encore/contributing/dependencies/) |
| `i18n/` | The Alexa interaction model and translations |

## Git hooks

`pnpm install` in `web/` (or any make target that needs it, such as `make dev`) also installs the repository's Git hooks with [Lefthook](https://lefthook.dev), configured in `.lefthook.yml`:

| Hook | Checks |
| --- | --- |
| `pre-commit` | actionlint for workflows; Biome for `web/` and `docs/`; editorconfig-checker (`.editorconfig`); golangci-lint with fixes on all three Go modules; hadolint for Dockerfiles; the interaction-model lint; ShellCheck for shell scripts; TruffleHog over the repository (`.trufflehog`); `tsc` for `web/`; typos (`.typos.toml`, Australian English in prose); yamllint for sorted keys in the skill's language files (`.yamllint.yml`); and zizmor for workflows. Each runs only when matching files are staged, and fixes are staged again |
| `commit-msg` | [commitlint](https://commitlint.js.org/) (`web/commitlint.config.mjs`): a Conventional Commits header of at most 72 characters with a known scope |

`make lint-repo` runs the repository-wide checks over everything, as CI's **Repository checks** job does; CI also runs [OSV-Scanner](https://google.github.io/osv-scanner/) on what ships (Encore's `go.mod` and the web interface's production dependencies; `.osv-scanner.toml`). Skip the hooks for one commit with `LEFTHOOK=0 git commit …`.

## Running it

```sh
make dev
```

This runs Encore on `:8080` under [air](https://github.com/air-verse/air), which rebuilds and restarts it when its Go sources, interaction model or migrations change, and the Vite dev server at `http://localhost:3000`, which proxies `/api` to it. Data goes in `~/.local/share/encore-dev` (`DEV_DATA_DIR`), so it never touches a real installation. Ctrl-C stops both.

To reach it through a reverse proxy, override the host and allowed host names, and list the proxy in `DEV_PROXIES` so Encore trusts its forwarded headers: `make dev DEV_HOST=0.0.0.0 DEV_HOSTS=dev.example.net DEV_PROXIES=172.18.0.0/16`. To keep settings like these without typing them each time, put them in `.local.mk` at the repository root (`DEV_HOSTS = dev.example.net`, one per line); git ignores it, and command-line values still win. Without a server, `pnpm dev:mock` in `web/` serves the interface against a mock API in the browser.

## Make targets

`make` lists them all. The ones used most:

| Command | Does |
| --- | --- |
| `make dev` | The server and the web dev server, as above |
| `make lint` | golangci-lint (root, `tools/` and `benchmarks/`), interaction-model lint, Biome and `tsc`, and `make lint-repo` |
| `make test` | Go tests with the race detector, `tools/` tests and Vitest |
| `make test-benchmarks` / `make bench` | The `benchmarks/` module's parity tests / one quick run of its comparisons with benchstat |
| `make generate` | Regenerates the Kodi JSON-RPC and SMAPI clients, the OpenAPI document and the docs reference pages |
| `make build` | Builds the web interface, then `bin/encore` with it embedded |
| `make docs` / `make docs-dev` | Builds this site into `docs/dist` (checking links) / serves it locally |
| `make test-contract` | Contract tests against a real Kodi, MinIO and WebDAV server |
| `make docker` | Builds a local container image, `encore:dev` |

## Generated code

The Kodi JSON-RPC client (`internal/backend/kodi/kodirpc`), the SMAPI client (`internal/smapi/smapiclient`), the OpenAPI document and the web client (`web/src/api/generated`) are generated and committed. After changing a schema, a generator or the API, run `make generate` (and `pnpm generate:api` in `web/` for the web client) and commit the result. CI fails when generation leaves a diff.

The configuration, CLI, API endpoints and voice-command reference pages here are generated too, but not committed: the docs site's `dev` and `build` scripts run `go generate ./internal/docsgen` first, so they follow the code and every translation Crowdin syncs without a commit of their own. `pnpm dev` (or `make docs-dev`) also regenerates the OpenAPI document first, and while it runs it regenerates both again whenever a Go file under `internal/` or a translation under `i18n/` changes, so open pages follow your edits without a restart. The API endpoints page is built from the committed OpenAPI document, so what it says about a route or a field belongs in the Go code: the operation's `Summary` and `Description`, a field's `doc` tag, or a tag's description in `humaConfig`.

## Tests

- **Go:** `go test -race ./...`. Kodi is replaced by a fake JSON-RPC server (`internal/backend/kodi/fakekodi`) and Amazon's APIs by fakes, so nothing external is needed.
- **Web:** `pnpm test` (Vitest) in `web/`, in headless Chromium against the same mock API as `pnpm dev:mock`. Install the browser once with `pnpm exec playwright install chromium`; `make test` does it for you.
- **Browser:** `pnpm test:e2e:mock` runs Playwright against the mock API; `pnpm test:e2e` builds Encore and runs the first-run journey against a real server and a fake Kodi.
- **Contract:** the `contract` build tag runs against real services, nightly in CI. To run them locally:

```sh
docker run -d --rm --name kodi -p 8080:8080 -p 9090:9090 \
  -v "$PWD/internal/backend/kodi/testdata/kodi-headless/advancedsettings.xml:/config/.kodi/userdata/advancedsettings.xml:ro" \
  -v "$PWD/internal/backend/kodi/testdata/kodi-headless/library:/media:ro" \
  matthuisman/kodi-headless:Omega   # the mounted settings switch the image from MySQL to SQLite libraries
docker run -d --rm --name minio -p 9000:9000 \
  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
  cgr.dev/chainguard/minio:latest server /data
docker run -d --rm --name rclone -p 8081:80 \
  rclone/rclone:1.75.2 serve webdav /data --addr :80 --user encore --pass encore-contract

export ENCORE_CONTRACT_KODI_URL=http://127.0.0.1:8080/jsonrpc
export ENCORE_CONTRACT_S3_ENDPOINT=http://127.0.0.1:9000
export ENCORE_CONTRACT_WEBDAV_URL=http://127.0.0.1:8081/
export ENCORE_CONTRACT_WEBDAV_USERNAME=encore
export ENCORE_CONTRACT_WEBDAV_PASSWORD=encore-contract
make test-contract
```

The WebDAV test takes any server, and the nightly job also runs it against Nextcloud. To do the same, start Nextcloud, wait for `http://127.0.0.1:8082/status.php` to report `"installed":true`, and point `ENCORE_CONTRACT_WEBDAV_URL` at the user's files:

```sh
docker run -d --rm --name nextcloud -p 8082:80 \
  -e SQLITE_DATABASE=nextcloud -e NEXTCLOUD_TRUSTED_DOMAINS=127.0.0.1 \
  -e NEXTCLOUD_ADMIN_USER=encore -e NEXTCLOUD_ADMIN_PASSWORD=encore-contract \
  nextcloud:35.0.2-apache

ENCORE_CONTRACT_WEBDAV_URL=http://127.0.0.1:8082/remote.php/dav/files/encore/ \
  go test -tags contract -count=1 -run TestWebDAVServerContract ./internal/blobstore/
```

With `ENCORE_CONTRACT_KODI_LIBRARY` also set, the voice tests in `internal/app` ask Encore for the titles in the test library (`internal/backend/kodi/testdata/kodi-headless/library`) and check what it matched and played. Kodi has to have scanned that library first: the "Seed the library" step in `.github/workflows/nightly.yml` adds it and starts the scan.

## Test images

Every branch pushed to the repository, and every pull request from a fork, gets a container image to try the change in, for the same platforms as releases:

- The main branch is published as `ghcr.io/nightah/encore:main`, with `main-<commit>` as its version.
- A branch is published as `ghcr.io/nightah/encore:<branch>`, with each run of characters a tag can't hold turned into `-`, so `fix/abc` becomes `fix-abc` (`.github/scripts/image-tag.sh`). A branch whose name could take over a release or pull request tag, such as `1.2` or `latest`, gets no image, and neither do Renovate's `renovate/…` branches.
- A pull request from a fork is published as `ghcr.io/nightah/encore:PR<number>` once a maintainer approves the `pr-images` environment for its run.

The pull request gets a comment with the `docker pull` command, updated on every push. A branch or pull request that only changes `docs/` gets no image, since it would be main's. A branch's image is deleted with the branch, and a fork's when its pull request closes. An image a tag moves off is deleted when the new one is published, and a run whose commit has since been superseded publishes nothing.

CI builds the image (the Image job in `.github/workflows/ci-jobs.yml`) from the same binaries it tested, with no write access, so it can run a fork's code; publishing (`images-publish.yml`) runs from the default branch and only copies the built image to the registry.

## Releasing

Versions are chosen by hand, and a release is a tag:

1. Run `make version patch` (or `minor`, `major`; for an exact version, `make version VERSION=1.2.3`). It fetches `origin/main`, works out the next version from its `web/package.json`, creates a `release/vX.Y.Z` branch from it, stamps the version into `web/package.json` and `docs/package.json`, and commits them as `release: vX.Y.Z`. Run on a `release/…` branch, it commits there instead. Push the branch and open a pull request with that title; its milestone, `vX.Y.Z`, already holds the pull requests going into it.
2. Merge it. Its squash-merged `release: vX.Y.Z (#N)` commit is the release: the Tag release workflow (`tag-release.yml`) checks that both `package.json` files carry that version, tags the commit `vX.Y.Z` and pushes the tag as the `encore-helper` GitHub App, which starts the release. Nobody tags by hand; a tag pushed any other way is refused unless it points at that same commit.
3. The release workflow checks the tag is that merged release commit on `main`, builds the web interface, and runs GoReleaser: binaries, multi-arch images and SBOMs. It then attests them with GitHub artifact attestations: SLSA Build Level 3 provenance for the archives (`encore.intoto.jsonl` on the release) and the image (attached in the registry). The build and the attestations run in the reusable `release-slsa.yml`, which is what signs the provenance. The binaries report the version from the tag.

The `encore-helper` app has only Contents read and write, on this repository. It is the one bypass actor besides admins on the `v*` tag ruleset, which otherwise stops anyone creating, moving or deleting release tags. Its client ID and private key sit in two environments: `release-tag`, which only `main` can use, and `renovate-generate`, which only Renovate's `renovate/codegen` branch can use (see [Dependency updates](#dependency-updates)). If a key leaks, revoke it on the app's settings page, generate a new one and replace the `RELEASE_APP_PRIVATE_KEY` secret in both.

The release notes are GitHub's, generated from the pull requests merged since the previous tag and grouped by their labels (`.github/release.yml`): breaking changes, features, bug fixes, performance, translations, documentation, refactoring, tests, build and CI, reverts, dependencies, then anything unlabelled. Release pull requests are left out.

## Dependency updates

Renovate (the hosted app, configured in `.renovaterc.json`) opens the dependency pull requests. After a Go update it runs `go mod tidy` in the updated module and in every module that replaces it with a local path (`gomodTidyAll`), so `benchmarks/` stays in step with the root module.

The hosted app can't run `go generate`, so the dependencies whose updates change generated code (oapi-codegen and kin-openapi for the SMAPI client, huma for the OpenAPI document, cobra and pflag for the CLI reference) share one group, on the `renovate/codegen` branch. There, `renovate-generate.yml` runs `go generate ./...` without write access and commits whatever changed as the `encore-helper` app, which GitHub signs and which starts CI. Renovate ignores the app's commits (`gitIgnoredAuthors`), so it keeps rebasing the branch, and each rebase is regenerated in turn. A dependency that starts shaping generated code belongs in that group.

## Screenshots

The screenshots on this site and in the README come from `pnpm screenshots` in `web/`, which captures every page light and dark from the mock data, or from a running Encore with sensitive details replaced by examples. `web/README.md` lists its options.
