package main

import (
	"encoding/json"
	"testing"

	"nof0-api/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeDetailJSONContainsNestedAndFlatFields(t *testing.T) {
	detail := tradeDetailJSON(&types.Trade{
		Id:                     "trade-1",
		ModelId:                "gpt-5",
		Symbol:                 "btc",
		Side:                   "short",
		TradeType:              "short",
		TradeId:                "external-trade-1",
		Quantity:               -0.25,
		Leverage:               10,
		Confidence:             0.72,
		EntryPrice:             100000,
		ExitPrice:              99000,
		EntryTime:              1760000000,
		ExitTime:               1760003600,
		RealizedGrossPnl:       250,
		RealizedNetPnl:         245,
		TotalCommissionDollars: 5,
	}, 1760000000000, 1760003600000)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(detail), &payload))
	assert.Equal(t, "gpt-5", payload["model_id"])
	assert.Equal(t, "BTC", payload["symbol"])
	assert.Equal(t, "short", payload["side"])
	quantityPayload := payload["quantity"].(map[string]any)
	assert.Equal(t, 0.25, quantityPayload["total"])

	timePayload := payload["time"].(map[string]any)
	assert.Equal(t, float64(1760000000000), timePayload["open_ts_ms"])
	assert.Equal(t, float64(1760003600000), timePayload["close_ts_ms"])

	pnlPayload := payload["pnl"].(map[string]any)
	assert.Equal(t, 245.0, pnlPayload["net"])
}

func TestPositionDetailJSONPreservesSignedSideSeparately(t *testing.T) {
	detail := positionDetailJSON(positionView{
		EntryPrice:    100000,
		Quantity:      -0.2,
		Leverage:      10,
		Confidence:    0.67,
		RiskUsd:       250,
		UnrealizedPnl: 15,
	}, 1760000000000)

	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(detail), &payload))

	entry := payload["entry"].(map[string]any)
	assert.Equal(t, 100000.0, entry["price"])
	assert.Equal(t, 0.2, entry["quantity"])
	assert.Equal(t, 10.0, entry["leverage"])

	risk := payload["risk"].(map[string]any)
	assert.Equal(t, 0.67, risk["confidence"])
	assert.Equal(t, 250.0, risk["risk_usd"])

	metrics := payload["metrics"].(map[string]any)
	assert.Equal(t, 15.0, metrics["unrealized_pnl"])
}

func TestNormalizeSideFallsBackToQuantity(t *testing.T) {
	assert.Equal(t, "short", normalizeSide("", -1))
	assert.Equal(t, "long", normalizeSide("", 1))
	assert.Equal(t, "short", normalizeSide("short", 1))
	assert.Equal(t, "long", normalizeSide("long", -1))
}
