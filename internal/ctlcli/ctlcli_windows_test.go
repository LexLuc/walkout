//go:build windows

package ctlcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

var ctlPipeSequence atomic.Int64

func ctlPipeName() string {
	return fmt.Sprintf(
		`\\.\pipe\walkout-ctl-test-%d-%d`,
		os.Getpid(),
		ctlPipeSequence.Add(1),
	)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(1_786_000_000, 0).UTC() }

func startRealDaemonPipe(t *testing.T, pipeName string) {
	t.Helper()
	service, err := application.NewService(
		context.Background(),
		core.DefaultConfig(),
		fixedClock{},
		core.NewPolicy(),
		application.NewMemoryResultStore(),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	handler, err := ndjson.NewHandler(service, 0)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server, err := localipc.NewServer(localipc.Config{PipeName: pipeName}, handler)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	t.Cleanup(func() { _ = server.Close() })
}

func ctlArgs(subcommand, pipeName string) []string {
	return []string{subcommand, "-pipe", pipeName, "-timeout", "2s"}
}

func TestRunConfirmsActivityInOneStepOverRealPipe(t *testing.T) {
	pipeName := ctlPipeName()
	startRealDaemonPipe(t, pipeName)

	var doneOut, doneErr bytes.Buffer
	code := Run(context.Background(), ctlArgs("done", pipeName), &doneOut, &doneErr)
	if code != 0 || doneErr.Len() != 0 {
		t.Fatalf("done exit/stderr = %d/%q, want 0 and empty", code, doneErr.String())
	}
	if !strings.Contains(doneOut.String(), "state: working") {
		t.Fatalf("done stdout = %q, want state working", doneOut.String())
	}
	if !strings.Contains(doneOut.String(), "activity_count: 1") {
		t.Fatalf("done stdout = %q, want activity_count 1", doneOut.String())
	}
	if !strings.Contains(doneOut.String(), "health_debt_seconds: 0") {
		t.Fatalf("done stdout = %q, want cleared debt", doneOut.String())
	}
}

func TestRunGrantsEmergencyContinueOverRealPipe(t *testing.T) {
	pipeName := ctlPipeName()
	startRealDaemonPipe(t, pipeName)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), ctlArgs("emergency-continue", pipeName), &stdout, &stderr)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("emergency-continue exit/stderr = %d/%q, want 0 and empty", code, stderr.String())
	}
	// The command round-trips and prints a status; the daemon started at
	// working so state is unchanged, but the call must succeed cleanly.
	if !strings.Contains(stdout.String(), "state:") {
		t.Fatalf("emergency-continue stdout = %q, want a status block", stdout.String())
	}
}

func TestRunStatusReflectsEmergencyLeaseOverRealPipe(t *testing.T) {
	pipeName := ctlPipeName()
	startRealDaemonPipe(t, pipeName)

	// Before any lease, status reports the escape hatch inactive.
	var beforeOut, beforeErr bytes.Buffer
	if code := Run(context.Background(), ctlArgs("status", pipeName), &beforeOut, &beforeErr); code != 0 || beforeErr.Len() != 0 {
		t.Fatalf("status exit/stderr = %d/%q, want 0 and empty", code, beforeErr.String())
	}
	if !strings.Contains(beforeOut.String(), "state:") {
		t.Fatalf("status stdout = %q, want a status block", beforeOut.String())
	}
	if !strings.Contains(beforeOut.String(), "emergency_continue_active: false") {
		t.Fatalf("status stdout = %q, want lease inactive before grant", beforeOut.String())
	}

	// Grant the lease, then status must show it active with remaining time.
	var grantOut, grantErr bytes.Buffer
	if code := Run(context.Background(), ctlArgs("emergency-continue", pipeName), &grantOut, &grantErr); code != 0 {
		t.Fatalf("emergency-continue failed: %d/%q", code, grantErr.String())
	}
	var afterOut, afterErr bytes.Buffer
	if code := Run(context.Background(), ctlArgs("status", pipeName), &afterOut, &afterErr); code != 0 || afterErr.Len() != 0 {
		t.Fatalf("status exit/stderr = %d/%q, want 0 and empty", code, afterErr.String())
	}
	if !strings.Contains(afterOut.String(), "emergency_continue_active: true") {
		t.Fatalf("status stdout = %q, want lease active after grant", afterOut.String())
	}
	if !strings.Contains(afterOut.String(), "emergency_continue_remaining_seconds:") {
		t.Fatalf("status stdout = %q, want remaining seconds line", afterOut.String())
	}
	if strings.Contains(afterOut.String(), "emergency_continue_remaining_seconds: 0") {
		t.Fatalf("status stdout = %q, want positive remaining seconds within the window", afterOut.String())
	}
}

func TestRunStatusReflectsEmergencyReasonOverRealPipe(t *testing.T) {
	pipeName := ctlPipeName()
	startRealDaemonPipe(t, pipeName)

	const reason = "prod-incident"
	grantArgs := append(ctlArgs("emergency-continue", pipeName), "-reason", reason)
	var grantOut, grantErr bytes.Buffer
	if code := Run(context.Background(), grantArgs, &grantOut, &grantErr); code != 0 || grantErr.Len() != 0 {
		t.Fatalf("emergency-continue exit/stderr = %d/%q, want 0 and empty", code, grantErr.String())
	}

	var statusOut, statusErr bytes.Buffer
	if code := Run(context.Background(), ctlArgs("status", pipeName), &statusOut, &statusErr); code != 0 || statusErr.Len() != 0 {
		t.Fatalf("status exit/stderr = %d/%q, want 0 and empty", code, statusErr.String())
	}
	if !strings.Contains(statusOut.String(), "emergency_continue_reason: "+reason) {
		t.Fatalf("status stdout = %q, want a reason line with %q", statusOut.String(), reason)
	}
}

func TestRunReportsVisibleErrorWhenDaemonUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), ctlArgs("done", ctlPipeName()), &stdout, &stderr)
	if code == 0 {
		t.Fatal("done exit = 0 with no daemon, want a non-zero user-visible failure")
	}
	if stderr.Len() == 0 {
		t.Fatal("done produced no stderr, want a user-visible error (control plane is not fail-open)")
	}
	if stdout.Len() != 0 {
		t.Fatalf("done stdout = %q, want empty on failure", stdout.String())
	}
}

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	// snooze, start-break, and confirm-recovery were removed by the
	// three-state simplification and must fail fast as unknown verbs.
	tests := [][]string{
		nil,
		{"pause", "-pipe", ctlPipeName()},
		{"snooze", "-pipe", ctlPipeName()},
		{"start-break", "-pipe", ctlPipeName()},
		{"confirm-recovery", "-pipe", ctlPipeName()},
		{"done", "-unexpected"},
	}
	for _, args := range tests {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, &stdout, &stderr)
		if code == 0 {
			t.Fatalf("Run(%v) exit = 0, want non-zero for invalid usage", args)
		}
		if stderr.Len() == 0 {
			t.Fatalf("Run(%v) produced no stderr, want usage guidance", args)
		}
	}
}
