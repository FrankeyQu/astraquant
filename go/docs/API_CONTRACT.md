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

## Safe Control Plane

Trader lifecycle endpoints are connected to a safe control-plane state recorder. They validate configured trader IDs and execution modes, then record in-memory state (`running`, `paused`, `stopped`) and mirror it to `TraderRuntimeRepo` when that repository is wired. They do **not** start the manager trading loop, do **not** bypass `manager.ApproveDecision`, and do **not** submit orders.

`paper` and `testnet` traders can be accepted as control-plane state changes. `live` trader `start`/`resume` requires the existing live environment gate (`ASTRAQUANT_ALLOW_LIVE_TRADING=true` and `ASTRAQUANT_LIVE_TRADING_ACK=I_UNDERSTAND_THIS_CAN_LOSE_MONEY`) and is rejected by default.

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

Trader lifecycle responses share the shape:

```json
{
  "accepted": true,
  "status": "accepted",
  "trader_id": "paper-alpha",
  "action": "start",
  "correlation_id": "optional-correlation-id",
  "control_state": "running",
  "execution_mode": "paper",
  "queued": false,
  "control_plane_only": true,
  "message": "control state recorded; no trading loop was started and no orders were submitted",
  "server_time_ms": 1770000000000
}
```

Decision and order approve/reject endpoints remain intentionally safe placeholders until a real guarded queue exists. They return `accepted=false`, `status=not_implemented`, and `queued=false`.

Order preview is preview-only. It normalizes order shape, returns policy-style checks, and never submits or queues an order:

```json
{
  "accepted": true,
  "status": "preview_only",
  "preview_id": "preview-...",
  "correlation_id": "optional-correlation-id",
  "submitted": false,
  "checks": {
    "control_plane_only": true,
    "submitted": false,
    "overall": "preview_only",
    "normalized_orders": [
      {
        "symbol": "ETH",
        "side": "buy",
        "type": "limit",
        "quantity": 1.25,
        "limit_price": 3100
      }
    ]
  },
  "message": "preview only; no order was submitted or queued",
  "server_time_ms": 1770000000000
}
```
