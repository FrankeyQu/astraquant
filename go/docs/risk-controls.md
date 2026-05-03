# Trading Hard Risk Controls

AI output is treated as trading intent only. The manager must approve the intent and then re-check hard risk limits immediately before submitting an order to the exchange provider.

## Hard Guards

Configured per trader under `risk_params`:

| Field | Meaning |
| --- | --- |
| `allowed_symbols` | Optional symbol whitelist for new opens. Empty means no whitelist. Close actions are not blocked by the whitelist so positions can still be unwound. |
| `max_position_size_usd` | Maximum notional for one new position. |
| `major_coin_leverage` | Maximum leverage for BTC/ETH symbols. |
| `altcoin_leverage` | Maximum leverage for all other symbols. |
| `max_margin_usage_pct` | Maximum projected margin usage after a new position. |
| `max_daily_loss_usd` | Maximum UTC-day equity loss before new opens are blocked. |
| `max_daily_loss_pct` | Maximum UTC-day equity loss as a percentage of the daily starting equity. |
| `max_positions` | Maximum number of open virtual positions for the trader. |
| `min_confidence` | Minimum AI confidence accepted for new opens. |

When both `max_daily_loss_usd` and `max_daily_loss_pct` are set, the stricter positive limit is used.

## Enforcement Points

1. `ApproveDecision` validates the AI intent and creates a short-lived approval token.
2. `executeDecisionWithApproval` syncs account state before opening a position.
3. The manager re-runs hard open-risk checks after account sync and before any exchange submission.

This second check prevents stale approval from bypassing daily loss, margin, or size limits after account state changes.

## Daily Loss State And Circuit Breaker

The UTC daily equity baseline is persisted in `trader_runtime_state.detail.risk.daily`, so a process restart does not reset the daily loss counter.

When a new-open check exceeds `max_daily_loss_usd` or `max_daily_loss_pct`:

- the trader is moved to `paused`;
- `trader_runtime_state.is_running` is written as `false`;
- `detail.risk.circuit` records the trigger date, reason, and timestamp;
- no exchange order is submitted.

Manual `start` / `resume` clears the circuit flag, but it does not bypass risk policy. The next open still refreshes account state and re-runs the hard daily loss check before any exchange submission.

`GET /traders/:traderId/status` exposes `risk_state` with daily start equity, current equity, daily loss, effective daily loss limit, blocked state, block reason, and timestamps.

## Operational Rule

Live trading must remain blocked unless:

- `execution_mode: live` is intentional.
- `ASTRAQUANT_ALLOW_LIVE_TRADING=true`.
- `ASTRAQUANT_LIVE_TRADING_ACK=I_UNDERSTAND_THIS_CAN_LOSE_MONEY`.
- The configured symbol whitelist and daily loss limits are reviewed for the live account.
