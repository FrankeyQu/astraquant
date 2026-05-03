package manager

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/pkg/exchange"
	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/market"
	"nof0-api/pkg/repo"
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

func TestPolicyGatewayRejectsSymbolOutsideWhitelist(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.RiskParams.AllowedSymbols = []string{"BTC"}
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    &auditRecordingPersistence{},
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "ETH",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      3_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.ErrorContains(t, err, "allowed_symbols")
	require.Zero(t, ex.placeOrders, "symbol whitelist rejection must happen before exchange submission")
	requireAuditEvent(t, m.persistence.(*auditRecordingPersistence).events, AuditEventPolicyRejected)
}

func TestPreSubmitRiskRejectsDailyLossAfterAccountSync(t *testing.T) {
	ex := &countingExchangeProvider{accountValue: 9_000}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.RiskParams.MaxDailyLossUSD = 500
	now := time.Now().UTC()
	trader.DailyRisk = DailyRiskState{
		Date:           now.Format("2006-01-02"),
		StartEquityUSD: 10_000,
		LastEquityUSD:  10_000,
		UpdatedAt:      now,
	}
	runtimeRepo := &memoryRuntimeRepo{}
	m := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    &auditRecordingPersistence{},
		runtimeRepo:    runtimeRepo,
	}

	err := m.ExecuteDecision(trader, &executorpkg.Decision{
		Symbol:          "BTC",
		Action:          "open_long",
		PositionSizeUSD: 500,
		EntryPrice:      50_000,
		Leverage:        2,
		Confidence:      90,
	})

	require.ErrorContains(t, err, "daily loss")
	require.Zero(t, ex.placeOrders, "daily loss rejection must happen before exchange submission")
	require.Equal(t, TraderStatePaused, trader.State, "daily loss breach must trip circuit breaker")
	require.True(t, trader.RiskCircuit.Blocked)
	require.NotNil(t, runtimeRepo.state)
	require.NotNil(t, runtimeRepo.state.Detail.Risk)
	require.NotNil(t, runtimeRepo.state.Detail.Risk.Circuit)
	require.True(t, runtimeRepo.state.Detail.Risk.Circuit.Blocked)
	require.False(t, runtimeRepo.state.IsRunning)
	requireAuditEvent(t, m.persistence.(*auditRecordingPersistence).events, AuditEventApproved)
	requireAuditEvent(t, m.persistence.(*auditRecordingPersistence).events, AuditEventPolicyRejected)
}

func TestRuntimeStatePersistsAndHydratesDailyRisk(t *testing.T) {
	now := time.Now().UTC()
	trader := testPolicyTrader(&countingExchangeProvider{}, &staticMarketProvider{})
	trader.DailyRisk = DailyRiskState{
		Date:           now.Format("2006-01-02"),
		StartEquityUSD: 10_000,
		LastEquityUSD:  9_250,
		UpdatedAt:      now,
	}
	trader.RiskCircuit = RiskCircuitState{
		Blocked:     true,
		Date:        trader.DailyRisk.Date,
		Reason:      "daily loss 750.00 exceeds limit 500.00",
		TriggeredAt: now,
	}
	trader.State = TraderStatePaused

	detail := buildRuntimeStateDetail(trader)
	require.NotNil(t, detail.Risk)
	require.NotNil(t, detail.Risk.Daily)
	require.NotNil(t, detail.Risk.Circuit)
	require.True(t, detail.Risk.Circuit.Blocked)

	runtimeRepo := &memoryRuntimeRepo{
		state: &repo.RuntimeStateSnapshot{
			RuntimeStateRecord: repo.RuntimeStateRecord{
				TraderID:            trader.ID,
				ActiveConfigVersion: 7,
				IsRunning:           false,
				Detail:              detail,
			},
			UpdatedAt: now,
		},
	}
	hydrated := testPolicyTrader(&countingExchangeProvider{}, &staticMarketProvider{})
	hydrated.ConfigVersion = 1
	m := &Manager{runtimeRepo: runtimeRepo}

	run, ok := m.hydrateTraderFromState(context.Background(), hydrated)

	require.True(t, ok)
	require.False(t, run)
	require.Equal(t, int64(7), hydrated.ConfigVersion)
	require.Equal(t, trader.DailyRisk.Date, hydrated.DailyRisk.Date)
	require.Equal(t, trader.DailyRisk.StartEquityUSD, hydrated.DailyRisk.StartEquityUSD)
	require.Equal(t, trader.DailyRisk.LastEquityUSD, hydrated.DailyRisk.LastEquityUSD)
	require.True(t, hydrated.RiskCircuit.Blocked)
	require.Equal(t, trader.RiskCircuit.Reason, hydrated.RiskCircuit.Reason)
	require.Equal(t, TraderStatePaused, hydrated.State)
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
	placeOrders  int
	accountValue float64
	marginUsed   float64
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
	accountValue := p.accountValue
	if accountValue <= 0 {
		accountValue = 10000
	}
	return &exchange.AccountState{
		MarginSummary: exchange.MarginSummary{
			AccountValue:    fmt.Sprintf("%.2f", accountValue),
			TotalMarginUsed: fmt.Sprintf("%.2f", p.marginUsed),
		},
	}, nil
}

func (p *countingExchangeProvider) GetAccountValue(context.Context) (float64, error) {
	if p.accountValue > 0 {
		return p.accountValue, nil
	}
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

type memoryRuntimeRepo struct {
	state     *repo.RuntimeStateSnapshot
	upserts   []repo.RuntimeStateRecord
	cooldowns []repo.SymbolCooldownRecord
}

func (r *memoryRuntimeRepo) UpsertState(_ context.Context, record repo.RuntimeStateRecord) error {
	r.upserts = append(r.upserts, record)
	r.state = &repo.RuntimeStateSnapshot{
		RuntimeStateRecord: record,
		UpdatedAt:          time.Now().UTC(),
	}
	return nil
}

func (r *memoryRuntimeRepo) UpsertCooldown(_ context.Context, record repo.SymbolCooldownRecord) error {
	r.cooldowns = append(r.cooldowns, record)
	return nil
}

func (r *memoryRuntimeRepo) GetState(_ context.Context, traderID string) (*repo.RuntimeStateSnapshot, error) {
	if r.state == nil || r.state.TraderID != traderID {
		return nil, nil
	}
	return r.state, nil
}

func (r *memoryRuntimeRepo) ListCooldowns(_ context.Context, traderID string) ([]repo.SymbolCooldownRecord, error) {
	if r.state == nil || r.state.TraderID != traderID {
		return nil, nil
	}
	return r.cooldowns, nil
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
