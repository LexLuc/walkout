package application

import (
	"context"
	"errors"

	"github.com/LexLuc/walkout/internal/core"
)

// RestoreEngine constructs the domain aggregate from the latest committed
// state. An empty store starts a fresh aggregate; invalid durable state is
// returned as a diagnostic error so the host adapter can fail open.
func RestoreEngine(ctx context.Context, config core.Config, clock core.Clock, store ResultStore) (*core.Engine, error) {
	if store == nil {
		return nil, errors.New("result store is required")
	}

	state, exists, err := store.LoadLatestState(ctx)
	if err != nil {
		return nil, err
	}
	if !exists {
		return core.NewEngine(config, clock)
	}
	return core.NewEngineFromState(config, clock, state)
}
