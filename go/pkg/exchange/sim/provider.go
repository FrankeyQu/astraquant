package sim

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"nof0-api/pkg/exchange"
)

const (
	defaultInitialEquity  = 100000.0
	defaultFallbackPrice  = 100.0
	defaultTakerFeeRate   = 0.0004
	defaultMarketSlippage = 0.002
)

// Provider is a paper-trading exchange implementation that keeps positions,
// equity and risk metrics in-memory.
type Provider struct {
	mu sync.Mutex

	nextAssetID int
	nextOID     int64

	assetIndex  map[string]int // symbol -> asset id
	assetSymbol map[int]string // asset id -> symbol
	leverage    map[int]exchange.Leverage

	markPx    map[string]float64 // latest mark price per symbol
	positions map[string]*positionState

	initialEquity float64
	cash          float64
}

type positionState struct {
	Coin  string
	Qty   float64 // positive long, negative short
	Entry float64 // average entry price
}

// New constructs a new simulator instance with default equity.
func New() *Provider {
	return &Provider{
		nextAssetID:   1,
		nextOID:       1,
		assetIndex:    make(map[string]int),
		assetSymbol:   make(map[int]string),
		leverage:      make(map[int]exchange.Leverage),
		markPx:        make(map[string]float64),
		positions:     make(map[string]*positionState),
		initialEquity: defaultInitialEquity,
		cash:          defaultInitialEquity,
	}
}

func canonical(coin string) string { return strings.ToUpper(strings.TrimSpace(coin)) }

// GetAssetIndex resolves a stable asset identifier for the provided coin.
func (p *Provider) GetAssetIndex(ctx context.Context, coin string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	c := canonical(coin)
	if id, ok := p.assetIndex[c]; ok {
		return id, nil
	}
	id := p.nextAssetID
	p.nextAssetID++
	p.assetIndex[c] = id
	p.assetSymbol[id] = c
	return id, nil
}

