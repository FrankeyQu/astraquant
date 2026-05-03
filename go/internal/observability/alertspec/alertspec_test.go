package alertspec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type alertRulesFile struct {
	Version int `yaml:"version"`
	Source  struct {
		LivenessEndpoint  string `yaml:"liveness_endpoint"`
		ReadinessEndpoint string `yaml:"readiness_endpoint"`
		MetricsEndpoint   string `yaml:"metrics_endpoint"`
		ScrapeInterval    string `yaml:"scrape_interval"`
	} `yaml:"source"`
	Alerts []alertRule `yaml:"alerts"`
}

type alertRule struct {
	ID        string `yaml:"id"`
	Severity  string `yaml:"severity"`
	Signal    string `yaml:"signal"`
	Condition string `yaml:"condition"`
	Summary   string `yaml:"summary"`
	Runbook   string `yaml:"runbook"`
}

type scrapeExampleFile struct {
	Version    int `yaml:"version"`
	ScrapeJobs []struct {
		Name       string   `yaml:"name"`
		BaseURL    string   `yaml:"base_url"`
		Interval   string   `yaml:"interval"`
		Timeout    string   `yaml:"timeout"`
		ExpvarMaps []string `yaml:"expvar_maps"`
	} `yaml:"scrape_jobs"`
}

func TestAlertRulesAssetIsValid(t *testing.T) {
	var rules alertRulesFile
	readYAML(t, "deploy/observability/alert-rules.yaml", &rules)

	require.Equal(t, 1, rules.Version)
	require.Equal(t, "/healthz", rules.Source.LivenessEndpoint)
	require.Equal(t, "/readyz", rules.Source.ReadinessEndpoint)
	require.Equal(t, "/debug/vars", rules.Source.MetricsEndpoint)
	require.NotEmpty(t, rules.Source.ScrapeInterval)
	require.GreaterOrEqual(t, len(rules.Alerts), 8)

	requiredIDs := map[string]bool{
		"nof0_api_down":                         false,
		"nof0_api_not_ready":                    false,
		"nof0_db_write_errors":                  false,
		"nof0_persistence_avg_latency_high":     false,
		"nof0_cache_errors":                     false,
		"nof0_consistency_cache_hit_ratio_low":  false,
		"nof0_market_inconsistencies":           false,
		"nof0_market_inconsistencies_sustained": false,
	}
	for _, alert := range rules.Alerts {
		require.NotEmpty(t, alert.ID)
		require.Contains(t, []string{"warning", "critical"}, alert.Severity)
		require.NotEmpty(t, alert.Signal)
		require.NotEmpty(t, alert.Condition)
		require.NotEmpty(t, alert.Summary)
		require.NotEmpty(t, alert.Runbook)
		if _, ok := requiredIDs[alert.ID]; ok {
			requiredIDs[alert.ID] = true
		}
	}
	for id, seen := range requiredIDs {
		require.Truef(t, seen, "missing required alert %s", id)
	}
}

func TestExpvarScrapeExampleIsValid(t *testing.T) {
	var cfg scrapeExampleFile
	readYAML(t, "deploy/observability/expvar-scrape.example.yaml", &cfg)

	require.Equal(t, 1, cfg.Version)
	require.Len(t, cfg.ScrapeJobs, 1)
	job := cfg.ScrapeJobs[0]
	require.NotEmpty(t, job.Name)
	require.NotEmpty(t, job.BaseURL)
	require.NotEmpty(t, job.Interval)
	require.NotEmpty(t, job.Timeout)
	require.ElementsMatch(t, []string{
		"db_writes_total",
		"persistence_latency_seconds",
		"cache_ops_total",
		"inconsistency_counters_total",
	}, job.ExpvarMaps)
}

func readYAML(t *testing.T, relativePath string, out any) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(moduleRoot, relativePath))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(data, out))
}
