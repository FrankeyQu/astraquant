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
