// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"nof0-api/internal/model"
	"nof0-api/internal/svc"
	"nof0-api/internal/types"
	executorpkg "nof0-api/pkg/executor"
	"nof0-api/pkg/market"
	"nof0-api/pkg/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

type AccountTotalsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAccountTotalsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AccountTotalsLogic {
	return &AccountTotalsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AccountTotalsLogic) AccountTotals(req *types.AccountTotalsRequest) (resp *types.AccountTotalsResponse, err error) {
	if resp, used, err := l.loadAccountTotalsFromDB(req); used {
		if resp == nil {
			resp = emptyAccountTotalsResponse()
		}
		return resp, err
	}
	if l == nil || l.svcCtx == nil || l.svcCtx.DataLoader == nil {
		return emptyAccountTotalsResponse(), nil
	}
	return l.svcCtx.DataLoader.LoadAccountTotals()
}

func (l *AccountTotalsLogic) accountTotalsFromDB(req *types.AccountTotalsRequest) (*types.AccountTotalsResponse, bool, error) {
	if !hasAccountTotalsDBWiring(l.svcCtx) {
		return nil, false, nil
	}

	snapshots, err := l.queryAccountSnapshots(req)
	if err != nil {
		return nil, true, err
	}

	positionGroups, err := l.queryOpenPositions()
	if err != nil {
		return nil, true, err
	}

	modelIDs := collectAccountModelIDs(snapshots, positionGroups)
	decisionPlans, err := l.queryDecisionPlans(modelIDs)
	if err != nil {
		return nil, true, err
	}

	positionsByModel := make(map[string]map[string]types.Position, len(positionGroups))
	for modelID, records := range positionGroups {
		positionsByModel[modelID] = l.buildPositionsForModel(modelID, records, decisionPlans[modelID])
	}

	if len(snapshots) == 0 {
		resp := &types.AccountTotalsResponse{
			AccountTotals:        []types.AccountTotal{},
			LastHourlyMarkerRead: 0,
			ServerTime:           time.Now().UnixMilli(),
		}
		if req != nil && req.LastHourlyMarker <= 0 {
			resp.AccountTotals = l.syntheticTotalsFromPositions(positionGroups, positionsByModel)
		}
		return resp, true, nil
	}

	totals := make([]types.AccountTotal, 0, len(snapshots))
	for _, snapshot := range snapshots {
		totals = append(totals, l.accountTotalFromSnapshot(snapshot, clonePositionMap(positionsByModel[snapshot.ModelID]), modelIDDecisionMap(decisionPlans, snapshot.ModelID)))
	}

	if req == nil || req.LastHourlyMarker <= 0 {
		seen := make(map[string]struct{}, len(totals))
		for _, total := range totals {
			seen[total.ModelId] = struct{}{}
		}
		for modelID, records := range positionGroups {
			if _, ok := seen[modelID]; ok {
				continue
			}
			totals = append(totals, l.syntheticAccountTotal(modelID, records, clonePositionMap(positionsByModel[modelID]), modelIDDecisionMap(decisionPlans, modelID)))
		}
	}

	sort.SliceStable(totals, func(i, j int) bool {
		if totals[i].Timestamp == totals[j].Timestamp {
			if totals[i].ModelId == totals[j].ModelId {
				return totals[i].Id < totals[j].Id
			}
			return totals[i].ModelId < totals[j].ModelId
		}
		return totals[i].Timestamp < totals[j].Timestamp
	})

	resp := &types.AccountTotalsResponse{
		AccountTotals:        totals,
		LastHourlyMarkerRead: maxHourlyMarkerFromTotals(totals),
		ServerTime:           time.Now().UnixMilli(),
	}
	return resp, true, nil
}

