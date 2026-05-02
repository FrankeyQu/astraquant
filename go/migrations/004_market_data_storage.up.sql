-- Market data tables and compatibility columns for the persistence service.

CREATE TABLE IF NOT EXISTS market_assets (
    provider TEXT NOT NULL,
    symbol TEXT NOT NULL,
    name TEXT,
    sz_decimals INT,
    max_leverage DOUBLE PRECISION,
    only_isolated BOOLEAN,
    margin_table_id INT,
    is_delisted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS price_latest (
    provider TEXT NOT NULL,
    symbol TEXT NOT NULL,
    price DOUBLE PRECISION NOT NULL,
    ts_ms BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS market_asset_ctx (
    provider TEXT NOT NULL,
    symbol TEXT NOT NULL,
    context JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE IF EXISTS price_ticks
    ADD COLUMN IF NOT EXISTS provider TEXT,
    ADD COLUMN IF NOT EXISTS price DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS ts_ms BIGINT,
    ADD COLUMN IF NOT EXISTS volume DOUBLE PRECISION,
    ADD COLUMN IF NOT EXISTS raw JSONB;

ALTER TABLE IF EXISTS market_assets
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS price_latest
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE IF EXISTS market_asset_ctx
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS idx_market_assets_provider_symbol
    ON market_assets(provider, symbol);

CREATE UNIQUE INDEX IF NOT EXISTS idx_price_latest_provider_symbol
    ON price_latest(provider, symbol);

CREATE UNIQUE INDEX IF NOT EXISTS idx_market_asset_ctx_provider_symbol
    ON market_asset_ctx(provider, symbol);

CREATE INDEX IF NOT EXISTS idx_price_ticks_provider_symbol_ts_ms_desc
    ON price_ticks(provider, symbol, ts_ms DESC);
