package core

import (
	"errors"
	"sync"
	"time"
)

// Clock makes all time-dependent domain behavior testable without sleeps.
type Clock interface {
	Now() time.Time
}

// Snapshot is the current user-level participation and debt state.
type Snapshot struct {
	ContinuousWork     time.Duration
	HealthDebt         time.Duration
	EngagementActive   bool
	LastHumanSessionID string
	ActivityCount      uint64
	Revision           uint64
}

// Tracker merges human interaction windows across every Agent session.
// Agent runtime is deliberately observed separately and never extends the
// human engagement window.
type Tracker struct {
	mu sync.Mutex

	config Config
	clock  Clock

	initialized   bool
	lastAccounted time.Time
	engagedUntil  time.Time

	continuousWork     time.Duration
	lastHumanSessionID string
	activityCount      uint64
	revision           uint64
}

func NewTracker(config Config, clock Clock) (*Tracker, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		return nil, errors.New("clock is required")
	}
	return &Tracker{config: config, clock: clock}, nil
}

func (t *Tracker) ObserveHumanInteraction(sessionID string) error {
	return t.ObserveHumanInteractionAt(sessionID, t.clock.Now())
}

func (t *Tracker) ObserveHumanInteractionAt(sessionID string, occurredAt time.Time) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.advance(occurredAt); err != nil {
		return err
	}

	windowEnd := occurredAt.Add(t.config.InteractionWindow)
	if windowEnd.After(t.engagedUntil) {
		t.engagedUntil = windowEnd
	}
	t.lastHumanSessionID = sessionID
	t.revision++
	return nil
}

func (t *Tracker) ObserveAgentActivity(_ string) error {
	return t.ObserveAgentActivityAt(t.clock.Now())
}

func (t *Tracker) ObserveAgentActivityAt(occurredAt time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.advance(occurredAt); err != nil {
		return err
	}
	t.revision++
	return nil
}

func (t *Tracker) Tick() error {
	return t.TickAt(t.clock.Now())
}

func (t *Tracker) TickAt(occurredAt time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.advance(occurredAt)
}

// ConfirmActivity is the single-step "I moved" confirmation: it clears the
// continuous-work accounting (and thereby the derived debt) and counts one
// confirmed activity. The product does not measure or verify break duration.
func (t *Tracker) ConfirmActivity() error {
	return t.ConfirmActivityAt(t.clock.Now())
}

func (t *Tracker) ConfirmActivityAt(occurredAt time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.advance(occurredAt); err != nil {
		return err
	}

	t.continuousWork = 0
	t.engagedUntil = time.Time{}
	t.lastAccounted = occurredAt
	t.activityCount++
	t.revision++
	return nil
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()

	debt := t.continuousWork - t.config.WorkInterval
	if debt < 0 {
		debt = 0
	}

	engagementActive := !t.engagedUntil.IsZero() && t.lastAccounted.Before(t.engagedUntil)

	return Snapshot{
		ContinuousWork:     t.continuousWork,
		HealthDebt:         debt,
		EngagementActive:   engagementActive,
		LastHumanSessionID: t.lastHumanSessionID,
		ActivityCount:      t.activityCount,
		Revision:           t.revision,
	}
}

func (t *Tracker) advance(now time.Time) error {
	if !t.initialized {
		t.initialized = true
		t.lastAccounted = now
		return nil
	}
	if now.Before(t.lastAccounted) {
		return errors.New("clock moved backwards")
	}

	engagedEnd := now
	if t.engagedUntil.Before(engagedEnd) {
		engagedEnd = t.engagedUntil
	}
	if engagedEnd.After(t.lastAccounted) {
		t.continuousWork += engagedEnd.Sub(t.lastAccounted)
	}
	t.lastAccounted = now
	return nil
}
