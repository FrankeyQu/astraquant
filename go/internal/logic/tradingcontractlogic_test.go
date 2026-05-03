package logic

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/controlqueue"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	executorpkg "nof0-api/pkg/executor"
	managerpkg "nof0-api/pkg/manager"
	"nof0-api/pkg/repo"
)

func TestTradingContractTradersFromManagerConfig(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		ManagerConfig: &managerpkg.Config{
			Traders: []managerpkg.TraderConfig{
				{
					ID:               "alpha",
					Name:             "Alpha",
					Model:            "model-a",
					ExchangeProvider: "paper",
					MarketProvider:   "market",
					ExecutionMode:    managerpkg.ExecutionModePaper,
					OrderStyle:       managerpkg.OrderStyleLimitIOC,
					AllocationPct:    25,
				},
			},
		},
		ManagerPromptDigests: map[string]string{"alpha": "digest-1"},
	}

	resp, err := NewTradersLogic(context.Background(), svcCtx).Traders(&types.TradersRequest{})
	require.NoError(t, err)
	require.Len(t, resp.Traders, 1)
	require.Equal(t, "alpha", resp.Traders[0].Id)
	require.Equal(t, "configured", resp.Traders[0].Status.Status)

	detail, err := NewTraderDetailLogic(context.Background(), svcCtx).TraderDetail(&types.TraderPathRequest{TraderId: "alpha"})
	require.NoError(t, err)
	require.Equal(t, "digest-1", detail.Trader.PromptDigest)
}

func TestTradingContractControlRejectsUnknownTrader(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	orderResp, err := NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{})
	require.NoError(t, err)
	require.Equal(t, "not_available", orderResp.Status)
	require.Equal(t, "audit_repo_unavailable", orderResp.Meta.Source)
	require.Empty(t, orderResp.Orders)

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{TraderId: "missing"}, "start")
	require.NoError(t, err)
	require.False(t, controlResp.Accepted)
	require.Equal(t, "rejected", controlResp.Status)
	require.False(t, controlResp.Queued)
}

func TestOrdersUsesAuditEventsReadModel(t *testing.T) {
	older := time.Date(2026, 5, 1, 1, 1, 0, 0, time.UTC)
	newer := older.Add(time.Minute)
	auditRepo := &fakeAuditEventRepo{
		records: []repo.AuditEventRecord{
			{
				ID:              11,
				Type:            repo.AuditEventOrderSubmitted,
				TraderID:        "paper",
				CorrelationID:   "corr-11",
				Symbol:          "ETH",
				Action:          "open_long",
				ApprovalTokenID: "tok-11",
				Reason:          "limit_ioc",
				Detail:          json.RawMessage(`{"price":"3100.5","quantity":"1.25","cloid":"order-11"}`),
				CreatedAt:       older,
			},
			{
				ID:            12,
				Type:          repo.AuditEventOrderFailed,
				TraderID:      "paper",
				CorrelationID: "corr-12",
				Symbol:        "BTC",
				Action:        "open_short",
				Reason:        "market_ioc",
				Error:         "exchange rejected",
				Detail:        json.RawMessage(`{"price":90000,"quantity":0.2}`),
				CreatedAt:     newer,
			},
		},
	}
	svcCtx := &svc.ServiceContext{AuditEventRepo: auditRepo}

	resp, err := NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{
		TraderId: "paper",
		Symbol:   "eth",
		Limit:    10,
	})

	require.NoError(t, err)
	require.Equal(t, "read_only", resp.Status)
	require.Equal(t, "audit_event_repo", resp.Meta.Source)
	require.Len(t, resp.Orders, 1)
	require.Equal(t, "audit-11", resp.Orders[0].Id)
	require.Equal(t, "paper", resp.Orders[0].TraderId)
	require.Equal(t, "ETH", resp.Orders[0].Symbol)
	require.Equal(t, "buy", resp.Orders[0].Side)
	require.Equal(t, "limit_ioc", resp.Orders[0].Type)
	require.Equal(t, "submitted", resp.Orders[0].Status)
	require.Equal(t, 1.25, resp.Orders[0].Quantity)
	require.Equal(t, 3100.5, resp.Orders[0].LimitPrice)
	require.Equal(t, "corr-11", resp.Orders[0].CorrelationId)
}

