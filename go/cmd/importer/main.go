package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"

	"nof0-api/internal/data"
	// internal types for mapping JSON to DB rows
	"nof0-api/internal/types"
)

func main() {
	var (
		dsn      string
		dataPath string
		truncate bool
	)
	flag.StringVar(&dsn, "dsn", "postgres://nof0:nof0@localhost:5432/nof0?sslmode=disable", "Postgres DSN")
	flag.StringVar(&dataPath, "data", "../mcp/data", "Path to MCP data directory")
	flag.BoolVar(&truncate, "truncate", false, "Truncate destination tables before import")
	flag.Parse()

	ctx := context.Background()
	conn := sqlx.NewSqlConn("pgx", dsn)
	logx.Infof("connecting to %s", dsn)

	if truncate {
		mustExec(ctx, conn, `TRUNCATE TABLE conversation_messages, conversations, model_analytics, trades, positions, account_equity_snapshots, accounts, price_ticks, symbols, models RESTART IDENTITY CASCADE`)
	}

	// Use existing DataLoader to parse JSON
	dl := data.NewDataLoader(dataPath)

	// Collect models and symbols encountered for upsert
	modelSet := map[string]struct{}{}
	symbolSet := map[string]struct{}{}

	// 1) Crypto prices were previously stored in price_latest; Redis now serves as source of truth.
	if _, err := dl.LoadCryptoPrices(); err == nil {
		log.Printf("skip crypto prices: table removed (Redis-only)")
	} else {
		log.Printf("skip crypto prices: %v", err)
	}

	// 2) Since inception: current JSON不包含时间序列，跳过导入（由后续ETL产出再导入）。
	if _, err := dl.LoadSinceInception(); err == nil {
		log.Printf("skip since-inception: design expects timeseries; source has summary only")
	}

	// 3) Account totals -> account_equity_snapshots
	if resp, err := dl.LoadAccountTotals(); err == nil {
		imported := 0
		for _, total := range resp.AccountTotals {
			modelID := strings.TrimSpace(total.ModelId)
			if modelID == "" {
				continue
			}
			modelSet[modelID] = struct{}{}
			upsertModel(ctx, conn, modelID, modelID)
			insertEquitySnapshot(ctx, conn, &total)
			imported++
		}
		log.Printf("imported account totals: %d", imported)
	} else {
		log.Printf("skip account totals: %v", err)
	}

	// 4) Trades -> trades (+models, +symbols)
	if resp, err := dl.LoadTrades(); err == nil {
		for _, t := range resp.Trades {
			if t.ModelId != "" {
				modelSet[t.ModelId] = struct{}{}
				upsertModel(ctx, conn, t.ModelId, t.ModelId)
			}
			if t.Symbol != "" {
				symbolSet[t.Symbol] = struct{}{}
				upsertSymbol(ctx, conn, t.Symbol)
			}
			entryMs := toMsF(t.EntryTime)
			exitMs := toMsF(t.ExitTime)
			insertTrade(ctx, conn, &t, entryMs, exitMs)
		}
		log.Printf("imported trades: %d", len(resp.Trades))
	} else {
		log.Printf("skip trades: %v", err)
	}

	// 5) Positions -> positions (open)
	if resp, err := dl.LoadPositions(); err == nil {
		for _, pm := range resp.AccountTotals {
			if pm.ModelId != "" {
				modelSet[pm.ModelId] = struct{}{}
				upsertModel(ctx, conn, pm.ModelId, pm.ModelId)
			}
			for sym, pos := range pm.Positions {
				symbolSet[sym] = struct{}{}
				upsertSymbol(ctx, conn, sym)
				entryMs := toMsF(pos.EntryTime)
				pv := positionView{
					EntryPrice:    pos.EntryPrice,
					Quantity:      pos.Quantity,
					Leverage:      pos.Leverage,
					Confidence:    pos.Confidence,
					RiskUsd:       pos.RiskUsd,
					UnrealizedPnl: pos.UnrealizedPnl,
				}
				insertPositionOpen(ctx, conn, pm.ModelId, sym, pv, entryMs)
			}
		}
		log.Printf("imported positions: %d models", len(resp.AccountTotals))
	} else {
		log.Printf("skip positions: %v", err)
	}

	// 6) Analytics -> model_analytics payload cache
	if resp, err := dl.LoadAnalytics(); err == nil {
		imported := 0
		for _, analytics := range resp.Analytics {
			modelID := strings.TrimSpace(analytics.ModelId)
			if modelID == "" {
				modelID = strings.TrimSpace(analytics.Id)
			}
			if modelID == "" {
				continue
			}
			analytics.ModelId = modelID
			if analytics.Id == "" {
				analytics.Id = modelID
			}
			modelSet[modelID] = struct{}{}
			upsertModel(ctx, conn, modelID, modelID)
			upsertModelAnalytics(ctx, conn, &analytics, resp.ServerTime)
			imported++
		}
		log.Printf("imported analytics: %d", imported)
	} else {
		log.Printf("skip analytics: %v", err)
	}

	// 7) Conversations -> conversations & messages
	if resp, err := dl.LoadConversations(); err == nil {
		for _, c := range resp.Conversations {
			if c.ModelId != "" {
				modelSet[c.ModelId] = struct{}{}
				upsertModel(ctx, conn, c.ModelId, c.ModelId)
			}
			convID := insertConversation(ctx, conn, c.ModelId)
			for _, m := range c.Messages {
				ts := toMs(m.Timestamp)
				insertConversationMessage(ctx, conn, convID, m.Role, m.Content, ts)
			}
		}
		log.Printf("imported conversations: %d", len(resp.Conversations))
	} else {
		log.Printf("skip conversations: %v", err)
	}

	log.Printf("models upserted: %d, symbols upserted: %d", len(modelSet), len(symbolSet))
	log.Printf("done.")
}

