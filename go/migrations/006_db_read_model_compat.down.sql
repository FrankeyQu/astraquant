DROP INDEX IF EXISTS idx_trades_trader_close_ts_ms_desc;

ALTER TABLE IF EXISTS trades
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS close_ts_ms;

ALTER TABLE IF EXISTS positions
    DROP COLUMN IF EXISTS updated_at;

DROP INDEX IF EXISTS idx_decision_cycles_model_executed_at_desc;

ALTER TABLE IF EXISTS decision_cycles
    DROP COLUMN IF EXISTS decisions,
    DROP COLUMN IF EXISTS cot_trace,
    DROP COLUMN IF EXISTS prompt_digest,
    DROP COLUMN IF EXISTS config_version,
    DROP COLUMN IF EXISTS success,
    DROP COLUMN IF EXISTS model_id;

DROP INDEX IF EXISTS idx_conversation_messages_conversation_id;

ALTER TABLE IF EXISTS conversation_messages
    ALTER COLUMN conversation_id TYPE TEXT USING conversation_id::TEXT,
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS ts_ms,
    DROP COLUMN IF EXISTS content;

UPDATE conversation_messages
SET conversation_id = id::TEXT
WHERE conversation_id IS NULL;

ALTER TABLE IF EXISTS conversation_messages
    ALTER COLUMN conversation_id SET NOT NULL;

DROP TABLE IF EXISTS conversations CASCADE;

DROP INDEX IF EXISTS idx_account_equity_snapshots_model_ts_desc;
DROP TABLE IF EXISTS account_equity_snapshots CASCADE;
