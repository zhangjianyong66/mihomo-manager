package daemon

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrOperationConflict = errors.New("operation conflict")

type Operation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
}

type Coordinator struct {
	mu      sync.Mutex
	current *Operation
	clock   func() time.Time
}

func NewCoordinator() *Coordinator { return &Coordinator{clock: time.Now} }

func (c *Coordinator) TryAcquire(ctx context.Context, id, kind string) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current != nil {
		return nil, ErrOperationConflict
	}
	op := &Operation{ID: id, Kind: kind, StartedAt: c.clock().UTC()}
	c.current = op
	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			if c.current == op {
				c.current = nil
			}
			c.mu.Unlock()
		})
	}, nil
}

func (c *Coordinator) Current() (Operation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.current == nil {
		return Operation{}, false
	}
	return *c.current, true
}
