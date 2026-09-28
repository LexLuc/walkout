package application

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

type serviceClock struct {
	now time.Time
}

func (c *serviceClock) Now() time.Time {
	return c.now
}

func (commitFailStore) ProcessCommandOnce(
	_ context.Context,
	_ string,
	apply func() (StoredCommandResult, error),
) (StoredCommandResult, error) {
	if _, err := apply(); err != nil {
		return StoredCommandResult{}, err
	}
	return StoredCommandResult{}, errSimulatedCommit
}

func TestServiceStatusOnEmptyStoreIsReadOnly(t *testing.T) {
	t.Parallel()

	store := NewMemoryResultStore()
	service := mustNewService(t, core.DefaultConfig(), &serviceClock{now: testServiceStart()}, store)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != protocol.StateWorking {
		t.Fatalf("State = %q, want working", status.State)
	}
	if status.ContinuousWorkSeconds != 0 || status.HealthDebtSeconds != 0 {
		t.Fatalf("unexpected initial status: %#v", status)
	}
	if _, exists, err := store.LoadLatestState(context.Background()); err != nil || exists {
		t.Fatalf("LoadLatestState() after status = (_, %t, %v), want no persisted state", exists, err)
	}
}

func TestConfirmActivityIsIdempotentAndHostNeutral(t *testing.T) {
	t.Parallel()

	store := NewMemoryResultStore()
	service := mustNewService(t, core.DefaultConfig(), &serviceClock{now: testServiceStart()}, store)
	command := confirmActivityCommand("command-1")

	first, err := service.ExecuteCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("ExecuteCommand() error = %v", err)
	}
	second, err := service.ExecuteCommand(context.Background(), command)
	if err != nil {
		t.Fatalf("ExecuteCommand() duplicate error = %v", err)
	}

	if !reflect.DeepEqual(second, first) {
		t.Fatalf("duplicate response differs:\ngot:  %#v\nwant: %#v", second, first)
	}
	if first.Status.State != protocol.StateWorking {
		t.Fatalf("State = %q, want working", first.Status.State)
	}
	if first.Status.ActivityCount != 1 {
		t.Fatalf("ActivityCount = %d, want 1 (idempotent duplicate must not double-count)", first.Status.ActivityCount)
	}
}

func TestConfirmActivityClearsDebtInOneStepAndPersists(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	config.WorkInterval = 2 * time.Minute
	config.DebtLimit = 10 * time.Minute
	clock := &serviceClock{now: testServiceStart()}
	store := NewMemoryResultStore()
	service := mustNewService(t, config, clock, store)

	mustServiceEvent(t, service, healthEvent("event-1", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now))
	clock.now = clock.now.Add(4 * time.Minute)
	mustServiceEvent(t, service, healthEvent("event-2", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now))
	beforeConfirm, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() before confirm error = %v", err)
	}
	if beforeConfirm.State != protocol.StateOvertime {
		t.Fatalf("State = %q, want overtime before confirmation", beforeConfirm.State)
	}
	if beforeConfirm.HealthDebtSeconds != 120 {
		t.Fatalf("HealthDebtSeconds = %d, want 120", beforeConfirm.HealthDebtSeconds)
	}

	response, err := service.ExecuteCommand(context.Background(), confirmActivityCommand("confirm-1"))
	if err != nil {
		t.Fatalf("confirm activity error = %v", err)
	}
	if response.Status.State != protocol.StateWorking {
		t.Fatalf("State = %q, want working after one-step confirmation", response.Status.State)
	}
	if response.Status.ContinuousWorkSeconds != 0 || response.Status.HealthDebtSeconds != 0 {
		t.Fatalf("confirmation did not clear work and debt: %#v", response.Status)
	}
	if response.Status.ActivityCount != 1 {
		t.Fatalf("ActivityCount = %d, want 1", response.Status.ActivityCount)
	}

	restarted := mustNewService(t, config, clock, store)
	status, err := restarted.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after restart error = %v", err)
	}
	if status.State != protocol.StateWorking || status.ActivityCount != 1 {
		t.Fatalf("restored status = %#v, want confirmed activity", status)
	}
}

