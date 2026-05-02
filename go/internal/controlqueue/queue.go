package controlqueue

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	StatusQueued = "queued"

	TargetDecision = "decision"
	TargetOrder    = "order"
)

type Command struct {
	ID               string
	Type             string
	Target           string
	DecisionID       string
	OrderID          string
	TraderID         string
	Action           string
	RequestedBy      string
	Reason           string
	IdempotencyKey   string
	CorrelationID    string
	Status           string
	Queued           bool
	ControlPlaneOnly bool
	Submitted        bool
	CreatedAt        time.Time
}

type EnqueueRequest struct {
	Target         string
	DecisionID     string
	OrderID        string
	TraderID       string
	Action         string
	RequestedBy    string
	Reason         string
	IdempotencyKey string
	CorrelationID  string
	Now            time.Time
}

type EnqueueResult struct {
	Command Command
	Reused  bool
}

type Queue struct {
	mu                   sync.Mutex
	seq                  uint64
	commands             []Command
	byID                 map[string]Command
	idempotencyToID      map[string]string
	idToIdempotencyIndex map[string]string
}

func NewQueue() *Queue {
	return &Queue{
		byID:                 map[string]Command{},
		idempotencyToID:      map[string]string{},
		idToIdempotencyIndex: map[string]string{},
	}
}

func (q *Queue) Enqueue(req EnqueueRequest) EnqueueResult {
	if q == nil {
		q = NewQueue()
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	req = normalizeRequest(req)
	idemIndex := idempotencyIndex(req)
	if idemIndex != "" {
		if existingID := q.idempotencyToID[idemIndex]; existingID != "" {
			if existing, ok := q.byID[existingID]; ok {
				return EnqueueResult{Command: existing, Reused: true}
			}
		}
	}

	q.seq++
	id := commandID(req, q.seq)
	if req.CorrelationID == "" {
		req.CorrelationID = id
	}
	cmd := Command{
		ID:               id,
		Type:             commandType(req.Target, req.Action),
		Target:           req.Target,
		DecisionID:       req.DecisionID,
		OrderID:          req.OrderID,
		TraderID:         req.TraderID,
		Action:           req.Action,
		RequestedBy:      req.RequestedBy,
		Reason:           req.Reason,
		IdempotencyKey:   req.IdempotencyKey,
		CorrelationID:    req.CorrelationID,
		Status:           StatusQueued,
		Queued:           true,
		ControlPlaneOnly: true,
		Submitted:        false,
		CreatedAt:        req.Now,
	}
	q.commands = append(q.commands, cmd)
	q.byID[cmd.ID] = cmd
	if idemIndex != "" {
		q.idempotencyToID[idemIndex] = cmd.ID
		q.idToIdempotencyIndex[cmd.ID] = idemIndex
	}
	return EnqueueResult{Command: cmd}
}

func (q *Queue) Remove(id string) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	delete(q.byID, id)
	if idemIndex := q.idToIdempotencyIndex[id]; idemIndex != "" {
		delete(q.idempotencyToID, idemIndex)
		delete(q.idToIdempotencyIndex, id)
	}
	for i := range q.commands {
		if q.commands[i].ID == id {
			q.commands = append(q.commands[:i], q.commands[i+1:]...)
			return
		}
	}
}

func (q *Queue) Get(id string) (Command, bool) {
	if q == nil {
		return Command{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	cmd, ok := q.byID[strings.TrimSpace(id)]
	return cmd, ok
}

func (q *Queue) List() []Command {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]Command, len(q.commands))
	copy(out, q.commands)
	return out
}

func normalizeRequest(req EnqueueRequest) EnqueueRequest {
	req.Target = strings.ToLower(strings.TrimSpace(req.Target))
	req.DecisionID = strings.TrimSpace(req.DecisionID)
	req.OrderID = strings.TrimSpace(req.OrderID)
	req.TraderID = strings.TrimSpace(req.TraderID)
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.RequestedBy = strings.TrimSpace(req.RequestedBy)
	req.Reason = strings.TrimSpace(req.Reason)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.CorrelationID = strings.TrimSpace(req.CorrelationID)
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	} else {
		req.Now = req.Now.UTC()
	}
	return req
}

func idempotencyIndex(req EnqueueRequest) string {
	if req.IdempotencyKey == "" {
		return ""
	}
	return strings.Join([]string{req.Target, targetID(req), req.Action, req.IdempotencyKey}, "\x00")
}

func commandID(req EnqueueRequest, seq uint64) string {
	seed := fmt.Sprintf("%s|%s|%s|%s|%d|%d", req.Target, targetID(req), req.Action, req.IdempotencyKey, req.Now.UnixNano(), seq)
	sum := sha256.Sum256([]byte(seed))
	return "cmd-" + hex.EncodeToString(sum[:])[:20]
}

func commandType(target, action string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	action = strings.ToLower(strings.TrimSpace(action))
	if target == "" || action == "" {
		return strings.Trim(target+"_"+action, "_")
	}
	return target + "_" + action
}

func targetID(req EnqueueRequest) string {
	if req.Target == TargetOrder {
		return req.OrderID
	}
	return req.DecisionID
}
