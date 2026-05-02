package manager

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/repo"
)

func TestControlCommandWorkerExecutesDecisionThroughPolicyGateway(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	audit := &auditRecordingPersistence{}
	manager := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    audit,
	}
	commands := &fakeWorkerCommandRepo{
		claimed: []repo.ControlCommandRecord{workerDecisionCommand(t, trader.ID, executorpkg.Decision{
			Symbol:          "BTC",
			Action:          "open_long",
			PositionSizeUSD: 500,
			EntryPrice:      50_000,
			Leverage:        2,
			Confidence:      90,
		})},
	}

	result, err := NewControlCommandWorker(manager, commands).ProcessOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.Claimed)
	require.Equal(t, 1, result.Completed)
	require.Equal(t, 1, ex.placeOrders)
	require.Equal(t, "cmd-worker-1", commands.completedID)
	require.True(t, commands.completedSubmitted)
	requireAuditEvent(t, audit.events, AuditEventApproved)
	requireAuditEvent(t, audit.events, AuditEventOrderSubmitted)
}

func TestControlCommandWorkerFailsOversizeBeforeExchange(t *testing.T) {
	ex := &countingExchangeProvider{}
	trader := testPolicyTrader(ex, &staticMarketProvider{})
	trader.RiskParams.MaxPositionSizeUSD = 100
	audit := &auditRecordingPersistence{}
	manager := &Manager{
		traders:        map[string]*VirtualTrader{trader.ID: trader},
		positionOwners: make(map[string]string),
		persistence:    audit,
	}
	commands := &fakeWorkerCommandRepo{
		claimed: []repo.ControlCommandRecord{workerDecisionCommand(t, trader.ID, executorpkg.Decision{
			Symbol:          "BTC",
			Action:          "open_long",
			PositionSizeUSD: 500,
			EntryPrice:      50_000,
			Leverage:        2,
			Confidence:      90,
		})},
	}

	result, err := NewControlCommandWorker(manager, commands).ProcessOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Zero(t, ex.placeOrders)
	require.Equal(t, "cmd-worker-1", commands.failedID)
	require.Contains(t, commands.failedReason, "manager policy")
	requireAuditEvent(t, audit.events, AuditEventPolicyRejected)
}

func TestControlCommandWorkerFailsMissingDecisionPayload(t *testing.T) {
	manager := &Manager{
		traders:        map[string]*VirtualTrader{},
		positionOwners: make(map[string]string),
	}
	commands := &fakeWorkerCommandRepo{
		claimed: []repo.ControlCommandRecord{
			{
				ID:            "cmd-missing",
				Type:          "decision_approve",
				Target:        repo.ControlCommandTargetDecision,
				DecisionID:    "decision-missing",
				TraderID:      "paper",
				Action:        "approve",
				CorrelationID: "corr-missing",
				Status:        repo.ControlCommandStatusProcessing,
				Detail:        json.RawMessage(`{"decision_id":"decision-missing"}`),
			},
		},
	}

	result, err := NewControlCommandWorker(manager, commands).ProcessOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, "cmd-missing", commands.failedID)
	require.Contains(t, commands.failedReason, "missing decision payload")
}

func TestControlCommandWorkerCancelsRejectCommand(t *testing.T) {
	manager := &Manager{
		traders:        map[string]*VirtualTrader{},
		positionOwners: make(map[string]string),
	}
	commands := &fakeWorkerCommandRepo{
		claimed: []repo.ControlCommandRecord{
			{
				ID:            "cmd-reject",
				Type:          "decision_reject",
				Target:        repo.ControlCommandTargetDecision,
				DecisionID:    "decision-reject",
				Action:        "reject",
				CorrelationID: "corr-reject",
				Status:        repo.ControlCommandStatusProcessing,
				Detail:        json.RawMessage(`{"decision_id":"decision-reject"}`),
			},
		},
	}

	result, err := NewControlCommandWorker(manager, commands).ProcessOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, 1, result.Cancelled)
	require.Equal(t, "cmd-reject", commands.cancelledID)
	require.Contains(t, commands.cancelledReason, "operator rejected")
}

func workerDecisionCommand(t *testing.T, traderID string, decision executorpkg.Decision) repo.ControlCommandRecord {
	t.Helper()
	detail, err := json.Marshal(map[string]any{
		"trader_id": traderID,
		"decision":  decision,
	})
	require.NoError(t, err)
	return repo.ControlCommandRecord{
		ID:               "cmd-worker-1",
		Type:             "decision_approve",
		Target:           repo.ControlCommandTargetDecision,
		DecisionID:       "decision-worker-1",
		TraderID:         traderID,
		Action:           "approve",
		CorrelationID:    "corr-worker-1",
		Status:           repo.ControlCommandStatusProcessing,
		Queued:           true,
		ControlPlaneOnly: true,
		Submitted:        false,
		Detail:           detail,
		CreatedAt:        time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC),
	}
}

type fakeWorkerCommandRepo struct {
	claimed []repo.ControlCommandRecord
	err     error

	completedID        string
	completedSubmitted bool
	completedDetail    json.RawMessage

	failedID     string
	failedReason string
	failedDetail json.RawMessage

	cancelledID     string
	cancelledReason string
	cancelledDetail json.RawMessage
}

func (r *fakeWorkerCommandRepo) Enqueue(context.Context, repo.ControlCommandRecord) (repo.ControlCommandRecord, bool, error) {
	return repo.ControlCommandRecord{}, false, r.err
}

func (r *fakeWorkerCommandRepo) List(context.Context, repo.ControlCommandListFilter) ([]repo.ControlCommandRecord, error) {
	return nil, r.err
}

func (r *fakeWorkerCommandRepo) ClaimQueued(context.Context, int) ([]repo.ControlCommandRecord, error) {
	return r.claimed, r.err
}

func (r *fakeWorkerCommandRepo) Complete(_ context.Context, id string, submitted bool, detail json.RawMessage) error {
	if r.err != nil {
		return r.err
	}
	r.completedID = id
	r.completedSubmitted = submitted
	r.completedDetail = detail
	return nil
}

func (r *fakeWorkerCommandRepo) Fail(_ context.Context, id, reason string, detail json.RawMessage) error {
	if r.err != nil {
		return r.err
	}
	r.failedID = id
	r.failedReason = reason
	r.failedDetail = detail
	return nil
}

func (r *fakeWorkerCommandRepo) Cancel(_ context.Context, id, reason string, detail json.RawMessage) error {
	if r.err != nil {
		return r.err
	}
	r.cancelledID = id
	r.cancelledReason = reason
	r.cancelledDetail = detail
	return nil
}
