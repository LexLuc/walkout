package core

import (
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

func TestEngineTransitionsThroughOvertimeIntoWalkout(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	config.DebtLimit = 5 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	assertEngineState(t, engine, protocol.StateWorking)

	clock.Advance(4 * time.Minute)
	mustEngineHuman(t, engine, "session-a")
	clock.Advance(4 * time.Minute)
	mustEngineHuman(t, engine, "session-b")
	clock.Advance(2 * time.Minute)
	mustEngineHuman(t, engine, "session-a")
	assertEngineState(t, engine, protocol.StateOvertime)

	// Ignoring the reminder is itself the postponement: engaged time beyond
	// the interval accrues as debt until the limit forces the walkout.
	clock.Advance(2 * time.Minute)
	mustEngineHuman(t, engine, "session-a")
	assertEngineState(t, engine, protocol.StateOvertime)
	if got := engine.Snapshot().HealthDebt; got != 2*time.Minute {
		t.Fatalf("HealthDebt = %s, want 2m of engaged overtime", got)
	}

	clock.Advance(3 * time.Minute)
	mustEngineTick(t, engine)
	assertEngineState(t, engine, protocol.StateWalkout)
	if got := engine.Snapshot().HealthDebt; got != 5*time.Minute {
		t.Fatalf("HealthDebt = %s, want 5m of actual engaged overtime", got)
	}
}

func TestConfirmActivityClearsWorkAndDebtInOneStep(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	config.DebtLimit = 5 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	for range 3 {
		clock.Advance(4 * time.Minute)
		mustEngineHuman(t, engine, "session-a")
	}
	assertEngineState(t, engine, protocol.StateOvertime)

	if err := engine.ConfirmActivity(); err != nil {
		t.Fatalf("ConfirmActivity() error = %v", err)
	}
	snapshot := engine.Snapshot()
	if snapshot.State != protocol.StateWorking {
		t.Fatalf("State = %q, want working immediately after confirmation", snapshot.State)
	}
	if snapshot.ContinuousWork != 0 || snapshot.HealthDebt != 0 {
		t.Fatalf("work/debt = %s/%s, want both cleared", snapshot.ContinuousWork, snapshot.HealthDebt)
	}
	if snapshot.ActivityCount != 1 {
		t.Fatalf("ActivityCount = %d, want 1", snapshot.ActivityCount)
	}
}

func TestIdleAndAgentActivityDoNotCauseFalseTransitions(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	clock.Advance(3 * time.Minute)
	if err := engine.ObserveAgentActivity("session-a"); err != nil {
		t.Fatalf("ObserveAgentActivity() error = %v", err)
	}
	clock.Advance(7 * time.Minute)
	mustEngineTick(t, engine)
	mustEngineTick(t, engine)

	snapshot := engine.Snapshot()
	if snapshot.ContinuousWork != 5*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 5m", snapshot.ContinuousWork)
	}
	if snapshot.HealthDebt != 0 {
		t.Fatalf("HealthDebt = %s, want 0", snapshot.HealthDebt)
	}
	if snapshot.State != protocol.StateWorking {
		t.Fatalf("State = %q, want working", snapshot.State)
	}
}

func TestDebtLimitForcesWalkout(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	config.DebtLimit = 5 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	for range 3 {
		clock.Advance(4 * time.Minute)
		mustEngineHuman(t, engine, "session-a")
	}
	clock.Advance(3 * time.Minute)
	mustEngineTick(t, engine)

	assertEngineState(t, engine, protocol.StateWalkout)
}

func TestEmergencyContinueLeasesAllowanceWithoutClearingDebt(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	config.DebtLimit = 5 * time.Minute
	config.EmergencyExtension = 15 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	for range 3 {
		clock.Advance(4 * time.Minute)
		mustEngineHuman(t, engine, "session-a")
	}
	clock.Advance(3 * time.Minute)
	mustEngineTick(t, engine)
	assertEngineState(t, engine, protocol.StateWalkout)

	debtBefore := engine.Snapshot().HealthDebt
	if debtBefore <= 0 {
		t.Fatalf("expected accrued debt before emergency continue, got %v", debtBefore)
	}

	if err := engine.StartEmergencyContinue(""); err != nil {
		t.Fatalf("StartEmergencyContinue() error = %v", err)
	}
	active := engine.Snapshot()
	if !active.EmergencyLeaseActive {
		t.Fatal("emergency lease is not active immediately after granting it")
	}
	if active.State != protocol.StateWalkout {
		t.Fatalf("state = %q, want walkout (lease gates the prompt, not the state)", active.State)
	}
	if active.HealthDebt != debtBefore {
		t.Fatalf("HealthDebt = %v, want unchanged %v (lease must not forgive debt)", active.HealthDebt, debtBefore)
	}

	clock.Advance(config.EmergencyExtension + time.Second)
	if engine.Snapshot().EmergencyLeaseActive {
		t.Fatal("emergency lease is still active after the extension window elapsed")
	}
}

