package health

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"nof0-api/internal/svc"
)

const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
	StatusDisabled = "disabled"
	StatusError    = "error"

	defaultTimeout = 2 * time.Second
)

// CheckResult describes one dependency check in a health/readiness response.
type CheckResult struct {
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	Message   string `json:"message,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
}

// Report is the JSON payload returned by health endpoints.
type Report struct {
	Status       string                 `json:"status"`
	Checks       map[string]CheckResult `json:"checks"`
	ServerTimeMs int64                  `json:"server_time_ms"`
}

// Healthy reports whether all required checks passed.
func (r Report) Healthy() bool {
	return r.Status == StatusOK
}

// Checker evaluates process and dependency health for HTTP handlers.
type Checker struct {
	Service *svc.ServiceContext
	Timeout time.Duration
	Now     func() time.Time
}

// Liveness returns process-local health. It does not perform external IO.
func (c Checker) Liveness() Report {
	return c.report(map[string]CheckResult{
		"process": {
			Status:   StatusOK,
			Required: true,
			Message:  "process is running",
		},
	})
}

// Readiness checks configured dependencies required to serve API traffic.
func (c Checker) Readiness(ctx context.Context) Report {
	checks := map[string]CheckResult{
		"postgres":           c.checkPostgres(ctx),
		"redis":              c.checkRedis(ctx),
		"market_providers":   c.checkMarketProviders(),
		"exchange_providers": c.checkExchangeProviders(),
	}
	return c.report(checks)
}

func (c Checker) checkPostgres(ctx context.Context) CheckResult {
	if c.Service == nil || strings.TrimSpace(c.Service.Config.Postgres.DataSource) == "" {
		return disabled("postgres is not configured")
	}
	if c.Service.DBConn == nil {
		return failed("postgres is configured but connection is unavailable")
	}
	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	var one int
	if err := c.Service.DBConn.QueryRowCtx(checkCtx, &one, "SELECT 1"); err != nil {
		return failedWithLatency(fmt.Sprintf("postgres ping failed: %v", err), start)
	}
	return okWithLatency("postgres ping ok", start)
}

func (c Checker) checkRedis(ctx context.Context) CheckResult {
	if c.Service == nil || !cacheConfigured(c.Service) {
		return disabled("redis is not configured")
	}
	if c.Service.Redis == nil {
		return failed("redis is configured but client is unavailable")
	}
	start := time.Now()
	checkCtx, cancel := context.WithTimeout(ctx, c.timeout())
	defer cancel()
	if !c.Service.Redis.PingCtx(checkCtx) {
		return failedWithLatency("redis ping failed", start)
	}
	return okWithLatency("redis ping ok", start)
}

func (c Checker) checkMarketProviders() CheckResult {
	if c.Service == nil || (c.Service.MarketConfig == nil && len(c.Service.MarketProviders) == 0) {
		return disabled("market providers are not configured")
	}
	if len(c.Service.MarketProviders) == 0 {
		return failed("market config loaded but no providers were built")
	}
	if c.Service.MarketConfig != nil && strings.TrimSpace(c.Service.MarketConfig.Default) != "" && c.Service.DefaultMarket == nil {
		return failed(fmt.Sprintf("default market provider %q is unavailable", c.Service.MarketConfig.Default))
	}
	return ok(fmt.Sprintf("configured providers: %s", strings.Join(sortedKeys(c.Service.MarketProviders), ",")))
}

func (c Checker) checkExchangeProviders() CheckResult {
	if c.Service == nil || (c.Service.ExchangeConfig == nil && len(c.Service.ExchangeProviders) == 0) {
		return disabled("exchange providers are not configured")
	}
	if len(c.Service.ExchangeProviders) == 0 {
		return failed("exchange config loaded but no providers were built")
	}
	if c.Service.ExchangeConfig != nil && strings.TrimSpace(c.Service.ExchangeConfig.Default) != "" && c.Service.DefaultExchange == nil {
		return failed(fmt.Sprintf("default exchange provider %q is unavailable", c.Service.ExchangeConfig.Default))
	}
	return ok(fmt.Sprintf("configured providers: %s", strings.Join(sortedKeys(c.Service.ExchangeProviders), ",")))
}

func (c Checker) report(checks map[string]CheckResult) Report {
	status := StatusOK
	for _, check := range checks {
		if check.Required && check.Status == StatusError {
			status = StatusDegraded
			break
		}
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	return Report{
		Status:       status,
		Checks:       checks,
		ServerTimeMs: now().UTC().UnixMilli(),
	}
}

func (c Checker) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeout
}

func cacheConfigured(s *svc.ServiceContext) bool {
	for _, node := range s.Config.Cache {
		if strings.TrimSpace(node.Host) != "" {
			return true
		}
	}
	return false
}

func sortedKeys[V any](in map[string]V) []string {
	keys := make([]string, 0, len(in))
	for key := range in {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func ok(message string) CheckResult {
	return CheckResult{Status: StatusOK, Required: true, Message: message}
}

func okWithLatency(message string, start time.Time) CheckResult {
	result := ok(message)
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

func disabled(message string) CheckResult {
	return CheckResult{Status: StatusDisabled, Required: false, Message: message}
}

func failed(message string) CheckResult {
	return CheckResult{Status: StatusError, Required: true, Message: message}
}

func failedWithLatency(message string, start time.Time) CheckResult {
	result := failed(message)
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}
