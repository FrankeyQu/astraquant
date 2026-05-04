package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradesDBPath(t *testing.T) {
	rows := []model.Trades{
		makeTradeRow(t, tradeFixture{
			id:               "trade-older",
			modelID:          "paper-smoke",
			symbol:           "BTC",
			side:             "short",
			entryTsMs:        1761300000000,
			exitTsMs:         1761350000000,
			entryPrice:       50000,
			exitPrice:        49500,
			quantity:         2,
			leverage:         3,
			confidence:       0.72,
			entryHumanTime:   "2025-10-24 09:20:00.000000",
			exitHumanTime:    "2025-10-24 23:13:20.000000",
			tradeType:        "short",
			tradeID:          "paper-smoke-btc-short",
			entryOid:         111,
			exitOid:          222,
			entryTid:         333,
			exitTid:          444,
			entryCommission:  1.5,
			exitCommission:   1.5,
			entryClosedPnl:   -1.5,
			exitClosedPnl:    998.5,
			realizedGrossPnl: 1000,
			realizedNetPnl:   997,
			totalCommission:  3,
		}),
		makeTradeRow(t, tradeFixture{
			id:               "trade-newer",
			modelID:          "paper-smoke",
			symbol:           "XRP",
			side:             "long",
			entryTsMs:        1761314029249,
			exitTsMs:         1761418991842,
			entryPrice:       2.499,
			exitPrice:        2.6463,
			quantity:         1818,
			leverage:         1,
			confidence:       0,
			entryHumanTime:   "2025-10-24 13:53:49.249000",
			exitHumanTime:    "2025-10-25 19:03:11.842000",
			tradeType:        "long",
			tradeID:          "1761314029.249_XRP_1761418991.842_paper-smoke",
			entryOid:         211370609263,
			exitOid:          212380998540,
			entryTid:         236046654097081,
			exitTid:          1009375947925348,
			entryCrossed:     true,
			exitCrossed:      true,
			entryCommission:  1.817272,
			exitCommission:   1.924439,
			entryClosedPnl:   -1.817272,
			exitClosedPnl:    265.994061,
			realizedGrossPnl: 267.7914,
			realizedNetPnl:   264.176789,
			totalCommission:  3.741711,
		}),
		makeTradeRow(t, tradeFixture{
			id:               "trade-other",
			modelID:          "other-model",
			symbol:           "ETH",
			side:             "long",
			entryTsMs:        1761200000000,
			exitTsMs:         1761210000000,
			entryPrice:       3000,
			exitPrice:        3010,
			quantity:         1.5,
			leverage:         2,
			confidence:       0.61,
			entryHumanTime:   "2025-10-23 09:33:20.000000",
			exitHumanTime:    "2025-10-23 12:20:00.000000",
			tradeType:        "long",
			tradeID:          "other-eth-long",
			entryOid:         555,
			exitOid:          666,
			entryTid:         777,
			exitTid:          888,
			entryCommission:  0.8,
			exitCommission:   0.7,
			entryClosedPnl:   -0.8,
			exitClosedPnl:    14.2,
			realizedGrossPnl: 15,
			realizedNetPnl:   13.5,
			totalCommission:  1.5,
		}),
	}

	models := &fakeTradesModel{rows: rows}
	svcCtx := &svc.ServiceContext{
		TradesModel: models,
	}

	logic := NewTradesLogic(context.Background(), svcCtx)
	resp, err := logic.Trades(&types.TradesRequest{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Trades, 1)
	require.NotZero(t, resp.ServerTime)
	require.Len(t, models.queries, 1)
	assert.Contains(t, models.queries[0], "FROM public.trades")

	trade := resp.Trades[0]
	assert.Equal(t, "trade-newer", trade.Id)
	assert.Equal(t, "paper-smoke", trade.ModelId)
	assert.Equal(t, "XRP", trade.Symbol)
	assert.Equal(t, "long", trade.Side)
	assert.Equal(t, "long", trade.TradeType)
	assert.Equal(t, "1761314029.249_XRP_1761418991.842_paper-smoke", trade.TradeId)
	assert.Equal(t, 1818.0, trade.Quantity)
	assert.Equal(t, 1.0, trade.Leverage)
	assert.Equal(t, 2.499, trade.EntryPrice)
	assert.Equal(t, 2.6463, trade.ExitPrice)
	assert.InDelta(t, 1761314029.249, trade.EntryTime, 0.001)
	assert.InDelta(t, 1761418991.842, trade.ExitTime, 0.001)
	assert.Equal(t, "2025-10-24 13:53:49.249000", trade.EntryHumanTime)
	assert.Equal(t, "2025-10-25 19:03:11.842000", trade.ExitHumanTime)
	assert.EqualValues(t, 211370609263, trade.EntryOid)
	assert.EqualValues(t, 212380998540, trade.ExitOid)
	assert.EqualValues(t, 236046654097081, trade.EntryTid)
	assert.EqualValues(t, 1009375947925348, trade.ExitTid)
	assert.True(t, trade.EntryCrossed)
	assert.True(t, trade.ExitCrossed)
	assert.Equal(t, 1.817272, trade.EntryCommissionDollars)
	assert.Equal(t, 1.924439, trade.ExitCommissionDollars)
	assert.Equal(t, -1.817272, trade.EntryClosedPnl)
	assert.Equal(t, 265.994061, trade.ExitClosedPnl)
	assert.Equal(t, 267.7914, trade.RealizedGrossPnl)
	assert.Equal(t, 264.176789, trade.RealizedNetPnl)
	assert.Equal(t, 3.741711, trade.TotalCommissionDollars)
}

func TestTradesDBPathFilterAndSort(t *testing.T) {
	rows := []model.Trades{
		makeTradeRow(t, tradeFixture{
			id:               "trade-older",
			modelID:          "paper-smoke",
			symbol:           "BTC",
			side:             "short",
			entryTsMs:        1761300000000,
			exitTsMs:         1761350000000,
			entryPrice:       50000,
			exitPrice:        49500,
			quantity:         2,
			leverage:         3,
			confidence:       0.72,
			tradeType:        "short",
			tradeID:          "paper-smoke-btc-short",
			realizedGrossPnl: 1000,
			realizedNetPnl:   997,
			totalCommission:  3,
		}),
		makeTradeRow(t, tradeFixture{
			id:               "trade-newer",
			modelID:          "paper-smoke",
			symbol:           "XRP",
			side:             "long",
			entryTsMs:        1761314029249,
			exitTsMs:         1761418991842,
			entryPrice:       2.499,
			exitPrice:        2.6463,
			quantity:         1818,
			leverage:         1,
			tradeType:        "long",
			tradeID:          "paper-smoke-xrp-long",
			realizedGrossPnl: 267.7914,
			realizedNetPnl:   264.176789,
			totalCommission:  3.741711,
		}),
		makeTradeRow(t, tradeFixture{
			id:               "trade-other",
			modelID:          "other-model",
			symbol:           "ETH",
			side:             "long",
			entryTsMs:        1761200000000,
			exitTsMs:         1761210000000,
			entryPrice:       3000,
			exitPrice:        3010,
			quantity:         1.5,
			leverage:         2,
			tradeType:        "long",
			tradeID:          "other-eth-long",
			realizedGrossPnl: 15,
			realizedNetPnl:   13.5,
			totalCommission:  1.5,
		}),
	}

	models := &fakeTradesModel{rows: rows}
	svcCtx := &svc.ServiceContext{TradesModel: models}

	logic := NewTradesLogic(context.Background(), svcCtx)
	resp, err := logic.Trades(&types.TradesRequest{
		TraderId: "paper-smoke",
		Symbol:   "XRP",
		Side:     "long",
		Limit:    1,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Trades, 1)
	assert.Equal(t, "trade-newer", resp.Trades[0].Id)
	assert.Equal(t, "XRP", resp.Trades[0].Symbol)
	assert.Equal(t, "paper-smoke", resp.Trades[0].ModelId)
}

func makeTradeRow(t *testing.T, f tradeFixture) model.Trades {
	t.Helper()
	payload := map[string]any{
		"time": map[string]any{
			"open_ts_ms":  f.entryTsMs,
			"close_ts_ms": f.exitTsMs,
		},
		"prices": map[string]any{
			"entry": f.entryPrice,
			"exit":  f.exitPrice,
		},
		"quantity": map[string]any{
			"total": f.quantity,
		},
		"risk": map[string]any{
			"confidence": f.confidence,
			"leverage":   f.leverage,
		},
		"exchange": map[string]any{
			"provider": "hyperliquid",
		},
		"pnl": map[string]any{
			"net":   f.realizedNetPnl,
			"gross": f.realizedGrossPnl,
		},
		"model_id":                 f.modelID,
		"symbol":                   f.symbol,
		"side":                     f.side,
		"trade_type":               f.tradeType,
		"trade_id":                 f.tradeID,
		"entry_human_time":         f.entryHumanTime,
		"exit_human_time":          f.exitHumanTime,
		"entry_sz":                 f.quantity,
		"exit_sz":                  f.quantity,
		"entry_tid":                f.entryTid,
		"exit_tid":                 f.exitTid,
		"entry_oid":                f.entryOid,
		"exit_oid":                 f.exitOid,
		"entry_crossed":            f.entryCrossed,
		"exit_crossed":             f.exitCrossed,
		"entry_liquidation":        nil,
		"exit_liquidation":         nil,
		"entry_commission_dollars": f.entryCommission,
		"exit_commission_dollars":  f.exitCommission,
		"entry_closed_pnl":         f.entryClosedPnl,
		"exit_closed_pnl":          f.exitClosedPnl,
		"exit_plan":                map[string]any{},
		"realized_gross_pnl":       f.realizedGrossPnl,
		"realized_net_pnl":         f.realizedNetPnl,
		"total_commission_dollars": f.totalCommission,
	}
	raw := mustJSON(t, payload)
	return model.Trades{
		Id:        f.id,
		TraderId:  f.modelID,
		Symbol:    f.symbol,
		Side:      f.side,
		CloseTsMs: f.exitTsMs,
		Detail:    raw,
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

type tradeFixture struct {
	id               string
	modelID          string
	symbol           string
	side             string
	tradeType        string
	tradeID          string
	entryTsMs        int64
	exitTsMs         int64
	entryPrice       float64
	exitPrice        float64
	quantity         float64
	leverage         float64
	confidence       float64
	entryHumanTime   string
	exitHumanTime    string
	entryOid         int64
	exitOid          int64
	entryTid         int64
	exitTid          int64
	entryCrossed     bool
	exitCrossed      bool
	entryCommission  float64
	exitCommission   float64
	entryClosedPnl   float64
	exitClosedPnl    float64
	realizedGrossPnl float64
	realizedNetPnl   float64
	totalCommission  float64
}

type fakeTradesModel struct {
	rows    []model.Trades
	queries []string
}

func (m *fakeTradesModel) Insert(context.Context, *model.Trades) (sql.Result, error) {
	return nil, nil
}

func (m *fakeTradesModel) QueryRowsNoCacheCtx(_ context.Context, dest any, query string, args ...any) error {
	m.queries = append(m.queries, query)
	rows, ok := dest.(*[]model.Trades)
	if !ok {
		return fmt.Errorf("unexpected destination type %T", dest)
	}
	*rows = append((*rows)[:0], m.rows...)
	return nil
}

func (m *fakeTradesModel) RecentByModel(context.Context, string, int) ([]model.TradeRecord, error) {
	return nil, nil
}
