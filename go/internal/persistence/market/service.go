package marketpersist

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/zeromicro/go-zero/core/logx"
	gocache "github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	cachekeys "nof0-api/internal/cache"
	"nof0-api/internal/model"
	persistmetrics "nof0-api/internal/persistence/metrics"
	persistresilience "nof0-api/internal/persistence/resilience"
	"nof0-api/pkg/market"
)

const (
	assetSQLTimeout    = 30 * time.Second
	assetCacheTimeout  = 30 * time.Second
	snapshotSQLTimeout = 30 * time.Second
	hydrateSQLTimeout  = 30 * time.Second
	cacheWorkerLimit   = 32

	marketCacheAssetsOp       = "market.cache.assets"
	marketCachePriceOp        = "market.cache.price_latest"
	marketCacheContextOp      = "market.cache.context"
	marketCacheCryptoPricesOp = "market.cache.crypto_prices"

	marketDBAssetsOp      = "market.db.assets"
	marketDBSnapshotOp    = "market.db.snapshot"
	marketDBPriceSeriesOp = "market.db.price_ticks"
)

// Service implements market data persistence and caching hooks.
type Service struct {
	sqlConn         sqlx.SqlConn
	assetsModel     model.MarketAssetsModel
	priceTicksModel model.PriceTicksModel
	cache           gocache.Cache
	redis           *redis.Redis
	ttl             cachekeys.TTLSet
	retryQueue      persistresilience.Enqueuer
}

// Config enumerates dependencies required to persist market data.
type Config struct {
	SQLConn         sqlx.SqlConn
	AssetsModel     model.MarketAssetsModel
	PriceTicksModel model.PriceTicksModel
	Cache           gocache.Cache
	Redis           *redis.Redis
	TTL             cachekeys.TTLSet
	RetryQueue      persistresilience.Enqueuer
}

// NewService wires a market persistence service. Returns nil when dependencies missing.
func NewService(cfg Config) market.Persistence {
	if cfg.SQLConn == nil {
		return nil
	}
	return &Service{
		sqlConn:         cfg.SQLConn,
		assetsModel:     cfg.AssetsModel,
		priceTicksModel: cfg.PriceTicksModel,
		cache:           cfg.Cache,
		redis:           cfg.Redis,
		ttl:             cfg.TTL,
		retryQueue:      cfg.RetryQueue,
	}
}

