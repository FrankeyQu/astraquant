package manager

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"nof0-api/pkg/exchange"
	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/journal"
)

// PositionEventType distinguishes between open/close lifecycle hooks.
type PositionEventType string

const (
	// PositionEventOpen marks an entry event (new or increased exposure).
	PositionEventOpen PositionEventType = "open"
	// PositionEventClose marks an exit event (full close or reduce-only).
	PositionEventClose PositionEventType = "close"
)

// PositionEvent captures the minimal data needed for persistence/caching layers.
type PositionEvent struct {
	TraderID string
	Trader   *VirtualTrader
	Decision executorpkg.Decision
	Event    PositionEventType

	ExchangeResponse *exchange.OrderResponse
	OccurredAt       time.Time
	FillPrice        float64
	FillSize         float64
}

// DecisionCycleRecord is emitted after each decision loop for DB/cache mirroring.
type DecisionCycleRecord struct {
	TraderID      string
	ConfigVersion int64
	Cycle         *journal.CycleRecord
}

// AccountSyncSnapshot represents a normalized account/equity update.
type AccountSyncSnapshot struct {
	TraderID            string
	EquityUSD           float64
	MarginUsedUSD       float64
	AvailableBalanceUSD float64
	UnrealizedPnLUSD    float64
	SyncedAt            time.Time
}

// AnalyticsSnapshot captures performance metrics for persistence/leaderboard.
type AnalyticsSnapshot struct {
	TraderID       string
	TotalPnLUSD    float64
	TotalPnLPct    float64
	SharpeRatio    float64
	WinRate        float64
	TotalTrades    int
	MaxDrawdownPct float64
	UpdatedAt      time.Time
}

// AuditEventType names immutable events in the AI decision to order lifecycle.
type AuditEventType string

const (
	AuditEventDecisionGenerated        AuditEventType = "decision_generated"
	AuditEventDecisionValidationFailed AuditEventType = "decision_validation_failed"
	AuditEventPolicyRejected           AuditEventType = "policy_rejected"
	AuditEventApproved                 AuditEventType = "approved"
	AuditEventOrderSubmitted           AuditEventType = "order_submitted"
	AuditEventOrderFailed              AuditEventType = "order_failed"
)

// AuditEvent captures the minimal trace contract for manager and policy events.
type AuditEvent struct {
	Type            AuditEventType
	TraderID        string
	CycleID         int64
	CorrelationID   string
	Symbol          string
	Action          string
	ModelID         string
	ModelName       string
	PromptDigest    string
	ApprovalTokenID string
	Reason          string
	Error           string
	Detail          []byte
	CreatedAt       time.Time
}

// PersistenceService describes the hooks manager emits to capture state changes.
type PersistenceService interface {
	RecordAuditEvent(ctx context.Context, event AuditEvent) error
	RecordPositionEvent(ctx context.Context, event PositionEvent) error
	RecordDecisionCycle(ctx context.Context, record DecisionCycleRecord) error
	RecordAccountSnapshot(ctx context.Context, snapshot AccountSyncSnapshot) error
	RecordAnalytics(ctx context.Context, snapshot AnalyticsSnapshot) error
	HydrateCaches(ctx context.Context, traderIDs []string) error
}

type noopPersistenceService struct{}

func (noopPersistenceService) RecordAuditEvent(ctx context.Context, event AuditEvent) error {
	return nil
}

func (noopPersistenceService) RecordPositionEvent(ctx context.Context, event PositionEvent) error {
	return nil
}

func (noopPersistenceService) RecordDecisionCycle(ctx context.Context, record DecisionCycleRecord) error {
	return nil
}

func (noopPersistenceService) RecordAccountSnapshot(ctx context.Context, snapshot AccountSyncSnapshot) error {
	return nil
}

func (noopPersistenceService) RecordAnalytics(ctx context.Context, snapshot AnalyticsSnapshot) error {
	return nil
}

func (noopPersistenceService) HydrateCaches(ctx context.Context, traderIDs []string) error {
	return nil
}

// newNoopPersistenceService guarantees manager always has a persistence hook to call.
func newNoopPersistenceService() PersistenceService {
	return noopPersistenceService{}
}

func logPersistenceError(err error, msg string, fields map[string]any) {
	if err == nil {
		return
	}
	logx.WithContext(context.Background()).Errorf("manager: %s: %v fields=%v", msg, err, fields)
}
