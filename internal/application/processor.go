package application

import (
	"context"
	"errors"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

type Processor struct {
	engine *core.Engine
	policy core.Policy
	store  ResultStore
}

func NewProcessor(engine *core.Engine, policy core.Policy, store ResultStore) (*Processor, error) {
	if engine == nil {
		return nil, errors.New("engine is required")
	}
	if store == nil {
		return nil, errors.New("result store is required")
	}
	return &Processor{engine: engine, policy: policy, store: store}, nil
}

func (p *Processor) Process(
	ctx context.Context,
	event protocol.HealthEvent,
	assessment core.AssessmentStatus,
) (protocol.HealthDecision, error) {
	var checkpoint core.EngineState
	applied := false
	result, err := p.store.ProcessOnce(ctx, event.EventID, func() (StoredResult, error) {
		checkpoint = p.engine.ExportState()
		applied = true
		before := p.engine.Snapshot()
		if !protocol.IsSupportedSchema(event.SchemaVersion) {
			return StoredResult{
				Decision:    p.policy.Decide(policyInput(event, before, assessment)),
				EngineState: p.engine.ExportState(),
			}, nil
		}
		if err := validateSupportedEvent(event); err != nil {
			return StoredResult{}, err
		}
		if err := p.applyEvent(event); err != nil {
			return StoredResult{}, err
		}
		after := p.engine.Snapshot()
		return StoredResult{
			Decision:    p.policy.Decide(policyInput(event, after, assessment)),
			EngineState: p.engine.ExportState(),
		}, nil
	})
	if err != nil {
		if applied {
			if rollbackErr := p.engine.RestoreState(checkpoint); rollbackErr != nil {
				return protocol.HealthDecision{}, errors.Join(err, rollbackErr)
			}
		}
		return protocol.HealthDecision{}, err
	}
	return result.Decision, nil
}

func (p *Processor) applyEvent(event protocol.HealthEvent) error {
	switch event.ActivityKind {
	case protocol.ActivityKindHumanInput:
		return p.engine.ObserveHumanInteractionAt(event.SessionID, event.OccurredAt)
	case protocol.ActivityKindAgentWork, protocol.ActivityKindToolWork:
		return p.engine.ObserveAgentActivityAt(event.SessionID, event.OccurredAt)
	case protocol.ActivityKindSessionLifecycle:
		return p.engine.TickAt(event.OccurredAt)
	default:
		return invalidInput("unsupported activity kind")
	}
}

func validateSupportedEvent(event protocol.HealthEvent) error {
	if event.SessionID == "" {
		return invalidInput("session ID is required")
	}
	if event.OccurredAt.IsZero() {
		return invalidInput("occurred_at is required")
	}
	return nil
}

func policyInput(
	event protocol.HealthEvent,
	snapshot core.EngineSnapshot,
	assessment core.AssessmentStatus,
) core.PolicyInput {
	return core.PolicyInput{
		Event:                event,
		State:                snapshot.State,
		StateRevision:        snapshot.StateRevision,
		HealthDebt:           snapshot.HealthDebt,
		AssessmentStatus:     assessment,
		EmergencyLeaseActive: snapshot.EmergencyLeaseActive,
	}
}
