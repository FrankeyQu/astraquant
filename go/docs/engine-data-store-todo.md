# Engine Data Store – Execution TODO

## P0 – unblock persistence plumbing (Week 0)

1. **Inject persistence and cache dependencies into `pkg/manager`.**
   - Extend `Manager` struct to accept the required `internal/model` repositories (positions, trades, account_equity_snapshots, decision_cycles, model_analytics, trader_state) plus a Redis/cache client interface.
   - Update constructors (`NewManager`, wiring in `internal/svc`) so every `VirtualTrader` can access these collaborators without global state.
2. **Define a dedicated persistence interface/service.**
   - Wrap common DB + Redis operations (e.g., `SavePosition`, `ClosePosition`, `SaveSnapshot`, `PublishTradeEvent`) behind a small interface so manager code stays testable.
   - Decide where transactions live (service vs. call site) and document retry semantics.

## P1 – core trading loop persistence (Week 1–2)

3. [x] **Augment `ExecuteDecision` (open path) with Postgres + Redis writes.**
   - After a successful exchange submission, insert/ upsert into `positions`, include fill metadata, and update the `nof0:positions:{model_id}` hash.
   - Ensure cache-aside ordering (DB first, cache second) and guard against duplicate opens via idempotency keys.
4. [x] **Handle closes with full lifecycle bookkeeping.**
   - On `close_long/close_short`, update `positions` status, insert `trades`, compute realized PnL/fees, and trim Redis caches (`HDEL`, `LPUSH` recent trades, `XADD` stream).
   - Wrap DB work in a transaction to keep position+trade consistent.
5. [x] **Persist decision-cycle/journal data.**
   - Extend `writeJournalRecord` (or a new hook) to insert into `decision_cycles` and refresh `nof0:decision:last:{model_id}` while still writing the JSON journal file.
6. [x] **Implement `SyncTraderPositions` writes.**
   - Batch insert `account_equity_snapshots`, update `model_analytics`, and refresh analytics/leaderboard caches with throttling (e.g., min 5 min between snapshots per trader).

## P2 – market data + consistency (Week 2–3)

7. [x] **Design market-data ingestion path.**
   - Persistence happens through the `market.Persistence` hook injected into `pkg/market` providers, with `internal/ingest.MarketIngestor` driving periodic refreshes.
   - Market writes now cover `market_assets`, `market_asset_ctx`, `price_latest`, `price_ticks`, plus existing write-through market cache keys.
   - Schema compatibility is tracked by `migrations/004_market_data_storage.*.sql`.
8. [x] **Bootstrap cache warm-up + consistency jobs.**
   - Startup now hydrates manager position/trade/decision caches from Postgres via `engine.Service.HydrateCaches`.
   - Startup also hydrates market latest price/context/asset cache keys from `price_latest`, `market_asset_ctx`, and `market_assets`.
   - `internal/consistency.MarketChecker` runs periodically from `cmd/llm` and reports DB vs cache vs live market provider price mismatches without touching order execution.
   - Remediation: inspect `market consistency` warning logs, verify provider health, rerun market ingestion, then rerun cache hydration if cache keys are missing/stale.

## P3 – resilience & observability (Week 3+)

9. **Error handling & retry strategy.**
   - Define which DB/cache failures block execution vs. get queued for async retry, and add structured logging/alerting for each failure class.
10. **Performance & monitoring additions.**
    - Introduce batching (COPY/`INSERT ... VALUES` for ticks, Redis pipelining) once correctness is proven.
    - Emit metrics: `db_writes_total`, `persistence_latency_seconds`, cache hit ratios, inconsistency counters.

> Tracking convention: mark each item as `[ ]` / `[x]` once implemented in code and keep links to the relevant PRs for auditability.
