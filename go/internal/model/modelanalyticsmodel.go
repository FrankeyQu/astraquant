package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ ModelAnalyticsModel = (*customModelAnalyticsModel)(nil)

// AnalyticsPayloadRow contains the full API-shaped analytics payload for one model.
type AnalyticsPayloadRow struct {
	ModelID      string
	Payload      string
	ServerTimeMs *int64
	Metadata     string
	UpdatedAt    time.Time
}

type (
	// ModelAnalyticsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customModelAnalyticsModel.
	ModelAnalyticsModel interface {
		modelAnalyticsModel
		AllPayloads(ctx context.Context) ([]AnalyticsPayloadRow, error)
		PayloadByModel(ctx context.Context, modelID string) (*AnalyticsPayloadRow, error)
	}

	customModelAnalyticsModel struct {
		*defaultModelAnalyticsModel
	}
)

// NewModelAnalyticsModel returns a model for the database table.
func NewModelAnalyticsModel(conn sqlx.SqlConn, c cache.CacheConf, opts ...cache.Option) ModelAnalyticsModel {
	return &customModelAnalyticsModel{
		defaultModelAnalyticsModel: newModelAnalyticsModel(conn, c, opts...),
	}
}

// AllPayloads returns all materialized analytics payloads ordered by model id.
func (m *customModelAnalyticsModel) AllPayloads(ctx context.Context) ([]AnalyticsPayloadRow, error) {
	const query = `
SELECT
    model_id,
    payload::text AS payload,
    server_time_ms,
    metadata::text AS metadata,
    updated_at,
    created_at
FROM public.model_analytics
WHERE payload IS NOT NULL
ORDER BY model_id ASC`

	var rows []ModelAnalytics
	if err := m.QueryRowsNoCacheCtx(ctx, &rows, query); err != nil {
		return nil, fmt.Errorf("modelAnalytics.AllPayloads query: %w", err)
	}

	out := make([]AnalyticsPayloadRow, 0, len(rows))
	for i := range rows {
		out = append(out, analyticsPayloadRowFromModel(&rows[i]))
	}
	return out, nil
}

// PayloadByModel returns the materialized analytics payload for one model.
func (m *customModelAnalyticsModel) PayloadByModel(ctx context.Context, modelID string) (*AnalyticsPayloadRow, error) {
	const query = `
SELECT
    model_id,
    payload::text AS payload,
    server_time_ms,
    metadata::text AS metadata,
    updated_at,
    created_at
FROM public.model_analytics
WHERE model_id = $1
  AND payload IS NOT NULL
LIMIT 1`

	var row ModelAnalytics
	if err := m.QueryRowNoCacheCtx(ctx, &row, query, modelID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlx.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("modelAnalytics.PayloadByModel query: %w", err)
	}
	out := analyticsPayloadRowFromModel(&row)
	return &out, nil
}

func analyticsPayloadRowFromModel(row *ModelAnalytics) AnalyticsPayloadRow {
	out := AnalyticsPayloadRow{
		ModelID:   row.ModelId,
		Payload:   row.Payload,
		Metadata:  row.Metadata,
		UpdatedAt: row.UpdatedAt,
	}
	if row.ServerTimeMs.Valid {
		value := row.ServerTimeMs.Int64
		out.ServerTimeMs = &value
	}
	return out
}
