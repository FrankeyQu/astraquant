package manager_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/logic"
	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	"nof0-api/pkg/confkit"
	"nof0-api/pkg/exchange"
	executorpkg "nof0-api/pkg/executor"
	managerpkg "nof0-api/pkg/manager"
	"nof0-api/pkg/market"
	"nof0-api/pkg/repo"
)

func TestDecisionApprovalControlWorkerSmoke(t *testing.T) {
	store := newSmokeStore()
	exch := &smokeExchangeProvider{}
	mkt := &smokeMarketProvider{}
	mgr := newSmokeManager(t, store, exch, mkt)

	svcCtx := &svc.ServiceContext{
		ControlCommandRepo: &smokeControlRepo{store: store},
		AuditEventRepo:     &smokeAuditRepo{store: store},
		PositionsModel:     &smokePositionsModel{store: store},
	}

	decisionPayload := map[string]interface{}{
		"symbol":                 "BTC",
		"action":                 "open_long",
		"leverage":               3,
		"position_size_usd":      500,
		"entry_price":            50000,
		"stop_loss":              49000,
		"take_profit":            53000,
		"confidence":             88,
		"risk_usd":               100,
		"reasoning":              "smoke test decision",
		"invalidation_condition": "btc loses support",
	}

	resp, err := logic.NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:     "decision-smoke-1",
		TraderId:       "paper-smoke",
		RequestedBy:    "operator",
		Reason:         "smoke approval",
		Decision:       decisionPayload,
		IdempotencyKey: "smoke-idem-1",
		CorrelationId:  "corr-smoke-1",
	}, "approve")
	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "queued", resp.Status)
	require.NotEmpty(t, resp.CommandId)
	require.Equal(t, "corr-smoke-1", resp.CorrelationId)

	require.Len(t, store.repoAudits, 1)
	require.Equal(t, repo.AuditEventApproved, store.repoAudits[0].Type)
	require.Equal(t, "paper-smoke", store.repoAudits[0].TraderID)

	queued := store.snapshotCommand(t, resp.CommandId)
	require.Equal(t, []string{"queued"}, queued.StatusHistory)
	require.Equal(t, repo.ControlCommandStatusQueued, queued.Record.Status)
	require.Contains(t, string(queued.Record.Detail), `"decision"`)

	worker := managerpkg.NewControlCommandWorker(mgr, &smokeControlRepo{store: store}).WithBatchSize(10)
	result, err := worker.ProcessOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 1, result.Completed)
	require.Zero(t, result.Failed)
	require.Zero(t, result.Cancelled)

	completed := store.snapshotCommand(t, resp.CommandId)
	require.Equal(t, repo.ControlCommandStatusCompleted, completed.Record.Status)
	require.Equal(t, []string{"queued", "processing", "completed"}, completed.StatusHistory)
	require.True(t, completed.Record.Submitted)
	require.Equal(t, "decision-smoke-1", completed.Record.DecisionID)

	var terminalDetail map[string]any
	require.NoError(t, json.Unmarshal(completed.Record.Detail, &terminalDetail))
	require.Equal(t, true, terminalDetail["submitted"])
	require.Equal(t, "decision-smoke-1", terminalDetail["decision_id"])
	require.Equal(t, "paper-smoke", terminalDetail["trader_id"])

	require.Equal(t, 1, exch.placeOrders)
	require.Equal(t, 1, exch.setMarkPriceCalls)
	require.Equal(t, 1, exch.setStopLossCalls)
	require.Equal(t, 1, exch.setTakeProfitCalls)
	require.Equal(t, 50000.0, exch.lastMarkPrice)
	require.Equal(t, 1, len(exch.orders))
	require.True(t, exch.orders[0].IsBuy)
	require.Equal(t, "50000.00000000", exch.orders[0].LimitPx)

	require.Len(t, store.managerAudits, 2)
	require.Equal(t, managerpkg.AuditEventApproved, store.managerAudits[0].Type)
	require.Equal(t, managerpkg.AuditEventOrderSubmitted, store.managerAudits[1].Type)

	ordersResp, err := logic.NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{
		TraderId: "paper-smoke",
		Status:   "submitted",
	})
	require.NoError(t, err)
	require.Len(t, ordersResp.Orders, 1)
	require.Equal(t, "paper-smoke", ordersResp.Orders[0].TraderId)
	require.Equal(t, "BTC", ordersResp.Orders[0].Symbol)
	require.Equal(t, "buy", ordersResp.Orders[0].Side)
	require.Equal(t, "submitted", ordersResp.Orders[0].Status)
	require.Equal(t, "limit_ioc", ordersResp.Orders[0].Type)
	require.InDelta(t, 0.01, ordersResp.Orders[0].Quantity, 1e-9)
	require.Equal(t, 50000.0, ordersResp.Orders[0].LimitPrice)

	positionsResp, err := logic.NewPositionsLogic(context.Background(), svcCtx).Positions(&types.PositionsRequest{
		TraderId: "paper-smoke",
		Symbol:   "BTC",
	})
	require.NoError(t, err)
	require.Len(t, positionsResp.AccountTotals, 1)
	require.Equal(t, "paper-smoke", positionsResp.AccountTotals[0].ModelId)
	position, ok := positionsResp.AccountTotals[0].Positions["BTC"]
	require.True(t, ok)
	require.NotZero(t, position.EntryOid)
	require.Equal(t, "BTC", position.Symbol)
	require.Equal(t, 50000.0, position.EntryPrice)
	require.InDelta(t, 0.01, position.Quantity, 1e-9)
	require.Equal(t, 3.0, position.Leverage)
	require.Equal(t, 100.0, position.RiskUsd)
	require.Equal(t, 0.88, position.Confidence)
	require.Greater(t, position.Margin, 0.0)
	require.Equal(t, 50000.0, position.CurrentPrice)
}

