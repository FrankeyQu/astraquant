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

var _ ControlCommandsModel = (*customControlCommandsModel)(nil)

const controlCommandsRows = "id, command_type, target, decision_id, order_id, trader_id, action, requested_by, reason, idempotency_key, correlation_id, status, queued, control_plane_only, submitted, detail, created_at, updated_at"

type ControlCommands struct {
	Id               string         `db:"id"`
	CommandType      string         `db:"command_type"`
	Target           string         `db:"target"`
	DecisionId       sql.NullString `db:"decision_id"`
	OrderId          sql.NullString `db:"order_id"`
	TraderId         sql.NullString `db:"trader_id"`
	Action           string         `db:"action"`
	RequestedBy      string         `db:"requested_by"`
	Reason           string         `db:"reason"`
	IdempotencyKey   sql.NullString `db:"idempotency_key"`
	CorrelationId    string         `db:"correlation_id"`
	Status           string         `db:"status"`
	Queued           bool           `db:"queued"`
	ControlPlaneOnly bool           `db:"control_plane_only"`
	Submitted        bool           `db:"submitted"`
	Detail           string         `db:"detail"`
	CreatedAt        time.Time      `db:"created_at"`
	UpdatedAt        time.Time      `db:"updated_at"`
}

type ControlCommandsFilter struct {
	Target   string
	Status   string
	TraderID string
	Limit    int
	Offset   int
}

type (
	ControlCommandsModel interface {
		Insert(ctx context.Context, data *ControlCommands) error
		FindByIdempotency(ctx context.Context, target, targetID, action, idempotencyKey string) (*ControlCommands, error)
		List(ctx context.Context, filter ControlCommandsFilter) ([]*ControlCommands, error)
	}

	customControlCommandsModel struct {
		conn sqlx.SqlConn
	}
)

func NewControlCommandsModel(conn sqlx.SqlConn, _ cache.CacheConf, _ ...cache.Option) ControlCommandsModel {
	if conn == nil {
		return nil
	}
	return &customControlCommandsModel{conn: conn}
}

func (m *customControlCommandsModel) Insert(ctx context.Context, data *ControlCommands) error {
	if m == nil || m.conn == nil {
		return sql.ErrConnDone
	}
	if data == nil {
		return fmt.Errorf("control commands model: nil data")
	}
	if strings.TrimSpace(data.Detail) == "" {
		data.Detail = "{}"
	}
	createdAt := data.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := data.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	const query = `INSERT INTO public.control_commands (
    id, command_type, target, decision_id, order_id, trader_id, action, requested_by,
    reason, idempotency_key, correlation_id, status, queued, control_plane_only,
    submitted, detail, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14,
    $15, $16, $17, $18
)`
	_, err := m.conn.ExecCtx(
		ctx,
		query,
		data.Id,
		data.CommandType,
		data.Target,
		data.DecisionId,
		data.OrderId,
		data.TraderId,
		data.Action,
		data.RequestedBy,
		data.Reason,
		data.IdempotencyKey,
		data.CorrelationId,
		data.Status,
		data.Queued,
		data.ControlPlaneOnly,
		data.Submitted,
		data.Detail,
		createdAt.UTC(),
		updatedAt.UTC(),
	)
	return err
}

func (m *customControlCommandsModel) FindByIdempotency(ctx context.Context, target, targetID, action, idempotencyKey string) (*ControlCommands, error) {
	if m == nil || m.conn == nil {
		return nil, sql.ErrConnDone
	}
	target = strings.ToLower(strings.TrimSpace(target))
	targetID = strings.TrimSpace(targetID)
	action = strings.ToLower(strings.TrimSpace(action))
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if target == "" || targetID == "" || action == "" || idempotencyKey == "" {
		return nil, ErrNotFound
	}
	idColumn := "decision_id"
	if target == "order" {
		idColumn = "order_id"
	}
	query := fmt.Sprintf(`SELECT %s FROM public.control_commands
WHERE target = $1 AND %s = $2 AND action = $3 AND idempotency_key = $4
ORDER BY created_at DESC LIMIT 1`, controlCommandsRows, idColumn)
	var row ControlCommands
	if err := m.conn.QueryRowCtx(ctx, &row, query, target, targetID, action, idempotencyKey); err != nil {
		return nil, err
	}
	return &row, nil
}

func (m *customControlCommandsModel) List(ctx context.Context, filter ControlCommandsFilter) ([]*ControlCommands, error) {
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
	if v := strings.ToLower(strings.TrimSpace(filter.Target)); v != "" {
		add("target = $%d", v)
	}
	if v := strings.ToLower(strings.TrimSpace(filter.Status)); v != "" {
		add("status = $%d", v)
	}
	if v := strings.TrimSpace(filter.TraderID); v != "" {
		add("trader_id = $%d", v)
	}
	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, filter.Limit, filter.Offset)
	query := fmt.Sprintf(`SELECT %s FROM public.control_commands %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`, controlCommandsRows, where, len(args)-1, len(args))
	var rows []ControlCommands
	if err := m.conn.QueryRowsCtx(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	out := make([]*ControlCommands, 0, len(rows))
	for i := range rows {
		out = append(out, &rows[i])
	}
	return out, nil
}
