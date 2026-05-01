package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	executorpkg "nof0-api/pkg/executor"
)

type auditTrace struct {
	CycleID       int64
	CorrelationID string
	PromptDigest  string
}

func newAuditTrace(traderID string, ts time.Time) auditTrace {
	if ts.IsZero() {
		ts = time.Now()
	}
	cycleID := ts.UTC().UnixNano()
	idPart := strings.TrimSpace(traderID)
	if idPart == "" {
		idPart = "manager"
	}
	return auditTrace{
		CycleID:       cycleID,
		CorrelationID: fmt.Sprintf("%s-%d", idPart, cycleID),
	}
}

func (m *Manager) recordAuditDecisionEvent(eventType AuditEventType, trader *VirtualTrader, decision *executorpkg.Decision, trace auditTrace, approval *ApprovalToken, reason string, eventErr error, detail map[string]any) {
	if detail == nil {
		detail = make(map[string]any)
	}
	if decision != nil {
		detail["confidence"] = decision.Confidence
		detail["leverage"] = decision.Leverage
		detail["position_size_usd"] = decision.PositionSizeUSD
		if strings.TrimSpace(decision.Reasoning) != "" {
			detail["reasoning"] = decision.Reasoning
		}
	}
	event := AuditEvent{
		Type:          eventType,
		CycleID:       trace.CycleID,
		CorrelationID: trace.CorrelationID,
		PromptDigest:  trace.PromptDigest,
		Reason:        reason,
		Detail:        mustAuditDetail(detail),
		CreatedAt:     time.Now().UTC(),
	}
	if trader != nil {
		event.TraderID = trader.ID
		event.ModelID = trader.ID
		event.ModelName = trader.ModelName
	}
	if decision != nil {
		event.Symbol = decision.Symbol
		event.Action = decision.Action
	}
	if approval != nil {
		event.ApprovalTokenID = approval.ID
	}
	if eventErr != nil {
		event.Error = eventErr.Error()
	}
	m.recordAuditEvent(event)
}

func (m *Manager) recordAuditEvent(event AuditEvent) {
	if m == nil || m.persistence == nil {
		return
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if event.CycleID <= 0 && strings.TrimSpace(event.CorrelationID) == "" {
		trace := newAuditTrace(event.TraderID, event.CreatedAt)
		event.CycleID = trace.CycleID
		event.CorrelationID = trace.CorrelationID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := m.persistence.RecordAuditEvent(ctx, event)
	logPersistenceError(err, "audit event persistence failed", map[string]any{
		"trader_id":      event.TraderID,
		"event_type":     event.Type,
		"correlation_id": event.CorrelationID,
		"symbol":         event.Symbol,
		"action":         event.Action,
	})
}

func (m *Manager) approveDecisionWithAudit(trader *VirtualTrader, decision *executorpkg.Decision, trace auditTrace) (*ApprovalToken, error) {
	approval, err := m.ApproveDecision(trader, decision)
	if err != nil {
		m.recordAuditDecisionEvent(AuditEventPolicyRejected, trader, decision, trace, nil, "policy_rejected", err, nil)
		return nil, err
	}
	m.recordAuditDecisionEvent(AuditEventApproved, trader, decision, trace, approval, "policy_approved", nil, map[string]any{
		"checks": approval.Checks,
	})
	return approval, nil
}

func (m *Manager) recordOrderSubmitted(trader *VirtualTrader, decision *executorpkg.Decision, trace auditTrace, approval *ApprovalToken, reason string, detail map[string]any) {
	m.recordAuditDecisionEvent(AuditEventOrderSubmitted, trader, decision, trace, approval, reason, nil, detail)
}

func (m *Manager) recordOrderFailed(trader *VirtualTrader, decision *executorpkg.Decision, trace auditTrace, approval *ApprovalToken, reason string, err error, detail map[string]any) {
	m.recordAuditDecisionEvent(AuditEventOrderFailed, trader, decision, trace, approval, reason, err, detail)
}

func mustAuditDetail(detail map[string]any) []byte {
	if len(detail) == 0 {
		return nil
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return []byte(`{"detail_error":"marshal_failed"}`)
	}
	return data
}