func TestOrdersCanFilterFailedAuditEvents(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 1, 1, 0, 0, time.UTC)
	auditRepo := &fakeAuditEventRepo{
		records: []repo.AuditEventRecord{
			{
				ID:            21,
				Type:          repo.AuditEventOrderFailed,
				TraderID:      "paper",
				CorrelationID: "corr-21",
				Symbol:        "SOL",
				Action:        "open_short",
				Reason:        "market_ioc",
				Detail:        json.RawMessage(`{"price":150,"quantity":2}`),
				CreatedAt:     createdAt,
			},
		},
	}
	svcCtx := &svc.ServiceContext{AuditEventRepo: auditRepo}

	resp, err := NewOrdersLogic(context.Background(), svcCtx).Orders(&types.OrdersRequest{Status: "failed"})

	require.NoError(t, err)
	require.Len(t, resp.Orders, 1)
	require.Equal(t, "failed", resp.Orders[0].Status)
	require.Equal(t, "sell", resp.Orders[0].Side)
	require.Equal(t, repo.AuditEventOrderFailed, auditRepo.filters[len(auditRepo.filters)-1].Type)
}

func TestTradingContractControlPaperStartAccepted(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{
		TraderId:      "paper",
		RequestedBy:   "test",
		Reason:        "smoke",
		CorrelationId: "corr-1",
	}, "start")

	require.NoError(t, err)
	require.True(t, controlResp.Accepted)
	require.Equal(t, "accepted", controlResp.Status)
	require.Equal(t, "running", controlResp.ControlState)
	require.Equal(t, "paper", controlResp.ExecutionMode)
	require.Equal(t, "corr-1", controlResp.CorrelationId)
	require.False(t, controlResp.Queued)
	require.True(t, controlResp.ControlPlaneOnly)

	statusResp, err := NewTraderStatusLogic(context.Background(), svcCtx).TraderStatus(&types.TraderPathRequest{TraderId: "paper"})
	require.NoError(t, err)
	require.Equal(t, "running", statusResp.Status.Status)
	require.True(t, statusResp.Status.IsRunning)
}

func TestTradingContractControlLiveStartRejectedByDefault(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("ASTRAQUANT_ALLOW_LIVE_TRADING", "")
	t.Setenv("ASTRAQUANT_LIVE_TRADING_ACK", "")
	svcCtx := testTradingControlServiceContext()

	controlResp, err := NewTraderControlLogic(context.Background(), svcCtx).Control(&types.TraderControlRequest{TraderId: "live"}, "start")

	require.NoError(t, err)
	require.False(t, controlResp.Accepted)
	require.Equal(t, "rejected", controlResp.Status)
	require.Equal(t, "live", controlResp.ExecutionMode)
	require.Contains(t, controlResp.Message, "ASTRAQUANT_ALLOW_LIVE_TRADING")
}

func TestTradingContractControlPauseResumeStateFlow(t *testing.T) {
	svcCtx := testTradingControlServiceContext()
	logic := NewTraderControlLogic(context.Background(), svcCtx)

	start, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet"}, "start")
	require.NoError(t, err)
	require.True(t, start.Accepted)

	pause, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet", Reason: "operator pause"}, "pause")
	require.NoError(t, err)
	require.True(t, pause.Accepted)
	require.Equal(t, "paused", pause.ControlState)

	statusResp, err := NewTraderStatusLogic(context.Background(), svcCtx).TraderStatus(&types.TraderPathRequest{TraderId: "testnet"})
	require.NoError(t, err)
	require.Equal(t, "paused", statusResp.Status.Status)
	require.False(t, statusResp.Status.IsRunning)
	require.Equal(t, "operator pause", statusResp.Status.PauseReason)

	resume, err := logic.Control(&types.TraderControlRequest{TraderId: "testnet"}, "resume")
	require.NoError(t, err)
	require.True(t, resume.Accepted)
	require.Equal(t, "running", resume.ControlState)
}

