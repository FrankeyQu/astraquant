// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"strings"

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
	resp, err = l.svcCtx.DataLoader.LoadTrades()
	if err != nil || req == nil {
		return resp, err
	}

	traderID := strings.TrimSpace(req.TraderId)
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if traderID == "" && symbol == "" && side == "" && req.Limit <= 0 && req.Offset <= 0 {
		return resp, nil
	}

	filtered := make([]types.Trade, 0, len(resp.Trades))
	for _, trade := range resp.Trades {
		if traderID != "" && trade.ModelId != traderID {
			continue
		}
		if symbol != "" && strings.ToUpper(trade.Symbol) != symbol {
			continue
		}
		if side != "" && strings.ToLower(trade.Side) != side {
			continue
		}
		filtered = append(filtered, trade)
	}
	resp.Trades = paginateTrades(filtered, req.Limit, req.Offset)
	return resp, nil
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
