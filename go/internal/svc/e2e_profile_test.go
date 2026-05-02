package svc

import (
	"path/filepath"
	"testing"

	"nof0-api/internal/config"
)

func TestE2EProfileBuildsWithoutExternalSecrets(t *testing.T) {
	cfgPath := filepath.Join("..", "..", "etc", "nof0.e2e.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load e2e profile: %v", err)
	}

	ctx := NewServiceContext(*cfg, cfg.MainPath())
	if ctx.ManagerConfig == nil {
		t.Fatal("manager config not loaded")
	}
	if ctx.ExchangeConfig == nil {
		t.Fatal("exchange config not loaded")
	}
	if ctx.ExchangeConfig.Default != "paper_trading" {
		t.Fatalf("default exchange = %q, want paper_trading", ctx.ExchangeConfig.Default)
	}
	if ctx.ExchangeProviders["paper_trading"] == nil {
		t.Fatal("paper_trading exchange provider not built")
	}
	if len(ctx.ManagerConfig.Traders) == 0 {
		t.Fatal("no e2e traders configured")
	}
	for _, trader := range ctx.ManagerConfig.Traders {
		if trader.ExecutionMode != "paper" {
			t.Fatalf("trader %s execution_mode = %q, want paper", trader.ID, trader.ExecutionMode)
		}
		if trader.ExchangeProvider != "paper_trading" {
			t.Fatalf("trader %s exchange_provider = %q, want paper_trading", trader.ID, trader.ExchangeProvider)
		}
		if trader.AutoStart {
			t.Fatalf("trader %s auto_start = true, want false for e2e profile", trader.ID)
		}
	}
}
