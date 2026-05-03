package health

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"nof0-api/internal/config"
	"nof0-api/internal/svc"
)

func TestLivenessReportsProcessOK(t *testing.T) {
	report := Checker{
		Now: fixedNow,
	}.Liveness()

	require.True(t, report.Healthy())
	require.Equal(t, StatusOK, report.Status)
	require.Equal(t, StatusOK, report.Checks["process"].Status)
	require.Equal(t, int64(1777716000000), report.ServerTimeMs)
}

func TestReadinessTreatsUnconfiguredDependenciesAsDisabled(t *testing.T) {
	report := Checker{
		Service: &svc.ServiceContext{},
		Now:     fixedNow,
	}.Readiness(context.Background())

	require.True(t, report.Healthy())
	require.Equal(t, StatusDisabled, report.Checks["postgres"].Status)
	require.False(t, report.Checks["postgres"].Required)
	require.Equal(t, StatusDisabled, report.Checks["redis"].Status)
	require.Equal(t, StatusDisabled, report.Checks["market_providers"].Status)
	require.Equal(t, StatusDisabled, report.Checks["exchange_providers"].Status)
}

func TestReadinessFailsWhenConfiguredPostgresUnavailable(t *testing.T) {
	report := Checker{
		Service: &svc.ServiceContext{
			Config: config.Config{
				Postgres: config.PostgresConf{DataSource: "postgres://example"},
			},
		},
		Now: fixedNow,
	}.Readiness(context.Background())

	require.False(t, report.Healthy())
	require.Equal(t, StatusDegraded, report.Status)
	require.Equal(t, StatusError, report.Checks["postgres"].Status)
	require.True(t, report.Checks["postgres"].Required)
}

func TestReadinessChecksPostgresPing(t *testing.T) {
	report := Checker{
		Service: &svc.ServiceContext{
			Config: config.Config{
				Postgres: config.PostgresConf{DataSource: "postgres://example"},
			},
			DBConn: &readinessSqlConn{},
		},
		Now: fixedNow,
	}.Readiness(context.Background())

	require.True(t, report.Healthy())
	require.Equal(t, StatusOK, report.Checks["postgres"].Status)
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
}

type readinessSqlConn struct{}

func (c *readinessSqlConn) Exec(string, ...any) (sql.Result, error) {
	return nil, errReadinessUnsupported
}

func (c *readinessSqlConn) ExecCtx(context.Context, string, ...any) (sql.Result, error) {
	return nil, errReadinessUnsupported
}

func (c *readinessSqlConn) Prepare(string) (sqlx.StmtSession, error) {
	return nil, errReadinessUnsupported
}

func (c *readinessSqlConn) PrepareCtx(context.Context, string) (sqlx.StmtSession, error) {
	return nil, errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRow(v any, query string, args ...any) error {
	return c.QueryRowCtx(context.Background(), v, query, args...)
}

func (c *readinessSqlConn) QueryRowCtx(_ context.Context, v any, _ string, _ ...any) error {
	out, ok := v.(*int)
	if !ok {
		return errReadinessUnsupported
	}
	*out = 1
	return nil
}

func (c *readinessSqlConn) QueryRowPartial(any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRowPartialCtx(context.Context, any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRows(any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRowsCtx(context.Context, any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRowsPartial(any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) QueryRowsPartialCtx(context.Context, any, string, ...any) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) RawDB() (*sql.DB, error) {
	return nil, nil
}

func (c *readinessSqlConn) Transact(func(sqlx.Session) error) error {
	return errReadinessUnsupported
}

func (c *readinessSqlConn) TransactCtx(context.Context, func(context.Context, sqlx.Session) error) error {
	return errReadinessUnsupported
}

var errReadinessUnsupported = errors.New("unsupported readiness test operation")