func newSmokeManager(t *testing.T, store *smokeStore, exch exchange.Provider, mkt market.Provider) *managerpkg.Manager {
	t.Helper()
	cfg := &managerpkg.Config{
		Manager: managerpkg.ManagerConfig{
			TotalEquityUSD:      10000,
			ReserveEquityPct:    0,
			AllocationStrategy:  "static",
			StateStorageBackend: "memory",
			StateStoragePath:    t.TempDir(),
		},
		Monitoring: managerpkg.MonitoringConfig{
			MetricsExporter: "stdout",
		},
		Traders: []managerpkg.TraderConfig{
			{
				ID:                  "paper-smoke",
				Name:                "Paper Smoke",
				ExchangeProvider:    "paper",
				MarketProvider:      "market",
				ExecutionMode:       managerpkg.ExecutionModePaper,
				OrderStyle:          managerpkg.OrderStyleLimitIOC,
				PromptTemplate:      confkit.MustProjectPath("etc/prompts/manager/aggressive_short.tmpl"),
				ExecutorTemplate:    confkit.MustProjectPath("etc/prompts/executor/default_prompt.tmpl"),
				Model:               "smoke-model",
				DecisionInterval:    3 * time.Minute,
				DecisionIntervalRaw: "3m",
				RiskParams: managerpkg.RiskParameters{
					MaxPositions:       1,
					MaxPositionSizeUSD: 1000,
					MaxMarginUsagePct:  80,
					MajorCoinLeverage:  5,
					AltcoinLeverage:    3,
					MinRiskRewardRatio: 1.5,
					MinConfidence:      50,
				},
				AllocationPct: 10,
				AutoStart:     false,
			},
		},
	}
	mgr := managerpkg.NewManager(
		cfg,
		&smokeExecutorFactory{},
		map[string]exchange.Provider{"paper": exch},
		map[string]market.Provider{"market": mkt},
		store,
	)
	trader, err := mgr.RegisterTrader(context.Background(), cfg.Traders[0])
	require.NoError(t, err)
	require.NotNil(t, trader)
	return mgr
}

type smokeExecutorFactory struct{}

func (f *smokeExecutorFactory) NewExecutor(traderCfg managerpkg.TraderConfig) (executorpkg.Executor, error) {
	return &smokeExecutor{cfg: &executorpkg.Config{TraderID: traderCfg.ID}}, nil
}

type smokeExecutor struct {
	cfg *executorpkg.Config
}

func (e *smokeExecutor) GetFullDecision(*executorpkg.Context) (*executorpkg.FullDecision, error) {
	return &executorpkg.FullDecision{}, nil
}

func (e *smokeExecutor) UpdatePerformance(*executorpkg.PerformanceView) {}

func (e *smokeExecutor) GetConfig() *executorpkg.Config {
	return e.cfg
}

type smokeExchangeProvider struct {
	mu                 sync.Mutex
	placeOrders        int
	setMarkPriceCalls  int
	setStopLossCalls   int
	setTakeProfitCalls int
	lastMarkPrice      float64
	orders             []exchange.Order
}

func (p *smokeExchangeProvider) PlaceOrder(_ context.Context, order exchange.Order) (*exchange.OrderResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.placeOrders++
	p.orders = append(p.orders, order)
	return &exchange.OrderResponse{Status: "ok"}, nil
}

