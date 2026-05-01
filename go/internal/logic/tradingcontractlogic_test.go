package logic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	managerpkg "nof0-api/pkg/manager"
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