func TestTradingContractStatusIncludesRiskState(t *testing.T) {
	now := time.Now().UTC()
	trader := managerpkg.TraderConfig{
		ID:               "risk",
		Name:             "Risk",
		ExchangeProvider: "sim",
		MarketProvider:   "market",
		ExecutionMode:    managerpkg.ExecutionModePaper,
		Version:          3,
		RiskParams: managerpkg.RiskParameters{
			MaxDailyLossUSD: 500,
			MaxDailyLossPct: 10,
		},
	}
	svcCtx := &svc.ServiceContext{
		ManagerConfig: &managerpkg.Config{Traders: []managerpkg.TraderConfig{trader}},
		TraderRuntimeRepo: &fakeTraderRuntimeRepo{
			state: &repo.RuntimeStateSnapshot{
				RuntimeStateRecord: repo.RuntimeStateRecord{
					TraderID:            trader.ID,
					ActiveConfigVersion: 3,
					IsRunning:           true,
					Detail: repo.RuntimeStateDetail{
						Risk: &repo.RuntimeRiskDetail{
							Daily: &repo.RuntimeDailyRiskDetail{
								Date:           now.Format("2006-01-02"),
								StartEquityUSD: 10_000,
								LastEquityUSD:  9_250,
								UpdatedAt:      &now,
							},
							Circuit: &repo.RuntimeRiskCircuitDetail{
								Blocked:     true,
								Date:        now.Format("2006-01-02"),
								Reason:      "daily loss 750.00 exceeds limit 500.00",
								TriggeredAt: &now,
							},
						},
					},
				},
				UpdatedAt: now,
			},
		},
	}

	resp, err := NewTraderStatusLogic(context.Background(), svcCtx).TraderStatus(&types.TraderPathRequest{TraderId: "risk"})

	require.NoError(t, err)
	require.Equal(t, "paused", resp.Status.Status)
	require.False(t, resp.Status.IsRunning)
	require.NotNil(t, resp.Status.RiskState)
	require.True(t, resp.Status.RiskState.Blocked)
	require.Equal(t, 750.0, resp.Status.RiskState.DailyLossUsd)
	require.Equal(t, 500.0, resp.Status.RiskState.DailyLossLimitUsd)
	require.Contains(t, resp.Status.RiskState.BlockReason, "daily loss")
}

func TestTradingContractOrderPreviewDoesNotSubmitOrders(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	resp, err := NewOrderPreviewLogic(context.Background(), svcCtx).OrderPreview(&types.OrderPreviewRequest{
		TraderId:      "paper",
		DecisionId:    "decision-1",
		CorrelationId: "corr-preview",
		Orders: []types.Order{
			{Symbol: "eth", Side: "buy", Type: "limit", Quantity: 1.25, LimitPrice: 3100},
		},
	})

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "preview_only", resp.Status)
	require.False(t, resp.Submitted)
	require.NotEmpty(t, resp.PreviewId)
	require.Equal(t, "corr-preview", resp.CorrelationId)
	checks, ok := resp.Checks.(orderPreviewChecks)
	require.True(t, ok)
	require.True(t, checks.ControlPlaneOnly)
	require.False(t, checks.Submitted)
	require.Equal(t, "ETH", checks.NormalizedOrders[0].Symbol)
}

func TestTradingContractOrderPreviewRejectsUnknownTrader(t *testing.T) {
	svcCtx := testTradingControlServiceContext()

	resp, err := NewOrderPreviewLogic(context.Background(), svcCtx).OrderPreview(&types.OrderPreviewRequest{
		TraderId: "missing",
		Orders:   []types.Order{{Symbol: "btc", Side: "buy", Type: "market", Quantity: 1}},
	})

	require.NoError(t, err)
	require.False(t, resp.Accepted)
	require.Equal(t, "rejected", resp.Status)
	require.False(t, resp.Submitted)
}

func TestDecisionActionApproveQueuesCommandAndWritesAudit(t *testing.T) {
	auditRepo := &fakeAuditEventRepo{}
	svcCtx := &svc.ServiceContext{
		CommandQueue:   controlqueue.NewQueue(),
		AuditEventRepo: auditRepo,
	}

	resp, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:     "decision-1",
		TraderId:       "paper",
		RequestedBy:    "operator",
		Reason:         "manual approval",
		Decision:       testDecisionApprovalPayload(t),
		IdempotencyKey: "idem-1",
		CorrelationId:  "corr-approval",
	}, "approve")

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "queued", resp.Status)
	require.Equal(t, "decision-1", resp.DecisionId)
	require.Equal(t, "approve", resp.Action)
	require.NotEmpty(t, resp.CommandId)
	require.Equal(t, "corr-approval", resp.CorrelationId)
	require.True(t, resp.Queued)
	require.True(t, resp.ControlPlaneOnly)
	require.False(t, resp.Submitted)
	require.Len(t, svcCtx.CommandQueue.List(), 1)

	require.Len(t, auditRepo.recorded, 1)
	record := auditRepo.recorded[0]
	require.Equal(t, repo.AuditEventApproved, record.Type)
	require.Equal(t, "paper", record.TraderID)
	require.Equal(t, "corr-approval", record.CorrelationID)
	require.Equal(t, "decision_approve", record.Action)
	require.Equal(t, resp.CommandId, record.ApprovalTokenID)
	require.Equal(t, "manual approval", record.Reason)

	var detail map[string]interface{}
	require.NoError(t, json.Unmarshal(record.Detail, &detail))
	require.Equal(t, "decision-1", detail["decision_id"])
	require.Equal(t, false, detail["submitted"])
	require.Equal(t, true, detail["queued"])
}