func toMs(v interface{}) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		// some JSON times are seconds, others ms; heuristic: if < 1e12 treat as seconds
		if t < 1e12 {
			return int64(t * 1000)
		}
		return int64(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
		return 0
	case string:
		// iso8601 support best-effort
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return ts.UnixMilli()
		}
		return 0
	default:
		return 0
	}
}

func toMsF(f float64) int64 {
	if f < 1e12 {
		return int64(f * 1000)
	}
	return int64(f)
}

func mustExec(ctx context.Context, conn sqlx.SqlConn, query string, args ...interface{}) {
	if _, err := conn.ExecCtx(ctx, query, args...); err != nil {
		log.Fatalf("exec failed: %v", err)
	}
}

func upsertModel(ctx context.Context, conn sqlx.SqlConn, id, display string) {
	q := `INSERT INTO models(id, provider, name, detail)
          VALUES ($1, 'snapshot', $2, '{}'::jsonb)
          ON CONFLICT (id) DO UPDATE SET
            name = EXCLUDED.name,
            provider = EXCLUDED.provider`
	mustExec(ctx, conn, q, strings.TrimSpace(id), strings.TrimSpace(display))
}

func upsertSymbol(ctx context.Context, conn sqlx.SqlConn, symbol string) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}
	q := `INSERT INTO symbols(id, exchange_provider, symbol)
          VALUES ($1, 'snapshot', $2)
          ON CONFLICT (exchange_provider, symbol) DO NOTHING`
	mustExec(ctx, conn, q, "snapshot/"+symbol, symbol)
}

func insertEquitySnapshot(ctx context.Context, conn sqlx.SqlConn, total *types.AccountTotal) {
	if total == nil || strings.TrimSpace(total.ModelId) == "" {
		return
	}
	metadata := map[string]any{
		"source":           "mcp_snapshot",
		"account_total_id": total.Id,
		"positions_count":  len(total.Positions),
	}
	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		log.Fatalf("marshal account snapshot metadata: %v", err)
	}
	q := `INSERT INTO account_equity_snapshots(
            model_id, ts_ms, dollar_equity, realized_pnl, total_unrealized_pnl,
            metadata, cum_pnl_pct, sharpe_ratio, since_inception_hourly_marker,
            since_inception_minute_marker)
          VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10)
          ON CONFLICT (model_id, ts_ms) DO UPDATE SET
            dollar_equity = EXCLUDED.dollar_equity,
            realized_pnl = EXCLUDED.realized_pnl,
            total_unrealized_pnl = EXCLUDED.total_unrealized_pnl,
            metadata = EXCLUDED.metadata,
            cum_pnl_pct = EXCLUDED.cum_pnl_pct,
            sharpe_ratio = EXCLUDED.sharpe_ratio,
            since_inception_hourly_marker = EXCLUDED.since_inception_hourly_marker,
            since_inception_minute_marker = EXCLUDED.since_inception_minute_marker`
	mustExec(ctx, conn, q,
		strings.TrimSpace(total.ModelId),
		toMsF(total.Timestamp),
		total.DollarEquity,
		total.RealizedPnl,
		total.TotalUnrealizedPnl,
		string(metaJSON),
		total.CumPnlPct,
		total.SharpeRatio,
		total.SinceInceptionHourlyMarker,
		total.SinceInceptionMinuteMarker,
	)
}

