package application

import (
	"context"
	"errors"
	"sync"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

// StoredResult is committed as one unit after an event is applied.
type StoredResult struct {
	Decision    protocol.HealthDecision
	EngineState core.EngineState
}

// ResultStore serializes apply callbacks across events and guarantees that an
// event decision and its resulting domain state are committed together at most
// once.
type ResultStore interface {
	ProcessOnce(
		ctx context.Context,
		eventID string,
		apply func() (StoredResult, error),
	) (StoredResult, error)
	LoadLatestState(ctx context.Context) (core.EngineState, bool, error)
}

type MemoryResultStore struct {
	mu             sync.Mutex
	results        map[string]StoredResult
	commandResults map[string]StoredCommandResult
	latestState    core.EngineState
	hasState       bool
}

func NewMemoryResultStore() *MemoryResultStore {
	return &MemoryResultStore{
		results:        make(map[string]StoredResult),
		commandResults: make(map[string]StoredCommandResult),
	}
}

func (s *MemoryResultStore) ProcessOnce(
	ctx context.Context,
	eventID string,
	apply func() (StoredResult, error),
) (StoredResult, error) {
	if eventID == "" {
		return StoredResult{}, errors.New("event ID is required")
	}
	if apply == nil {
		return StoredResult{}, errors.New("apply function is required")
	}
	if err := ctx.Err(); err != nil {
		return StoredResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if result, exists := s.results[eventID]; exists {
		return result, nil
	}
	result, err := apply()
	if err != nil {
		return StoredResult{}, err
	}
	s.results[eventID] = result
	s.latestState = result.EngineState
	s.hasState = true
	return result, nil
}

func (s *MemoryResultStore) LoadLatestState(ctx context.Context) (core.EngineState, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.EngineState{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.latestState, s.hasState, nil
}
