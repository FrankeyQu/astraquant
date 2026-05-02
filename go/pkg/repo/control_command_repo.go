package repo

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"nof0-api/internal/model"
)

const (
	ControlCommandStatusQueued = "queued"

	ControlCommandTargetDecision = "decision"
	ControlCommandTargetOrder    = "order"
)

type ControlCommandRecord struct {
	ID               string
	Type             string
	Target           string
	DecisionID       string
	OrderID          string
	TraderID         string
	Action           string
	RequestedBy      string
	Reason           string
	IdempotencyKey   string
	CorrelationID    string
	Status           string
	Queued           bool
	ControlPlaneOnly bool
	Submitted        bool
	Detail           json.RawMessage
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ControlCommandListFilter struct {
	Target   string
	Status   string
	TraderID string
	Limit    int
	Offset   int
}

type ControlCommandRepository interface {
	Enqueue(ctx context.Context, record ControlCommandRecord) (ControlCommandRecord, bool, error)
	List(ctx context.Context, filter ControlCommandListFilter) ([]ControlCommandRecord, error)
}

type controlCommandRepo struct {
	model model.ControlCommandsModel
}

func NewControlCommandRepository(commandModel model.ControlCommandsModel) ControlCommandRepository {
	if commandModel == nil {
		return nil
	}
	return &controlCommandRepo{model: commandModel}
}

func (r *controlCommandRepo) Enqueue(ctx context.Context, record ControlCommandRecord) (ControlCommandRecord, bool, error) {
	if r == nil || r.model == nil {
		return record, false, nil
	}
	normalized, err := normalizeControlCommand(record)
	if err != nil {
		return ControlCommandRecord{}, false, err
	}
	if normalized.IdempotencyKey != "" {
		existing, err := r.findExisting(ctx, normalized)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, model.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
			return ControlCommandRecord{}, false, err
		}
	}
	if err := r.model.Insert(ctx, controlCommandRow(normalized)); err != nil {
		if normalized.IdempotencyKey != "" {
			if existing, findErr := r.findExisting(ctx, normalized); findErr == nil {
				return existing, true, nil
			}
		}
		return ControlCommandRecord{}, false, err
	}
	return normalized, false, nil
}

func (r *controlCommandRepo) List(ctx context.Context, filter ControlCommandListFilter) ([]ControlCommandRecord, error) {
	if r == nil || r.model == nil {
		return nil, nil
	}
	rows, err := r.model.List(ctx, model.ControlCommandsFilter{
		Target:   filter.Target,
		Status:   filter.Status,
		TraderID: filter.TraderID,
		Limit:    filter.Limit,
		Offset:   filter.Offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]ControlCommandRecord, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		out = append(out, controlCommandFromRow(row))
	}
	return out, nil
}

func (r *controlCommandRepo) findExisting(ctx context.Context, record ControlCommandRecord) (ControlCommandRecord, error) {
	targetID := record.DecisionID
	if record.Target == ControlCommandTargetOrder {
		targetID = record.OrderID
	}
	row, err := r.model.FindByIdempotency(ctx, record.Target, targetID, record.Action, record.IdempotencyKey)
	if err != nil {
		return ControlCommandRecord{}, err
	}
	return controlCommandFromRow(row), nil
}