// UpsertAssets persists static metadata and refreshes Redis cache.
func (s *Service) UpsertAssets(ctx context.Context, provider string, assets []market.Asset) error {
	if s == nil || s.sqlConn == nil || len(assets) == 0 {
		return nil
	}
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	start := time.Now()

	// Prepare batch INSERT with multiple VALUES clauses
	var valueClauses []string
	var args []interface{}
	validAssets := make([]market.Asset, 0, len(assets))
	argIndex := 1

	for _, asset := range assets {
		if strings.TrimSpace(asset.Symbol) == "" {
			continue
		}
		name := asset.Symbol
		if base := strings.TrimSpace(asset.Base); base != "" {
			name = base
		}
		metadata := asset.RawMetadata
		maxLev := nullFloatFromMeta(metadata, "maxLeverage")
		marginTbl := nullIntFromMeta(metadata, "marginTable", "margin_table_id")
		onlyIso := nullBoolFromMeta(metadata, "onlyIsolated", "only_isolated")
		precision := sql.NullInt64{}
		if asset.Precision > 0 {
			precision = sql.NullInt64{Int64: int64(asset.Precision), Valid: true}
		}
		isDelisted := !asset.IsActive

		// Build placeholders for this row: ($1, $2, $3, ..., $8)
		placeholders := make([]string, 8)
		for i := 0; i < 8; i++ {
			placeholders[i] = fmt.Sprintf("$%d", argIndex)
			argIndex++
		}
		valueClauses = append(valueClauses, fmt.Sprintf("(%s, NOW(), NOW())", strings.Join(placeholders, ", ")))

		// Append arguments in order
		args = append(args,
			provider,
			asset.Symbol,
			sql.NullString{String: name, Valid: name != ""},
			precision,
			maxLev,
			onlyIso,
			marginTbl,
			isDelisted,
		)

		validAssets = append(validAssets, asset)
	}

	if len(valueClauses) == 0 {
		logx.WithContext(ctx).Infow("marketpersist: no valid assets to upsert", logx.Field("provider", provider))
		return nil
	}

	// Build single batch INSERT statement
	stmt := fmt.Sprintf(`
INSERT INTO public.market_assets (
    provider, symbol, name, sz_decimals, max_leverage, only_isolated, margin_table_id, is_delisted, created_at, updated_at
) VALUES %s
ON CONFLICT (provider, symbol) DO UPDATE SET
    name = EXCLUDED.name,
    sz_decimals = EXCLUDED.sz_decimals,
    max_leverage = EXCLUDED.max_leverage,
    only_isolated = EXCLUDED.only_isolated,
    margin_table_id = EXCLUDED.margin_table_id,
    is_delisted = EXCLUDED.is_delisted,
    updated_at = NOW()`, strings.Join(valueClauses, ", "))

	sqlCtx, sqlCancel := context.WithTimeout(context.Background(), assetSQLTimeout)
	defer sqlCancel()
	queryStart := time.Now()
	if _, err := s.sqlConn.ExecCtx(sqlCtx, stmt, args...); err != nil {
		persistmetrics.RecordDBWrite(marketDBAssetsOp, persistmetrics.StatusError, len(validAssets), time.Since(queryStart))
		logx.WithContext(sqlCtx).Errorf("marketpersist: batch upsert failed provider=%s count=%d err=%v", provider, len(validAssets), err)
		return err
	}
	sqlDuration := time.Since(queryStart)
	persistmetrics.RecordDBWrite(marketDBAssetsOp, persistmetrics.StatusOK, len(validAssets), sqlDuration)

	cacheCtx, cacheCancel := context.WithTimeout(context.Background(), assetCacheTimeout)
	defer cacheCancel()
	cacheStart := time.Now()
	s.cacheAssets(cacheCtx, provider, validAssets)
	cacheDuration := time.Since(cacheStart)

	logx.WithContext(ctx).Infof("marketpersist: batch upserted assets provider=%s count=%d sql_duration=%dms cache_duration=%dms total_duration=%dms",
		provider, len(validAssets), sqlDuration.Milliseconds(), cacheDuration.Milliseconds(), time.Since(start).Milliseconds())
	return nil
}

// RecordSnapshot persists latest price/context data to Postgres + Redis.
func (s *Service) RecordSnapshot(ctx context.Context, provider string, snapshot *market.Snapshot) error {
	if s == nil || s.sqlConn == nil || snapshot == nil || strings.TrimSpace(snapshot.Symbol) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	provider = strings.TrimSpace(provider)
	symbol := strings.ToUpper(strings.TrimSpace(snapshot.Symbol))
	if provider == "" || symbol == "" {
		return nil
	}
	price := snapshot.Price.Last
	if !(price > 0) || math.IsNaN(price) || math.IsInf(price, 0) {
		return fmt.Errorf("marketpersist: invalid snapshot price provider=%s symbol=%s price=%v", provider, symbol, price)
	}
	now := time.Now().UTC()
	ctxDoc := buildMarketContextDocument(provider, symbol, snapshot, now)
	ctxJSON, err := json.Marshal(ctxDoc)
	if err != nil {
		return fmt.Errorf("marketpersist: marshal market context provider=%s symbol=%s: %w", provider, symbol, err)
	}
	txCtx, txCancel := context.WithTimeout(ctx, snapshotSQLTimeout)
	defer txCancel()
	txStart := time.Now()
	if err := s.sqlConn.TransactCtx(txCtx, func(txCtx context.Context, session sqlx.Session) error {
		if err := upsertPriceLatest(txCtx, session, provider, symbol, price, now); err != nil {
			return err
		}
		return upsertMarketAssetCtx(txCtx, session, provider, symbol, ctxJSON)
	}); err != nil {
		persistmetrics.RecordDBWrite(marketDBSnapshotOp, persistmetrics.StatusError, 2, time.Since(txStart))
		logx.WithContext(txCtx).Errorf("marketpersist: persist snapshot failed provider=%s symbol=%s err=%v", provider, symbol, err)
		return err
	}
	persistmetrics.RecordDBWrite(marketDBSnapshotOp, persistmetrics.StatusOK, 2, time.Since(txStart))
	s.cachePrice(ctx, provider, symbol, price, now)
	s.cacheMarketCtx(ctx, provider, symbol, snapshot, now)
	s.updateCryptoPrices(ctx, provider, symbol, price)
	return nil
}

