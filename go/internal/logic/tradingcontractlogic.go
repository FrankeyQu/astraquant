// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"nof0-api/internal/controlqueue"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	managerpkg "nof0-api/pkg/manager"
	"nof0-api/pkg/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	controlCommandQueuedMessage = "control command queued; no order was submitted"
	controlPlaneAuditTraderID   = "control-plane"
)

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
	if l.svcCtx == nil || l.svcCtx.AuditEventRepo == nil {
		return &types.OrdersResponse{
			Orders:       []types.Order{},
			Meta:         listMeta(limit, offset, 0, 0, "audit_repo_unavailable"),
			Status:       "not_available",
			Message:      "orders read model requires audit event repository wiring",
			ServerTimeMs: time.Now().UnixMilli(),
		}, nil
	}
	orders, totalSeen, err := ordersFromAuditEvents(l.ctx, l.svcCtx.AuditEventRepo, req, limit, offset)
	if err != nil {
		return nil, err
	}
	return &types.OrdersResponse{
		Orders:       orders,
		Meta:         listMeta(limit, offset, len(orders), totalSeen, "audit_event_repo"),
		Status:       "read_only",
		Message:      "orders are derived from immutable audit events; no order action endpoint submits or queues orders",
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
	requestedBy := ""
	reason := ""
	idempotencyKey := ""
	effectiveUntil := ""
	if req != nil {
		traderID = req.TraderId
		correlationID = req.CorrelationId
		requestedBy = req.RequestedBy
		reason = req.Reason
		idempotencyKey = req.IdempotencyKey
		effectiveUntil = req.EffectiveUntil
	}
	control := managerControl(l.svcCtx)
	if control == nil {
		return &types.TraderControlResponse{
			Accepted:         false,
			Status:           "rejected",
			TraderId:         traderID,
			Action:           action,
			CorrelationId:    correlationID,
			Queued:           false,
			ControlPlaneOnly: true,
			Message:          "manager control plane is not configured",
			ServerTimeMs:     time.Now().UnixMilli(),
		}, nil
	}
	result, err := control.Handle(l.ctx, managerpkg.ControlRequest{
		TraderID:       traderID,
		Action:         managerpkg.ControlAction(action),
		RequestedBy:    requestedBy,
		Reason:         reason,
		IdempotencyKey: idempotencyKey,
		CorrelationID:  correlationID,
		EffectiveUntil: effectiveUntil,
	})
	if err != nil {
		return nil, err
	}
	return &types.TraderControlResponse{
		Accepted:         result.Accepted,
		Status:           result.Status,
		TraderId:         result.TraderID,
		Action:           string(result.Action),
		CorrelationId:    result.CorrelationID,
		ControlState:     string(result.State),
		ExecutionMode:    string(result.ExecutionMode),
		Queued:           result.Queued,
		ControlPlaneOnly: result.ControlPlaneOnly,
		Message:          result.Message,
		ServerTimeMs:     time.Now().UnixMilli(),
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
	result, err := enqueueDecisionCommand(l.ctx, l.svcCtx, req, action)
	if err != nil {
		return nil, err
	}
	command := result.Command
	if !result.Reused {
		if err := recordControlCommandAudit(l.ctx, l.svcCtx, command); err != nil {
			if l.svcCtx != nil && l.svcCtx.CommandQueue != nil {
				l.svcCtx.CommandQueue.Remove(command.ID)
			}
			return nil, err
		}
	}
	return &types.DecisionActionResponse{
		Accepted:         true,
		Status:           command.Status,
		DecisionId:       command.DecisionID,
		Action:           command.Action,
		CommandId:        command.ID,
		CorrelationId:    command.CorrelationID,
		Queued:           command.Queued,
		ControlPlaneOnly: command.ControlPlaneOnly,
		Submitted:        command.Submitted,
		Message:          controlCommandQueuedMessage,
		ServerTimeMs:     time.Now().UnixMilli(),
	}, nil
}

func enqueueDecisionCommand(ctx context.Context, svcCtx *svc.ServiceContext, req *types.DecisionActionRequest, action string) (controlqueue.EnqueueResult, error) {
	var input controlqueue.EnqueueRequest
	if req != nil {
		input.DecisionID = req.DecisionId
		input.TraderID = req.TraderId
		input.RequestedBy = req.RequestedBy
		input.Reason = req.Reason
		input.IdempotencyKey = req.IdempotencyKey
		input.CorrelationID = req.CorrelationId
	}
	input.Target = controlqueue.TargetDecision
	input.Action = action
	if err := validateControlCommandInput(input); err != nil {
		return controlqueue.EnqueueResult{}, err
	}
	if svcCtx != nil && svcCtx.ControlCommandRepo != nil {
		record, reused, err := svcCtx.ControlCommandRepo.Enqueue(ctx, controlCommandRecordFromInput(input))
		if err != nil {
			return controlqueue.EnqueueResult{}, err
		}
		return controlqueue.EnqueueResult{Command: controlCommandFromRecord(record), Reused: reused}, nil
	}
	queue := ensureCommandQueue(svcCtx)
	if queue == nil {
		return controlqueue.EnqueueResult{}, fmt.Errorf("control command queue is not configured")
	}
	return queue.Enqueue(input), nil
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
	preview := buildOrderPreview(l.svcCtx, req)
	return &types.OrderPreviewResponse{
		Accepted:      preview.accepted,
		Status:        preview.status,
		PreviewId:     preview.previewID,
		CorrelationId: correlationID,
		Checks:        preview.checks,
		Submitted:     false,
		Message:       preview.message,
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
	result, err := enqueueOrderCommand(l.ctx, l.svcCtx, req, action)
	if err != nil {
		return nil, err
	}
	command := result.Command
	if !result.Reused {
		if err := recordControlCommandAudit(l.ctx, l.svcCtx, command); err != nil {
			if l.svcCtx != nil && l.svcCtx.CommandQueue != nil {
				l.svcCtx.CommandQueue.Remove(command.ID)
			}
			return nil, err
		}
	}
	return &types.OrderActionResponse{
		Accepted:         true,
		Status:           command.Status,
		OrderId:          command.OrderID,
		Action:           command.Action,
		CommandId:        command.ID,
		CorrelationId:    command.CorrelationID,
		Queued:           command.Queued,
		ControlPlaneOnly: command.ControlPlaneOnly,
		Submitted:        command.Submitted,
		Message:          controlCommandQueuedMessage,
		ServerTimeMs:     time.Now().UnixMilli(),
	}, nil
}

func enqueueOrderCommand(ctx context.Context, svcCtx *svc.ServiceContext, req *types.OrderActionRequest, action string) (controlqueue.EnqueueResult, error) {
	var input controlqueue.EnqueueRequest
	if req != nil {
		input.OrderID = req.OrderId
		input.TraderID = req.TraderId
		input.RequestedBy = req.RequestedBy
		input.Reason = req.Reason
		input.IdempotencyKey = req.IdempotencyKey
		input.CorrelationID = req.CorrelationId
	}
	input.Target = controlqueue.TargetOrder
	input.Action = action
	if err := validateControlCommandInput(input); err != nil {
		return controlqueue.EnqueueResult{}, err
	}
	if svcCtx != nil && svcCtx.ControlCommandRepo != nil {
		record, reused, err := svcCtx.ControlCommandRepo.Enqueue(ctx, controlCommandRecordFromInput(input))
		if err != nil {
			return controlqueue.EnqueueResult{}, err
		}
		return controlqueue.EnqueueResult{Command: controlCommandFromRecord(record), Reused: reused}, nil
	}
	queue := ensureCommandQueue(svcCtx)
	if queue == nil {
		return controlqueue.EnqueueResult{}, fmt.Errorf("control command queue is not configured")
	}
	return queue.Enqueue(input), nil
}

func ensureCommandQueue(svcCtx *svc.ServiceContext) *controlqueue.Queue {
	if svcCtx == nil {
		return nil
	}
	if svcCtx.CommandQueue == nil {
		svcCtx.CommandQueue = controlqueue.NewQueue()
	}
	return svcCtx.CommandQueue
}

func validateControlCommandInput(req controlqueue.EnqueueRequest) error {
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action != "approve" && action != "reject" {
		return fmt.Errorf("unsupported control action %q", req.Action)
	}
	switch strings.ToLower(strings.TrimSpace(req.Target)) {
	case controlqueue.TargetDecision:
		if strings.TrimSpace(req.DecisionID) == "" {
			return fmt.Errorf("decision id is required")
		}
	case controlqueue.TargetOrder:
		if strings.TrimSpace(req.OrderID) == "" {
			return fmt.Errorf("order id is required")
		}
	default:
		return fmt.Errorf("unsupported control target %q", req.Target)
	}
	if strings.TrimSpace(req.RequestedBy) == "" {
		return fmt.Errorf("requested_by is required")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return fmt.Errorf("reason is required")
	}
	return nil
}

func recordControlCommandAudit(ctx context.Context, svcCtx *svc.ServiceContext, command controlqueue.Command) error {
	if svcCtx == nil || svcCtx.AuditEventRepo == nil {
		return nil
	}
	detail, err := controlCommandAuditDetail(command)
	if err != nil {
		return err
	}
	_, err = svcCtx.AuditEventRepo.Record(ctx, repo.AuditEventRecord{
		Type:            controlCommandAuditType(command.Action),
		TraderID:        controlCommandAuditTraderID(command.TraderID),
		CorrelationID:   command.CorrelationID,
		Action:          command.Type,
		ApprovalTokenID: command.ID,
		Reason:          command.Reason,
		Detail:          detail,
		CreatedAt:       command.CreatedAt,
	})
	return err
}

func controlCommandRecordFromInput(input controlqueue.EnqueueRequest) repo.ControlCommandRecord {
	return repo.ControlCommandRecord{
		Target:         input.Target,
		DecisionID:     input.DecisionID,
		OrderID:        input.OrderID,
		TraderID:       input.TraderID,
		Action:         input.Action,
		RequestedBy:    input.RequestedBy,
		Reason:         input.Reason,
		IdempotencyKey: input.IdempotencyKey,
		CorrelationID:  input.CorrelationID,
		Detail:         controlCommandInputDetail(input),
	}
}

func controlCommandFromRecord(record repo.ControlCommandRecord) controlqueue.Command {
	return controlqueue.Command{
		ID:               record.ID,
		Type:             record.Type,
		Target:           record.Target,
		DecisionID:       record.DecisionID,
		OrderID:          record.OrderID,
		TraderID:         record.TraderID,
		Action:           record.Action,
		RequestedBy:      record.RequestedBy,
		Reason:           record.Reason,
		IdempotencyKey:   record.IdempotencyKey,
		CorrelationID:    record.CorrelationID,
		Status:           record.Status,
		Queued:           record.Queued,
		ControlPlaneOnly: record.ControlPlaneOnly,
		Submitted:        record.Submitted,
		CreatedAt:        record.CreatedAt,
	}
}

func controlCommandInputDetail(input controlqueue.EnqueueRequest) json.RawMessage {
	detail := map[string]interface{}{
		"target":          input.Target,
		"action":          input.Action,
		"requested_by":    input.RequestedBy,
		"idempotency_key": input.IdempotencyKey,
	}
	if input.DecisionID != "" {
		detail["decision_id"] = input.DecisionID
	}
	if input.OrderID != "" {
		detail["order_id"] = input.OrderID
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}

func controlCommandAuditType(action string) repo.AuditEventType {
	if strings.ToLower(strings.TrimSpace(action)) == "approve" {
		return repo.AuditEventApproved
	}
	return repo.AuditEventPolicyRejected
}

func controlCommandAuditTraderID(traderID string) string {
	if traderID = strings.TrimSpace(traderID); traderID != "" {
		return traderID
	}
	return controlPlaneAuditTraderID
}

func controlCommandAuditDetail(command controlqueue.Command) (json.RawMessage, error) {
	detail := map[string]interface{}{
		"command_id":         command.ID,
		"command_type":       command.Type,
		"target":             command.Target,
		"action":             command.Action,
		"requested_by":       command.RequestedBy,
		"idempotency_key":    command.IdempotencyKey,
		"queued":             command.Queued,
		"status":             command.Status,
		"control_plane_only": command.ControlPlaneOnly,
		"submitted":          command.Submitted,
	}
	if command.DecisionID != "" {
		detail["decision_id"] = command.DecisionID
	}
	if command.OrderID != "" {
		detail["order_id"] = command.OrderID
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

type orderPreviewResult struct {
	accepted  bool
	status    string
	previewID string
	checks    orderPreviewChecks
	message   string
}

type orderPreviewChecks struct {
	ControlPlaneOnly bool                `json:"control_plane_only"`
	Submitted        bool                `json:"submitted"`
	TraderID         string              `json:"trader_id,omitempty"`
	ExecutionMode    string              `json:"execution_mode,omitempty"`
	DecisionID       string              `json:"decision_id,omitempty"`
	Overall          string              `json:"overall"`
	Checks           []orderPreviewCheck `json:"checks"`
	NormalizedOrders []orderPreviewOrder `json:"normalized_orders,omitempty"`
	RiskContext      interface{}         `json:"risk_context,omitempty"`
}

type orderPreviewCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type orderPreviewOrder struct {
	Symbol     string  `json:"symbol"`
	Side       string  `json:"side"`
	Type       string  `json:"type"`
	Quantity   float64 `json:"quantity"`
	LimitPrice float64 `json:"limit_price,omitempty"`
}

func buildOrderPreview(svcCtx *svc.ServiceContext, req *types.OrderPreviewRequest) orderPreviewResult {
	checks := orderPreviewChecks{
		ControlPlaneOnly: true,
		Submitted:        false,
		Overall:          "rejected",
		Checks:           []orderPreviewCheck{},
	}
	if req == nil {
		checks.Checks = append(checks.Checks, rejectedPreviewCheck("request_shape", "request body is required"))
		return orderPreviewResult{
			accepted: false,
			status:   "rejected",
			checks:   checks,
			message:  "preview rejected; no order was submitted or queued",
		}
	}
	traderID := strings.TrimSpace(req.TraderId)
	checks.TraderID = traderID
	checks.DecisionID = strings.TrimSpace(req.DecisionId)
	checks.RiskContext = req.RiskContext
	trader, ok := findConfiguredTrader(svcCtx, traderID)
	if !ok {
		checks.Checks = append(checks.Checks, rejectedPreviewCheck("trader_exists", fmt.Sprintf("trader %q not found", traderID)))
		return orderPreviewResult{
			accepted: false,
			status:   "rejected",
			checks:   checks,
			message:  "preview rejected; no order was submitted or queued",
		}
	}
	checks.ExecutionMode = string(trader.ExecutionMode)
	checks.Checks = append(checks.Checks, acceptedPreviewCheck("trader_exists", "configured trader found"))
	switch trader.ExecutionMode {
	case managerpkg.ExecutionModePaper, managerpkg.ExecutionModeTestnet:
		checks.Checks = append(checks.Checks, acceptedPreviewCheck("execution_mode", "paper/testnet preview only"))
	case managerpkg.ExecutionModeLive:
		checks.Checks = append(checks.Checks, acceptedPreviewCheck("execution_mode", "live trader preview only; no submission path is exposed"))
	default:
		checks.Checks = append(checks.Checks, rejectedPreviewCheck("execution_mode", fmt.Sprintf("unsupported execution_mode %q", trader.ExecutionMode)))
	}
	if len(req.Orders) == 0 {
		checks.Checks = append(checks.Checks, rejectedPreviewCheck("orders_present", "at least one order is required"))
	}
	for i, order := range req.Orders {
		normalized, orderChecks := normalizePreviewOrder(i, order)
		checks.Checks = append(checks.Checks, orderChecks...)
		if normalized.Symbol != "" || normalized.Side != "" || normalized.Type != "" || normalized.Quantity != 0 || normalized.LimitPrice != 0 {
			checks.NormalizedOrders = append(checks.NormalizedOrders, normalized)
		}
	}
	rejected := false
	for _, check := range checks.Checks {
		if check.Status == "rejected" {
			rejected = true
			break
		}
	}
	if rejected {
		checks.Overall = "rejected"
		return orderPreviewResult{
			accepted: false,
			status:   "rejected",
			checks:   checks,
			message:  "preview rejected; no order was submitted or queued",
		}
	}
	checks.Overall = "preview_only"
	checks.Checks = append(checks.Checks, acceptedPreviewCheck("submission", "preview only; no order was submitted or queued"))
	return orderPreviewResult{
		accepted:  true,
		status:    "preview_only",
		previewID: previewID(req, checks.NormalizedOrders),
		checks:    checks,
		message:   "preview only; no order was submitted or queued",
	}
}

func normalizePreviewOrder(index int, order types.Order) (orderPreviewOrder, []orderPreviewCheck) {
	normalized := orderPreviewOrder{
		Symbol:     strings.ToUpper(strings.TrimSpace(order.Symbol)),
		Side:       strings.ToLower(strings.TrimSpace(order.Side)),
		Type:       strings.ToLower(strings.TrimSpace(order.Type)),
		Quantity:   order.Quantity,
		LimitPrice: order.LimitPrice,
	}
	checks := []orderPreviewCheck{}
	prefix := fmt.Sprintf("orders[%d]", index)
	if normalized.Symbol == "" {
		checks = append(checks, rejectedPreviewCheck(prefix+".symbol", "symbol is required"))
	} else {
		checks = append(checks, acceptedPreviewCheck(prefix+".symbol", "symbol normalized"))
	}
	if normalized.Side == "" {
		checks = append(checks, rejectedPreviewCheck(prefix+".side", "side is required"))
	} else {
		checks = append(checks, acceptedPreviewCheck(prefix+".side", "side normalized"))
	}
	if normalized.Type == "" {
		checks = append(checks, rejectedPreviewCheck(prefix+".type", "type is required"))
	} else {
		checks = append(checks, acceptedPreviewCheck(prefix+".type", "type normalized"))
	}
	if normalized.Quantity <= 0 {
		checks = append(checks, rejectedPreviewCheck(prefix+".quantity", "quantity must be positive"))
	} else {
		checks = append(checks, acceptedPreviewCheck(prefix+".quantity", "quantity is positive"))
	}
	if strings.Contains(normalized.Type, "limit") && normalized.LimitPrice <= 0 {
		checks = append(checks, rejectedPreviewCheck(prefix+".limit_price", "limit orders require a positive limit_price"))
	}
	if normalized.LimitPrice < 0 {
		checks = append(checks, rejectedPreviewCheck(prefix+".limit_price", "limit_price cannot be negative"))
	}
	return normalized, checks
}

func acceptedPreviewCheck(name, message string) orderPreviewCheck {
	return orderPreviewCheck{Name: name, Status: "accepted", Message: message}
}

func rejectedPreviewCheck(name, message string) orderPreviewCheck {
	return orderPreviewCheck{Name: name, Status: "rejected", Message: message}
}

func previewID(req *types.OrderPreviewRequest, orders []orderPreviewOrder) string {
	payload := struct {
		TraderID      string              `json:"trader_id"`
		DecisionID    string              `json:"decision_id,omitempty"`
		CorrelationID string              `json:"correlation_id,omitempty"`
		Orders        []orderPreviewOrder `json:"orders"`
	}{
		TraderID:      strings.TrimSpace(req.TraderId),
		DecisionID:    strings.TrimSpace(req.DecisionId),
		CorrelationID: strings.TrimSpace(req.CorrelationId),
		Orders:        orders,
	}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "preview-" + hex.EncodeToString(sum[:8])
}

func configuredTraders(svcCtx *svc.ServiceContext) []managerpkg.TraderConfig {
	if svcCtx == nil || svcCtx.ManagerConfig == nil {
		return nil
	}
	return svcCtx.ManagerConfig.Traders
}

func managerControl(svcCtx *svc.ServiceContext) *managerpkg.ControlPlane {
	if svcCtx == nil {
		return nil
	}
	if svcCtx.ManagerControl != nil {
		return svcCtx.ManagerControl
	}
	if svcCtx.ManagerConfig == nil {
		return nil
	}
	return managerpkg.NewControlPlane(svcCtx.ManagerConfig, svcCtx.TraderRuntimeRepo)
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
	if svcCtx != nil && svcCtx.ManagerControl != nil {
		if snapshot, ok := svcCtx.ManagerControl.Snapshot(trader.ID); ok {
			return traderStatusFromControlSnapshot(trader, snapshot)
		}
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

func traderStatusFromControlSnapshot(trader managerpkg.TraderConfig, snapshot managerpkg.ControlStateSnapshot) types.TraderStatus {
	status := types.TraderStatus{
		TraderId:            trader.ID,
		Status:              string(snapshot.State),
		IsRunning:           snapshot.State == managerpkg.TraderStateRunning,
		ActiveConfigVersion: snapshot.ActiveConfigVersion,
		UpdatedAt:           formatRFC3339(snapshot.UpdatedAt),
		Detail: map[string]any{
			"control_plane_only": true,
			"last_action":        string(snapshot.LastAction),
			"correlation_id":     snapshot.CorrelationID,
			"requested_by":       snapshot.RequestedBy,
			"reason":             snapshot.Reason,
		},
	}
	if status.ActiveConfigVersion == 0 {
		status.ActiveConfigVersion = trader.Version
	}
	if snapshot.PauseUntil != nil {
		status.PausedUntil = formatRFC3339(*snapshot.PauseUntil)
	}
	status.PauseReason = snapshot.PauseReason
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

func ordersFromAuditEvents(ctx context.Context, auditRepo repo.AuditEventRepository, req *types.OrdersRequest, limit, offset int) ([]types.Order, int, error) {
	if auditRepo == nil {
		return []types.Order{}, 0, nil
	}
	if req == nil {
		req = &types.OrdersRequest{}
	}
	eventTypes := orderEventTypesForStatus(req.Status)
	if len(eventTypes) == 0 {
		return []types.Order{}, 0, nil
	}
	scanLimit := limit + offset
	if scanLimit <= 0 {
		scanLimit = 100
	}
	if scanLimit > 500 {
		scanLimit = 500
	}
	var records []repo.AuditEventRecord
	for _, eventType := range eventTypes {
		rows, err := auditRepo.List(ctx, repo.AuditEventListFilter{
			TraderID: strings.TrimSpace(req.TraderId),
			Type:     eventType,
			Limit:    scanLimit,
			Offset:   0,
		})
		if err != nil {
			return nil, 0, err
		}
		records = append(records, rows...)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].ID > records[j].ID
		}
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	symbol := strings.ToUpper(strings.TrimSpace(req.Symbol))
	filtered := make([]types.Order, 0, len(records))
	for _, record := range records {
		order := orderFromAuditEvent(record)
		if symbol != "" && strings.ToUpper(order.Symbol) != symbol {
			continue
		}
		filtered = append(filtered, order)
	}
	totalSeen := len(filtered)
	return paginateOrders(filtered, limit, offset), totalSeen, nil
}

func orderEventTypesForStatus(status string) []repo.AuditEventType {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return []repo.AuditEventType{repo.AuditEventOrderSubmitted, repo.AuditEventOrderFailed}
	case "submitted", "sent", "filled", "open", string(repo.AuditEventOrderSubmitted):
		return []repo.AuditEventType{repo.AuditEventOrderSubmitted}
	case "failed", "error", "rejected", string(repo.AuditEventOrderFailed):
		return []repo.AuditEventType{repo.AuditEventOrderFailed}
	default:
		return nil
	}
}

func orderFromAuditEvent(record repo.AuditEventRecord) types.Order {
	detail := auditDetailMap(record.Detail)
	order := types.Order{
		Id:            fmt.Sprintf("audit-%d", record.ID),
		TraderId:      record.TraderID,
		Symbol:        strings.ToUpper(strings.TrimSpace(record.Symbol)),
		Side:          sideFromAuditAction(record.Action),
		Type:          orderTypeFromAuditReason(record.Reason),
		Status:        orderStatusFromAuditType(record.Type),
		Quantity:      detailFloat(detail, "quantity"),
		LimitPrice:    detailFloat(detail, "price"),
		CreatedAt:     formatRFC3339(record.CreatedAt),
		UpdatedAt:     formatRFC3339(record.CreatedAt),
		CorrelationId: record.CorrelationID,
		Detail:        detail,
	}
	if order.Symbol == "" {
		order.Symbol = strings.ToUpper(detailString(detail, "symbol"))
	}
	if order.Quantity == 0 {
		order.Quantity = detailFloat(detail, "size")
	}
	if order.LimitPrice == 0 {
		order.LimitPrice = detailFloat(detail, "limit_price")
	}
	if action := strings.ToLower(strings.TrimSpace(record.Action)); action != "" {
		detail["action"] = action
	}
	if record.ApprovalTokenID != "" {
		detail["approval_token_id"] = record.ApprovalTokenID
	}
	detail["audit_event_id"] = record.ID
	return order
}

func auditDetailMap(raw json.RawMessage) map[string]any {
	detail := map[string]any{}
	if len(raw) == 0 {
		return detail
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return map[string]any{"raw_detail": string(raw)}
	}
	return detail
}

func orderStatusFromAuditType(eventType repo.AuditEventType) string {
	if eventType == repo.AuditEventOrderFailed {
		return "failed"
	}
	return "submitted"
}

func orderTypeFromAuditReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	switch reason {
	case "market_ioc":
		return "market_ioc"
	case "limit_ioc":
		return "limit_ioc"
	case "close_position":
		return "close"
	default:
		return reason
	}
}

func sideFromAuditAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "close_short":
		return "buy"
	case "open_short", "close_long":
		return "sell"
	default:
		return strings.ToLower(strings.TrimSpace(action))
	}
}

func detailFloat(detail map[string]any, key string) float64 {
	value, ok := detail[key]
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f
	default:
		return 0
	}
}

func detailString(detail map[string]any, key string) string {
	value, ok := detail[key]
	if !ok {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

func paginateOrders(items []types.Order, limit, offset int) []types.Order {
	if offset >= len(items) {
		return []types.Order{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
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
