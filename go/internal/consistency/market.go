package consistency

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/zeromicro/go-zero/core/logx"
	gocache "github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	cachekeys "nof0-api/internal/cache"
	persistmetrics "nof0-api/internal/persistence/metrics"
	marketpkg "nof0-api/pkg/market"
)

const (
	defaultMarketMaxAge          = 2 * time.Minute
	defaultMarketTolerancePct    = 0.001
	defaultMarketExchangeTimeout = 5 * time.Second
	defaultMarketCheckInterval   = 2 * time.Minute
	marketConsistencyCacheReadOp = "market.consistency.cache_read"
)

// MarketCheckerConfig wires a read-only market data consistency checker.
type MarketCheckerConfig struct {
	SQLConn           sqlx.SqlConn
	Cache             gocache.Cache
	Providers         map[string]marketpkg.Provider
	Symbols           []string
	MaxAge            time.Duration
	PriceTolerancePct float64
	ExchangeTimeout   time.Duration
	CompareExchange   bool
}

// MarketChecker compares latest market data across DB, cache, and optional live providers.
type MarketChecker struct {
	sqlConn           sqlx.SqlConn
	cache             gocache.Cache
	providers         map[string]marketpkg.Provider
	providerNames     []string
	symbols           []string
	maxAge            time.Duration
	priceTolerancePct float64
	exchangeTimeout   time.Duration
	compareExchange   bool
}

// MarketReport is the structured output of one consistency pass.
type MarketReport struct {
	CheckedAt time.Time
	Summary   MarketSummary
	Issues    []MarketIssue
}

// Healthy reports whether the check found no consistency issues.
func (r MarketReport) Healthy() bool {
	return len(r.Issues) == 0
}

// MarketSummary contains counters useful for logs and tests.
type MarketSummary struct {
	DBRows           int
	DBMissing        int
	CacheChecked     int
	CacheMissing     int
	CacheStale       int
	CacheMismatched  int
	ExchangeChecked  int
	ExchangeErrors   int
	ExchangeMismatch int
}

// MarketIssue describes one observed inconsistency.
type MarketIssue struct {
	Severity      string
	Scope         string
	Provider      string
	Symbol        string
	Message       string
	DBPrice       float64
	CachePrice    float64
	ExchangePrice float64
	DBTsMs        int64
	CacheTsMs     int64
}

type marketLatestRow struct {
	Provider string  `db:"provider"`
	Symbol   string  `db:"symbol"`
	Price    float64 `db:"price"`
	TsMs     int64   `db:"ts_ms"`
}

type latestCachePayload struct {
	Price       float64 `json:"price"`
	Ts          int64   `json:"ts"`
	TimestampMs int64   `json:"timestamp_ms"`
}

// NewMarketChecker creates a checker. It returns nil when SQL/cache dependencies are absent.
func NewMarketChecker(cfg MarketCheckerConfig) *MarketChecker {
	if cfg.SQLConn == nil || cfg.Cache == nil {
		return nil
	}
	providers := make(map[string]marketpkg.Provider, len(cfg.Providers))
	names := make([]string, 0, len(cfg.Providers))
	for name, provider := range cfg.Providers {
		name = strings.TrimSpace(name)
		if name == "" || provider == nil {
			continue
		}
		providers[name] = provider
		names = append(names, name)
	}
	sort.Strings(names)

	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = defaultMarketMaxAge
	}
	tolerance := cfg.PriceTolerancePct
	if tolerance <= 0 {
		tolerance = defaultMarketTolerancePct
	}
	exchangeTimeout := cfg.ExchangeTimeout
	if exchangeTimeout <= 0 {
		exchangeTimeout = defaultMarketExchangeTimeout
	}
	return &MarketChecker{
		sqlConn:           cfg.SQLConn,
		cache:             cfg.Cache,
		providers:         providers,
		providerNames:     names,
		symbols:           normalizeSymbols(cfg.Symbols),
		maxAge:            maxAge,
		priceTolerancePct: tolerance,
		exchangeTimeout:   exchangeTimeout,
		compareExchange:   cfg.CompareExchange,
	}
}

// Check performs one consistency pass.
func (c *MarketChecker) Check(ctx context.Context) (*MarketReport, error) {
	if c == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	checkedAt := time.Now().UTC()
	rows, err := c.loadLatestRows(ctx)
	if err != nil {
		return nil, err
	}
	report := &MarketReport{
		CheckedAt: checkedAt,
		Summary: MarketSummary{
			DBRows: len(rows),
		},
	}
	byPair := make(map[string]marketLatestRow, len(rows))
	for _, row := range rows {
		provider := strings.TrimSpace(row.Provider)
		symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
		if provider == "" || symbol == "" {
			continue
		}
		row.Provider = provider
		row.Symbol = symbol
		byPair[marketPairKey(provider, symbol)] = row
	}

	for _, pair := range c.expectedPairs(byPair) {
		row, ok := byPair[marketPairKey(pair.provider, pair.symbol)]
		if !ok {
			report.Summary.DBMissing++
			report.Issues = append(report.Issues, MarketIssue{
				Severity: "warn",
				Scope:    "db",
				Provider: pair.provider,
				Symbol:   pair.symbol,
				Message:  "missing latest price row",
			})
			continue
		}
		c.checkCache(ctx, report, row, checkedAt)
		if c.compareExchange {
			c.checkExchange(ctx, report, row)
		}
	}
	for _, issue := range report.Issues {
		persistmetrics.RecordInconsistency(issue.Scope, issue.Provider, issue.Symbol, 1)
	}
	return report, nil
}

