package manager

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	executorpkg "nof0-api/pkg/executor"
)

const approvalTokenTTL = 30 * time.Second

const (
	allowLiveTradingEnv = "ASTRAQUANT_ALLOW_LIVE_TRADING"
	liveTradingAckEnv   = "ASTRAQUANT_LIVE_TRADING_ACK"
	liveTradingAckValue = "I_UNDERSTAND_THIS_CAN_LOSE_MONEY"
)

// ApprovalToken is the hand-off between AI intent and executable order flow.
// The manager only submits orders after a decision has passed this policy layer.
type ApprovalToken struct {
	ID          string
	TraderID    string
	Symbol      string
	Action      string
	NotionalUSD float64
	Leverage    int
	ApprovedAt  time.Time
	ExpiresAt   time.Time
	Checks      []string
}

// ApproveDecision applies hard execution policy and returns a short-lived token.
func (m *Manager) ApproveDecision(trader *VirtualTrader, decision *executorpkg.Decision) (*ApprovalToken, error) {
	if trader == nil || decision == nil {
		return nil, errors.New("manager policy: trader and decision are required")
	}
	action := strings.TrimSpace(decision.Action)
	symbol := normalizeSymbol(decision.Symbol)
	if action == "" {
		return nil, errors.New("manager policy: decision action is required")
	}
	if decision.PositionSizeUSD < 0 {
		return nil, errors.New("manager policy: position size must be non-negative")
	}

	isOpen := action == "open_long" || action == "open_short"
	isClose := action == "close_long" || action == "close_short"
	isNoop := action == "hold" || action == "wait"
	if !isOpen && !isClose && !isNoop {
		return nil, fmt.Errorf("manager policy: unsupported action %q", action)
	}
	if (isOpen || isClose) && symbol == "" {
		return nil, errors.New("manager policy: symbol is required for trade action")
	}
	if isOpen || isClose {
		if err := m.enforceExecutionMode(trader); err != nil {
			return nil, err
		}
	}

	checks := []string{"action_allowed", "shape_valid", "execution_mode"}
	lev := decision.Leverage
	if isOpen {
		if decision.PositionSizeUSD <= 0 {
			return nil, errors.New("manager policy: open action requires positive position size")
		}
		if trader.RiskParams.MinConfidence > 0 && decision.Confidence < trader.RiskParams.MinConfidence {
			return nil, fmt.Errorf("manager policy: confidence %d below minimum %d", decision.Confidence, trader.RiskParams.MinConfidence)
		}
		if trader.RiskParams.MaxPositionSizeUSD > 0 && decision.PositionSizeUSD > trader.RiskParams.MaxPositionSizeUSD+1e-6 {
			return nil, fmt.Errorf("manager policy: position size %.2f exceeds max %.2f", decision.PositionSizeUSD, trader.RiskParams.MaxPositionSizeUSD)
		}
		maxLev := trader.RiskParams.AltcoinLeverage
		if isBTCorETH(symbol) {
			maxLev = trader.RiskParams.MajorCoinLeverage
		}
		if lev <= 0 {
			lev = maxLev
			decision.Leverage = lev
		}
		if maxLev > 0 && lev > maxLev {
			return nil, fmt.Errorf("manager policy: leverage %d exceeds cap %d for %s", lev, maxLev, symbol)
		}
		if err := m.ensureSymbolAvailable(trader, symbol); err != nil {
			return nil, fmt.Errorf("manager policy: %w", err)
		}
		if err := m.enforcePositionCapacity(trader); err != nil {
			return nil, err
		}
		if err := m.enforceSecondaryRisk(trader, decision, lev); err != nil {
			return nil, fmt.Errorf("manager policy: %w", err)
		}
		checks = append(checks, "confidence_floor", "size_cap", "leverage_cap", "symbol_ownership", "position_count", "margin_cap")
	}
	if isClose {
		if err := m.ensureCloseOwnership(trader, symbol); err != nil {
			return nil, fmt.Errorf("manager policy: %w", err)
		}
		checks = append(checks, "symbol_ownership")
	}
	if isNoop {
		checks = append(checks, "noop")
	}

	now := time.Now().UTC()
	return &ApprovalToken{
		ID:          fmt.Sprintf("approval-%s-%d", trader.ID, now.UnixNano()),
		TraderID:    trader.ID,
		Symbol:      symbol,
		Action:      action,
		NotionalUSD: decision.PositionSizeUSD,
		Leverage:    lev,
		ApprovedAt:  now,
		ExpiresAt:   now.Add(approvalTokenTTL),
		Checks:      checks,
	}, nil
}

