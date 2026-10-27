---
title: Health checks
description: The health endpoint, the container health check and probes for orchestrators.
---

## `/api/v1/healthz`

`GET /api/v1/healthz` answers `200 OK` with the running version while the server is up. It needs no sign-in and is never cached.

```sh
curl -s http://127.0.0.1:8080/api/v1/healthz
```

```json
{"status":"ok","version":"0.1.0"}
```

With [`server.base_path`](/encore/reference/configuration/#serverbase_path) set, it answers under that prefix like every other route, and at `/api/v1/healthz` as well.

## `encore healthcheck`

`encore healthcheck` calls the health endpoint of the server on the same machine and exits 0 when it answers `200`, or 1 otherwise. It reads the same configuration as `encore serve`, so it finds the right port. The container image uses it as its `HEALTHCHECK` (every 30 seconds, after a 20-second start period):

```sh
docker inspect --format '{{.State.Health.Status}}' encore
```

## Kubernetes

```yaml
livenessProbe:
  httpGet:
    path: /api/v1/healthz
    port: 8080
  periodSeconds: 30
readinessProbe:
  httpGet:
    path: /api/v1/healthz
    port: 8080
  periodSeconds: 10
```

## Metrics

Encore has no metrics endpoint yet. The [request log](/encore/guides/request-log/) and the overview in the web interface show request counts, failures and response times, and Encore's log can be written as JSON ([`log.format: json`](/encore/reference/configuration/#logformat)) for a log pipeline.
