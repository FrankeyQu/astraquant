package handler

import (
	"expvar"
	"net/http"
	"time"

	"github.com/zeromicro/go-zero/rest/httpx"

	"nof0-api/internal/health"
	"nof0-api/internal/svc"
)

const healthCheckTimeout = 2 * time.Second

func MetricsHandler() http.HandlerFunc {
	return expvar.Handler().ServeHTTP
}

func HealthzHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := health.Checker{
			Service: svcCtx,
			Timeout: healthCheckTimeout,
		}.Liveness()
		httpx.WriteJsonCtx(r.Context(), w, http.StatusOK, report)
	}
}

func ReadyzHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := health.Checker{
			Service: svcCtx,
			Timeout: healthCheckTimeout,
		}.Readiness(r.Context())

		statusCode := http.StatusOK
		if !report.Healthy() {
			statusCode = http.StatusServiceUnavailable
		}
		httpx.WriteJsonCtx(r.Context(), w, statusCode, report)
	}
}
