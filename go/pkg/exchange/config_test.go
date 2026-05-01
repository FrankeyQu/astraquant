package exchange_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	exchange "nof0-api/pkg/exchange"
	_ "nof0-api/pkg/exchange/hyperliquid"

	"github.com/stretchr/testify/assert"
)

const testPrivateKey = "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a741b52d7c5d5095e2f"

func TestLoadConfigAndBuildProviders(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ASTRAQUANT_HYPERLIQUID_PRIVATE_KEY", testPrivateKey)

	configYAML := `
default: hyperliquid_testnet
providers:
  hyperliquid_testnet:
    type: hyperliquid
    timeout: 45s
    testnet: true
    vault_address: 0x0000000000000000000000000000000000000000
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should not error")
	assert.NotNil(t, cfg, "config should not be nil")
	assert.Equal(t, "hyperliquid_testnet", cfg.Default, "default should be hyperliquid_testnet")

	providers, err := cfg.BuildProviders()
	assert.NoError(t, err, "BuildProviders should not error")
	assert.Len(t, providers, 1, "should have 1 provider")
	assert.Contains(t, providers, "hyperliquid_testnet", "provider map should contain hyperliquid_testnet")
}

func TestBuildProvidersSupportsScopedSecretLookup(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ASTRAQUANT_SECRET_TRADER_A_SESSION_1_HYPERLIQUID_PRIVATE_KEY", testPrivateKey)
	t.Setenv("ASTRAQUANT_HYPERLIQUID_PRIVATE_KEY", "not-a-private-key")

	configYAML := `
default: hyperliquid_testnet
providers:
  hyperliquid_testnet:
    type: hyperliquid
    timeout: 45s
    testnet: true
    credentials:
      trader_id: trader-a
      session_id: session-1
      provider: hyperliquid
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should not error")
	assert.Empty(t, cfg.Providers["hyperliquid_testnet"].PrivateKey, "config should not retain secret material")

	providers, err := cfg.BuildProviders()
	assert.NoError(t, err, "BuildProviders should use scoped secret")
	assert.Contains(t, providers, "hyperliquid_testnet")
}

func TestBuildProvidersResolvesEnvReferencePrivateKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_EXCHANGE_PRIVATE_KEY", testPrivateKey)

	configYAML := `
default: hyperliquid_testnet
providers:
  hyperliquid_testnet:
    type: hyperliquid
    private_key: ${TEST_EXCHANGE_PRIVATE_KEY}
    testnet: true
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should not error")
	assert.Equal(t, "${TEST_EXCHANGE_PRIVATE_KEY}", cfg.Providers["hyperliquid_testnet"].PrivateKey, "config should keep env reference instead of expanded secret")

	providers, err := cfg.BuildProviders()
	assert.NoError(t, err, "BuildProviders should resolve env reference")
	assert.Contains(t, providers, "hyperliquid_testnet")
	assert.Equal(t, "${TEST_EXCHANGE_PRIVATE_KEY}", cfg.Providers["hyperliquid_testnet"].PrivateKey, "BuildProviders should not mutate config with secret material")
}

func TestBuildProvidersRequiresPrivateKey(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
providers:
  hyperliquid_testnet:
    type: hyperliquid
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should allow secrets to resolve at build time")

	_, err = cfg.BuildProviders()
	assert.Error(t, err, "BuildProviders should error for missing private_key")
	assert.Contains(t, err.Error(), "private_key", "error should mention private_key")
	assert.NotContains(t, err.Error(), testPrivateKey, "error should not dump secret values")
}

func TestBuildProvidersMissingEnvReferenceDoesNotLeakSecretValue(t *testing.T) {
	dir := t.TempDir()
	configYAML := `
providers:
  hyperliquid_testnet:
    type: hyperliquid
    private_key: ${MISSING_EXCHANGE_PRIVATE_KEY}
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should allow secrets to resolve at build time")

	_, err = cfg.BuildProviders()
	assert.Error(t, err, "BuildProviders should error for missing env reference")
	assert.Contains(t, err.Error(), "private_key", "error should mention the missing secret kind")
	assert.NotContains(t, err.Error(), "MISSING_EXCHANGE_PRIVATE_KEY=", "error should not dump env assignment text")
}

func TestBuildProvidersRedactsBuilderErrors(t *testing.T) {
	const rawSecret = "builder-secret-value-1234567890"
	exchange.RegisterProvider("redacttest", func(name string, cfg *exchange.ProviderConfig) (exchange.Provider, error) {
		return nil, fmt.Errorf("builder saw %s", cfg.PrivateKey)
	})

	dir := t.TempDir()
	configYAML := `
providers:
  failing:
    type: redacttest
    private_key: builder-secret-value-1234567890
`
	path := filepath.Join(dir, "exchange.yaml")
	err := os.WriteFile(path, []byte(configYAML), 0o600)
	assert.NoError(t, err, "write config should succeed")

	cfg, err := exchange.LoadConfig(path)
	assert.NoError(t, err, "LoadConfig should not error")

	_, err = cfg.BuildProviders()
	assert.Error(t, err, "BuildProviders should return builder error")
	assert.NotContains(t, err.Error(), rawSecret, "error should not include raw secret")
	assert.Contains(t, err.Error(), "buil[REDACTED]7890", "error should include redacted secret marker")
}