// RunPeriodic checks consistency on an interval until ctx is cancelled.
func (c *MarketChecker) RunPeriodic(ctx context.Context, interval time.Duration) {
	if c == nil {
		return
	}
	if interval <= 0 {
		interval = defaultMarketCheckInterval
	}
	c.logOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.logOnce(ctx)
		}
	}
}

func (c *MarketChecker) logOnce(ctx context.Context) {
	report, err := c.Check(ctx)
	if err != nil {
		logx.WithContext(ctx).Errorf("market consistency: check failed err=%v", err)
		return
	}
	if report == nil {
		return
	}
	if report.Healthy() {
		logx.WithContext(ctx).Infof("market consistency: healthy db_rows=%d cache_checked=%d exchange_checked=%d",
			report.Summary.DBRows, report.Summary.CacheChecked, report.Summary.ExchangeChecked)
		return
	}
	logx.WithContext(ctx).Slowf("market consistency: issues=%d db_missing=%d cache_missing=%d cache_stale=%d cache_mismatch=%d exchange_errors=%d exchange_mismatch=%d",
		len(report.Issues),
		report.Summary.DBMissing,
		report.Summary.CacheMissing,
		report.Summary.CacheStale,
		report.Summary.CacheMismatched,
		report.Summary.ExchangeErrors,
		report.Summary.ExchangeMismatch,
	)
	for i, issue := range report.Issues {
		if i >= 20 {
			logx.WithContext(ctx).Slowf("market consistency: suppressing %d additional issues", len(report.Issues)-i)
			break
		}
		logx.WithContext(ctx).Slowf("market consistency issue: severity=%s scope=%s provider=%s symbol=%s msg=%s db_price=%.8f cache_price=%.8f exchange_price=%.8f",
			issue.Severity,
			issue.Scope,
			issue.Provider,
			issue.Symbol,
			issue.Message,
			issue.DBPrice,
			issue.CachePrice,
			issue.ExchangePrice,
		)
	}
}

