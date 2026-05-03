package manager

import (
	"context"
	"strings"
	"time"
)

const riskCircuitBreakerReason = "risk_circuit_breaker"

func (m *Manager) tripRiskCircuitBreaker(ctx context.Context, trader *VirtualTrader, reason string) {
	if trader == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "daily loss limit exceeded"
	}
	now := time.Now().UTC()
	date := now.Format("2006-01-02")

	trader.mu.Lock()
	if trader.DailyRisk.Date != "" {
		date = trader.DailyRisk.Date
	}
	trader.State = TraderStatePaused
	trader.PauseUntil = time.Time{}
	trader.RiskCircuit = RiskCircuitState{
		Blocked:     true,
		Date:        date,
		Reason:      reason,
		TriggeredAt: now,
	}
	trader.UpdatedAt = now
	trader.mu.Unlock()

	m.persistRuntimeState(ctx, trader)
}

func clearStaleRiskCircuit(trader *VirtualTrader, daily DailyRiskState) {
	if trader == nil || !trader.RiskCircuit.Blocked {
		return
	}
	if daily.Date == "" {
		return
	}
	if trader.RiskCircuit.Date == "" {
		if !sameUTCDate(trader.RiskCircuit.TriggeredAt, daily.UpdatedAt) {
			trader.RiskCircuit = RiskCircuitState{}
		}
		return
	}
	if trader.RiskCircuit.Date != daily.Date {
		trader.RiskCircuit = RiskCircuitState{}
	}
}