// SetMarkPrice updates the reference price used for unrealised PnL and IOC fills.
func (p *Provider) SetMarkPrice(ctx context.Context, coin string, price float64) error {
	if price <= 0 {
		return fmt.Errorf("sim: mark price must be positive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.markPx[canonical(coin)] = price
	return nil
}

// PlaceOrder synchronously fills an IOC-like order at the provided limit price.
func (p *Provider) PlaceOrder(ctx context.Context, order exchange.Order) (*exchange.OrderResponse, error) {
	if order.Sz == "" {
		return nil, fmt.Errorf("sim: order size is required")
	}
	price, err := strconv.ParseFloat(strings.TrimSpace(order.LimitPx), 64)
	if err != nil || price <= 0 {
		return nil, fmt.Errorf("sim: invalid limit price %q", order.LimitPx)
	}
	size, err := strconv.ParseFloat(strings.TrimSpace(order.Sz), 64)
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("sim: invalid size %q", order.Sz)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	coin := ""
	for sym, id := range p.assetIndex {
		if id == order.Asset {
			coin = sym
			break
		}
	}
	if coin == "" {
		return nil, fmt.Errorf("sim: unknown asset index %d", order.Asset)
	}

	_, filled, _, err := p.executeOrderLocked(coin, price, size, order.IsBuy, order.ReduceOnly, p.feeRateForOrder(order))
	if err != nil {
		return nil, err
	}
	if filled > 0 {
		p.markPx[coin] = price
	}
	oid := p.nextOrderIDLocked()

	resp := &exchange.OrderResponse{
		Status: "ok",
		Response: exchange.OrderResponseData{
			Type: "order",
			Data: exchange.OrderResponseDataDetail{
				Statuses: []exchange.OrderStatusResponse{{
					Filled: &exchange.FilledOrder{
						TotalSz: formatDecimal(filled),
						AvgPx:   formatDecimal(price),
						Oid:     oid,
					},
				}},
			},
		},
	}
	return resp, nil
}

func (p *Provider) executeOrderLocked(coin string, price, size float64, isBuy, reduceOnly bool, feeRate float64) (float64, float64, float64, error) {
	oldCash := p.cash
	oldState := clonePosition(p.positions[coin])
	existed := oldState != nil

	realized, filled, err := p.applyOrderLocked(coin, price, size, isBuy, reduceOnly)
	if err != nil {
		return 0, 0, 0, err
	}
	if filled == 0 {
		return realized, filled, 0, nil
	}

	fee := math.Abs(filled * price * feeRate)
	p.cash += realized - fee

	_, unrealized, _, margin := p.buildAccountSnapshotLocked()
	equity := p.cash + unrealized
	if margin-equity > 1e-9 {
		p.cash = oldCash
		if existed {
			p.positions[coin] = oldState
		} else {
			delete(p.positions, coin)
		}
		return 0, 0, 0, fmt.Errorf("sim: insufficient margin")
	}
	return realized, filled, fee, nil
}

func (p *Provider) applyOrderLocked(coin string, price, size float64, isBuy, reduceOnly bool) (float64, float64, error) {
	if price <= 0 {
		return 0, 0, fmt.Errorf("sim: price must be positive")
	}
	if size <= 0 {
		return 0, 0, fmt.Errorf("sim: size must be positive")
	}

	state := p.positions[coin]
	if reduceOnly {
		if state == nil || state.Qty == 0 {
			return 0, 0, nil
		}
	} else if state == nil {
		state = &positionState{Coin: coin}
		p.positions[coin] = state
	}

	execSize := size
	delta := execSize
	if !isBuy {
		delta = -execSize
	}

	if reduceOnly {
		if state.Qty*delta > 0 {
			return 0, 0, fmt.Errorf("sim: reduce-only order would increase position")
		}
		maxQty := math.Abs(state.Qty)
		if execSize > maxQty {
			execSize = maxQty
		}
		if execSize <= 0 {
			return 0, 0, nil
		}
		delta = execSize
		if !isBuy {
			delta = -execSize
		}
	}

	oldQty := state.Qty
	newQty := oldQty + delta

	realized := 0.0
	if oldQty != 0 && oldQty*delta < 0 {
		closeQty := math.Min(math.Abs(oldQty), math.Abs(delta))
		dir := 1.0
		if oldQty < 0 {
			dir = -1.0
		}
		realized = closeQty * (price - state.Entry) * dir
	}

	switch {
	case oldQty == 0:
		state.Entry = price
	case oldQty*delta > 0:
		state.Entry = ((oldQty * state.Entry) + (delta * price)) / newQty
	case oldQty*delta < 0:
		if newQty == 0 {
			state.Entry = price
		} else if oldQty*newQty < 0 {
			state.Entry = price
		}
	}

	state.Qty = newQty
	if math.Abs(state.Qty) < 1e-10 {
		state.Qty = 0
	}
	if state.Qty == 0 {
		state.Entry = 0
		delete(p.positions, coin)
	}
	return realized, math.Abs(delta), nil
}

// CancelOrder is a no-op for the simulator (orders fill immediately).
func (p *Provider) CancelOrder(ctx context.Context, asset int, oid int64) error { return nil }

// GetOpenOrders always returns an empty slice because fills are synchronous.
func (p *Provider) GetOpenOrders(ctx context.Context) ([]exchange.OrderStatus, error) {
	return nil, nil
}

// IOCMarket emulates a market IOC order around the latest mark price.
func (p *Provider) IOCMarket(ctx context.Context, coin string, isBuy bool, qty float64, slippage float64, reduceOnly bool) (*exchange.OrderResponse, error) {
	if qty <= 0 {
		return nil, fmt.Errorf("sim: IOCMarket qty must be positive")
	}
	p.mu.Lock()
	price := p.resolveMarkPriceLocked(canonical(coin))
	if price <= 0 {
		price = defaultFallbackPrice
	}
	if slippage <= 0 {
		slippage = defaultMarketSlippage
	}
	if isBuy {
		price *= 1 + slippage
	} else {
		price *= math.Max(0, 1-slippage)
	}
	p.mu.Unlock()

	order := exchange.Order{
		Asset:      0, // resolved in PlaceOrder
		IsBuy:      isBuy,
		LimitPx:    formatDecimal(price),
		Sz:         formatDecimal(qty),
		ReduceOnly: reduceOnly,
		OrderType:  exchange.OrderType{Limit: &exchange.LimitOrderType{TIF: "Ioc"}},
	}
	assetIdx, err := p.GetAssetIndex(ctx, coin)
	if err != nil {
		return nil, err
	}
	order.Asset = assetIdx
	return p.PlaceOrder(ctx, order)
}

// GetPositions returns the current open positions with mark-to-market values.
func (p *Provider) GetPositions(ctx context.Context) ([]exchange.Position, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	positions, _, _, _ := p.buildAccountSnapshotLocked()
	return positions, nil
}

// ClosePosition fully closes the position for the given coin at the latest mark price.
func (p *Provider) ClosePosition(ctx context.Context, coin string) (*exchange.OrderResponse, error) {
	c := canonical(coin)
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.positions[c]
	if state == nil || state.Qty == 0 {
		return nil, nil
	}
	price := p.resolveMarkPriceLocked(c)
	if price <= 0 {
		price = state.Entry
	}
	size := math.Abs(state.Qty)
	isBuy := state.Qty < 0
	price = applySlippage(price, isBuy, defaultMarketSlippage)
	_, filled, _, err := p.executeOrderLocked(c, price, size, isBuy, true, defaultTakerFeeRate)
	if err != nil {
		return nil, err
	}
	if filled > 0 {
		p.markPx[c] = price
	}
	oid := p.nextOrderIDLocked()
	resp := &exchange.OrderResponse{
		Status: "ok",
		Response: exchange.OrderResponseData{
			Type: "order",
			Data: exchange.OrderResponseDataDetail{
				Statuses: []exchange.OrderStatusResponse{
					{
						Filled: &exchange.FilledOrder{
							TotalSz: formatDecimal(filled),
							AvgPx:   formatDecimal(price),
							Oid:     oid,
						},
					},
				},
			},
		},
	}
	return resp, nil
}

// UpdateLeverage stores leverage preferences for later margin calculations.
func (p *Provider) UpdateLeverage(ctx context.Context, asset int, isCross bool, leverage int) error {
	if leverage <= 0 {
		return fmt.Errorf("sim: leverage must be positive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	mode := map[bool]string{true: "cross", false: "isolated"}[isCross]
	p.leverage[asset] = exchange.Leverage{Type: mode, Value: leverage}
	return nil
}

// GetAccountState returns a snapshot with equity, margin usage and open positions.
func (p *Provider) GetAccountState(ctx context.Context) (*exchange.AccountState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	positions, unrealized, notional, margin := p.buildAccountSnapshotLocked()
	equity := p.cash + unrealized
	state := &exchange.AccountState{
		MarginSummary: exchange.MarginSummary{
			AccountValue:    formatDecimal(equity),
			TotalMarginUsed: formatDecimal(margin),
			TotalNtlPos:     formatDecimal(notional),
			TotalRawUSD:     formatDecimal(notional),
		},
		CrossMarginSummary: exchange.CrossMarginSummary{
			AccountValue:    formatDecimal(equity),
			TotalMarginUsed: formatDecimal(margin),
			TotalNtlPos:     formatDecimal(notional),
			TotalRawUSD:     formatDecimal(notional),
		},
		AssetPositions: positions,
	}
	return state, nil
}

// GetAccountValue returns the current equity (cash + unrealised PnL).
func (p *Provider) GetAccountValue(ctx context.Context) (float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, unrealized, _, _ := p.buildAccountSnapshotLocked()
	return p.cash + unrealized, nil
}

// FormatPrice normalises price formatting to 8 decimal places.
func (p *Provider) FormatPrice(ctx context.Context, coin string, price float64) (string, error) {
	if price <= 0 {
		return "", fmt.Errorf("sim: price must be positive")
	}
	return formatDecimal(price), nil
}

// FormatSize normalises size formatting to 8 decimal places.
func (p *Provider) FormatSize(ctx context.Context, coin string, size float64) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("sim: size must be positive")
	}
	return formatDecimal(size), nil
}

// CancelAllBySymbol is a no-op for the simulator.
func (p *Provider) CancelAllBySymbol(ctx context.Context, coin string) error {
	return nil
}

// Registry hook for exchange.Config.
func init() {
	exchange.RegisterProvider("sim", func(name string, cfg *exchange.ProviderConfig) (exchange.Provider, error) {
		return New(), nil
	})
}

func (p *Provider) resolveMarkPriceLocked(coin string) float64 {
	if price, ok := p.markPx[coin]; ok && price > 0 {
		return price
	}
	if state, ok := p.positions[coin]; ok && state.Entry > 0 {
		return state.Entry
	}
	return defaultFallbackPrice
}

func (p *Provider) nextOrderIDLocked() int64 {
	oid := p.nextOID
	p.nextOID++
	return oid
}

func (p *Provider) feeRateForOrder(order exchange.Order) float64 {
	rate := defaultTakerFeeRate
	if order.Builder != nil && order.Builder.FeeBps > 0 {
		rate += float64(order.Builder.FeeBps) / 10000
	}
	return rate
}

func applySlippage(price float64, isBuy bool, slippage float64) float64 {
	if slippage <= 0 {
		return price
	}
	if isBuy {
		return price * (1 + slippage)
	}
	return price * math.Max(0, 1-slippage)
}

func clonePosition(state *positionState) *positionState {
	if state == nil {
		return nil
	}
	cp := *state
	return &cp
}

func (p *Provider) buildAccountSnapshotLocked() ([]exchange.Position, float64, float64, float64) {
	positions := make([]exchange.Position, 0, len(p.positions))
	totalUnreal := 0.0
	totalNotional := 0.0
	totalMargin := 0.0

	for coin, state := range p.positions {
		qty := state.Qty
		mark := p.resolveMarkPriceLocked(coin)
		notional := math.Abs(qty * mark)
		unreal := qty * (mark - state.Entry)
		lev := p.leverageForCoinLocked(coin)
		margin := notional
		if lev.Value > 0 {
			margin = notional / float64(lev.Value)
		}

		totalUnreal += unreal
		totalNotional += notional
		totalMargin += margin

		var entryPtr *string
		if state.Entry > 0 {
			entry := formatDecimal(state.Entry)
			entryPtr = new(string)
			*entryPtr = entry
		}
		roe := "0"
		if margin > 0 {
			roe = formatDecimal((unreal / margin) * 100)
		}
		pos := exchange.Position{
			Coin:           coin,
			EntryPx:        entryPtr,
			PositionValue:  formatDecimal(notional),
			Szi:            formatDecimal(qty),
			UnrealizedPnl:  formatDecimal(unreal),
			ReturnOnEquity: roe,
			Leverage:       lev,
		}
		positions = append(positions, pos)
	}

	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Coin < positions[j].Coin
	})
	return positions, totalUnreal, totalNotional, totalMargin
}

func (p *Provider) leverageForCoinLocked(coin string) exchange.Leverage {
	if id, ok := p.assetIndex[coin]; ok {
		if lev, exists := p.leverage[id]; exists {
			return lev
		}
	}
	return exchange.Leverage{Type: "cross", Value: 1}
}

func formatDecimal(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if math.Abs(v) < 1e-9 {
		return "0"
	}
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		return "0"
	}
	return s
}
