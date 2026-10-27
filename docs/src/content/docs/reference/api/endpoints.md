---
title: Endpoints
description: The admin API's endpoints by area, with the shapes of its main resources.
tableOfContents:
  maxHeadingLevel: 2
---

Every path is under `/api/v1` and relative to the base path. Routes need an admin unless marked *public*; writes need the CSRF token (see [conventions](/encore/reference/api/)).

The [Operations](/encore/reference/api/operations/) pages and the Swagger UI at `/api/` have the full request and response schemas.

## Auth and setup

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/auth/state` | *Public.* `{"setup_required", "authenticated", "user": {"name"} \| null, "mode": "local"\|"oidc"\|"header", "csrf_token", "oidc_login_url" \| null, "local_login"}`. `local_login` says whether the password form is available: always in local mode, otherwise only with the break-glass login on (Settings › Access, or `ENCORE_BREAK_GLASS=true`). Sets the per-browser CSRF cookie the token is bound to. |
| POST | `/setup/admin` | *Public, first run only.* `{"setup_token", "username", "password"}` → 204 and a session. 409 once an admin exists. |
| POST | `/auth/login` | *Public.* `{"username", "password", "totp"?}` → 204 and a session. 401 on bad credentials, 429 when locked out. |
| POST | `/auth/logout` | 204. |
| PUT | `/auth/password` | `{"current_password", "new_password"}` → 204; signs out every other session. 401 if the current password is wrong. |
| POST | `/auth/totp/enroll` | → `{"secret", "otpauth_url"}`; takes effect once confirmed. |
| POST | `/auth/totp/confirm` | `{"code"}` → 204, 401 on a wrong code. Afterwards login answers 401 with `errors[0].location: "body.totp"` until a code is sent. |
| POST | `/auth/totp/disable` | `{"code"}` → 204. |
| POST | `/setup/reachability` | `{"url", "setup_token"?}` → `{"url", "checks": [{"name", "level", "detail"}]}`. Checks the public address: `alexa_endpoint` (an unsigned POST to `/api/v1/alexa` should get Encore's 400), `certificate` (trusted, matching, not expiring) and `admin_exposed` (for information: whether other `/api/v1` routes answer there too; it passes either way, with a `note` that they need a sign-in). Levels are `ok`, `warn` or `fail`; a passing check may carry a `note`. Redirects are not followed. Needs an admin or, before the admin account exists, the setup token (403 if wrong). |
| GET | `/auth/oidc/login` | *Public.* Redirects to the OpenID Connect provider. |
| GET | `/auth/oidc/callback` | *Public.* Completes OIDC sign-in and redirects to the web interface. |
| POST | `/client-errors` | *Public.* `{"kind": "render"\|"unhandled", "message", "path", "stack"?}` → 204. The web interface reports its own errors here; they are logged at warn level, at most 20 a minute. |

## Overview

`GET /overview` is the dashboard:

```json
{
  "skill": {"state": "live|not_deployed|deploying|failed", "stage": "development", "locales": ["en-AU"], "needs_deploy": false, "update_reason": "none|settings|library"},
  "endpoint": {"url": "https://encore.example.net/api/v1/alexa", "reachable": true, "latency_ms": 182, "checked_at": "…"},
  "libraries": {"count": 2, "synced_at": "…", "shared_groups": 1},
  "requests_today": {"total": 63, "failed": 2, "median_ms": 160},
  "instances": ["Instance…"],
  "recent_requests": ["RequestSummary…"],
  "attention": [{"kind": "unlinked_device", "title": "…", "detail": "…", "link": "/echo-devices"}]
}
```

Attention kinds: `unlinked_device`, `amazon_not_connected`, `amazon_reauthorise`, `privacy_unanswered`, `skill_not_deployed`, `needs_deploy`, `deploy_failed`, `instance_offline`, `kodi_unauthorized`, `no_instances`.

## Kodi instances

An `Instance`:

```json
{
  "id": 1, "name": "Lounge",
  "url": "http://192.168.1.20:8080/jsonrpc", "websocket_url": "ws://192.168.1.20:9090/jsonrpc",
  "username": "kodi", "has_password": true,
  "mac": "DC:A6:32:00:00:01", "broadcast": "",
  "default_for_unlinked": true, "managed_by_config": false,
  "language": "en", "timezone": "", "shutdown": "inherit|quit|power_off",
  "status": {"state": "connected|http_only|asleep|offline", "kodi_version": "21.3", "api_version": "13.5.0", "last_event_at": "…", "last_sync_at": "…", "next_sync_at": "…"},
  "library": {"group_id": 3, "shared": true, "is_source": true, "movies": 2184, "shows": 312, "episodes": 18406, "songs": 41907, "newest_title": {"title": "Conclave", "year": 2024, "findable": true}},
  "now_playing": {"kind": "episode", "title": "Woe's Hollow", "show": "Severance", "season": 2, "episode": 4, "position_s": 1872, "duration_s": 2943, "paused": false, "art_url": "/api/v1/kodi/instances/1/art?ref=image%3A%2F%2F…"}
}
```

`now_playing` is `null` when nothing plays. `language` picks the title-matching rules, an empty `timezone` is learnt from Kodi's clock, and `shutdown: inherit` follows the `shutdown_quits` feature.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/kodi/instances` | `[Instance]` |
| POST | `/kodi/instances` | `{"name", "url", "websocket_url"?, "username"?, "password"?, "mac"?, "broadcast"?, "language"?, "timezone"?, "shutdown"?}` → 201 `Instance`. Credentials in the URL are moved to `username` and `password`. An empty `websocket_url` disables live updates. |
| GET | `/kodi/instances/{id}` | `Instance` |
| PATCH | `/kodi/instances/{id}` | Any of the POST fields. 409 for instances declared in config. |
| DELETE | `/kodi/instances/{id}` | 204. 409 for instances declared in config. |
| POST | `/kodi/instances/{id}/test` | `{"checks": [{"name": "ping\|websocket\|auth\|library", "ok", "ms", "detail"}]}` |
| POST | `/kodi/instances/{id}/sync` | 202 `{"id"}`: reloads the library. |
| POST | `/kodi/instances/{id}/wake` | 202 `{"id"}`; 409 when no MAC address is known. |
| POST | `/kodi/instances/{id}/default` | 204; makes it the default for unlinked Echos. |
| GET | `/kodi/instances/{id}/art?ref=` | The image for an `image://` reference (as in `now_playing.art_url`), fetched from Kodi with the instance's credentials so the browser needn't reach Kodi. Anything else is refused. |
| GET | `/kodi/discovered` | `[{"name", "host", "http_port", "ws_port", "version"}]` found with mDNS, excluding instances already added. |

