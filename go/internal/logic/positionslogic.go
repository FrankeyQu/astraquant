// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
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

type PositionsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewPositionsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *PositionsLogic {
	return &PositionsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *PositionsLogic) Positions(req *types.PositionsRequest) (resp *types.PositionsResponse, err error) {
	resp, err = l.loadPositions(req)
	if err != nil || req == nil {
		return resp, err
	}
	if resp == nil {
		resp = emptyPositionsResponse()
	}

	traderID := strings.TrimSpace(req.TraderId)
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	if traderID == "" && symbol == "" && req.Limit <= 0 && req.Offset <= 0 {
		return resp, nil
	}

	var filtered []types.PositionsByModel
	for _, group := range resp.AccountTotals {
		if traderID != "" && group.ModelId != traderID {
			continue
		}
		positions := make(map[string]types.Position, len(group.Positions))
		for key, position := range group.Positions {
			if symbol != "" && strings.ToUpper(position.Symbol) != symbol && strings.ToUpper(key) != symbol {
				continue
			}
			positions[key] = position
		}
		if len(positions) > 0 || symbol == "" {
			group.Positions = positions
			filtered = append(filtered, group)
		}
	}
	resp.AccountTotals = paginatePositionGroups(filtered, req.Limit, req.Offset)
	return resp, nil
}

func (l *PositionsLogic) loadPositions(req *types.PositionsRequest) (*types.PositionsResponse, error) {
	if l != nil && l.svcCtx != nil && l.svcCtx.PositionsModel != nil {
		var traderIDs []string
		if req != nil {
			if traderID := strings.TrimSpace(req.TraderId); traderID != "" {
				traderIDs = []string{traderID}
			}
		}
		records, err := l.svcCtx.PositionsModel.ActiveByModels(l.ctx, traderIDs)
		if err != nil {
			return nil, err
		}
		return positionsResponseFromRecords(records), nil
	}
	if l == nil || l.svcCtx == nil || l.svcCtx.DataLoader == nil {
		return emptyPositionsResponse(), nil
	}
	return l.svcCtx.DataLoader.LoadPositions()
}

func positionsResponseFromRecords(records map[string][]model.PositionRecord) *types.PositionsResponse {
	modelIDs := make([]string, 0, len(records))
	for modelID := range records {
		if strings.TrimSpace(modelID) != "" {
			modelIDs = append(modelIDs, modelID)
		}
	}
	sort.Strings(modelIDs)

	groups := make([]types.PositionsByModel, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		rows := append([]model.PositionRecord(nil), records[modelID]...)
		sort.SliceStable(rows, func(i, j int) bool {
			return strings.ToUpper(rows[i].Symbol) < strings.ToUpper(rows[j].Symbol)
		})
		positions := make(map[string]types.Position, len(rows))
		for _, row := range rows {
			symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
			if symbol == "" {
				continue
			}
			positions[symbol] = positionFromRecord(row, modelID, symbol)
		}
		if len(positions) == 0 {
			continue
		}
		groups = append(groups, types.PositionsByModel{
			ModelId:   modelID,
			Positions: positions,
		})
	}
	return &types.PositionsResponse{
		AccountTotals: groups,
		ServerTime:    time.Now().UnixMilli(),
	}
}

func positionFromRecord(record model.PositionRecord, fallbackTraderID, fallbackSymbol string) types.Position {
	symbol := strings.ToUpper(strings.TrimSpace(record.Symbol))
	if symbol == "" {
		symbol = fallbackSymbol
	}
	traderID := strings.TrimSpace(record.TraderID)
	if traderID == "" {
		traderID = strings.TrimSpace(fallbackTraderID)
	}
	leverage := derefFloat(record.Leverage)
	if leverage <= 0 {
		leverage = 1
	}
	quantity := math.Abs(record.Quantity)
	if strings.EqualFold(record.Side, "short") {
		quantity = -quantity
	}
	notional := math.Abs(quantity) * record.EntryPrice
	margin := notional / leverage
	riskUSD := derefFloat(record.RiskUsd)
	if riskUSD <= 0 {
		riskUSD = margin
	}
	currentPrice := record.EntryPrice
	unrealizedPnL := derefFloat(record.UnrealizedPnl)
	entryOID := stablePositionOID(record.ID, traderID, symbol)

	return types.Position{
		EntryOid:      entryOID,
		RiskUsd:       riskUSD,
		Confidence:    normalizePositionConfidence(derefFloat(record.Confidence)),
		EntryTime:     positionEntrySeconds(record.EntryTimeMs),
		Symbol:        symbol,
		EntryPrice:    record.EntryPrice,
		Margin:        margin,
		Oid:           entryOID,
		CurrentPrice:  currentPrice,
		Leverage:      leverage,
		Quantity:      quantity,
		UnrealizedPnl: unrealizedPnL,
	}
}

func derefFloat(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizePositionConfidence(value float64) float64 {
	if value > 1 {
		value = value / 100
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func positionEntrySeconds(entryTimeMs int64) float64 {
	if entryTimeMs <= 0 {
		return 0
	}
	return float64(entryTimeMs) / 1000
}

func stablePositionOID(parts ...string) int64 {
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

func emptyPositionsResponse() *types.PositionsResponse {
	return &types.PositionsResponse{
		AccountTotals: []types.PositionsByModel{},
		ServerTime:    time.Now().UnixMilli(),
	}
}

func paginatePositionGroups(items []types.PositionsByModel, limit, offset int) []types.PositionsByModel {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(items)
	}
	if offset >= len(items) {
		return []types.PositionsByModel{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}
