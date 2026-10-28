---
title: Docker
description: Run Encore with Docker or Docker Compose.
---

Encore ships as a multi-arch container image, `ghcr.io/nightah/encore`, for `linux/amd64`, `linux/arm64` and `linux/arm/v7` (32-bit Raspberry Pi OS). It is built on [antilax3's Alpine s6-overlay base](https://hub.docker.com/r/antilax3/alpine).

## Docker Compose

```yaml title="compose.yaml"
services:
  encore:
    image: ghcr.io/nightah/encore:latest
    container_name: encore
    restart: unless-stopped
    environment:
      PUID: "1000"
      PGID: "1000"
      TZ: Australia/Melbourne
    volumes:
      - ./encore:/config
    ports:
      - "8080:8080"
```

```sh
docker compose up -d
docker compose logs -f encore
```

## docker run

```sh
docker run -d --name encore \
  -e PUID=1000 -e PGID=1000 \
  -p 8080:8080 \
  -v /path/to/encore:/config \
  ghcr.io/nightah/encore
```

## What the image does

- **`PUID` and `PGID`** choose the user and group Encore runs as (default `911`). Use the IDs that should own the files in `/config`; on start the container hands `/config` to that user.
- **`/config`** is the data directory (`ENCORE_DATA_DIR=/config`). It holds the SQLite database (`encore.db`), the generated secrets key (`secrets.key`), local storage (`blobs/`) and an optional `encore.yml`, which is loaded automatically.
- **Port 8080** is the only port, shared by the web interface, the admin API and the Alexa endpoint. Kodi's web server also defaults to 8080, so if Kodi runs on the same machine, publish Encore on another host port (`-p 8081:8080`) or set `ENCORE_SERVER_ADDRESS=:8081`.
- **Health check:** the image runs `encore healthcheck` every 30 seconds, which calls `/api/v1/healthz` on the local port; see [health checks](/encore/guides/health/).
- **Time zone:** backup schedules such as `daily@03:00` use the container's local time, so set `TZ` if you rely on them.

:::danger[Back up the secrets key]
Kodi passwords, storage credentials and your Amazon tokens are encrypted with the key in `/config/secrets.key`. Without it, a restored database is unreadable. Keep a copy somewhere safe, or supply your own key with `ENCORE_SECRETS_KEY_FILE`; see [storage and backups](/encore/guides/storage/#the-secrets-key).
:::

## Configuration

Most settings live in the web interface. To manage them as code, put an `encore.yml` in `/config`, or set `ENCORE_*` environment variables (the key in upper case with dots as underscores, e.g. `ENCORE_LOG_LEVEL=debug` for `log.level`). Variables ending in `_FILE` read their value from a file, which suits Docker secrets:

```yaml title="compose.yaml (excerpt)"
services:
  encore:
    environment:
      ENCORE_SECRETS_KEY_FILE: /run/secrets/encore_key
    secrets:
      - encore_key
secrets:
  encore_key:
    file: ./encore_key.txt # 32 random bytes, base64: openssl rand -base64 32
```

Settings made in config are shown in the web interface as *managed by config*. See the [configuration reference](/encore/reference/configuration/) for every key.

## Networking notes

- **Kodi discovery** uses mDNS, and **Wake-on-LAN** sends broadcast packets. Neither crosses Docker's default bridge network, so add instances by address or run the container with `network_mode: host` (then drop `ports:`). For Wake-on-LAN from a bridge network, setting the instance's broadcast address to your LAN's directed broadcast (for example `192.168.1.255`) may work.
- Encore needs to reach Kodi's web server (port 8080 by default) and, for live updates, Kodi's JSON-RPC WebSocket (port 9090).

## Updating

```sh
docker compose pull && docker compose up -d
```

Image tags: `latest`, the full version (`1.2.3`), `1.2` and, from 1.0, `1`. Pre-releases are tagged only with their full version. `main` follows the main branch: it's rebuilt on every merge, reports `main-<commit>` as its version, and isn't a release, so it has no provenance. Each image carries [SLSA Build Level 3](https://slsa.dev) provenance, signed through [GitHub artifact attestations](https://docs.github.com/en/actions/concepts/security/artifact-attestations) and attached in the registry; check it with the [GitHub CLI](https://cli.github.com), signed in: `gh attestation verify oci://ghcr.io/nightah/encore:1.2.3 --repo nightah/encore`. For a stricter check, add `--signer-workflow nightah/encore/.github/workflows/release-slsa.yml` and `--source-ref refs/tags/v1.2.3`. 0.1.0 and 0.1.1 were attested differently; [SECURITY.md](https://github.com/nightah/encore/blob/main/SECURITY.md#verifying-releases) shows how to check them.

Other tags, such as `fix-abc` or `PR12`, are test builds of unmerged changes ([test images](/encore/contributing/development/#test-images)): they carry no provenance and disappear when the change merges or closes.

## Next

Put a [reverse proxy](/encore/guides/reverse-proxy/) in front of Encore, then do the [first run](/encore/getting-started/first-run/).
