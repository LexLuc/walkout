package core

import (
	"errors"
	"time"
)

// Config contains deterministic policy parameters for the domain core.
type Config struct {
	WorkInterval       time.Duration
	DebtLimit          time.Duration
	InteractionWindow  time.Duration
	AbsenceRequired    time.Duration
	EmergencyExtension time.Duration
}

// DefaultConfig returns the V1 defaults defined by product-spec.md. There is
// no snooze or grace-period parameter since the 2026-08-26 three-state
// simplification: ignoring reminders is itself the postponement and its cost
// is expressed by accruing debt.
func DefaultConfig() Config {
	return Config{
		WorkInterval:       45 * time.Minute,
		DebtLimit:          120 * time.Minute,
		InteractionWindow:  5 * time.Minute,
		AbsenceRequired:    20 * time.Second,
		EmergencyExtension: 15 * time.Minute,
	}
}

func (c Config) validate() error {
	if c.WorkInterval <= 0 {
		return errors.New("work interval must be positive")
	}
	if c.InteractionWindow <= 0 {
		return errors.New("interaction window must be positive")
	}
	if c.DebtLimit <= 0 {
		return errors.New("debt limit must be positive")
	}
	return nil
}