func TestDecisionActionApproveRequiresDecisionPayload(t *testing.T) {
	svcCtx := &svc.ServiceContext{
		CommandQueue: controlqueue.NewQueue(),
	}

	resp, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:  "decision-missing",
		TraderId:    "paper",
		RequestedBy: "operator",
		Reason:      "manual approval",
	}, "approve")

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "decision payload is required")
}

func TestDecisionActionRejectQueuesPolicyAuditWithFallbackTrader(t *testing.T) {
	auditRepo := &fakeAuditEventRepo{}
	svcCtx := &svc.ServiceContext{
		CommandQueue:   controlqueue.NewQueue(),
		AuditEventRepo: auditRepo,
	}

	resp, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:  "decision-2",
		RequestedBy: "operator",
		Reason:      "risk reject",
	}, "reject")

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "queued", resp.Status)
	require.NotEmpty(t, resp.CommandId)
	require.Equal(t, resp.CommandId, resp.CorrelationId)
	require.Len(t, auditRepo.recorded, 1)
	require.Equal(t, repo.AuditEventPolicyRejected, auditRepo.recorded[0].Type)
	require.Equal(t, "control-plane", auditRepo.recorded[0].TraderID)
	require.Equal(t, "decision_reject", auditRepo.recorded[0].Action)
}

func TestDecisionActionIdempotencyReusesQueuedCommand(t *testing.T) {
	auditRepo := &fakeAuditEventRepo{}
	svcCtx := &svc.ServiceContext{
		CommandQueue:   controlqueue.NewQueue(),
		AuditEventRepo: auditRepo,
	}
	req := &types.DecisionActionRequest{
		DecisionId:     "decision-idem",
		TraderId:       "paper",
		RequestedBy:    "operator",
		Reason:         "same approval",
		Decision:       testDecisionApprovalPayload(t),
		IdempotencyKey: "same-key",
	}

	first, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(req, "approve")
	require.NoError(t, err)
	second, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(req, "approve")
	require.NoError(t, err)

	require.Equal(t, first.CommandId, second.CommandId)
	require.Len(t, svcCtx.CommandQueue.List(), 1)
	require.Len(t, auditRepo.recorded, 1)
}

func TestOrderActionQueuesWithoutAuditRepository(t *testing.T) {
	svcCtx := &svc.ServiceContext{CommandQueue: controlqueue.NewQueue()}

	resp, err := NewOrderActionLogic(context.Background(), svcCtx).OrderAction(&types.OrderActionRequest{
		OrderId:     "order-1",
		TraderId:    "paper",
		RequestedBy: "operator",
		Reason:      "queue only",
	}, "approve")

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "queued", resp.Status)
	require.Equal(t, "order-1", resp.OrderId)
	require.NotEmpty(t, resp.CommandId)
	require.True(t, resp.Queued)
	require.True(t, resp.ControlPlaneOnly)
	require.False(t, resp.Submitted)
	require.Len(t, svcCtx.CommandQueue.List(), 1)
}

