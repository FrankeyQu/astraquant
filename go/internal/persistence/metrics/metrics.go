package metrics

import (
	"expvar"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	StatusOK    = "ok"
	StatusError = "error"
	StatusSkip  = "skip"
	StatusHit   = "hit"
	StatusMiss  = "miss"
)

var (
	dbWritesTotal             = expvar.NewMap("db_writes_total")
	persistenceLatencySeconds = expvar.NewMap("persistence_latency_seconds")
	cacheOpsTotal             = expvar.NewMap("cache_ops_total")
	inconsistencyCounters     = expvar.NewMap("inconsistency_counters_total")

	mu sync.Mutex
)

// RecordDBWrite records DB write count and latency. Rows should be the affected
// logical rows, not necessarily RowsAffected from the driver.
func RecordDBWrite(operation, status string, rows int, latency time.Duration) {
	if rows <= 0 {
		rows = 1
	}
	key := metricKey(operation, status)
	dbWritesTotal.Add(key, int64(rows))
	persistenceLatencySeconds.AddFloat(key, latency.Seconds())
}

// RecordCacheOp records cache operation counts.
func RecordCacheOp(operation, status string, count int) {
	if count <= 0 {
		count = 1
	}
	cacheOpsTotal.Add(metricKey(operation, status), int64(count))
}

// RecordInconsistency records consistency checker issue counters.
func RecordInconsistency(scope, provider, symbol string, count int) {
	if count <= 0 {
		count = 1
	}
	inconsistencyCounters.Add(metricKey(scope, provider, symbol), int64(count))
}

// Snapshot returns current metric values for tests and diagnostics.
func Snapshot() map[string]map[string]float64 {
	mu.Lock()
	defer mu.Unlock()
	return map[string]map[string]float64{
		"db_writes_total":              mapValues(dbWritesTotal),
		"persistence_latency_seconds":  mapValues(persistenceLatencySeconds),
		"cache_ops_total":              mapValues(cacheOpsTotal),
		"inconsistency_counters_total": mapValues(inconsistencyCounters),
	}
}

func metricKey(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		part = strings.NewReplacer(" ", "_", "|", "_", ":", "_", "/", "_", "\\", "_").Replace(part)
		if part == "" {
			part = "unknown"
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "|")
}

func mapValues(m *expvar.Map) map[string]float64 {
	out := make(map[string]float64)
	m.Do(func(kv expvar.KeyValue) {
		if kv.Value == nil {
			return
		}
		var value float64
		if _, err := fmt.Sscan(kv.Value.String(), &value); err != nil {
			return
		}
		out[kv.Key] = value
	})
	return sortedMap(out)
}

func sortedMap(in map[string]float64) map[string]float64 {
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]float64, len(in))
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}
