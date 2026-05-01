package manager

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"nof0-api/pkg/repo"
)

// ControlAction names the safe control-plane lifecycle commands.
type ControlAction string

const (
	ControlActionStart  ControlAction = "start"
	ControlActionStop   ControlAction = "stop"
	ControlActionPause  ControlAction = "pause"
	ControlActionResume ControlAction = "resume"
)

// ControlRequest is the normalized command accepted by the safe control plane.
type ControlRequest struct {
	TraderID       string
	Action         ControlAction
	RequestedBy    string
	Reason         string
	IdempotencyKey string
	CorrelationID  string
	EffectiveUntil string
}

// ControlResult describes the recorded command outcome. Accepted=true means
// only that the control-plane state was recorded; it never starts a trade loop.
type ControlResult struct {
	Accepted         bool
	Status           string
	TraderID         string
	Action           ControlAction
	CorrelationID    string
	Message          string
	State            TraderState
	ExecutionMode    ExecutionMode
	Queued           bool
	ControlPlaneOnly bool
	UpdatedAt        time.Time
}

// ControlStateSnapshot is the in-memory runtime view owned by ControlPlane.
type ControlStateSnapshot struct {
	TraderID            string
	State               TraderState
	ActiveConfigVersion int64
	PauseUntil          *time.Time
	PauseReason         string
	RequestedBy         string
	Reason              string
	LastAction          ControlAction
	CorrelationID       string
	UpdatedAt           time.Time
}

// ControlPlane records safe lifecycle intent for configured traders. It is
// deliberately separate from Manager so API control endpoints cannot bypass
// manager policy or live order gates.
type ControlPlane struct {
	mu          sync.RWMutex
	config      *Config
	runtimeRepo repo.TraderRuntimeRepository
	states      map[string]ControlStateSnapshot
}

// NewControlPlane creates a memory-backed safe control plane.
func NewControlPlane(cfg *Config, runtimeRepo repo.TraderRuntimeRepository) *ControlPlane {
	return &ControlPlane{
		config:      cfg,
		runtimeRepo: runtimeRepo,
		states:      make(map[string]ControlStateSnapshot),
	}
}

// SetRuntimeRepo wires persistence after ServiceContext initializes models.
func (c *ControlPlane) SetRuntimeRepo(runtimeRepo repo.TraderRuntimeRepository) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.runtimeRepo = runtimeRepo
}

