package marketpersist

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
	"nof0-api/internal/model"
	persistresilience "nof0-api/internal/persistence/resilience"
	"nof0-api/pkg/market"
)

func TestUpsertAssetsPersistsBatch(t *testing.T) {
	conn := &recordingSqlConn{}
	svc := &Service{sqlConn: conn}

	err := svc.UpsertAssets(context.Background(), " hyperliquid ", []market.Asset{
		{
			Symbol:    "BTC",
			Base:      "Bitcoin",
			Precision: 5,
			IsActive:  true,
			RawMetadata: map[string]any{
				"maxLeverage":  50.0,
				"marginTable":  1,
				"onlyIsolated": false,
			},
		},
		{
			Symbol:    "ETH",
			Precision: 4,
			IsActive:  false,
			RawMetadata: map[string]any{
				"maxLeverage": "25",
			},
		},
	})

	require.NoError(t, err)
	execs := conn.snapshot()
	require.Len(t, execs, 1)
	require.False(t, execs[0].inTx)
	require.Contains(t, strings.ToLower(execs[0].query), "insert into public.market_assets")
	require.Contains(t, strings.ToLower(execs[0].query), "on conflict (provider, symbol) do update")
	require.Len(t, execs[0].args, 16)
	require.Equal(t, "hyperliquid", execs[0].args[0])
	require.Equal(t, "BTC", execs[0].args[1])
	require.Equal(t, sql.NullString{String: "Bitcoin", Valid: true}, execs[0].args[2])
	require.Equal(t, sql.NullInt64{Int64: 5, Valid: true}, execs[0].args[3])
	require.Equal(t, sql.NullFloat64{Float64: 50, Valid: true}, execs[0].args[4])
	require.Equal(t, sql.NullBool{Bool: false, Valid: true}, execs[0].args[5])
	require.Equal(t, sql.NullInt64{Int64: 1, Valid: true}, execs[0].args[6])
	require.Equal(t, false, execs[0].args[7])
	require.Equal(t, true, execs[0].args[15])
}

func TestRecordSnapshotPersistsLatestPriceAndMarketContext(t *testing.T) {
	conn := &recordingSqlConn{}
	svc := &Service{sqlConn: conn}

	err := svc.RecordSnapshot(context.Background(), "hyperliquid", &market.Snapshot{
		Symbol: "btc",
		Price:  market.PriceInfo{Last: 50000},
		Change: market.ChangeInfo{OneHour: 0.02, FourHour: 0.05},
		Funding: &market.FundingInfo{
			Rate: 0.001,
		},
		OpenInterest: &market.OpenInterestInfo{
			Latest:  12345,
			Average: 12000,
		},
		Indicators: market.IndicatorInfo{
			MACD: 42,
			EMA:  map[string]float64{"EMA20": 49000},
			RSI:  map[string]float64{"RSI14": 55},
		},
	})

	require.NoError(t, err)
	execs := conn.snapshot()
	require.Len(t, execs, 2)
	require.True(t, execs[0].inTx)
	require.True(t, execs[1].inTx)

	require.Contains(t, strings.ToLower(execs[0].query), "insert into public.price_latest")
	require.Equal(t, "hyperliquid", execs[0].args[0])
	require.Equal(t, "BTC", execs[0].args[1])
	require.Equal(t, 50000.0, execs[0].args[2])
	require.IsType(t, int64(0), execs[0].args[3])
	require.Greater(t, execs[0].args[3].(int64), int64(0))

	require.Contains(t, strings.ToLower(execs[1].query), "insert into public.market_asset_ctx")
	require.Equal(t, "hyperliquid", execs[1].args[0])
	require.Equal(t, "BTC", execs[1].args[1])
	ctxJSON, ok := execs[1].args[2].(string)
	require.True(t, ok)
	require.Contains(t, ctxJSON, `"provider":"hyperliquid"`)
	require.Contains(t, ctxJSON, `"symbol":"BTC"`)
	require.Contains(t, ctxJSON, `"price":50000`)
}