func (p *smokeExchangeProvider) CancelOrder(context.Context, int, int64) error { return nil }

func (p *smokeExchangeProvider) GetOpenOrders(context.Context) ([]exchange.OrderStatus, error) {
	return nil, nil
}

func (p *smokeExchangeProvider) GetPositions(context.Context) ([]exchange.Position, error) {
	return nil, nil
}

func (p *smokeExchangeProvider) ClosePosition(context.Context, string) (*exchange.OrderResponse, error) {
	return &exchange.OrderResponse{Status: "ok"}, nil
}

func (p *smokeExchangeProvider) UpdateLeverage(context.Context, int, bool, int) error { return nil }

func (p *smokeExchangeProvider) GetAccountState(context.Context) (*exchange.AccountState, error) {
	return &exchange.AccountState{
		MarginSummary: exchange.MarginSummary{
			AccountValue:    "10000",
			TotalMarginUsed: "0",
		},
		AssetPositions: nil,
	}, nil
}

func (p *smokeExchangeProvider) GetAccountValue(context.Context) (float64, error) {
	return 10000, nil
}

func (p *smokeExchangeProvider) GetAssetIndex(context.Context, string) (int, error) {
	return 0, nil
}

func (p *smokeExchangeProvider) SetMarkPrice(_ context.Context, _ string, price float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setMarkPriceCalls++
	p.lastMarkPrice = price
	return nil
}

func (p *smokeExchangeProvider) SetStopLoss(context.Context, string, string, float64, float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setStopLossCalls++
	return nil
}

func (p *smokeExchangeProvider) SetTakeProfit(context.Context, string, string, float64, float64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.setTakeProfitCalls++
	return nil
}

type smokeMarketProvider struct{}

func (p *smokeMarketProvider) Snapshot(context.Context, string) (*market.Snapshot, error) {
	return &market.Snapshot{
		Symbol: "BTC",
		Price:  market.PriceInfo{Last: 50000},
	}, nil
}

func (p *smokeMarketProvider) ListAssets(context.Context) ([]market.Asset, error) {
	return []market.Asset{
		{Symbol: "BTC", IsActive: true, RawMetadata: map[string]any{"maxLeverage": 50}},
	}, nil
}

type smokePositionsModel struct {
	store *smokeStore
}

func (m *smokePositionsModel) ActiveByModels(_ context.Context, modelIDs []string) (map[string][]model.PositionRecord, error) {
	if m == nil || m.store == nil {
		return map[string][]model.PositionRecord{}, nil
	}
	return m.store.listPositionRecords(modelIDs), nil
}

func (m *smokePositionsModel) FindOne(context.Context, string) (*model.Positions, error) {
	return nil, model.ErrNotFound
}

func (m *smokePositionsModel) QueryRowsNoCacheCtx(context.Context, any, string, ...any) error {
	return nil
}

func smokePositionRecordFromEvent(event managerpkg.PositionEvent) (model.PositionRecord, bool) {
	traderID := strings.TrimSpace(event.TraderID)
	if traderID == "" && event.Trader != nil {
		traderID = strings.TrimSpace(event.Trader.ID)
	}
	symbol := strings.ToUpper(strings.TrimSpace(event.Decision.Symbol))
	if traderID == "" || symbol == "" {
		return model.PositionRecord{}, false
	}
	price := event.FillPrice
	if price <= 0 {
		price = event.Decision.EntryPrice
	}
	qty := event.FillSize
	if qty <= 0 && price > 0 && event.Decision.PositionSizeUSD > 0 {
		qty = event.Decision.PositionSizeUSD / price
	}
	if qty < 0 {
		qty = -qty
	}
	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	side := "long"
	if strings.EqualFold(event.Decision.Action, "open_short") {
		side = "short"
	}
	leverage := float64(event.Decision.Leverage)
	confidence := float64(event.Decision.Confidence)
	riskUSD := event.Decision.RiskUSD
	return model.PositionRecord{
		ID:          fmt.Sprintf("%s|%s", traderID, symbol),
		TraderID:    traderID,
		Symbol:      symbol,
		Side:        side,
		Status:      "open",
		EntryTimeMs: occurredAt.UTC().UnixMilli(),
		EntryPrice:  price,
		Quantity:    qty,
		Leverage:    &leverage,
		Confidence:  &confidence,
		RiskUsd:     &riskUSD,
	}, true
}

