package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/market"
	"nof0-api/pkg/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountTotalsDBPath(t *testing.T) {
	snapshotSource := &fakeAccountEquitySnapshotsModel{
		rows: []accountSnapshotRow{
			{
				ModelID:                    "paper-smoke",
				TsMs:                       1714582800000,
				DollarEquity:               10050,
				RealizedPnl:                12.5,
				TotalUnrealizedPnl:         0,
				CumPnlPct:                  sql.NullFloat64{Float64: 0.5, Valid: true},
				SharpeRatio:                sql.NullFloat64{Float64: 1.23, Valid: true},
				SinceInceptionHourlyMarker: sql.NullInt64{Int64: 42, Valid: true},
				SinceInceptionMinuteMarker: sql.NullInt64{Int64: 1680, Valid: true},
			},
		},
	}
	positionsSource := &fakePositionsModel{
		records: map[string][]model.PositionRecord{
			"paper-smoke": {
				{
					ID:          "pos-1",
					TraderID:    "paper-smoke",
					Symbol:      "btc",
					Side:        "long",
					Status:      "open",
					EntryTimeMs: 1714580000000,
					EntryPrice:  50000,
					Quantity:    1,
					Leverage:    floatPtr(5),
					Confidence:  floatPtr(88),
					RiskUsd:     floatPtr(100),
				},
			},
		},
	}
	commandRepo := &fakeAccountTotalsControlCommandRepo{
		records: []repo.ControlCommandRecord{
			{
				ID:        "cmd-older",
				TraderID:  "paper-smoke",
				Target:    repo.ControlCommandTargetDecision,
				Status:    repo.ControlCommandStatusCompleted,
				CreatedAt: time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC),
				Detail:    mustDecisionDetail(t, executorpkg.Decision{Symbol: "BTC", Action: "open_long", TakeProfit: 53000, StopLoss: 48500, InvalidationCondition: "close below EMA20"}),
			},
			{
				ID:        "cmd-newer",
				TraderID:  "paper-smoke",
				Target:    repo.ControlCommandTargetDecision,
				Status:    repo.ControlCommandStatusCompleted,
				CreatedAt: time.Date(2026, 5, 2, 1, 0, 0, 0, time.UTC),
				Detail:    mustDecisionDetail(t, executorpkg.Decision{Symbol: "BTC", Action: "open_long", TakeProfit: 54000, StopLoss: 49000, InvalidationCondition: "close below VWAP"}),
			},
		},
	}
	marketProvider := &fakeMarketProvider{
		snapshots: map[string]*market.Snapshot{
			"BTC": {
				Price: market.PriceInfo{Last: 51000},
			},
		},
	}
	svcCtx := &svc.ServiceContext{
		AccountEquitySnapshotsModel: snapshotSource,
		PositionsModel:              positionsSource,
		ControlCommandRepo:          commandRepo,
		ManagerTraderMarket: map[string]market.Provider{
			"paper-smoke": marketProvider,
		},
	}

	logic := NewAccountTotalsLogic(context.Background(), svcCtx)
	resp, err := logic.AccountTotals(&types.AccountTotalsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.AccountTotals, 1)

	total := resp.AccountTotals[0]
	require.NotNil(t, total.Positions)
	require.Contains(t, total.Positions, "BTC")

	assert.Equal(t, "paper-smoke", total.ModelId)
	assert.Equal(t, "paper-smoke_1714582800000", total.Id)
	assert.InDelta(t, 1714582800, total.Timestamp, 0.001)
	assert.Equal(t, 10050.0, total.DollarEquity)
	assert.Equal(t, 12.5, total.RealizedPnl)
	assert.Equal(t, 1000.0, total.TotalUnrealizedPnl)
	assert.Equal(t, 0.5, total.CumPnlPct)
	assert.Equal(t, 1.23, total.SharpeRatio)
	assert.Equal(t, 42, total.SinceInceptionHourlyMarker)
	assert.Equal(t, 1680, total.SinceInceptionMinuteMarker)
	assert.Equal(t, 42, resp.LastHourlyMarkerRead)
	assert.NotZero(t, resp.ServerTime)

	position := total.Positions["BTC"]
	assert.Equal(t, "BTC", position.Symbol)
	assert.Equal(t, 51000.0, position.CurrentPrice)
	assert.Equal(t, 1000.0, position.UnrealizedPnl)
	assert.Equal(t, 40000.0, position.LiquidationPrice)
	assert.Equal(t, 50000.0, position.EntryPrice)
	assert.Equal(t, 5.0, position.Leverage)
	assert.Equal(t, 1.0, position.Quantity)
	assert.Equal(t, map[string]any{
		"profit_target":          54000.0,
		"stop_loss":              49000.0,
		"invalidation_condition": "close below VWAP",
	}, position.ExitPlan)

	require.Len(t, snapshotSource.queries, 1)
	assert.Contains(t, snapshotSource.queries[0], "FROM public.account_equity_snapshots")
	require.Len(t, positionsSource.requests, 1)
	assert.Empty(t, positionsSource.requests[0])
	require.Len(t, commandRepo.filters, 1)
	assert.Equal(t, repo.ControlCommandTargetDecision, commandRepo.filters[0].Target)
	assert.Equal(t, repo.ControlCommandStatusCompleted, commandRepo.filters[0].Status)
	assert.Equal(t, "paper-smoke", commandRepo.filters[0].TraderID)
	assert.Equal(t, 500, commandRepo.filters[0].Limit)
}

