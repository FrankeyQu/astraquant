# AstraQuant API Contract

This document records the phase-2 REST surface for Web Console and external control planes. All new fields use `snake_case`. Persisted timestamps are RFC3339 strings; request/response processing timestamps use Unix milliseconds with a `_ms` suffix.

## Read APIs

| Method | Path | Status | Notes |
| --- | --- | --- | --- |
| `GET` | `/api/traders` | implemented | Lists configured traders from manager config; optional `status`, `execution_mode`, `limit`, `offset`. |
| `GET` | `/api/traders/:traderId` | implemented | Returns config, risk params, exec guards, prompt digest, and runtime status when runtime repo is wired. |
| `GET` | `/api/traders/:traderId/status` | implemented | Returns `configured`, `running`, `stopped`, or `paused`; does not infer live exchange state. |
| `GET` | `/api/audit-events` | read-only | Filters: `trader_id`, `type`, `correlation_id`, `created_after_rfc3339`, `created_before_rfc3339`, `limit`, `offset`. Returns empty with `source=database_unavailable` if DB is not configured. |
| `GET` | `/api/trades` | implemented | Existing trade feed plus optional `trader_id`, `symbol`, `side`, `limit`, `offset` filtering. |
| `GET` | `/api/positions` | implemented | Existing position feed plus optional `trader_id`, `symbol`, `status`, `limit`, `offset`. The file-backed feed currently represents open positions. |
| `GET` | `/api/orders` | contract-only | Returns `status=not_available` and an empty list until order persistence/service wiring exists. |

## Control Drafts

Control endpoints are intentionally not connected to live trading. They return `accepted=false` and `status=not_implemented` until a guarded manager command queue and audit trail are wired.

| Method | Path | Request |
| --- | --- | --- |
| `POST` | `/api/traders/:traderId/start` | `requested_by`, `reason`, optional `idempotency_key`, `correlation_id` |
| `POST` | `/api/traders/:traderId/stop` | same as start |
| `POST` | `/api/traders/:traderId/pause` | same as start plus optional `effective_until` RFC3339 |
| `POST` | `/api/traders/:traderId/resume` | same as start |
| `POST` | `/api/decisions/:decisionId/approve` | `requested_by`, `reason`, optional `idempotency_key`, `correlation_id` |
| `POST` | `/api/decisions/:decisionId/reject` | same as approve |
| `POST` | `/api/orders/preview` | `trader_id`, optional `decision_id`, `correlation_id`, `orders`, `risk_context` |
| `POST` | `/api/orders/:orderId/approve` | `requested_by`, `reason`, optional `idempotency_key`, `correlation_id` |
| `POST` | `/api/orders/:orderId/reject` | same as approve |

Control responses share the shape:

```json
{
  "accepted": false,
  "status": "not_implemented",
  "action": "approve",
  "correlation_id": "optional-correlation-id",
  "message": "control endpoint is contract-only until wired to manager guarded command queue",
  "server_time_ms": 1770000000000
}
```
