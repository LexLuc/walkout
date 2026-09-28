# Walkout 三态真机验收测试用例

本文把 2026-09-25 → 2026-09-28 的真机验收过程固化为可复用的测试用例，供插件、宿主版本或提醒文案变更后复验。用例只描述**用户可观察的输入与输出**；机制层证据由单元测试、契约测试与探针 README 承载。

- 首次执行结果：Claude Code `2.1.281`、Codex CLI `0.156.1`（Windows 11，模型分别为 Claude Fable 5.1 与 GPT-6-Astra），全部用例通过。
- 探针脚本与清理步骤见 `claude-plugin-install/README.md` 与 `codex-emergency-continue/README.md`；本文只引用，不重复。
- 用例 ID 前缀：`CA`（Claude 阻断链）、`CB`（Claude 提醒链）、`CU`（Claude 升级）、`XA`（Codex 阻断链）、`XB`（Codex 提醒链）、`XS`（Codex 沙箱确认实验）、`XP`（Codex 对等入口）、`U`（两宿主并集）、`L`（lease 生命周期）。

## 0. 环境准备与操作纪律

### 0.1 需要的窗口

| 窗口 | 用途 |
| --- | --- |
| 终端 A | 前台运行探针 `walkoutd`（`start-daemon.ps1`），换预置状态时 Ctrl+C 停止 |
| 终端 B | 被测宿主 TUI（Codex 用 `start-codex.ps1`；Claude 在探针项目目录运行 `claude`） |
| 终端 C | 记录结果、执行安装/重置命令的工作窗口 |

### 0.2 预置状态

`reset.ps1` 支持两种预置，均要求 daemon 已停止（否则 `state.db` 被占用报错）：

| 参数 | 预置内容 | 用途 |
| --- | --- | --- |
| （默认）`walkout` | 连续工作 170 分钟、债务 125 分钟、无 lease | 阻断链、逃生口、lease 用例 |
| `-State overtime` | 连续工作 50 分钟、债务 5 分钟、无 lease | 提醒注入、活动确认用例（首条提醒即为"债务累积"档，属正确） |

两宿主探针的 daemon 可互换使用（同一默认管道），并集用例正依赖这一点。

### 0.3 操作纪律（本轮踩坑）

