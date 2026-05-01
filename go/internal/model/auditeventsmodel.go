package model

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

var _ AuditEventsModel = (*customAuditEventsModel)(nil)

const auditEventsRows = "id, event_type, trader_id, cycle_id, correlation_id, symbol, action, model_id, model_name, prompt_digest, approval_token_id, reason, error_message, detail, created_at"

// AuditEvents represents one immutable audit event.
type AuditEvents struct {
	Id              int64          `db:"id"`
	EventType       string         `db:"event_type"`
	TraderId        string         `db:"trader_id"`
	CycleId         sql.NullInt64  `db:"cycle_id"`
	CorrelationId   sql.NullString `db:"correlation_id"`
	Symbol          sql.NullString `db:"symbol"`
	Action          sql.NullString `db:"action"`
	ModelId         sql.NullString `db:"model_id"`
	ModelName       sql.NullString `db:"model_name"`
	PromptDigest    sql.NullString `db:"prompt_digest"`
	ApprovalTokenId sql.NullString `db:"approval_token_id"`
	Reason          sql.NullString `db:"reason"`
	ErrorMessage    sql.NullString `db:"error_message"`
	Detail          string         `db:"detail"`
	CreatedAt       time.Time      `db:"created_at"`
}

type (
	// AuditEventsFilter captures supported query constraints for audit event reads.
	AuditEventsFilter struct {
		TraderID      string
		EventType     string
		CorrelationID string
		CreatedAfter  *time.Time
		CreatedBefore *time.Time
		Limit         int
		Offset        int
	}

	// AuditEventsModel is an interface to be customized, add more methods here,
	// and implement the added methods in customAuditEventsModel.
	AuditEventsModel interface {
		Insert(ctx context.Context, data *AuditEvents) (int64, error)
		List(ctx context.Context, filter AuditEventsFilter) ([]*AuditEvents, error)
		ListByTrader(ctx context.Context, traderID string, limit int) ([]*AuditEvents, error)
	}

	customAuditEventsModel struct {
		conn sqlx.SqlConn
	}
)

// NewAuditEventsModel returns a model for the database table.
func NewAuditEventsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) AuditEventsModel {
	if conn == nil {
		return nil
	}
	return &customAuditEventsModel{conn: conn}
}

func (m *customAuditEventsModel) Insert(ctx context.Context, data *AuditEvents) (int64, error) {
	if m == nil || m.conn == nil {
		return 0, sql.ErrConnDone
	}
	if data == nil {
		return 0, fmt.Errorf("audit events model: nil data")
	}
	if strings.TrimSpace(data.Detail) == "" {
		data.Detail = "{}"
	}
	createdAt := data.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	query := `INSERT INTO public.audit_events (
    event_type, trader_id, cycle_id, correlation_id, symbol, action, model_id, model_name,
    prompt_digest, approval_token_id, reason, error_message, detail, created_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING id`
	var id int64
	err := m.conn.QueryRowCtx(
		ctx,
		&id,
		query,
		data.EventType,
		data.TraderId,
		data.CycleId,
		data.CorrelationId,
		data.Symbol,
		data.Action,
		data.ModelId,
		data.ModelName,
		data.PromptDigest,
		data.ApprovalTokenId,
		data.Reason,
		data.ErrorMessage,
		data.Detail,
		createdAt.UTC(),
	)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (m *customAuditEventsModel) ListByTrader(ctx context.Context, traderID string, limit int) ([]*AuditEvents, error) {
	return m.List(ctx, AuditEventsFilter{TraderID: traderID, Limit: limit})
}

func (m *customAuditEventsModel) List(ctx context.Context, filter AuditEventsFilter) ([]*AuditEvents, error) {
	if m == nil || m.conn == nil {
		return nil, sql.ErrConnDone
	}
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	var clauses []string
	var args []any
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}
	if v := strings.TrimSpace(filter.TraderID); v != "" {
		add("trader_id = $%d", v)
	}
	if v := strings.TrimSpace(filter.EventType); v != "" {
		add("event_type = $%d", v)
	}
	if v := strings.TrimSpace(filter.CorrelationID); v != "" {
		add("correlation_id = $%d", v)
	}
	if filter.CreatedAfter != nil {
		add("created_at >= $%d", filter.CreatedAfter.UTC())
	}
	if filter.CreatedBefore != nil {
		add("created_at <= $%d", filter.CreatedBefore.UTC())
	}

	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(`SELECT %s FROM public.audit_events %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, auditEventsRows, where, len(args)-1, len(args))
	var rows []AuditEvents
	if err := m.conn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make([]*AuditEvents, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}
