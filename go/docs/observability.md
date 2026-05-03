# Observability Endpoints

The HTTP API exposes process liveness, dependency readiness, and expvar metrics on root-level routes. These routes are intentionally outside the `/api` prefix so deployment probes and metric scrapers do not depend on application API routing.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Liveness probe. Returns `200` when the process can serve HTTP. Does not call external dependencies. |
| `GET` | `/readyz` | Readiness probe. Checks configured Postgres and Redis connectivity, plus exchange/market provider wiring. Returns `503` when a required configured dependency fails. |
| `GET` | `/debug/vars` | Standard expvar JSON endpoint. Includes Go runtime variables and persistence metric maps. |
| `GET` | `/metrics` | Alias for the expvar JSON endpoint for scraper compatibility. |

## Readiness Semantics

Dependency checks use these statuses:

| Status | Meaning |
| --- | --- |
| `ok` | Required dependency is configured and the check passed. |
| `disabled` | Dependency is not configured in this process and is not required for readiness. |
| `error` | Required dependency is configured but unavailable or invalid. |
| `degraded` | Overall readiness status when one or more required checks failed. |

Current readiness checks:

- `postgres`: runs `SELECT 1` when `Postgres.DataSource` is configured.
- `redis`: runs `PING` when at least one cache node is configured.
- `market_providers`: validates configured provider map and default provider wiring without making live exchange calls.
- `exchange_providers`: validates configured provider map and default provider wiring without submitting or querying orders.

## Metrics

The persistence layer emits expvar maps:

- `db_writes_total`
- `persistence_latency_seconds`
- `cache_ops_total`
- `inconsistency_counters_total`

Cache hit ratio for consistency reads can be derived from:

```text
cache_ops_total["market.consistency.cache_read|hit"] /
(cache_ops_total["market.consistency.cache_read|hit"] + cache_ops_total["market.consistency.cache_read|miss"])
```

If Prometheus text exposition is required later, add a bridge/exporter instead of changing these application counters in place.