## Shared libraries

A `Library`:

```json
{
  "id": 3, "name": "Lounge and Bedroom",
  "members": [{"instance_id": 1, "name": "Lounge"}, {"instance_id": 2, "name": "Bedroom"}],
  "source_instance_id": 1, "syncing_instance_id": 1,
  "state": "solo|likely_shared|confirmed_shared|manual", "evidence": "200 of 200 sampled items match",
  "last_sync_at": "…", "counts": {"movies": 2184, "shows": 312, "songs": 41907}
}
```

`syncing_instance_id` differs from `source_instance_id` while the source is unreachable and another member stands in. See [shared libraries](/encore/guides/shared-libraries/).

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/libraries` | `[Library]` |
| PATCH | `/libraries/{id}` | `{"source_instance_id"}` |
| POST | `/libraries/group` | `{"instance_ids": [1, 2]}` → `Library`. A manual group, which detection never undoes. |
| POST | `/libraries/{id}/split` | `{"instance_id"}` → 204 |
| GET | `/libraries/{id}/recent?limit=20` | `[{"title", "year", "kind", "instance", "added_at", "findable", "in_skill_model"}]` |

## Echo devices and rooms

A `Device`: `{"id", "alexa_device_id", "name", "room_id", "kodi_instance_id", "first_seen", "last_seen", "request_count", "request_count_today"}`. Unlinked devices have `kodi_instance_id: null`.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/devices` | `[Device]` |
| PATCH | `/devices/{id}` | `{"name"?, "room_id"?, "kodi_instance_id"?}` |
| DELETE | `/devices/{id}` | 204 |
| GET | `/rooms` | `[{"id", "name", "kodi_instance_id", "aliases": [{"id", "alias"}]}]` |
| POST | `/rooms` | `{"name", "kodi_instance_id"?}` → 201 |
| PATCH | `/rooms/{id}` | `{"name"?, "kodi_instance_id"?}` |
| DELETE | `/rooms/{id}` | 204 |
| POST | `/rooms/{id}/aliases` | `{"alias"}` → 201 `{"id", "alias"}`; 409 if the spoken name is already used. |
| DELETE | `/rooms/{id}/aliases/{alias_id}` | 204 |

