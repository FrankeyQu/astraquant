package sim

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"nof0-api/pkg/exchange"
)

func TestSimProvider_ChargesFeesAndMarksUnrealizedPnL(t *testing.T) {
	p := New()
	ctx := context.Background()

	asset, err := p.GetAssetIndex(ctx, "BTC")
	assert.NoError(t, err)

	resp, err := p.PlaceOrder(ctx, exchange.Order{
		Asset:   asset,
		IsBuy:   true,
		LimitPx: "100",
		Sz:      "1",
	})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), resp.Response.Data.Statuses[0].Filled.Oid)

	value, err := p.GetAccountValue(ctx)
	assert.NoError(t, err)
	assert.InDelta(t, 99999.96, value, 1e-9)

	assert.NoError(t, p.SetMarkPrice(ctx, "BTC", 110))
	value, err = p.GetAccountValue(ctx)
	assert.NoError(t, err)
	assert.InDelta(t, 100009.96, value, 1e-9)

	state, err := p.GetAccountState(ctx)
	assert.NoError(t, err)
	assert.Equal(t, "110", state.MarginSummary.TotalNtlPos)
	assert.Equal(t, "110", state.MarginSummary.TotalMarginUsed)
	assert.Equal(t, "10", state.AssetPositions[0].UnrealizedPnl)
}

func TestSimProvider_ClosePositionUsesSlippageAndRealizesPnL(t *testing.T) {
	p := New()
	ctx := context.Background()

	asset, err := p.GetAssetIndex(ctx, "BTC")
	assert.NoError(t, err)

	_, err = p.PlaceOrder(ctx, exchange.Order{
		Asset:   asset,
		IsBuy:   true,
		LimitPx: "100",
		Sz:      "1",
	})
	assert.NoError(t, err)
	assert.NoError(t, p.SetMarkPrice(ctx, "BTC", 110))

	resp, err := p.ClosePosition(ctx, "BTC")
	assert.NoError(t, err)
	if assert.NotNil(t, resp) && assert.NotNil(t, resp.Response.Data.Statuses[0].Filled) {
		assert.Equal(t, int64(2), resp.Response.Data.Statuses[0].Filled.Oid)
		assert.Equal(t, "109.78", resp.Response.Data.Statuses[0].Filled.AvgPx)
	}

	positions, err := p.GetPositions(ctx)
	assert.NoError(t, err)
	assert.Empty(t, positions)

	value, err := p.GetAccountValue(ctx)
	assert.NoError(t, err)
	assert.InDelta(t, 100009.696088, value, 1e-9)
}

func TestSimProvider_RejectsOrdersThatExceedAvailableMargin(t *testing.T) {
	p := New()
	ctx := context.Background()

	asset, err := p.GetAssetIndex(ctx, "BTC")
	assert.NoError(t, err)
	assert.NoError(t, p.UpdateLeverage(ctx, asset, true, 10))

	resp, err := p.PlaceOrder(ctx, exchange.Order{
		Asset:   asset,
		IsBuy:   true,
		LimitPx: "100000",
		Sz:      "20",
	})
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "insufficient margin")

	positions, err := p.GetPositions(ctx)
	assert.NoError(t, err)
	assert.Empty(t, positions)

	value, err := p.GetAccountValue(ctx)
	assert.NoError(t, err)
	assert.Equal(t, defaultInitialEquity, value)
}

func TestSimProvider_ReduceOnlyCannotOpenOrReverse(t *testing.T) {
	p := New()
	ctx := context.Background()

	asset, err := p.GetAssetIndex(ctx, "ETH")
	assert.NoError(t, err)

	resp, err := p.PlaceOrder(ctx, exchange.Order{
		Asset:      asset,
		IsBuy:      false,
		LimitPx:    "1000",
		Sz:         "1",
		ReduceOnly: true,
	})
	assert.NoError(t, err)
	if assert.NotNil(t, resp.Response.Data.Statuses[0].Filled) {
		assert.Equal(t, "0", resp.Response.Data.Statuses[0].Filled.TotalSz)
	}

	_, err = p.PlaceOrder(ctx, exchange.Order{
		Asset:   asset,
		IsBuy:   true,
		LimitPx: "1000",
		Sz:      "1",
	})
	assert.NoError(t, err)

	resp, err = p.PlaceOrder(ctx, exchange.Order{
		Asset:      asset,
		IsBuy:      false,
		LimitPx:    "1000",
		Sz:         "2",
		ReduceOnly: true,
	})
	assert.NoError(t, err)
	if assert.NotNil(t, resp.Response.Data.Statuses[0].Filled) {
		assert.Equal(t, "1", resp.Response.Data.Statuses[0].Filled.TotalSz)
	}

	positions, err := p.GetPositions(ctx)
	assert.NoError(t, err)
	assert.Empty(t, positions)
}