func TestEmergencyContinueAllowsPromptsWhilePausedUntilLeaseExpires(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	config.WorkInterval = 2 * time.Minute

	config.DebtLimit = 5 * time.Minute
	config.EmergencyExtension = 15 * time.Minute
	clock := &serviceClock{now: testServiceStart()}
	store := NewMemoryResultStore()
	service := mustNewService(t, config, clock, store)

	// Accrue enough debt to enter service_paused.
	mustServiceEvent(t, service, healthEvent("event-1", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now))
	for i := range 4 {
		clock.now = clock.now.Add(4 * time.Minute)
		mustServiceEvent(t, service, healthEvent(fmt.Sprintf("event-acc-%d", i), protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now))
	}
	paused, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if paused.State != protocol.StateWalkout {
		t.Fatalf("state = %q, want service_paused", paused.State)
	}
	debtAtPause := paused.HealthDebtSeconds

	// A prompt while paused without a lease is paused.
	blocked, err := service.ProcessEvent(context.Background(), healthEvent("event-blocked", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now), core.AssessmentMissing)
	if err != nil {
		t.Fatalf("ProcessEvent() error = %v", err)
	}
	if blocked.Action != protocol.ActionPausePrompt {
		t.Fatalf("Action = %q, want pause_prompt", blocked.Action)
	}

	// Grant the emergency lease.
	if _, err := service.ExecuteCommand(context.Background(), protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "emergency-1",
		CommandType:   protocol.CommandEmergencyContinue,
	}); err != nil {
		t.Fatalf("emergency continue error = %v", err)
	}
	afterGrant, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after grant error = %v", err)
	}
	if afterGrant.HealthDebtSeconds != debtAtPause {
		t.Fatalf("HealthDebtSeconds = %d, want unchanged %d", afterGrant.HealthDebtSeconds, debtAtPause)
	}

	// A prompt within the lease window is allowed while state stays paused.
	allowed, err := service.ProcessEvent(context.Background(), healthEvent("event-allowed", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now), core.AssessmentMissing)
	if err != nil {
		t.Fatalf("ProcessEvent() during lease error = %v", err)
	}
	if allowed.Action != protocol.ActionAllow || allowed.ReasonCode != protocol.ReasonEmergencyContinue {
		t.Fatalf("decision = %q/%q, want allow/emergency_continue", allowed.Action, allowed.ReasonCode)
	}

	// After the lease expires, prompts pause again.
	clock.now = clock.now.Add(config.EmergencyExtension + time.Minute)
	expired, err := service.ProcessEvent(context.Background(), healthEvent("event-expired", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, clock.now), core.AssessmentMissing)
	if err != nil {
		t.Fatalf("ProcessEvent() after lease error = %v", err)
	}
	if expired.Action != protocol.ActionPausePrompt {
		t.Fatalf("Action after lease = %q, want pause_prompt", expired.Action)
	}
}

func TestEmergencyContinueRecordsReasonInStatus(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	config.EmergencyExtension = 15 * time.Minute
	clock := &serviceClock{now: testServiceStart()}
	store := NewMemoryResultStore()
	service := mustNewService(t, config, clock, store)

	const reason = "prod incident, need one more turn"
	if _, err := service.ExecuteCommand(context.Background(), protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "emergency-reason-1",
		CommandType:   protocol.CommandEmergencyContinue,
		Reason:        reason,
	}); err != nil {
		t.Fatalf("emergency continue error = %v", err)
	}

	// Status reads back through the persistence boundary, so the reason must
	// survive export → LoadLatestState → restore, not only live in memory.
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.EmergencyContinueActive {
		t.Fatal("EmergencyContinueActive = false, want true after grant")
	}
	if status.EmergencyContinueReason != reason {
		t.Fatalf("EmergencyContinueReason = %q, want %q", status.EmergencyContinueReason, reason)
	}
}