// RecordPriceSeries persists historical ticks (typically OHLCV candles).
func (s *Service) RecordPriceSeries(ctx context.Context, provider string, symbol string, ticks []market.PriceTick) error {
	if s == nil || (s.sqlConn == nil && s.priceTicksModel == nil) {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	provider = strings.TrimSpace(provider)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if provider == "" || symbol == "" || len(ticks) == 0 {
		return nil
	}
	rows := make([]*model.PriceTicks, 0, len(ticks))
	for _, tick := range ticks {
		if tick.Timestamp.IsZero() || !(tick.Price > 0) {
			continue
		}
		row := &model.PriceTicks{
			Provider: provider,
			Symbol:   symbol,
			Price:    tick.Price,
			TsMs:     tick.Timestamp.UTC().UnixMilli(),
		}
		if tick.HasVolume {
			row.Volume = sql.NullFloat64{Float64: tick.Volume, Valid: true}
		}
		if raw := buildTickRaw(tick); raw.Valid {
			row.Raw = raw
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		persistmetrics.RecordDBWrite(marketDBPriceSeriesOp, persistmetrics.StatusSkip, 0, 0)
		return nil
	}
	start := time.Now()
	if s.sqlConn != nil {
		if err := s.insertPriceTicksBatch(ctx, rows); err != nil {
			persistmetrics.RecordDBWrite(marketDBPriceSeriesOp, persistmetrics.StatusError, len(rows), time.Since(start))
			return err
		}
		persistmetrics.RecordDBWrite(marketDBPriceSeriesOp, persistmetrics.StatusOK, len(rows), time.Since(start))
		return nil
	}
	for _, row := range rows {
		if _, err := s.priceTicksModel.Insert(ctx, row); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			persistmetrics.RecordDBWrite(marketDBPriceSeriesOp, persistmetrics.StatusError, len(rows), time.Since(start))
			return err
		}
	}
	persistmetrics.RecordDBWrite(marketDBPriceSeriesOp, persistmetrics.StatusOK, len(rows), time.Since(start))
	return nil
}

func (s *Service) insertPriceTicksBatch(ctx context.Context, rows []*model.PriceTicks) error {
	if s == nil || s.sqlConn == nil || len(rows) == 0 {
		return nil
	}
	valueClauses := make([]string, 0, len(rows))
	args := make([]any, 0, len(rows)*6)
	argIndex := 1
	for _, row := range rows {
		valueClauses = append(valueClauses, fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d::jsonb)", argIndex, argIndex+1, argIndex+2, argIndex+3, argIndex+4, argIndex+5))
		args = append(args, row.Provider, row.Symbol, row.Price, row.TsMs, row.Volume, row.Raw)
		argIndex += 6
	}
	stmt := fmt.Sprintf(`
INSERT INTO public.price_ticks (
    provider, symbol, price, ts_ms, volume, raw
) VALUES %s
ON CONFLICT DO NOTHING`, strings.Join(valueClauses, ", "))
	_, err := s.sqlConn.ExecCtx(ctx, stmt, args...)
	return err
}

// HydrateCaches reloads market cache keys from Postgres after process startup.
func (s *Service) HydrateCaches(ctx context.Context, providers []string) error {
	if s == nil || s.sqlConn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	filters := normalizeProviders(providers)
	hydrateCtx, cancel := context.WithTimeout(ctx, hydrateSQLTimeout)
	defer cancel()

	var errs []error
	if s.cache != nil {
		if err := s.hydrateLatestPrices(hydrateCtx, filters); err != nil {
			errs = append(errs, err)
		}
		if err := s.hydrateMarketContexts(hydrateCtx, filters); err != nil {
			errs = append(errs, err)
		}
	}
	if s.redis != nil {
		if err := s.hydrateMarketAssets(hydrateCtx, filters); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		logx.WithContext(ctx).Errorf("marketpersist: hydrate caches failed providers=%v err=%v", filters, err)
		return err
	}
	logx.WithContext(ctx).Infof("marketpersist: hydrated caches providers=%v", filters)
	return nil
}

func (s *Service) hydrateLatestPrices(ctx context.Context, providers []string) error {
	rows := make([]priceLatestHydrateRow, 0)
	query, args := buildProviderFilterQuery(`
SELECT provider, symbol, price, ts_ms
FROM public.price_latest`, `ORDER BY provider, symbol`, providers)
	if err := s.sqlConn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return err
	}
	cryptoPrices := make(map[string]float64, len(rows))
	for _, row := range rows {
		provider := strings.TrimSpace(row.Provider)
		symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
		if provider == "" || symbol == "" || !(row.Price > 0) {
			continue
		}
		recordedAt := time.UnixMilli(row.TsMs).UTC()
		s.cachePrice(ctx, provider, symbol, row.Price, recordedAt)
		cryptoPrices[fmt.Sprintf("%s:%s", provider, symbol)] = row.Price
	}
	s.cacheCryptoPrices(ctx, cryptoPrices)
	return nil
}