func insertTrade(ctx context.Context, conn sqlx.SqlConn, t *types.Trade, entryMs, exitMs int64) {
	if t == nil || strings.TrimSpace(t.ModelId) == "" || strings.TrimSpace(t.Symbol) == "" {
		return
	}
	detail := tradeDetailJSON(t, entryMs, exitMs)
	q := `INSERT INTO trades(id, trader_id, symbol, side, close_ts_ms, detail)
          VALUES ($1,$2,$3,$4,$5,$6::jsonb)
          ON CONFLICT (id) DO UPDATE SET
            trader_id = EXCLUDED.trader_id,
            symbol = EXCLUDED.symbol,
            side = EXCLUDED.side,
            close_ts_ms = EXCLUDED.close_ts_ms,
            detail = EXCLUDED.detail,
            updated_at = NOW()`
	mustExec(ctx, conn, q,
		firstNonEmpty(t.Id, fmt.Sprintf("%s:%s:%d", t.ModelId, strings.ToUpper(t.Symbol), exitMs)),
		strings.TrimSpace(t.ModelId),
		strings.ToUpper(strings.TrimSpace(t.Symbol)),
		normalizeSide(t.Side, t.Quantity),
		exitMs,
		detail,
	)
}

func upsertModelAnalytics(ctx context.Context, conn sqlx.SqlConn, analytics *types.ModelAnalytics, serverTimeMs int64) {
	payload, err := json.Marshal(analytics)
	if err != nil {
		log.Fatalf("marshal analytics payload: %v", err)
	}
	q := `INSERT INTO model_analytics(model_id, payload, server_time_ms, metadata, updated_at)
          VALUES ($1, $2::jsonb, $3, '{}'::jsonb, COALESCE(to_timestamp(NULLIF($4, 0)), NOW()))
          ON CONFLICT (model_id) DO UPDATE SET
            payload = EXCLUDED.payload,
            server_time_ms = EXCLUDED.server_time_ms,
            metadata = EXCLUDED.metadata,
            updated_at = EXCLUDED.updated_at`
	mustExec(ctx, conn, q, analytics.ModelId, string(payload), serverTimeMs, analytics.UpdatedAt)
}

type positionView struct {
	EntryPrice    float64 `json:"entry_price"`
	Quantity      float64 `json:"quantity"`
	Leverage      float64 `json:"leverage"`
	Confidence    float64 `json:"confidence"`
	RiskUsd       float64 `json:"risk_usd"`
	UnrealizedPnl float64 `json:"unrealized_pnl"`
}

func insertPositionOpen(ctx context.Context, conn sqlx.SqlConn, modelId, symbol string, pos positionView, entryMs int64) {
	modelId = strings.TrimSpace(modelId)
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if modelId == "" || symbol == "" {
		return
	}
	detail := positionDetailJSON(pos, entryMs)
	q := `INSERT INTO positions(id, trader_id, symbol, side, status, detail)
          VALUES ($1,$2,$3,$4,'open',$5::jsonb)
          ON CONFLICT (id) DO UPDATE SET
            trader_id = EXCLUDED.trader_id,
            symbol = EXCLUDED.symbol,
            side = EXCLUDED.side,
            status = 'open',
            detail = EXCLUDED.detail,
            updated_at = NOW()`
	pid := fmt.Sprintf("%s:%s:%d", modelId, symbol, entryMs)
	mustExec(ctx, conn, q, pid, modelId, symbol, normalizeSide("", pos.Quantity), detail)
}

func insertConversation(ctx context.Context, conn sqlx.SqlConn, modelId string) int64 {
	q := `INSERT INTO conversations(model_id, topic, created_at) VALUES ($1, $2, NOW()) RETURNING id`
	var id int64
	if err := conn.QueryRowCtx(ctx, &id, q, strings.TrimSpace(modelId), "snapshot import"); err != nil {
		log.Fatalf("insert conversation: %v", err)
	}
	return id
}

