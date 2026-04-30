package model

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const (
	traderConfigRows                     = "id, version, exchange_provider, market_provider, allocation_pct, detail, created_by, created_at, updated_at"
	traderConfigRowsExpectAutoSet        = "id, version, exchange_provider, market_provider, allocation_pct, detail, created_by"
	cacheTraderConfigIdPrefix            = "cache:trader_config:id:"
	traderConfigHistoryRows              = "id, trader_id, version, config_snapshot, changed_fields, change_reason, changed_by, changed_at"
	traderConfigHistoryRowsExpectAutoSet = "trader_id, version, config_snapshot, changed_fields, change_reason, changed_by, changed_at"
	traderSymbolCooldownsRows            = "trader_id, symbol, cooldown_until, detail, created_at, updated_at"
)

type modelBase struct {
	conn  sqlx.SqlConn
	table string
}

func (m *modelBase) tableName() string {
	return m.table
}

func (m *modelBase) ExecNoCacheCtx(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if m == nil || m.conn == nil {
		return nil, sql.ErrConnDone
	}
	return m.conn.ExecCtx(ctx, query, args...)
}

func (m *modelBase) QueryRowNoCacheCtx(ctx context.Context, v any, query string, args ...any) error {
	if m == nil || m.conn == nil {
		return sql.ErrConnDone
	}
	return m.conn.QueryRowCtx(ctx, v, query, args...)
}

func (m *modelBase) QueryRowsNoCacheCtx(ctx context.Context, v any, query string, args ...any) error {
	if m == nil || m.conn == nil {
		return sql.ErrConnDone
	}
	return m.conn.QueryRowsCtx(ctx, v, query, args...)
}

func (m *modelBase) DelCacheCtx(context.Context, ...string) error {
	return nil
}

