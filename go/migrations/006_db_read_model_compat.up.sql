-- Compatibility schema for DB-first API read models and persistence hooks.

CREATE TABLE IF NOT EXISTS account_equity_snapshots (
    id BIGSERIAL PRIMARY KEY,
    model_id TEXT NOT NULL,
    ts_ms BIGINT NOT NULL,
    dollar_equity DOUBLE PRECISION NOT NULL DEFAULT 0,
    realized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    total_unrealized_pnl DOUBLE PRECISION NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    cum_pnl_pct DOUBLE PRECISION,
    sharpe_ratio DOUBLE PRECISION,
    since_inception_hourly_marker BIGINT,
    since_inception_minute_marker BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (model_id, ts_ms)
);

CREATE INDEX IF NOT EXISTS idx_account_equity_snapshots_model_ts_desc
    ON account_equity_snapshots(model_id, ts_ms DESC);

CREATE TABLE IF NOT EXISTS conversations (
    id BIGSERIAL PRIMARY KEY,
    model_id TEXT NOT NULL,
    topic TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE IF EXISTS conversation_messages
    ADD COLUMN IF NOT EXISTS content TEXT,
    ADD COLUMN IF NOT EXISTS ts_ms BIGINT,
    ADD COLUMN IF NOT EXISTS metadata TEXT NOT NULL DEFAULT '';

ALTER TABLE IF EXISTS conversation_messages
    ALTER COLUMN trader_id DROP NOT NULL,
    ALTER COLUMN model_id DROP NOT NULL,
    ALTER COLUMN conversation_id DROP NOT NULL,
    ALTER COLUMN conversation_id TYPE BIGINT
        USING CASE
            WHEN conversation_id ~ '^[0-9]+$' THEN conversation_id::BIGINT
            ELSE NULL
        END;

CREATE INDEX IF NOT EXISTS idx_conversation_messages_conversation_id
    ON conversation_messages(conversation_id);

ALTER TABLE IF EXISTS decision_cycles
    ADD COLUMN IF NOT EXISTS model_id TEXT,
    ADD COLUMN IF NOT EXISTS success BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS prompt_digest TEXT,
    ADD COLUMN IF NOT EXISTS cot_trace TEXT,
    ADD COLUMN IF NOT EXISTS decisions TEXT;

ALTER TABLE IF EXISTS decision_cycles
    ALTER COLUMN trader_id DROP NOT NULL;

UPDATE decision_cycles
SET model_id = trader_id
WHERE model_id IS NULL
  AND trader_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_decision_cycles_model_executed_at_desc
    ON decision_cycles(model_id, executed_at DESC);

ALTER TABLE IF EXISTS positions
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ALTER COLUMN symbol_id DROP NOT NULL,
    ALTER COLUMN exchange_provider DROP NOT NULL;

ALTER TABLE IF EXISTS trades
    ADD COLUMN IF NOT EXISTS close_ts_ms BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ALTER COLUMN symbol_id DROP NOT NULL,
    ALTER COLUMN exchange_provider DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_trades_trader_close_ts_ms_desc
    ON trades(trader_id, close_ts_ms DESC);
