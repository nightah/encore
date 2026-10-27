---
title: Admin API
description: The HTTP API behind Encore's web interface, its conventions, and where to find the OpenAPI document.
---

Encore's web interface is a client of an HTTP API under `/api/v1/`, so anything it does can be scripted. An OpenAPI 3.1 document generated from the server code describes the API, so it can't drift from what the server does.

- **Swagger UI:** every Encore serves an interactive API browser at `/api/` (for example `http://192.168.1.10:8080/api/`). The page is public; **Try it out** uses your web interface session and adds the CSRF token, so sign in first.
- **OpenAPI document:** `GET /api/v1/openapi.json` on any Encore (public), and committed in the repository as [`internal/api/openapi.json`](https://github.com/nightah/encore/blob/main/internal/api/openapi.json).
- **In these docs:** [endpoints by area](/encore/reference/api/endpoints/) with the shapes of the main resources, and a generated page per operation under [Operations](/encore/reference/api/operations/).

:::note[Sign-in required]
Like the web interface, the API needs a sign-in wherever it's reached from, apart from the few public routes below. The [reverse proxy recipes](/encore/guides/reverse-proxy/) show how to expose only the Alexa endpoint, or all of Encore.
:::

All paths are relative to [`server.base_path`](/encore/reference/configuration/#serverbase_path).

## Authentication

Every `/api/v1/` route requires an admin unless it is marked *public*. An admin is either:

- a browser **session**: the `encore_session` cookie set by `POST /api/v1/auth/login`, `POST /api/v1/setup/admin` or the OIDC callback. It is `HttpOnly`, `SameSite=Lax`, and `Secure` when the request came over HTTPS (directly or through a [trusted proxy](/encore/guides/reverse-proxy/#servertrusted_proxies)). Sessions slide: activity extends them up to [`auth.session_hours`](/encore/reference/configuration/#authsession_hours) of inactivity.
- in [trusted-header mode](/encore/guides/sign-in/#trusted-headers), a request from a trusted proxy carrying the user header.

The public routes are `GET /api/v1/auth/state`, `POST /api/v1/auth/login`, `POST /api/v1/setup/admin` (first run only), `POST /api/v1/setup/reachability` (with the setup token before an admin exists), `POST /api/v1/client-errors`, the OIDC login and callback, the Login with Amazon callback, `GET /api/v1/openapi.json`, `GET /api/v1/healthz` and the Alexa endpoint.

## CSRF protection

State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) must send an `X-CSRF-Token` header with the `csrf_token` from `GET /api/v1/auth/state`. The token is bound to a per-browser cookie that the same call sets, so fetch it with the same cookie jar. The server also applies Go's cross-origin protection (`http.CrossOriginProtection`), which refuses cross-site browser requests.

```sh
jar=$(mktemp)
token=$(curl -s -c "$jar" -b "$jar" http://encore.lan:8080/api/v1/auth/state | jq -r .csrf_token)
curl -s -c "$jar" -b "$jar" -H "X-CSRF-Token: $token" -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<your password>"}' http://encore.lan:8080/api/v1/auth/login
curl -s -b "$jar" http://encore.lan:8080/api/v1/kodi/instances
```

## Requests and responses

- JSON field names are `snake_case`.
- Times are RFC 3339 strings in UTC. Durations are integer milliseconds (`*_ms`) or seconds (`*_s`), as the field name says.
- **Creating** an instance, room, room alias or title alias answers `201 Created` with the new resource.
- **Secrets are write-only.** Requests may set a password or key; responses only say whether one is stored, as `has_password`, `has_secret_key`, `has_client_secret` and so on. In a settings patch, an empty string removes a stored secret.
- **Nullable objects** are `null` rather than absent (`oneOf: [T, {"type": "null"}]` in the OpenAPI document); for example an instance's `now_playing` is `null` when nothing plays.

## Errors

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem documents with the media type `application/problem+json`:

```json
{
  "title": "Unprocessable Entity",
  "status": 422,
  "detail": "validation failed",
  "errors": [{"location": "body.url", "message": "must be an http(s) URL such as http://kodi:8080/jsonrpc", "value": "kodi"}]
}
```

| Status | When |
| --- | --- |
| 401 | Not signed in, wrong credentials, or (on login) a TOTP code is needed: `errors[0].location` is `body.totp`. |
| 403 | A missing or invalid `X-CSRF-Token`, a trusted-header user outside the admin group, a wrong setup token, or exporting secrets without a recent sign-in. |
| 409 | A conflict: the value is managed by config, the resource already exists, a deploy is already running, Amazon isn't connected. |
| 422 | Validation failed. `errors` lists each field by `location`. Nothing is saved. |
| 429 | Too many failed sign-ins; the account and address are locked out for a while. |

## Managed by config

Resources and settings that come from `encore.yml` or `ENCORE_*` variables are read-only through the API:

- Kodi instances declared in config carry `managed_by_config: true` and answer `409` to `PATCH` and `DELETE`.
- `GET /api/v1/settings` includes a `managed` list of the dotted paths (as the settings document names them, such as `storage.backend`, `auth.header.trusted_proxies` or `auth.local.session_hours`) that come from config, plus `skill.locales_enabled` when [`alexa.locales`](/encore/reference/configuration/#alexalocales) is set. Changing one answers `409`.

The web interface shows these with a "Managed by config" badge.

## Background work

Syncing a library, waking a Kodi instance, deploying the skill and running a backup start in the background and answer `202 Accepted` with `{"id": "…"}`. Progress arrives on the event stream; deploys can also be followed with `GET /api/v1/skill/deploy/{id}`.

## Event stream

`GET /api/v1/events` is a [Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) stream of live updates. Each event has a monotonic `id`, an `event` name and a JSON `data` payload; a comment heartbeat is sent every 15 seconds.

| `event` | `data` |
| --- | --- |
| `request` | A `RequestSummary`: a new voice request was handled. |
| `instance` | An `Instance`: status, now playing or library counts changed. |
| `library` | A `Library`: a sync finished or grouping changed. |
| `device` | A `Device`: a new or updated Echo. |
| `deploy` | One deploy step (as in `GET /api/v1/skill/deploy/{id}`) with its `deploy_id`. |
| `attention` | The full current attention list from the overview. |

The last 256 events are kept. Reconnect with the `Last-Event-ID` header to resume. A client that falls more than 64 events behind is disconnected and should reconnect the same way.

```sh
curl -N -b "$jar" http://encore.lan:8080/api/v1/events
```
