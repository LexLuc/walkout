---
description: Grant a Walkout emergency-continue lease (optionally with a reason) so a paused session can keep working.
argument-hint: '[reason]'
disable-model-invocation: true
allowed-tools: Bash
---

!`"${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe" emergency-continue -reason "$ARGUMENTS"`

The emergency continue is now active. In one short line, tell the user the lease status (active or not, seconds remaining, and the recorded reason); do not read or modify any files and do not take any other action.
