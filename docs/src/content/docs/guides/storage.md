---
title: Storage and backups
description: Local, S3-compatible and WebDAV storage, scheduled database backups, the secrets key, and restoring with encore restore.
---

Encore's SQLite database (`encore.db`) always lives in the data directory. The **storage backend** is where everything else goes:

| Key prefix | What |
| --- | --- |
| `Encore/` | Scheduled and manual database backups (`encore-<UTC time>.db.gz`); the folder is [`storage.backups.prefix`](/encore/reference/configuration/#storagebackupsprefix). |
| `library/` | Snapshots of each library's catalog, so voice requests work straight after a restart. |
| `state/` | The live database in [stateless mode](/encore/guides/stateless/). |

Choose the backend under **Settings** → **Storage** or with [`storage`](/encore/reference/configuration/#storage) in config. **Test** writes, reads and deletes a probe object to check the settings.

## Local

The default. Objects are written under `blobs/` in the data directory, or [`storage.local.path`](/encore/reference/configuration/#storagelocalpath). Backups on the same disk as the database protect against mistakes, not against losing the disk, so copy the data directory elsewhere too, or use remote storage.

## S3-compatible

Any S3-compatible service works: AWS S3, MinIO, Silo, Garage, Ceph RGW, SeaweedFS, Cloudflare R2, Backblaze B2 and others. The bucket must already exist.

```yaml title="encore.yml"
storage:
  backend: s3
  s3:
    endpoint: http://minio:9000
    region: us-east-1
    bucket: encore
    prefix: home/ # optional
    addressing: path
    access_key: encore
```

```sh
ENCORE_STORAGE_S3_SECRET_KEY_FILE=/run/secrets/s3_secret_key
```

| Service | `endpoint` | `region` | `addressing` |
| --- | --- | --- | --- |
| MinIO | `http://minio:9000` | `us-east-1` (or your configured region) | `path` |
| Garage | `http://garage:3900` | your Garage `s3_region`, e.g. `garage` | `path` |
| Ceph RGW, SeaweedFS, Silo | your gateway URL | as configured, or empty | `path` |
| Cloudflare R2 | `https://<account-id>.r2.cloudflarestorage.com` | `auto` | `path` |
| Backblaze B2 | `https://s3.<region>.backblazeb2.com` | the bucket's region, e.g. `us-west-004` | `auto` |
| AWS S3 | `https://s3.<region>.amazonaws.com` | the bucket's region | `auto` |

- `addressing` is `auto`, `path` (`https://host/bucket/key`) or `virtual` (`https://bucket.host/key`). Most self-hosted servers need `path`.
- For a private CA, set `ca_bundle` to a PEM file of extra CAs. `insecure_skip_verify` turns certificate checks off; only use it in a lab.
- `sse: s3` or `sse: kms` (with `sse_kms_key_id`) requests server-side encryption where the service supports it.

## WebDAV

Works with Nextcloud, ownCloud and other WebDAV servers. Point `url` at a folder; with Nextcloud or ownCloud use an **app password** rather than your account password.

```yaml title="encore.yml"
storage:
  backend: webdav
  webdav:
    url: https://cloud.example.net/remote.php/dav/files/backup/encore
    username: backup
```

```sh
ENCORE_STORAGE_WEBDAV_PASSWORD_FILE=/run/secrets/webdav_app_password
```

`prefix`, `ca_bundle` and `insecure_skip_verify` work as for S3.

## Backups

Encore writes a consistent, gzipped copy of its database to the storage backend on a schedule, and keeps the newest 14 ([`storage.backups.keep`](/encore/reference/configuration/#storagebackupskeep)) in the `Encore/` folder. Change the folder with **Backup folder** under **Settings** → **Storage**, or [`storage.backups.prefix`](/encore/reference/configuration/#storagebackupsprefix); leave it empty to keep backups at the top of the storage location. The folder sits inside the backend's own location, so with S3 a key reads `<prefix>/Encore/encore-<UTC time>.db.gz`, and clearing only the S3 prefix still leaves the `Encore/` folder. **Run backup now** runs one immediately, as does changing the backend, its location or the folder, so the new place has a copy.

[`storage.backups.schedule`](/encore/reference/configuration/#storagebackupsschedule) takes:

| Schedule | Runs |
| --- | --- |
| `off` | never |
| `hourly` | at the top of every hour |
| `daily@03:00` | every day at 03:00 (the default) |
| `weekly@sun@03:00` | every Sunday at 03:00 (`sun`, `mon`, `tue`, `wed`, `thu`, `fri`, `sat`) |
| `every@6h` | at a fixed interval, at least `1m` |

Times are in the server's local time zone (set `TZ` in the container).

A backup holds everything in the database: settings, Kodi instances, Echo links and rooms, aliases, the request log, the deploy state and the encrypted secrets.

## The secrets key

Kodi passwords, storage credentials, the OIDC client secret, the TOTP secret and the Login with Amazon tokens are encrypted (AES-256-GCM) before they reach the database. **Backups contain them encrypted, but not the key.** Without the key, a restored database can't decrypt them.

- By default Encore generates the key into `secrets.key` in the data directory. Copy that file somewhere safe, separately from your backups.
- Or supply your own 32-byte key, base64-encoded, with `ENCORE_SECRETS_KEY_FILE` (or `secrets.key` / `secrets.key_file` in config). Generate one with `openssl rand -base64 32`. [Stateless deployments](/encore/guides/stateless/) must do this.
- **Settings** → **Secrets** shows where the key comes from and can rotate a generated key. A key from config or a file is rotated by changing it there.

## Restore

`encore restore` replaces the database with a backup from the configured storage. Stop Encore first: `encore serve` holds a lock on `encore.lock` in the data directory, so restore refuses to run while it is serving (and a second `encore serve` on the same data directory won't start either).

```sh
encore restore --list                                # backups in storage, newest first
encore restore --latest                              # restore the newest backup
encore restore encore-20261026T030000.000Z.db.gz     # restore a specific one (key or file name)
encore restore --latest --yes                        # without the confirmation prompt
```

Before anything is replaced the backup is downloaded and checked: it must be a sound SQLite database, its schema must not be newer than this Encore's (an older one is migrated on the next start), and the configured secrets key must decrypt its secrets. The current database is then moved aside as `encore.db.before-restore-<UTC time>` (for example `encore.db.before-restore-20261026T101500Z`) and the backup takes its place. Start Encore again to use it.

`encore restore` asks for confirmation; `--yes` skips it and is required when there is no terminal. The storage settings are taken from config and from what was saved in the web interface, exactly as the server would use them.

In [stateless mode](/encore/guides/stateless/) the backup is uploaded as the saved state instead, keeping the current state as the previous copy (`state/encore.db.prev.gz`). It refuses while a running Encore's heartbeat is less than three minutes old; a clean shutdown releases the storage straight away.

### Restoring in Docker

Stop the container, then run the command in a one-off container with the same volume and environment:

```sh
docker compose stop encore
docker compose run --rm --entrypoint /app/encore encore restore --list
docker compose run --rm --entrypoint /app/encore encore restore --latest
docker compose start encore
```

With plain `docker run`, the same works with `docker run --rm -it -v /path/to/encore:/config --entrypoint /app/encore ghcr.io/nightah/encore restore --latest`.

Overriding the entrypoint skips the image's user setup, so the restored files belong to root until the container next starts, when it hands `/config` back to `PUID`/`PGID`.

## Export and import settings

**Settings** → **Import and export** downloads Encore's settings and Kodi instances as an `encore.yml`, and imports one. Secrets are left out unless you ask for them, which needs a sign-in within the last 15 minutes. Imports add or update Kodi instances by name and report which keys were applied or skipped.
