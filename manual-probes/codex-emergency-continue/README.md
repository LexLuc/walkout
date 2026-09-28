# Codex 对话内紧急继续真机探针

本探针验证一条完整、可重复的宿主链路：预置自洽的 `walkout` 状态，启动真实 `walkoutd`，让真实 Codex TUI 从已安装的生产插件触发 `SessionStart`、`UserPromptSubmit` 与 `SessionEnd` hook，再分别提交普通提示词、`$walkout:continue` 主入口与 `!walkout-ctl` 兜底，最后回读 lease 状态。

完整的可复用验收用例（含 2026-09 实测结果与操作纪律）见 [`../acceptance-test-cases.md`](../acceptance-test-cases.md)；本文负责环境准备、脚本与清理。

## 为什么必须用真实 TUI

单元测试和命名管道契约测试能证明 adapter、RPC、engine 与 renderer 的组合行为，不能证明目标 Codex 版本会安装 plugin、加载 plugin-bundled hook、替换 `${PLUGIN_ROOT}`、解析 skill、保留原始 prompt 文本或以预期方式显示阻止反馈。真机探针把这些宿主边界作为版本化能力验证，而不是长期假设。

## 隐私边界

- 本轮分发探针直接执行生产 plugin-bundled hook；`start-codex.ps1` 只给该 Codex 子进程设置 `WALKOUT_EVENT_PROBE_OUTPUT`，由正式 `walkout-hook.exe` 在真实 daemon 成功接收事件后写入 `.tmp/codex-emergency-probe/records/health-events.jsonl`。
- `health-events.jsonl` 每行只含 `schema_version`、`provider`、`host_version`、`event_type`；不记录 prompt、session ID、cwd、reason 或原始 payload。
- 已有 `internal/adapters/codex/testdata/0.146.0/` fixture 继续负责宿主 payload 契约；需要重新录制时才单独启用目录内既有 `record-and-run.ps1`，并按原脱敏规则处理。
- 插件 skill 不复述或解释 reason，不读文件、不调用工具、不继续先前工作。

## 准备

以下命令中的 `<repo-root>` 指本仓库检出目录的绝对路径。在普通 PowerShell 中运行：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/prepare.ps1
codex plugin marketplace add <repo-root>/.tmp/codex-emergency-probe/marketplace
codex plugin add walkout@walkout-probe
```

`prepare.ps1` 会构建生产 `walkout-hook.exe`，在 `.tmp/codex-emergency-probe/` 中构建 daemon/CLI、复制完整待测插件、生成临时 marketplace，并写入自洽暂停态数据库。probe 根目录不会生成项目级 `.codex/hooks.json`，因此三个生命周期事件只能来自安装后的插件。脚本本身不修改 Codex 的个人配置；后两条显式命令会添加临时 marketplace 并安装待测插件。

## 作用域与副作用

| 动作 | 生命周期与影响范围 |
| --- | --- |
| `prepare.ps1` | 只写仓库内 `.tmp/codex-emergency-probe/`，不修改 Codex 用户配置或系统 PATH |
| `codex plugin marketplace add` | 持久写入 Codex 用户配置，所有后续 Codex 会话都能看到该临时 marketplace，直至显式移除 |
| `codex plugin add` | 持久安装 `walkout` plugin，直至显式移除；`continue` skill 禁止隐式调用，普通 prompt 不会主动触发它 |
| `start-daemon.ps1` | 前台进程存活期间占用当前用户的默认 Walkout 命名管道；其他启用了 Walkout hook 的 Codex/Claude Code 会话可能连接到这份测试状态，测试期间不要并行使用这些会话 |
| `start-codex.ps1` | 只给它启动的 Codex 子进程临时追加 probe `bin/` 到 PATH、设置脱敏事件元数据输出路径并把工作目录切到 probe；不会修改系统/用户 PATH 或全局环境变量 |
| plugin `.codex/hooks.json` | 随临时 plugin 复制进 Codex 安装缓存；三个事件都通过 `${PLUGIN_ROOT}` 执行同一生产 wrapper 与 hook binary |
| hook 信任 | Codex 按 plugin、事件和 command hash 持久保存信任；命令变化后需在新会话逐事件重新授权 |

## 两个终端

终端 A 保持 daemon 运行：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/start-daemon.ps1
```

