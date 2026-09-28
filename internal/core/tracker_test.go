package core

import (
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()

	if config.WorkInterval != 45*time.Minute {
		t.Fatalf("WorkInterval = %s, want 45m", config.WorkInterval)
	}
	if config.InteractionWindow != 5*time.Minute {
		t.Fatalf("InteractionWindow = %s, want 5m", config.InteractionWindow)
	}
	if config.DebtLimit != 120*time.Minute {
		t.Fatalf("DebtLimit = %s, want 120m", config.DebtLimit)
	}
}

func TestAgentActivityDoesNotExtendHumanEngagement(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)}
	tracker := mustNewTracker(t, DefaultConfig(), clock)

	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(3 * time.Minute)
	mustObserveAgent(t, tracker, "session-a")
	clock.Advance(5 * time.Minute)
	mustTick(t, tracker)

	snapshot := tracker.Snapshot()
	if snapshot.ContinuousWork != 5*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 5m", snapshot.ContinuousWork)
	}
	if snapshot.EngagementActive {
		t.Fatal("EngagementActive = true, want false after the human interaction window expires")
	}
}

func TestMultipleSessionsUseTheUnionOfHumanInteractionWindows(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)}
	tracker := mustNewTracker(t, DefaultConfig(), clock)

	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(3 * time.Minute)
	mustObserveHuman(t, tracker, "session-b")
	clock.Advance(7 * time.Minute)
	mustTick(t, tracker)

	snapshot := tracker.Snapshot()
	if snapshot.ContinuousWork != 8*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 8m union across sessions", snapshot.ContinuousWork)
	}
	if snapshot.LastHumanSessionID != "session-b" {
		t.Fatalf("LastHumanSessionID = %q, want session-b", snapshot.LastHumanSessionID)
	}
}

func TestDebtUsesActualEngagedTimeAfterTheWorkInterval(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)}
	tracker := mustNewTracker(t, config, clock)

	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-b")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-a")

	snapshot := tracker.Snapshot()
	if snapshot.ContinuousWork != 12*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 12m", snapshot.ContinuousWork)
	}
	if snapshot.HealthDebt != 2*time.Minute {
		t.Fatalf("HealthDebt = %s, want 2m", snapshot.HealthDebt)
	}
}

func TestActivityConfirmationImmediatelyClearsWorkAndDebt(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 10 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)}
	tracker := mustNewTracker(t, config, clock)

	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(4 * time.Minute)
	mustObserveHuman(t, tracker, "session-a")

	if err := tracker.ConfirmActivity(); err != nil {
		t.Fatalf("ConfirmActivity() error = %v", err)
	}

	snapshot := tracker.Snapshot()
	if snapshot.ContinuousWork != 0 {
		t.Fatalf("ContinuousWork = %s, want 0", snapshot.ContinuousWork)
	}
	if snapshot.HealthDebt != 0 {
		t.Fatalf("HealthDebt = %s, want 0", snapshot.HealthDebt)
	}
	if snapshot.ActivityCount != 1 {
		t.Fatalf("ActivityCount = %d, want 1", snapshot.ActivityCount)
	}
	if snapshot.EngagementActive {
		t.Fatal("EngagementActive = true, want false until the next human work interaction")
	}
}

func TestIdleTimePausesAccountingWithoutCompletingRecovery(t *testing.T) {
	t.Parallel()

	config := DefaultConfig()
	config.WorkInterval = 2 * time.Minute
	clock := &fakeClock{now: time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC)}
	tracker := mustNewTracker(t, config, clock)

	mustObserveHuman(t, tracker, "session-a")
	clock.Advance(10 * time.Minute)
	mustTick(t, tracker)

	snapshot := tracker.Snapshot()
	if snapshot.ContinuousWork != 5*time.Minute {
		t.Fatalf("ContinuousWork = %s, want 5m", snapshot.ContinuousWork)
	}
	if snapshot.HealthDebt != 3*time.Minute {
		t.Fatalf("HealthDebt = %s, want 3m", snapshot.HealthDebt)
	}
	if snapshot.ActivityCount != 0 {
		t.Fatalf("ActivityCount = %d, want 0", snapshot.ActivityCount)
	}
}

func mustNewTracker(t *testing.T, config Config, clock Clock) *Tracker {
	t.Helper()

	tracker, err := NewTracker(config, clock)
	if err != nil {
		t.Fatalf("NewTracker() error = %v", err)
	}
	return tracker
}

func mustObserveHuman(t *testing.T, tracker *Tracker, sessionID string) {
	t.Helper()
	if err := tracker.ObserveHumanInteraction(sessionID); err != nil {
		t.Fatalf("ObserveHumanInteraction() error = %v", err)
	}
}

func mustObserveAgent(t *testing.T, tracker *Tracker, sessionID string) {
	t.Helper()
	if err := tracker.ObserveAgentActivity(sessionID); err != nil {
		t.Fatalf("ObserveAgentActivity() error = %v", err)
	}
}

func mustTick(t *testing.T, tracker *Tracker) {
	t.Helper()
	if err := tracker.Tick(); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
}
