package commandworker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/config"
	"nof0-api/internal/svc"
)

func TestStartDisabledReturnsNil(t *testing.T) {
	runtime, err := Start(context.Background(), config.CommandWorkerConf{Enabled: false}, &svc.ServiceContext{})

	require.NoError(t, err)
	require.Nil(t, runtime)
}

func TestStartEnabledRequiresCommandRepository(t *testing.T) {
	runtime, err := Start(context.Background(), config.CommandWorkerConf{
		Enabled:   true,
		Interval:  time.Second,
		BatchSize: 1,
	}, &svc.ServiceContext{})

	require.ErrorContains(t, err, "ControlCommandRepo is required")
	require.Nil(t, runtime)
}

func TestConfigValidatesCommandWorker(t *testing.T) {
	cfg := config.Config{
		Env:      "test",
		DataPath: "../../mcp/data",
		TTL:      config.CacheTTL{Short: 10, Medium: 60, Long: 300},
		CommandWorker: config.CommandWorkerConf{
			Enabled:   true,
			Interval:  0,
			BatchSize: 10,
		},
	}

	err := cfg.Validate()

	require.ErrorContains(t, err, "commandWorker.interval")
}
