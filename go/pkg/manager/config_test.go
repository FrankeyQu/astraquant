package manager

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	managerPromptAgg := filepath.Join(dir, "prompts/manager/aggressive_short.tmpl")
	managerPromptCon := filepath.Join(dir, "prompts/manager/conservative_long.tmpl")
	err := os.MkdirAll(filepath.Dir(managerPromptAgg), 0o700)
	assert.NoError(t, err, "mkdir prompts should succeed")

	execPrompt := filepath.Join(dir, "prompts/executor/default_prompt.tmpl")
	err = os.MkdirAll(filepath.Dir(execPrompt), 0o700)
	assert.NoError(t, err, "mkdir executor prompts should succeed")

	err = os.WriteFile(managerPromptAgg, []byte("aggressive short prompt"), 0o600)
	assert.NoError(t, err, "write aggressive prompt should succeed")

	err = os.WriteFile(managerPromptCon, []byte("conservative long prompt"), 0o600)
	assert.NoError(t, err, "write conservative prompt should succeed")

	err = os.WriteFile(execPrompt, []byte("executor prompt"), 0o600)
	assert.NoError(t, err, "write executor prompt should succeed")

	configYAML := `
manager:
  total_equity_usd: 10000
  reserve_equity_pct: 10
  allocation_strategy: performance_based
  rebalance_interval: 2h
  state_storage_backend: file
  state_storage_path: ./state/manager.json

traders:
  - id: trader_a
    name: Aggressive Short
    exchange_provider: " hyperliquid_primary "
    market_provider: " hl_market "
    prompt_template: prompts/manager/aggressive_short.tmpl
    executor_prompt_template: prompts/executor/default_prompt.tmpl
    model: deepseek-chat
    decision_interval: 4m
    allocation_pct: 40
    auto_start: true
    risk_params:
      max_positions: 3
      max_position_size_usd: 500
      max_margin_usage_pct: 60
      max_daily_loss_usd: 250
      max_daily_loss_pct: 5
      allowed_symbols: [" eth ", "BTC", "ETH"]
      major_coin_leverage: 20
      altcoin_leverage: 10
      min_risk_reward_ratio: 3.0
      min_confidence: 75
      stop_loss_enabled: true
      take_profit_enabled: true

  - id: trader_b
    name: Conservative Long
    exchange_provider: hyperliquid_secondary
    market_provider: hl_market
    prompt_template: prompts/manager/conservative_long.tmpl
    executor_prompt_template: prompts/executor/default_prompt.tmpl
    decision_interval: 5m
    allocation_pct: 30
    auto_start: false
    risk_params:
      max_positions: 2
      max_position_size_usd: 300
      max_margin_usage_pct: 50
      major_coin_leverage: 10
      altcoin_leverage: 5
      min_risk_reward_ratio: 2.5
      min_confidence: 80
      stop_loss_enabled: true
      take_profit_enabled: true

monitoring:
  update_interval: 15s
  metrics_exporter: prometheus
`
	path := filepath.Join(dir, "manager.yaml")
	err = os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write manager config should succeed")

	cfg, err := LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should not error")
	assert.NotNil(t, cfg, "config should not be nil")

	assert.Equal(t, "2h0m0s", cfg.Manager.RebalanceInterval.String(), "RebalanceInterval should be parsed correctly")
	assert.Equal(t, "4m0s", cfg.Traders[0].DecisionInterval.String(), "DecisionInterval should be parsed correctly")
	assert.Equal(t, "hyperliquid_primary", cfg.Traders[0].ExchangeProvider, "ExchangeProvider should be trimmed")
	assert.Equal(t, "hl_market", cfg.Traders[0].MarketProvider, "MarketProvider should be trimmed")
	assert.Equal(t, ExecutionModePaper, cfg.Traders[0].ExecutionMode, "ExecutionMode should default to paper for non-testnet providers")
	assert.Equal(t, OrderStyleLimitIOC, cfg.Traders[0].OrderStyle, "OrderStyle should default to limit_ioc")
	assert.Equal(t, defaultMarketIOCSlippageBps, cfg.Traders[0].MarketIOCSlippageBps, "MarketIOCSlippageBps should default")
	assert.Equal(t, []string{"BTC", "ETH"}, cfg.Traders[0].RiskParams.AllowedSymbols, "AllowedSymbols should be normalized and deduplicated")
	assert.Equal(t, 250.0, cfg.Traders[0].RiskParams.MaxDailyLossUSD, "MaxDailyLossUSD should load")
	assert.Equal(t, 5.0, cfg.Traders[0].RiskParams.MaxDailyLossPct, "MaxDailyLossPct should load")

	wantStatePath := filepath.Join(dir, "state/manager.json")
	assert.Equal(t, wantStatePath, cfg.Manager.StateStoragePath, "StateStoragePath should match expected path")
}