func TestRecordSnapshotQueuesCacheRetryOnTransientCacheFailure(t *testing.T) {
	conn := &recordingSqlConn{}
	cache := newRecordingCache()
	cache.setErr = context.DeadlineExceeded
	retries := &recordingRetryQueue{}
	svc := &Service{
		sqlConn:    conn,
		cache:      cache,
		ttl:        cachekeys.TTLSet{Short: time.Minute, Medium: time.Minute},
		retryQueue: retries,
	}

	err := svc.RecordSnapshot(context.Background(), "hyperliquid", &market.Snapshot{
		Symbol: "btc",
		Price:  market.PriceInfo{Last: 50000},
	})

	require.NoError(t, err)
	tasks := retries.snapshot()
	require.NotEmpty(t, tasks)
	require.Equal(t, marketCachePriceOp, tasks[0].Operation)
	require.Contains(t, tasks[0].Fields, "failure_class")
}

func TestRecordPriceSeriesPersistsValidTicks(t *testing.T) {
	ticks := &recordingPriceTicksModel{}
	svc := &Service{priceTicksModel: ticks}
	ts := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)

	err := svc.RecordPriceSeries(context.Background(), "hyperliquid", "btc", []market.PriceTick{
		{
			Timestamp: ts,
			Price:     50000,
			Interval:  "3m",
			Open:      49900,
			High:      50100,
			Low:       49800,
			Close:     50000,
			Volume:    123,
			HasVolume: true,
		},
		{Timestamp: time.Time{}, Price: 49900},
		{Timestamp: ts.Add(time.Minute), Price: 0},
	})

	require.NoError(t, err)
	require.Len(t, ticks.rows, 1)
	require.Equal(t, "hyperliquid", ticks.rows[0].Provider)
	require.Equal(t, "BTC", ticks.rows[0].Symbol)
	require.Equal(t, 50000.0, ticks.rows[0].Price)
	require.Equal(t, ts.UnixMilli(), ticks.rows[0].TsMs)
	require.Equal(t, sql.NullFloat64{Float64: 123, Valid: true}, ticks.rows[0].Volume)
	require.True(t, ticks.rows[0].Raw.Valid)
	require.Contains(t, ticks.rows[0].Raw.String, `"interval":"3m"`)
}

func TestHydrateCachesRestoresLatestPriceAndMarketContext(t *testing.T) {
	ts := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	conn := &recordingSqlConn{}
	conn.queryRowsFn = func(v any, query string, args ...any) error {
		switch rows := v.(type) {
		case *[]priceLatestHydrateRow:
			*rows = []priceLatestHydrateRow{
				{Provider: "hyperliquid", Symbol: "BTC", Price: 50000, TsMs: ts.UnixMilli()},
			}
		case *[]marketContextHydrateRow:
			*rows = []marketContextHydrateRow{
				{
					Provider:  "hyperliquid",
					Symbol:    "BTC",
					Context:   `{"price":50000,"change":{"one_hour":0.01},"funding":{"rate":0.001},"open_interest":{"latest":123},"indicators":{"macd":42},"recorded_at_ms":1777716000000}`,
					UpdatedAt: ts,
				},
			}
		default:
			return errUnsupportedTestSQL
		}
		return nil
	}
	cache := newRecordingCache()
	svc := &Service{
		sqlConn: conn,
		cache:   cache,
		ttl: cachekeys.TTLSet{
			Short:  time.Minute,
			Medium: 2 * time.Minute,
			Long:   5 * time.Minute,
		},
	}

	err := svc.HydrateCaches(context.Background(), []string{" hyperliquid "})

	require.NoError(t, err)
	queries := conn.querySnapshot()
	require.Len(t, queries, 2)
	require.Contains(t, strings.ToLower(queries[0].query), "from public.price_latest")
	require.Contains(t, strings.ToLower(queries[0].query), "provider = any($1)")
	require.Len(t, queries[0].args, 1)
	require.Contains(t, strings.ToLower(queries[1].query), "from public.market_asset_ctx")

	writes := cache.snapshot()
	require.Contains(t, writes, cachekeys.PriceLatestByProviderKey("hyperliquid", "BTC"))
	require.Contains(t, writes, cachekeys.PriceLatestKey("BTC"))
	require.Contains(t, writes, cachekeys.CryptoPricesKey())
	require.Contains(t, writes, cachekeys.MarketAssetCtxKey("hyperliquid", "BTC"))

	providerPrice, ok := writes[cachekeys.PriceLatestByProviderKey("hyperliquid", "BTC")].value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, 50000.0, providerPrice["price"])
	require.Equal(t, ts.UnixMilli(), providerPrice["ts"])

	cryptoPrices, ok := writes[cachekeys.CryptoPricesKey()].value.(map[string]float64)
	require.True(t, ok)
	require.Equal(t, 50000.0, cryptoPrices["hyperliquid:BTC"])

	ctxPayload, ok := writes[cachekeys.MarketAssetCtxKey("hyperliquid", "BTC")].value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, 50000.0, ctxPayload["price"])
	require.Contains(t, ctxPayload, "oi")
	require.Equal(t, int64(1777716000000), ctxPayload["timestamp_ms"])
}