func (s *Service) hydrateMarketContexts(ctx context.Context, providers []string) error {
	rows := make([]marketContextHydrateRow, 0)
	query, args := buildProviderFilterQuery(`
SELECT provider, symbol, context, updated_at
FROM public.market_asset_ctx`, `ORDER BY provider, symbol`, providers)
	if err := s.sqlConn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return err
	}
	ttl := cachekeys.MarketAssetCtxTTL(s.ttl)
	if ttl <= 0 {
		return nil
	}
	for _, row := range rows {
		provider := strings.TrimSpace(row.Provider)
		symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
		if provider == "" || symbol == "" || strings.TrimSpace(row.Context) == "" {
			continue
		}
		payload, err := marketContextCachePayload(row.Context, row.UpdatedAt)
		if err != nil {
			logx.WithContext(ctx).Errorf("marketpersist: hydrate ctx decode provider=%s symbol=%s err=%v", provider, symbol, err)
			continue
		}
		key := cachekeys.MarketAssetCtxKey(provider, symbol)
		write := func(writeCtx context.Context) error {
			return s.cache.SetWithExpireCtx(writeCtx, key, payload, ttl)
		}
		if err := write(ctx); err != nil {
			s.handleCacheFailure(ctx, marketCacheContextOp, key, map[string]any{
				"provider":  provider,
				"symbol":    symbol,
				"cache_key": key,
				"source":    "hydrate",
			}, err, write)
		}
	}
	return nil
}

