# Encore web UI

The admin UI for Encore: a React single-page app that is built into `internal/server/dist/` and embedded in the Go binary with `//go:embed all:dist`.

## Requirements

- Node.js, the version in `package.json` `engines`
- pnpm, the version pinned in `package.json` `packageManager` (`corepack enable` or pnpm's own version management picks it up)

## Develop

```sh
make -C .. dev      # the Go server and the dev server together; Ctrl-C stops both
```

`make dev` runs Encore under [air](https://github.com/air-verse/air), which rebuilds it without the embedded UI and restarts it whenever its Go sources, interaction model or migrations change (`.air.toml`; a change that doesn't compile leaves the last build running). Encore runs on `:8080` with its own data directory (`~/.local/share/encore-dev`, or `DEV_DATA_DIR`), and the dev server starts at http://localhost:3000. Choose the log level under Settings › General. air runs from the `tools/` module (`go tool -modfile=tools/go.mod air`), so nothing needs installing. To run them separately:

```sh
pnpm install
pnpm dev            # http://localhost:3000 (needs the Go server)
```

The Vite dev server proxies `/api/*` to the Go server on `http://127.0.0.1:8080` (override with `ENCORE_API_URL`). The server-sent events stream `/api/v1/events` is proxied without timeouts or buffering.

### Behind a reverse proxy

To reach the dev server through a proxy on another host (for example to test the Amazon login, which needs an HTTPS return URL), listen on all interfaces and allow the proxy's hostname:

```sh
make -C .. dev DEV_HOST=0.0.0.0 DEV_HOSTS=dev.example.net  # point the proxy at port 3000
```

Encore trusts proxies on loopback by default (`server.trusted_proxies`), so it honours the dev server's forwarded headers without extra setup. To run them separately:

```sh
go run ./cmd/encore serve                                   # from the repository root
VITE_ALLOWED_HOSTS=dev.example.net pnpm dev --host 0.0.0.0  # point the proxy at port 3000
```

### Without the Go server (mock API)

```sh
pnpm dev:mock       # http://localhost:3000, API served by MSW in the browser
```

`vite --mode mock` starts an [MSW](https://mswjs.io) service worker that implements the admin API against an in-memory database with sample data (Lounge, Bedroom and Kids' room; five Echos, one unlinked; a request log). Sync, wake and deploy progress and the first "say hello" request arrive over a mocked event stream. The state lives in `sessionStorage`, so reloads keep it.

- `?mock=fresh` starts from a first run (setup required; the setup token is `K7QF-2M9X-PL4D`).
- `?mock=configured` resets to the sample home. Its admin password is `correct-horse-battery`.

msw's Vite plugin (`msw/vite`, worker-only mode) serves the worker script straight from the installed msw, in mock mode and only from the dev server, so it always matches msw and production builds never contain it; `import.meta.env.MODE === "mock"` is static, so the mock code is dropped from builds too. Tests use the same handlers, answering the test page's `fetch` calls without the worker (`src/test/server.ts`).

## Build

```sh
pnpm build
```

Output goes to `../internal/server/dist/` (emptied on every build). A small Vite plugin recreates `internal/server/dist/.gitkeep` after the build, so `go build` still works on a clean checkout where the UI was never built. Don't commit build output; only `.gitkeep` is tracked.

Assets use relative URLs (`base: "./"`) so the Go server can mount the UI under any path. The Go server injects `<base href>` (its configured `server.base_path`) and a CSP nonce into `index.html`; `src/basePath.ts` reads the base for the router and API client. The nonce placeholder `<meta property="csp-nonce" content="{{ .CSPNonce }}">` is read at startup and passed to Base UI's `CSPProvider`. `index.html` contains no inline scripts or styles.

## Check and test

```sh
pnpm lint           # Biome lint + format check
pnpm format         # Biome: apply formatting and safe fixes
pnpm typecheck      # TypeScript 7 (native compiler)
pnpm test           # Vitest + Testing Library, in headless Chromium
pnpm test:e2e:mock  # Playwright against `vite --mode mock` (started for you)
pnpm test:e2e       # Playwright against the real binary and a fake Kodi (started for you)
```

Unit and screen tests (Vitest browser mode + Testing Library) run in headless Chromium through Playwright, so install it once with `pnpm exec playwright install chromium` (`make test` does). Files named `*.node.test.ts` read the repository or start servers, so they run in Node instead; the browser and node projects are in `vite.config.ts`. The tests render the whole app with `renderApp()` from `src/test/utils.tsx` against the MSW handlers. `recordRequests()` captures method, path, CSRF header and body; `failWith()` makes one endpoint return problem+json; `FakeEventSource.emit()` (`src/test/eventSource.ts`) delivers SSE events, and the mock's own simulated events are forwarded to it. A test waits for what it acts on (`findBy…`, `waitFor`, `findEnabledButton()`) rather than assuming the mock API has answered. After each test the setup waits for the app's requests to finish, so none lands in the next test, and `pageLocation` (`src/lib/pageLocation.ts`) stands in for reloads and redirects, which would take the test page with them.

Specs directly in `e2e/` run in both modes; `e2e/mock/` only against the mock API and `e2e/real/` only against the real server.

`pnpm test:e2e:mock` runs `e2e/mock/` (setup wizard, linking an Echo, match tester, theme persistence, sign-in method fields) and the shared specs. It starts Vite in mock mode on port 3001 (`E2E_MOCK_PORT` to change), or outside CI reuses one already running there; the port differs from the dev server's so the real app is never tested by mistake.

`pnpm test:e2e` runs against the real server. `e2e/real/global-setup.ts` builds and starts the fake Kodi (`internal/backend/kodi/fakekodi/cmd/fakekodi`, port 18080 or `E2E_KODI_PORT`) and starts Encore on a fresh temporary data directory (port 19091 or `E2E_PORT`), reading the one-time setup token from its log. The journey in `e2e/real/` goes through the setup wizard, sign-in and every page, and fails on any API error or console error; the shared specs then run signed in as the admin it created. It first builds the UI and then the binary into the temporary directory, so the run always tests the current code; set `E2E_ENCORE_BIN` to use a binary you've already built instead, such as `bin/encore` from `make build` (relative to the repository root; CI does this). Set `E2E_SERVER_LOG=1` to see Encore's log.

```sh
pnpm exec playwright install chromium
pnpm test:e2e
```

## Screenshots

`pnpm screenshots` captures every page, light and dark, into `docs/src/assets/screenshots/`, which the docs site's tour and the README use. Sensitive details are replaced with realistic examples rather than blurred:

- The captured Encore's host name becomes `encore.example.net`, other host names `<first label>.example.net`; URLs keep their paths.
- IP addresses come from `192.168.1.0/24` (IPv6 from `fd00::/64`), keeping prefix lengths and ports.
- Amazon IDs, UUIDs, MAC addresses, vendor IDs and tokens keep their prefix and shape with made-up characters; emails become `admin@example.net` and home directories `/home/encore`.
- An element marked `data-sensitive="example"` shows the example; with an empty value, or for a filled-in password field, it's blurred.

The same original always gets the same example, so related values still match and repeated captures don't change.

Without options it captures the mock data. To capture a running Encore:

```sh
SCREENSHOT_URL=https://encore.example.net SCREENSHOT_SESSION_FILE=~/.config/encore/session pnpm screenshots
```

- `SCREENSHOT_SESSION_FILE`: a file holding the value of the `encore_session` cookie from a signed-in browser (for OpenID Connect or proxy sign-in). Or `ENCORE_PASSWORD_FILE` (and `ENCORE_USERNAME`, default admin) to sign in with the password.
- `SCREENSHOT_REPLACE`: more text to replace, as comma-separated `original=example` pairs, such as `MisatoNUC=Lounge`. Without `=example`, the text is blurred.
- `SCREENSHOT_ONLY`: only these pages, e.g. `kodi,alexa-skill` (`login` for the sign-in page).
- `SCREENSHOT_DIR`: somewhere else to write them.

Check every image before committing it: the patterns catch the usual details, not everything.

## Layout

Import through the folder aliases, never relative paths: `@api/…`, `@components/…`, `@features/…`, `@lib/…` and so on for each folder in `src/`, `@src/…` for the files directly in `src/` (`App`, `router`, `basePath`), and `@e2e/…` in the Playwright specs. They are declared once, in `tsconfig.json`'s `paths`, which Vite reads too (`resolve.tsconfigPaths`), and Biome's `noRestrictedImports` fails on a relative import. Biome also sorts imports into groups: `react` first, then Node built-ins and packages, then the aliases, each group separated by a blank line, with named imports in case-sensitive order (digits, capitals, then lower case). Object keys are kept sorted too, so nothing may depend on their order: put an ordered list in an array (as `CHECK_ORDER` does), give callbacks that TypeScript would otherwise infer from a neighbouring option an explicit parameter type (as `getNextPageParam` does), and make overlapping proxy rules exclude each other instead of relying on which is listed first. Generated code (`src/api/generated/`, `routeTree.gen.ts`) is left as its generators write it.

| Path                   | What                                                                                                                            |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `src/routes/`          | TanStack Router file-based routes (`_shell` = pages inside the rail; `setup`, `login` outside)                                  |
| `src/routeTree.gen.ts` | Generated by the router plugin on `dev`/`build`; committed, not linted                                                          |
| `src/features/`        | Page parts and their tests, one folder per screen                                                                               |
| `src/components/`      | App shell, page frame, theme toggle, status helpers                                                                             |
| `src/components/ui/`   | Component set on Base UI + Tailwind (Button, Field, Dialog, Tabs, Toast, Table, Meter…)                                         |
| `src/styles/theme.css` | Design tokens (light/dark) and the Tailwind `@theme` mapping                                                                    |
| `src/theme/store.ts`   | Theme preference store: `system` (default), `light`, `dark`                                                                     |
| `src/api/`             | Generated API client (`generated/`), query hooks and the SSE event stream                                                       |
| `src/i18n/`            | Languages and strings: one JSON file per namespace and language in `locales/`; English is the source, Crowdin supplies the rest |
| `src/lib/`             | Formatting, error reporting, stale-chunk reload and setup progress                                                              |
| `src/mocks/`           | MSW handlers and sample data (mock mode and tests), typed against the generated model                                           |
| `src/test/`            | Test setup and helpers (`renderApp`, `recordRequests`, `FakeEventSource`)                                                       |
| `public/brand/`        | Logo and mark (see `public/brand/ATTRIBUTION.txt`)                                                                              |

Theme: the preference lives in `localStorage` under `encore.theme` and resolves to `data-theme="light|dark"` on `<html>`. Tailwind's `dark:` variant keys off that attribute. With `system`, the store follows `prefers-color-scheme` live; before JavaScript runs, CSS falls back to the OS preference.

## API layer

The client is generated from the server's OpenAPI 3.1 spec, `internal/api/openapi.json` (written by Huma; `go generate ./internal/api` refreshes it and a Go test fails when it drifts):

- `generated/` (committed, don't edit): `pnpm generate:api` runs orval (`orval.config.ts`) and writes one fetch function per operation to `generated/endpoints/<tag>/` and one type per schema to `generated/model/`. Run it after the spec changes and commit the result; types are the contract, so drift shows up in `pnpm typecheck`.
- `orvalFetch.ts`: the generated functions' mutator. It resolves paths against the base path, sends `X-CSRF-Token` (from `GET /auth/state`) on writes, resolves to the response body and throws `ApiError` with the parsed problem+json (`error.fieldErrors` maps validation errors to form fields).
- One module per resource (`auth`, `overview`, `kodi` (instances and libraries), `devices` (and rooms), `skill`, `requests`, `library` (match and aliases), `settings`) wraps the generated functions in TanStack Query hooks (`useInstances()`, `useUpdateDevice()`…). Mutations invalidate or update the affected queries. Components import types from `@api/generated/model`.
- `client.ts`: `ApiError`, `errorMessage()`, the CSRF token and `buildUrl()` for plain links (config export, skill package) and the event stream.
- `health.ts`: `GET /api/v1/healthz` is served outside the admin API, so it is the one hand-typed call.
- Mutation `meta`: `success` shows a toast; errors toast unless `inline` is set because the form shows them (`src/router.tsx`).
- `events.ts`: one shared `EventSource` for `/api/v1/events` while signed in. Event payloads are typed with the generated models (SSE bodies aren't in the spec; the event names are listed on the `streamEvents` operation). The browser resumes with `Last-Event-ID`. Each event updates the cache: `request` → request log pages and overview, `instance` → instances and overview, `library`, `device`, `deploy` → that deploy's progress, `attention` → overview. `useServerEvent()` subscribes to one event type.
- `keys.ts`: query keys.

## Stack

React 19.3, Vite 8 with `@vitejs/plugin-react`, TypeScript 7 (the native `tsc`), Tailwind CSS 4.3 with `@tailwindcss/vite`, Base UI (`@base-ui/react`), lucide-react, TanStack Router (file-based) and TanStack Query 5, react-hook-form and zod 4, Biome, Vitest (browser mode, with Playwright) and Testing Library, Playwright. Fonts are self-hosted from Fontsource: Bricolage Grotesque, IBM Plex Sans and IBM Plex Mono.

## Brand

The logo is an adapted Go gopher. The Go gopher was designed by Renée French and is licensed under CC BY 4.0. See `docs/brand/ATTRIBUTION.md`.