type recordedExec struct {
	inTx  bool
	query string
	args  []any
}

type recordedQuery struct {
	query string
	args  []any
}

type recordingSqlConn struct {
	mu          sync.Mutex
	execs       []recordedExec
	queries     []recordedQuery
	queryRowsFn func(v any, query string, args ...any) error
}

func (c *recordingSqlConn) snapshot() []recordedExec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]recordedExec, len(c.execs))
	copy(out, c.execs)
	return out
}

func (c *recordingSqlConn) querySnapshot() []recordedQuery {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]recordedQuery, len(c.queries))
	copy(out, c.queries)
	return out
}

func (c *recordingSqlConn) record(inTx bool, query string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copied := append([]any(nil), args...)
	c.execs = append(c.execs, recordedExec{inTx: inTx, query: query, args: copied})
}

func (c *recordingSqlConn) recordQuery(query string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copied := append([]any(nil), args...)
	c.queries = append(c.queries, recordedQuery{query: query, args: copied})
}

func (c *recordingSqlConn) Exec(query string, args ...any) (sql.Result, error) {
	return c.ExecCtx(context.Background(), query, args...)
}

func (c *recordingSqlConn) ExecCtx(_ context.Context, query string, args ...any) (sql.Result, error) {
	c.record(false, query, args...)
	return recordingResult(1), nil
}

func (c *recordingSqlConn) Prepare(string) (sqlx.StmtSession, error) {
	return nil, errUnsupportedTestSQL
}