func TestDecisionActionUsesPersistentCommandRepository(t *testing.T) {
	auditRepo := &fakeAuditEventRepo{}
	commandRepo := &fakeControlCommandRepo{}
	svcCtx := &svc.ServiceContext{
		ControlCommandRepo: commandRepo,
		AuditEventRepo:     auditRepo,
	}

	resp, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:     "decision-persisted",
		TraderId:       "paper",
		RequestedBy:    "operator",
		Reason:         "persist command",
		Decision:       testDecisionApprovalPayload(t),
		IdempotencyKey: "persist-key",
	}, "approve")

	require.NoError(t, err)
	require.True(t, resp.Accepted)
	require.Equal(t, "queued", resp.Status)
	require.Equal(t, "cmd-persisted", resp.CommandId)
	require.Equal(t, "cmd-persisted", resp.CorrelationId)
	require.Nil(t, svcCtx.CommandQueue)
	require.Len(t, commandRepo.enqueued, 1)
	require.Equal(t, "decision-persisted", commandRepo.enqueued[0].DecisionID)
	var payload struct {
		DecisionID string               `json:"decision_id"`
		Decision   executorpkg.Decision `json:"decision"`
	}
	require.NoError(t, json.Unmarshal(commandRepo.enqueued[0].Detail, &payload))
	require.Equal(t, "decision-persisted", payload.DecisionID)
	require.Equal(t, "open_long", payload.Decision.Action)
	require.Equal(t, "BTC", payload.Decision.Symbol)
	require.Len(t, auditRepo.recorded, 1)
	require.Equal(t, "cmd-persisted", auditRepo.recorded[0].ApprovalTokenID)
}

func TestDecisionActionSkipsAuditForReusedPersistentCommand(t *testing.T) {
	auditRepo := &fakeAuditEventRepo{}
	commandRepo := &fakeControlCommandRepo{reused: true}
	svcCtx := &svc.ServiceContext{
		ControlCommandRepo: commandRepo,
		AuditEventRepo:     auditRepo,
	}

	resp, err := NewDecisionActionLogic(context.Background(), svcCtx).DecisionAction(&types.DecisionActionRequest{
		DecisionId:     "decision-reused",
		TraderId:       "paper",
		RequestedBy:    "operator",
		Reason:         "same command",
		Decision:       testDecisionApprovalPayload(t),
		IdempotencyKey: "same-key",
	}, "approve")

	require.NoError(t, err)
	require.Equal(t, "cmd-persisted", resp.CommandId)
	require.Empty(t, auditRepo.recorded)
}

func testTradingControlServiceContext() *svc.ServiceContext {
	cfg := &managerpkg.Config{
		Traders: []managerpkg.TraderConfig{
			{ID: "paper", Name: "Paper", ExchangeProvider: "sim", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModePaper},
			{ID: "testnet", Name: "Testnet", ExchangeProvider: "hyperliquid-testnet", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModeTestnet},
			{ID: "live", Name: "Live", ExchangeProvider: "hyperliquid", MarketProvider: "market", ExecutionMode: managerpkg.ExecutionModeLive},
		},
	}
	return &svc.ServiceContext{
		ManagerConfig:  cfg,
		ManagerControl: managerpkg.NewControlPlane(cfg, nil),
	}
}

func TestAuditEventsUsesRepositoryContract(t *testing.T) {
	createdAt := time.Date(2026, 5, 1, 1, 2, 3, 0, time.UTC)
	auditRepo := &fakeAuditEventRepo{
		records: []repo.AuditEventRecord{
			{
				ID:            10,
				Type:          repo.AuditEventOrderSubmitted,
				TraderID:      "alpha",
				CorrelationID: "corr-1",
				Detail:        json.RawMessage(`{"ok":true}`),
				CreatedAt:     createdAt,
			},
		},
	}
	svcCtx := &svc.ServiceContext{AuditEventRepo: auditRepo}

	resp, err := NewAuditEventsLogic(context.Background(), svcCtx).AuditEvents(&types.AuditEventsRequest{
		TraderId:      "alpha",
		Type:          string(repo.AuditEventOrderSubmitted),
		CorrelationId: "corr-1",
		Limit:         5,
		Offset:        2,
	})

	require.NoError(t, err)
	require.Equal(t, repo.AuditEventListFilter{
		TraderID:      "alpha",
		Type:          repo.AuditEventOrderSubmitted,
		CorrelationID: "corr-1",
		Limit:         5,
		Offset:        2,
	}, auditRepo.filter)
	require.Len(t, resp.Events, 1)
	require.Equal(t, int64(10), resp.Events[0].Id)
	require.Equal(t, "audit_event_repo", resp.Meta.Source)
}

func TestAuditEventsNoRepositoryDegradesSafely(t *testing.T) {
	resp, err := NewAuditEventsLogic(context.Background(), &svc.ServiceContext{}).AuditEvents(&types.AuditEventsRequest{})

	require.NoError(t, err)
	require.Empty(t, resp.Events)
	require.Equal(t, "audit_repo_unavailable", resp.Meta.Source)
}

