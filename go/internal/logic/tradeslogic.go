// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type TradesLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTradesLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TradesLogic {
	return &TradesLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *TradesLogic) Trades(req *types.TradesRequest) (resp *types.TradesResponse, err error) {
	if resp, used, err := l.loadTradesFromDB(req); used {
		if resp == nil {
			resp = emptyTradesResponse()
		}
		return resp, err
	}
	if l == nil || l.svcCtx == nil || l.svcCtx.DataLoader == nil {
		return emptyTradesResponse(), nil
	}
	return l.svcCtx.DataLoader.LoadTrades()
}

func (l *TradesLogic) loadTradesFromDB(req *types.TradesRequest) (*types.TradesResponse, bool, error) {
	if !hasTradesDBWiring(l.svcCtx) {
		return nil, false, nil
	}

	rows, err := l.queryTradeRows()
	if err != nil {
		return nil, true, err
	}

	trades := make([]types.Trade, 0, len(rows))
	for _, row := range rows {
		trades = append(trades, tradeFromRow(row))
	}

	sortTrades(trades)
	if req != nil {
		trades = filterTrades(trades, req)
		limit, offset := normalizeLimitOffset(req.Limit, req.Offset, 100)
		trades = paginateTrades(trades, limit, offset)
	}

	return &types.TradesResponse{
		Trades:     trades,
		ServerTime: time.Now().UnixMilli(),
	}, true, nil
}

func (l *TradesLogic) queryTradeRows() ([]model.Trades, error) {
	if l == nil || l.svcCtx == nil || l.svcCtx.TradesModel == nil {
		return nil, nil
	}
	const query = `
SELECT
    id,
    trader_id,
    symbol,
    side,
    close_ts_ms,
    detail
FROM public.trades`
	var rows []model.Trades
	if err := l.svcCtx.TradesModel.QueryRowsNoCacheCtx(tradesContext(l.ctx), &rows, query); err != nil {
		return nil, fmt.Errorf("trades query: %w", err)
	}
	return rows, nil
}

func filterTrades(trades []types.Trade, req *types.TradesRequest) []types.Trade {
	if req == nil {
		return trades
	}
	traderID := strings.TrimSpace(req.TraderId)
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if traderID == "" && symbol == "" && side == "" {
		return trades
	}
	filtered := make([]types.Trade, 0, len(trades))
	for _, trade := range trades {
		if traderID != "" && trade.ModelId != traderID {
			continue
		}
		if symbol != "" && strings.ToUpper(strings.TrimSpace(trade.Symbol)) != symbol {
			continue
		}
		if side != "" && strings.ToLower(strings.TrimSpace(trade.Side)) != side {
			continue
		}
		filtered = append(filtered, trade)
	}
	return filtered
}

func sortTrades(trades []types.Trade) {
	sort.SliceStable(trades, func(i, j int) bool {
		ti := tradeSortTime(trades[i])
		tj := tradeSortTime(trades[j])
		if ti == tj {
			if trades[i].Id == trades[j].Id {
				return trades[i].ModelId > trades[j].ModelId
			}
			return trades[i].Id > trades[j].Id
		}
		return ti > tj
	})
}

func tradeSortTime(trade types.Trade) float64 {
	if trade.ExitTime > 0 {
		return trade.ExitTime
	}
	return trade.EntryTime
}