func normalizeControlCommand(record ControlCommandRecord) (ControlCommandRecord, error) {
	record.Target = strings.ToLower(strings.TrimSpace(record.Target))
	record.DecisionID = strings.TrimSpace(record.DecisionID)
	record.OrderID = strings.TrimSpace(record.OrderID)
	record.TraderID = strings.TrimSpace(record.TraderID)
	record.Action = strings.ToLower(strings.TrimSpace(record.Action))
	record.RequestedBy = strings.TrimSpace(record.RequestedBy)
	record.Reason = strings.TrimSpace(record.Reason)
	record.IdempotencyKey = strings.TrimSpace(record.IdempotencyKey)
	record.CorrelationID = strings.TrimSpace(record.CorrelationID)
	record.Status = strings.ToLower(strings.TrimSpace(record.Status))
	if record.Status == "" {
		record.Status = ControlCommandStatusQueued
	}
	record.Type = strings.TrimSpace(record.Type)
	if record.Type == "" && record.Target != "" && record.Action != "" {
		record.Type = record.Target + "_" + record.Action
	}
	switch record.Target {
	case ControlCommandTargetDecision:
		if record.DecisionID == "" {
			return ControlCommandRecord{}, fmt.Errorf("control command repo: decision id required")
		}
	case ControlCommandTargetOrder:
		if record.OrderID == "" {
			return ControlCommandRecord{}, fmt.Errorf("control command repo: order id required")
		}
	default:
		return ControlCommandRecord{}, fmt.Errorf("control command repo: unsupported target %q", record.Target)
	}
	if record.Action != "approve" && record.Action != "reject" {
		return ControlCommandRecord{}, fmt.Errorf("control command repo: unsupported action %q", record.Action)
	}
	if record.RequestedBy == "" {
		return ControlCommandRecord{}, fmt.Errorf("control command repo: requested_by required")
	}
	if record.Reason == "" {
		return ControlCommandRecord{}, fmt.Errorf("control command repo: reason required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if record.ID == "" {
		record.ID = newControlCommandID(record)
	}
	if record.CorrelationID == "" {
		record.CorrelationID = record.ID
	}
	record.Queued = true
	record.ControlPlaneOnly = true
	record.Submitted = false
	detail, err := normalizeControlCommandDetail(record.Detail)
	if err != nil {
		return ControlCommandRecord{}, err
	}
	record.Detail = detail
	return record, nil
}

func newControlCommandID(record ControlCommandRecord) string {
	var nonce [8]byte
	_, _ = crand.Read(nonce[:])
	seed := fmt.Sprintf("%s|%s|%s|%s|%d|%x", record.Target, controlCommandTargetID(record), record.Action, record.IdempotencyKey, record.CreatedAt.UnixNano(), nonce)
	sum := sha256.Sum256([]byte(seed))
	return "cmd-" + hex.EncodeToString(sum[:])[:20]
}

func controlCommandTargetID(record ControlCommandRecord) string {
	if record.Target == ControlCommandTargetOrder {
		return record.OrderID
	}
	return record.DecisionID
}

func normalizeControlCommandDetail(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var payload any
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, fmt.Errorf("control command repo: invalid detail json: %w", err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func controlCommandRow(record ControlCommandRecord) *model.ControlCommands {
	return &model.ControlCommands{
		Id:               record.ID,
		CommandType:      record.Type,
		Target:           record.Target,
		DecisionId:       nullString(record.DecisionID),
		OrderId:          nullString(record.OrderID),
		TraderId:         nullString(record.TraderID),
		Action:           record.Action,
		RequestedBy:      record.RequestedBy,
		Reason:           record.Reason,
		IdempotencyKey:   nullString(record.IdempotencyKey),
		CorrelationId:    record.CorrelationID,
		Status:           record.Status,
		Queued:           record.Queued,
		ControlPlaneOnly: record.ControlPlaneOnly,
		Submitted:        record.Submitted,
		Detail:           string(record.Detail),
		CreatedAt:        record.CreatedAt,
		UpdatedAt:        record.UpdatedAt,
	}
}

func controlCommandFromRow(row *model.ControlCommands) ControlCommandRecord {
	if row == nil {
		return ControlCommandRecord{}
	}
	return ControlCommandRecord{
		ID:               row.Id,
		Type:             row.CommandType,
		Target:           row.Target,
		DecisionID:       row.DecisionId.String,
		OrderID:          row.OrderId.String,
		TraderID:         row.TraderId.String,
		Action:           row.Action,
		RequestedBy:      row.RequestedBy,
		Reason:           row.Reason,
		IdempotencyKey:   row.IdempotencyKey.String,
		CorrelationID:    row.CorrelationId,
		Status:           row.Status,
		Queued:           row.Queued,
		ControlPlaneOnly: row.ControlPlaneOnly,
		Submitted:        row.Submitted,
		Detail:           json.RawMessage(row.Detail),
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}
