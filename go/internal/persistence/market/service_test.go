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

	"nof0-api/internal/model"
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

type recordedExec struct {
	inTx  bool
	query string
	args  []any
}

type recordingSqlConn struct {
	mu    sync.Mutex
	execs []recordedExec
}

func (c *recordingSqlConn) snapshot() []recordedExec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]recordedExec, len(c.execs))
	copy(out, c.execs)
	return out
}

func (c *recordingSqlConn) record(inTx bool, query string, args ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copied := append([]any(nil), args...)
	c.execs = append(c.execs, recordedExec{inTx: inTx, query: query, args: copied})
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

func (c *recordingSqlConn) QueryRowsCtx(context.Context, any, string, ...any) error {
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
