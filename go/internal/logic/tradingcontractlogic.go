// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	managerpkg "nof0-api/pkg/manager"
	"nof0-api/pkg/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

const controlNotImplementedMessage = "control endpoint is contract-only until wired to manager guarded command queue"

type TradersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTradersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TradersLogic {
	return &TradersLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *TradersLogic) Traders(req *types.TradersRequest) (*types.TradersResponse, error) {
	if req == nil {
		req = &types.TradersRequest{}
	}
	limit, offset := normalizeLimitOffset(req.Limit, req.Offset, 100)
	traders := configuredTraders(l.svcCtx)
	items := make([]types.TraderSummary, 0, len(traders))
	for _, trader := range traders {
		status := traderStatusFromContext(l.ctx, l.svcCtx, trader)
		if req.Status != "" && status.Status != strings.ToLower(strings.TrimSpace(req.Status)) {
			continue
		}
		if req.ExecutionMode != "" && string(trader.ExecutionMode) != strings.ToLower(strings.TrimSpace(req.ExecutionMode)) {
			continue
		}
		items = append(items, traderSummary(trader, status))
	}
	paged := paginateTraderSummaries(items, limit, offset)
	return &types.TradersResponse{
		Traders:      paged,
		Meta:         listMeta(limit, offset, len(paged), len(items), "manager_config"),
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

type TraderDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTraderDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TraderDetailLogic {
	return &TraderDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *TraderDetailLogic) TraderDetail(req *types.TraderPathRequest) (*types.TraderResponse, error) {
	trader, ok := findConfiguredTrader(l.svcCtx, req.TraderId)
	if !ok {
		return nil, fmt.Errorf("trader %q not found", req.TraderId)
	}
	return &types.TraderResponse{
		Trader:       traderDetail(trader, traderStatusFromContext(l.ctx, l.svcCtx, trader), promptDigest(l.svcCtx, trader.ID)),
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

type TraderStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTraderStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TraderStatusLogic {
	return &TraderStatusLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *TraderStatusLogic) TraderStatus(req *types.TraderPathRequest) (*types.TraderStatusResponse, error) {
	trader, ok := findConfiguredTrader(l.svcCtx, req.TraderId)
	if !ok {
		return nil, fmt.Errorf("trader %q not found", req.TraderId)
	}
	return &types.TraderStatusResponse{
		Status:       traderStatusFromContext(l.ctx, l.svcCtx, trader),
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

type AuditEventsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditEventsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditEventsLogic {
	return &AuditEventsLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *AuditEventsLogic) AuditEvents(req *types.AuditEventsRequest) (*types.AuditEventsResponse, error) {
	if req == nil {
		req = &types.AuditEventsRequest{}
	}
	limit, offset := normalizeLimitOffset(req.Limit, req.Offset, 100)
	if l.svcCtx == nil || l.svcCtx.AuditEventRepo == nil {
		return &types.AuditEventsResponse{
			Events:       []types.AuditEvent{},
			Meta:         listMeta(limit, offset, 0, 0, "audit_repo_unavailable"),
			ServerTimeMs: time.Now().UnixMilli(),
		}, nil
	}

	filter, err := auditEventFilters(req)
	if err != nil {
		return nil, err
	}
	filter.Limit = limit
	filter.Offset = offset
	records, err := l.svcCtx.AuditEventRepo.List(l.ctx, filter)
	if err != nil {
		return nil, err
	}
	events := make([]types.AuditEvent, 0, len(records))
	for i := range records {
		events = append(events, auditEventFromRecord(records[i]))
	}
	return &types.AuditEventsResponse{
		Events:       events,
		Meta:         listMeta(limit, offset, len(events), offset+len(events), "audit_event_repo"),
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

type OrdersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOrdersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrdersLogic {
	return &OrdersLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *OrdersLogic) Orders(req *types.OrdersRequest) (*types.OrdersResponse, error) {
	limit, offset := 100, 0
	if req != nil {
		limit, offset = normalizeLimitOffset(req.Limit, req.Offset, 100)
	}
	return &types.OrdersResponse{
		Orders:       []types.Order{},
		Meta:         listMeta(limit, offset, 0, 0, "orders_not_wired"),
		Status:       "not_available",
		Message:      "orders query contract is defined; persistence/service wiring is pending",
		ServerTimeMs: time.Now().UnixMilli(),
	}, nil
}

type TraderControlLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewTraderControlLogic(ctx context.Context, svcCtx *svc.ServiceContext) *TraderControlLogic {
	return &TraderControlLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *TraderControlLogic) Control(req *types.TraderControlRequest, action string) (*types.TraderControlResponse, error) {
	traderID := ""
	correlationID := ""
	if req != nil {
		traderID = req.TraderId
		correlationID = req.CorrelationId
	}
	return &types.TraderControlResponse{
		Accepted:      false,
		Status:        "not_implemented",
		TraderId:      traderID,
		Action:        action,
		CorrelationId: correlationID,
		Message:       controlNotImplementedMessage,
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

type DecisionActionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewDecisionActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *DecisionActionLogic {
	return &DecisionActionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *DecisionActionLogic) DecisionAction(req *types.DecisionActionRequest, action string) (*types.DecisionActionResponse, error) {
	decisionID := ""
	correlationID := ""
	if req != nil {
		decisionID = req.DecisionId
		correlationID = req.CorrelationId
	}
	return &types.DecisionActionResponse{
		Accepted:      false,
		Status:        "not_implemented",
		DecisionId:    decisionID,
		Action:        action,
		CorrelationId: correlationID,
		Message:       controlNotImplementedMessage,
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

type OrderPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOrderPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrderPreviewLogic {
	return &OrderPreviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *OrderPreviewLogic) OrderPreview(req *types.OrderPreviewRequest) (*types.OrderPreviewResponse, error) {
	correlationID := ""
	if req != nil {
		correlationID = req.CorrelationId
	}
	return &types.OrderPreviewResponse{
		Accepted:      false,
		Status:        "not_implemented",
		CorrelationId: correlationID,
		Message:       controlNotImplementedMessage,
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

type OrderActionLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOrderActionLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OrderActionLogic {
	return &OrderActionLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *OrderActionLogic) OrderAction(req *types.OrderActionRequest, action string) (*types.OrderActionResponse, error) {
	orderID := ""
	correlationID := ""
	if req != nil {
		orderID = req.OrderId
		correlationID = req.CorrelationId
	}
	return &types.OrderActionResponse{
		Accepted:      false,
		Status:        "not_implemented",
		OrderId:       orderID,
		Action:        action,
		CorrelationId: correlationID,
		Message:       controlNotImplementedMessage,
		ServerTimeMs:  time.Now().UnixMilli(),
	}, nil
}

func configuredTraders(svcCtx *svc.ServiceContext) []managerpkg.TraderConfig {
	if svcCtx == nil || svcCtx.ManagerConfig == nil {
		return nil
	}
	return svcCtx.ManagerConfig.Traders
}

func findConfiguredTrader(svcCtx *svc.ServiceContext, traderID string) (managerpkg.TraderConfig, bool) {
	traderID = strings.TrimSpace(traderID)
	for _, trader := range configuredTraders(svcCtx) {
		if trader.ID == traderID {
			return trader, true
		}
	}
	return managerpkg.TraderConfig{}, false
}

func traderSummary(trader managerpkg.TraderConfig, status types.TraderStatus) types.TraderSummary {
	return types.TraderSummary{
		Id:               trader.ID,
		Name:             trader.Name,
		Model:            trader.Model,
		ExchangeProvider: trader.ExchangeProvider,
		MarketProvider:   trader.MarketProvider,
		ExecutionMode:    string(trader.ExecutionMode),
		OrderStyle:       string(trader.OrderStyle),
		AllocationPct:    trader.AllocationPct,
		AutoStart:        trader.AutoStart,
		Status:           status,
	}
}

func traderDetail(trader managerpkg.TraderConfig, status types.TraderStatus, digest string) types.TraderDetail {
	return types.TraderDetail{
		Id:                   trader.ID,
		Name:                 trader.Name,
		Model:                trader.Model,
		ExchangeProvider:     trader.ExchangeProvider,
		MarketProvider:       trader.MarketProvider,
		ExecutionMode:        string(trader.ExecutionMode),
		OrderStyle:           string(trader.OrderStyle),
		MarketIocSlippageBps: trader.MarketIOCSlippageBps,
		DecisionInterval:     trader.DecisionIntervalRaw,
		AllocationPct:        trader.AllocationPct,
		AutoStart:            trader.AutoStart,
		JournalEnabled:       trader.JournalEnabled,
		RiskParams: types.TraderRiskParams{
			MaxPositions:       trader.RiskParams.MaxPositions,
			MaxPositionSizeUsd: trader.RiskParams.MaxPositionSizeUSD,
			MaxMarginUsagePct:  trader.RiskParams.MaxMarginUsagePct,
			MajorCoinLeverage:  trader.RiskParams.MajorCoinLeverage,
			AltcoinLeverage:    trader.RiskParams.AltcoinLeverage,
			MinRiskRewardRatio: trader.RiskParams.MinRiskRewardRatio,
			MinConfidence:      trader.RiskParams.MinConfidence,
			StopLossEnabled:    trader.RiskParams.StopLossEnabled,
			TakeProfitEnabled:  trader.RiskParams.TakeProfitEnabled,
		},
		ExecGuards: types.TraderExecGuards{
			MaxNewPositionsPerCycle: trader.ExecGuards.MaxNewPositionsPerCycle,
			LiquidityThresholdUsd:   trader.ExecGuards.LiquidityThresholdUSD,
			MaxMarginUsagePct:       trader.ExecGuards.MaxMarginUsagePct,
			CooldownAfterClose:      trader.ExecGuards.CooldownAfterCloseRaw,
			PauseDurationOnBreach:   trader.ExecGuards.PauseDurationOnBreachRaw,
		},
		Status:       status,
		PromptDigest: digest,
	}
}

func traderStatusFromContext(ctx context.Context, svcCtx *svc.ServiceContext, trader managerpkg.TraderConfig) types.TraderStatus {
	status := types.TraderStatus{
		TraderId:            trader.ID,
		Status:              "configured",
		ActiveConfigVersion: trader.Version,
	}
	if svcCtx == nil || svcCtx.TraderRuntimeRepo == nil {
		return status
	}
	snapshot, err := svcCtx.TraderRuntimeRepo.GetState(ctx, trader.ID)
	if err != nil || snapshot == nil {
		return status
	}
	status.IsRunning = snapshot.IsRunning
	status.ActiveConfigVersion = snapshot.ActiveConfigVersion
	status.UpdatedAt = formatRFC3339(snapshot.UpdatedAt)
	status.Detail = snapshot.Detail
	if snapshot.IsRunning {
		status.Status = "running"
	} else {
		status.Status = "stopped"
	}
	if snapshot.Detail.Decision != nil {
		if snapshot.Detail.Decision.LastAt != nil {
			status.LastDecisionAt = formatRFC3339(*snapshot.Detail.Decision.LastAt)
		}
		if snapshot.Detail.Decision.NextAt != nil {
			status.NextDecisionAt = formatRFC3339(*snapshot.Detail.Decision.NextAt)
		}
	}
	if snapshot.Detail.Pause != nil {
		status.PauseReason = snapshot.Detail.Pause.Reason
		if snapshot.Detail.Pause.Until != nil {
			status.PausedUntil = formatRFC3339(*snapshot.Detail.Pause.Until)
			if snapshot.Detail.Pause.Until.After(time.Now()) {
				status.Status = "paused"
			}
		}
	}
	return status
}

func promptDigest(svcCtx *svc.ServiceContext, traderID string) string {
	if svcCtx == nil || svcCtx.ManagerPromptDigests == nil {
		return ""
	}
	return svcCtx.ManagerPromptDigests[traderID]
}

func auditEventFilters(req *types.AuditEventsRequest) (repo.AuditEventListFilter, error) {
	var filter repo.AuditEventListFilter
	if v := strings.TrimSpace(req.TraderId); v != "" {
		filter.TraderID = v
	}
	if v := strings.TrimSpace(req.Type); v != "" {
		filter.Type = repo.AuditEventType(v)
	}
	if v := strings.TrimSpace(req.CorrelationId); v != "" {
		filter.CorrelationID = v
	}
	if v := strings.TrimSpace(req.CreatedAfterRfc3339); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return filter, fmt.Errorf("created_after_rfc3339 must be RFC3339: %w", err)
		}
		filter.CreatedAfter = &t
	}
	if v := strings.TrimSpace(req.CreatedBeforeRfc3339); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return filter, fmt.Errorf("created_before_rfc3339 must be RFC3339: %w", err)
		}
		filter.CreatedBefore = &t
	}
	return filter, nil
}

func auditEventFromRecord(row repo.AuditEventRecord) types.AuditEvent {
	var detail any
	if len(row.Detail) > 0 {
		if err := json.Unmarshal(row.Detail, &detail); err != nil {
			detail = row.Detail
		}
	}
	event := types.AuditEvent{
		Id:              row.ID,
		Type:            string(row.Type),
		TraderId:        row.TraderID,
		CycleId:         row.CycleID,
		CorrelationId:   row.CorrelationID,
		Symbol:          row.Symbol,
		Action:          row.Action,
		ModelId:         row.ModelID,
		ModelName:       row.ModelName,
		PromptDigest:    row.PromptDigest,
		ApprovalTokenId: row.ApprovalTokenID,
		Reason:          row.Reason,
		Error:           row.Error,
		Detail:          detail,
		CreatedAt:       formatRFC3339(row.CreatedAt),
	}
	return event
}

func normalizeLimitOffset(limit, offset, defaultLimit int) (int, int) {
	if defaultLimit <= 0 {
		defaultLimit = 100
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func listMeta(limit, offset, count, totalSeen int, source string) types.ListMeta {
	meta := types.ListMeta{
		Limit:  limit,
		Offset: offset,
		Count:  count,
		Source: source,
	}
	if count > 0 && offset+count < totalSeen {
		meta.NextOffset = offset + count
	}
	return meta
}

func paginateTraderSummaries(items []types.TraderSummary, limit, offset int) []types.TraderSummary {
	if offset >= len(items) {
		return []types.TraderSummary{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func formatRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
