-- Store API-shaped analytics payloads for DB-first analytics endpoints.

CREATE TABLE IF NOT EXISTS model_analytics (
    model_id TEXT PRIMARY KEY,
    payload JSONB NOT NULL,
    server_time_ms BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_model_analytics_updated_at_desc
    ON model_analytics(updated_at DESC);
