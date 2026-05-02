package consistency

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	cachekeys "nof0-api/internal/cache"
	marketpkg "nof0-api/pkg/market"
)

func TestMarketCheckerReportsCacheAndExchangeDifferences(t *testing.T) {
	ts := time.Now().UTC()
	conn := &checkerSqlConn{}
	conn.queryRowsFn = func(v any, query string, args ...any) error {
		rows, ok := v.(*[]marketLatestRow)
		require.True(t, ok)
		require.Contains(t, strings.ToLower(query), "from public.price_latest")
		require.Contains(t, strings.ToLower(query), "provider = any($1)")
		require.Contains(t, strings.ToLower(query), "symbol = any($2)")
		require.Len(t, args, 2)
		*rows = []marketLatestRow{
			{Provider: "hyperliquid", Symbol: "BTC", Price: 50000, TsMs: ts.UnixMilli()},
		}
		return nil
	}
	cache := newCheckerCache()
	cache.values[cachekeys.PriceLatestByProviderKey("hyperliquid", "BTC")] = latestCachePayload{
		Price: 49000,
		Ts:    ts.UnixMilli(),
	}
	checker := NewMarketChecker(MarketCheckerConfig{
		SQLConn: conn,
		Cache:   cache,
		Providers: map[string]marketpkg.Provider{
			"hyperliquid": &checkerMarketProvider{price: 51000},
		},
		Symbols:           []string{"BTC", "ETH"},
		PriceTolerancePct: 0.001,
		CompareExchange:   true,
	})

	report, err := checker.Check(context.Background())

	require.NoError(t, err)
	require.NotNil(t, report)
	require.False(t, report.Healthy())
	require.Equal(t, 1, report.Summary.DBRows)
	require.Equal(t, 1, report.Summary.DBMissing)
	require.Equal(t, 1, report.Summary.CacheChecked)
	require.Equal(t, 1, report.Summary.CacheMismatched)
	require.Equal(t, 1, report.Summary.ExchangeChecked)
	require.Equal(t, 1, report.Summary.ExchangeMismatch)
	require.Len(t, report.Issues, 3)
}

func TestMarketCheckerReportsMissingCache(t *testing.T) {
	ts := time.Now().UTC()
	conn := &checkerSqlConn{}
	conn.queryRowsFn = func(v any, _ string, _ ...any) error {
		rows := v.(*[]marketLatestRow)
		*rows = []marketLatestRow{
			{Provider: "hyperliquid", Symbol: "BTC", Price: 50000, TsMs: ts.UnixMilli()},
		}
		return nil
	}
	checker := NewMarketChecker(MarketCheckerConfig{
		SQLConn: conn,
		Cache:   newCheckerCache(),
	})

	report, err := checker.Check(context.Background())

	require.NoError(t, err)
	require.NotNil(t, report)
	require.Equal(t, 1, report.Summary.CacheMissing)
	require.Len(t, report.Issues, 1)
	require.Equal(t, "cache", report.Issues[0].Scope)
}

type checkerSqlConn struct {
	mu          sync.Mutex
	queryRowsFn func(v any, query string, args ...any) error
}

func (c *checkerSqlConn) Exec(string, ...any) (sql.Result, error) {
	return nil, errCheckerUnsupported
}

func (c *checkerSqlConn) ExecCtx(context.Context, string, ...any) (sql.Result, error) {
	return nil, errCheckerUnsupported
}

func (c *checkerSqlConn) Prepare(string) (sqlx.StmtSession, error) {
	return nil, errCheckerUnsupported
}

func (c *checkerSqlConn) PrepareCtx(context.Context, string) (sqlx.StmtSession, error) {
	return nil, errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRow(any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRowCtx(context.Context, any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRowPartial(any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRowPartialCtx(context.Context, any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRows(v any, query string, args ...any) error {
	return c.QueryRowsCtx(context.Background(), v, query, args...)
}

func (c *checkerSqlConn) QueryRowsCtx(_ context.Context, v any, query string, args ...any) error {
	if c.queryRowsFn != nil {
		return c.queryRowsFn(v, query, args...)
	}
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRowsPartial(any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) QueryRowsPartialCtx(context.Context, any, string, ...any) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) RawDB() (*sql.DB, error) {
	return nil, nil
}

func (c *checkerSqlConn) Transact(func(sqlx.Session) error) error {
	return errCheckerUnsupported
}

func (c *checkerSqlConn) TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error {
	return errCheckerUnsupported
}

type checkerCache struct {
	values map[string]any
}

func newCheckerCache() *checkerCache {
	return &checkerCache{values: make(map[string]any)}
}

func (c *checkerCache) Del(...string) error {
	return nil
}

func (c *checkerCache) DelCtx(context.Context, ...string) error {
	return nil
}

func (c *checkerCache) Get(key string, val any) error {
	return c.GetCtx(context.Background(), key, val)
}

func (c *checkerCache) GetCtx(_ context.Context, key string, val any) error {
	raw, ok := c.values[key]
	if !ok {
		return errCheckerCacheMiss
	}
	switch dest := val.(type) {
	case *latestCachePayload:
		payload, ok := raw.(latestCachePayload)
		if !ok {
			return errCheckerUnsupported
		}
		*dest = payload
	default:
		return errCheckerUnsupported
	}
	return nil
}

func (c *checkerCache) IsNotFound(err error) bool {
	return errors.Is(err, errCheckerCacheMiss)
}

func (c *checkerCache) Set(string, any) error {
	return nil
}

func (c *checkerCache) SetCtx(context.Context, string, any) error {
	return nil
}

func (c *checkerCache) SetWithExpire(string, any, time.Duration) error {
	return nil
}

func (c *checkerCache) SetWithExpireCtx(context.Context, string, any, time.Duration) error {
	return nil
}

func (c *checkerCache) Take(any, string, func(any) error) error {
	return errCheckerUnsupported
}

func (c *checkerCache) TakeCtx(context.Context, any, string, func(any) error) error {
	return errCheckerUnsupported
}

func (c *checkerCache) TakeWithExpire(any, string, func(any, time.Duration) error) error {
	return errCheckerUnsupported
}

func (c *checkerCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return errCheckerUnsupported
}

type checkerMarketProvider struct {
	price float64
}

func (p *checkerMarketProvider) Snapshot(context.Context, string) (*marketpkg.Snapshot, error) {
	return &marketpkg.Snapshot{
		Symbol: "BTC",
		Price:  marketpkg.PriceInfo{Last: p.price},
	}, nil
}

func (p *checkerMarketProvider) ListAssets(context.Context) ([]marketpkg.Asset, error) {
	return nil, nil
}

var (
	errCheckerUnsupported = errors.New("unsupported checker test operation")
	errCheckerCacheMiss   = errors.New("checker cache miss")
)
