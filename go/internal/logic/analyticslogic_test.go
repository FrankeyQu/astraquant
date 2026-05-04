package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsDBPath(t *testing.T) {
	updated := time.Date(2026, 5, 4, 1, 2, 3, 0, time.UTC)
	source := &fakeModelAnalyticsModel{
		rows: []model.AnalyticsPayloadRow{
			{
				ModelID:   "gpt-5",
				Payload:   analyticsPayload(t, "gpt-5", 1761452304.432, 9, 0.64),
				UpdatedAt: updated,
			},
			{
				ModelID:   "claude-sonnet-4-5",
				Payload:   analyticsPayload(t, "claude-sonnet-4-5", 0, 4, 0.71),
				UpdatedAt: updated,
			},
		},
	}
	logic := NewAnalyticsLogic(context.Background(), &svc.ServiceContext{
		ModelAnalyticsModel: source,
	})

	resp, err := logic.Analytics()
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Analytics, 2)
	require.NotZero(t, resp.ServerTime)
	require.Len(t, source.allQueries, 1)

	assert.Equal(t, "claude-sonnet-4-5", resp.Analytics[0].ModelId)
	assert.Equal(t, "gpt-5", resp.Analytics[1].ModelId)
	assert.Equal(t, 9, resp.Analytics[1].SignalsBreakdownTable.TotalSignals)
	assert.Equal(t, 0.64, resp.Analytics[1].SignalsBreakdownTable.AvgConfidence)
	assert.InDelta(t, float64(updated.Unix()), resp.Analytics[0].UpdatedAt, 0.001)
}

func TestModelAnalyticsDBPath(t *testing.T) {
	source := &fakeModelAnalyticsModel{
		byModel: map[string]model.AnalyticsPayloadRow{
			"gpt-5": {
				ModelID: "gpt-5",
				Payload: wrappedAnalyticsPayload(t, map[string]any{
					"id":       "gpt-5",
					"model_id": "gpt-5",
					"signals_breakdown_table": map[string]any{
						"total_signals": 12,
					},
				}),
				UpdatedAt: time.Date(2026, 5, 4, 1, 2, 3, 0, time.UTC),
			},
		},
	}
	logic := NewModelAnalyticsLogic(context.Background(), &svc.ServiceContext{
		ModelAnalyticsModel: source,
	})

	resp, err := logic.ModelAnalytics("gpt-5")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotZero(t, resp.ServerTime)
	require.Equal(t, []string{"gpt-5"}, source.modelQueries)
	assert.Equal(t, "gpt-5", resp.Analytics.ModelId)
	assert.Equal(t, 12, resp.Analytics.SignalsBreakdownTable.TotalSignals)
}

func TestModelAnalyticsDBPathMissingReturnsEmptyModel(t *testing.T) {
	source := &fakeModelAnalyticsModel{}
	logic := NewModelAnalyticsLogic(context.Background(), &svc.ServiceContext{
		ModelAnalyticsModel: source,
	})

	resp, err := logic.ModelAnalytics("missing-model")
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "missing-model", resp.Analytics.ModelId)
	assert.NotZero(t, resp.ServerTime)
}

func TestAnalyticsDBPathInvalidPayload(t *testing.T) {
	source := &fakeModelAnalyticsModel{
		rows: []model.AnalyticsPayloadRow{
			{ModelID: "broken", Payload: "{not-json"},
		},
	}
	logic := NewAnalyticsLogic(context.Background(), &svc.ServiceContext{
		ModelAnalyticsModel: source,
	})

	resp, err := logic.Analytics()
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.Analytics)
	assert.Contains(t, err.Error(), "broken")
}

func analyticsPayload(t *testing.T, modelID string, updatedAt float64, totalSignals int, avgConfidence float64) string {
	t.Helper()
	return rawAnalyticsPayload(t, map[string]any{
		"id":         modelID,
		"model_id":   modelID,
		"updated_at": updatedAt,
		"signals_breakdown_table": map[string]any{
			"total_signals":  totalSignals,
			"avg_confidence": avgConfidence,
		},
	})
}

func wrappedAnalyticsPayload(t *testing.T, analytics map[string]any) string {
	t.Helper()
	return rawAnalyticsPayload(t, map[string]any{
		"analytics": analytics,
	})
}

func rawAnalyticsPayload(t *testing.T, payload map[string]any) string {
	t.Helper()
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return string(b)
}

type fakeModelAnalyticsModel struct {
	rows         []model.AnalyticsPayloadRow
	byModel      map[string]model.AnalyticsPayloadRow
	allQueries   []string
	modelQueries []string
}

func (m *fakeModelAnalyticsModel) AllPayloads(context.Context) ([]model.AnalyticsPayloadRow, error) {
	m.allQueries = append(m.allQueries, "all")
	out := make([]model.AnalyticsPayloadRow, len(m.rows))
	copy(out, m.rows)
	return out, nil
}

func (m *fakeModelAnalyticsModel) PayloadByModel(_ context.Context, modelID string) (*model.AnalyticsPayloadRow, error) {
	m.modelQueries = append(m.modelQueries, modelID)
	if m.byModel == nil {
		return nil, nil
	}
	row, ok := m.byModel[modelID]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (m *fakeModelAnalyticsModel) QueryRowsNoCacheCtx(context.Context, any, string, ...any) error {
	return fmt.Errorf("unexpected QueryRowsNoCacheCtx call")
}

func (m *fakeModelAnalyticsModel) QueryRowNoCacheCtx(context.Context, any, string, ...any) error {
	return fmt.Errorf("unexpected QueryRowNoCacheCtx call")
}