func (s *Service) hydrateMarketAssets(ctx context.Context, providers []string) error {
	rows := make([]marketAssetHydrateRow, 0)
	query, args := buildProviderFilterQuery(`
SELECT provider, symbol, name, sz_decimals, max_leverage, only_isolated, margin_table_id, is_delisted
FROM public.market_assets`, `ORDER BY provider, symbol`, providers)
	if err := s.sqlConn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return err
	}
	grouped := make(map[string][]market.Asset)
	for _, row := range rows {
		provider := strings.TrimSpace(row.Provider)
		symbol := strings.ToUpper(strings.TrimSpace(row.Symbol))
		if provider == "" || symbol == "" {
			continue
		}
		asset := market.Asset{
			Symbol:   symbol,
			IsActive: !row.IsDelisted,
			RawMetadata: map[string]any{
				"is_delisted": row.IsDelisted,
			},
		}
		if row.Name.Valid {
			asset.Base = row.Name.String
		}
		if row.SzDecimals.Valid {
			asset.Precision = int(row.SzDecimals.Int64)
			asset.RawMetadata["sz_decimals"] = row.SzDecimals.Int64
		}
		if row.MaxLeverage.Valid {
			asset.RawMetadata["maxLeverage"] = row.MaxLeverage.Float64
		}
		if row.OnlyIsolated.Valid {
			asset.RawMetadata["onlyIsolated"] = row.OnlyIsolated.Bool
		}
		if row.MarginTableID.Valid {
			asset.RawMetadata["marginTable"] = row.MarginTableID.Int64
		}
		grouped[provider] = append(grouped[provider], asset)
	}
	for provider, assets := range grouped {
		s.cacheAssets(ctx, provider, assets)
	}
	return nil
}

func (s *Service) cacheAssets(ctx context.Context, provider string, assets []market.Asset) {
	if s.redis == nil || len(assets) == 0 {
		return
	}

	key := cachekeys.MarketAssetKey(provider)
	ttl := s.ttl.Duration(cachekeys.TTLLong)
	if ttl <= 0 {
		ttl = cachekeys.MarketAssetTTL(s.ttl)
	}

	// Build hash fields map: symbol -> JSON payload
	fields := make(map[string]string, len(assets))
	for _, asset := range assets {
		if strings.TrimSpace(asset.Symbol) == "" {
			continue
		}

		// Build asset payload
		payload := map[string]any{
			"symbol":     asset.Symbol,
			"base":       asset.Base,
			"quote":      asset.Quote,
			"precision":  asset.Precision,
			"is_active":  asset.IsActive,
			"updated_at": time.Now().UTC().UnixMilli(),
		}
		if asset.RawMetadata != nil && len(asset.RawMetadata) > 0 {
			payload["metadata"] = asset.RawMetadata
		}

		// Convert to JSON
		data, err := json.Marshal(payload)
		if err != nil {
			logx.WithContext(ctx).Errorf("marketpersist: marshal asset failed symbol=%s err=%v", asset.Symbol, err)
			continue
		}
		fields[asset.Symbol] = string(data)
	}

	if len(fields) == 0 {
		return
	}

	write := func(writeCtx context.Context) error {
		if s.redis == nil {
			return nil
		}
		return s.redis.PipelinedCtx(writeCtx, func(pipe redis.Pipeliner) error {
			pipe.HMSet(writeCtx, key, fields)
			pipe.Expire(writeCtx, key, ttl)
			return nil
		})
	}
	if err := write(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCacheAssetsOp, key, map[string]any{
			"provider":  provider,
			"count":     len(fields),
			"cache_key": key,
		}, err, write)
	} else {
		persistmetrics.RecordCacheOp(marketCacheAssetsOp, persistmetrics.StatusOK, len(fields))
	}
}

func (s *Service) cachePrice(ctx context.Context, provider, symbol string, price float64, ts time.Time) {
	if s.cache == nil {
		return
	}
	ttl := cachekeys.PriceTTL(s.ttl)
	if ttl <= 0 {
		return
	}
	// Provider scoped key
	key := cachekeys.PriceLatestByProviderKey(provider, symbol)
	payload := map[string]any{
		"price": price,
		"ts":    ts.UnixMilli(),
	}
	writeProvider := func(writeCtx context.Context) error {
		return s.cache.SetWithExpireCtx(writeCtx, key, payload, ttl)
	}
	if err := writeProvider(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCachePriceOp, key, map[string]any{
			"provider":  provider,
			"symbol":    symbol,
			"cache_key": key,
			"scope":     "provider",
		}, err, writeProvider)
	} else {
		persistmetrics.RecordCacheOp(marketCachePriceOp, persistmetrics.StatusOK, 1)
	}
	// Global key
	global := cachekeys.PriceLatestKey(symbol)
	writeGlobal := func(writeCtx context.Context) error {
		return s.cache.SetWithExpireCtx(writeCtx, global, payload, ttl)
	}
	if err := writeGlobal(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCachePriceOp, global, map[string]any{
			"provider":  provider,
			"symbol":    symbol,
			"cache_key": global,
			"scope":     "global",
		}, err, writeGlobal)
	} else {
		persistmetrics.RecordCacheOp(marketCachePriceOp, persistmetrics.StatusOK, 1)
	}
}