func TestConfirmActivityEndsEmergencyLease(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	config.DebtLimit = 5 * time.Minute
	config.EmergencyExtension = 15 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	mustEngineHuman(t, engine, "session-a")
	for range 3 {
		clock.Advance(4 * time.Minute)
		mustEngineHuman(t, engine, "session-a")
	}
	clock.Advance(3 * time.Minute)
	mustEngineTick(t, engine)
	assertEngineState(t, engine, protocol.StateWalkout)
	if err := engine.StartEmergencyContinue("hotfix"); err != nil {
		t.Fatalf("StartEmergencyContinue() error = %v", err)
	}

	if err := engine.ConfirmActivity(); err != nil {
		t.Fatalf("ConfirmActivity() error = %v", err)
	}
	after := engine.Snapshot()
	if after.State != protocol.StateWorking {
		t.Fatalf("state = %q, want working", after.State)
	}
	// Confirming activity ends the emergency episode: the lease belongs to
	// the walkout it overrode and must not pre-authorize the next one, and
	// status must not report an active emergency while working.
	if after.EmergencyLeaseActive {
		t.Fatal("emergency lease is still active after ConfirmActivity")
	}
	if after.EmergencyLeaseReason != "hotfix" {
		t.Fatalf("EmergencyLeaseReason = %q, want the last recorded reason kept", after.EmergencyLeaseReason)
	}
}

func TestEmergencyContinueRecordsReasonAndSurvivesRestart(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.EmergencyExtension = 15 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	const reason = "production incident hotfix"
	if err := engine.StartEmergencyContinue(reason); err != nil {
		t.Fatalf("StartEmergencyContinue() error = %v", err)
	}
	if got := engine.Snapshot().EmergencyLeaseReason; got != reason {
		t.Fatalf("EmergencyLeaseReason = %q, want %q", got, reason)
	}

	// The reason is durable accountability metadata: within the lease window it
	// survives an export→restore exactly like the absolute expiry does.
	within := &fakeClock{now: clock.now.Add(5 * time.Minute)}
	restored, err := NewEngineFromState(config, within, engine.ExportState())
	if err != nil {
		t.Fatalf("NewEngineFromState() error = %v", err)
	}
	snapshot := restored.Snapshot()
	if !snapshot.EmergencyLeaseActive {
		t.Fatal("emergency lease inactive after restore within its window")
	}
	if snapshot.EmergencyLeaseReason != reason {
		t.Fatalf("restored EmergencyLeaseReason = %q, want %q", snapshot.EmergencyLeaseReason, reason)
	}
}

func TestEmergencyContinueAcceptsEmptyReason(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.EmergencyExtension = 15 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}
	engine := mustNewEngine(t, config, clock)

	// An empty reason is allowed so the escape hatch is never slowed by a
	// mandatory field; the lease still activates and records no reason.
	if err := engine.StartEmergencyContinue(""); err != nil {
		t.Fatalf("StartEmergencyContinue(\"\") error = %v", err)
	}
	snapshot := engine.Snapshot()
	if !snapshot.EmergencyLeaseActive {
		t.Fatal("emergency lease inactive after granting with an empty reason")
	}
	if snapshot.EmergencyLeaseReason != "" {
		t.Fatalf("EmergencyLeaseReason = %q, want empty", snapshot.EmergencyLeaseReason)
	}
}

func mustNewEngine(t *testing.T, config Config, clock Clock) *Engine {
	t.Helper()

	engine, err := NewEngine(config, clock)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}

func mustEngineHuman(t *testing.T, engine *Engine, sessionID string) {
	t.Helper()
	if err := engine.ObserveHumanInteraction(sessionID); err != nil {
		t.Fatalf("ObserveHumanInteraction() error = %v", err)
	}
}

func mustEngineTick(t *testing.T, engine *Engine) {
	t.Helper()
	if err := engine.Tick(); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
}

func assertEngineState(t *testing.T, engine *Engine, want protocol.State) {
	t.Helper()
	if got := engine.Snapshot().State; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
}
