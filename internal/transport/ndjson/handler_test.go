package ndjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type failingService struct {
	err error
}

func (s failingService) Status(context.Context) (protocol.HealthStatus, error) {
	return protocol.HealthStatus{}, s.err
}

func (s failingService) ProcessEvent(
	context.Context,
	protocol.HealthEvent,
	core.AssessmentStatus,
) (protocol.HealthDecision, error) {
	return protocol.HealthDecision{}, s.err
}

func (s failingService) ExecuteCommand(
	context.Context,
	protocol.HealthControlCommand,
) (protocol.HealthControlResponse, error) {
	return protocol.HealthControlResponse{}, s.err
}

func TestHandlerDispatchesAllOperationsAndPreservesRequestIDs(t *testing.T) {
	t.Parallel()

	handler := mustHandler(t, newApplicationService(t), 0)
	start := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	event := protocol.HealthEvent{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "event-1",
		Provider:      protocol.ProviderCodex,
		HostVersion:   "test",
		SessionID:     "session-a",
		EventType:     protocol.EventTypePromptSubmit,
		OccurredAt:    start,
		ActivityKind:  protocol.ActivityKindHumanInput,
		Payload:       map[string]json.RawMessage{},
	}
	requests := []Request{
		request("request-status", OperationStatus, map[string]any{}),
		request("request-event", OperationProcessEvent, ProcessEventPayload{
			Event:            event,
			AssessmentStatus: string(core.AssessmentMissing),
		}),
		request("request-command", OperationExecuteCommand, protocol.HealthControlCommand{
			SchemaVersion: protocol.SchemaVersion,
			CommandID:     "command-1",
			CommandType:   protocol.CommandConfirmActivity,
		}),
	}

	responses := serveRequests(t, handler, requests...)
	if len(responses) != len(requests) {
		t.Fatalf("response count = %d, want %d", len(responses), len(requests))
	}
	for index, response := range responses {
		if !response.OK || response.Error != nil {
			t.Fatalf("response %d = %#v, want success", index, response)
		}
		if response.RequestID != requests[index].RequestID {
			t.Fatalf("response request ID = %q, want %q", response.RequestID, requests[index].RequestID)
		}
	}

	var status protocol.HealthStatus
	if err := json.Unmarshal(responses[0].Payload, &status); err != nil {
		t.Fatalf("decode status payload error = %v", err)
	}
	if status.State != protocol.StateWorking {
		t.Fatalf("initial status state = %q, want working", status.State)
	}
	var decision protocol.HealthDecision
	if err := json.Unmarshal(responses[1].Payload, &decision); err != nil {
		t.Fatalf("decode decision payload error = %v", err)
	}
	if decision.SourceEventID != event.EventID {
		t.Fatalf("decision source event ID = %q, want %q", decision.SourceEventID, event.EventID)
	}
	var command protocol.HealthControlResponse
	if err := json.Unmarshal(responses[2].Payload, &command); err != nil {
		t.Fatalf("decode command payload error = %v", err)
	}
	if command.Status.State != protocol.StateWorking {
		t.Fatalf("command state = %q, want working", command.Status.State)
	}
	if command.Status.ActivityCount != 1 {
		t.Fatalf("command activity count = %d, want 1", command.Status.ActivityCount)
	}
}

func TestHandlerReturnsStructuredErrorsAndContinues(t *testing.T) {
	t.Parallel()

	handler := mustHandler(t, newApplicationService(t), 0)
	secret := "confidential-client-matter"
	input := strings.Join([]string{
		"",
		`{"schema_version":"1.0","request_id":"broken"`,
		string(mustJSON(t, request("unknown", Operation("unknown"), map[string]any{}))),
		string(mustJSON(t, request("invalid-payload", OperationExecuteCommand, map[string]any{
			"schema_version": protocol.SchemaVersion,
			"command_id":     "bad-command",
			"command_type":   protocol.CommandConfirmActivity,
			"activity_type":  "future_activity",
			"work_context":   secret,
		}))),
		string(mustJSON(t, request("after-errors", OperationStatus, map[string]any{}))),
	}, "\n") + "\n"

	var output bytes.Buffer
	if err := handler.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 5 {
		t.Fatalf("response count = %d, want 5", len(responses))
	}
	wantCodes := []ErrorCode{ErrorInvalidRequest, ErrorInvalidRequest, ErrorUnsupportedOperation, ErrorInvalidPayload}
	for index, want := range wantCodes {
		if responses[index].OK || responses[index].Error == nil || responses[index].Error.Code != want {
			t.Fatalf("response %d = %#v, want error code %q", index, responses[index], want)
		}
	}
	if !responses[4].OK || responses[4].RequestID != "after-errors" {
		t.Fatalf("response after errors = %#v, want successful status", responses[4])
	}
	if bytes.Contains(output.Bytes(), []byte(secret)) {
		t.Fatal("error response leaked rejected work context")
	}
}