func (s *Service) cacheMarketCtx(ctx context.Context, provider, symbol string, snapshot *market.Snapshot, recordedAt time.Time) {
	if s.cache == nil {
		return
	}
	ttl := cachekeys.MarketAssetCtxTTL(s.ttl)
	if ttl <= 0 {
		return
	}
	key := cachekeys.MarketAssetCtxKey(provider, symbol)
	ctxPayload := map[string]any{
		"price":        snapshot.Price.Last,
		"change":       snapshot.Change,
		"funding":      snapshot.Funding,
		"oi":           snapshot.OpenInterest,
		"indicators":   snapshot.Indicators,
		"timestamp_ms": recordedAt.UnixMilli(),
	}
	write := func(writeCtx context.Context) error {
		return s.cache.SetWithExpireCtx(writeCtx, key, ctxPayload, ttl)
	}
	if err := write(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCacheContextOp, key, map[string]any{
			"provider":  provider,
			"symbol":    symbol,
			"cache_key": key,
		}, err, write)
	} else {
		persistmetrics.RecordCacheOp(marketCacheContextOp, persistmetrics.StatusOK, 1)
	}
}

func (s *Service) updateCryptoPrices(ctx context.Context, provider, symbol string, price float64) {
	if s.cache == nil {
		return
	}
	key := cachekeys.CryptoPricesKey()
	field := fmt.Sprintf("%s:%s", provider, symbol)
	ttl := cachekeys.CryptoPricesTTL(s.ttl)
	if ttl <= 0 {
		return
	}
	write := func(writeCtx context.Context) error {
		var payload map[string]float64
		if err := s.cache.GetCtx(writeCtx, key, &payload); err != nil && !s.cache.IsNotFound(err) {
			return err
		}
		if payload == nil {
			payload = make(map[string]float64)
		}
		payload[field] = price
		return s.cache.SetWithExpireCtx(writeCtx, key, payload, ttl)
	}
	if err := write(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCacheCryptoPricesOp, key, map[string]any{
			"provider":  provider,
			"symbol":    symbol,
			"cache_key": key,
			"field":     field,
		}, err, write)
	} else {
		persistmetrics.RecordCacheOp(marketCacheCryptoPricesOp, persistmetrics.StatusOK, 1)
	}
}

func (s *Service) cacheCryptoPrices(ctx context.Context, payload map[string]float64) {
	if s.cache == nil || len(payload) == 0 {
		return
	}
	ttl := cachekeys.CryptoPricesTTL(s.ttl)
	if ttl <= 0 {
		return
	}
	key := cachekeys.CryptoPricesKey()
	write := func(writeCtx context.Context) error {
		return s.cache.SetWithExpireCtx(writeCtx, key, payload, ttl)
	}
	if err := write(ctx); err != nil {
		s.handleCacheFailure(ctx, marketCacheCryptoPricesOp, key, map[string]any{
			"cache_key": key,
			"count":     len(payload),
			"source":    "hydrate",
		}, err, write)
	} else {
		persistmetrics.RecordCacheOp(marketCacheCryptoPricesOp, persistmetrics.StatusOK, len(payload))
	}
}

