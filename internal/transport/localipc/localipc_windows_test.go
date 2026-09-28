//go:build windows

package localipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
	"github.com/LexLuc/walkout/internal/transport/ndjson"

	"golang.org/x/sys/windows"
)

var pipeSequence atomic.Uint64

type statusService struct{}

func (statusService) Status(context.Context) (protocol.HealthStatus, error) {
	return protocol.HealthStatus{
		SchemaVersion: protocol.SchemaVersion,
		State:         protocol.StateWorking,
	}, nil
}

func (statusService) ProcessEvent(
	context.Context,
	protocol.HealthEvent,
	core.AssessmentStatus,
) (protocol.HealthDecision, error) {
	return protocol.HealthDecision{}, errors.New("unexpected ProcessEvent call")
}

func (statusService) ExecuteCommand(
	context.Context,
	protocol.HealthControlCommand,
) (protocol.HealthControlResponse, error) {
	return protocol.HealthControlResponse{}, errors.New("unexpected ExecuteCommand call")
}

func TestNamedPipeRoundTripAndConcurrentRequestIsolation(t *testing.T) {
	handler := newStatusHandler(t)
	server, err := NewServer(Config{PipeName: uniquePipeName(t)}, handler)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	serveDone := serveServer(t, server)
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
		waitForServe(t, serveDone)
	})

	client := Client{PipeName: server.PipeName(), Timeout: time.Second}
	const clientCount = 2
	responses := make(chan ndjson.Response, clientCount)
	errorsSeen := make(chan error, clientCount)
	var group sync.WaitGroup
	for index := range clientCount {
		requestID := fmt.Sprintf("request-%d", index)
		group.Add(1)
		go func() {
			defer group.Done()
			response, callErr := client.Call(context.Background(), statusRequest(requestID))
			if callErr != nil {
				errorsSeen <- callErr
				return
			}
			responses <- response
		}()
	}
	group.Wait()
	close(responses)
	close(errorsSeen)
	for callErr := range errorsSeen {
		t.Errorf("Call() error = %v", callErr)
	}
	seen := make(map[string]bool, clientCount)
	for response := range responses {
		if !response.OK || response.Error != nil {
			t.Errorf("response = %#v, want success", response)
		}
		seen[response.RequestID] = true
	}
	for index := range clientCount {
		requestID := fmt.Sprintf("request-%d", index)
		if !seen[requestID] {
			t.Errorf("missing response for %q", requestID)
		}
	}
}

func TestNamedPipeIsSingleInstanceAndCloseReleasesIt(t *testing.T) {
	pipeName := uniquePipeName(t)
	first, err := NewServer(Config{PipeName: pipeName}, newStatusHandler(t))
	if err != nil {
		t.Fatalf("first NewServer() error = %v", err)
	}
	second, err := NewServer(Config{PipeName: pipeName}, newStatusHandler(t))
	if second != nil {
		_ = second.Close()
		t.Fatal("second NewServer() returned a server, want single-instance rejection")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second NewServer() error = %v, want ErrAlreadyRunning", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	replacement, err := NewServer(Config{PipeName: pipeName}, newStatusHandler(t))
	if err != nil {
		t.Fatalf("NewServer() after close error = %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatalf("replacement Close() error = %v", err)
	}
}

func TestMissingNamedPipeReturnsTypedUnavailableError(t *testing.T) {
	client := Client{PipeName: uniquePipeName(t), Timeout: DefaultTimeout}

	_, err := client.Call(context.Background(), statusRequest("unavailable"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Call() error = %v, want ErrUnavailable", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, missing pipe should fail before deadline", err)
	}
}

func TestUnresponsiveNamedPipeUsesSeventyFiveMillisecondDefaultDeadline(t *testing.T) {
	if DefaultTimeout != 75*time.Millisecond {
		t.Fatalf("DefaultTimeout = %v, want 75ms", DefaultTimeout)
	}
	server, err := NewServer(Config{PipeName: uniquePipeName(t)}, newStatusHandler(t))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	client := Client{PipeName: server.PipeName()}
	_, err = client.Call(context.Background(), statusRequest("stalled"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Call() error = %v, want ErrUnavailable", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want context deadline", err)
	}
}

func TestCallerDeadlineTakesPriorityOverClientTimeout(t *testing.T) {
	server, err := NewServer(Config{PipeName: uniquePipeName(t)}, newStatusHandler(t))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	client := Client{PipeName: server.PipeName(), Timeout: time.Minute}
	_, err = client.Call(ctx, statusRequest("caller-deadline"))
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Call() error = %v, want unavailable context deadline", err)
	}
}

func TestCurrentUserSecurityDescriptorIsProtectedAndLeastPrivilege(t *testing.T) {
	identity, err := CurrentUserIdentity()
	if err != nil {
		t.Fatalf("CurrentUserIdentity() error = %v", err)
	}
	if identity.SID == "" {
		t.Fatal("current user SID is empty")
	}
	if !strings.HasPrefix(identity.PipeName, `\\.\pipe\walkout-`) {
		t.Fatalf("pipe name = %q, want per-user walkout prefix", identity.PipeName)
	}

	sddl := identity.SecurityDescriptor
	if _, err := windows.SecurityDescriptorFromString(sddl); err != nil {
		t.Fatalf("security descriptor %q is invalid: %v", sddl, err)
	}
	for _, required := range []string{"D:P", identity.SID, ";;;SY)"} {
		if !strings.Contains(sddl, required) {
			t.Errorf("security descriptor %q does not contain %q", sddl, required)
		}
	}
	for _, forbidden := range []string{";;;WD)", ";;;AU)", ";;;BU)", ";;;BA)"} {
		if strings.Contains(sddl, forbidden) {
			t.Errorf("security descriptor %q grants broad principal %q", sddl, forbidden)
		}
	}
}

func newStatusHandler(t *testing.T) *ndjson.Handler {
	t.Helper()
	handler, err := ndjson.NewHandler(statusService{}, 0)
	if err != nil {
		t.Fatalf("ndjson.NewHandler() error = %v", err)
	}
	return handler
}

func statusRequest(requestID string) ndjson.Request {
	payload, err := json.Marshal(struct{}{})
	if err != nil {
		panic(err)
	}
	return ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		Operation:     ndjson.OperationStatus,
		Payload:       payload,
	}
}

func uniquePipeName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(
		`\\.\pipe\walkout-test-%d-%d`,
		os.Getpid(),
		pipeSequence.Add(1),
	)
}

func serveServer(t *testing.T, server *Server) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background())
	}()
	return done
}

func waitForServe(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Serve() did not stop after Close()")
	}
}
