// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AnalyticsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAnalyticsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AnalyticsLogic {
	return &AnalyticsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AnalyticsLogic) Analytics() (resp *types.AnalyticsResponse, err error) {
	if resp, used, err := l.loadAnalyticsFromDB(); used {
		if resp == nil {
			resp = emptyAnalyticsResponse()
		}
		return resp, err
	}
	if l == nil || l.svcCtx == nil || l.svcCtx.DataLoader == nil {
		return emptyAnalyticsResponse(), nil
	}
	return l.svcCtx.DataLoader.LoadAnalytics()
}

func (l *AnalyticsLogic) loadAnalyticsFromDB() (*types.AnalyticsResponse, bool, error) {
	source, ok := analyticsPayloadSource(l.svcCtx)
	if !ok {
		return nil, false, nil
	}

	rows, err := source.AllPayloads(analyticsContext(l.ctx))
	if err != nil {
		return nil, true, err
	}

	items := make([]types.ModelAnalytics, 0, len(rows))
	for _, row := range rows {
		analytics, err := analyticsFromPayload(row)
		if err != nil {
			return nil, true, err
		}
		items = append(items, analytics)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.TrimSpace(items[i].ModelId) < strings.TrimSpace(items[j].ModelId)
	})

	return &types.AnalyticsResponse{
		Analytics:  items,
		ServerTime: time.Now().UnixMilli(),
	}, true, nil
}

func analyticsFromPayload(row model.AnalyticsPayloadRow) (types.ModelAnalytics, error) {
	payload := strings.TrimSpace(row.Payload)
	if payload == "" {
		return types.ModelAnalytics{}, fmt.Errorf("model analytics payload is empty for model %q", row.ModelID)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return types.ModelAnalytics{}, fmt.Errorf("decode model analytics payload for %q: %w", row.ModelID, err)
	}

	var analytics types.ModelAnalytics
	if wrapped, ok := raw["analytics"]; ok {
		if err := json.Unmarshal(wrapped, &analytics); err != nil {
			return types.ModelAnalytics{}, fmt.Errorf("decode wrapped model analytics payload for %q: %w", row.ModelID, err)
		}
	} else if err := json.Unmarshal([]byte(payload), &analytics); err != nil {
		return types.ModelAnalytics{}, fmt.Errorf("decode model analytics payload for %q: %w", row.ModelID, err)
	}

	modelID := strings.TrimSpace(firstNonEmptyString(analytics.ModelId, row.ModelID, analytics.Id))
	if analytics.ModelId == "" {
		analytics.ModelId = modelID
	}
	if analytics.Id == "" {
		analytics.Id = modelID
	}
	if analytics.UpdatedAt == 0 && !row.UpdatedAt.IsZero() {
		analytics.UpdatedAt = float64(row.UpdatedAt.UnixNano()) / float64(time.Second)
	}
	return analytics, nil
}

func analyticsPayloadSource(svcCtx *svc.ServiceContext) (analyticsPayloadReader, bool) {
	if svcCtx == nil || svcCtx.ModelAnalyticsModel == nil {
		return nil, false
	}
	source, ok := any(svcCtx.ModelAnalyticsModel).(analyticsPayloadReader)
	return source, ok
}

func analyticsContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func emptyAnalyticsResponse() *types.AnalyticsResponse {
	return &types.AnalyticsResponse{
		Analytics:  []types.ModelAnalytics{},
		ServerTime: time.Now().UnixMilli(),
	}
}

type analyticsPayloadReader interface {
	AllPayloads(context.Context) ([]model.AnalyticsPayloadRow, error)
	PayloadByModel(context.Context, string) (*model.AnalyticsPayloadRow, error)
}
