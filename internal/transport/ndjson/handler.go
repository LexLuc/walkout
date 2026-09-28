package ndjson

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

const DefaultMaxRequestBytes = 64 * 1024

type Operation string

const (
	OperationStatus         Operation = "status"
	OperationProcessEvent   Operation = "process_event"
	OperationExecuteCommand Operation = "execute_command"
)

type ErrorCode string

const (
	ErrorInvalidRequest       ErrorCode = "invalid_request"
	ErrorRequestTooLarge      ErrorCode = "request_too_large"
	ErrorUnsupportedOperation ErrorCode = "unsupported_operation"
	ErrorInvalidPayload       ErrorCode = "invalid_payload"
	ErrorBusinessRejected     ErrorCode = "business_rejected"
	ErrorInternal             ErrorCode = "internal_error"
)

type Request struct {
	SchemaVersion string          `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	Operation     Operation       `json:"operation"`
	Payload       json.RawMessage `json:"payload"`
}

type RPCError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

type Response struct {
	SchemaVersion string          `json:"schema_version"`
	RequestID     string          `json:"request_id"`
	OK            bool            `json:"ok"`
	Payload       json.RawMessage `json:"payload"`
	Error         *RPCError       `json:"error"`
}

type ProcessEventPayload struct {
	Event            protocol.HealthEvent `json:"event"`
	AssessmentStatus string               `json:"assessment_status"`
}

type Service interface {
	Status(ctx context.Context) (protocol.HealthStatus, error)
	ProcessEvent(
		ctx context.Context,
		event protocol.HealthEvent,
		assessment core.AssessmentStatus,
	) (protocol.HealthDecision, error)
	ExecuteCommand(
		ctx context.Context,
		command protocol.HealthControlCommand,
	) (protocol.HealthControlResponse, error)
}

type Handler struct {
	service         Service
	maxRequestBytes int
}

func NewHandler(service Service, maxRequestBytes int) (*Handler, error) {
	if service == nil {
		return nil, errors.New("service is required")
	}
	if maxRequestBytes < 0 {
		return nil, errors.New("maximum request size cannot be negative")
	}
	if maxRequestBytes == 0 {
		maxRequestBytes = DefaultMaxRequestBytes
	}
	return &Handler{service: service, maxRequestBytes: maxRequestBytes}, nil
}

// Serve processes one JSON request and writes one JSON response per line. A
// protocol or business error affects only its own line; I/O and context errors
// terminate the connection.
func (h *Handler) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	encoder := json.NewEncoder(output)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, tooLarge, done, err := readBoundedLine(reader, h.maxRequestBytes)
		if err != nil {
			return err
		}
		if done && len(line) == 0 && !tooLarge {
			return nil
		}

		var response Response
		if tooLarge {
			response = errorResponse("", ErrorRequestTooLarge, "request exceeds size limit")
		} else {
			response = h.handle(ctx, line)
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (h *Handler) handle(ctx context.Context, line []byte) Response {
	if len(bytes.TrimSpace(line)) == 0 {
		return errorResponse("", ErrorInvalidRequest, "request must be a JSON object")
	}

	var request Request
	if err := decodeStrict(line, &request); err != nil {
		return errorResponse("", ErrorInvalidRequest, "request must be valid JSON")
	}
	if !protocol.IsSupportedSchema(request.SchemaVersion) || request.RequestID == "" || request.Operation == "" {
		return errorResponse(request.RequestID, ErrorInvalidRequest, "request envelope is invalid")
	}

	var payload any
	var err error
	switch request.Operation {
	case OperationStatus:
		var empty struct{}
		if err := decodeStrict(request.Payload, &empty); err != nil {
			return errorResponse(request.RequestID, ErrorInvalidPayload, "status payload must be empty")
		}
		payload, err = h.service.Status(ctx)
	case OperationProcessEvent:
		var operation ProcessEventPayload
		if err := decodeStrict(request.Payload, &operation); err != nil {
			return errorResponse(request.RequestID, ErrorInvalidPayload, "process_event payload is invalid")
		}
		assessment, valid := assessmentStatus(operation.AssessmentStatus)
		if !valid {
			return errorResponse(request.RequestID, ErrorInvalidPayload, "assessment_status is invalid")
		}
		payload, err = h.service.ProcessEvent(ctx, operation.Event, assessment)
	case OperationExecuteCommand:
		var command protocol.HealthControlCommand
		if err := decodeStrict(request.Payload, &command); err != nil {
			return errorResponse(request.RequestID, ErrorInvalidPayload, "execute_command payload is invalid")
		}
		payload, err = h.service.ExecuteCommand(ctx, command)
	default:
		return errorResponse(request.RequestID, ErrorUnsupportedOperation, "operation is not supported")
	}
	if err != nil {
		return serviceErrorResponse(request.RequestID, err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return errorResponse(request.RequestID, ErrorInternal, "daemon operation failed")
	}
	return Response{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     request.RequestID,
		OK:            true,
		Payload:       encoded,
		Error:         nil,
	}
}

func serviceErrorResponse(requestID string, err error) Response {
	switch {
	case errors.Is(err, application.ErrInvalidInput):
		return errorResponse(requestID, ErrorInvalidPayload, "operation payload is invalid")
	case errors.Is(err, application.ErrBusinessRejected):
		return errorResponse(requestID, ErrorBusinessRejected, "operation rejected by current health state")
	default:
		return errorResponse(requestID, ErrorInternal, "daemon operation failed")
	}
}

func errorResponse(requestID string, code ErrorCode, message string) Response {
	return Response{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		OK:            false,
		Payload:       nil,
		Error:         &RPCError{Code: code, Message: message},
	}
}

func assessmentStatus(value string) (core.AssessmentStatus, bool) {
	status := core.AssessmentStatus(value)
	switch status {
	case core.AssessmentMissing, core.AssessmentSafeNow, core.AssessmentFinishCurrentStep:
		return status, true
	default:
		return "", false
	}
}

func decodeStrict(encoded []byte, destination any) error {
	if len(encoded) == 0 {
		return io.ErrUnexpectedEOF
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func readBoundedLine(reader *bufio.Reader, limit int) (line []byte, tooLarge bool, done bool, err error) {
	for {
		fragment, isPrefix, readErr := reader.ReadLine()
		if !tooLarge {
			if len(line)+len(fragment) > limit {
				line = nil
				tooLarge = true
			} else {
				line = append(line, fragment...)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return line, tooLarge, true, nil
			}
			return nil, false, false, readErr
		}
		if !isPrefix {
			return line, tooLarge, false, nil
		}
	}
}

var _ Service = (*application.Service)(nil)