1. **本机所有启用 Walkout 的宿主进程共享同一 daemon。** 记录结果的工作会话若装有插件，其 hook 会把每条消息计入工作时间，在 `walkout` 预置下会被阻断，而 `/walkout:*` 命令会直接改写探针状态。做法：工作会话使用未安装插件的宿主，或先卸载插件。
2. **卸载插件不影响已启动的宿主进程**（hook 与命令在进程启动时加载一次）。卸载后必须重启该进程才真正脱离。
3. **预置后先核对再进 TUI**：`walkout-ctl status`（探针 `bin\` 下或 `!walkout-ctl status`）应显示预期状态；若已被其他进程改写，停 daemon 重新 `reset.ps1`。
4. 改动 hook 二进制或 skill 后，Codex 侧须 `codex plugin remove` + `codex plugin add` 让缓存更新；Claude 侧本地路径 marketplace 按引用运行源目录，`build.ps1` 后无需重装（升级流程除外）。
5. 只改 daemon 时，`prepare.ps1` 重建后重启终端 A 即可，宿主无需重装。

## 1. Claude Code — 阻断链（预置 `walkout`）

前置：`claude-plugin-install/prepare.ps1` → `claude plugin marketplace add <repo-root>` → `claude plugin install walkout@lexicon` → 终端 A `start-daemon.ps1` → 终端 B 在 `.tmp/walkout-plugin-probe/project` 运行 `claude`。

| ID | 输入 | 预期 | 2026-09-25 实测 |
| --- | --- | --- | --- |
| CA1 | `/walkout` | 列表出现 `walkout:done`、`walkout:continue`、`walkout:status` 及描述 | 通过 |
| CA2 | `hello` | 被阻断；英文单句同时含 `/walkout:done` 与 `/walkout:continue [reason]` | 通过；`2.1.281` 额外回显 `Original prompt: hello` |
| CA3 | 按 ↑ | 被阻的 `hello` 原样回填可重发 | 通过（`History 6/6`） |
| CA4 | `/walkout:continue production-hotfix` | 不被阻断；模型只回一句：lease 激活、900 秒、reason 原样 | 通过 |
| CA5 | `/walkout:status` | `state: walkout`、`emergency_continue_active: true`、reason 原样、债务不变 | 通过（债务 7688 s 未赦免） |

## 2. Claude Code — 提醒注入与活动确认（预置 `overtime`）

前置：终端 B `/exit`，终端 A Ctrl+C，`reset.ps1 -State overtime`，`start-daemon.ps1`，终端 B 重新 `claude`。

| ID | 输入 | 预期 | 实测 |
| --- | --- | --- | --- |
| CB1 | `What is 2+2? Answer briefly.` | 不被阻断；回答末尾追加一句模型自拟的起身提醒，贴合当前任务、只针对久坐、提到 `/walkout:done`；**不得出现** "health debt / debt / pause limit / break interval / walkout" 等术语 | 09-25 通过但措辞含 "health debt / pause limit"（已修）；09-28 复验通过、无术语 |
| CB2 | `What is 3+3? Answer briefly.` | 再次提醒，措辞明显比上一条更坚决、更直接 | 09-28 通过（"you're still in the chair after the last reminder"） |
| CB3 | `/walkout:done` | 一句确认 | 通过 |
| CB4 | `/walkout:status` | `state: working`、`activity_count: 1`、债务 0；输出为原始字段原样转述，两次调用格式一致 | 09-25 格式不稳定（已修）；09-28 两次一致 |
| CB5 | （重新预置 overtime 后）`What is 2+2? Answer briefly.` | 出现提醒 | 通过 |
| CB6 | `我刚才已经起来活动了五分钟` | 模型只用 Bash 执行插件捆绑的 `walkout-ctl.exe done`，一句确认，不做他事 | 通过；auto mode 下 Bash 由分类器放行、无弹窗（默认权限模式应预期一次授权提示） |
| CB7 | `/walkout:status` | `state: working` | 通过 |

## 3. Claude Code — 插件升级（门槛 11）

前置：退出 Claude、停 daemon；把 `plugins/walkout/.claude-plugin/plugin.json` 与 `.claude-plugin/marketplace.json` 的 `version` 同步升为新版本（契约测试 `TestWalkoutClaudePluginVersionMatchesMarketplace` 强制两者一致）。

| ID | 输入 | 预期 | 2026-09-25 实测 |
| --- | --- | --- | --- |
| CU1 | `claude plugin marketplace update lexicon` | 成功 | 通过 |
| CU2 | `claude plugin update walkout@lexicon` | 报告 `updated from 0.1.0 to 0.1.1` | 通过 |
| CU3 | `claude plugin details walkout@lexicon` | 显示新版本；`Skills (3)`、`Hooks (4)` | 通过 |
| CU4 | 重启 daemon 与 `claude`，`/walkout:status` | 命令仍可用；启动**不**重新要求信任插件 hooks | 通过（静默启动） |

验证后回退两个版本文件。

## 4. Codex — 阻断链与逃生口（预置 `walkout`）

前置：`codex-emergency-continue/prepare.ps1` → `codex plugin marketplace add <repo-root>\.tmp\codex-emergency-probe\marketplace` → `codex plugin add walkout@walkout-probe` → 终端 A `start-daemon.ps1` → 终端 B `start-codex.ps1`，按提示逐一信任 `SessionStart` / `UserPromptSubmit` / `SessionEnd`。

| ID | 输入 | 预期 | 2026-09-28 实测 |
| --- | --- | --- | --- |
| XA1 | `$` | 列表出现 `Walkout Status`、`Confirm Activity`、`Emergency Continue` 三个 skill | 通过（status skill 于同日加入后复验） |
| XA2 | `hello` | `Blocked by hook`，英文单句含 `$walkout:done`、`$walkout:continue [reason]` 与 `!walkout-ctl emergency-continue -reason "reason"` 兜底 | 通过 |
| XA3 | `$walkout:continue 生产事故热修复` | 同一提交不被阻断；skill 只回一句 | 通过 |
| XA4 | `!walkout-ctl status` | `state: walkout`、lease 激活、reason 原样（含中文）、债务不变 | 通过（886 s） |
| XA5 | `Reply with exactly OK and do not use tools.` | lease 内放行 | 通过 |
| XA6 | `!walkout-ctl emergency-continue -reason "fallback probe"` | 兜底续期，剩余 900 s | 通过 |
| XA7 | `!walkout-ctl status` | reason 更新为 `fallback probe` | 通过 |

## 5. Codex — 提醒注入与活动确认（预置 `overtime`）

前置：退出 Codex，终端 A Ctrl+C，`reset.ps1 -State overtime`，`start-daemon.ps1`，终端 B 重新 `start-codex.ps1`。

| ID | 输入 | 预期 | 实测 |
| --- | --- | --- | --- |
| XB1 | `What is 2+2? Answer briefly.` | 不被阻断；末尾追加模型自拟提醒，提到 `$walkout:done`，无术语 | 通过 |
| XB2 | `What is 3+3? Answer briefly.` | 再次提醒，更坚决 | 通过（GPT 模型差异较温和："Before the next calculation…"） |
| XB3 | `$walkout:done` | 一句确认 | 通过 |
| XB4 | `!walkout-ctl status` | `state: working`、`activity_count: 1` | 通过 |
| XB5 | （重新预置 overtime 后）`What is 2+2? Answer briefly.` | 出现提醒 | 通过 |
| XB6 | `我刚才已经起来活动了五分钟` | 模型以**脱沙箱**权限执行 `walkout-ctl done`，一句确认；auto 模式无批准提示 | 首轮失败：沙箱内执行报 `open \\.\pipe\walkout-<SID>: Access is denied`（daemon 状态未受影响）；注入指令改为要求脱沙箱后复验通过 |
| XB7 | `!walkout-ctl status` | `state: working` | 通过 |

## 6. Codex — 沙箱确认实验（XB6 失败时的定位步骤）

| ID | 输入 | 预期 | 2026-09-28 实测 |
| --- | --- | --- | --- |
| XS1 | `Run walkout-ctl status with your shell tool and print the output verbatim.` | 若沙箱阻断管道：只读 `status` 同样 `Access is denied`，证实是沙箱而非 `done` 本身 | `Access is denied`，沙箱原因证实 |
| XS2 | `Run walkout-ctl status with your shell tool, requesting escalated permissions to run outside the sandbox because it must open a local named pipe. Print the output verbatim.` | 脱沙箱后成功打印状态 | 成功；auto 模式无批准提示 |

结论：Codex 模型 shell 默认在沙箱内，无法打开按交互用户 ACL 的命名管道；所有需要模型执行 `walkout-ctl` 的指令必须要求脱沙箱。on-request 审批策略下每次会弹一次批准；`never` 策略下会失败，模型应回退为一句话请用户提交 `$walkout:done`。

## 7. Codex — 与 Claude Code 对等的入口（预置 `walkout`）

前置：`prepare.ps1`（重建并预置 walkout）→ `codex plugin remove walkout@walkout-probe` → `codex plugin add walkout@walkout-probe` → `start-daemon.ps1` → `start-codex.ps1`。

| ID | 输入 | 预期 | 2026-09-28 实测 |
| --- | --- | --- | --- |
| XP1 | `$` | 三个 skill 可发现 | 通过 |
| XP2 | `$walkout:continue parity-check` | 放行 | 通过 |
| XP3 | `$walkout:status` | 模型脱沙箱执行 `walkout-ctl status`，代码块原样打印：`walkout`、lease 激活、reason `parity-check` | 通过（带一句前缀，与 Claude 一致） |
| XP4 | `$walkout:done` | 一句确认 | 通过 |
| XP5 | `$walkout:status` | `working`、`emergency_continue_active: false`、**`emergency_continue_reason` 为空** | 通过 |

## 8. 两宿主参与时间并集（门槛 2/3/4）

前置：一个 daemon 运行；Claude 与 Codex 两个 TUI 同时打开、均已安装插件；状态 `working`。

| ID | 步骤 | 预期 | 2026-09-28 实测 |
| --- | --- | --- | --- |
| U1 | Claude `/walkout:status` 记下 `continuous_work_seconds` | 基线 | 0 s（15:29） |
| U2 | Claude 发一条普通提示词；Codex 发一条；Codex `!walkout-ctl status` | 数值增长 | 79 s（15:30） |
| U3 | 等约一分钟，两宿主各再发一条；两边分别查 status | 两宿主看到**同一个**数值，`state_revision` 不变，切换宿主不重置 | 两边均 350 s（15:34–15:35） |

## 9. Lease 生命周期（预置 `walkout`）

| ID | 输入 | 预期 | 实测 |
| --- | --- | --- | --- |
| L1 | `/walkout:continue test`（或 `$walkout:continue test`） | lease 激活 900 s，reason `test` | 通过 |
| L2 | 普通提示词 | lease 内放行 | 通过 |
| L3 | `/walkout:done` | 一句确认 | 通过 |
| L4 | `/walkout:status` | `working`、`emergency_continue_active: false`、`emergency_continue_reason` 为空 | 首轮 lease 仍激活（已修：`confirm_activity` 结束 lease）；reason 仍显示 `test`（已修：仅 lease 激活时展示）；09-28 复验通过 |

## 10. 清理

按两份探针 README 的「完整清理」与「彻底清理清单」执行 `cleanup.ps1 -RemoveArtifacts`，并做零残留检查：插件缓存目录、Codex `config.toml` 信任孤儿段、`~/.claude.json` 探针路径、无 `walkoutd` 进程、仓库 `.tmp/` 下无探针目录。
