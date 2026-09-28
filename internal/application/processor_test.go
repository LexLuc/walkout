package application

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestDuplicateEventReturnsTheFirstDecisionWithoutMutatingStateAgain(t *testing.T) {
	t.Parallel()

	engine := mustApplicationEngine(t, core.DefaultConfig())
	processor := mustProcessor(t, engine)
	event := healthEvent(
		"event-1",
		protocol.EventTypePromptSubmit,
		protocol.ActivityKindHumanInput,
		time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
	)

	first := mustProcess(t, processor, event, core.AssessmentMissing)
	firstSnapshot := engine.Snapshot()
	second := mustProcess(t, processor, event, core.AssessmentSafeNow)
	secondSnapshot := engine.Snapshot()

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("duplicate event returned a different decision:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if !reflect.DeepEqual(firstSnapshot, secondSnapshot) {
		t.Fatalf("duplicate event mutated engine state:\nfirst:  %#v\nsecond: %#v", firstSnapshot, secondSnapshot)
	}
	if !firstSnapshot.EngagementActive {
		t.Fatal("human_input did not open the engagement window")
	}
}

func TestNonHumanEventsAdvanceButDoNotExtendTheHumanWindow(t *testing.T) {
	t.Parallel()

	engine := mustApplicationEngine(t, core.DefaultConfig())
	processor := mustProcessor(t, engine)
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)

	mustProcess(t, processor, healthEvent("human", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start), core.AssessmentMissing)
	mustProcess(t, processor, healthEvent("tool", protocol.EventTypePostTool, protocol.ActivityKindToolWork, start.Add(3*time.Minute)), core.AssessmentMissing)
	mustProcess(t, processor, healthEvent("session", protocol.EventTypeSessionEnd, protocol.ActivityKindSessionLifecycle, start.Add(8*time.Minute)), core.AssessmentMissing)

	snapshot := engine.Snapshot()
	if snapshot.ContinuousWork != 5*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 5m", snapshot.ContinuousWork)
	}
	if snapshot.EngagementActive {
		t.Fatal("EngagementActive = true, want false after the original window expires")
	}
}

func TestIncompatibleSchemaFailsOpenWithoutMutatingEngine(t *testing.T) {
	t.Parallel()

	engine := mustApplicationEngine(t, core.DefaultConfig())
	processor := mustProcessor(t, engine)
	event := healthEvent(
		"future-event",
		protocol.EventTypePromptSubmit,
		protocol.ActivityKindHumanInput,
		time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC),
	)
	event.SchemaVersion = "2.0"

	decision := mustProcess(t, processor, event, core.AssessmentMissing)
	if decision.Action != protocol.ActionFailOpen {
		t.Fatalf("Action = %q, want fail_open", decision.Action)
	}
	if decision.ReasonCode != protocol.ReasonSchemaIncompatible {
		t.Fatalf("ReasonCode = %q, want schema_incompatible", decision.ReasonCode)
	}

	snapshot := engine.Snapshot()
	if snapshot.ContinuousWork != 0 || snapshot.EngagementActive || snapshot.Revision != 0 {
		t.Fatalf("incompatible schema mutated engine: %#v", snapshot)
	}
}

func TestPromptThatReachesTheDebtLimitIsPausedAtTheSameRevision(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	config.WorkInterval = 2 * time.Minute

	config.DebtLimit = 2 * time.Minute
	engine := mustApplicationEngine(t, config)
	processor := mustProcessor(t, engine)
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)

	mustProcess(t, processor, healthEvent("event-1", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start), core.AssessmentMissing)
	mustProcess(t, processor, healthEvent("event-2", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start.Add(2*time.Minute)), core.AssessmentMissing)
	decision := mustProcess(t, processor, healthEvent("event-3", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, start.Add(4*time.Minute)), core.AssessmentMissing)

	snapshot := engine.Snapshot()
	if snapshot.State != protocol.StateWalkout {
		t.Fatalf("State = %q, want service_paused", snapshot.State)
	}
	if decision.Action != protocol.ActionPausePrompt {
		t.Fatalf("Action = %q, want pause_prompt", decision.Action)
	}
	if decision.StateRevision != snapshot.StateRevision {
		t.Fatalf("decision revision = %d, engine revision = %d", decision.StateRevision, snapshot.StateRevision)
	}
}

func mustApplicationEngine(t *testing.T, config core.Config) *core.Engine {
	t.Helper()

	engine, err := core.NewEngine(config, fixedClock{now: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("core.NewEngine() error = %v", err)
	}
	return engine
}

func mustProcessor(t *testing.T, engine *core.Engine) *Processor {
	t.Helper()

	processor, err := NewProcessor(engine, core.NewPolicy(), NewMemoryResultStore())
	if err != nil {
		t.Fatalf("NewProcessor() error = %v", err)
	}
	return processor
}

func mustProcess(t *testing.T, processor *Processor, event protocol.HealthEvent, assessment core.AssessmentStatus) protocol.HealthDecision {
	t.Helper()

	decision, err := processor.Process(context.Background(), event, assessment)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	return decision
}

func healthEvent(id string, eventType protocol.EventType, activityKind protocol.ActivityKind, occurredAt time.Time) protocol.HealthEvent {
	return protocol.HealthEvent{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       id,
		Provider:      protocol.ProviderCodex,
		HostVersion:   "test",
		SessionID:     "session-a",
		EventType:     eventType,
		OccurredAt:    occurredAt,
		ActivityKind:  activityKind,
		Payload:       map[string]json.RawMessage{},
	}
}
