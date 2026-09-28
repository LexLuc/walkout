package application

import (
	"context"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

func TestRestoreEngineLoadsLatestStateAndRebasesDowntime(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	sourceClock := fixedClock{now: start}
	source, err := core.NewEngine(config, sourceClock)
	if err != nil {
		t.Fatalf("core.NewEngine() error = %v", err)
	}
	if err := source.ObserveHumanInteractionAt("session-a", start); err != nil {
		t.Fatalf("ObserveHumanInteractionAt() error = %v", err)
	}
	if err := source.ObserveAgentActivityAt("session-a", start.Add(3*time.Minute)); err != nil {
		t.Fatalf("ObserveAgentActivityAt() error = %v", err)
	}

	store := NewMemoryResultStore()
	_, err = store.ProcessOnce(context.Background(), "event-1", func() (StoredResult, error) {
		return StoredResult{
			Decision:    protocol.HealthDecision{SourceEventID: "event-1"},
			EngineState: source.ExportState(),
		}, nil
	})
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}

	restartClock := fixedClock{now: start.Add(2 * time.Hour)}
	restored, err := RestoreEngine(context.Background(), config, restartClock, store)
	if err != nil {
		t.Fatalf("RestoreEngine() error = %v", err)
	}
	snapshot := restored.Snapshot()
	if snapshot.ContinuousWork != 3*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 3m", snapshot.ContinuousWork)
	}
	if snapshot.EngagementActive {
		t.Fatal("EngagementActive = true after downtime")
	}
}

func TestRestoreEngineRejectsCorruptLatestState(t *testing.T) {
	t.Parallel()

	store := NewMemoryResultStore()
	_, err := store.ProcessOnce(context.Background(), "corrupt", func() (StoredResult, error) {
		return StoredResult{
			Decision:    protocol.HealthDecision{SourceEventID: "corrupt"},
			EngineState: core.EngineState{SchemaVersion: "99.0"},
		}, nil
	})
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}

	engine, err := RestoreEngine(
		context.Background(),
		core.DefaultConfig(),
		fixedClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)},
		store,
	)
	if err == nil {
		t.Fatal("RestoreEngine() error = nil, want diagnostic error")
	}
	if engine != nil {
		t.Fatalf("RestoreEngine() engine = %#v, want nil", engine)
	}
}
