package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/repo"
)

const defaultControlCommandWorkerBatchSize = 10

type ControlCommandWorker struct {
	manager   *Manager
	commands  repo.ControlCommandRepository
	batchSize int
}

type ControlCommandWorkerResult struct {
	Claimed   int
	Completed int
	Failed    int
	Cancelled int
}

func NewControlCommandWorker(manager *Manager, commands repo.ControlCommandRepository) *ControlCommandWorker {
	return &ControlCommandWorker{
		manager:   manager,
		commands:  commands,
		batchSize: defaultControlCommandWorkerBatchSize,
	}
}

func (w *ControlCommandWorker) WithBatchSize(batchSize int) *ControlCommandWorker {
	if w == nil {
		return nil
	}
	if batchSize > 0 {
		w.batchSize = batchSize
	}
	return w
}

func (w *ControlCommandWorker) ProcessOnce(ctx context.Context) (ControlCommandWorkerResult, error) {
	var result ControlCommandWorkerResult
	if w == nil || w.commands == nil {
		return result, errors.New("control command worker: command repository is required")
	}
	if w.manager == nil {
		return result, errors.New("control command worker: manager is required")
	}
	batchSize := w.batchSize
	if batchSize <= 0 {
		batchSize = defaultControlCommandWorkerBatchSize
	}
	commands, err := w.commands.ClaimQueued(ctx, batchSize)
	if err != nil {
		return result, err
	}
	result.Claimed = len(commands)
	for _, command := range commands {
		switch outcome := w.processCommand(ctx, command); outcome {
		case repo.ControlCommandStatusCompleted:
			result.Completed++
		case repo.ControlCommandStatusCancelled:
			result.Cancelled++
		default:
			result.Failed++
		}
	}
	return result, nil
}

func (w *ControlCommandWorker) processCommand(ctx context.Context, command repo.ControlCommandRecord) string {
	if command.Action == "reject" {
		_ = w.commands.Cancel(ctx, command.ID, "operator rejected command", commandWorkerDetail(command, nil, nil))
		return repo.ControlCommandStatusCancelled
	}
	if command.Target != repo.ControlCommandTargetDecision || command.Action != "approve" {
		err := fmt.Errorf("unsupported command target/action %s/%s", command.Target, command.Action)
		_ = w.commands.Fail(ctx, command.ID, err.Error(), commandWorkerDetail(command, nil, err))
		return repo.ControlCommandStatusFailed
	}

	traderID, decision, err := decisionFromCommand(command)
	if err != nil {
		_ = w.commands.Fail(ctx, command.ID, err.Error(), commandWorkerDetail(command, nil, err))
		return repo.ControlCommandStatusFailed
	}
	if err := w.manager.ExecuteDecisionForTraderID(traderID, &decision); err != nil {
		_ = w.commands.Fail(ctx, command.ID, err.Error(), commandWorkerDetail(command, &decision, err))
		return repo.ControlCommandStatusFailed
	}
	_ = w.commands.Complete(ctx, command.ID, isTradeAction(decision.Action), commandWorkerDetail(command, &decision, nil))
	return repo.ControlCommandStatusCompleted
}

func (m *Manager) ExecuteDecisionForTraderID(traderID string, decision *executorpkg.Decision) error {
	if m == nil {
		return errors.New("manager: nil manager")
	}
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		return errors.New("manager: trader id is required")
	}
	m.mu.RLock()
	trader := m.traders[traderID]
	m.mu.RUnlock()
	if trader == nil {
		return fmt.Errorf("manager: trader %s not found", traderID)
	}
	return m.ExecuteDecision(trader, decision)
}

type commandDecisionPayload struct {
	TraderID  string                 `json:"trader_id,omitempty"`
	Decision  *executorpkg.Decision  `json:"decision,omitempty"`
	Decisions []executorpkg.Decision `json:"decisions,omitempty"`
}

func decisionFromCommand(command repo.ControlCommandRecord) (string, executorpkg.Decision, error) {
	traderID := strings.TrimSpace(command.TraderID)
	var payload commandDecisionPayload
	if len(command.Detail) > 0 {
		if err := json.Unmarshal(command.Detail, &payload); err != nil {
			return "", executorpkg.Decision{}, fmt.Errorf("invalid command detail: %w", err)
		}
	}
	if traderID == "" {
		traderID = strings.TrimSpace(payload.TraderID)
	}
	if traderID == "" {
		return "", executorpkg.Decision{}, errors.New("decision command missing trader_id")
	}
	if payload.Decision != nil {
		return traderID, *payload.Decision, validateWorkerDecision(*payload.Decision)
	}
	if len(payload.Decisions) > 0 {
		return traderID, payload.Decisions[0], validateWorkerDecision(payload.Decisions[0])
	}
	var direct executorpkg.Decision
	if len(command.Detail) > 0 {
		if err := json.Unmarshal(command.Detail, &direct); err == nil && strings.TrimSpace(direct.Action) != "" {
			return traderID, direct, validateWorkerDecision(direct)
		}
	}
	return "", executorpkg.Decision{}, errors.New("decision command missing decision payload")
}

func validateWorkerDecision(decision executorpkg.Decision) error {
	if strings.TrimSpace(decision.Action) == "" {
		return errors.New("decision action is required")
	}
	return nil
}

func isTradeAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "open_long", "open_short", "close_long", "close_short":
		return true
	default:
		return false
	}
}

func commandWorkerDetail(command repo.ControlCommandRecord, decision *executorpkg.Decision, commandErr error) json.RawMessage {
	detail := map[string]any{
		"command_id":     command.ID,
		"command_type":   command.Type,
		"target":         command.Target,
		"action":         command.Action,
		"correlation_id": command.CorrelationID,
	}
	if command.DecisionID != "" {
		detail["decision_id"] = command.DecisionID
	}
	if command.OrderID != "" {
		detail["order_id"] = command.OrderID
	}
	if command.TraderID != "" {
		detail["trader_id"] = command.TraderID
	}
	if decision != nil {
		detail["decision"] = decision
		detail["submitted"] = isTradeAction(decision.Action)
	} else {
		detail["submitted"] = false
	}
	if commandErr != nil {
		detail["error"] = commandErr.Error()
	}
	data, err := json.Marshal(detail)
	if err != nil {
		return json.RawMessage(`{"detail_error":"marshal_failed"}`)
	}
	return json.RawMessage(data)
}
