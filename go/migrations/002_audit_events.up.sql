-- Audit events for AI decision, policy, and order traceability.

CREATE TABLE IF NOT EXISTS audit_events (
    id BIGSERIAL PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (
        event_type IN (
            'decision_generated',
            'decision_validation_failed',
            'policy_rejected',
            'approved',
            'order_submitted',
            'order_failed'
        )
    ),
    trader_id TEXT NOT NULL,
    cycle_id BIGINT,
    correlation_id TEXT,
    symbol TEXT,
    action TEXT,
    model_id TEXT,
    model_name TEXT,
    prompt_digest TEXT,
    approval_token_id TEXT,
    reason TEXT,
    error_message TEXT,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (cycle_id IS NOT NULL OR correlation_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_audit_events_trader_created_at_desc
    ON audit_events(trader_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_events_correlation_id
    ON audit_events(correlation_id)
    WHERE correlation_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audit_events_cycle_id
    ON audit_events(cycle_id)
    WHERE cycle_id IS NOT NULL;