func (c *recordingSqlConn) PrepareCtx(context.Context, string) (sqlx.StmtSession, error) {
	return nil, errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRow(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowPartial(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowPartialCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRows(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowsCtx(_ context.Context, v any, query string, args ...any) error {
	c.recordQuery(query, args...)
	if c.queryRowsFn != nil {
		return c.queryRowsFn(v, query, args...)
	}
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowsPartial(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) QueryRowsPartialCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (c *recordingSqlConn) RawDB() (*sql.DB, error) {
	return nil, nil
}

func (c *recordingSqlConn) Transact(fn func(sqlx.Session) error) error {
	return fn(&recordingSession{conn: c})
}

func (c *recordingSqlConn) TransactCtx(ctx context.Context, fn func(context.Context, sqlx.Session) error) error {
	return fn(ctx, &recordingSession{conn: c})
}

type recordingSession struct {
	conn *recordingSqlConn
}

func (s *recordingSession) Exec(query string, args ...any) (sql.Result, error) {
	return s.ExecCtx(context.Background(), query, args...)
}

func (s *recordingSession) ExecCtx(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.conn.record(true, query, args...)
	return recordingResult(1), nil
}

func (s *recordingSession) Prepare(string) (sqlx.StmtSession, error) {
	return nil, errUnsupportedTestSQL
}

func (s *recordingSession) PrepareCtx(context.Context, string) (sqlx.StmtSession, error) {
	return nil, errUnsupportedTestSQL
}

func (s *recordingSession) QueryRow(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowPartial(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowPartialCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRows(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowsCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowsPartial(any, string, ...any) error {
	return errUnsupportedTestSQL
}

func (s *recordingSession) QueryRowsPartialCtx(context.Context, any, string, ...any) error {
	return errUnsupportedTestSQL
}

type recordingPriceTicksModel struct {
	rows []*model.PriceTicks
}

func (m *recordingPriceTicksModel) Insert(_ context.Context, row *model.PriceTicks) (sql.Result, error) {
	copied := *row
	m.rows = append(m.rows, &copied)
	return recordingResult(1), nil
}

type recordingResult int64

func (r recordingResult) LastInsertId() (int64, error) { return int64(r), nil }

func (r recordingResult) RowsAffected() (int64, error) { return int64(r), nil }

var errUnsupportedTestSQL = errors.New("unsupported test sql operation")

type cacheWrite struct {
	value any
	ttl   time.Duration
}

type recordingCache struct {
	mu     sync.Mutex
	values map[string]cacheWrite
	setErr error
}

func newRecordingCache() *recordingCache {
	return &recordingCache{values: make(map[string]cacheWrite)}
}

func (c *recordingCache) snapshot() map[string]cacheWrite {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]cacheWrite, len(c.values))
	for k, v := range c.values {
		out[k] = v
	}
	return out
}

func (c *recordingCache) Del(keys ...string) error {
	return c.DelCtx(context.Background(), keys...)
}

func (c *recordingCache) DelCtx(_ context.Context, keys ...string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.values, key)
	}
	return nil
}

func (c *recordingCache) Get(key string, val any) error {
	return c.GetCtx(context.Background(), key, val)
}

func (c *recordingCache) GetCtx(_ context.Context, key string, val any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.values[key]
	if !ok {
		return errRecordingCacheNotFound
	}
	switch dest := val.(type) {
	case *map[string]float64:
		value, ok := entry.value.(map[string]float64)
		if !ok {
			return errUnsupportedTestSQL
		}
		*dest = value
	default:
		return errUnsupportedTestSQL
	}
	return nil
}

func (c *recordingCache) IsNotFound(err error) bool {
	return errors.Is(err, errRecordingCacheNotFound)
}

func (c *recordingCache) Set(key string, val any) error {
	return c.SetWithExpireCtx(context.Background(), key, val, 0)
}

func (c *recordingCache) SetCtx(ctx context.Context, key string, val any) error {
	return c.SetWithExpireCtx(ctx, key, val, 0)
}

func (c *recordingCache) SetWithExpire(key string, val any, expire time.Duration) error {
	return c.SetWithExpireCtx(context.Background(), key, val, expire)
}

func (c *recordingCache) SetWithExpireCtx(_ context.Context, key string, val any, expire time.Duration) error {
	if c.setErr != nil {
		return c.setErr
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = cacheWrite{value: val, ttl: expire}
	return nil
}

func (c *recordingCache) Take(any, string, func(any) error) error {
	return errUnsupportedTestSQL
}

func (c *recordingCache) TakeCtx(context.Context, any, string, func(any) error) error {
	return errUnsupportedTestSQL
}

func (c *recordingCache) TakeWithExpire(any, string, func(any, time.Duration) error) error {
	return errUnsupportedTestSQL
}

func (c *recordingCache) TakeWithExpireCtx(context.Context, any, string, func(any, time.Duration) error) error {
	return errUnsupportedTestSQL
}

var errRecordingCacheNotFound = errors.New("recording cache not found")

type recordingRetryQueue struct {
	mu    sync.Mutex
	tasks []persistresilience.Task
}

func (q *recordingRetryQueue) Enqueue(_ context.Context, task persistresilience.Task) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.tasks = append(q.tasks, task)
	return true
}

func (q *recordingRetryQueue) snapshot() []persistresilience.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]persistresilience.Task, len(q.tasks))
	copy(out, q.tasks)
	return out
}
