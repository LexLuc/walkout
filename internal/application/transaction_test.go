package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

var errSimulatedCommit = errors.New("simulated commit failure")

type commitFailStore struct{}

func (commitFailStore) ProcessOnce(
	_ context.Context,
	_ string,
	apply func() (StoredResult, error),
) (StoredResult, error) {
	if _, err := apply(); err != nil {
		return StoredResult{}, err
	}
	return StoredResult{}, errSimulatedCommit
}

func (commitFailStore) LoadLatestState(context.Context) (core.EngineState, bool, error) {
	return core.EngineState{}, false, nil
}

type orderedCommitStore struct {
	firstEntered  chan struct{}
	secondEntered chan struct{}
	allowFirst    chan struct{}
	firstDone     chan struct{}

	mu              sync.Mutex
	successfulState core.EngineState
}

func newOrderedCommitStore() *orderedCommitStore {
	return &orderedCommitStore{
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		allowFirst:    make(chan struct{}),
		firstDone:     make(chan struct{}),
	}
}

func (s *orderedCommitStore) ProcessOnce(
	_ context.Context,
	eventID string,
	apply func() (StoredResult, error),
) (StoredResult, error) {
	switch eventID {
	case "first":
		close(s.firstEntered)
		<-s.allowFirst
		result, err := apply()
		if err == nil {
			s.mu.Lock()
			s.successfulState = result.EngineState
			s.mu.Unlock()
		}
		close(s.firstDone)
		return result, err
	case "second":
		close(s.secondEntered)
		<-s.firstDone
		if _, err := apply(); err != nil {
			return StoredResult{}, err
		}
		return StoredResult{}, errSimulatedCommit
	default:
		return StoredResult{}, errors.New("unexpected event ID")
	}
}

func (s *orderedCommitStore) LoadLatestState(context.Context) (core.EngineState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successfulState, s.successfulState.SchemaVersion != "", nil
}

func TestCommitFailureRollsBackTheEngine(t *testing.T) {
	t.Parallel()

	engine := mustApplicationEngine(t, core.DefaultConfig())
	processor, err := NewProcessor(engine, core.NewPolicy(), commitFailStore{})
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	before := engine.Snapshot()
	event := healthEvent(
		"will-fail",
		protocol.EventTypePromptSubmit,
		protocol.ActivityKindHumanInput,
		time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
	)

	_, err = processor.Process(context.Background(), event, core.AssessmentMissing)
	if !errors.Is(err, errSimulatedCommit) {
		t.Fatalf("Process() error = %v, want %v", err, errSimulatedCommit)
	}
	if got := engine.Snapshot(); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed commit mutated engine:\ngot:  %#v\nwant: %#v", got, before)
	}
}

func TestFailedConcurrentCommitDoesNotRollBackAnEarlierSuccessfulEvent(t *testing.T) {
	t.Parallel()

	engine := mustApplicationEngine(t, core.DefaultConfig())
	store := newOrderedCommitStore()
	processor, err := NewProcessor(engine, core.NewPolicy(), store)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	firstEvent := healthEvent("first", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start)
	secondEvent := healthEvent("second", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start.Add(time.Minute))
	firstResult := make(chan error, 1)
	secondResult := make(chan error, 1)

	go func() {
		_, err := processor.Process(context.Background(), firstEvent, core.AssessmentMissing)
		firstResult <- err
	}()
	<-store.firstEntered
	go func() {
		_, err := processor.Process(context.Background(), secondEvent, core.AssessmentMissing)
		secondResult <- err
	}()
	<-store.secondEntered
	close(store.allowFirst)

	if err := <-firstResult; err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if err := <-secondResult; !errors.Is(err, errSimulatedCommit) {
		t.Fatalf("second Process() error = %v, want %v", err, errSimulatedCommit)
	}
	want, exists, err := store.LoadLatestState(context.Background())
	if err != nil || !exists {
		t.Fatalf("LoadLatestState() = (%#v, %t, %v), want committed state", want, exists, err)
	}
	if got := engine.ExportState(); !reflect.DeepEqual(got, want) {
		t.Fatalf("failed concurrent commit rolled back successful state:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestCommittedEventRemainsIdempotentAfterProcessorRestart(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	eventTime := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	clock := fixedClock{now: eventTime}
	engine, err := core.NewEngine(config, clock)
	if err != nil {
		t.Fatalf("core.NewEngine() error = %v", err)
	}
	store := NewMemoryResultStore()
	processor, err := NewProcessor(engine, core.NewPolicy(), store)
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	event := healthEvent("persisted-event", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, eventTime)
	first := mustProcess(t, processor, event, core.AssessmentMissing)

	state, exists, err := store.LoadLatestState(context.Background())
	if err != nil {
		t.Fatalf("LoadLatestState() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadLatestState() exists = false, want true")
	}
	restored, err := core.NewEngineFromState(config, clock, state)
	if err != nil {
		t.Fatalf("core.NewEngineFromState() error = %v", err)
	}
	restarted, err := NewProcessor(restored, core.NewPolicy(), store)
	if err != nil {
		t.Fatalf("NewProcessor() after restart error = %v", err)
	}
	beforeDuplicate := restored.Snapshot()
	second := mustProcess(t, restarted, event, core.AssessmentSafeNow)

	if !reflect.DeepEqual(second, first) {
		t.Fatalf("duplicate after restart differs:\ngot:  %#v\nwant: %#v", second, first)
	}
	if got := restored.Snapshot(); !reflect.DeepEqual(got, beforeDuplicate) {
		t.Fatalf("duplicate after restart mutated engine:\ngot:  %#v\nwant: %#v", got, beforeDuplicate)
	}
}