type smokeStore struct {
	mu            sync.Mutex
	nextID        int64
	commands      map[string]repo.ControlCommandRecord
	order         []string
	idemIndex     map[string]string
	statusHistory map[string][]string

	repoAudits    []repo.AuditEventRecord
	managerAudits []managerpkg.AuditEvent

	positionEvents  []managerpkg.PositionEvent
	positionRecords map[string]model.PositionRecord
}

func newSmokeStore() *smokeStore {
	return &smokeStore{
		commands:        make(map[string]repo.ControlCommandRecord),
		idemIndex:       make(map[string]string),
		statusHistory:   make(map[string][]string),
		positionRecords: make(map[string]model.PositionRecord),
	}
}

func (s *smokeStore) RecordAuditEvent(_ context.Context, event managerpkg.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.managerAudits = append(s.managerAudits, event)
	s.appendRepoAuditLocked(repo.AuditEventRecord{
		Type:            repo.AuditEventType(event.Type),
		TraderID:        event.TraderID,
		CycleID:         event.CycleID,
		CorrelationID:   event.CorrelationID,
		Symbol:          event.Symbol,
		Action:          event.Action,
		ModelID:         event.ModelID,
		ModelName:       event.ModelName,
		PromptDigest:    event.PromptDigest,
		ApprovalTokenID: event.ApprovalTokenID,
		Reason:          event.Reason,
		Error:           event.Error,
		Detail:          append(json.RawMessage(nil), event.Detail...),
		CreatedAt:       event.CreatedAt,
	})
	return nil
}

func (s *smokeStore) RecordPositionEvent(_ context.Context, event managerpkg.PositionEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positionEvents = append(s.positionEvents, event)
	record, ok := smokePositionRecordFromEvent(event)
	if !ok {
		return nil
	}
	if event.Event == managerpkg.PositionEventClose {
		delete(s.positionRecords, record.ID)
		return nil
	}
	s.positionRecords[record.ID] = record
	return nil
}

func (s *smokeStore) RecordDecisionCycle(context.Context, managerpkg.DecisionCycleRecord) error {
	return nil
}

func (s *smokeStore) RecordAccountSnapshot(context.Context, managerpkg.AccountSyncSnapshot) error {
	return nil
}

func (s *smokeStore) RecordAnalytics(context.Context, managerpkg.AnalyticsSnapshot) error { return nil }

func (s *smokeStore) HydrateCaches(context.Context, []string) error { return nil }

func (s *smokeStore) recordRepoAudit(record repo.AuditEventRecord) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendRepoAuditLocked(record), nil
}

func (s *smokeStore) appendRepoAuditLocked(record repo.AuditEventRecord) int64 {
	if record.ID == 0 {
		record.ID = int64(len(s.repoAudits) + 1)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	s.repoAudits = append(s.repoAudits, record)
	return record.ID
}

func (s *smokeStore) listRepoAudits(filter repo.AuditEventListFilter) []repo.AuditEventRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]repo.AuditEventRecord, 0, len(s.repoAudits))
	for _, record := range s.repoAudits {
		if filter.TraderID != "" && record.TraderID != filter.TraderID {
			continue
		}
		if filter.Type != "" && record.Type != filter.Type {
			continue
		}
		if filter.CorrelationID != "" && record.CorrelationID != filter.CorrelationID {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (s *smokeStore) listPositionRecords(modelIDs []string) map[string][]model.PositionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	allowed := make(map[string]struct{}, len(modelIDs))
	for _, modelID := range modelIDs {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			allowed[modelID] = struct{}{}
		}
	}
	out := make(map[string][]model.PositionRecord)
	for _, record := range s.positionRecords {
		if record.Status != "" && record.Status != "open" {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[record.TraderID]; !ok {
				continue
			}
		}
		out[record.TraderID] = append(out[record.TraderID], record)
	}
	return out
}

func (s *smokeStore) enqueueCommand(record repo.ControlCommandRecord) (repo.ControlCommandRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.Target == "" {
		record.Target = repo.ControlCommandTargetDecision
	}
	if record.Action == "" {
		record.Action = "approve"
	}
	if record.Type == "" {
		record.Type = record.Target + "_" + record.Action
	}
	if record.ID == "" {
		s.nextID++
		record.ID = fmt.Sprintf("cmd-smoke-%d", s.nextID)
	}
	if record.CorrelationID == "" {
		record.CorrelationID = record.ID
	}
	record.Status = repo.ControlCommandStatusQueued
	record.Queued = true
	record.ControlPlaneOnly = true
	record.Submitted = false
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	record.UpdatedAt = record.CreatedAt

	s.commands[record.ID] = record
	s.order = append(s.order, record.ID)
	if record.IdempotencyKey != "" {
		idemKey := strings.Join([]string{record.Target, record.DecisionID, record.Action, record.IdempotencyKey}, "\x00")
		s.idemIndex[idemKey] = record.ID
	}
	s.statusHistory[record.ID] = append(s.statusHistory[record.ID], record.Status)
	return record, false, nil
}

