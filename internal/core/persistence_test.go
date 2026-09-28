package core

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEngineStateJSONRoundTripRestoresTheSameSnapshot(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 4 * time.Minute

	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	clock.Advance(5 * time.Minute)
	mustEngineHuman(t, engine, "session-b")
	want := engine.Snapshot()

	encoded, err := json.Marshal(engine.ExportState())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded EngineState
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	restored, err := NewEngineFromState(config, clock, decoded)
	if err != nil {
		t.Fatalf("NewEngineFromState() error = %v", err)
	}

	if got := restored.Snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("restored snapshot differs:\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestEngineRestoreDoesNotCountProcessDowntime(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	beforeStop := &fakeClock{now: start}
	engine := mustNewEngine(t, config, beforeStop)

	mustEngineHuman(t, engine, "session-a")
	beforeStop.Advance(3 * time.Minute)
	if err := engine.ObserveAgentActivity("session-a"); err != nil {
		t.Fatalf("ObserveAgentActivity() error = %v", err)
	}
	state := engine.ExportState()

	afterRestart := &fakeClock{now: start.Add(2 * time.Hour)}
	restored, err := NewEngineFromState(config, afterRestart, state)
	if err != nil {
		t.Fatalf("NewEngineFromState() error = %v", err)
	}
	if got := restored.Snapshot().ContinuousWork; got != 3*time.Minute {
		t.Fatalf("ContinuousWork after restart = %s, want 3m", got)
	}
	if restored.Snapshot().EngagementActive {
		t.Fatal("EngagementActive = true after the persisted interaction window expired")
	}

	mustEngineHuman(t, restored, "session-b")
	afterRestart.Advance(2 * time.Minute)
	mustEngineTick(t, restored)
	if got := restored.Snapshot().ContinuousWork; got != 5*time.Minute {
		t.Fatalf("ContinuousWork after new interaction = %s, want 5m", got)
	}
}

func TestEmergencyLeaseSurvivesRestartByAbsoluteExpiry(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.EmergencyExtension = 15 * time.Minute
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	beforeStop := &fakeClock{now: start}
	engine := mustNewEngine(t, config, beforeStop)

	mustEngineHuman(t, engine, "session-a")
	if err := engine.StartEmergencyContinue(""); err != nil {
		t.Fatalf("StartEmergencyContinue() error = %v", err)
	}
	state := engine.ExportState()

	// Restart 5 minutes later: the 15-minute lease (expiring at start+15m) must
	// still be active, judged against the real clock rather than reset.
	withinLease := &fakeClock{now: start.Add(5 * time.Minute)}
	restored, err := NewEngineFromState(config, withinLease, state)
	if err != nil {
		t.Fatalf("NewEngineFromState() error = %v", err)
	}
	if !restored.Snapshot().EmergencyLeaseActive {
		t.Fatal("emergency lease inactive after restart within its window")
	}

	// Restart 20 minutes later: the same absolute lease has expired.
	afterLease := &fakeClock{now: start.Add(20 * time.Minute)}
	expired, err := NewEngineFromState(config, afterLease, state)
	if err != nil {
		t.Fatalf("NewEngineFromState() error = %v", err)
	}
	if expired.Snapshot().EmergencyLeaseActive {
		t.Fatal("emergency lease still active after its absolute expiry")
	}
}

func TestInvalidEngineStateIsRejected(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	tests := map[string]EngineState{
		"incompatible schema": {
			SchemaVersion: "99.0",
		},
		"negative work duration": {
			SchemaVersion: EngineStateSchemaVersion,
			State:         "working",
			Tracker: TrackerState{
				ContinuousWork: -time.Minute,
			},
		},
	}

	for name, state := range tests {
		t.Run(name, func(t *testing.T) {
			engine, err := NewEngineFromState(DefaultConfig(), clock, state)
			if err == nil {
				t.Fatal("NewEngineFromState() error = nil, want diagnostic error")
			}
			if engine != nil {
				t.Fatalf("NewEngineFromState() engine = %#v, want nil", engine)
			}
		})
	}
}
