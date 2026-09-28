# Walkout

**Your agent walks out until you do.** / 你不动，它罢工。

English below · [中文在后半部分](#walkout-中文)

> **AI-assisted, human-reviewed.** This project's code and documents were drafted with AI assistance and reviewed by a human maintainer. Product decisions, privacy rules, and anything resembling health advice are the maintainer's responsibility, not the model's.

Walkout is a local health guard for people who work through AI coding agents. It measures how long you have been *actively* driving Claude Code or Codex, reminds you to stand up once you pass a work interval, and—if you keep ignoring it—has your agent walk out: the next ordinary prompt is paused until you confirm you have moved. An emergency-continue escape hatch always exists, and nothing leaves your machine.

It is an opt-in self-commitment device, not surveillance and not an unbypassable control.

## How it works

Three user-visible states, one shared timer across all your agent sessions:

| State | Enters when | What you experience | How you leave |
| --- | --- | --- | --- |
| `working` | timer starts, or you confirm activity | nothing; silent counting | — |
| `overtime` | 45 min of continuous engaged work | every prompt still runs, and your agent appends a short reminder in its own words, tied to what you are doing; the longer you keep going, the firmer it gets. Engaged minutes past the interval accrue as *health debt* | confirm activity (command or, in Claude Code / Codex, plain language) |
| `walkout` | health debt reaches 120 min | the agent walks out: your next ordinary prompt is paused with a one-line notice | confirm activity (command), or take a 15-minute emergency continue |

Confirming activity clears the timer and the debt in one step from any state. Walkout does not measure or verify your break; ignoring reminders is the only "snooze", and its cost is expressed as debt.

Engagement is inferred from your interaction with any session (a 5-minute human-interaction window), not from agent runtime: background tool calls do not extend it, and multiple sessions count as one union.

## Controls

| | Claude Code | Codex | Shell fallback |
| --- | --- | --- | --- |
| Confirm activity | `/walkout:done` | `$walkout:done` | `walkout-ctl done` |
| Emergency continue (15 min, optional reason) | `/walkout:continue [reason]` | `$walkout:continue [reason]` | `walkout-ctl emergency-continue -reason "…"` |
| Status | `/walkout:status` | `$walkout:status` | `walkout-ctl status` |

In `overtime`, simply telling the agent you already took a break also works: it runs the confirmation for you and acknowledges in one line. In `walkout` your prompt never reaches the model, so only the commands apply. The emergency continue never forgives debt; when it expires, the walkout resumes.

## Install (Windows x64)

Walkout is three small Go binaries plus one plugin per host. V1 has no installer or service registration yet; you build from source and start the daemon yourself.

Prerequisites: Go 1.26+, Claude Code and/or Codex CLI, PowerShell 7.

```powershell
git clone https://github.com/LexLuc/walkout.git
cd walkout
go build -o bin/walkoutd.exe ./cmd/walkoutd
go build -o bin/walkout-ctl.exe ./cmd/walkout-ctl
```

Add `bin\` to your PATH (Codex's `$walkout:status` skill and the `!walkout-ctl` fallback rely on it), then start the daemon in a terminal you keep open:

```powershell
.\bin\walkoutd.exe
```

State lives in `%LocalAppData%\Walkout\walkout.db`; the daemon listens on a per-user named pipe.

**Claude Code plugin**

```powershell
pwsh -NoProfile -File plugins/walkout/build.ps1
claude plugin marketplace add <absolute path to this clone>
claude plugin install walkout@lexicon
```

Restart Claude Code. `/walkout` lists `done`, `continue`, `status`; the plugin's bundled hooks forward lifecycle events to the daemon.

**Codex plugin**

```powershell
pwsh -NoProfile -File plugins/walkout-codex/build.ps1
codex plugin marketplace add <absolute path to this clone>
codex plugin add walkout@lexicon
```

Start a new Codex session and approve the three hook events (`SessionStart`, `UserPromptSubmit`, `SessionEnd`) when asked. `$` lists the three skills. Codex runs model-issued shell commands in a sandbox that cannot open the daemon's pipe, so the `status` skill and the plain-language confirmation request escalated execution; in auto mode this is silent, under an on-request approval policy you will see one prompt per call.

**Do not install from the GitHub marketplace form yet.** `claude plugin marketplace add LexLuc/walkout` and `codex plugin marketplace add LexLuc/walkout` both succeed, but the plugin binaries are build outputs that are not in the repository, so the installed plugin has no `walkout-hook.exe` and its guard never engages (hooks fail open silently). Use the local-clone form above until prebuilt release binaries ship.

## Defaults

45 min work interval · 120 min debt limit · 5 min human-interaction window · 15 min emergency continue. These are starting parameters, not medical conclusions.

## Privacy and safety

- Everything runs locally: hooks talk to a local daemon over a named pipe; no network, no telemetry, no prompt content stored. The daemon persists only timing state.
- Reminders may be blunt about the *behavior* (sitting, skipping reminders), never about identity, body, ability, or health; no fear appeals, no medical claims.
- A walkout only pauses ordinary new prompts after the current turn finishes; saving, exporting, and the emergency continue remain available.
- Details: `privacy-and-safety.md` (Chinese).

## Uninstall

`uninstall-and-cleanup.md` covers both plugins, the daemon, its data, and the residues host commands do not remove (plugin caches, Codex hook-trust entries).

## Development

```powershell
go test ./...
go vet ./...
```

TDD throughout; all time-dependent code takes an injected clock. Domain core in `internal/core`, wire protocol in `internal/protocol`, host adapters in `internal/adapters`, daemon in `internal/daemon`. Real-machine acceptance cases: `manual-probes/acceptance-test-cases.md`. Design documents are in Chinese: `product-spec.md` (state machine, source of truth), `logical-architecture.md` (protocol), `v1-scope.md` (release gates), `agent-lifecycle-hooks-explained.md` (how host hooks make this possible), `experiment-plan.md`, `agent-prompt.md`, `development-status.md` (current intent; read before contributing).

## License

MIT — see `LICENSE`.

---

# Walkout（中文）

**你不动，它罢工。** / Your agent walks out until you do.

> **AI 辅助与人工审核声明**：本项目的代码与文档由 AI 辅助起草，并由人类维护者审核。产品决策、隐私规则与任何近似健康建议的内容由维护者负责，不由模型负责。

Walkout 是一个面向"通过 AI 编码 Agent 工作的人"的本地健康守卫。它统计你**实际驱动** Claude Code 或 Codex 的连续时长，超过工作间隔后提醒你起身；若持续忽略，就让你的 Agent 罢工——下一条普通提示词被暂停，直到你确认已经活动。紧急继续的逃生口永远存在，且没有任何数据离开你的机器。

它是用户主动选择的自我承诺装置，不是监控，也不是无法绕过的管控。

## 工作原理

三个用户可见状态，所有 Agent 会话共享同一个计时：

| 状态 | 进入条件 | 你的体验 | 离开方式 |
| --- | --- | --- | --- |
| `working` | 开始计时，或确认已活动 | 静默计时，无打扰 | — |
| `overtime` | 连续参与工作达 45 分钟 | 每条提示词照常执行，Agent 在回答末尾用自己的话、结合你正在做的事追加一句起身提醒；继续越久，语气越坚决。超出间隔后的参与分钟数累计为**健康债务** | 确认已活动（命令，或在 Claude Code / Codex 中直接用自然语言说） |
| `walkout` | 健康债务达到 120 分钟 | Agent 罢工：下一条普通提示词被暂停，并显示一句提示 | 确认已活动（仅命令），或获得 15 分钟紧急继续 |

确认已活动从任意状态一步清零计时与债务。Walkout 不测量、不验证休息时长；忽略提醒是唯一的"延后"，其代价由债务表达。

参与时间按你与任意会话的交互推断（5 分钟人类交互窗口），不是 Agent 运行时间：后台工具调用不延长窗口，多个会话取并集。

## 控制入口

| | Claude Code | Codex | Shell 兜底 |
| --- | --- | --- | --- |
| 确认已活动 | `/walkout:done` | `$walkout:done` | `walkout-ctl done` |
| 紧急继续（15 分钟，可附原因） | `/walkout:continue [原因]` | `$walkout:continue [原因]` | `walkout-ctl emergency-continue -reason "…"` |
| 状态 | `/walkout:status` | `$walkout:status` | `walkout-ctl status` |

`overtime` 期直接告诉 Agent"我已经活动过了"同样有效：它会替你执行确认并一句话回应。`walkout` 期提示词到不了模型，只能用命令。紧急继续不赦免债务，到期后罢工恢复。

## 安装（Windows x64）

Walkout 由三个小的 Go 二进制加每宿主一个插件组成。V1 尚无安装器与服务注册：从源码构建，自行启动 daemon。

前置：Go 1.26+、Claude Code 和/或 Codex CLI、PowerShell 7。

```powershell
git clone https://github.com/LexLuc/walkout.git
cd walkout
go build -o bin/walkoutd.exe ./cmd/walkoutd
go build -o bin/walkout-ctl.exe ./cmd/walkout-ctl
```

把 `bin\` 加入 PATH（Codex 的 `$walkout:status` skill 与 `!walkout-ctl` 兜底依赖它），然后在一个保持打开的终端里启动 daemon：

```powershell
.\bin\walkoutd.exe
```

状态保存在 `%LocalAppData%\Walkout\walkout.db`；daemon 监听按用户隔离的命名管道。

**Claude Code 插件**

```powershell
pwsh -NoProfile -File plugins/walkout/build.ps1
claude plugin marketplace add <本仓库检出目录的绝对路径>
claude plugin install walkout@lexicon
```

重启 Claude Code。`/walkout` 可见 `done`、`continue`、`status`；插件打包的 hook 会把生命周期事件转发给 daemon。

**Codex 插件**

```powershell
pwsh -NoProfile -File plugins/walkout-codex/build.ps1
codex plugin marketplace add <本仓库检出目录的绝对路径>
codex plugin add walkout@lexicon
```

新开 Codex 会话，按提示批准三个 hook 事件（`SessionStart`、`UserPromptSubmit`、`SessionEnd`）。`$` 可见三个 skill。Codex 把模型发出的 shell 命令放在沙箱里执行、无法打开 daemon 的管道，因此 `status` skill 与自然语言确认会申请脱沙箱执行：auto 模式静默，on-request 审批策略下每次调用弹一次批准。

**暂勿以 GitHub marketplace 形式安装。** `claude plugin marketplace add LexLuc/walkout` 与 `codex plugin marketplace add LexLuc/walkout` 都能成功，但插件二进制是构建产物、不在仓库中，安装后的插件没有 `walkout-hook.exe`，守卫永远不会生效（hook 静默失败放行）。在提供预构建发布二进制之前，请使用上面的本地检出方式。

## 默认参数

工作间隔 45 分钟 · 债务上限 120 分钟 · 人类交互窗口 5 分钟 · 紧急继续 15 分钟。这些是初始参数，不是医学结论。

## 隐私与安全

- 全部本地运行：hook 经命名管道与本地 daemon 通信；无网络、无遥测、不保存提示词内容，daemon 只持久化计时状态。
- 提醒可以对**行为**（久坐、跳过提醒）直言不讳，但不涉及身份、身体、能力或健康；不用恐吓，不做医学断言。
- 罢工只在当前 turn 完成后暂停新的普通提示词；保存、导出与紧急继续始终可用。
- 详见 `privacy-and-safety.md`。

## 卸载

`uninstall-and-cleanup.md` 覆盖两宿主插件、daemon、数据，以及宿主命令不会清除的残留（插件缓存、Codex hook 信任记录）。

## 开发

```powershell
go test ./...
go vet ./...
```

全程 TDD；所有时间相关代码注入可替换时钟。领域核心在 `internal/core`，协议在 `internal/protocol`，宿主适配器在 `internal/adapters`，daemon 在 `internal/daemon`。真机验收用例：`manual-probes/acceptance-test-cases.md`。设计文档：`product-spec.md`（状态机，唯一事实来源）、`logical-architecture.md`（协议）、`v1-scope.md`（发布门槛）、`agent-lifecycle-hooks-explained.md`（宿主 hook 原理详解）、`experiment-plan.md`、`agent-prompt.md`、`development-status.md`（当前开发意图，贡献前必读）。

## 产品命题（设计背景）

它面向所有正在用 AI Agent 完成电脑工作的群体——法律、会计与财务、设计、研究、运营、咨询、行政、管理和软件开发，不只服务程序员。V1 接入 Codex 与 Claude Code 两个宿主；其他宿主（如 WorkBuddy）待两者发布并稳定后再考虑。

"提醒起身"本身不难，难的是提醒经常出现在用户最不愿被打断的时候，而普通通知很快被习惯性忽略。产品因此要解决三个更具体的问题：识别合适的打断时机（区分连续工作、会议、演示、专注写作和已离席）；建立有效但不过界的升级机制（提醒可以有压力和个性，同时避免羞辱、恐吓和妨碍紧急工作）；以最少的数据证明行动发生（可选摄像头实验只能较可靠地判断"人是否离开画面"，不证明其他任何事）。

核心假设：**当提醒具有人格、后果逐步升级，并且完成动作的成本低于继续忽略的成本时，用户更可能真正离席。** 这需要实验验证：毒舌文案可能短期提高行动率，也可能造成反感、关闭功能或降低信任，因此它应是用户主动选择的人格风格，而不是默认强加。毒舌只针对行为，不攻击身份、外貌、能力或意志力，例如："你已经把身体当外设晾了 50 分钟。起来，让它重新连上。""导出还在跑，别陪进度条一起坐牢。""Agent 暂停营业。请带着你的膝盖去完成一次现实世界的移动。"

第一版只验证三件事：用户是否愿意开启持续工作计时；哪种提醒强度能提高离席完成率；罢工与可恢复流程是否提高行动率而不显著提高关闭率。摄像头确认是单独的可选实验（本地判断离开画面至少 20 秒），不阻塞基础版本。逐工具阻断、系统级监控、日历与办公连接器、原生设置应用、云同步和企业管理均排除；不尝试推断健康状况或给出医疗建议。

成功指标：主要看提醒后 5 分钟内的确认活动率；护栏指标是功能关闭率、强制退出率、负面反馈率；长期看每工作日有效离席次数与连续久坐超过 90 分钟的次数。不用"通知点击率"作核心指标——点击不等于行为改变。

## 许可证

MIT，见 `LICENSE`。