type (
	Accounts struct {
		Id string `db:"id"`
	}

	AccountEquitySnapshots struct {
		Id                         int64           `db:"id"`
		ModelId                    string          `db:"model_id"`
		TsMs                       int64           `db:"ts_ms"`
		DollarEquity               float64         `db:"dollar_equity"`
		RealizedPnl                float64         `db:"realized_pnl"`
		TotalUnrealizedPnl         float64         `db:"total_unrealized_pnl"`
		Metadata                   string          `db:"metadata"`
		CumPnlPct                  sql.NullFloat64 `db:"cum_pnl_pct"`
		SharpeRatio                sql.NullFloat64 `db:"sharpe_ratio"`
		SinceInceptionHourlyMarker sql.NullInt64   `db:"since_inception_hourly_marker"`
		SinceInceptionMinuteMarker sql.NullInt64   `db:"since_inception_minute_marker"`
		CreatedAt                  time.Time       `db:"created_at"`
	}

	Conversations struct {
		Id        int64          `db:"id"`
		ModelId   string         `db:"model_id"`
		Topic     sql.NullString `db:"topic"`
		CreatedAt time.Time      `db:"created_at"`
	}

	ConversationMessages struct {
		Id             int64         `db:"id"`
		ConversationId int64         `db:"conversation_id"`
		Role           string        `db:"role"`
		Content        string        `db:"content"`
		TsMs           sql.NullInt64 `db:"ts_ms"`
		Metadata       string        `db:"metadata"`
		CreatedAt      time.Time     `db:"created_at"`
	}

	DecisionCycles struct {
		Id            int64          `db:"id"`
		ModelId       string         `db:"model_id"`
		Success       bool           `db:"success"`
		ConfigVersion int64          `db:"config_version"`
		CycleNumber   sql.NullInt64  `db:"cycle_number"`
		PromptDigest  sql.NullString `db:"prompt_digest"`
		CotTrace      sql.NullString `db:"cot_trace"`
		Decisions     sql.NullString `db:"decisions"`
		ErrorMessage  sql.NullString `db:"error_message"`
		ExecutedAt    time.Time      `db:"executed_at"`
	}

	MarketAssets struct {
		Provider      string         `db:"provider"`
		Symbol        string         `db:"symbol"`
		Name          sql.NullString `db:"name"`
		SzDecimals    sql.NullInt64  `db:"sz_decimals"`
		MaxLeverage   sql.NullInt64  `db:"max_leverage"`
		OnlyIsolated  bool           `db:"only_isolated"`
		MarginTableId sql.NullInt64  `db:"margin_table_id"`
		IsDelisted    bool           `db:"is_delisted"`
		CreatedAt     time.Time      `db:"created_at"`
		UpdatedAt     time.Time      `db:"updated_at"`
	}

	MarketAssetCtx struct {
		Provider  string    `db:"provider"`
		Symbol    string    `db:"symbol"`
		Context   string    `db:"context"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	ModelAnalytics struct {
		ModelId   string    `db:"model_id"`
		Metric    string    `db:"metric"`
		Value     float64   `db:"value"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	Models struct {
		Id        string         `db:"id"`
		Name      sql.NullString `db:"name"`
		Provider  sql.NullString `db:"provider"`
		CreatedAt time.Time      `db:"created_at"`
	}

	Positions struct {
		Id        string    `db:"id"`
		TraderId  string    `db:"trader_id"`
		Symbol    string    `db:"symbol"`
		Side      string    `db:"side"`
		Status    string    `db:"status"`
		Detail    string    `db:"detail"`
		CreatedAt time.Time `db:"created_at"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	PriceLatest struct {
		Provider  string    `db:"provider"`
		Symbol    string    `db:"symbol"`
		Price     float64   `db:"price"`
		TsMs      int64     `db:"ts_ms"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	PriceTicks struct {
		Id       int64           `db:"id"`
		Provider string          `db:"provider"`
		Symbol   string          `db:"symbol"`
		Price    float64         `db:"price"`
		TsMs     int64           `db:"ts_ms"`
		Volume   sql.NullFloat64 `db:"volume"`
		Raw      sql.NullString  `db:"raw"`
	}

	Symbols struct {
		Symbol    string    `db:"symbol"`
		CreatedAt time.Time `db:"created_at"`
	}

	TraderConfig struct {
		Id               string         `db:"id"`
		Version          int64          `db:"version"`
		ExchangeProvider string         `db:"exchange_provider"`
		MarketProvider   string         `db:"market_provider"`
		AllocationPct    float64        `db:"allocation_pct"`
		Detail           string         `db:"detail"`
		CreatedBy        sql.NullString `db:"created_by"`
		CreatedAt        time.Time      `db:"created_at"`
		UpdatedAt        time.Time      `db:"updated_at"`
	}

	TraderConfigHistory struct {
		Id             int64          `db:"id"`
		TraderId       string         `db:"trader_id"`
		Version        int64          `db:"version"`
		ConfigSnapshot string         `db:"config_snapshot"`
		ChangedFields  pq.StringArray `db:"changed_fields"`
		ChangeReason   sql.NullString `db:"change_reason"`
		ChangedBy      sql.NullString `db:"changed_by"`
		ChangedAt      time.Time      `db:"changed_at"`
	}

	TraderRuntimeState struct {
		TraderId            string    `db:"trader_id"`
		ActiveConfigVersion int64     `db:"active_config_version"`
		IsRunning           bool      `db:"is_running"`
		Detail              string    `db:"detail"`
		UpdatedAt           time.Time `db:"updated_at"`
	}

	TraderState struct {
		TraderId  string    `db:"trader_id"`
		Detail    string    `db:"detail"`
		UpdatedAt time.Time `db:"updated_at"`
	}

	TraderSymbolCooldowns struct {
		TraderId      string    `db:"trader_id"`
		Symbol        string    `db:"symbol"`
		CooldownUntil time.Time `db:"cooldown_until"`
		Detail        string    `db:"detail"`
		CreatedAt     time.Time `db:"created_at"`
		UpdatedAt     time.Time `db:"updated_at"`
	}

	Trades struct {
		Id        string    `db:"id"`
		TraderId  string    `db:"trader_id"`
		Symbol    string    `db:"symbol"`
		Side      string    `db:"side"`
		CloseTsMs int64     `db:"close_ts_ms"`
		Detail    string    `db:"detail"`
		CreatedAt time.Time `db:"created_at"`
	}
)

type accountsModel interface{}
type defaultAccountsModel struct{ modelBase }

func newAccountsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultAccountsModel {
	return &defaultAccountsModel{modelBase: modelBase{conn: conn, table: "accounts"}}
}

type accountEquitySnapshotsModel interface {
	Insert(context.Context, *AccountEquitySnapshots) (sql.Result, error)
	Update(context.Context, *AccountEquitySnapshots) error
	FindOneByModelIdTsMs(context.Context, string, int64) (*AccountEquitySnapshots, error)
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
}
type defaultAccountEquitySnapshotsModel struct{ modelBase }

func newAccountEquitySnapshotsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultAccountEquitySnapshotsModel {
	return &defaultAccountEquitySnapshotsModel{modelBase: modelBase{conn: conn, table: "account_equity_snapshots"}}
}

func (m *defaultAccountEquitySnapshotsModel) Insert(ctx context.Context, data *AccountEquitySnapshots) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (model_id, ts_ms, dollar_equity, realized_pnl, total_unrealized_pnl, metadata, cum_pnl_pct, sharpe_ratio, since_inception_hourly_marker, since_inception_minute_marker)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, m.tableName())
	return m.ExecNoCacheCtx(ctx, query, data.ModelId, data.TsMs, data.DollarEquity, data.RealizedPnl, data.TotalUnrealizedPnl, data.Metadata, data.CumPnlPct, data.SharpeRatio, data.SinceInceptionHourlyMarker, data.SinceInceptionMinuteMarker)
}

