package application

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

// maxEmergencyContinueReasonRunes bounds the optional accountability note so a
// stray paste cannot bloat the persisted state; the reason is metadata, not the
// session transcript. It is measured in runes to stay fair to CJK text.
const maxEmergencyContinueReasonRunes = 256

type Service struct {
	config    core.Config
	clock     core.Clock
	engine    *core.Engine
	processor *Processor
	store     Store
}

func NewService(
	ctx context.Context,
	config core.Config,
	clock core.Clock,
	policy core.Policy,
	store Store,
) (*Service, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	engine, err := RestoreEngine(ctx, config, clock, store)
	if err != nil {
		return nil, err
	}
	processor, err := NewProcessor(engine, policy, store)
	if err != nil {
		return nil, err
	}
	return &Service{
		config:    config,
		clock:     clock,
		engine:    engine,
		processor: processor,
		store:     store,
	}, nil
}

func (s *Service) ProcessEvent(
	ctx context.Context,
	event protocol.HealthEvent,
	assessment core.AssessmentStatus,
) (protocol.HealthDecision, error) {
	return s.processor.Process(ctx, event, assessment)
}

// Status reads the last committed state through the Store serialization
// boundary. It never creates an event or advances domain time.
func (s *Service) Status(ctx context.Context) (protocol.HealthStatus, error) {
	state, exists, err := s.store.LoadLatestState(ctx)
	if err != nil {
		return protocol.HealthStatus{}, err
	}
	if !exists {
		return healthStatus(s.engine.Snapshot()), nil
	}
	committed, err := core.NewEngineFromState(s.config, s.clock, state)
	if err != nil {
		return protocol.HealthStatus{}, err
	}
	return healthStatus(committed.Snapshot()), nil
}

func (s *Service) ExecuteCommand(
	ctx context.Context,
	command protocol.HealthControlCommand,
) (protocol.HealthControlResponse, error) {
	if err := validateCommand(command); err != nil {
		return protocol.HealthControlResponse{}, err
	}

	var checkpoint core.EngineState
	applied := false
	result, err := s.store.ProcessCommandOnce(ctx, command.CommandID, func() (StoredCommandResult, error) {
		checkpoint = s.engine.ExportState()
		applied = true
		if err := s.applyCommand(command); err != nil {
			return StoredCommandResult{}, err
		}
		return StoredCommandResult{
			Response: protocol.HealthControlResponse{
				SchemaVersion: protocol.SchemaVersion,
				CommandID:     command.CommandID,
				CommandType:   command.CommandType,
				Status:        healthStatus(s.engine.Snapshot()),
			},
			EngineState: s.engine.ExportState(),
		}, nil
	})
	if err != nil {
		if applied {
			if rollbackErr := s.engine.RestoreState(checkpoint); rollbackErr != nil {
				return protocol.HealthControlResponse{}, errors.Join(err, rollbackErr)
			}
		}
		return protocol.HealthControlResponse{}, err
	}
	return result.Response, nil
}

func (s *Service) applyCommand(command protocol.HealthControlCommand) error {
	switch command.CommandType {
	case protocol.CommandConfirmActivity:
		return s.engine.ConfirmActivity()
	case protocol.CommandEmergencyContinue:
		return s.engine.StartEmergencyContinue(command.Reason)
	default:
		return invalidInput("unsupported control command")
	}
}

func validateCommand(command protocol.HealthControlCommand) error {
	if !protocol.IsSupportedSchema(command.SchemaVersion) {
		return invalidInput("unsupported control schema")
	}
	if command.CommandID == "" {
		return invalidInput("command ID is required")
	}
	switch command.CommandType {
	case protocol.CommandConfirmActivity:
		if command.Reason != "" {
			return invalidInput("confirm_activity does not accept a reason")
		}
	case protocol.CommandEmergencyContinue:
		if utf8.RuneCountInString(command.Reason) > maxEmergencyContinueReasonRunes {
			return invalidInput("emergency_continue reason is too long")
		}
	default:
		return invalidInput("unsupported control command")
	}
	return nil
}

func healthStatus(snapshot core.EngineSnapshot) protocol.HealthStatus {
	// The engine keeps the last override reason across restarts, but status
	// only reports it while the lease is active: a reason beside an inactive
	// lease reads as stale, whether it ended by confirmation or by expiry.
	reason := ""
	if snapshot.EmergencyLeaseActive {
		reason = snapshot.EmergencyLeaseReason
	}
	return protocol.HealthStatus{
		SchemaVersion:                    protocol.SchemaVersion,
		State:                            snapshot.State,
		StateRevision:                    snapshot.StateRevision,
		ContinuousWorkSeconds:            durationSeconds(snapshot.ContinuousWork),
		HealthDebtSeconds:                durationSeconds(snapshot.HealthDebt),
		EngagementActive:                 snapshot.EngagementActive,
		ActivityCount:                    snapshot.ActivityCount,
		EmergencyContinueActive:          snapshot.EmergencyLeaseActive,
		EmergencyContinueRemainingSecond: durationSeconds(snapshot.EmergencyLeaseRemaining),
		EmergencyContinueReason:          reason,
	}
}

func durationSeconds(duration time.Duration) int64 {
	return int64(duration / time.Second)
}
