package core

import (
	"errors"
	"fmt"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

const EngineStateSchemaVersion = "1.0"

// EngineState is the durable representation of the user-level aggregate.
// Derived values such as health debt and engagement activity remain excluded.
type EngineState struct {
	SchemaVersion        string         `json:"schema_version"`
	Tracker              TrackerState   `json:"tracker"`
	State                protocol.State `json:"state"`
	StateRevision        uint64         `json:"state_revision"`
	EmergencyLeaseUntil  time.Time      `json:"emergency_lease_until"`
	EmergencyLeaseReason string         `json:"emergency_lease_reason,omitempty"`
}

// TrackerState contains only the facts needed to resume participation
// accounting. HealthDebt and EngagementActive are derived after restoration.
type TrackerState struct {
	Initialized        bool          `json:"initialized"`
	LastAccounted      time.Time     `json:"last_accounted"`
	EngagedUntil       time.Time     `json:"engaged_until"`
	ContinuousWork     time.Duration `json:"continuous_work"`
	LastHumanSessionID string        `json:"last_human_session_id"`
	ActivityCount      uint64        `json:"activity_count"`
	Revision           uint64        `json:"revision"`
}

// ExportState returns a serialization-safe checkpoint of the aggregate.
func (e *Engine) ExportState() EngineState {
	e.mu.Lock()
	defer e.mu.Unlock()

	return EngineState{
		SchemaVersion:        EngineStateSchemaVersion,
		Tracker:              e.tracker.exportState(),
		State:                e.state,
		StateRevision:        e.stateRevision,
		EmergencyLeaseUntil:  e.emergencyLeaseUntil,
		EmergencyLeaseReason: e.emergencyLeaseReason,
	}
}

// NewEngineFromState restores durable facts and rebases participation
// accounting at the current process time. Time elapsed while the process was
// stopped is therefore never counted as engaged work.
func NewEngineFromState(config Config, clock Clock, state EngineState) (*Engine, error) {
	engine, err := NewEngine(config, clock)
	if err != nil {
		return nil, err
	}
	if err := state.validate(); err != nil {
		return nil, err
	}
	if err := engine.restoreState(state, clock.Now()); err != nil {
		return nil, err
	}
	return engine, nil
}

// RestoreState replaces the aggregate with an exact checkpoint. It is used by
// the application layer to roll back an in-memory mutation when persistence
// fails before commit.
func (e *Engine) RestoreState(state EngineState) error {
	if err := state.validate(); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.restoreState(state.Tracker, time.Time{}); err != nil {
		return err
	}
	e.state = state.State
	e.stateRevision = state.StateRevision
	e.emergencyLeaseUntil = state.EmergencyLeaseUntil
	e.emergencyLeaseReason = state.EmergencyLeaseReason
	return nil
}

func (e *Engine) restoreState(state EngineState, rebaseAt time.Time) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := e.tracker.restoreState(state.Tracker, rebaseAt); err != nil {
		return err
	}
	e.state = state.State
	e.stateRevision = state.StateRevision
	// The lease is an absolute instant, so it is copied verbatim rather than
	// rebased: a restart must not extend or reset a running emergency window.
	e.emergencyLeaseUntil = state.EmergencyLeaseUntil
	e.emergencyLeaseReason = state.EmergencyLeaseReason
	return nil
}

func (s EngineState) validate() error {
	if s.SchemaVersion != EngineStateSchemaVersion {
		return fmt.Errorf("unsupported engine state schema %q", s.SchemaVersion)
	}
	if !isKnownState(s.State) {
		return fmt.Errorf("invalid engine state %q", s.State)
	}
	return s.Tracker.validate()
}

func (s TrackerState) validate() error {
	if s.ContinuousWork < 0 {
		return errors.New("continuous work cannot be negative")
	}
	if s.Initialized && s.LastAccounted.IsZero() {
		return errors.New("initialized tracker requires last_accounted")
	}
	return nil
}

func isKnownState(state protocol.State) bool {
	switch state {
	case protocol.StateWorking,
		protocol.StateOvertime,
		protocol.StateWalkout:
		return true
	default:
		return false
	}
}

func (t *Tracker) exportState() TrackerState {
	t.mu.Lock()
	defer t.mu.Unlock()

	return TrackerState{
		Initialized:        t.initialized,
		LastAccounted:      t.lastAccounted,
		EngagedUntil:       t.engagedUntil,
		ContinuousWork:     t.continuousWork,
		LastHumanSessionID: t.lastHumanSessionID,
		ActivityCount:      t.activityCount,
		Revision:           t.revision,
	}
}

func (t *Tracker) restoreState(state TrackerState, rebaseAt time.Time) error {
	if err := state.validate(); err != nil {
		return err
	}
	if state.Initialized && !rebaseAt.IsZero() && rebaseAt.Before(state.LastAccounted) {
		return errors.New("restore time precedes last_accounted")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.initialized = state.Initialized
	t.lastAccounted = state.LastAccounted
	if state.Initialized && !rebaseAt.IsZero() {
		t.lastAccounted = rebaseAt
	}
	t.engagedUntil = state.EngagedUntil
	t.continuousWork = state.ContinuousWork
	t.lastHumanSessionID = state.LastHumanSessionID
	t.activityCount = state.ActivityCount
	t.revision = state.Revision
	return nil
}
