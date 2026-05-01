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

func TestTradingContractControlRejectsUnknownTrader(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	orderResp, err := NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{})
	require.NoError(t, err)
	require.Equal(t, "not_available", orderResp.Status)
	require.Empty(t, orderResp.Orders)

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{TraderId: "missing"}, "start")
	require.NoError(t, err)
	require.False(t, controlResp.Accepted)
	require.Equal(t, "rejected", controlResp.Status)
	require.False(t, controlResp.Queued)
}

func TestTradingContractControlPaperStartAccepted(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{
		TraderId:      "paper",
		RequestedBy:   "test",
		Reason:        "smoke",
		CorrelationId: "corr-1",
	}, "start")

	require.NoError(t, err)
	require.True(t, controlResp.Accepted)
	require.Equal(t, "accepted", controlResp.Status)
	require.Equal(t, "running", controlResp.ControlState)
	require.Equal(t, "paper", controlResp.ExecutionMode)
	require.Equal(t, "corr-1", controlResp.CorrelationId)
	require.False(t, controlResp.Queued)
	require.True(t, controlResp.ControlPlaneOnly)

	statusResp, err := NewTraderStatusLogic(context.Background(), svcCtx).TraderStatus(&types.TraderPathRequest{TraderId: "paper"})
	require.NoError(t, err)
	require.Equal(t, "running", statusResp.Status.Status)
	require.True(t, statusResp.Status.IsRunning)
}

func TestTradingContractControlLiveStartRejectedByDefault(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("ASTRAQUANT_ALLOW_LIVE_TRADING", "")
	t.Setenv("ASTRAQUANT_LIVE_TRADING_ACK", "")
	svcCtx := testTradingControlServiceContext()

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{TraderId: "live"}, "start")

	require.NoError(t, err)
	require.False(t, controlResp.Accepted)
	require.Equal(t, "rejected", controlResp.Status)
	require.Equal(t, "live", controlResp.ExecutionMode)
	require.Contains(t, controlResp.Message, "ASTRAQUANT_ALLOW_LIVE_TRADING")
}

func TestTradingContractControlPauseResumeStateFlow(t *testing.T) {
	svcCtx := testTradingControlServiceContext()
	logic := NewTraderControlLogic(context.Background(), svcCtx)

	start, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet"}, "start")
	require.NoError(t, err)
	require.True(t, start.Accepted)

	pause, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet", Reason: "operator pause"}, "pause")
	require.NoError(t, err)
	require.True(t, pause.Accepted)
	require.Equal(t, "paused", pause.ControlState)

	statusResp, err := NewTraderStatusLogic(context.Background(), svcCtx).TraderStatus(&types.TraderPathRequest{TraderId: "testnet"})
	require.NoError(t, err)
	require.Equal(t, "paused", statusResp.Status.Status)
	require.False(t, statusResp.Status.IsRunning)
	require.Equal(t, "operator pause", statusResp.Status.PauseReason)

	resume, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet"}, "resume")
	require.NoError(t, err)
	require.True(t, resume.Accepted)
	require.Equal(t, "running", resume.ControlState)
}

func TestTradingContractOrderPreviewDoesNotSubmitOrders(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	resp, err := NewOrderPreviewLogic(context.Background(), svcCtx).OrderPreview(&types.OrderPreviewRequest{
		TraderId:      "paper",
		DecisionId:    "decision-1",
		CorrelationId: "corr-preview",
		Orders: []types.Order{
			{Symbol: "eth", Side: "buy", Type: "limit", Quantity: 1.25, LimitPrice: 3100},
		},
	})

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "preview_only", resp.Status)
	require.False(t, resp.Submitted)
	require.NotEmpty(t, resp.PreviewId)
	require.Equal(t, "corr-preview", resp.CorrelationId)
	checks, ok := resp.Checks.(orderPreviewChecks)
	require.True(t, ok)
	require.True(t, checks.ControlPlaneOnly)
	require.False(t, checks.Submitted)
	require.Equal(t, "ETH", checks.NormalizedOrders[0].Symbol)
}

func TestTradingContractOrderPreviewRejectsUnknownTrader(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	resp, err := NewOrderPreviewLogic(context.Background(), svcCtx).OrderPreview(&types.OrderPreviewRequest{
		TraderId: "missing",
		Orders:   []types.Order{{Symbol: "btc", Side: "buy", Type: "market", Quantity: 1}},
	})

	require.NoError(t, err)
	require.False(t, resp.Accepted)
	require.Equal(t, "rejected", resp.Status)
	require.False(t, resp.Submitted)
}

func testTradingControlServiceContext() *svc.ServiceContext {
	cfg := &managerpkg.Config{
		Traders: []managerpkg.TraderConfig{
			{ID: "paper", Name: "Paper", ExchangeProvider: "sim", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModePaper},
			{ID: "testnet", Name: "Testnet", ExchangeProvider: "hyperliquid-testnet", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModeTestnet},
			{ID: "live", Name: "Live", ExchangeProvider: "hyperliquid", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModeLive},
		},
	}
	return &svc.ServiceContext{
		ManagerConfig:  cfg,
		ManagerControl: managerpkg.NewControlPlane(cfg, nil),
	}
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