func tradeFromRow(row model.Trades) types.Trade {
	nested, flat := parseTradeDetail(row.Detail)
	side := normalizeTradeSide(firstNonEmptyString(row.Side, flat.Side))
	symbol := strings.ToUpper(strings.TrimSpace(firstNonEmptyString(row.Symbol, flat.Symbol)))
	modelID := strings.TrimSpace(firstNonEmptyString(row.TraderId, flat.ModelID))

	entryTime := firstPositiveFloat(flat.EntryTime, float64(nested.Time.OpenTsMs)/1000)
	exitTime := firstPositiveFloat(flat.ExitTime, float64(nested.Time.CloseTsMs)/1000, float64(row.CloseTsMs)/1000)

	entryPrice := firstPositiveFloat(flat.EntryPrice, nested.Prices.Entry)
	exitPrice := firstPositiveFloat(flat.ExitPrice, nested.Prices.Exit)
	quantity := math.Abs(firstPositiveFloat(flat.Quantity, nested.Quantity.Total))
	if quantity == 0 {
		quantity = math.Abs(firstPositiveFloat(flat.EntrySz, flat.ExitSz))
	}
	leverage := firstPositiveFloat(flat.Leverage, nested.Risk.Leverage)
	confidence := firstPositiveFloat(flat.Confidence, nested.Risk.Confidence)
	realizedNet := firstPositiveFloat(flat.RealizedNetPnl, nested.PnL.Net)
	realizedGross := firstPositiveFloat(flat.RealizedGrossPnl, tradeGrossPnl(side, quantity, entryPrice, exitPrice))
	totalCommission := flat.TotalCommissionDollars
	if totalCommission == 0 && (realizedGross != 0 || realizedNet != 0) {
		totalCommission = realizedGross - realizedNet
	}
	if totalCommission == 0 {
		totalCommission = firstPositiveFloat(flat.EntryCommissionDollars+flat.ExitCommissionDollars, 0)
	}
	entryCommission := flat.EntryCommissionDollars
	if entryCommission == 0 && totalCommission != 0 {
		entryCommission = totalCommission / 2
	}
	exitCommission := flat.ExitCommissionDollars
	if exitCommission == 0 && totalCommission != 0 {
		exitCommission = totalCommission - entryCommission
	}
	entryClosedPnl := flat.EntryClosedPnl
	if entryClosedPnl == 0 && entryCommission != 0 {
		entryClosedPnl = -entryCommission
	}
	exitClosedPnl := flat.ExitClosedPnl
	if exitClosedPnl == 0 && (realizedGross != 0 || exitCommission != 0) {
		exitClosedPnl = realizedGross - exitCommission
	}

	entryHumanTime := firstNonEmptyString(flat.EntryHumanTime, tradeHumanTime(entryTime))
	exitHumanTime := firstNonEmptyString(flat.ExitHumanTime, tradeHumanTime(exitTime))
	tradeType := firstNonEmptyString(flat.TradeType, side)
	tradeID := firstNonEmptyString(flat.TradeID, row.Id)
	entrySz := firstPositiveFloat(flat.EntrySz, quantity)
	exitSz := firstPositiveFloat(flat.ExitSz, quantity)
	entryCrossed := flat.EntryCrossed
	exitCrossed := flat.ExitCrossed

	entryOID := flat.EntryOid
	if entryOID == 0 {
		entryOID = stableTradeOID(row.Id, symbol, "entry-oid")
	}
	exitOID := flat.ExitOid
	if exitOID == 0 {
		exitOID = stableTradeOID(row.Id, symbol, "exit-oid")
	}
	entryTID := flat.EntryTid
	if entryTID == 0 {
		entryTID = stableTradeOID(row.Id, symbol, "entry-tid")
	}
	exitTID := flat.ExitTid
	if exitTID == 0 {
		exitTID = stableTradeOID(row.Id, symbol, "exit-tid")
	}

	return types.Trade{
		Id:                     row.Id,
		ModelId:                modelID,
		Symbol:                 symbol,
		Side:                   side,
		TradeType:              tradeType,
		TradeId:                tradeID,
		Quantity:               quantity,
		Leverage:               leverage,
		Confidence:             confidence,
		EntryPrice:             entryPrice,
		EntryTime:              entryTime,
		EntryHumanTime:         entryHumanTime,
		EntrySz:                entrySz,
		EntryTid:               entryTID,
		EntryOid:               entryOID,
		EntryCrossed:           entryCrossed,
		EntryLiquidation:       flat.EntryLiquidation,
		EntryCommissionDollars: entryCommission,
		EntryClosedPnl:         entryClosedPnl,
		ExitPrice:              exitPrice,
		ExitTime:               exitTime,
		ExitHumanTime:          exitHumanTime,
		ExitSz:                 exitSz,
		ExitTid:                exitTID,
		ExitOid:                exitOID,
		ExitCrossed:            exitCrossed,
		ExitLiquidation:        flat.ExitLiquidation,
		ExitCommissionDollars:  exitCommission,
		ExitClosedPnl:          exitClosedPnl,
		ExitPlan:               flat.ExitPlan,
		RealizedGrossPnl:       realizedGross,
		RealizedNetPnl:         realizedNet,
		TotalCommissionDollars: totalCommission,
	}
}

func parseTradeDetail(raw string) (tradeNestedDetail, tradeFlatDetail) {
	var nested tradeNestedDetail
	var flat tradeFlatDetail
	if strings.TrimSpace(raw) == "" {
		return nested, flat
	}
	_ = json.Unmarshal([]byte(raw), &nested)
	_ = json.Unmarshal([]byte(raw), &flat)
	return nested, flat
}

