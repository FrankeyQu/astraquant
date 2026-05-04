// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"strings"
	"time"

	"nof0-api/internal/svc"
	"nof0-api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ModelAnalyticsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewModelAnalyticsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ModelAnalyticsLogic {
	return &ModelAnalyticsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ModelAnalyticsLogic) ModelAnalytics(modelId string) (resp *types.ModelAnalyticsResponse, err error) {
	if resp, used, err := l.loadModelAnalyticsFromDB(modelId); used {
		if resp == nil {
			resp = emptyModelAnalyticsResponse(modelId)
		}
		return resp, err
	}
	if l == nil || l.svcCtx == nil || l.svcCtx.DataLoader == nil {
		return emptyModelAnalyticsResponse(modelId), nil
	}
	return l.svcCtx.DataLoader.LoadModelAnalytics(modelId)
}

func (l *ModelAnalyticsLogic) loadModelAnalyticsFromDB(modelID string) (*types.ModelAnalyticsResponse, bool, error) {
	source, ok := analyticsPayloadSource(l.svcCtx)
	if !ok {
		return nil, false, nil
	}

	row, err := source.PayloadByModel(analyticsContext(l.ctx), strings.TrimSpace(modelID))
	if err != nil {
		return nil, true, err
	}
	if row == nil {
		return emptyModelAnalyticsResponse(modelID), true, nil
	}

	analytics, err := analyticsFromPayload(*row)
	if err != nil {
		return nil, true, err
	}
	return &types.ModelAnalyticsResponse{
		Analytics:  analytics,
		ServerTime: time.Now().UnixMilli(),
	}, true, nil
}

func emptyModelAnalyticsResponse(modelID string) *types.ModelAnalyticsResponse {
	return &types.ModelAnalyticsResponse{
		Analytics: types.ModelAnalytics{
			ModelId: strings.TrimSpace(modelID),
		},
		ServerTime: time.Now().UnixMilli(),
	}
}