## Alexa skill

`GET /skill`:

```json
{
  "state": "live|not_deployed|deploying|failed",
  "amazon": {"connected": true, "vendor_id": "M2X8…", "profile_client_id": "amzn1.application-oa2-client.…", "has_client_secret": true, "needs_reauthorise": false},
  "skill_id": "amzn1.ask.skill.…",
  "invocation": "encore", "invocation_name": "encore", "skill_name": "Encore",
  "locales_available": ["de-DE", "en-AU", "en-CA", "en-GB", "en-IN", "en-US", "es-ES", "fr-FR", "it-IT"],
  "locales_enabled": ["en-AU"],
  "content": ["movies", "tv", "music"],
  "endpoint": {"url": "https://encore.example.net/api/v1/alexa"},
  "privacy": {"uses_personal_info": false, "is_child_directed": false, "is_export_compliant": true, "contains_ads": false, "allows_purchases": false},
  "last_deploy": {"id": "d-42", "at": "…", "state": "succeeded|failed|in_progress", "hash": "…", "error": "",
    "locales": [{"locale": "en-AU", "status": "succeeded|failed|in_progress", "errors": [], "titles": 3480, "size_bytes": 1174000, "built_at": "…"}],
    "warnings": []},
  "needs_deploy": false, "update_reason": "none|settings|library",
  "model_size": {"locale": "en-AU", "bytes": 1174000, "limit_bytes": 1500000, "titles_in_model": 3480, "titles_server_only": 1206}
}
```

