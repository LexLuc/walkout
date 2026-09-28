package adapters

import (
	"context"
	"errors"

	"github.com/LexLuc/walkout/internal/protocol"
)

type Translator interface {
	Translate(input []byte) (protocol.HealthEvent, error)
}

type DecisionClient interface {
	Decide(ctx context.Context, event protocol.HealthEvent) (protocol.HealthDecision, error)
}

type Renderer interface {
	Render(event protocol.HealthEvent, decision protocol.HealthDecision) (HookResult, error)
}

// HookResult contains only bytes intended for the host process streams.
// FailOpen is adapter-internal and must not be serialized to the host.
type HookResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	FailOpen bool
}

type Runner struct {
	translator Translator
	client     DecisionClient
	renderer   Renderer
}

func NewRunner(translator Translator, client DecisionClient, renderer Renderer) (*Runner, error) {
	if translator == nil {
		return nil, errors.New("translator is required")
	}
	if client == nil {
		return nil, errors.New("decision client is required")
	}
	if renderer == nil {
		return nil, errors.New("renderer is required")
	}
	return &Runner{translator: translator, client: client, renderer: renderer}, nil
}

func (r *Runner) Handle(ctx context.Context, input []byte) HookResult {
	event, err := r.translator.Translate(input)
	if err != nil {
		return failOpen()
	}
	decision, err := r.client.Decide(ctx, event)
	if err != nil {
		return failOpen()
	}
	if !protocol.IsSupportedSchema(decision.SchemaVersion) ||
		decision.SourceEventID != event.EventID ||
		decision.Action == "" ||
		decision.Action == protocol.ActionFailOpen {
		return failOpen()
	}
	result, err := r.renderer.Render(event, decision)
	if err != nil {
		return failOpen()
	}
	return result
}

func failOpen() HookResult {
	return HookResult{ExitCode: 0, FailOpen: true}
}
