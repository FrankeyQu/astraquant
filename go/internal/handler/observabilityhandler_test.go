package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/config"
	"nof0-api/internal/health"
	persistmetrics "nof0-api/internal/persistence/metrics"
	"nof0-api/internal/svc"
)

func TestHealthzHandlerReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	HealthzHandler(&svc.ServiceContext{})(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var report health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	require.True(t, report.Healthy())
	require.Equal(t, health.StatusOK, report.Checks["process"].Status)
}

func TestReadyzHandlerReturnsServiceUnavailableWhenRequiredDependencyFails(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	svcCtx := &svc.ServiceContext{
		Config: config.Config{
			Postgres: config.PostgresConf{DataSource: "postgres://example"},
		},
	}

	ReadyzHandler(svcCtx)(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	var report health.Report
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	require.False(t, report.Healthy())
	require.Equal(t, health.StatusError, report.Checks["postgres"].Status)
}

func TestMetricsHandlerExposesExpvarMetrics(t *testing.T) {
	persistmetrics.RecordDBWrite("handler_test", persistmetrics.StatusOK, 1, 0)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	MetricsHandler()(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	body := rec.Body.String()
	require.True(t, strings.Contains(body, `"db_writes_total"`) || strings.Contains(body, "db_writes_total"))
	require.Contains(t, body, "handler_test|ok")
}
