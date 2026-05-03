package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordPersistenceMetrics(t *testing.T) {
	before := Snapshot()
	startWrites := before["db_writes_total"]["price_ticks|ok"]
	startLatency := before["persistence_latency_seconds"]["price_ticks|ok"]
	startCache := before["cache_ops_total"]["market_cache|ok"]
	startIssues := before["inconsistency_counters_total"]["cache|hyperliquid|btc"]

	RecordDBWrite("price_ticks", StatusOK, 3, 1500*time.Millisecond)
	RecordCacheOp("market cache", StatusOK, 2)
	RecordInconsistency("cache", "hyperliquid", "BTC", 1)

	after := Snapshot()
	require.Equal(t, startWrites+3, after["db_writes_total"]["price_ticks|ok"])
	require.InDelta(t, startLatency+1.5, after["persistence_latency_seconds"]["price_ticks|ok"], 0.0001)
	require.Equal(t, startCache+2, after["cache_ops_total"]["market_cache|ok"])
	require.Equal(t, startIssues+1, after["inconsistency_counters_total"]["cache|hyperliquid|btc"])
}