Each `privacy` value is `null` until you answer it, and deploys are refused until all are answered. `invocation` is `encore` (each locale's own name: "encore", or "kodi" in French) or `kodi` ("kodi" everywhere); `invocation_name` is read-only and reports what people say in the first enabled locale. An empty `skill_name` uses each locale's default. `content` is what the skill can play (`music` includes music videos). `update_reason` says why `needs_deploy` is set: the skill's settings changed, or only the library titles in the model.

| Method | Path | Notes |
| --- | --- | --- |
| PUT | `/skill/settings` | `{"invocation", "skill_name", "locales_enabled", "endpoint", "privacy", "content"?}` → the skill, as in `GET /skill`. Omit `content` to keep it. |
| PUT | `/skill/content` | `{"content"}` → the skill, as in `GET /skill`. Saves only what the skill plays, as the setup wizard does before the first Kodi is added so its first sync fetches only that. Library syncs follow it straight away, the skill model on the next deploy. |
| PUT | `/skill/amazon/credentials` | `{"client_id", "client_secret"}` → 204. Omit `client_secret` to keep the stored one. A new client ID forgets the stored authorisation. |
| GET | `/skill/amazon/authorize?return_url=` | `{"url", "return_urls": ["https://…/api/v1/oauth/lwa/callback", "http://127.0.0.1:9090/cb"]}`. Add every `return_urls` entry to the security profile's Allowed Return URLs. The consent page returns to the callback when Encore is reached over HTTPS, otherwise to the local address for the paste flow; `return_url` picks one explicitly. State and the PKCE verifier stay on the server for 15 minutes. 409 until credentials are saved. |
| GET | `/oauth/lwa/callback` | *Public; checked against the pending state.* Completes Login with Amazon and redirects to the Alexa skill page. |
| POST | `/skill/amazon/paste` | `{"redirect_url"}` or `{"refresh_token"}` (the fallbacks) → 204; 422 if Amazon rejects it. |
| DELETE | `/skill/amazon` | Forgets the Login with Amazon tokens → 204. |
| POST | `/skill/deploy` | `{"force"?}` → 202 `{"id"}`. 422 until privacy is answered and the endpoint is `https://`; 409 when Amazon isn't connected or a deploy is running. |
| GET | `/skill/deploy/{id}` | `{"id", "state": "in_progress\|succeeded\|failed", "error", "events": [{"step", "state", "message", "locales": {"en-AU": {"status", "errors"}}, "at"}]}`. Steps are `package`, `vendor`, `skill`, `upload`, `import`, `build`, `enable` and `simulate`; step states are `started`, `succeeded`, `failed` or `skipped`. |
| GET | `/skill/package` | `application/zip`: the skill package with `ask-resources.json`, for a manual upload or `ask deploy`. |

## Request log

A `RequestSummary`: `{"id", "time", "device_name", "device_id", "locale", "intent", "heard", "outcome", "duration_ms", "target", "simulated"}`, where `outcome` is `played`, `asked`, `not_found`, `unlinked`, `error` or `ok`, `device_id` is the Echo's ID in `/devices` or `null` if it has been forgotten, and `simulated` marks a request from Amazon's simulator during a deploy's smoke test.

A `Request` (the detail) adds:

```text
{
  "slots": {},
  "match": {"normalized": "…", "decision": "play|ask|not_found", "margin": 0.12, "target": "…", "error": "",
    "candidates": [{"title", "year", "kind", "media_id", "score", "breakdown": {"alias", "token_set", "jaro_winkler", "osa", "phonetic", "boost"}}]},
  "kodi_calls": [{"method": "Player.Open", "ms": 41, "ok": true}],
  "timing": {"verify_ms": 3, "match_ms": 12, "kodi_ms": 41},
  "speech": "…"
}
```

`kodi_calls` are the JSON-RPC calls the request made, in order. `timing` is `null` for entries logged before Encore recorded it. A logged candidate's `kind` and `media_id` are looked up in the target instance's index when the entry predates them.

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/requests?limit=50&before={id}&device={id}&outcome=&q=` | `{"items": [RequestSummary], "next_before": id \| null}`. `limit` is 1 to 200. |
| GET | `/requests/{id}` | `Request` |
| POST | `/requests/{id}/replay` | A dry run against the current index that doesn't touch Kodi: `{"speech", "match", "would_call": [{"method", "params"}]}` |

## Library and match tester

| Method | Path | Notes |
| --- | --- | --- |
| POST | `/match` | `{"library_id", "query", "kinds"?: ["movie"]}` → `{"normalized", "decision": "play\|ask\|not_found", "margin", "candidates": […]}`, with candidates as in the request log. |
| GET | `/aliases?library_id=` | `[{"id", "library_id", "kind", "media_id", "title", "alias"}]` |
| POST | `/aliases` | `{"library_id", "kind", "media_id", "alias"}` → 201. Used for matching straight away and added to the skill model on the next deploy. |
| DELETE | `/aliases/{id}` | 204 |

## Settings

`GET /settings`:

```json
{
  "auth": {"mode": "local|oidc|header",
    "local": {"username": "admin", "session_hours": 168, "lockout_attempts": 5, "lockout_minutes": 15, "totp_enabled": false},
    "oidc": {"issuer": "", "client_id": "", "has_client_secret": false, "scopes": ["openid", "profile", "email", "groups"], "admin_group": "", "groups_claim": "groups", "redirect_uri": "…/api/v1/auth/oidc/callback", "break_glass": false},
    "header": {"trusted_proxies": ["172.20.0.0/16"], "user_header": "Remote-User", "groups_header": "Remote-Groups", "admin_group": "encore-admins"}},
  "storage": {"backend": "local|s3|webdav",
    "s3": {"endpoint": "", "region": "", "bucket": "", "prefix": "", "addressing": "auto|path|virtual", "access_key": "", "has_secret_key": false, "ca_bundle": "", "insecure_skip_verify": false, "sse": ""},
    "webdav": {"url": "", "username": "", "has_password": false, "prefix": ""},
    "local": {"path": "/config/blobs"},
    "backups": {"schedule": "daily@03:00", "keep": 14, "prefix": "", "last_backup_at": "…"}},
  "secrets": {"key_source": "generated|config|file", "key_id": "v1-…", "rotated_at": "…"},
  "features": {"deep_search": true, "shutdown_quits": false, "confirm_sound": true, "queue_limit": 100},
  "locale": {"timezone": "Australia/Melbourne", "default_locale": "en-AU"},
  "logging": {"level": "info"},
  "server": {"base_path": "/"},
  "managed": ["storage.backend", "auth.header.trusted_proxies"]
}
```

`features` are [`alexa.deep_search`](/encore/reference/configuration/#alexadeep_search), [`alexa.shutdown_quits`](/encore/reference/configuration/#alexashutdown_quits), [`alexa.confirm_sound`](/encore/reference/configuration/#alexaconfirm_sound) and [`alexa.queue_limit`](/encore/reference/configuration/#alexaqueue_limit); `logging.level` and `server.base_path` are [`log.level`](/encore/reference/configuration/#loglevel) and [`server.base_path`](/encore/reference/configuration/#serverbase_path), and apply straight away. Config wins over the web interface, and a Kodi instance's `kodi[].shutdown_quits` wins over both. `backups.schedule` uses the config syntax: `off`, `hourly`, `daily@HH:MM`, `weekly@DAY@HH:MM` or `every@DURATION`.

| Method | Path | Notes |
| --- | --- | --- |
| PATCH | `/settings` | Any part of the above, with secrets as `secret_key`, `password` and `client_secret` (an empty string removes one) → the new settings. Read-only fields may be sent back and are ignored. Keys in `managed` answer 409 and invalid values 422; in both cases nothing is saved. Changing `auth.mode` signs out every other session; switching to `header` from a connection that isn't from a trusted proxy is refused with 409. |
| POST | `/settings/storage/test` | `{"ok", "ms", "error"}`: writes, reads back and deletes a probe object. |
| POST | `/settings/secrets/rotate` | 204. 409 when the key comes from `secrets.key` or a key file; rotate it there. |
| POST | `/settings/backups/run` | 202 `{"id"}` |
| GET | `/config/export?include_secrets=false` | `application/yaml` in `encore.yml` form: auth, storage, `alexa.locales`, trusted proxies and Kodi instances. `include_secrets=true` needs a sign-in within the last 15 minutes (403 otherwise); trusted-header requests always qualify. |
| POST | `/config/import` | A YAML body (`Content-Type: application/yaml`) → `{"applied": […], "skipped": ["key: reason"]}`. Kodi instances are added or updated by name. Invalid values answer 422 and nothing is saved. |

## Events and documents

| Method | Path | Notes |
| --- | --- | --- |
| GET | `/events` | Server-Sent Events; see the [event stream](/encore/reference/api/#event-stream). |
| GET | `/openapi.json` | *Public.* The OpenAPI 3.1 document. |
| GET | `/healthz` | *Public.* `{"status": "ok", "version"}`; see [health checks](/encore/guides/health/). |