func mustDecisionDetail(t *testing.T, decision executorpkg.Decision) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"decision": decision,
	})
	require.NoError(t, err)
	return payload
}

func floatPtr(v float64) *float64 {
	return &v
}

type fakeAccountEquitySnapshotsModel struct {
	rows    []accountSnapshotRow
	queries []string
	args    [][]any
}

func (m *fakeAccountEquitySnapshotsModel) Insert(context.Context, *model.AccountEquitySnapshots) (sql.Result, error) {
	return nil, nil
}

func (m *fakeAccountEquitySnapshotsModel) Update(context.Context, *model.AccountEquitySnapshots) error {
	return nil
}

func (m *fakeAccountEquitySnapshotsModel) FindOneByModelIdTsMs(context.Context, string, int64) (*model.AccountEquitySnapshots, error) {
	return nil, nil
}

func (m *fakeAccountEquitySnapshotsModel) LatestSnapshots(context.Context, []string) (map[string]model.AccountSnapshot, error) {
	return nil, nil
}

func (m *fakeAccountEquitySnapshotsModel) QueryRowsNoCacheCtx(_ context.Context, dest any, query string, args ...any) error {
	m.queries = append(m.queries, query)
	m.args = append(m.args, append([]any(nil), args...))
	rows, ok := dest.(*[]accountSnapshotRow)
	if !ok {
		return fmt.Errorf("unexpected destination type %T", dest)
	}
	*rows = append((*rows)[:0], m.rows...)
	return nil
}

type fakePositionsModel struct {
	records  map[string][]model.PositionRecord
	requests [][]string
}

func (m *fakePositionsModel) FindOne(context.Context, string) (*model.Positions, error) {
	return nil, nil
}

func (m *fakePositionsModel) QueryRowsNoCacheCtx(context.Context, any, string, ...any) error {
	return nil
}

func (m *fakePositionsModel) ActiveByModels(_ context.Context, modelIDs []string) (map[string][]model.PositionRecord, error) {
	requestCopy := append([]string(nil), modelIDs...)
	m.requests = append(m.requests, requestCopy)
	out := make(map[string][]model.PositionRecord, len(m.records))
	for modelID, rows := range m.records {
		cloned := make([]model.PositionRecord, len(rows))
		copy(cloned, rows)
		out[modelID] = cloned
	}
	return out, nil
}

type fakeAccountTotalsControlCommandRepo struct {
	records []repo.ControlCommandRecord
	filters []repo.ControlCommandListFilter
}

func (r *fakeAccountTotalsControlCommandRepo) Enqueue(context.Context, repo.ControlCommandRecord) (repo.ControlCommandRecord, bool, error) {
	return repo.ControlCommandRecord{}, false, nil
}

func (r *fakeAccountTotalsControlCommandRepo) List(_ context.Context, filter repo.ControlCommandListFilter) ([]repo.ControlCommandRecord, error) {
	r.filters = append(r.filters, filter)
	out := make([]repo.ControlCommandRecord, len(r.records))
	copy(out, r.records)
	return out, nil
}

func (r *fakeAccountTotalsControlCommandRepo) ClaimQueued(context.Context, int) ([]repo.ControlCommandRecord, error) {
	return nil, nil
}

func (r *fakeAccountTotalsControlCommandRepo) Complete(context.Context, string, bool, json.RawMessage) error {
	return nil
}

func (r *fakeAccountTotalsControlCommandRepo) Fail(context.Context, string, string, json.RawMessage) error {
	return nil
}

func (r *fakeAccountTotalsControlCommandRepo) Cancel(context.Context, string, string, json.RawMessage) error {
	return nil
}

type fakeMarketProvider struct {
	snapshots map[string]*market.Snapshot
	requests  []string
}

func (p *fakeMarketProvider) Snapshot(_ context.Context, symbol string) (*market.Snapshot, error) {
	p.requests = append(p.requests, symbol)
	if p.snapshots == nil {
		return nil, nil
	}
	if snap, ok := p.snapshots[symbol]; ok {
		return snap, nil
	}
	return nil, nil
}

func (p *fakeMarketProvider) ListAssets(context.Context) ([]market.Asset, error) {
	return nil, nil
}