终端 B 启动真实 Codex TUI：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/start-codex.ps1
```

首次运行按提示信任临时项目，并逐一授权 `SessionStart`、`UserPromptSubmit` 与 `SessionEnd`。Codex 以事件和 command hash 保存授权；hook 命令变化后必须在新会话重新授权受影响事件。

## TUI 内验证序列

1. 输入 `$` 并确认列表中可发现 `walkout:done`、`walkout:continue` 与 `walkout:status`。
2. 输入普通提示词 `hello`；预期被 `walkout` 态阻止，并显示同时包含 `$walkout:done`、`$walkout:continue [reason]` 与固定 `!walkout-ctl ...` 兜底的英文单句。
3. 输入 `$walkout:continue 生产事故热修复`；预期同一控制提交不被阻止，模型只输出 skill 约束的一句请求确认，不读文件、不调用工具。
4. 输入 `!walkout-ctl status`；预期 `state: walkout`、`emergency_continue_active: true`、reason 原样记录，债务未被赦免。再输入 `$walkout:status`；预期模型以脱沙箱权限执行 `walkout-ctl status` 并原样打印同样的字段（auto 模式无批准提示；无 lease 的 `walkout` 态下该 skill 提交本身会被阻断，与 Claude 的 `/walkout:status` 一致）。
5. 再输入普通提示词 `Reply with exactly OK and do not use tools.`；预期放行。
6. 输入 `!walkout-ctl emergency-continue -reason "fallback probe"`；预期 shell 兜底在暂停 hook 之外执行，并续期既有 lease。
7. 再输入 `!walkout-ctl status`；预期 reason 更新为 `fallback probe`，剩余时间接近 900 秒。

退出 TUI 时确认 `SessionEnd` 没有拖住退出；该事件的生产 timeout 上限为 3 秒。

## Overtime 提醒注入与活动确认序列（三态新增验收点）

这一段验证此前从未真机验证过的宿主能力：`additionalContext` 注入是否真的进入模型上下文，以及 `$walkout:done` 与自然语言反馈是否可用。先在终端 A 按 Ctrl+C 停 daemon，再重置为 overtime 态并重启：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/reset.ps1 -State overtime
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/start-daemon.ps1
```

终端 B 重新 `start-codex.ps1` 后：

1. 输入 `$` 并确认列表中可发现 `walkout:done`。
2. 输入普通提示词 `What is 2+2? Answer briefly.`；预期**不被阻断**，且模型在回答末尾用自己的话追加一句只针对久坐行为、结合当前任务的起身提醒，并提到 `$walkout:done`。若回答完全没有提醒，说明 `additionalContext` 注入在该 Codex 版本未生效——记录版本号，这是预置的降级讨论触发条件。
3. 再输入一条普通提示词；预期提醒语气升级（债务累积中，措辞更坚决）。
4. 输入 `$walkout:done`；预期模型只回一句确认。
5. 输入 `!walkout-ctl status`；预期 `state: working`、`activity_count: 1`、连续工作时长已清零。
6. （自然语言反馈）停 daemon → `reset.ps1 -State overtime` → 重启 daemon 与 TUI，先发一条普通提示词收到提醒后，输入 `我刚才已经起来活动了五分钟`；预期模型不做其他事，只用 shell 工具**以脱沙箱（escalated）权限**执行插件捆绑的 `walkout-ctl.exe done` 并用一句话确认。随后 `!walkout-ctl status` 应显示 `state: working`。若模型不执行或授权体验不可接受，记录现象——预案是从注入指令中移除自然语言分支，仅保留命令通道。

   2026-09-28 真机（Codex `0.156.1`）：模型 shell 工具默认在沙箱内执行，打开 daemon 命名管道报 `Access is denied`（只读 `status` 同样失败，已证实是沙箱而非命令本身）；注入指令因此要求脱沙箱执行。auto 模式下脱沙箱申请静默通过、无批准提示；on-request 审批策略下应预期对该命令的一次批准；`never` 策略下申请被拒、回退为模型一句话请用户提交 `$walkout:done`。