// Snapshot returns a memory-backed control state when one has been recorded.
func (c *ControlPlane) Snapshot(traderID string) (ControlStateSnapshot, bool) {
	if c == nil {
		return ControlStateSnapshot{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap, ok := c.states[strings.TrimSpace(traderID)]
	return snap, ok
}

// Handle validates and records a safe lifecycle command.
func (c *ControlPlane) Handle(ctx context.Context, req ControlRequest) (ControlResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	traderID := strings.TrimSpace(req.TraderID)
	action := ControlAction(strings.ToLower(strings.TrimSpace(string(req.Action))))
	correlationID := strings.TrimSpace(req.CorrelationID)
	if correlationID == "" {
		correlationID = newControlCorrelationID(traderID, action)
	}
	req.CorrelationID = correlationID
	base := ControlResult{
		Accepted:         false,
		Status:           "rejected",
		TraderID:         traderID,
		Action:           action,
		CorrelationID:    correlationID,
		Queued:           false,
		ControlPlaneOnly: true,
	}
	if c == nil || c.config == nil {
		base.Message = "manager control plane is not configured"
		return base, nil
	}
	trader, ok := c.findTrader(traderID)
	if !ok {
		base.Message = fmt.Sprintf("trader %q not found", traderID)
		return base, nil
	}
	base.ExecutionMode = normalizeExecutionMode(trader.ExecutionMode, trader.ExchangeProvider)
	if !isControlAction(action) {
		base.Message = fmt.Sprintf("unsupported control action %q", action)
		return base, nil
	}
	if action == ControlActionStart || action == ControlActionResume {
		if err := enforceControlExecutionMode(base.ExecutionMode); err != nil {
			base.Message = err.Error()
			return base, nil
		}
	}

	now := time.Now().UTC()
	next, err := c.nextState(ctx, trader, req, action, now)
	if err != nil {
		base.Message = err.Error()
		return base, nil
	}
	if err := c.persistState(ctx, next); err != nil {
		return base, err
	}
	c.mu.Lock()
	c.states[trader.ID] = next
	c.mu.Unlock()

	base.Accepted = true
	base.Status = "accepted"
	base.State = next.State
	base.UpdatedAt = next.UpdatedAt
	base.Message = "control state recorded; no trading loop was started and no orders were submitted"
	return base, nil
}

func (c *ControlPlane) findTrader(traderID string) (TraderConfig, bool) {
	if c == nil || c.config == nil || traderID == "" {
		return TraderConfig{}, false
	}
	for _, trader := range c.config.Traders {
		if trader.ID == traderID {
			return trader, true
		}
	}
	return TraderConfig{}, false
}

func (c *ControlPlane) nextState(ctx context.Context, trader TraderConfig, req ControlRequest, action ControlAction, now time.Time) (ControlStateSnapshot, error) {
	current, ok := c.currentState(ctx, trader)
	if !ok {
		current = ControlStateSnapshot{
			TraderID:            trader.ID,
			State:               TraderStateStopped,
			ActiveConfigVersion: trader.Version,
		}
	}
	next := current
	next.TraderID = trader.ID
	next.ActiveConfigVersion = trader.Version
	next.UpdatedAt = now
	next.RequestedBy = strings.TrimSpace(req.RequestedBy)
	next.Reason = strings.TrimSpace(req.Reason)
	next.LastAction = action
	next.CorrelationID = strings.TrimSpace(req.CorrelationID)

	switch action {
	case ControlActionStart:
		next.State = TraderStateRunning
		next.PauseUntil = nil
		next.PauseReason = ""
	case ControlActionStop:
		next.State = TraderStateStopped
		next.PauseUntil = nil
		next.PauseReason = ""
	case ControlActionPause:
		if current.State == TraderStateStopped {
			return next, fmt.Errorf("cannot pause stopped trader %q", trader.ID)
		}
		until, err := parseOptionalControlTime(req.EffectiveUntil)
		if err != nil {
			return next, err
		}
		next.State = TraderStatePaused
		next.PauseUntil = until
		next.PauseReason = strings.TrimSpace(req.Reason)
	case ControlActionResume:
		if current.State == TraderStateStopped {
			return next, fmt.Errorf("cannot resume stopped trader %q; start it first", trader.ID)
		}
		next.State = TraderStateRunning
		next.PauseUntil = nil
		next.PauseReason = ""
	default:
		return next, fmt.Errorf("unsupported control action %q", action)
	}
	return next, nil
}

func (c *ControlPlane) currentState(ctx context.Context, trader TraderConfig) (ControlStateSnapshot, bool) {
	c.mu.RLock()
	snap, ok := c.states[trader.ID]
	c.mu.RUnlock()
	if ok {
		return snap, true
	}
	c.mu.RLock()
	runtimeRepo := c.runtimeRepo
	c.mu.RUnlock()
	if runtimeRepo == nil {
		return ControlStateSnapshot{}, false
	}
	record, err := runtimeRepo.GetState(ctx, trader.ID)
	if err != nil || record == nil {
		return ControlStateSnapshot{}, false
	}
	state := TraderStateStopped
	if record.IsRunning {
		state = TraderStateRunning
	}
	if record.Detail.Pause != nil {
		if record.Detail.Pause.Until == nil || record.Detail.Pause.Until.After(time.Now().UTC()) {
			state = TraderStatePaused
		}
	}
	snap = ControlStateSnapshot{
		TraderID:            trader.ID,
		State:               state,
		ActiveConfigVersion: record.ActiveConfigVersion,
		UpdatedAt:           record.UpdatedAt,
	}
	if record.Detail.Pause != nil {
		snap.PauseUntil = record.Detail.Pause.Until
		snap.PauseReason = record.Detail.Pause.Reason
	}
	return snap, true
}

func (c *ControlPlane) persistState(ctx context.Context, snap ControlStateSnapshot) error {
	c.mu.RLock()
	runtimeRepo := c.runtimeRepo
	c.mu.RUnlock()
	if runtimeRepo == nil {
		return nil
	}
	detail := repo.RuntimeStateDetail{}
	if snap.State == TraderStatePaused || snap.PauseUntil != nil || strings.TrimSpace(snap.PauseReason) != "" {
		detail.Pause = &repo.RuntimePauseDetail{
			Until:  snap.PauseUntil,
			Reason: snap.PauseReason,
		}
	}
	return runtimeRepo.UpsertState(ctx, repo.RuntimeStateRecord{
		TraderID:            snap.TraderID,
		ActiveConfigVersion: snap.ActiveConfigVersion,
		IsRunning:           snap.State == TraderStateRunning,
		Detail:              detail,
	})
}

func isControlAction(action ControlAction) bool {
	switch action {
	case ControlActionStart, ControlActionStop, ControlActionPause, ControlActionResume:
		return true
	default:
		return false
	}
}

func enforceControlExecutionMode(mode ExecutionMode) error {
	switch mode {
	case ExecutionModePaper, ExecutionModeTestnet:
		return nil
	case ExecutionModeLive:
		if os.Getenv("CI") != "" {
			return fmt.Errorf("live trader control is disabled in CI")
		}
		if !truthyEnv(os.Getenv(allowLiveTradingEnv)) {
			return fmt.Errorf("live trader control requires %s=true", allowLiveTradingEnv)
		}
		if os.Getenv(liveTradingAckEnv) != liveTradingAckValue {
			return fmt.Errorf("live trader control requires %s=%s", liveTradingAckEnv, liveTradingAckValue)
		}
		return nil
	default:
		return fmt.Errorf("unsupported execution_mode %q", mode)
	}
}

func parseOptionalControlTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("effective_until must be RFC3339: %w", err)
	}
	utc := parsed.UTC()
	return &utc, nil
}

func newControlCorrelationID(traderID string, action ControlAction) string {
	traderID = strings.TrimSpace(traderID)
	if traderID == "" {
		traderID = "unknown"
	}
	if action == "" {
		action = "control"
	}
	return fmt.Sprintf("ctrl-%s-%s-%d", traderID, action, time.Now().UTC().UnixNano())
}