func TestHandlerRejectsOversizedLineAndProcessesTheNextRequest(t *testing.T) {
	t.Parallel()

	handler := mustHandler(t, newApplicationService(t), 256)
	valid := mustJSON(t, request("after-large", OperationStatus, map[string]any{}))
	input := strings.Repeat("x", 300) + "\n" + string(valid) + "\n"

	var output bytes.Buffer
	if err := handler.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	responses := decodeResponses(t, output.Bytes())
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != ErrorRequestTooLarge {
		t.Fatalf("oversized response = %#v, want request_too_large", responses[0])
	}
	if !responses[1].OK || responses[1].RequestID != "after-large" {
		t.Fatalf("response after oversized line = %#v, want success", responses[1])
	}
}

func businessRejectedForTest() error {
	return fmt.Errorf("%w: not available in this state", application.ErrBusinessRejected)
}

func TestHandlerSeparatesBusinessRejectionFromInternalFailure(t *testing.T) {
	t.Parallel()

	businessHandler := mustHandler(t, failingService{err: businessRejectedForTest()}, 0)
	businessResponses := serveRequests(t, businessHandler, request(
		"business-rejected",
		OperationStatus,
		map[string]any{},
	))
	if businessResponses[0].Error == nil || businessResponses[0].Error.Code != ErrorBusinessRejected {
		t.Fatalf("business response = %#v, want business_rejected", businessResponses[0])
	}

	internalDetail := `database failed at D:\private\health.db for confidential matter`
	internalHandler := mustHandler(t, failingService{err: errors.New(internalDetail)}, 0)
	internalResponses := serveRequests(t, internalHandler, request("internal", OperationStatus, map[string]any{}))
	if internalResponses[0].Error == nil || internalResponses[0].Error.Code != ErrorInternal {
		t.Fatalf("internal response = %#v, want internal_error", internalResponses[0])
	}
	encoded := mustJSON(t, internalResponses[0])
	if bytes.Contains(encoded, []byte(internalDetail)) || bytes.Contains(encoded, []byte(`D:\private`)) {
		t.Fatal("internal response leaked implementation detail")
	}
}

func newApplicationService(t *testing.T) *application.Service {
	t.Helper()
	service, err := application.NewService(
		context.Background(),
		core.DefaultConfig(),
		fixedClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)},
		core.NewPolicy(),
		application.NewMemoryResultStore(),
	)
	if err != nil {
		t.Fatalf("application.NewService() error = %v", err)
	}
	return service
}

func mustHandler(t *testing.T, service Service, maxRequestBytes int) *Handler {
	t.Helper()
	handler, err := NewHandler(service, maxRequestBytes)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func request(id string, operation Operation, payload any) Request {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     id,
		Operation:     operation,
		Payload:       encoded,
	}
}

func serveRequests(t *testing.T, handler *Handler, requests ...Request) []Response {
	t.Helper()
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for _, request := range requests {
		if err := encoder.Encode(request); err != nil {
			t.Fatalf("encode request error = %v", err)
		}
	}
	var output bytes.Buffer
	if err := handler.Serve(context.Background(), &input, &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	return decodeResponses(t, output.Bytes())
}

func decodeResponses(t *testing.T, encoded []byte) []Response {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var responses []Response
	for decoder.More() {
		var response Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("decode response error = %v\n%s", err, encoded)
		}
		responses = append(responses, response)
	}
	return responses
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}