func (s *Service) handleCacheFailure(ctx context.Context, operation, resource string, fields map[string]any, err error, retryFn func(context.Context) error) {
	if err == nil {
		return
	}
	classification := persistresilience.Classify(err)
	if classification.Class == persistresilience.FailureClassIgnore {
		return
	}
	persistmetrics.RecordCacheOp(operation, persistmetrics.StatusError, 1)
	if fields == nil {
		fields = map[string]any{}
	}
	fields["failure_class"] = classification.Class
	fields["failure_reason"] = classification.Reason

	if persistresilience.IsRetryContext(ctx) {
		logx.WithContext(ctx).Errorf("marketpersist: cache retry failed op=%s resource=%s fields=%v err=%v", operation, resource, fields, err)
		return
	}
	if classification.Class == persistresilience.FailureClassRetryAsync && s.retryQueue != nil && retryFn != nil {
		if s.retryQueue.Enqueue(ctx, persistresilience.Task{
			Operation: operation,
			Resource:  resource,
			Fields:    fields,
			Do:        retryFn,
		}) {
			logx.WithContext(ctx).Slowf("marketpersist: cache retry queued op=%s resource=%s fields=%v err=%v", operation, resource, fields, err)
			return
		}
		logx.WithContext(ctx).Errorf("marketpersist: cache retry queue saturated op=%s resource=%s fields=%v err=%v", operation, resource, fields, err)
		return
	}
	logx.WithContext(ctx).Errorf("marketpersist: cache write failed op=%s resource=%s fields=%v err=%v", operation, resource, fields, err)
}

func buildTickRaw(tick market.PriceTick) sql.NullString {
	payload := map[string]any{
		"interval": tick.Interval,
		"open":     tick.Open,
		"high":     tick.High,
		"low":      tick.Low,
		"close":    tick.Close,
	}
	if tick.HasVolume {
		payload["volume"] = tick.Volume
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: string(data), Valid: true}
}

func buildMarketContextDocument(provider, symbol string, snapshot *market.Snapshot, recordedAt time.Time) map[string]any {
	return map[string]any{
		"provider":       provider,
		"symbol":         symbol,
		"price":          snapshot.Price.Last,
		"change":         snapshot.Change,
		"funding":        snapshot.Funding,
		"open_interest":  snapshot.OpenInterest,
		"indicators":     snapshot.Indicators,
		"intraday":       snapshot.Intraday,
		"long_term":      snapshot.LongTerm,
		"recorded_at_ms": recordedAt.UnixMilli(),
	}
}

func upsertPriceLatest(ctx context.Context, session sqlx.Session, provider, symbol string, price float64, recordedAt time.Time) error {
	const query = `
INSERT INTO public.price_latest (
    provider, symbol, price, ts_ms, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, NOW(), NOW()
)
ON CONFLICT (provider, symbol) DO UPDATE SET
    price = EXCLUDED.price,
    ts_ms = EXCLUDED.ts_ms,
    updated_at = NOW()`
	_, err := session.ExecCtx(ctx, query, provider, symbol, price, recordedAt.UTC().UnixMilli())
	return err
}

func upsertMarketAssetCtx(ctx context.Context, session sqlx.Session, provider, symbol string, ctxJSON []byte) error {
	const query = `
INSERT INTO public.market_asset_ctx (
    provider, symbol, context, created_at, updated_at
) VALUES (
    $1, $2, $3::jsonb, NOW(), NOW()
)
ON CONFLICT (provider, symbol) DO UPDATE SET
    context = EXCLUDED.context,
    updated_at = NOW()`
	_, err := session.ExecCtx(ctx, query, provider, symbol, string(ctxJSON))
	return err
}

func nullFloatFromMeta(meta map[string]any, keys ...string) sql.NullFloat64 {
	for _, key := range keys {
		if v, ok := meta[key]; ok {
			if f, conv := toFloat64(v); conv {
				return sql.NullFloat64{Float64: f, Valid: true}
			}
		}
	}
	return sql.NullFloat64{}
}

