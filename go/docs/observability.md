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

## Deployment Assets

Operational alert definitions live in:

- `deploy/observability/alert-rules.yaml`
- `deploy/observability/expvar-scrape.example.yaml`
- `scripts/check_observability.sh`

The alert file is vendor-neutral because the service currently emits expvar JSON. A collector can translate the rules into Prometheus, Grafana, Datadog, or another backend.

Minimum production alerts:

| Alert | Severity | Trigger |
| --- | --- | --- |
| `nof0_api_down` | critical | `/healthz` is not HTTP 200 for 1 minute. |
| `nof0_api_not_ready` | critical | `/readyz` is not HTTP 200 for 2 minutes. |
| `nof0_db_write_errors` | critical | Any `db_writes_total` `|error` counter increases over 5 minutes. |
| `nof0_persistence_avg_latency_high` | warning | Average DB write latency is above 1.5 seconds over 10 minutes. |
| `nof0_cache_errors` | warning | Cache error counters increase by at least 5 over 5 minutes. |
| `nof0_consistency_cache_hit_ratio_low` | warning | Consistency cache-read hit ratio is below 95 percent over 10 minutes. |
| `nof0_market_inconsistencies_sustained` | critical | Consistency counters increase by at least 10 over 15 minutes. |

Operational rule: DB write failures and sustained market-data inconsistencies should block live automated trading until the root cause is understood.

Manual probe:

```bash
NOF0_BASE_URL=http://127.0.0.1:8888 scripts/check_observability.sh
```
