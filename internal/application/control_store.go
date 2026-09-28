package application

import (
	"context"
	"errors"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

type StoredCommandResult struct {
	Response    protocol.HealthControlResponse
	EngineState core.EngineState
}

// CommandStore serializes control commands with every other state mutation and
// commits a command response together with the resulting Engine state.
type CommandStore interface {
	ProcessCommandOnce(
		ctx context.Context,
		commandID string,
		apply func() (StoredCommandResult, error),
	) (StoredCommandResult, error)
}

// Store is the complete persistence boundary used by the daemon application
// service. Implementations must serialize event and command callbacks globally.
type Store interface {
	ResultStore
	CommandStore
}

func (s *MemoryResultStore) ProcessCommandOnce(
	ctx context.Context,
	commandID string,
	apply func() (StoredCommandResult, error),
) (StoredCommandResult, error) {
	if commandID == "" {
		return StoredCommandResult{}, errors.New("command ID is required")
	}
	if apply == nil {
		return StoredCommandResult{}, errors.New("apply function is required")
	}
	if err := ctx.Err(); err != nil {
		return StoredCommandResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if result, exists := s.commandResults[commandID]; exists {
		return result, nil
	}
	result, err := apply()
	if err != nil {
		return StoredCommandResult{}, err
	}
	s.commandResults[commandID] = result
	s.latestState = result.EngineState
	s.hasState = true
	return result, nil
}