type fakeAuditEventRepo struct {
	filter   repo.AuditEventListFilter
	filters  []repo.AuditEventListFilter
	records  []repo.AuditEventRecord
	recorded []repo.AuditEventRecord
}

func (r *fakeAuditEventRepo) Record(_ context.Context, record repo.AuditEventRecord) (int64, error) {
	r.recorded = append(r.recorded, record)
	return int64(len(r.recorded)), nil
}

func (r *fakeAuditEventRepo) List(ctx context.Context, filter repo.AuditEventListFilter) ([]repo.AuditEventRecord, error) {
	r.filter = filter
	r.filters = append(r.filters, filter)
	out := make([]repo.AuditEventRecord, 0, len(r.records))
	for _, record := range r.records {
		if filter.TraderID != "" && record.TraderID != filter.TraderID {
			continue
		}
		if filter.Type != "" && record.Type != filter.Type {
			continue
		}
		if filter.CorrelationID != "" && record.CorrelationID != filter.CorrelationID {
			continue
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *fakeAuditEventRepo) ListByTrader(context.Context, string, int) ([]repo.AuditEventRecord, error) {
	return r.records, nil
}

type fakeControlCommandRepo struct {
	enqueued []repo.ControlCommandRecord
	reused   bool
	err      error
}

func (r *fakeControlCommandRepo) Enqueue(_ context.Context, record repo.ControlCommandRecord) (repo.ControlCommandRecord, bool, error) {
	if r.err != nil {
		return repo.ControlCommandRecord{}, false, r.err
	}
	r.enqueued = append(r.enqueued, record)
	record.ID = "cmd-persisted"
	record.Type = record.Target + "_" + record.Action
	record.CorrelationID = "cmd-persisted"
	record.Status = "queued"
	record.Queued = true
	record.ControlPlaneOnly = true
	record.Submitted = false
	record.CreatedAt = time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC)
	return record, r.reused, nil
}

func (r *fakeControlCommandRepo) List(context.Context, repo.ControlCommandListFilter) ([]repo.ControlCommandRecord, error) {
	return nil, r.err
}

func (r *fakeControlCommandRepo) ClaimQueued(context.Context, int) ([]repo.ControlCommandRecord, error) {
	return nil, r.err
}

func (r *fakeControlCommandRepo) Complete(context.Context, string, bool, json.RawMessage) error {
	return r.err
}

func (r *fakeControlCommandRepo) Fail(context.Context, string, string, json.RawMessage) error {
	return r.err
}

type fakeTraderRuntimeRepo struct {
	state     *repo.RuntimeStateSnapshot
	upserts   []repo.RuntimeStateRecord
	cooldowns []repo.SymbolCooldownRecord
}

func (r *fakeTraderRuntimeRepo) UpsertState(_ context.Context, record repo.RuntimeStateRecord) error {
	r.upserts = append(r.upserts, record)
	r.state = &repo.RuntimeStateSnapshot{
		RuntimeStateRecord: record,
		UpdatedAt:          time.Now().UTC(),
	}
	return nil
}

func (r *fakeTraderRuntimeRepo) UpsertCooldown(_ context.Context, record repo.SymbolCooldownRecord) error {
	r.cooldowns = append(r.cooldowns, record)
	return nil
}

func (r *fakeTraderRuntimeRepo) GetState(_ context.Context, traderID string) (*repo.RuntimeStateSnapshot, error) {
	if r.state == nil || r.state.TraderID != traderID {
		return nil, nil
	}
	return r.state, nil
}

func (r *fakeTraderRuntimeRepo) ListCooldowns(_ context.Context, traderID string) ([]repo.SymbolCooldownRecord, error) {
	if r.state == nil || r.state.TraderID != traderID {
		return nil, nil
	}
	return r.cooldowns, nil
}

func (r *fakeControlCommandRepo) Cancel(context.Context, string, string, json.RawMessage) error {
	return r.err
}

func testDecisionApprovalPayload(t *testing.T) map[string]interface{} {
	t.Helper()
	return map[string]interface{}{
		"symbol":                 "BTC",
		"action":                 "open_long",
		"leverage":               3,
		"position_size_usd":      500,
		"entry_price":            50000,
		"stop_loss":              49000,
		"take_profit":            53000,
		"confidence":             88,
		"risk_usd":               100,
		"reasoning":              "unit test decision payload",
		"invalidation_condition": "btc loses support",
	}
}