func (m *defaultAccountEquitySnapshotsModel) Update(ctx context.Context, data *AccountEquitySnapshots) error {
	query := fmt.Sprintf(`update %s set dollar_equity = $2, realized_pnl = $3, total_unrealized_pnl = $4, metadata = $5, cum_pnl_pct = $6, sharpe_ratio = $7, since_inception_hourly_marker = $8, since_inception_minute_marker = $9 where id = $1`, m.tableName())
	_, err := m.ExecNoCacheCtx(ctx, query, data.Id, data.DollarEquity, data.RealizedPnl, data.TotalUnrealizedPnl, data.Metadata, data.CumPnlPct, data.SharpeRatio, data.SinceInceptionHourlyMarker, data.SinceInceptionMinuteMarker)
	return err
}

func (m *defaultAccountEquitySnapshotsModel) FindOneByModelIdTsMs(ctx context.Context, modelID string, tsMs int64) (*AccountEquitySnapshots, error) {
	query := fmt.Sprintf(`select id, model_id, ts_ms, dollar_equity, realized_pnl, total_unrealized_pnl, metadata, cum_pnl_pct, sharpe_ratio, since_inception_hourly_marker, since_inception_minute_marker, created_at from %s where model_id = $1 and ts_ms = $2 limit 1`, m.tableName())
	var resp AccountEquitySnapshots
	if err := m.QueryRowNoCacheCtx(ctx, &resp, query, modelID, tsMs); err != nil {
		return nil, err
	}
	return &resp, nil
}

type conversationsModel interface{}
type defaultConversationsModel struct{ modelBase }

func newConversationsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultConversationsModel {
	return &defaultConversationsModel{modelBase: modelBase{conn: conn, table: "conversations"}}
}

type conversationMessagesModel interface{}
type defaultConversationMessagesModel struct{ modelBase }

func newConversationMessagesModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultConversationMessagesModel {
	return &defaultConversationMessagesModel{modelBase: modelBase{conn: conn, table: "conversation_messages"}}
}

type decisionCyclesModel interface {
	Insert(context.Context, *DecisionCycles) (sql.Result, error)
}
type defaultDecisionCyclesModel struct{ modelBase }

func newDecisionCyclesModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultDecisionCyclesModel {
	return &defaultDecisionCyclesModel{modelBase: modelBase{conn: conn, table: "decision_cycles"}}
}

func (m *defaultDecisionCyclesModel) Insert(ctx context.Context, data *DecisionCycles) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (model_id, success, config_version, cycle_number, prompt_digest, cot_trace, decisions, error_message, executed_at)
values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, m.tableName())
	return m.ExecNoCacheCtx(ctx, query, data.ModelId, data.Success, data.ConfigVersion, data.CycleNumber, data.PromptDigest, data.CotTrace, data.Decisions, data.ErrorMessage, data.ExecutedAt)
}

type marketAssetsModel interface{}
type defaultMarketAssetsModel struct{ modelBase }

func newMarketAssetsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultMarketAssetsModel {
	return &defaultMarketAssetsModel{modelBase: modelBase{conn: conn, table: "market_assets"}}
}

type marketAssetCtxModel interface{}
type defaultMarketAssetCtxModel struct{ modelBase }

func newMarketAssetCtxModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultMarketAssetCtxModel {
	return &defaultMarketAssetCtxModel{modelBase: modelBase{conn: conn, table: "market_asset_ctx"}}
}

type modelAnalyticsModel interface{}
type defaultModelAnalyticsModel struct{ modelBase }

func newModelAnalyticsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultModelAnalyticsModel {
	return &defaultModelAnalyticsModel{modelBase: modelBase{conn: conn, table: "model_analytics"}}
}

type modelsModel interface{}
type defaultModelsModel struct{ modelBase }

func newModelsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultModelsModel {
	return &defaultModelsModel{modelBase: modelBase{conn: conn, table: "models"}}
}

type positionsModel interface {
	FindOne(context.Context, string) (*Positions, error)
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
}
type defaultPositionsModel struct{ modelBase }

func newPositionsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultPositionsModel {
	return &defaultPositionsModel{modelBase: modelBase{conn: conn, table: "positions"}}
}

func (m *defaultPositionsModel) FindOne(ctx context.Context, id string) (*Positions, error) {
	query := fmt.Sprintf(`select id, trader_id, symbol, side, status, detail, created_at, updated_at from %s where id = $1 limit 1`, m.tableName())
	var resp Positions
	if err := m.QueryRowNoCacheCtx(ctx, &resp, query, id); err != nil {
		return nil, err
	}
	return &resp, nil
}

type priceLatestModel interface{}
type defaultPriceLatestModel struct{ modelBase }

func newPriceLatestModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultPriceLatestModel {
	return &defaultPriceLatestModel{modelBase: modelBase{conn: conn, table: "price_latest"}}
}

type priceTicksModel interface {
	Insert(context.Context, *PriceTicks) (sql.Result, error)
}
type defaultPriceTicksModel struct{ modelBase }

func newPriceTicksModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultPriceTicksModel {
	return &defaultPriceTicksModel{modelBase: modelBase{conn: conn, table: "price_ticks"}}
}

func (m *defaultPriceTicksModel) Insert(ctx context.Context, data *PriceTicks) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (provider, symbol, price, ts_ms, volume, raw) values ($1, $2, $3, $4, $5, $6)`, m.tableName())
	return m.ExecNoCacheCtx(ctx, query, data.Provider, data.Symbol, data.Price, data.TsMs, data.Volume, data.Raw)
}

type symbolsModel interface{}
type defaultSymbolsModel struct{ modelBase }

func newSymbolsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultSymbolsModel {
	return &defaultSymbolsModel{modelBase: modelBase{conn: conn, table: "symbols"}}
}

type traderConfigModel interface {
	FindOne(context.Context, string) (*TraderConfig, error)
	QueryRowNoCacheCtx(context.Context, any, string, ...any) error
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
	DelCacheCtx(context.Context, ...string) error
	tableName() string
}
type defaultTraderConfigModel struct{ modelBase }

func newTraderConfigModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTraderConfigModel {
	return &defaultTraderConfigModel{modelBase: modelBase{conn: conn, table: "trader_config"}}
}

func (m *defaultTraderConfigModel) FindOne(ctx context.Context, id string) (*TraderConfig, error) {
	query := fmt.Sprintf(`select %s from %s where id = $1 order by version desc limit 1`, traderConfigRows, m.tableName())
	var resp TraderConfig
	if err := m.QueryRowNoCacheCtx(ctx, &resp, query, id); err != nil {
		return nil, err
	}
	return &resp, nil
}

type traderConfigHistoryModel interface {
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
	tableName() string
}
type defaultTraderConfigHistoryModel struct{ modelBase }

func newTraderConfigHistoryModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTraderConfigHistoryModel {
	return &defaultTraderConfigHistoryModel{modelBase: modelBase{conn: conn, table: "trader_config_history"}}
}

type traderRuntimeStateModel interface {
	FindOne(context.Context, string) (*TraderRuntimeState, error)
	ExecNoCacheCtx(context.Context, string, ...any) (sql.Result, error)
	tableName() string
}
type defaultTraderRuntimeStateModel struct{ modelBase }

func newTraderRuntimeStateModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTraderRuntimeStateModel {
	return &defaultTraderRuntimeStateModel{modelBase: modelBase{conn: conn, table: "trader_runtime_state"}}
}

func (m *defaultTraderRuntimeStateModel) FindOne(ctx context.Context, traderID string) (*TraderRuntimeState, error) {
	query := fmt.Sprintf(`select trader_id, active_config_version, is_running, detail, updated_at from %s where trader_id = $1 limit 1`, m.tableName())
	var resp TraderRuntimeState
	if err := m.QueryRowNoCacheCtx(ctx, &resp, query, traderID); err != nil {
		return nil, err
	}
	return &resp, nil
}

type traderStateModel interface{}
type defaultTraderStateModel struct{ modelBase }

func newTraderStateModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTraderStateModel {
	return &defaultTraderStateModel{modelBase: modelBase{conn: conn, table: "trader_state"}}
}

type traderSymbolCooldownsModel interface {
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
	ExecNoCacheCtx(context.Context, string, ...any) (sql.Result, error)
	tableName() string
}
type defaultTraderSymbolCooldownsModel struct{ modelBase }

func newTraderSymbolCooldownsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTraderSymbolCooldownsModel {
	return &defaultTraderSymbolCooldownsModel{modelBase: modelBase{conn: conn, table: "trader_symbol_cooldowns"}}
}

type tradesModel interface {
	Insert(context.Context, *Trades) (sql.Result, error)
	QueryRowsNoCacheCtx(context.Context, any, string, ...any) error
}
type defaultTradesModel struct{ modelBase }

func newTradesModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) *defaultTradesModel {
	return &defaultTradesModel{modelBase: modelBase{conn: conn, table: "trades"}}
}

func (m *defaultTradesModel) Insert(ctx context.Context, data *Trades) (sql.Result, error) {
	query := fmt.Sprintf(`insert into %s (id, trader_id, symbol, side, close_ts_ms, detail) values ($1, $2, $3, $4, $5, $6)`, m.tableName())
	return m.ExecNoCacheCtx(ctx, query, data.Id, data.TraderId, data.Symbol, data.Side, data.CloseTsMs, data.Detail)
}
