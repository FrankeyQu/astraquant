package svc

import (
	"testing"

	"nof0-api/internal/config"
)

func TestNewServiceContextWithoutDatabaseLeavesPersistenceUnavailable(t *testing.T) {
	cfg := config.Config{
		Env:      "test",
		DataPath: "../../mcp/data",
		TTL:      config.CacheTTL{Short: 10, Medium: 60, Long: 300},
	}

	svcCtx := NewServiceContext(cfg, "")

	if svcCtx.DBConn != nil {
		t.Fatal("expected DBConn to be nil without postgres datasource")
	}
	if svcCtx.AuditEventsModel != nil || svcCtx.AuditEventRepo != nil {
		t.Fatal("expected audit persistence dependencies to stay nil without DB")
	}
	if svcCtx.ManagerPersistenceService != nil {
		t.Fatal("expected manager persistence service to stay nil without DB")
	}
}
