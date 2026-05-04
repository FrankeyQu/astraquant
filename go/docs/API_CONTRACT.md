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
| `GET` | `/api/orders` | read-only | Returns a read-only order stream derived from `order_submitted` / `order_failed` audit events; filters: `trader_id`, `symbol`, `status`, `limit`, `offset`. |

## Safe Control Plane

Trader lifecycle endpoints are connected to a safe control-plane state recorder. They validate configured trader IDs and execution modes, then record in-memory state (`running`, `paused`, `stopped`) and mirror it to `TraderRuntimeRepo` when that repository is wired. They do **not** start the manager trading loop, do **not** bypass `manager.ApproveDecision`, and do **not** submit orders.

`paper` and `testnet` traders can be accepted as control-plane state changes. `live` trader `start`/`resume` requires the existing live environment gate (`ASTRAQUANT_ALLOW_LIVE_TRADING=true` and `ASTRAQUANT_LIVE_TRADING_ACK=I_UNDERSTAND_THIS_CAN_LOSE_MONEY`) and is rejected by default.

| Method | Path | Request |
| --- | --- | --- |
| `POST` | `/api/traders/:traderId/start` | `requested_by`, `reason`, optional `idempotency_key`, `correlation_id` |
| `POST` | `/api/traders/:traderId/stop` | same as start |
| `POST` | `/api/traders/:traderId/pause` | same as start plus optional `effective_until` RFC3339 |
| `POST` | `/api/traders/:traderId/resume` | same as start |
| `POST` | `/api/decisions/:decisionId/approve` | optional `trader_id`, `requested_by`, `reason`, `decision`, optional `idempotency_key`, `correlation_id` |
| `POST` | `/api/decisions/:decisionId/reject` | same as approve, but `decision` is optional |
| `POST` | `/api/orders/preview` | `trader_id`, optional `decision_id`, `correlation_id`, `orders`, `risk_context` |
| `POST` | `/api/orders/:orderId/approve` | optional `trader_id`, `requested_by`, `reason`, optional `idempotency_key`, `correlation_id` |
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

Decision and order approve/reject endpoints enqueue control-plane commands and record an audit event when `AuditEventRepo` is available. When the database is configured, commands are persisted in `control_commands`; otherwise the API falls back to the in-memory command queue. They do **not** submit orders, do **not** execute persisted decisions, and do **not** bypass `manager.ApproveDecision`. `idempotency_key` reuses the same queued command for the same target/action/key combination.

Decision approval requests must include a complete `decision` object using snake_case fields such as `symbol`, `action`, `leverage`, `position_size_usd`, `entry_price`, `stop_loss`, `take_profit`, `risk_usd`, `confidence`, `reasoning`, and `invalidation_condition`. The API normalizes that payload into `control_commands.detail.decision` so the guarded worker can replay it through `Manager.ExecuteDecision`. Manager policy then revalidates entry/stop/take-profit direction, reward/risk floor, account sync, asset resolution, leverage update, and exchange response status before any local position is booked.

Decision and order action responses share this safe command shape:

```json
{
  "accepted": true,
  "status": "queued",
  "decision_id": "decision-123",
  "action": "approve",
  "command_id": "cmd-...",
  "correlation_id": "optional-correlation-id-or-command-id",
  "queued": true,
  "control_plane_only": true,
  "submitted": false,
  "message": "control command queued; no order was submitted",
  "server_time_ms": 1770000000000
}
```

`GET /api/orders` is intentionally observational. It reads immutable `order_submitted` / `order_failed` audit events to show submitted/failed order attempts. Queued control commands are a separate control-plane state and are not treated as exchange submissions. The guarded command worker may consume `control_commands`, but it only executes `decision approve` commands that include a complete decision payload; it re-runs manager policy checks through `Manager.ExecuteDecision` before any exchange order can be submitted. Commands missing payloads, direct order commands, and rejected commands terminate safely without submitting orders.

The guarded command worker is disabled by default. Enable it explicitly with `CommandWorker.Enabled=true`; it requires `Postgres`, `Cache`, `LLM`, `Manager`, `Exchange`, and `Market` wiring to be available. When disabled, approve/reject endpoints still queue commands but nothing consumes them.

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
