package claudecode

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/LexLuc/walkout/internal/adapters"
	"github.com/LexLuc/walkout/internal/protocol"
)

// Renderer maps a HealthDecision onto the Claude Code hook output verified by
// the recorded probe: a UserPromptSubmit block is stdout JSON
// {"decision":"block","reason":...} with exit code 0, and overtime reminders
// are hookSpecificOutput.additionalContext gated on the inject_context
// capability. Actions without a verified host mechanism degrade to a silent
// allow-through instead of guessing host behavior.
type Renderer struct {
	// CtlCommand is the command the injected instruction tells the model to run
	// for the natural-language "I moved" confirmation. The hook CLI sets it to
	// the absolute path of the bundled walkout-ctl.exe; the bare name is a
	// PATH-dependent fallback.
	CtlCommand string
}

func NewRenderer() *Renderer {
	return &Renderer{CtlCommand: "walkout-ctl"}
}

type blockOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// Walkout block copy targets behavior only, makes no health-damage claims, and
// always names both the confirm entry and the emergency-continue escape hatch
// required by privacy-and-safety.md.
var pauseReasonCopy = map[protocol.ReasonCode]string{
	protocol.ReasonDebtLimit: "Your agent has walked out because health debt reached its limit; move around and confirm with /walkout:done, or use /walkout:continue [reason] for a 15-minute emergency continue.",
}

const pauseReasonFallback = "Your agent has walked out; move around and confirm with /walkout:done, or use /walkout:continue [reason] for a 15-minute emergency continue."

// Overtime reminder instructions are addressed to the model: it phrases the
// reminder in the user's current work context (so copy is not one fixed
// sentence) and executes the natural-language confirmation when the user says
// they have already moved. Tone escalates once debt starts accruing.
// The situation is stated in plain words and the internal vocabulary is
// banned explicitly, because the model tends to parrot whatever terms the
// instruction uses into the user-facing sentence.
const reminderInstructionTemplate = "Break reminder (system note to the assistant): the user has been sitting and working for a long stretch without a break%s. After completing this response, append one short reminder in your own words, tied to their current task and targeting only the sitting behavior, phrased the way a considerate colleague would say it, to stand up and move, mentioning that /walkout:done confirms the break. Do not use system terminology such as 'health debt', 'debt', 'pause limit', 'break interval' or 'walkout'. If the user's message itself clearly states they have already taken a break or moved, instead run `%s done` with your shell tool and acknowledge it in one short line without doing anything else."

var reminderEscalation = map[protocol.ReasonCode]string{
	protocol.ReasonBreakDue:    "",
	protocol.ReasonDebtGrowing: ", and has kept going after earlier reminders, so make this reminder noticeably firmer and more direct than a first nudge",
}

type injectOutput struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

func (r *Renderer) Render(event protocol.HealthEvent, decision protocol.HealthDecision) (adapters.HookResult, error) {
	switch decision.Action {
	case protocol.ActionAllow:
		return adapters.HookResult{}, nil
	case protocol.ActionPausePrompt:
		if event.EventType != protocol.EventTypePromptSubmit || !hasCapability(event, protocol.CapabilityBlockPrompt) {
			return adapters.HookResult{}, nil
		}
		return renderBlock(decision.ReasonCode)
	case protocol.ActionInjectReminder:
		// additionalContext injection is gated on the inject_context capability;
		// it stays a silent allow-through until the capability is declared for
		// the target host version.
		if event.EventType != protocol.EventTypePromptSubmit || !hasCapability(event, protocol.CapabilityInjectContext) {
			return adapters.HookResult{}, nil
		}
		return r.renderReminder(decision.ReasonCode)
	default:
		return adapters.HookResult{}, fmt.Errorf("unknown decision action %q", decision.Action)
	}
}

func renderBlock(reason protocol.ReasonCode) (adapters.HookResult, error) {
	copyText, ok := pauseReasonCopy[reason]
	if !ok {
		copyText = pauseReasonFallback
	}
	stdout, err := json.Marshal(blockOutput{Decision: "block", Reason: copyText})
	if err != nil {
		return adapters.HookResult{}, err
	}
	return adapters.HookResult{ExitCode: 0, Stdout: stdout}, nil
}

func (r *Renderer) renderReminder(reason protocol.ReasonCode) (adapters.HookResult, error) {
	escalation, ok := reminderEscalation[reason]
	if !ok {
		// An unmapped reason has no defined reminder phase; stay silent.
		return adapters.HookResult{}, nil
	}
	ctl := strings.TrimSpace(r.CtlCommand)
	if ctl == "" {
		ctl = "walkout-ctl"
	}
	var output injectOutput
	output.HookSpecificOutput.HookEventName = "UserPromptSubmit"
	output.HookSpecificOutput.AdditionalContext = fmt.Sprintf(reminderInstructionTemplate, escalation, ctl)
	stdout, err := json.Marshal(output)
	if err != nil {
		return adapters.HookResult{}, err
	}
	return adapters.HookResult{ExitCode: 0, Stdout: stdout}, nil
}

func hasCapability(event protocol.HealthEvent, capability protocol.Capability) bool {
	return slices.Contains(event.Capabilities, capability)
}