func (l *AccountTotalsLogic) queryAccountSnapshots(req *types.AccountTotalsRequest) ([]accountSnapshotRow, error) {
	source, ok := accountSnapshotRowsSource(l.svcCtx)
	if !ok {
		return nil, nil
	}
	query := `
SELECT
    model_id,
    ts_ms,
    dollar_equity,
    realized_pnl,
    total_unrealized_pnl,
    cum_pnl_pct,
    sharpe_ratio,
    since_inception_hourly_marker,
    since_inception_minute_marker,
    metadata
FROM public.account_equity_snapshots`
	var args []any
	if req != nil && req.LastHourlyMarker > 0 {
		query += ` WHERE COALESCE(since_inception_hourly_marker, 0) > $1`
		args = append(args, req.LastHourlyMarker)
	}
	query += ` ORDER BY ts_ms ASC, model_id ASC`

	var rows []accountSnapshotRow
	if err := source.QueryRowsNoCacheCtx(accountTotalsContext(l.ctx), &rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

func (l *AccountTotalsLogic) queryOpenPositions() (map[string][]model.PositionRecord, error) {
	source, ok := accountPositionsSource(l.svcCtx)
	if !ok {
		return nil, nil
	}
	return source.ActiveByModels(accountTotalsContext(l.ctx), nil)
}

func (l *AccountTotalsLogic) queryDecisionPlans(modelIDs []string) (map[string]map[string]executorpkg.Decision, error) {
	source, ok := accountCommandSource(l.svcCtx)
	if !ok || len(modelIDs) == 0 {
		return map[string]map[string]executorpkg.Decision{}, nil
	}
	result := make(map[string]map[string]executorpkg.Decision, len(modelIDs))
	for _, modelID := range modelIDs {
		records, err := source.List(accountTotalsContext(l.ctx), repo.ControlCommandListFilter{
			Target:   repo.ControlCommandTargetDecision,
			Status:   repo.ControlCommandStatusCompleted,
			TraderID: modelID,
			Limit:    500,
		})
		if err != nil {
			return nil, err
		}
		latest := make(map[string]decisionWithTime)
		for _, record := range records {
			decision, ok := decisionFromControlCommandDetail(record.Detail)
			if !ok || !isOpenDecisionAction(decision.Action) {
				continue
			}
			symbol := strings.ToUpper(strings.TrimSpace(decision.Symbol))
			if symbol == "" {
				continue
			}
			if prev, exists := latest[symbol]; !exists || record.CreatedAt.After(prev.createdAt) || (record.CreatedAt.Equal(prev.createdAt) && record.ID > prev.id) {
				latest[symbol] = decisionWithTime{decision: decision, createdAt: record.CreatedAt, id: record.ID}
			}
		}
		if len(latest) == 0 {
			continue
		}
		result[modelID] = make(map[string]executorpkg.Decision, len(latest))
		for symbol, entry := range latest {
			result[modelID][symbol] = entry.decision
		}
	}
	return result, nil
}

func (l *AccountTotalsLogic) buildPositionsForModel(modelID string, records []model.PositionRecord, decisionMap map[string]executorpkg.Decision) map[string]types.Position {
	positions := make(map[string]types.Position, len(records))
	marketProvider := l.marketProviderForModel(modelID)
	currentPrices := make(map[string]float64, len(records))
	for _, record := range records {
		symbol := strings.ToUpper(strings.TrimSpace(record.Symbol))
		if symbol == "" {
			continue
		}
		pos := positionFromRecord(record, modelID, symbol)
		currentPrice := l.currentPriceForSymbol(marketProvider, currentPrices, symbol, pos.EntryPrice)
		pos.CurrentPrice = currentPrice
		pos.UnrealizedPnl = positionUnrealizedPnL(pos.EntryPrice, currentPrice, pos.Quantity, record.UnrealizedPnl)
		pos.LiquidationPrice = approximateLiquidationPrice(pos.EntryPrice, pos.Leverage, pos.Quantity)
		pos.WaitForFill = false
		pos.TpOid = -1
		pos.SlOid = -1
		pos.IndexCol = nil
		pos.ExitPlan = nil
		pos.Commission = 0
		pos.Slippage = 0
		pos.ClosedPnl = 0
		if decision, ok := decisionMap[symbol]; ok {
			pos.ExitPlan = exitPlanFromDecision(decision)
		}
		positions[symbol] = pos
	}
	return positions
}

func (l *AccountTotalsLogic) currentPriceForSymbol(provider market.Provider, cache map[string]float64, symbol string, fallback float64) float64 {
	if cached, ok := cache[symbol]; ok {
		return cached
	}
	price := fallback
	if provider != nil {
		if snap, err := provider.Snapshot(accountTotalsContext(l.ctx), symbol); err == nil && snap != nil && snap.Price.Last > 0 {
			price = snap.Price.Last
		}
	}
	if price <= 0 {
		price = fallback
	}
	cache[symbol] = price
	return price
}

func (l *AccountTotalsLogic) marketProviderForModel(modelID string) market.Provider {
	if l == nil || l.svcCtx == nil {
		return nil
	}
	if provider, ok := l.svcCtx.ManagerTraderMarket[modelID]; ok && provider != nil {
		return provider
	}
	return l.svcCtx.DefaultMarket
}

func (l *AccountTotalsLogic) accountTotalFromSnapshot(snapshot accountSnapshotRow, positions map[string]types.Position, decisionMap map[string]executorpkg.Decision) types.AccountTotal {
	total := types.AccountTotal{
		Id:                         accountTotalSnapshotID(snapshot.ModelID, snapshot.TsMs),
		ModelId:                    snapshot.ModelID,
		Timestamp:                  unixSecondsFromMillis(snapshot.TsMs),
		DollarEquity:               snapshot.DollarEquity,
		RealizedPnl:                snapshot.RealizedPnl,
		TotalUnrealizedPnl:         snapshot.totalUnrealizedPnl(),
		CumPnlPct:                  snapshot.cumPnlPct(),
		SharpeRatio:                snapshot.sharpeRatio(),
		SinceInceptionHourlyMarker: snapshot.hourlyMarker(),
		SinceInceptionMinuteMarker: snapshot.minuteMarker(),
		Positions:                  positions,
	}
	if total.TotalUnrealizedPnl == 0 {
		total.TotalUnrealizedPnl = sumPositionUnrealized(positions)
	}
	if total.CumPnlPct == 0 && snapshot.DollarEquity > 0 {
		total.CumPnlPct = ((snapshot.DollarEquity - 10000) / 10000) * 100
	}
	if len(total.Positions) == 0 {
		total.Positions = map[string]types.Position{}
	}
	if len(decisionMap) == 0 {
		return total
	}
	for symbol, pos := range total.Positions {
		if decision, ok := decisionMap[symbol]; ok {
			pos.ExitPlan = exitPlanFromDecision(decision)
			total.Positions[symbol] = pos
		}
	}
	return total
}

func (l *AccountTotalsLogic) syntheticAccountTotal(modelID string, records []model.PositionRecord, positions map[string]types.Position, decisionMap map[string]executorpkg.Decision) types.AccountTotal {
	unrealized := sumPositionUnrealized(positions)
	equity := 10000 + unrealized
	if equity < 0 {
		equity = 0
	}
	total := types.AccountTotal{
		Id:                         fmt.Sprintf("%s_synthetic", modelID),
		ModelId:                    modelID,
		Timestamp:                  unixSecondsFromMillis(latestPositionEntryMillis(records)),
		DollarEquity:               equity,
		RealizedPnl:                0,
		TotalUnrealizedPnl:         unrealized,
		CumPnlPct:                  ((equity - 10000) / 10000) * 100,
		SharpeRatio:                0,
		SinceInceptionHourlyMarker: 0,
		SinceInceptionMinuteMarker: 0,
		Positions:                  positions,
	}
	if total.Positions == nil {
		total.Positions = map[string]types.Position{}
	}
	for symbol, pos := range total.Positions {
		if decision, ok := decisionMap[symbol]; ok {
			pos.ExitPlan = exitPlanFromDecision(decision)
			total.Positions[symbol] = pos
		}
	}
	return total
}

func (l *AccountTotalsLogic) syntheticTotalsFromPositions(positionGroups map[string][]model.PositionRecord, positionsByModel map[string]map[string]types.Position) []types.AccountTotal {
	if len(positionGroups) == 0 {
		return []types.AccountTotal{}
	}
	modelIDs := make([]string, 0, len(positionGroups))
	for modelID := range positionGroups {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)
	totals := make([]types.AccountTotal, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		totals = append(totals, l.syntheticAccountTotal(modelID, positionGroups[modelID], clonePositionMap(positionsByModel[modelID]), map[string]executorpkg.Decision{}))
	}
	return totals
}

func (l *AccountTotalsLogic) loadAccountTotalsFromDB(req *types.AccountTotalsRequest) (*types.AccountTotalsResponse, bool, error) {
	if !hasAccountTotalsDBWiring(l.svcCtx) {
		return nil, false, nil
	}
	resp, _, err := l.accountTotalsFromDB(req)
	return resp, true, err
}

func hasAccountTotalsDBWiring(svcCtx *svc.ServiceContext) bool {
	return svcCtx != nil && svcCtx.AccountEquitySnapshotsModel != nil && svcCtx.PositionsModel != nil
}

func accountSnapshotRowsSource(svcCtx *svc.ServiceContext) (accountSnapshotRowsQueryer, bool) {
	if svcCtx == nil || svcCtx.AccountEquitySnapshotsModel == nil {
		return nil, false
	}
	source, ok := any(svcCtx.AccountEquitySnapshotsModel).(accountSnapshotRowsQueryer)
	return source, ok
}

func accountPositionsSource(svcCtx *svc.ServiceContext) (accountPositionsReader, bool) {
	if svcCtx == nil || svcCtx.PositionsModel == nil {
		return nil, false
	}
	source, ok := any(svcCtx.PositionsModel).(accountPositionsReader)
	return source, ok
}

func accountCommandSource(svcCtx *svc.ServiceContext) (accountCommandLister, bool) {
	if svcCtx == nil || svcCtx.ControlCommandRepo == nil {
		return nil, false
	}
	source, ok := any(svcCtx.ControlCommandRepo).(accountCommandLister)
	return source, ok
}

func accountTotalsContext(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func accountTotalSnapshotID(modelID string, tsMs int64) string {
	return fmt.Sprintf("%s_%d", strings.TrimSpace(modelID), tsMs)
}

func unixSecondsFromMillis(tsMs int64) float64 {
	if tsMs <= 0 {
		return 0
	}
	return float64(tsMs) / 1000
}

func maxHourlyMarkerFromTotals(totals []types.AccountTotal) int {
	maxMarker := 0
	for _, total := range totals {
		if total.SinceInceptionHourlyMarker > maxMarker {
			maxMarker = total.SinceInceptionHourlyMarker
		}
	}
	return maxMarker
}

func sumPositionUnrealized(positions map[string]types.Position) float64 {
	var total float64
	for _, pos := range positions {
		total += pos.UnrealizedPnl
	}
	return total
}

func sumPositionMargin(positions map[string]types.Position) float64 {
	var total float64
	for _, pos := range positions {
		total += pos.Margin
	}
	return total
}

func latestPositionEntryMillis(records []model.PositionRecord) int64 {
	var max int64
	for _, record := range records {
		if record.EntryTimeMs > max {
			max = record.EntryTimeMs
		}
	}
	if max > 0 {
		return max
	}
	return time.Now().UTC().UnixMilli()
}

func clonePositionMap(src map[string]types.Position) map[string]types.Position {
	if len(src) == 0 {
		return map[string]types.Position{}
	}
	out := make(map[string]types.Position, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func collectAccountModelIDs(snapshots []accountSnapshotRow, positionsByModel map[string][]model.PositionRecord) []string {
	ids := make(map[string]struct{}, len(snapshots)+len(positionsByModel))
	for _, snapshot := range snapshots {
		if modelID := strings.TrimSpace(snapshot.ModelID); modelID != "" {
			ids[modelID] = struct{}{}
		}
	}
	for modelID := range positionsByModel {
		if modelID = strings.TrimSpace(modelID); modelID != "" {
			ids[modelID] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	for modelID := range ids {
		out = append(out, modelID)
	}
	sort.Strings(out)
	return out
}

func modelIDDecisionMap(decisions map[string]map[string]executorpkg.Decision, modelID string) map[string]executorpkg.Decision {
	if len(decisions) == 0 {
		return map[string]executorpkg.Decision{}
	}
	if modelMap, ok := decisions[modelID]; ok && len(modelMap) > 0 {
		return modelMap
	}
	return map[string]executorpkg.Decision{}
}

func decisionFromControlCommandDetail(raw json.RawMessage) (executorpkg.Decision, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return executorpkg.Decision{}, false
	}
	var payload struct {
		Decision *executorpkg.Decision `json:"decision"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Decision != nil {
		return *payload.Decision, true
	}
	var direct executorpkg.Decision
	if err := json.Unmarshal(raw, &direct); err == nil && strings.TrimSpace(direct.Action) != "" {
		return direct, true
	}
	return executorpkg.Decision{}, false
}

func isOpenDecisionAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "open_long", "open_short":
		return true
	default:
		return false
	}
}

func exitPlanFromDecision(decision executorpkg.Decision) map[string]any {
	plan := map[string]any{}
	if decision.TakeProfit > 0 {
		plan["profit_target"] = decision.TakeProfit
	}
	if decision.StopLoss > 0 {
		plan["stop_loss"] = decision.StopLoss
	}
	if strings.TrimSpace(decision.InvalidationCondition) != "" {
		plan["invalidation_condition"] = decision.InvalidationCondition
	}
	if len(plan) == 0 {
		return nil
	}
	return plan
}

func positionUnrealizedPnL(entryPrice, currentPrice, quantity float64, fallback *float64) float64 {
	if entryPrice > 0 && currentPrice > 0 && quantity != 0 {
		return (currentPrice - entryPrice) * quantity
	}
	if fallback != nil {
		return *fallback
	}
	return 0
}

func approximateLiquidationPrice(entryPrice, leverage, quantity float64) float64 {
	if entryPrice <= 0 || leverage <= 0 {
		return 0
	}
	delta := entryPrice / leverage
	if quantity < 0 {
		return entryPrice + delta
	}
	price := entryPrice - delta
	if price < 0 {
		return 0
	}
	return price
}

type accountSnapshotRowsQueryer interface {
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
}

type accountPositionsReader interface {
	ActiveByModels(context.Context, []string) (map[string][]model.PositionRecord, error)
}

type accountCommandLister interface {
	List(context.Context, repo.ControlCommandListFilter) ([]repo.ControlCommandRecord, error)
}

type decisionWithTime struct {
	decision  executorpkg.Decision
	createdAt time.Time
	id        string
}

type accountSnapshotRow struct {
	ModelID                    string          `db:"model_id"`
	TsMs                       int64           `db:"ts_ms"`
	DollarEquity               float64         `db:"dollar_equity"`
	RealizedPnl                float64         `db:"realized_pnl"`
	TotalUnrealizedPnl         float64         `db:"total_unrealized_pnl"`
	CumPnlPct                  sql.NullFloat64 `db:"cum_pnl_pct"`
	SharpeRatio                sql.NullFloat64 `db:"sharpe_ratio"`
	SinceInceptionHourlyMarker sql.NullInt64   `db:"since_inception_hourly_marker"`
	SinceInceptionMinuteMarker sql.NullInt64   `db:"since_inception_minute_marker"`
	Metadata                   string          `db:"metadata"`
}

func (r accountSnapshotRow) totalUnrealizedPnl() float64 {
	return r.TotalUnrealizedPnl
}

func (r accountSnapshotRow) cumPnlPct() float64 {
	if r.CumPnlPct.Valid {
		return r.CumPnlPct.Float64
	}
	return 0
}

func (r accountSnapshotRow) sharpeRatio() float64 {
	if r.SharpeRatio.Valid {
		return r.SharpeRatio.Float64
	}
	return 0
}

func (r accountSnapshotRow) hourlyMarker() int {
	if r.SinceInceptionHourlyMarker.Valid {
		return int(r.SinceInceptionHourlyMarker.Int64)
	}
	return 0
}

func (r accountSnapshotRow) minuteMarker() int {
	if r.SinceInceptionMinuteMarker.Valid {
		return int(r.SinceInceptionMinuteMarker.Int64)
	}
	return 0
}

func emptyAccountTotalsResponse() *types.AccountTotalsResponse {
	return &types.AccountTotalsResponse{
		AccountTotals:        []types.AccountTotal{},
		LastHourlyMarkerRead: 0,
		ServerTime:           time.Now().UnixMilli(),
	}
}
