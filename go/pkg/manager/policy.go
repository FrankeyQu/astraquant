package manager

import (
	"context"
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

// DailyLossLimitError is returned when the UTC daily equity drawdown exceeds
// the configured hard limit for new opens.
type DailyLossLimitError struct {
	Date             string
	StartEquityUSD   float64
	CurrentEquityUSD float64
	LossUSD          float64
	LimitUSD         float64
}

func (e *DailyLossLimitError) Error() string {
	if e == nil {
		return "daily loss limit exceeded"
	}
	return fmt.Sprintf("daily loss %.2f exceeds limit %.2f", e.LossUSD, e.LimitUSD)
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
		if err := enforceOpenDecisionPriceGuards(trader.RiskParams, decision); err != nil {
			return nil, fmt.Errorf("manager policy: %w", err)
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
		if err := m.enforceOpenRisk(trader, decision, lev); err != nil {
			return nil, fmt.Errorf("manager policy: %w", err)
		}
		checks = append(checks, "entry_price", "stop_loss", "take_profit", "risk_reward", "confidence_floor", "symbol_whitelist", "size_cap", "leverage_cap", "daily_loss_limit", "symbol_ownership", "position_count", "margin_cap")
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

func enforceOpenDecisionPriceGuards(params RiskParameters, decision *executorpkg.Decision) error {
	if decision == nil {
		return errors.New("decision is required")
	}
	action := strings.TrimSpace(decision.Action)
	if decision.EntryPrice <= 0 {
		return errors.New("open action requires positive entry_price")
	}
	if params.StopLossEnabled && decision.StopLoss <= 0 {
		return errors.New("open action requires positive stop_loss")
	}
	if params.TakeProfitEnabled && decision.TakeProfit <= 0 {
		return errors.New("open action requires positive take_profit")
	}
	if decision.StopLoss > 0 {
		switch action {
		case "open_long":
			if decision.StopLoss >= decision.EntryPrice {
				return errors.New("open_long requires stop_loss below entry_price")
			}
		case "open_short":
			if decision.StopLoss <= decision.EntryPrice {
				return errors.New("open_short requires stop_loss above entry_price")
			}
		}
	}
	if decision.TakeProfit > 0 {
		switch action {
		case "open_long":
			if decision.TakeProfit <= decision.EntryPrice {
				return errors.New("open_long requires take_profit above entry_price")
			}
		case "open_short":
			if decision.TakeProfit >= decision.EntryPrice {
				return errors.New("open_short requires take_profit below entry_price")
			}
		}
	}
	if params.MinRiskRewardRatio > 0 {
		if decision.StopLoss <= 0 || decision.TakeProfit <= 0 {
			return errors.New("min_risk_reward_ratio requires stop_loss and take_profit")
		}
		risk := 0.0
		reward := 0.0
		switch action {
		case "open_long":
			risk = decision.EntryPrice - decision.StopLoss
			reward = decision.TakeProfit - decision.EntryPrice
		case "open_short":
			risk = decision.StopLoss - decision.EntryPrice
			reward = decision.EntryPrice - decision.TakeProfit
		}
		if risk <= 0 || reward <= 0 {
			return errors.New("invalid entry_price/stop_loss/take_profit relationship")
		}
		ratio := reward / risk
		if ratio+1e-9 < params.MinRiskRewardRatio {
			return fmt.Errorf("reward/risk %.2f below minimum %.2f", ratio, params.MinRiskRewardRatio)
		}
	}
	return nil
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

func (m *Manager) enforceOpenRisk(trader *VirtualTrader, decision *executorpkg.Decision, leverage int) error {
	if trader == nil || decision == nil {
		return errors.New("manager policy: trader and decision are required")
	}
	if err := enforceAllowedSymbol(trader.RiskParams.AllowedSymbols, decision.Symbol); err != nil {
		return err
	}
	if err := enforceDailyLossLimit(trader, time.Now().UTC()); err != nil {
		var dailyErr *DailyLossLimitError
		if errors.As(err, &dailyErr) {
			m.tripRiskCircuitBreaker(context.Background(), trader, dailyErr.Error())
		}
		return err
	}
	return m.enforceSecondaryRisk(trader, decision, leverage)
}

func enforceAllowedSymbol(allowed []string, symbol string) error {
	symbol = normalizeSymbol(symbol)
	if symbol == "" {
		return errors.New("symbol is required")
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, candidate := range allowed {
		if normalizeSymbol(candidate) == symbol {
			return nil
		}
	}
	return fmt.Errorf("symbol %s is not in allowed_symbols", symbol)
}

func enforceDailyLossLimit(trader *VirtualTrader, now time.Time) error {
	if trader == nil {
		return errors.New("trader is required")
	}
	maxUSD := trader.RiskParams.MaxDailyLossUSD
	maxPct := trader.RiskParams.MaxDailyLossPct
	if maxUSD <= 0 && maxPct <= 0 {
		return nil
	}
	trader.mu.RLock()
	state := trader.DailyRisk
	alloc := trader.ResourceAlloc
	trader.mu.RUnlock()

	start := state.StartEquityUSD
	current := state.LastEquityUSD
	if !sameUTCDate(state.UpdatedAt, now) {
		start = alloc.CurrentEquityUSD
		current = alloc.CurrentEquityUSD
	}
	if start <= 0 || current <= 0 {
		return nil
	}
	loss := start - current
	if loss <= 0 {
		return nil
	}
	limit := positiveMin(maxUSD, start*(maxPct/100.0))
	if limit <= 0 {
		return nil
	}
	if loss > limit+1e-6 {
		date := state.Date
		if date == "" {
			date = now.UTC().Format("2006-01-02")
		}
		return &DailyLossLimitError{
			Date:             date,
			StartEquityUSD:   start,
			CurrentEquityUSD: current,
			LossUSD:          loss,
			LimitUSD:         limit,
		}
	}
	return nil
}

func updateDailyRiskState(prev DailyRiskState, equity float64, now time.Time) DailyRiskState {
	if equity <= 0 {
		return prev
	}
	now = now.UTC()
	date := now.Format("2006-01-02")
	if prev.Date != date || prev.StartEquityUSD <= 0 {
		return DailyRiskState{
			Date:           date,
			StartEquityUSD: equity,
			LastEquityUSD:  equity,
			UpdatedAt:      now,
		}
	}
	prev.LastEquityUSD = equity
	prev.UpdatedAt = now
	return prev
}

func sameUTCDate(left, right time.Time) bool {
	if left.IsZero() || right.IsZero() {
		return false
	}
	return left.UTC().Format("2006-01-02") == right.UTC().Format("2006-01-02")
}

func positiveMin(values ...float64) float64 {
	min := 0.0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if min == 0 || value < min {
			min = value
		}
	}
	return min
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