## 版本源核对

托管安装可在启动 TUI 的同一 shell 中核对 `$env:CODEX_MANAGED_PACKAGE_ROOT/package.json` 的 `version`。生产 wrapper 从该文件读取版本并传给 translator；变量缺失、文件不可读或 JSON 无效时传入空字符串，由 translator 明确记为 `"unknown"`。独立二进制回落不关闭守卫。

退出 TUI 后读取 `records/health-events.jsonl`：托管安装的真实事件应直接显示当前 `host_version`；用 native binary 绕过 JS launcher 的复验应显示 `"unknown"`。该文件记录的是 translator 生成且被 daemon 成功接受的最终 `HealthEvent` 元数据，而不是从 package 文件单独推断的预期值。

## 重跑

重跑前先在终端 A 按 Ctrl+C 停止 daemon（daemon 仍在运行时 `reset.ps1` 会因 `state.db` 被占用而失败），再运行：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/reset.ps1            # 默认 walkout
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/reset.ps1 -State overtime
```

两条操作纪律（2026-09-28 踩坑）：本机任何启用了 Walkout 的 Claude Code / Codex 进程都共享默认管道，其 hook 与 `/walkout:*`、`$walkout:*` 命令会直接改写探针 daemon 的状态——包括你用来记录结果的那个工作会话；卸载插件不影响已启动的进程（hook 与命令在进程启动时加载一次），须重启该进程。预置后先用 `!walkout-ctl status`（或探针 `bin\walkout-ctl.exe status`）核对状态与预期一致，再进入 TUI 序列。

## 完整清理

清理前必须依次完成：

1. 退出终端 B 中的 Codex TUI，确保 `start-codex.ps1` 已结束。
2. 在终端 A 按 Ctrl+C 停止 probe daemon；清理脚本检测到 probe daemon 仍运行时会拒绝继续。
3. 确认没有仍需保留的 probe 状态或诊断产物。

默认清理只移除持久的 plugin 与 marketplace，保留 `.tmp` probe 产物：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/cleanup.ps1
```

确认 probe 产物不再需要后，用显式开关完成零运行产物清理：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/cleanup.ps1 -RemoveArtifacts
```

`-RemoveArtifacts` 只允许递归删除经过绝对路径校验、且位于本仓库 `.tmp/` 下的 `codex-emergency-probe`。如果 plugin 或 marketplace 移除失败，脚本不会删除 marketplace 源目录，以免留下指向不存在路径的持久配置。

清理脚本可重复运行：plugin 或 marketplace 已经不存在时会视为清理完成，不会因“未找到”而阻止后续安全的产物删除。

Codex 当前没有按单条 plugin hook hash 删除信任记录的专用 CLI。本探针不自动编辑用户 `config.toml`；plugin 与 marketplace 被删除后，该信任记录不再有可加载的 hook 定义，属于无行为影响的孤立元数据。若发布流程要求配置文件字节级零残留，应先只读定位精确条目，再经人工确认单独删除。

## 彻底清理清单（零残留验收，真机多轮验证 2026-08-25）

`cleanup.ps1 -RemoveArtifacts` 之外，还有两类残留 **不会** 被任何 Codex 命令自动清除：

1. **插件缓存**：`codex plugin remove` + `codex plugin marketplace remove` 均不清 `~/.codex/plugins/cache/<marketplace 名>/`，须手动删除该目录。
2. **信任孤儿段**：`~/.codex/config.toml` 中该插件每个事件一条的 `[hooks.state."<plugin>@<marketplace>:.codex/hooks.json:<event>:0:0"]` 段。安全清理手法：
   - 先备份 `config.toml`；
   - 只读定位每条段的精确行区间（段头 + `trusted_hash` 行 + 相邻空行），确认无其他配置混入后删除；
   - 用 `codex plugin marketplace list` 验证配置仍可解析（exit 0）；
   - 验证通过后删除备份。

零残留验收命令：缓存目录不存在、`config.toml` 中 `grep` 不到插件名、`codex plugin list` 中无该 marketplace、无 `walkoutd` 进程存活、仓库 `.tmp/` 下无 probe 目录。