func insertConversationMessage(ctx context.Context, conn sqlx.SqlConn, convId int64, role, content string, ts int64) {
	if role == "" {
		role = "assistant"
	}
	q := `INSERT INTO conversation_messages(conversation_id, role, content, ts_ms, metadata, created_at)
          VALUES ($1,$2,$3,$4,'{}',NOW())`
	mustExec(ctx, conn, q, convId, role, content, nullInt(ts))
}

func tradeDetailJSON(t *types.Trade, entryMs, exitMs int64) string {
	payload := map[string]any{
		"time": map[string]any{
			"open_ts_ms":  entryMs,
			"close_ts_ms": exitMs,
		},
		"prices": map[string]any{
			"entry": t.EntryPrice,
			"exit":  t.ExitPrice,
		},
		"quantity": map[string]any{
			"total": absFloat(t.Quantity),
		},
		"risk": map[string]any{
			"confidence": t.Confidence,
			"leverage":   t.Leverage,
		},
		"exchange": map[string]any{
			"provider": "snapshot",
		},
		"pnl": map[string]any{
			"gross": t.RealizedGrossPnl,
			"net":   t.RealizedNetPnl,
		},
		"model_id":                 t.ModelId,
		"symbol":                   strings.ToUpper(strings.TrimSpace(t.Symbol)),
		"side":                     normalizeSide(t.Side, t.Quantity),
		"trade_type":               firstNonEmpty(t.TradeType, normalizeSide(t.Side, t.Quantity)),
		"trade_id":                 t.TradeId,
		"leverage":                 t.Leverage,
		"confidence":               t.Confidence,
		"entry_price":              t.EntryPrice,
		"exit_price":               t.ExitPrice,
		"entry_time":               t.EntryTime,
		"exit_time":                t.ExitTime,
		"entry_human_time":         t.EntryHumanTime,
		"exit_human_time":          t.ExitHumanTime,
		"entry_sz":                 t.EntrySz,
		"exit_sz":                  t.ExitSz,
		"entry_tid":                t.EntryTid,
		"exit_tid":                 t.ExitTid,
		"entry_oid":                t.EntryOid,
		"exit_oid":                 t.ExitOid,
		"entry_crossed":            t.EntryCrossed,
		"exit_crossed":             t.ExitCrossed,
		"entry_liquidation":        t.EntryLiquidation,
		"exit_liquidation":         t.ExitLiquidation,
		"entry_commission_dollars": t.EntryCommissionDollars,
		"exit_commission_dollars":  t.ExitCommissionDollars,
		"entry_closed_pnl":         t.EntryClosedPnl,
		"exit_closed_pnl":          t.ExitClosedPnl,
		"exit_plan":                t.ExitPlan,
		"realized_gross_pnl":       t.RealizedGrossPnl,
		"realized_net_pnl":         t.RealizedNetPnl,
		"total_commission_dollars": t.TotalCommissionDollars,
	}
	return mustMarshalJSON(payload, "trade detail")
}

func positionDetailJSON(pos positionView, entryMs int64) string {
	payload := map[string]any{
		"entry": map[string]any{
			"price":    pos.EntryPrice,
			"quantity": absFloat(pos.Quantity),
			"time_ms":  entryMs,
			"leverage": pos.Leverage,
		},
		"exchange": map[string]any{
			"provider": "snapshot",
		},
		"risk": map[string]any{
			"confidence": pos.Confidence,
			"risk_usd":   firstPositiveFloat(pos.RiskUsd, absFloat(pos.Quantity)*pos.EntryPrice/maxFloat(pos.Leverage, 1)),
		},
		"metrics": map[string]any{
			"unrealized_pnl": pos.UnrealizedPnl,
		},
	}
	return mustMarshalJSON(payload, "position detail")
}

func mustMarshalJSON(payload any, label string) string {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("marshal %s: %v", label, err)
	}
	return string(data)
}

func normalizeSide(side string, quantity float64) string {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case "short":
		return "short"
	case "long":
		return "long"
	default:
		if quantity < 0 {
			return "short"
		}
		return "long"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullInt(v int64) interface{} {
	if v == 0 {
		return nil
	}
	return v
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func firstPositiveFloat(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