func (m *Manager) enforceExecutionMode(trader *VirtualTrader) error {
	if trader == nil {
		return errors.New("manager policy: trader is required")
	}
	mode := normalizeExecutionMode(trader.ExecutionMode, trader.Exchange)
	switch mode {
	case ExecutionModePaper:
		if !isPaperExecutionTarget(trader) {
			return fmt.Errorf("manager policy: execution_mode=paper requires a paper/sim exchange provider, got %q", trader.Exchange)
		}
	case ExecutionModeTestnet:
		if !isTestnetExecutionTarget(trader) {
			return fmt.Errorf("manager policy: execution_mode=testnet requires a testnet exchange provider, got %q", trader.Exchange)
		}
	case ExecutionModeLive:
		if os.Getenv("CI") != "" {
			return errors.New("manager policy: live trading is disabled in CI")
		}
		if !truthyEnv(os.Getenv(allowLiveTradingEnv)) {
			return fmt.Errorf("manager policy: live trading requires %s=true", allowLiveTradingEnv)
		}
		if os.Getenv(liveTradingAckEnv) != liveTradingAckValue {
			return fmt.Errorf("manager policy: live trading requires %s=%s", liveTradingAckEnv, liveTradingAckValue)
		}
	default:
		return fmt.Errorf("manager policy: unsupported execution_mode %q", mode)
	}
	return nil
}

func isPaperExecutionTarget(trader *VirtualTrader) bool {
	name := strings.ToLower(strings.TrimSpace(trader.Exchange))
	if strings.Contains(name, "paper") || strings.Contains(name, "sim") {
		return true
	}
	providerType := strings.ToLower(fmt.Sprintf("%T", trader.ExchangeProvider))
	return strings.Contains(providerType, "exchange/sim")
}

func isTestnetExecutionTarget(trader *VirtualTrader) bool {
	name := strings.ToLower(strings.TrimSpace(trader.Exchange))
	return strings.Contains(name, "testnet")
}

func truthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func (m *Manager) enforcePositionCapacity(trader *VirtualTrader) error {
	if trader == nil {
		return errors.New("manager policy: trader is required")
	}
	trader.mu.RLock()
	openCount := len(trader.VirtualPositions)
	trader.mu.RUnlock()
	if trader.RiskParams.MaxPositions > 0 && openCount >= trader.RiskParams.MaxPositions {
		return fmt.Errorf("manager policy: max positions reached (%d)", trader.RiskParams.MaxPositions)
	}
	return nil
}

func (t *ApprovalToken) Validate(trader *VirtualTrader, decision *executorpkg.Decision) error {
	if t == nil {
		return errors.New("manager policy: approval token is required")
	}
	if trader == nil || decision == nil {
		return errors.New("manager policy: trader and decision are required")
	}
	if time.Now().UTC().After(t.ExpiresAt) {
		return errors.New("manager policy: approval token expired")
	}
	if t.TraderID != trader.ID {
		return fmt.Errorf("manager policy: approval token trader mismatch %q != %q", t.TraderID, trader.ID)
	}
	if t.Action != strings.TrimSpace(decision.Action) {
		return errors.New("manager policy: approval token action mismatch")
	}
	if t.Symbol != normalizeSymbol(decision.Symbol) {
		return errors.New("manager policy: approval token symbol mismatch")
	}
	if absFloat(t.NotionalUSD-decision.PositionSizeUSD) > 1e-6 {
		return errors.New("manager policy: approval token notional mismatch")
	}
	if t.Leverage != decision.Leverage {
		return errors.New("manager policy: approval token leverage mismatch")
	}
	return nil
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
