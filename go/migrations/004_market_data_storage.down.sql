DROP INDEX IF EXISTS idx_price_ticks_provider_symbol_ts_ms_desc;
DROP INDEX IF EXISTS idx_market_asset_ctx_provider_symbol;
DROP INDEX IF EXISTS idx_price_latest_provider_symbol;
DROP INDEX IF EXISTS idx_market_assets_provider_symbol;

ALTER TABLE IF EXISTS price_ticks
    DROP COLUMN IF EXISTS raw,
    DROP COLUMN IF EXISTS volume,
    DROP COLUMN IF EXISTS ts_ms,
    DROP COLUMN IF EXISTS price,
    DROP COLUMN IF EXISTS provider;

DROP TABLE IF EXISTS market_asset_ctx CASCADE;
DROP TABLE IF EXISTS price_latest CASCADE;
DROP TABLE IF EXISTS market_assets CASCADE;
