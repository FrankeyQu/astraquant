package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/pkg/exchange"
	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/market"
)

func TestManagerAssignReleaseVirtualPosition(t *testing.T) {
	m := &Manager{
		traders:        make(map[string]*VirtualTrader),
		positionOwners: make(map[string]string),
	}
	trader := &VirtualTrader{ID: "t1", VirtualPositions: make(map[string]VirtualPosition)}
	m.traders[trader.ID] = trader

	err := m.assignVirtualPosition(trader, VirtualPosition{Symbol: "BTC", Side: "long", Quantity: 1, EntryPrice: 100})
	require.NoError(t, err)
	require.Equal(t, "t1", m.getPositionOwner("BTC"))

	m.releaseVirtualPosition(trader.ID, "BTC")
	require.Equal(t, "", m.getPositionOwner("BTC"))
	trader.mu.RLock()
	_, exists := trader.VirtualPositions[normalizeSymbol("BTC")]
	trader.mu.RUnlock()
	require.False(t, exists)
}

func TestManagerEnforceSecondaryRisk(t *testing.T) {
	m := &Manager{}
	trader := &VirtualTrader{
		ID: "t-risk",
		RiskParams: RiskParameters{
			MaxPositionSizeUSD: 500,
			MaxMarginUsagePct:  30,
		},
		VirtualPositions: make(map[string]VirtualPosition),
	}
	trader.ResourceAlloc = ResourceAllocation{
		CurrentEquityUSD: 1000,
		MarginUsedUSD:    200,
	}

	decision := &executorpkg.Decision{Symbol: "BTC", PositionSizeUSD: 600}
	err := m.enforceSecondaryRisk(trader, decision, 3)
	require.Error(t, err, "should block oversize position")

	decision.PositionSizeUSD = 300
	err = m.enforceSecondaryRisk(trader, decision, 2) // adds 150 margin => 35%
	require.Error(t, err, "should block excessive margin usage")

	decision.PositionSizeUSD = 200
	err = m.enforceSecondaryRisk(trader, decision, 4)
	require.NoError(t, err, "should allow within caps")
}

func TestFilterPositionsForTrader(t *testing.T) {
	m := &Manager{
		positionOwners: map[string]string{"BTC": "t1"},
	}
	positions := []exchange.Position{
		{Coin: "BTC"},
		{Coin: "ETH"},
	}

	mine := m.filterPositionsForTrader("t1", positions)
	require.Len(t, mine, 2, "owner should see assigned and unowned positions")

	other := m.filterPositionsForTrader("t2", positions)
	require.Len(t, other, 1, "other trader should not see BTC")
	require.Equal(t, "ETH", other[0].Coin)
}