func nullIntFromMeta(meta map[string]any, keys ...string) sql.NullInt64 {
	for _, key := range keys {
		if v, ok := meta[key]; ok {
			switch t := v.(type) {
			case int:
				return sql.NullInt64{Int64: int64(t), Valid: true}
			case int64:
				return sql.NullInt64{Int64: t, Valid: true}
			case float64:
				return sql.NullInt64{Int64: int64(t), Valid: true}
			case json.Number:
				if val, err := t.Int64(); err == nil {
					return sql.NullInt64{Int64: val, Valid: true}
				}
			}
		}
	}
	return sql.NullInt64{}
}

func nullBoolFromMeta(meta map[string]any, keys ...string) sql.NullBool {
	for _, key := range keys {
		if v, ok := meta[key]; ok {
			switch t := v.(type) {
			case bool:
				return sql.NullBool{Bool: t, Valid: true}
			case string:
				if strings.EqualFold(t, "true") {
					return sql.NullBool{Bool: true, Valid: true}
				}
				if strings.EqualFold(t, "false") {
					return sql.NullBool{Bool: false, Valid: true}
				}
			}
		}
	}
	return sql.NullBool{}
}

func toFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	pgErr, ok := err.(*pq.Error)
	return ok && pgErr.Code == "23505"
}

type priceLatestHydrateRow struct {
	Provider string  `db:"provider"`
	Symbol   string  `db:"symbol"`
	Price    float64 `db:"price"`
	TsMs     int64   `db:"ts_ms"`
}

type marketContextHydrateRow struct {
	Provider  string    `db:"provider"`
	Symbol    string    `db:"symbol"`
	Context   string    `db:"context"`
	UpdatedAt time.Time `db:"updated_at"`
}

type marketAssetHydrateRow struct {
	Provider      string          `db:"provider"`
	Symbol        string          `db:"symbol"`
	Name          sql.NullString  `db:"name"`
	SzDecimals    sql.NullInt64   `db:"sz_decimals"`
	MaxLeverage   sql.NullFloat64 `db:"max_leverage"`
	OnlyIsolated  sql.NullBool    `db:"only_isolated"`
	MarginTableID sql.NullInt64   `db:"margin_table_id"`
	IsDelisted    bool            `db:"is_delisted"`
}

func normalizeProviders(providers []string) []string {
	set := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		set[provider] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for provider := range set {
		out = append(out, provider)
	}
	sort.Strings(out)
	return out
}

func buildProviderFilterQuery(base, suffix string, providers []string) (string, []any) {
	base = strings.TrimSpace(base)
	suffix = strings.TrimSpace(suffix)
	if len(providers) == 0 {
		return strings.TrimSpace(base + "\n" + suffix), nil
	}
	return strings.TrimSpace(base + "\nWHERE provider = ANY($1)\n" + suffix), []any{pq.Array(providers)}
}

func marketContextCachePayload(raw string, updatedAt time.Time) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}
	payload := make(map[string]any, 6)
	copyIfPresent(payload, doc, "price", "price")
	copyIfPresent(payload, doc, "change", "change")
	copyIfPresent(payload, doc, "funding", "funding")
	copyIfPresent(payload, doc, "open_interest", "oi")
	copyIfPresent(payload, doc, "oi", "oi")
	copyIfPresent(payload, doc, "indicators", "indicators")
	if recorded, ok := numericValueFromMap(doc, "recorded_at_ms"); ok {
		payload["timestamp_ms"] = int64(recorded)
	} else if !updatedAt.IsZero() {
		payload["timestamp_ms"] = updatedAt.UTC().UnixMilli()
	}
	return payload, nil
}

func copyIfPresent(dst map[string]any, src map[string]any, srcKey, dstKey string) {
	if val, ok := src[srcKey]; ok {
		dst[dstKey] = val
	}
}

func numericValueFromMap(src map[string]any, key string) (float64, bool) {
	val, ok := src[key]
	if !ok {
		return 0, false
	}
	return toFloat64(val)
}
