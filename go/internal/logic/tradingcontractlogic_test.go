package logic

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	managerpkg "nof0-api/pkg/manager"
	"nof0-api/pkg/repo"
)

func TestTradingContractTradersFromManagerConfig(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		ManagerConfig: &managerpkg.Config{
			Traders: []managerpkg.TraderConfig{
				{
					ID:               "alpha",
					Name:             "Alpha",
					Model:            "model-a",
					ExchangeProvider: "paper",
					MarketProvider:   "market",
					ExecutionMode:    managerpkg.ExecutionModePaper,
					OrderStyle:       managerpkg.OrderStyleLimitIOC,
					AllocationPct:    25,
				},
			},
		},
		ManagerPromptDigests: map[string]string{"alpha": "digest-1"},
	}

	resp, err := NewTradersLogic(context.Background(), svcCtx).Traders(&types.TradersRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Traders, 1)
	require.Equal(t, "alpha", resp.Traders[0].Id)
	require.Equal(t, "configured", resp.Traders[0].Status.Status)

	detail, err := NewTraderDetailLogic(context.Background(), svcCtx).TraderDetail(&types.TraderPathRequest{TraderId: "alpha"})
	require.NoError(t, err)
	require.Equal(t, "digest-1", detail.Trader.PromptDigest)
}

func TestTradingContractControlSkeletonsDoNotAccept(t *testing.T) {
	svcCtx := &svc.ServiceContext{}

	orderResp, err := NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{})
	require.NoError(t, err)
	require.Equal(t, "not_available", orderResp.Status)
	require.Empty(t, orderResp.Orders)

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{TraderId: "alpha"}, "start")
	require.NoError(t, err)
	require.False(t, controlResp.Accepted)
	require.Equal(t, "not_implemented", controlResp.Status)
}

func TestAuditEventsUsesRepositoryContract(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	auditRepo := &fakeAuditEventRepo{
		records: []repo.AuditEventRecord{
			{
				ID:            10,
				Type:          repo.AuditEventOrderSubmitted,
				TraderID:      "alpha",
				CorrelationID: "corr-1",
				Detail:        json.RawMessage(`{"ok":true}`),
				CreatedAt:     createdAt,
			},
		},
	}
	svcCtx := &svc.ServiceContext{AuditEventRepo: auditRepo}

	resp, err := NewAuditEventsLogic(context.Background(), svcCtx).AuditEvents(&types.AuditEventsRequest{
		TraderId:      "alpha",
		Type:          string(repo.AuditEventOrderSubmitted),
		CorrelationId: "corr-1",
		Limit:         5,
		Offset:        2,
	})

	require.NoError(t, err)
	require.Equal(t, repo.AuditEventListFilter{
		TraderID:      "alpha",
		Type:          repo.AuditEventOrderSubmitted,
		CorrelationID: "corr-1",
		Limit:         5,
		Offset:        2,
	}, auditRepo.filter)
	require.Len(t, resp.Events, 1)
	require.Equal(t, int64(10), resp.Events[0].Id)
	require.Equal(t, "audit_event_repo", resp.Meta.Source)
}

func TestAuditEventsNoRepositoryDegradesSafely(t *testing.T) {
	resp, err := NewAuditEventsLogic(context.Background(), &svc.ServiceContext{}).AuditEvents(&types.AuditEventsRequest{})

	require.NoError(t, err)
	require.Empty(t, resp.Events)
	require.Equal(t, "audit_repo_unavailable", resp.Meta.Source)
}

type fakeAuditEventRepo struct {
	filter  repo.AuditEventListFilter
	records []repo.AuditEventRecord
}

func (r *fakeAuditEventRepo) Record(context.Context, repo.AuditEventRecord) (int64, error) {
	return 0, nil
}

func (r *fakeAuditEventRepo) List(ctx context.Context, filter repo.AuditEventListFilter) ([]repo.AuditEventRecord, error) {
	r.filter = filter
	return r.records, nil
}

func (r *fakeAuditEventRepo) ListByTrader(context.Context, string, int) ([]repo.AuditEventRecord, error) {
	return r.records, nil
}