func tradeGrossPnl(side string, quantity, entryPrice, exitPrice float64) float64 {
	if quantity <= 0 || entryPrice <= 0 || exitPrice <= 0 {
		return 0
	}
	switch normalizeTradeSide(side) {
	case "short":
		return (entryPrice - exitPrice) * quantity
	default:
		return (exitPrice - entryPrice) * quantity
	}
}

func tradeHumanTime(sec float64) string {
	if sec <= 0 {
		return ""
	}
	t := time.Unix(0, int64(math.Round(sec*float64(time.Second)))).UTC()
	return t.Format("2006-01-02 15:04:05.000000")
}

func normalizeTradeSide(side string) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "short":
		return "short"
	default:
		return "long"
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func stableTradeOID(parts ...string) int64 {
	h := fnv.New64a()
	wrote := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
		wrote = true
	}
	if !wrote {
		return 1
	}
	value := h.Sum64() & 0x7fffffffffffffff
	if value == 0 {
		return 1
	}
	return int64(value)
}

func paginateTrades(items []types.Trade, limit, offset int) []types.Trade {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(items)
	}
	if offset >= len(items) {
		return []types.Trade{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func hasTradesDBWiring(svcCtx *svc.ServiceContext) bool {
	return svcCtx != nil && svcCtx.TradesModel != nil
}

func tradesContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func emptyTradesResponse() *types.TradesResponse {
	return &types.TradesResponse{
		Trades:     []types.Trade{},
		ServerTime: time.Now().UnixMilli(),
	}
}

type tradeNestedDetail struct {
	Time struct {
		OpenTsMs        int64 `json:"open_ts_ms"`
		CloseTsMs       int64 `json:"close_ts_ms"`
		DurationSeconds int64 `json:"duration_seconds"`
	} `json:"time"`
	Prices struct {
		Entry float64 `json:"entry"`
		Exit  float64 `json:"exit"`
	} `json:"prices"`
	Quantity struct {
		Total float64 `json:"total"`
	} `json:"quantity"`
	Risk struct {
		Confidence float64 `json:"confidence"`
		Leverage   float64 `json:"leverage"`
	} `json:"risk"`
	Exchange struct {
		Provider string `json:"provider"`
	} `json:"exchange"`
	PnL struct {
		Net   float64 `json:"net"`
		Gross float64 `json:"gross"`
	} `json:"pnl"`
}

type tradeFlatDetail struct {
	ModelID                string      `json:"model_id"`
	Symbol                 string      `json:"symbol"`
	Side                   string      `json:"side"`
	TradeType              string      `json:"trade_type"`
	TradeID                string      `json:"trade_id"`
	Quantity               float64     `json:"quantity"`
	Leverage               float64     `json:"leverage"`
	Confidence             float64     `json:"confidence"`
	EntryPrice             float64     `json:"entry_price"`
	ExitPrice              float64     `json:"exit_price"`
	EntryTime              float64     `json:"entry_time"`
	ExitTime               float64     `json:"exit_time"`
	EntryHumanTime         string      `json:"entry_human_time"`
	ExitHumanTime          string      `json:"exit_human_time"`
	EntrySz                float64     `json:"entry_sz"`
	EntryTid               int64       `json:"entry_tid"`
	EntryOid               int64       `json:"entry_oid"`
	EntryCrossed           bool        `json:"entry_crossed"`
	EntryLiquidation       interface{} `json:"entry_liquidation"`
	EntryCommissionDollars float64     `json:"entry_commission_dollars"`
	EntryClosedPnl         float64     `json:"entry_closed_pnl"`
	ExitSz                 float64     `json:"exit_sz"`
	ExitTid                int64       `json:"exit_tid"`
	ExitOid                int64       `json:"exit_oid"`
	ExitCrossed            bool        `json:"exit_crossed"`
	ExitLiquidation        interface{} `json:"exit_liquidation"`
	ExitCommissionDollars  float64     `json:"exit_commission_dollars"`
	ExitClosedPnl          float64     `json:"exit_closed_pnl"`
	ExitPlan               interface{} `json:"exit_plan"`
	RealizedGrossPnl       float64     `json:"realized_gross_pnl"`
	RealizedNetPnl         float64     `json:"realized_net_pnl"`
	TotalCommissionDollars float64     `json:"total_commission_dollars"`
}
