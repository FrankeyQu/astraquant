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
	resp, err = l.svcCtx.DataLoader.LoadPositions()
	if err != nil || req == nil {
		return resp, err
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
