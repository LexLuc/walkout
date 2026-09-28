package adapters

import (
	"context"
	"errors"
	"testing"

	"github.com/LexLuc/walkout/internal/protocol"
)

type fakeTranslator struct {
	event protocol.HealthEvent
	err   error
}

func (t fakeTranslator) Translate([]byte) (protocol.HealthEvent, error) {
	return t.event, t.err
}

type fakeDecisionClient struct {
	decision protocol.HealthDecision
	err      error
}

func (c fakeDecisionClient) Decide(context.Context, protocol.HealthEvent) (protocol.HealthDecision, error) {
	return c.decision, c.err
}

type fakeRenderer struct {
	result HookResult
	err    error
	calls  int
}

func (r *fakeRenderer) Render(protocol.HealthEvent, protocol.HealthDecision) (HookResult, error) {
	r.calls++
	return r.result, r.err
}

func TestRunnerFailsOpenWithoutWritingSensitiveOrDiagnosticOutput(t *testing.T) {
	secret := []byte("CONFIDENTIAL_CLIENT_MATTER")
	internalFailure := errors.New(`database failed at D:\private\health.db`)
	validEvent := protocol.HealthEvent{EventID: "event-1"}
	validDecision := protocol.HealthDecision{
		SchemaVersion: protocol.SchemaVersion,
		SourceEventID: "event-1",
		Action:        protocol.ActionAllow,
	}
	tests := []struct {
		name       string
		translator Translator
		client     DecisionClient
		renderer   *fakeRenderer
	}{
		{"translation error", fakeTranslator{err: internalFailure}, fakeDecisionClient{}, &fakeRenderer{}},
		{"daemon error", fakeTranslator{event: validEvent}, fakeDecisionClient{err: internalFailure}, &fakeRenderer{}},
		{"schema mismatch", fakeTranslator{event: validEvent}, fakeDecisionClient{decision: protocol.HealthDecision{SchemaVersion: "2.0", SourceEventID: "event-1"}}, &fakeRenderer{}},
		{"source mismatch", fakeTranslator{event: validEvent}, fakeDecisionClient{decision: protocol.HealthDecision{SchemaVersion: protocol.SchemaVersion, SourceEventID: "other"}}, &fakeRenderer{}},
		{"render error", fakeTranslator{event: validEvent}, fakeDecisionClient{decision: validDecision}, &fakeRenderer{err: internalFailure}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner, err := NewRunner(test.translator, test.client, test.renderer)
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}
			result := runner.Handle(context.Background(), secret)
			if !result.FailOpen || result.ExitCode != 0 || len(result.Stdout) != 0 || len(result.Stderr) != 0 {
				t.Fatalf("result = %#v, want silent fail-open", result)
			}
		})
	}
}

func TestRunnerRendersValidatedDecision(t *testing.T) {
	event := protocol.HealthEvent{EventID: "event-1"}
	decision := protocol.HealthDecision{
		SchemaVersion: protocol.SchemaVersion,
		SourceEventID: "event-1",
		Action:        protocol.ActionAllow,
	}
	want := HookResult{ExitCode: 0, Stdout: []byte(`{"continue":true}`)}
	renderer := &fakeRenderer{result: want}
	runner, err := NewRunner(
		fakeTranslator{event: event},
		fakeDecisionClient{decision: decision},
		renderer,
	)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	got := runner.Handle(context.Background(), []byte(`{}`))
	if got.FailOpen || got.ExitCode != want.ExitCode || string(got.Stdout) != string(want.Stdout) {
		t.Fatalf("Handle() = %#v, want %#v", got, want)
	}
	if renderer.calls != 1 {
		t.Fatalf("Render() calls = %d, want 1", renderer.calls)
	}
}
