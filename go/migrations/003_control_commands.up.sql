-- Durable control-plane commands for decision/order approve-reject requests.

CREATE TABLE IF NOT EXISTS control_commands (
    id TEXT PRIMARY KEY,
    command_type TEXT NOT NULL,
    target TEXT NOT NULL CHECK (target IN ('decision', 'order')),
    decision_id TEXT,
    order_id TEXT,
    trader_id TEXT,
    action TEXT NOT NULL CHECK (action IN ('approve', 'reject')),
    requested_by TEXT NOT NULL,
    reason TEXT NOT NULL,
    idempotency_key TEXT,
    correlation_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (
        status IN ('queued', 'processing', 'completed', 'failed', 'cancelled')
    ),
    queued BOOLEAN NOT NULL DEFAULT TRUE,
    control_plane_only BOOLEAN NOT NULL DEFAULT TRUE,
    submitted BOOLEAN NOT NULL DEFAULT FALSE,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (decision_id IS NOT NULL OR order_id IS NOT NULL),
    CHECK (
        (target = 'decision' AND decision_id IS NOT NULL)
        OR (target = 'order' AND order_id IS NOT NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_control_commands_idempotency
    ON control_commands (
        target,
        COALESCE(decision_id, ''),
        COALESCE(order_id, ''),
        action,
        idempotency_key
    )
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS idx_control_commands_status_created_at
    ON control_commands(status, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_control_commands_trader_created_at_desc
    ON control_commands(trader_id, created_at DESC)
    WHERE trader_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_control_commands_correlation_id
    ON control_commands(correlation_id);