func TestStatusOmitsEmergencyReasonWhenLeaseInactive(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	config.EmergencyExtension = 15 * time.Minute
	clock := &serviceClock{now: testServiceStart()}
	service := mustNewService(t, config, clock, NewMemoryResultStore())

	const reason = "test"
	if _, err := service.ExecuteCommand(context.Background(), protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "emergency-reason-inactive-1",
		CommandType:   protocol.CommandEmergencyContinue,
		Reason:        reason,
	}); err != nil {
		t.Fatalf("emergency continue error = %v", err)
	}

	// A reason shown next to an inactive lease reads as a stale bug, whether
	// the lease ended by activity confirmation or by natural expiry.
	if _, err := service.ExecuteCommand(context.Background(), protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "confirm-after-lease-1",
		CommandType:   protocol.CommandConfirmActivity,
	}); err != nil {
		t.Fatalf("confirm activity error = %v", err)
	}
	afterDone, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after done error = %v", err)
	}
	if afterDone.EmergencyContinueActive || afterDone.EmergencyContinueReason != "" {
		t.Fatalf("after done: active=%v reason=%q, want inactive with empty reason", afterDone.EmergencyContinueActive, afterDone.EmergencyContinueReason)
	}

	if _, err := service.ExecuteCommand(context.Background(), protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "emergency-reason-inactive-2",
		CommandType:   protocol.CommandEmergencyContinue,
		Reason:        reason,
	}); err != nil {
		t.Fatalf("second emergency continue error = %v", err)
	}
	clock.now = clock.now.Add(config.EmergencyExtension + time.Second)
	afterExpiry, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after expiry error = %v", err)
	}
	if afterExpiry.EmergencyContinueActive || afterExpiry.EmergencyContinueReason != "" {
		t.Fatalf("after expiry: active=%v reason=%q, want inactive with empty reason", afterExpiry.EmergencyContinueActive, afterExpiry.EmergencyContinueReason)
	}
}

func TestControlCommandRejectsInvalidReason(t *testing.T) {
	t.Parallel()

	config := core.DefaultConfig()
	service := mustNewService(t, config, &serviceClock{now: testServiceStart()}, NewMemoryResultStore())

	tooLong := strings.Repeat("汉", maxEmergencyContinueReasonRunes+1)
	tests := map[string]protocol.HealthControlCommand{
		"reason on confirm_activity": {
			SchemaVersion: protocol.SchemaVersion,
			CommandID:     "ca-reason",
			CommandType:   protocol.CommandConfirmActivity,
			Reason:        "why",
		},
		"unknown command type": {
			SchemaVersion: protocol.SchemaVersion,
			CommandID:     "unknown-type",
			CommandType:   protocol.HealthControlCommandType("start_break"),
		},
		"over-long emergency reason": {
			SchemaVersion: protocol.SchemaVersion,
			CommandID:     "ec-long",
			CommandType:   protocol.CommandEmergencyContinue,
			Reason:        tooLong,
		},
	}
	for name, command := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := service.ExecuteCommand(context.Background(), command); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("ExecuteCommand() error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestControlCommitFailureRollsBackEngine(t *testing.T) {
	t.Parallel()

	service := mustNewService(t, core.DefaultConfig(), &serviceClock{now: testServiceStart()}, commitFailStore{})
	before := service.engine.Snapshot()

	_, err := service.ExecuteCommand(context.Background(), confirmActivityCommand("will-fail"))
	if !errors.Is(err, errSimulatedCommit) {
		t.Fatalf("ExecuteCommand() error = %v, want %v", err, errSimulatedCommit)
	}
	if got := service.engine.Snapshot(); !reflect.DeepEqual(got, before) {
		t.Fatalf("failed command mutated engine:\ngot:  %#v\nwant: %#v", got, before)
	}
}

func mustNewService(t *testing.T, config core.Config, clock core.Clock, store Store) *Service {
	t.Helper()
	service, err := NewService(context.Background(), config, clock, core.NewPolicy(), store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func mustServiceEvent(t *testing.T, service *Service, event protocol.HealthEvent) {
	t.Helper()
	if _, err := service.ProcessEvent(context.Background(), event, core.AssessmentMissing); err != nil {
		t.Fatalf("ProcessEvent() error = %v", err)
	}
}

func confirmActivityCommand(id string) protocol.HealthControlCommand {
	return protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     id,
		CommandType:   protocol.CommandConfirmActivity,
	}
}

func testServiceStart() time.Time {
	return time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
}