func TestRunTradingLoopSkipsInvalidDecisionPayload(t *testing.T) {
	exec := &validationErrorExecutor{}
	ex := &countingExchangeProvider{}
	mkt := &staticMarketProvider{}
	audit := &auditRecordingPersistence{}
	m := &Manager{
		traders: map[string]*VirtualTrader{
			"t-invalid": {
				ID:               "t-invalid",
				Exchange:         "paper_trading",
				ExchangeProvider: ex,
				MarketProvider:   mkt,
				Executor:         exec,
				ExecutionMode:    ExecutionModePaper,
				RiskParams:       RiskParameters{MaxPositions: 3, MaxPositionSizeUSD: 1000},
				ResourceAlloc:    ResourceAllocation{CurrentEquityUSD: 10_000},
				State:            TraderStateRunning,
				DecisionInterval: time.Hour,
				VirtualPositions: make(map[string]VirtualPosition),
				Cooldown:         make(map[string]time.Time),
			},
		},
		positionOwners: make(map[string]string),
		persistence:    audit,
		stopChan:       make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()

	err := m.RunTradingLoop(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 1, exec.calls, "test should exercise one decision cycle")
	require.Zero(t, ex.placeOrders, "invalid decision payload must not reach order execution")
	requireAuditEvent(t, audit.events, AuditEventDecisionValidationFailed)
}

func TestPolicyGatewayRejectsOversizeBeforeExchange(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.RiskParams.MaxPositionSizeUSD = 100
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    &auditRecordingPersistence{},
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "BTC",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      50_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.ErrorContains(t, err, "manager policy")
	require.Zero(t, ex.placeOrders, "policy rejection must happen before exchange submission")
	requireAuditEvent(t, m.persistence.(*auditRecordingPersistence).events, AuditEventPolicyRejected)
}

func TestPolicyGatewayApprovesAndExecutesValidDecision(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	audit := &auditRecordingPersistence{}
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    audit,
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "BTC",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      50_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.NoError(t, err)
	require.Equal(t, 1, ex.placeOrders)
	requireAuditEvent(t, audit.events, AuditEventApproved)
	requireAuditEvent(t, audit.events, AuditEventOrderSubmitted)
}

func TestPolicyGatewayRejectsLiveModeWithoutExplicitAck(t *testing.T) {
	t.Setenv(allowLiveTradingEnv, "")
	t.Setenv(liveTradingAckEnv, "")
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.Exchange = "hyperliquid"
	trader.ExecutionMode = ExecutionModeLive
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "BTC",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      50_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.ErrorContains(t, err, "live trading")
	require.Zero(t, ex.placeOrders, "live gate rejection must happen before exchange submission")
}

func TestPolicyGatewayRejectsPaperModeOnNonPaperProvider(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.Exchange = "hyperliquid"
	trader.ExecutionMode = ExecutionModePaper
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "BTC",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      50_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.ErrorContains(t, err, "execution_mode=paper")
	require.Zero(t, ex.placeOrders, "paper mode must not submit through a non-paper provider")
}

type validationErrorExecutor struct {
	calls int
}

func (e *validationErrorExecutor) GetFullDecision(*executorpkg.Context) (*executorpkg.FullDecision, error) {
	e.calls++
	return &executorpkg.FullDecision{
		Decisions: []executorpkg.Decision{
			{
				Symbol:          "BTC",
				Action:          "open_long",
				PositionSizeUSD: 500,
				EntryPrice:      50_000,
				Leverage:        2,
				Confidence:      90,
			},
		},
	}, errors.New("validation failed")
}

func (e *validationErrorExecutor) UpdatePerformance(*executorpkg.PerformanceView) {}

func (e *validationErrorExecutor) GetConfig() *executorpkg.Config {
	return &executorpkg.Config{}
}

type countingExchangeProvider struct {
	placeOrders int
}

func (p *countingExchangeProvider) PlaceOrder(context.Context, exchange.Order) (*exchange.OrderResponse, error) {
	p.placeOrders++
	return &exchange.OrderResponse{Status: "ok"}, nil
}

func (p *countingExchangeProvider) CancelOrder(context.Context, int, int64) error { return nil }

func (p *countingExchangeProvider) GetOpenOrders(context.Context) ([]exchange.OrderStatus, error) {
	return nil, nil
}

func (p *countingExchangeProvider) GetPositions(context.Context) ([]exchange.Position, error) {
	return nil, nil
}

func (p *countingExchangeProvider) ClosePosition(context.Context, string) (*exchange.OrderResponse, error) {
	return &exchange.OrderResponse{Status: "ok"}, nil
}

func (p *countingExchangeProvider) UpdateLeverage(context.Context, int, bool, int) error {
	return nil
}

func (p *countingExchangeProvider) GetAccountState(context.Context) (*exchange.AccountState, error) {
	return &exchange.AccountState{
		MarginSummary: exchange.MarginSummary{
			AccountValue:    "10000",
			TotalMarginUsed: "0",
		},
	}, nil
}

func (p *countingExchangeProvider) GetAccountValue(context.Context) (float64, error) {
	return 10000, nil
}

func (p *countingExchangeProvider) GetAssetIndex(context.Context, string) (int, error) {
	return 0, nil
}

type staticMarketProvider struct{}

func (p *staticMarketProvider) Snapshot(context.Context, string) (*market.Snapshot, error) {
	return &market.Snapshot{Symbol: "BTC", Price: market.PriceInfo{Last: 50_000}}, nil
}

func (p *staticMarketProvider) ListAssets(context.Context) ([]market.Asset, error) {
	return []market.Asset{{Symbol: "BTC", IsActive: true, RawMetadata: map[string]any{"maxLeverage": 50}}}, nil
}

func testPolicyTrader(ex exchange.Provider, mkt market.Provider) *VirtualTrader {
	return &VirtualTrader{
		ID:               "t-policy",
		Exchange:         "paper_trading",
		ExchangeProvider: ex,
		MarketProvider:   mkt,
		ExecutionMode:    ExecutionModePaper,
		RiskParams: RiskParameters{
			MaxPositions:       3,
			MaxPositionSizeUSD: 1000,
			MaxMarginUsagePct:  80,
			MajorCoinLeverage:  5,
			AltcoinLeverage:    3,
			MinConfidence:      50,
		},
		ResourceAlloc:    ResourceAllocation{CurrentEquityUSD: 10_000},
		State:            TraderStateRunning,
		VirtualPositions: make(map[string]VirtualPosition),
		Cooldown:         make(map[string]time.Time),
	}
}

type auditRecordingPersistence struct {
	events []AuditEvent
}

func (p *auditRecordingPersistence) RecordAuditEvent(_ context.Context, event AuditEvent) error {
	p.events = append(p.events, event)
	return nil
}

func (p *auditRecordingPersistence) RecordPositionEvent(context.Context, PositionEvent) error {
	return nil
}

func (p *auditRecordingPersistence) RecordDecisionCycle(context.Context, DecisionCycleRecord) error {
	return nil
}

func (p *auditRecordingPersistence) RecordAccountSnapshot(context.Context, AccountSyncSnapshot) error {
	return nil
}

func (p *auditRecordingPersistence) RecordAnalytics(context.Context, AnalyticsSnapshot) error {
	return nil
}

func (p *auditRecordingPersistence) HydrateCaches(context.Context, []string) error {
	return nil
}

func requireAuditEvent(t *testing.T, events []AuditEvent, eventType AuditEventType) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			require.NotEmpty(t, event.TraderID)
			require.NotEmpty(t, event.CorrelationID)
			return
		}
	}
	require.Failf(t, "missing audit event", "event_type=%s events=%v", eventType, events)
}
