//go:build windows

// Command seed-paused pre-populates a probe database with a self-consistent
// engine state so a real host can exercise prompt blocking (walkout) or
// reminder injection (overtime) without waiting for the production intervals.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/infrastructure/sqlitestore"
	"github.com/LexLuc/walkout/internal/protocol"
)

func main() {
	if len(os.Args) != 2 && len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: seed-paused <database-path> [walkout|overtime]")
		os.Exit(2)
	}
	state := "walkout"
	if len(os.Args) == 3 {
		state = os.Args[2]
	}
	if err := run(os.Args[1], state); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(databasePath string, stateName string) error {
	var (
		state          protocol.State
		action         protocol.DecisionAction
		reason         protocol.ReasonCode
		continuousWork time.Duration
	)
	switch stateName {
	case "walkout":
		// Debt already past the 120-minute limit: 170m - 45m interval = 125m.
		state = protocol.StateWalkout
		action = protocol.ActionPausePrompt
		reason = protocol.ReasonDebtLimit
		continuousWork = 170 * time.Minute
	case "overtime":
		// Just past the 45-minute interval with a small accruing debt, so the
		// next prompt draws an escalating inject_reminder instead of a block.
		state = protocol.StateOvertime
		action = protocol.ActionInjectReminder
		reason = protocol.ReasonDebtGrowing
		continuousWork = 50 * time.Minute
	default:
		return fmt.Errorf("unknown seed state %q (want walkout or overtime)", stateName)
	}

	store, err := sqlitestore.Open(databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	now := time.Now()
	_, err = store.ProcessOnce(context.Background(), "manual-probe-seed-event", func() (application.StoredResult, error) {
		return application.StoredResult{
			Decision: protocol.HealthDecision{
				SchemaVersion: protocol.SchemaVersion,
				DecisionID:    "manual-probe-seed-decision",
				SourceEventID: "manual-probe-seed-event",
				StateRevision: 1,
				State:         state,
				Action:        action,
				ReasonCode:    reason,
			},
			EngineState: core.EngineState{
				SchemaVersion: core.EngineStateSchemaVersion,
				Tracker: core.TrackerState{
					Initialized:    true,
					LastAccounted:  now,
					EngagedUntil:   now.Add(5 * time.Minute),
					ContinuousWork: continuousWork,
					Revision:       1,
				},
				State:         state,
				StateRevision: 1,
			},
		}, nil
	})
	if err != nil {
		return err
	}
	fmt.Println("seeded", stateName, "state into", databasePath)
	return nil
}
