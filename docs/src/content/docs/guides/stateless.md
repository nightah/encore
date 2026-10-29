---
title: Stateless deployment
description: Run Encore without a persistent volume, keeping its database in S3-compatible or WebDAV storage.
---

Encore can run without a persistent volume, like Kanzi's cloud deployments: on Kubernetes without a PersistentVolume, Cloud Run, Fly, Heroku-style hosts, or a plain container with no `/config` mount.

In stateless mode Encore uses a temporary data directory. At startup it restores its database from the storage backend (or starts fresh if there's none yet), saves any changes back every minute and once more on shutdown, so configuration, Amazon tokens and Echo links survive restarts.

## Requirements

- [`stateless: true`](/encore/reference/configuration/#stateless).
- A remote [storage backend](/encore/guides/storage/): S3-compatible (MinIO, Silo, Garage, Ceph, R2, B2, AWS, …) or WebDAV. Local storage is refused.
- The secrets key from outside, with `ENCORE_SECRETS_KEY` or `ENCORE_SECRETS_KEY_FILE`. A generated key would be lost on restart, and every stored secret with it. Generate one with `openssl rand -base64 32`.

## Declare everything as code

A fresh start should need nothing else, so declare Kodi instances, rooms and Echo links in config:

```yaml title="encore.yml"
stateless: true
storage:
  backend: s3
  s3:
    endpoint: https://minio.example.net
    region: us-east-1
    bucket: encore
    addressing: path
    access_key: encore
kodi:
  - name: Lounge
    url: http://192.168.1.20:8080/jsonrpc
    websocket_url: ws://192.168.1.20:9090/jsonrpc
    username: kodi
    default: true
rooms:
  - name: Lounge
    kodi: Lounge
    aliases: [living room]
    devices: [TV]
devices:
  - id: amzn1.ask.device.EXAMPLE-DEVICE-ID
    name: Lounge Echo Show
    kodi: Lounge
    room: Lounge
```

Pass the secrets as files or environment variables rather than writing them into the config:

```sh
ENCORE_SECRETS_KEY_FILE=/run/secrets/encore_key
ENCORE_STORAGE_S3_SECRET_KEY_FILE=/run/secrets/s3_secret_key
```

Kodi instances in a list can't be set through environment variables, so either keep the Kodi password in the config file (mounted as a secret) or enter it once in the web interface; it is then saved, encrypted, with the database.

Settings changed in the web interface are saved too, since they live in the database.

## Without a config file

With no file to mount, environment variables cover everything except the lists (`kodi`, `rooms`, `devices`):

```sh
ENCORE_STATELESS=true
ENCORE_STORAGE_BACKEND=s3
ENCORE_STORAGE_S3_ENDPOINT=https://minio.example.net
ENCORE_STORAGE_S3_REGION=us-east-1
ENCORE_STORAGE_S3_BUCKET=encore
ENCORE_STORAGE_S3_ADDRESSING=path
ENCORE_STORAGE_S3_ACCESS_KEY=encore
ENCORE_STORAGE_S3_SECRET_KEY_FILE=/run/secrets/s3_secret_key
ENCORE_SECRETS_KEY_FILE=/run/secrets/encore_key
```

Add Kodi instances, rooms and Echo links in the web interface; they are kept in the saved database.

## One copy per storage location

Run one Encore per storage location; two would overwrite each other's database. Encore keeps an owner marker with a heartbeat in `state/owner.json` and warns in the log when it sees another copy, but object stores offer no reliable lock, so it can't prevent it. A clean shutdown releases the marker straight away; after a crash it goes stale within a few minutes.

## Objects in storage

| Key | What |
| --- | --- |
| `state/encore.db.gz` | The current database. |
| `state/encore.db.prev.gz` | The copy before the last save. |
| `state/owner.json` | The owner marker. |

Scheduled [backups](/encore/guides/storage/#backups) still run as usual, into the backup folder (`Encore/` unless changed). [`encore restore`](/encore/guides/storage/#restore) in stateless mode uploads the chosen backup as `state/encore.db.gz`, keeping the current one as the previous copy; it refuses while another Encore's heartbeat is fresh.
