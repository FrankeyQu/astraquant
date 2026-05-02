package repo

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"nof0-api/internal/model"
)

func TestControlCommandRepositoryEnqueueNormalizesContract(t *testing.T) {
	fake := &fakeControlCommandsModel{}
	repository := NewControlCommandRepository(fake)
	createdAt := time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC)

	record, reused, err := repository.Enqueue(context.Background(), ControlCommandRecord{
		Target:         " decision ",
		DecisionID:     " decision-1 ",
		TraderID:       " paper ",
		Action:         " APPROVE ",
		RequestedBy:    " operator ",
		Reason:         " manual approval ",
		IdempotencyKey: " idem-1 ",
		CorrelationID:  " corr-1 ",
		Detail:         []byte(`{"b":2,"a":1}`),
		CreatedAt:      createdAt,
	})

	require.NoError(t, err)
	require.False(t, reused)
	require.NotEmpty(t, record.ID)
	require.Equal(t, "decision_approve", record.Type)
	require.Equal(t, "decision", record.Target)
	require.Equal(t, "decision-1", record.DecisionID)
	require.Equal(t, "paper", record.TraderID)
	require.Equal(t, "approve", record.Action)
	require.Equal(t, "queued", record.Status)
	require.True(t, record.Queued)
	require.True(t, record.ControlPlaneOnly)
	require.False(t, record.Submitted)
	require.Equal(t, createdAt, record.CreatedAt)

	require.NotNil(t, fake.inserted)
	require.Equal(t, record.ID, fake.inserted.Id)
	require.Equal(t, `{"a":1,"b":2}`, fake.inserted.Detail)
	require.Equal(t, "decision-1", fake.inserted.DecisionId.String)
	require.False(t, fake.inserted.OrderId.Valid)
}

func TestControlCommandRepositoryReusesIdempotentCommand(t *testing.T) {
	createdAt := time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC)
	fake := &fakeControlCommandsModel{
		existing: &model.ControlCommands{
			Id:               "cmd-existing",
			CommandType:      "decision_approve",
			Target:           "decision",
			DecisionId:       sql.NullString{String: "decision-1", Valid: true},
			TraderId:         sql.NullString{String: "paper", Valid: true},
			Action:           "approve",
			RequestedBy:      "operator",
			Reason:           "manual approval",
			IdempotencyKey:   sql.NullString{String: "idem-1", Valid: true},
			CorrelationId:    "corr-existing",
			Status:           "queued",
			Queued:           true,
			ControlPlaneOnly: true,
			Submitted:        false,
			Detail:           "{}",
			CreatedAt:        createdAt,
			UpdatedAt:        createdAt,
		},
	}
	repository := NewControlCommandRepository(fake)

	record, reused, err := repository.Enqueue(context.Background(), ControlCommandRecord{
		Target:         "decision",
		DecisionID:     "decision-1",
		Action:         "approve",
		RequestedBy:    "operator",
		Reason:         "manual approval",
		IdempotencyKey: "idem-1",
	})

	require.NoError(t, err)
	require.True(t, reused)
	require.Equal(t, "cmd-existing", record.ID)
	require.Nil(t, fake.inserted)
	require.Equal(t, "decision", fake.findTarget)
	require.Equal(t, "decision-1", fake.findTargetID)
	require.Equal(t, "approve", fake.findAction)
	require.Equal(t, "idem-1", fake.findKey)
}

func TestControlCommandRepositoryRequiresTargetID(t *testing.T) {
	repository := NewControlCommandRepository(&fakeControlCommandsModel{})

	_, _, err := repository.Enqueue(context.Background(), ControlCommandRecord{
		Target:      "decision",
		Action:      "approve",
		RequestedBy: "operator",
		Reason:      "missing id",
	})

	require.ErrorContains(t, err, "decision id required")
}