func (s *smokeStore) listCommands(filter repo.ControlCommandListFilter) []repo.ControlCommandRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]repo.ControlCommandRecord, 0, len(s.order))
	for _, id := range s.order {
		record, ok := s.commands[id]
		if !ok {
			continue
		}
		if filter.Target != "" && record.Target != filter.Target {
			continue
		}
		if filter.Status != "" && record.Status != filter.Status {
			continue
		}
		if filter.TraderID != "" && record.TraderID != filter.TraderID {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (s *smokeStore) claimQueued(limit int) []repo.ControlCommandRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	if limit <= 0 {
		limit = len(s.order)
	}
	out := make([]repo.ControlCommandRecord, 0, limit)
	for _, id := range s.order {
		if len(out) >= limit {
			break
		}
		record, ok := s.commands[id]
		if !ok || record.Status != repo.ControlCommandStatusQueued {
			continue
		}
		record.Status = repo.ControlCommandStatusProcessing
		record.UpdatedAt = time.Now().UTC()
		s.commands[id] = record
		s.statusHistory[id] = append(s.statusHistory[id], record.Status)
		out = append(out, record)
	}
	return out
}

func (s *smokeStore) updateTerminal(id, status string, submitted bool, detail json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commands[id]
	if !ok {
		return fmt.Errorf("smoke store: command %s not found", id)
	}
	record.Status = status
	record.Submitted = submitted
	record.Detail = detail
	record.UpdatedAt = time.Now().UTC()
	s.commands[id] = record
	s.statusHistory[id] = append(s.statusHistory[id], status)
	return nil
}

type smokeAuditRepo struct {
	store *smokeStore
}

func (r *smokeAuditRepo) Record(_ context.Context, record repo.AuditEventRecord) (int64, error) {
	if r == nil || r.store == nil {
		return 0, nil
	}
	return r.store.recordRepoAudit(record)
}

func (r *smokeAuditRepo) List(_ context.Context, filter repo.AuditEventListFilter) ([]repo.AuditEventRecord, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	return r.store.listRepoAudits(filter), nil
}

func (r *smokeAuditRepo) ListByTrader(ctx context.Context, traderID string, limit int) ([]repo.AuditEventRecord, error) {
	return r.List(ctx, repo.AuditEventListFilter{TraderID: traderID, Limit: limit})
}

type smokeControlRepo struct {
	store *smokeStore
}

func (r *smokeControlRepo) Enqueue(_ context.Context, record repo.ControlCommandRecord) (repo.ControlCommandRecord, bool, error) {
	if r == nil || r.store == nil {
		return record, false, nil
	}
	return r.store.enqueueCommand(record)
}

func (r *smokeControlRepo) List(_ context.Context, filter repo.ControlCommandListFilter) ([]repo.ControlCommandRecord, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	return r.store.listCommands(filter), nil
}

func (r *smokeControlRepo) ClaimQueued(_ context.Context, limit int) ([]repo.ControlCommandRecord, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	return r.store.claimQueued(limit), nil
}

func (r *smokeControlRepo) Complete(_ context.Context, id string, submitted bool, detail json.RawMessage) error {
	if r == nil || r.store == nil {
		return nil
	}
	return r.store.updateTerminal(id, repo.ControlCommandStatusCompleted, submitted, detail)
}

func (r *smokeControlRepo) Fail(_ context.Context, id, reason string, detail json.RawMessage) error {
	if r == nil || r.store == nil {
		return nil
	}
	_ = reason
	return r.store.updateTerminal(id, repo.ControlCommandStatusFailed, false, detail)
}

func (r *smokeControlRepo) Cancel(_ context.Context, id, reason string, detail json.RawMessage) error {
	if r == nil || r.store == nil {
		return nil
	}
	_ = reason
	return r.store.updateTerminal(id, repo.ControlCommandStatusCancelled, false, detail)
}

type smokeSnapshot struct {
	Record        repo.ControlCommandRecord
	StatusHistory []string
}

func (s *smokeStore) snapshotCommand(t *testing.T, id string) smokeSnapshot {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.commands[id]
	require.True(t, ok, "command %s should exist", id)
	history := append([]string(nil), s.statusHistory[id]...)
	return smokeSnapshot{Record: record, StatusHistory: history}
}
