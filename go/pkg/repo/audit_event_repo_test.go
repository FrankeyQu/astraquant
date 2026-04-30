package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/model"
)

func TestAuditEventRepositoryRecordNormalizesContract(t *testing.T) {
	fake := &fakeAuditEventsModel{}
	repository := NewAuditEventRepository(fake)
	createdAt := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)

	id, err := repository.Record(context.Background(), AuditEventRecord{
		Type:            AuditEventOrderSubmitted,
		TraderID:        " trader-a ",
		CorrelationID:   "corr-1",
		Symbol:          "eth",
		Action:          "OPEN_LONG",
		ModelID:         "model-1",
		PromptDigest:    "sha256:abc",
		ApprovalTokenID: "tok-1",
		Detail:          json.RawMessage(`{"b":2,"a":1}`),
		CreatedAt:       createdAt,
	})

	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	require.NotNil(t, fake.inserted)
	require.Equal(t, AuditEventOrderSubmitted, AuditEventType(fake.inserted.EventType))
	require.Equal(t, "trader-a", fake.inserted.TraderId)
	require.Equal(t, "ETH", fake.inserted.Symbol.String)
	require.Equal(t, "open_long", fake.inserted.Action.String)
	require.Equal(t, `{"a":1,"b":2}`, fake.inserted.Detail)
	require.Equal(t, createdAt, fake.inserted.CreatedAt)
}

func TestAuditEventRepositoryRequiresTraceKey(t *testing.T) {
	repository := NewAuditEventRepository(&fakeAuditEventsModel{})

	_, err := repository.Record(context.Background(), AuditEventRecord{
		Type:     AuditEventDecisionGenerated,
		TraderID: "trader-a",
	})

	require.ErrorContains(t, err, "cycle id or correlation id required")
}

func TestAuditEventRepositoryRejectsUnknownType(t *testing.T) {
	repository := NewAuditEventRepository(&fakeAuditEventsModel{})

	_, err := repository.Record(context.Background(), AuditEventRecord{
		Type:          AuditEventType("unknown"),
		TraderID:      "trader-a",
		CorrelationID: "corr-1",
	})

	require.ErrorContains(t, err, "unsupported event type")
}

func TestAuditEventRepositoryListByTrader(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	repository := NewAuditEventRepository(&fakeAuditEventsModel{
		listRows: []*model.AuditEvents{
			{
				Id:            7,
				EventType:     string(AuditEventPolicyRejected),
				TraderId:      "trader-a",
				CorrelationId: sql.NullString{String: "corr-1", Valid: true},
				Reason:        sql.NullString{String: "risk_limit", Valid: true},
				Detail:        `{"limit":1}`,
				CreatedAt:     createdAt,
			},
		},
	})

	events, err := repository.ListByTrader(context.Background(), "trader-a", 10)

	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(7), events[0].ID)
	require.Equal(t, AuditEventPolicyRejected, events[0].Type)
	require.Equal(t, "risk_limit", events[0].Reason)
	require.Equal(t, createdAt, events[0].CreatedAt)
}

type fakeAuditEventsModel struct {
	inserted *model.AuditEvents
	listRows []*model.AuditEvents
	err      error
}

func (m *fakeAuditEventsModel) Insert(ctx context.Context, data *model.AuditEvents) (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	if data == nil {
		return 0, errors.New("nil data")
	}
	m.inserted = data
	return 42, nil
}

func (m *fakeAuditEventsModel) ListByTrader(ctx context.Context, traderID string, limit int) ([]*model.AuditEvents, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.listRows, nil
}