func TestControlCommandRepositoryClaimsAndCompletesCommands(t *testing.T) {
	createdAt := time.Date(2026, 5, 2, 1, 2, 3, 0, time.UTC)
	fake := &fakeControlCommandsModel{
		listRows: []*model.ControlCommands{
			{
				Id:               "cmd-1",
				CommandType:      "decision_approve",
				Target:           "decision",
				DecisionId:       sql.NullString{String: "decision-1", Valid: true},
				Action:           "approve",
				RequestedBy:      "operator",
				Reason:           "run",
				CorrelationId:    "corr-1",
				Status:           "processing",
				Queued:           true,
				ControlPlaneOnly: true,
				Submitted:        false,
				Detail:           "{}",
				CreatedAt:        createdAt,
				UpdatedAt:        createdAt,
			},
		},
	}
	repository := NewControlCommandRepository(fake)

	claimed, err := repository.ClaimQueued(context.Background(), 5)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "cmd-1", claimed[0].ID)
	require.Equal(t, 5, fake.claimLimit)

	err = repository.Complete(context.Background(), "cmd-1", true, []byte(`{"ok":true}`))
	require.NoError(t, err)
	require.Equal(t, "cmd-1", fake.updatedID)
	require.Equal(t, ControlCommandStatusCompleted, fake.updatedStatus)
	require.True(t, fake.updatedSubmitted)
	require.Contains(t, fake.updatedDetail, `"ok":true`)
	require.Contains(t, fake.updatedDetail, "terminal_at")
}

func TestControlCommandRepositoryFailsWithTerminalReason(t *testing.T) {
	fake := &fakeControlCommandsModel{}
	repository := NewControlCommandRepository(fake)

	err := repository.Fail(context.Background(), "cmd-1", "policy rejected", []byte(`{"error":"size"}`))

	require.NoError(t, err)
	require.Equal(t, ControlCommandStatusFailed, fake.updatedStatus)
	require.False(t, fake.updatedSubmitted)
	require.Contains(t, fake.updatedDetail, `"error":"size"`)
	require.Contains(t, fake.updatedDetail, `"terminal_reason":"policy rejected"`)
}

type fakeControlCommandsModel struct {
	inserted         *model.ControlCommands
	existing         *model.ControlCommands
	listRows         []*model.ControlCommands
	listFilter       model.ControlCommandsFilter
	err              error
	claimLimit       int
	updatedID        string
	updatedStatus    string
	updatedSubmitted bool
	updatedDetail    string
	findTarget       string
	findTargetID     string
	findAction       string
	findKey          string
}

func (m *fakeControlCommandsModel) Insert(_ context.Context, data *model.ControlCommands) error {
	if m.err != nil {
		return m.err
	}
	if data == nil {
		return errors.New("nil data")
	}
	m.inserted = data
	return nil
}

func (m *fakeControlCommandsModel) FindByIdempotency(_ context.Context, target, targetID, action, idempotencyKey string) (*model.ControlCommands, error) {
	m.findTarget = target
	m.findTargetID = targetID
	m.findAction = action
	m.findKey = idempotencyKey
	if m.err != nil {
		return nil, m.err
	}
	if m.existing == nil {
		return nil, model.ErrNotFound
	}
	return m.existing, nil
}

func (m *fakeControlCommandsModel) List(_ context.Context, filter model.ControlCommandsFilter) ([]*model.ControlCommands, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.listFilter = filter
	return m.listRows, nil
}

func (m *fakeControlCommandsModel) ClaimQueued(_ context.Context, limit int) ([]*model.ControlCommands, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.claimLimit = limit
	return m.listRows, nil
}

func (m *fakeControlCommandsModel) UpdateStatus(_ context.Context, id, status string, submitted bool, detail string) error {
	if m.err != nil {
		return m.err
	}
	m.updatedID = id
	m.updatedStatus = status
	m.updatedSubmitted = submitted
	m.updatedDetail = detail
	return nil
}
