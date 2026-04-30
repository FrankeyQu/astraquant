package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvStoreLookupPrefersScopedSecret(t *testing.T) {
	t.Setenv("ASTRAQUANT_HYPERLIQUID_PRIVATE_KEY", "global")
	t.Setenv("ASTRAQUANT_SECRET_TRADER_A_SESSION_1_HYPERLIQUID_PRIVATE_KEY", "scoped")

	store := NewEnvStore()
	secret, err := store.Lookup(context.Background(), Key{
		TraderID:  "trader-a",
		SessionID: "session-1",
		Provider:  "hyperliquid",
		Name:      "private_key",
	})
	require.NoError(t, err)
	assert.Equal(t, "scoped", secret.Value)
	assert.Equal(t, "ASTRAQUANT_SECRET_TRADER_A_SESSION_1_HYPERLIQUID_PRIVATE_KEY", secret.Source)
}

func TestEnvStoreLookupSupportsProviderFallback(t *testing.T) {
	t.Setenv("ASTRAQUANT_HYPERLIQUID_PRIVATE_KEY", "provider-secret")

	store := NewEnvStore()
	secret, err := store.Lookup(context.Background(), Key{Provider: "hyperliquid", Name: "private_key"})
	require.NoError(t, err)
	assert.Equal(t, "provider-secret", secret.Value)
}

func TestEnvStoreLookupReturnsNotFoundWithoutValue(t *testing.T) {
	store := NewEnvStore()
	_, err := store.Lookup(context.Background(), Key{Provider: "hyperliquid", Name: "private_key"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
	assert.NotContains(t, err.Error(), "PRIVATE_KEY=")
}

func TestRedactHelpers(t *testing.T) {
	secret := "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a741b52d7c5d5095e2f"

	assert.Equal(t, "0x59[REDACTED]5e2f", Redact(secret))
	assert.Equal(t, "failed with 0x59[REDACTED]5e2f", RedactText("failed with "+secret, secret))
	assert.EqualError(t, RedactError(assert.AnError, secret), "assert.AnError general error for testing")
}
