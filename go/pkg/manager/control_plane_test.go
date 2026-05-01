package manager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestControlPlaneRejectsUnknownTrader(t *testing.T) {
	cp := NewControlPlane(testControlConfig(), nil)

	resp, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "missing",
		Action:   ControlActionStart,
	})

	require.NoError(t, err)
	require.False(t, resp.Accepted)
	require.Equal(t, "rejected", resp.Status)
	require.Contains(t, resp.Message, "not found")
}

func TestControlPlaneAcceptsPaperAndTestnetStart(t *testing.T) {
	cp := NewControlPlane(testControlConfig(), nil)

	paper, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "paper",
		Action:   ControlActionStart,
	})
	require.NoError(t, err)
	require.True(t, paper.Accepted)
	require.Equal(t, "accepted", paper.Status)
	require.Equal(t, TraderStateRunning, paper.State)
	require.True(t, paper.ControlPlaneOnly)
	require.False(t, paper.Queued)

	testnet, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "testnet",
		Action:   ControlActionStart,
	})
	require.NoError(t, err)
	require.True(t, testnet.Accepted)
	require.Equal(t, TraderStateRunning, testnet.State)
}

func TestControlPlaneRejectsLiveStartByDefault(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(allowLiveTradingEnv, "")
	t.Setenv(liveTradingAckEnv, "")
	cp := NewControlPlane(testControlConfig(), nil)

	resp, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "live",
		Action:   ControlActionStart,
	})

	require.NoError(t, err)
	require.False(t, resp.Accepted)
	require.Equal(t, "rejected", resp.Status)
	require.Contains(t, resp.Message, allowLiveTradingEnv)
}

func TestControlPlanePauseResumeStateFlow(t *testing.T) {
	cp := NewControlPlane(testControlConfig(), nil)

	start, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "paper",
		Action:   ControlActionStart,
	})
	require.NoError(t, err)
	require.True(t, start.Accepted)

	pause, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "paper",
		Action:   ControlActionPause,
		Reason:   "operator hold",
	})
	require.NoError(t, err)
	require.True(t, pause.Accepted)
	require.Equal(t, TraderStatePaused, pause.State)

	snap, ok := cp.Snapshot("paper")
	require.True(t, ok)
	require.Equal(t, TraderStatePaused, snap.State)
	require.Equal(t, "operator hold", snap.PauseReason)

	resume, err := cp.Handle(context.Background(), ControlRequest{
		TraderID: "paper",
		Action:   ControlActionResume,
	})
	require.NoError(t, err)
	require.True(t, resume.Accepted)
	require.Equal(t, TraderStateRunning, resume.State)
}

func testControlConfig() *Config {
	return &Config{
		Traders: []TraderConfig{
			{ID: "paper", Name: "Paper", ExchangeProvider: "sim", MarketProvider: "market", ExecutionMode: ExecutionModePaper},
			{ID: "testnet", Name: "Testnet", ExchangeProvider: "hyperliquid-testnet", MarketProvider: "market", ExecutionMode: ExecutionModeTestnet},
			{ID: "live", Name: "Live", ExchangeProvider: "hyperliquid", MarketProvider: "market", ExecutionMode: ExecutionModeLive},
		},
	}
}