func TestAllocationValidation(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.tmpl")
	err := os.WriteFile(promptPath, []byte("generic prompt"), 0o600)
	assert.NoError(t, err, "write prompt should succeed")

	execPrompt := filepath.Join(dir, "prompts/executor/default_prompt.tmpl")
	err = os.MkdirAll(filepath.Dir(execPrompt), 0o700)
	assert.NoError(t, err, "mkdir executor prompts should succeed")
	err = os.WriteFile(execPrompt, []byte("executor prompt"), 0o600)
	assert.NoError(t, err, "write executor prompt should succeed")

	configYAML := `
manager:
  total_equity_usd: 1000
  reserve_equity_pct: 0
  allocation_strategy: equal
  rebalance_interval: 1h
  state_storage_backend: file
  state_storage_path: state.json

traders:
  - id: t1
    name: Trader1
    exchange_provider: ex
    market_provider: market_a
    prompt_template: prompt.tmpl
    executor_prompt_template: prompts/executor/default_prompt.tmpl
    decision_interval: 3m
    allocation_pct: 80
    auto_start: true
    risk_params:
      max_positions: 1
      max_position_size_usd: 100
      max_margin_usage_pct: 50
      major_coin_leverage: 10
      altcoin_leverage: 5
      min_risk_reward_ratio: 2
      min_confidence: 70
      stop_loss_enabled: true
      take_profit_enabled: true

  - id: t2
    name: Trader2
    exchange_provider: ex
    market_provider: market_a
    prompt_template: prompt.tmpl
    executor_prompt_template: prompts/executor/default_prompt.tmpl
    decision_interval: 3m
    allocation_pct: 30
    auto_start: true
    risk_params:
      max_positions: 1
      max_position_size_usd: 100
      max_margin_usage_pct: 50
      major_coin_leverage: 10
      altcoin_leverage: 5
      min_risk_reward_ratio: 2
      min_confidence: 70
      stop_loss_enabled: true
      take_profit_enabled: true

monitoring:
  update_interval: 10s
  metrics_exporter: prometheus
`
	path := filepath.Join(dir, "bad.yaml")
	err = os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	_, err = LoadConfig(path)
	assert.Error(t, err, "LoadConfig should error for invalid allocation")
	assert.Contains(t, err.Error(), "allocation", "error should mention allocation")
}

func TestLoadConfigMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	execPrompt := filepath.Join(dir, "prompts/executor/default_prompt.tmpl")
	err := os.MkdirAll(filepath.Dir(execPrompt), 0o700)
	assert.NoError(t, err, "mkdir executor prompts should succeed")
	err = os.WriteFile(execPrompt, []byte("executor prompt"), 0o600)
	assert.NoError(t, err, "write executor prompt should succeed")
	configYAML := `
manager:
  total_equity_usd: 1000
  reserve_equity_pct: 0
  allocation_strategy: equal
  rebalance_interval: 1h
  state_storage_backend: file
  state_storage_path: state.json

traders:
  - id: t1
    name: Trader1
    exchange_provider: ex
    market_provider: market_a
    prompt_template: missing.tmpl
    executor_prompt_template: prompts/executor/default_prompt.tmpl
    decision_interval: 3m
    allocation_pct: 50
    auto_start: true
    risk_params:
      max_positions: 1
      max_position_size_usd: 100
      max_margin_usage_pct: 50
      major_coin_leverage: 10
      altcoin_leverage: 5
      min_risk_reward_ratio: 2
      min_confidence: 70
      stop_loss_enabled: true
      take_profit_enabled: true

monitoring:
  update_interval: 10s
  metrics_exporter: prometheus
`
	path := filepath.Join(dir, "bad.yaml")
	err = os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	_, err = LoadConfig(path)
	assert.Error(t, err, "LoadConfig should error for missing prompt template")
	assert.Contains(t, err.Error(), "prompt_template", "error should mention prompt_template")
}

func TestMissingMarketProvider(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "prompt.tmpl")
	err := os.WriteFile(promptPath, []byte("generic prompt"), 0o600)
	assert.NoError(t, err, "write prompt should succeed")

	configYAML := `
manager:
  total_equity_usd: 1000
  reserve_equity_pct: 0
  allocation_strategy: equal
  rebalance_interval: 1h
  state_storage_backend: file
  state_storage_path: state.json

traders:
  - id: t1
    name: Trader1
    exchange_provider: ex
    prompt_template: prompt.tmpl
    decision_interval: 3m
    allocation_pct: 50
    auto_start: true
    risk_params:
      max_positions: 1
      max_position_size_usd: 100
      max_margin_usage_pct: 50
      major_coin_leverage: 10
      altcoin_leverage: 5
      min_risk_reward_ratio: 2
      min_confidence: 70
      stop_loss_enabled: true
      take_profit_enabled: true

monitoring:
  update_interval: 10s
  metrics_exporter: prometheus
`
	path := filepath.Join(dir, "bad.yaml")
	err = os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	_, err = LoadConfig(path)
	assert.Error(t, err, "LoadConfig should error for missing market provider")
	assert.Contains(t, err.Error(), "market_provider", "error should mention market_provider")
}
