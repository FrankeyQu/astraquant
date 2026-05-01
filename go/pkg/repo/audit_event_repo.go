package repo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nof0-api/internal/model"
)

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

var validAuditEventTypes = map[AuditEventType]struct{}{
	AuditEventDecisionGenerated:        {},
	AuditEventDecisionValidationFailed: {},
	AuditEventPolicyRejected:           {},
	AuditEventApproved:                 {},
	AuditEventOrderSubmitted:           {},
	AuditEventOrderFailed:              {},
}

// AuditEventRecord is the stable contract for recording traceable manager events.
type AuditEventRecord struct {
	ID              int64
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
	Detail          json.RawMessage
	CreatedAt       time.Time
}

// AuditEventListFilter is the stable query contract for control-plane audit reads.
type AuditEventListFilter struct {
	TraderID      string
	Type          AuditEventType
	CorrelationID string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	Limit         int
	Offset        int
}

// AuditEventRepository persists and queries immutable audit events.
type AuditEventRepository interface {
	Record(ctx context.Context, event AuditEventRecord) (int64, error)
	List(ctx context.Context, filter AuditEventListFilter) ([]AuditEventRecord, error)
	ListByTrader(ctx context.Context, traderID string, limit int) ([]AuditEventRecord, error)
}

type auditEventRepo struct {
	model model.AuditEventsModel
}

// NewAuditEventRepository wires the audit repository with its table model.
func NewAuditEventRepository(auditModel model.AuditEventsModel) AuditEventRepository {
	if auditModel == nil {
		return nil
	}
	return &auditEventRepo{model: auditModel}
}

func (r *auditEventRepo) Record(ctx context.Context, event AuditEventRecord) (int64, error) {
	if r == nil || r.model == nil {
		return 0, nil
	}
	if err := validateAuditEvent(event); err != nil {
		return 0, err
	}
	row, err := buildAuditEventRow(event)
	if err != nil {
		return 0, err
	}
	return r.model.Insert(ctx, row)
}

func (r *auditEventRepo) ListByTrader(ctx context.Context, traderID string, limit int) ([]AuditEventRecord, error) {
	if strings.TrimSpace(traderID) == "" {
		return nil, nil
	}
	return r.List(ctx, AuditEventListFilter{TraderID: traderID, Limit: limit})
}

func (r *auditEventRepo) List(ctx context.Context, filter AuditEventListFilter) ([]AuditEventRecord, error) {
	if r == nil || r.model == nil {
		return nil, nil
	}
	rows, err := r.model.List(ctx, model.AuditEventsFilter{
		TraderID:      filter.TraderID,
		EventType:     string(filter.Type),
		CorrelationID: filter.CorrelationID,
		CreatedAfter:  filter.CreatedAfter,
		CreatedBefore: filter.CreatedBefore,
		Limit:         filter.Limit,
		Offset:        filter.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AuditEventRecord, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, auditEventFromRow(row))
	}
	return out, nil
}

func validateAuditEvent(event AuditEventRecord) error {
	if _, ok := validAuditEventTypes[event.Type]; !ok {
		return fmt.Errorf("audit repo: unsupported event type %q", event.Type)
	}
	if strings.TrimSpace(event.TraderID) == "" {
		return errors.New("audit repo: trader id required")
	}
	if event.CycleID <= 0 && strings.TrimSpace(event.CorrelationID) == "" {
		return errors.New("audit repo: cycle id or correlation id required")
	}
	return nil
}

func buildAuditEventRow(event AuditEventRecord) (*model.AuditEvents, error) {
	detail, err := normalizeAuditDetail(event.Detail)
	if err != nil {
		return nil, err
	}
	row := &model.AuditEvents{
		EventType: string(event.Type),
		TraderId:  strings.TrimSpace(event.TraderID),
		Detail:    detail,
		CreatedAt: event.CreatedAt,
	}
	if event.CycleID > 0 {
		row.CycleId = sql.NullInt64{Int64: event.CycleID, Valid: true}
	}
	row.CorrelationId = nullString(event.CorrelationID)
	row.Symbol = nullString(strings.ToUpper(event.Symbol))
	row.Action = nullString(strings.ToLower(event.Action))
	row.ModelId = nullString(event.ModelID)
	row.ModelName = nullString(event.ModelName)
	row.PromptDigest = nullString(event.PromptDigest)
	row.ApprovalTokenId = nullString(event.ApprovalTokenID)
	row.Reason = nullString(event.Reason)
	row.ErrorMessage = nullString(event.Error)
	return row, nil
}

func auditEventFromRow(row *model.AuditEvents) AuditEventRecord {
	return AuditEventRecord{
		ID:              row.Id,
		Type:            AuditEventType(row.EventType),
		TraderID:        row.TraderId,
		CycleID:         row.CycleId.Int64,
		CorrelationID:   row.CorrelationId.String,
		Symbol:          row.Symbol.String,
		Action:          row.Action.String,
		ModelID:         row.ModelId.String,
		ModelName:       row.ModelName.String,
		PromptDigest:    row.PromptDigest.String,
		ApprovalTokenID: row.ApprovalTokenId.String,
		Reason:          row.Reason.String,
		Error:           row.ErrorMessage.String,
		Detail:          json.RawMessage(row.Detail),
		CreatedAt:       row.CreatedAt,
	}
}

func normalizeAuditDetail(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "{}", nil
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return "", fmt.Errorf("audit repo: invalid detail json: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func nullString(value string) sql.NullString {
	value = strings.TrimSpace(value)
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}