func (c *MarketChecker) loadLatestRows(ctx context.Context) ([]marketLatestRow, error) {
	rows := make([]marketLatestRow, 0)
	query, args := buildLatestRowsQuery(c.providerNames, c.symbols)
	if err := c.sqlConn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

func (c *MarketChecker) checkCache(ctx context.Context, report *MarketReport, row marketLatestRow, checkedAt time.Time) {
	report.Summary.CacheChecked++
	key := cachekeys.PriceLatestByProviderKey(row.Provider, row.Symbol)
	var payload latestCachePayload
	if err := c.cache.GetCtx(ctx, key, &payload); err != nil {
		if c.cache.IsNotFound(err) {
			persistmetrics.RecordCacheOp(marketConsistencyCacheReadOp, persistmetrics.StatusMiss, 1)
			report.Summary.CacheMissing++
			report.Issues = append(report.Issues, MarketIssue{
				Severity: "warn",
				Scope:    "cache",
				Provider: row.Provider,
				Symbol:   row.Symbol,
				Message:  "missing provider latest price cache",
				DBPrice:  row.Price,
				DBTsMs:   row.TsMs,
			})
			return
		}
		persistmetrics.RecordCacheOp(marketConsistencyCacheReadOp, persistmetrics.StatusError, 1)
		report.Summary.CacheMissing++
		report.Issues = append(report.Issues, MarketIssue{
			Severity: "warn",
			Scope:    "cache",
			Provider: row.Provider,
			Symbol:   row.Symbol,
			Message:  fmt.Sprintf("read latest price cache failed: %v", err),
			DBPrice:  row.Price,
			DBTsMs:   row.TsMs,
		})
		return
	}
	persistmetrics.RecordCacheOp(marketConsistencyCacheReadOp, persistmetrics.StatusHit, 1)
	cacheTs := payload.Ts
	if cacheTs == 0 {
		cacheTs = payload.TimestampMs
	}
	if !pricesClose(row.Price, payload.Price, c.priceTolerancePct) {
		report.Summary.CacheMismatched++
		report.Issues = append(report.Issues, MarketIssue{
			Severity:   "warn",
			Scope:      "cache",
			Provider:   row.Provider,
			Symbol:     row.Symbol,
			Message:    "latest price cache differs from DB",
			DBPrice:    row.Price,
			CachePrice: payload.Price,
			DBTsMs:     row.TsMs,
			CacheTsMs:  cacheTs,
		})
	}
	if cacheTs > 0 && c.maxAge > 0 {
		age := checkedAt.Sub(time.UnixMilli(cacheTs).UTC())
		if age > c.maxAge {
			report.Summary.CacheStale++
			report.Issues = append(report.Issues, MarketIssue{
				Severity:   "warn",
				Scope:      "cache",
				Provider:   row.Provider,
				Symbol:     row.Symbol,
				Message:    fmt.Sprintf("latest price cache stale age=%s", age.Round(time.Second)),
				DBPrice:    row.Price,
				CachePrice: payload.Price,
				DBTsMs:     row.TsMs,
				CacheTsMs:  cacheTs,
			})
		}
	}
}

func (c *MarketChecker) checkExchange(ctx context.Context, report *MarketReport, row marketLatestRow) {
	provider := c.providers[row.Provider]
	if provider == nil {
		return
	}
	report.Summary.ExchangeChecked++
	reqCtx, cancel := context.WithTimeout(ctx, c.exchangeTimeout)
	snapshot, err := provider.Snapshot(reqCtx, row.Symbol)
	cancel()
	if err != nil {
		report.Summary.ExchangeErrors++
		report.Issues = append(report.Issues, MarketIssue{
			Severity: "warn",
			Scope:    "exchange",
			Provider: row.Provider,
			Symbol:   row.Symbol,
			Message:  fmt.Sprintf("snapshot failed: %v", err),
			DBPrice:  row.Price,
			DBTsMs:   row.TsMs,
		})
		return
	}
	if snapshot == nil || !(snapshot.Price.Last > 0) {
		report.Summary.ExchangeErrors++
		report.Issues = append(report.Issues, MarketIssue{
			Severity: "warn",
			Scope:    "exchange",
			Provider: row.Provider,
			Symbol:   row.Symbol,
			Message:  "snapshot missing latest price",
			DBPrice:  row.Price,
			DBTsMs:   row.TsMs,
		})
		return
	}
	exchangePrice := snapshot.Price.Last
	if !pricesClose(row.Price, exchangePrice, c.priceTolerancePct) {
		report.Summary.ExchangeMismatch++
		report.Issues = append(report.Issues, MarketIssue{
			Severity:      "warn",
			Scope:         "exchange",
			Provider:      row.Provider,
			Symbol:        row.Symbol,
			Message:       "latest DB price differs from live provider snapshot",
			DBPrice:       row.Price,
			ExchangePrice: exchangePrice,
			DBTsMs:        row.TsMs,
		})
	}
}

type marketPair struct {
	provider string
	symbol   string
}

func (c *MarketChecker) expectedPairs(rows map[string]marketLatestRow) []marketPair {
	pairs := make([]marketPair, 0)
	if len(c.providerNames) > 0 && len(c.symbols) > 0 {
		for _, provider := range c.providerNames {
			for _, symbol := range c.symbols {
				pairs = append(pairs, marketPair{provider: provider, symbol: symbol})
			}
		}
		return pairs
	}
	for _, row := range rows {
		pairs = append(pairs, marketPair{provider: row.Provider, symbol: row.Symbol})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].provider == pairs[j].provider {
			return pairs[i].symbol < pairs[j].symbol
		}
		return pairs[i].provider < pairs[j].provider
	})
	return pairs
}

func buildLatestRowsQuery(providers, symbols []string) (string, []any) {
	base := "SELECT provider, symbol, price, ts_ms FROM public.price_latest"
	conditions := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if len(providers) > 0 {
		args = append(args, pq.Array(providers))
		conditions = append(conditions, fmt.Sprintf("provider = ANY($%d)", len(args)))
	}
	if len(symbols) > 0 {
		args = append(args, pq.Array(symbols))
		conditions = append(conditions, fmt.Sprintf("symbol = ANY($%d)", len(args)))
	}
	if len(conditions) > 0 {
		base += " WHERE " + strings.Join(conditions, " AND ")
	}
	base += " ORDER BY provider, symbol"
	return base, args
}

func normalizeSymbols(symbols []string) []string {
	set := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			continue
		}
		set[symbol] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for symbol := range set {
		out = append(out, symbol)
	}
	sort.Strings(out)
	return out
}

func marketPairKey(provider, symbol string) string {
	return strings.TrimSpace(provider) + "\x00" + strings.ToUpper(strings.TrimSpace(symbol))
}

func pricesClose(left, right, tolerancePct float64) bool {
	if left == right {
		return true
	}
	if !(left > 0) || !(right > 0) {
		return false
	}
	base := math.Max(math.Abs(left), math.Abs(right))
	if base == 0 {
		return true
	}
	return math.Abs(left-right)/base <= tolerancePct
}
