package core

import (
	"sync"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

// EngineSnapshot combines participation accounting with the canonical product
// state. The embedded Snapshot remains the source of work and debt values.
type EngineSnapshot struct {
	Snapshot
	State                   protocol.State
	StateRevision           uint64
	EmergencyLeaseActive    bool
	EmergencyLeaseUntil     time.Time
	EmergencyLeaseRemaining time.Duration
	EmergencyLeaseReason    string
}

// Engine is the user-level domain aggregate. It connects inferred human
// participation, health debt, the single-step activity confirmation, and the
// canonical three-state machine (working → overtime → walkout) without
// host-specific behavior.
type Engine struct {
	mu sync.Mutex

	config  Config
	clock   Clock
	tracker *Tracker

	state         protocol.State
	stateRevision uint64

	// emergencyLeaseUntil is an absolute wall-clock instant. While the clock is
	// before it, a walkout lets prompts through. It is deliberately not
	// rebased on restore: a lease keeps its real expiry across daemon restarts.
	emergencyLeaseUntil time.Time
	// emergencyLeaseReason records why the user last overrode a walkout. It is
	// the accountability trail for the self-commitment device: kept as the most
	// recent grant's reason and surfaced through the snapshot and status.
	emergencyLeaseReason string
}

func NewEngine(config Config, clock Clock) (*Engine, error) {
	tracker, err := NewTracker(config, clock)
	if err != nil {
		return nil, err
	}
	return &Engine{
		config:  config,
		clock:   clock,
		tracker: tracker,
		state:   protocol.StateWorking,
	}, nil
}

func (e *Engine) ObserveHumanInteraction(sessionID string) error {
	return e.ObserveHumanInteractionAt(sessionID, e.clock.Now())
}

func (e *Engine) ObserveHumanInteractionAt(sessionID string, occurredAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.ObserveHumanInteractionAt(sessionID, occurredAt); err != nil {
		return err
	}
	e.reconcile()
	return nil
}

func (e *Engine) ObserveAgentActivity(sessionID string) error {
	return e.ObserveAgentActivityAt(sessionID, e.clock.Now())
}

func (e *Engine) ObserveAgentActivityAt(_ string, occurredAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.ObserveAgentActivityAt(occurredAt); err != nil {
		return err
	}
	e.reconcile()
	return nil
}

func (e *Engine) Tick() error {
	return e.TickAt(e.clock.Now())
}

func (e *Engine) TickAt(occurredAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.TickAt(occurredAt); err != nil {
		return err
	}
	e.reconcile()
	return nil
}

// ConfirmActivity is the single-step "I moved" confirmation. It clears the
// continuous-work accounting and the derived debt and returns to working from
// any state. The product does not measure or verify break duration.
func (e *Engine) ConfirmActivity() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.ConfirmActivity(); err != nil {
		return err
	}
	// The emergency lease belongs to the walkout it overrode; confirming
	// activity ends that episode so a leftover lease can never pre-authorize
	// the next walkout. The last reason stays as accountability metadata.
	e.emergencyLeaseUntil = time.Time{}
	e.reconcile()
	return nil
}

// StartEmergencyContinue grants a bounded lease that lets a walkout admit
// prompts until the emergency extension elapses. It never forgives accrued
// debt: once the lease expires the walkout resumes at the same debt. The
// reason is optional accountability metadata recorded verbatim for the grant.
func (e *Engine) StartEmergencyContinue(reason string) error {
	return e.StartEmergencyContinueAt(e.clock.Now(), reason)
}

func (e *Engine) StartEmergencyContinueAt(now time.Time, reason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.emergencyLeaseUntil = now.Add(e.config.EmergencyExtension)
	e.emergencyLeaseReason = reason
	return nil
}

func (e *Engine) Snapshot() EngineSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.clock.Now()
	active := e.emergencyLeaseActiveLocked(now)
	var remaining time.Duration
	if active {
		remaining = e.emergencyLeaseUntil.Sub(now)
	}
	return EngineSnapshot{
		Snapshot:                e.tracker.Snapshot(),
		State:                   e.state,
		StateRevision:           e.stateRevision,
		EmergencyLeaseActive:    active,
		EmergencyLeaseUntil:     e.emergencyLeaseUntil,
		EmergencyLeaseRemaining: remaining,
		EmergencyLeaseReason:    e.emergencyLeaseReason,
	}
}

func (e *Engine) emergencyLeaseActiveLocked(now time.Time) bool {
	return !e.emergencyLeaseUntil.IsZero() && now.Before(e.emergencyLeaseUntil)
}

func (e *Engine) reconcile() {
	snapshot := e.tracker.Snapshot()
	switch {
	case snapshot.HealthDebt >= e.config.DebtLimit:
		e.setState(protocol.StateWalkout)
	case snapshot.ContinuousWork >= e.config.WorkInterval:
		e.setState(protocol.StateOvertime)
	default:
		e.setState(protocol.StateWorking)
	}
}

func (e *Engine) setState(state protocol.State) {
	if e.state == state {
		return
	}
	e.state = state
	e.stateRevision++
}
