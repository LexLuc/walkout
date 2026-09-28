# AGENTS.md

This file provides guidance to AI coding agents (Claude Code, Codex, and others) when working in this repository.

## What this repository is

A product specification and Go codebase for **Walkout** — a local AI health-intervention system that coordinates Codex, Claude Code, WorkBuddy, and future Agent hosts through lifecycle hooks, a shared `walkoutd` daemon, and deterministic health policies. Product documents remain in Simplified Chinese; code identifiers and protocol fields use English.

The implementation is developed with TDD. Go is the primary language for the daemon, protocol, state machine, persistence, and CLI because the V1 target is a low-latency Windows x64 local service distributed as a single executable. Host adapters should stay thin and use the minimum language/runtime required by each host.

## Development workflow

- Follow red → green → refactor for every behavior change: add or update a failing test, implement the smallest passing behavior, then clean up while keeping tests green.
- Prefer table-driven unit tests for deterministic policies and state transitions.
- Inject a fake clock into all time-dependent code; do not use real sleeps in unit tests.
- Add recorded hook JSON fixtures and contract tests before implementing each host adapter.
- Keep the domain core independent from SQLite, named pipes, MCP, CLI, and host-specific payloads.
- Run `go test ./...` after each slice and `go vet ./...` before handoff.
- Treat `product-spec.md` as the source of truth for product behavior and `logical-architecture.md` as the source of truth for protocol boundaries.
- Read `development-status.md` before starting a development slice and update it when the current intent, assumption, redirection condition, or next action changes.
- Use `development-status.md` for information Git and tests cannot explain: why the current slice is next, which assumption it validates, its behavioral acceptance criteria, unresolved questions, and evidence that would change direction.
- Do not duplicate commit hashes, changed-file lists, test output, coverage history, or completed-step logs in `development-status.md`; Git, tests, and CI own that evidence.
- Keep `development-status.md` current by replacing completed intent with the next active slice rather than appending a historical diary.

## Document roles

- `README.md` — public entry point, bilingual (English first, Chinese second): mechanism, controls, install, privacy, uninstall pointer, and the product thesis / hypothesis / success metrics in the Chinese half. Carries the AI-assisted disclosure.
- `product-spec.md` — the **source of truth for the state machine** (`working → overtime → walkout`, simplified per the 2026-08-26 ruling), default timing parameters, camera verification flow, and open questions.
- `experiment-plan.md` — three A/B experiments (reminder persona, escalation consequence, verification method), the minimal event/telemetry model, and pre-committed continue/stop thresholds.
- `privacy-and-safety.md` — hard boundaries for camera use, forced pause, and sharp-tongued copy. These act as content rules for the whole repo (see below).
- `agent-prompt.md` — the reminder agent's system prompt draft and its JSON output schema (`tone` / `message` / `primary_action` / `secondary_action` / `next_reminder_minutes`).
- `agent-lifecycle-hooks-explained.md` — technical feasibility deep-dive: how Codex / Claude Code lifecycle hooks plus a local `walkoutd` daemon could implement the product; hook I/O contracts, capability limits, and a phased implementation roadmap.
- `logical-architecture.md` — source of truth for `HealthEvent`, `HealthDecision`, Agent/daemon responsibility boundaries, and host capability probes.
- `v1-scope.md` — source of truth for V1 delivery boundaries and release gates.
- `development-status.md` — mandatory start-of-session context: current development intent, assumptions, redirection conditions, and next action; deliberately not a history log.
- `manual-probes/acceptance-test-cases.md` — reusable real-machine acceptance test cases for both hosts (inputs, expected output, observed results with host versions, operating rules); the probe READMEs own environment setup and cleanup.
- `uninstall-and-cleanup.md` — the complete uninstall and zero-residue cleanup procedure for both host plugins, the daemon, its data, and probe artifacts, including residues no host command removes.

## Cross-document and code consistency

The default parameters — 45 min work interval, 120 min debt limit, 5 min human-interaction window, 20 s optional camera absence, and 15 min emergency extension (no explicit snooze or grace period since the 2026-08-26 three-state simplification) — must stay aligned across `README.md`, `product-spec.md`, `v1-scope.md`, code defaults, and tests. The canonical state machine names and escalation order in `product-spec.md`, `logical-architecture.md`, `agent-lifecycle-hooks-explained.md`, code, and tests must also stay aligned.

## Content boundaries (apply to all copy and prompts)

These come from `privacy-and-safety.md` and the README, and any drafted reminder copy or prompt edits must respect them:

- Sharp-tongued ("毒舌") copy may only target behavior (sitting, skipping reminders); never identity, appearance, weight, age, disability, illness, ability, or mental health. No death/sudden-death fear appeals.
- Only claim what can be proven: the product may say "you left the frame for 20 seconds", never "you drank water" or assert health damage/medical conclusions.
- Camera processing is local-only: no upload, no recording, no face recognition; output is limited to present/absent/uncertain plus duration. Always offer a no-camera alternative.
- Forced pause only suspends non-essential agent capabilities; save/export/emergency/accessibility functions stay available, and an emergency-continue escape hatch always exists.
- The escalation mechanism is a user-opted self-commitment device, not an unbypassable control — documents should state this honestly rather than overclaim enforcement.

`agent-lifecycle-hooks-explained.md` carries an AI-assisted / human-reviewed disclosure near the top; preserve it, and add the same disclosure to any new public-facing technical article in this repo.
