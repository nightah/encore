---
title: Binary
description: Run Encore from a release archive on Linux, macOS or Windows.
---

Every release on [GitHub](https://github.com/nightah/encore/releases) has archives for:

| OS | Architectures |
| --- | --- |
| Linux | amd64, arm64, armv7 |
| macOS | amd64, arm64 |
| Windows | amd64 |

Archives are named `encore_<version>_<os>_<arch>.tar.gz` (`.zip` for Windows) and contain the `encore` binary with the web interface built in. `checksums.txt` lists their SHA-256 sums, and SBOMs are attached. Each release carries [SLSA Build Level 3](https://slsa.dev) provenance, `encore.intoto.jsonl`, which proves an archive was built from this repository by its release workflow. Check one with [slsa-verifier](https://github.com/slsa-framework/slsa-verifier):

```sh
slsa-verifier verify-artifact --provenance-path encore.intoto.jsonl --source-uri github.com/nightah/encore encore_1.2.3_linux_amd64.tar.gz
```

## Install

```sh
tar -xzf encore_*_linux_amd64.tar.gz
sudo install -m 0755 encore /usr/local/bin/encore
encore version
```

## Data directory

Encore keeps its database, generated secrets key and local storage in a per-user data directory, created on first start with private permissions:

| Platform | Default |
|---|---|
| Linux and other Unix systems | `$XDG_DATA_HOME/encore`, normally `~/.local/share/encore` |
| macOS | `~/Library/Application Support/encore` |
| Windows | `%LocalAppData%\encore` |

So `encore serve` works as an ordinary user with no setup. To use another directory, set `ENCORE_DATA_DIR` or `data_dir` in a config file:

```sh
ENCORE_DATA_DIR=/srv/encore encore serve
```

Encore loads `encore.yml` from the data directory when it exists. Use `--config <path>` (or `ENCORE_CONFIG`) to load a file elsewhere. Only one `encore serve` can use a data directory at a time.

## Run as a systemd service

Release archives include `packaging/systemd/encore.service`, also in the repository at [`packaging/systemd/encore.service`](https://github.com/nightah/encore/blob/main/packaging/systemd/encore.service). It runs Encore as an unprivileged user that systemd creates for it (`DynamicUser=yes`), with `/var/lib/encore` as the data directory (`StateDirectory=encore`), and hardening that leaves Encore only the network.

```sh
sudo install -m 0644 packaging/systemd/encore.service /etc/systemd/system/encore.service
sudo systemctl daemon-reload
sudo systemctl enable --now encore
journalctl -u encore -f
```

The first-run setup token is in the journal. Put settings in `/var/lib/encore/encore.yml`, or add `Environment=ENCORE_...` lines with `sudo systemctl edit encore`. To run `encore restore` against the service's data, stop the service, run it as root with the same data directory, then hand the files back to the service's user:

```sh
sudo systemctl stop encore
sudo ENCORE_DATA_DIR=/var/lib/encore encore restore --latest
sudo chown -R --reference=/var/lib/encore /var/lib/encore/
sudo systemctl start encore
```

## Flags

`encore serve` accepts `--server.address`, `--log.level` and `--log.format`, which override every other source. See the [CLI reference](/encore/reference/cli/).

## Next

Put a [reverse proxy](/encore/guides/reverse-proxy/) in front of Encore, then do the [first run](/encore/getting-started/first-run/).
