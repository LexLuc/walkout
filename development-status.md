# 当前开发状态

本文是每次开始开发前必须阅读的当前进度入口，只记录从 Git、测试和 CI 中无法直接读出的开发意图。完成一条切片后，以新的当前状态覆盖旧内容，不追加开发流水账。

## 产品方向裁定（Master 2026-08-11，高优先）

**本工具的主要用户入口一定是 AI Agent 宿主程序内的主动控制入口，不是独立 CLI。** `walkout-ctl` 从"交付面"降为**底层机制 / 脚本兜底**；宿主入口按真实扩展原语允许差异：Claude Code 使用 `/walkout:continue [原因]`，Codex 使用 `$walkout:continue [原因]`。对话内逃生口因此是 **V1 的主交互交付**，不是补强；`-reason` 机制是两宿主入口的共同地基。

**2026-08-12 Codex 宿主特例裁定**：Master 接受 Codex 使用 `$walkout:continue [原因]` 作为主入口、`!walkout-ctl emergency-continue -reason "原因"` 作为兜底；Claude Code 继续使用 `/walkout:continue [原因]`。同时接受 `$` 控制提交会形成正常模型 turn、`!` 是全权限 shell escape 这两项残余风险。

## 开发路径计划（V1 大致路线，非流水账）

标记：✅ 已闭合 ／ ▶ 当前 ／ ⏭ 计划 ／ ⏳ 待人工/外部条件。顺序可因实测调整，每完成一条把 ▶ 前移。已闭合条目只留一句结论；证据由 Git、测试、`agent-lifecycle-hooks-explained.md` 与下方契约段落承载。

1. ✅ 协议 + 状态机（2026-08-26 起为三态 `working → overtime → walkout`）+ 引擎/tracker/policy 核心 + SQLite 持久化 + Windows 命名管道 daemon。
2. ✅ 两宿主 translator + renderer，均以真实脱敏 fixture 固定（Codex `0.147.0` 基础 hook + `0.146.0` 控制提交 / Claude Code `2.1.224`）。
3. ✅ hook CLI（`walkout-hook`）：stdin → translator → daemon → renderer，全链路静默 fail-open。
4. ✅ 控制 CLI（`walkout-ctl`）：三态简化后为 `status` / `done` / `emergency-continue`，真实二进制端到端闭合完整健康周期。CLI 是底层机制 / 兜底，不是主入口（见产品方向裁定）。
5. ✅ 状态可观测：`walkout-ctl status` + `HealthStatus` 显式暴露紧急 lease（`emergency_continue_active` + 剩余秒）。
6. ✅ 紧急继续原因记录：`emergency_continue` 可选原因（≤256 rune、可空）全链路暴露并跨持久化逐字保留（详见「已固定的适配器契约」）。
7. ✅ **对话内逃生口（V1 主交互交付）**：Claude Code Path A（内联 `` !`bash` `` 在预处理阶段先授 lease、同一 `UserPromptSubmit` 复查后自授权放行）与 Codex `$walkout:continue` plugin skill + hook 固定标记自授权、`!walkout-ctl ...` 兜底，均经真机验证闭合；两宿主阻止文案（英文单句）指向各自入口。Claude Code 控制面已打包（见第 8 步）。Codex 侧调查结论/机制矩阵见 `agent-lifecycle-hooks-explained.md §9.2.2`，探针复现见 `manual-probes/codex-emergency-continue/` 与 `internal/adapters/codex/testdata/v0.146.0/manifest.json`，候选 A/B/C/D 取舍与根因见 Git `a316abe`。可选残余：Claude Code 控制提交的模型 turn 约束复测。
8. ✅ 安装器 / 插件打包：Claude Code 与 Codex 两侧均已形成可构建、可安装的独立插件；各自 lifecycle hook、对话内控制入口、版本回落、默认 SID 管道、首次信任与升级重授权均经真实宿主安装探针闭合。
9. ✅ 命名最终化：品牌词 **Walkout**、伞品牌 `lexicon`（Master 2026-08-17 裁定），一次性替换完成并经两宿主真机安装复验闭合（Master 2026-08-25，见下方结论）；仓库目录与远端均已为 `walkout`（2026-09-28 随开源发布完成）。
10. ▶ **发布前冲刺**：两宿主发布门槛已全部真机闭合，公开仓库已建立（2026-09-28）；余下本地改名、探针零残留终验与 GitHub 形式安装验证（见下方「下一动作」）。
11. ⏭ 实验/遥测最小事件模型：`experiment-plan.md` 的三组 A/B（提醒人格、升级后果、验证方式）所需最小事件；不阻塞首发，随首批真实使用数据需求启动。
12. ⏳ 摄像头验证流程（**Master 2026-08-25 裁定：移出开发主线，降为可选优化 / V1.x**）：present/absent/uncertain 本地验证仅在两宿主发布且稳定后按 `v1-scope.md` §3 的 bounded spike 评估；无摄像头路径是且始终是正式通路。
13. ⏳ WorkBuddy 适配器（**Master 2026-08-25 裁定：Claude Code 与 Codex 发布并稳定使用后再考虑的增强项**）：待其可安装/可 probe，隔离为独立 Beta。
14. ✅ 残留人工验收：Claude Code 被阻止后 ↑ 原样回填（`2.1.281` 并回显 `Original prompt`）与两宿主参与时间并集均于 2026-09 真机闭合。

## 已固定的端到端结论（两宿主，真机实测）

> **2026-08-17 更名说明**：品牌词由工作名 deskhealth / Desk Health Agent 更名为 **Walkout**（伞品牌 `lexicon`）。本文历史裁定与证据中的插件、命令、二进制标识已按新名规范化，实测事实本身未改动；个别 verbatim 宿主输出（如 `Plugin - deskhealth@…`）与历史探针名（`deskhealth-probe`）保留当时原样。更名前的真机证据对新名安装的效力以第 9 步复验为准。
>
> **2026-08-26 三态简化说明**：状态机收敛为 `working → overtime → walkout`，控制命令收敛为 `confirm_activity` + `emergency_continue`。下文历史结论中的 `service_paused` 读作 `walkout`、`gentle_nudge`/`sharp_nudge`/`debt` 读作 `overtime` 内的提醒/债务阶段、`start-break`+`confirm-recovery` 两步读作单步 `done`、`recovery_count` 读作 `activity_count`；这些结论记录的行为事实（阻断、lease、fail-open、信任等）不受状态命名影响，但旧命令与 verifying/recovery 两态相关的链路已被单步确认取代，对新代码的效力以三态真机验收轮为准。

- **Claude Code `2.1.224`**：`claude -p` → hooks → `walkout-hook.exe`（默认 SID 管道）→ 真实 `walkoutd`（临时 SQLite）全链路走通。`allow` 四类 hook 全部 exit 0 零输出、会话产出与无 hook 一致；一致预置的 `service_paused` 下 `UserPromptSubmit` 被阻止，宿主显示 debt_limit 确定性文案、`num_turns=0`、零模型调用，阻止后状态不漂移。
- **Codex `0.147.0`**：项目级 `.codex/hooks.json` 指向 `walkout-hook.exe -provider codex`。交互 TUI 会话经真人授权后 hook 真实执行，daemon 状态推进（`engagement_active=true`）；授权后 headless `codex exec` 也认信任并触发阻止，控制台显示 `hook: UserPromptSubmit Blocked`、模型未产出、`service_paused` 状态不漂移。
- **Codex `0.146.0` 可分发插件（Master 2026-08-16 真机）**：Codex 支持从 plugin manifest 的 `hooks` 字段加载 plugin-bundled `.codex/hooks.json`，无需写全局或项目级 hook；`codex plugin marketplace add <repo-root>` + `codex plugin add walkout@lexicon` 是本版本的安装等价路径（命令名是 `add`，无 `plugin install` 子命令）。安装缓存中的三个 hook 均显示来源 `Plugin - deskhealth@…`，`${PLUGIN_ROOT}` 被替换为缓存目录；本机 hook shell 为 PowerShell，故命令通过 `pwsh -File` wrapper 启动 exe。真人逐条审核 `SessionStart` / `SessionEnd` / `UserPromptSubmit` 后，`$` 可发现 plugin 与 `Emergency Continue` skill，暂停态普通 prompt 被打包 hook 拦且状态不漂移，控制提交同次放行、15 分钟 lease 激活、reason 原样且债务保留，lease 内普通 prompt 放行，`SessionEnd` 未超过 3 秒。
- **Codex 版本字段与回落（同机最终事件直接观测）**：仅给真机探针子进程启用脱敏事件元数据输出后，npm launcher 触发且被真实 daemon 接受的 `session_start` / `prompt_submit` / `session_end` 三条 `HealthEvent` 均为 `host_version: "0.146.0"`；同一安装缓存改由 direct native binary 启动并显式移除 `CODEX_MANAGED_PACKAGE_ROOT` 后，三条最终事件均为 `host_version: "unknown"`。记录只含 schema/provider/version/event type；两条路径的暂停守卫都生效。
- **插件 hook 两处健壮性修复经真机复验（Master 2026-08-16）**：版本源改为传环境变量**名** `-HostPackageEnv CODEX_MANAGED_PACKAGE_ROOT` 由恒为 PowerShell 的 `run-hook.ps1` 自解析（消除"哪个 shell 展开 `command`/`commandWindows`"的歧义，两串现完全一致），wrapper 的 async 读取移到 stdin 写入前（消除输出填满管道即死锁）。经 `codex plugin marketplace add` + `codex plugin add` 真机安装：改写后的 `.codex/hooks.json` 被 Codex loader **正常接受**（3 hook 全注册、`trusted_hash` 齐全、`user_prompt_submit enabled`），暂停态 `hello` 被拦、`$walkout:con` 部分 token **不误配**、`$walkout:continue reverify` 同次自授权放行（skill 只回一句）。版本源合成证据：真机 `CODEX_MANAGED_PACKAGE_ROOT` 指向 `@openai/codex/package.json` version=`0.146.0`，非交互复验（真二进制 + 真 daemon）已证 wrapper 经 env 名 → package.json → version 链路（探针取到注入的 `9.9.9-probe`，env 缺失回落 `unknown`，两路守卫都生效）。契约测试新增 `command==commandWindows`、禁 `.../package.json` shell 展开、reader-先于-write 位置断言三条防回归。
- **Codex hook 信任是每事件 + 命令 sha256 持久化**（`~/.codex/config.toml` 的 `[hooks.state.'…:user_prompt_submit:0:0'] trusted_hash`）：首次执行时交互授权，之后 headless 复用；**改动 hook 命令行会使该事件信任失效**，安装器与升级流程必须据此重新引导授权。
- **Codex `0.146.0` 的 Hook 设置只能禁用，不能撤销信任**：`/hooks` 可把当前仍可加载的非托管 hook 写为 `hooks.state.<id>.enabled = false`，使其停止运行，但不会删除 `trusted_hash` 或把它恢复为待审核；已卸载 plugin 留下的孤立 `hooks.state` 记录无可加载目标，因而不会出现在 `/hooks` 中供禁用。若要让该 hook 下次安装时重新进入审核，须手动删除 `~/.codex/config.toml` 中对应的精确 `hooks.state` 段；托管 hook 不允许从用户 hook 浏览器禁用。本次 `deskhealth-probe` 卸载后遗留的三个 `SessionStart` / `SessionEnd` / `UserPromptSubmit` 信任段已手动清除。
- **两宿主卸载均不清插件缓存（Master 2026-08-25 真机）**：`claude plugin uninstall`/`codex plugin remove` 加各自的 `marketplace remove` 之后，`~/.claude/plugins/cache/<marketplace>/` 与 `~/.codex/plugins/cache/<marketplace>/` 仍原样留存，须手动删除；连同 Codex 信任孤儿段的安全清理手法与零残留验收清单，已固化在两个探针 README 的「彻底清理清单」章节。
- **宿主渲染差异**：Claude Code 把确定性 reason 文案直接显示在宿主输出；Codex headless 控制台只显示 `UserPromptSubmit Blocked`，不回显 reason 文本（reason 仍送达并触发阻止）。这印证了 §9.2.1「控制台 trace 与 recorder 产物不同」的既有结论。
- 默认 75 ms IPC deadline 在真实事务写入下够用；hook 进程整体墙钟约 100–140 ms（含进程启动），对宿主不可感知。
- 引擎 `reconcile()` 按 tracker 事实重推状态：注入状态必须与事实自洽（债务需 ≥ 120 分钟上限才维持 `service_paused`，连续工作 170 分钟 → 债务 125 分钟）。
- 冒烟自动化注意：本机 agent 工具层会折叠命令文本中的 `\\`，命名管道参数避免字面量传递，优先默认 SID 管道或文件承载配置；`codex exec` 启动期偶发 `codex_models_manager` 模型刷新超时会拖长墙钟，与 hook 无关，父进程需设 deadline。
- **交互界面被阻止后的行为（Codex TUI，Master 2026-08-09 亲手确认）**：① reason 文案完整呈现（`UserPromptSubmit hook (blocked) feedback: <文案>`），非通用 "Blocked"；② 输入框在提交后清空；③ 但提示词进入 `~/.codex/history.jsonl`，按 ↑ 原样调回并可重发——无损重试可行但非自动。附带验证：daemon 停止后重发被静默 fail-open 放行、agent 正常运行，证明"守卫故障即恢复用户工作能力"在真实交互界面成立（不仅 headless）。
- **斜杠逃生口 Path A 成立（Claude Code，Master 2026-08-11 真机）**：已注册斜杠命令**会**走 `UserPromptSubmit`，但内联 `` !`bash` `` 在**预处理阶段先执行**。暂停态下：`/dh-continue <原因>`（授予 lease）**未被阻**——同一条提交的 `UserPromptSubmit` 复查时 lease 已激活 → 自授权放行；只读 `/dh-status`（不授予）**被阻**，两者差异正是时序证据；随后普通提示词也放行。故逃生口零新增 hook 代码即可用（斜杠→内联 CLI→lease→自授权）。副作用：放行后内联 CLI 输出注入模型，无约束时模型会自作主张读文件——生产命令体须加"一句话告知、勿做他事"约束。
- **Walkout 新名两宿主真机复验通过（Master 2026-08-25）**：Codex——`codex plugin marketplace add <repo-root>` 识别 `lexicon`，`codex plugin add walkout@lexicon` 安装 0.1.0；三个 lifecycle 事件因命令 hash 变化按预期重新逐条授权；暂停态 `hello` 被拦（新文案指向 `$walkout:continue` 与 `!walkout-ctl` 兜底）、`$walkout:con` 部分 token 不误配、`$walkout:continue <reason>` 同次自授权放行、lease 内普通 prompt 放行，卸载与 marketplace 移除干净。Claude Code——`claude plugin install walkout@lexicon` 后 `bin/walkout-ctl.exe`、`bin/walkout-hook.exe` 随安装完整落地到 `~/.claude/plugins/cache/lexicon/walkout/0.1.0/`（**未**被 gitignore 过滤，打包 gotcha 排除）；`/walkout` 可发现两命令、暂停态 `hello` 被拦、`/walkout:continue <reason>` 同次自授权放行（lease 900s、reason 原样）、lease 内 `/walkout:status` 放行并原样回显 reason；本机 `$CLAUDE_CODE_VERSION` 命令串展开与 env **均为空**，host_version 记 `"unknown"`，守卫行为不受影响（与既有"Claude 版本遥测暂记 unknown"结论一致）。
- **Codex `$` 主入口与 `!` 兜底成立（Codex CLI `0.146.0`，Master 2026-08-12 真机）**：完整实测记录见 `agent-lifecycle-hooks-explained.md §9.2.2`；结论——`$walkout:continue <原因>` 同提交自授权放行、skill 只确认一句、状态 lease active 且原样 reason、债务继续累计；`!` 兜底绕过 prompt hook 直接续期；隐私 recorder 只保留固定控制提交。

## 已固定的适配器契约

- Codex `0.147.0` 基础 hook、`0.146.0` 控制提交与 Claude Code `2.1.224` 的输入差异和实现影响详见 `agent-lifecycle-hooks-explained.md` §9.2.1 / §9.2.2 / §9.3.1；两 translator 不共享宿主 payload struct。
- 两宿主 renderer：`pause_prompt` 仅在 `prompt_submit` + `block_prompt` capability 时输出阻止 JSON；`inject_reminder` 仅在 `prompt_submit` + `inject_context` capability 时输出 `additionalContext` 模型指令（按 `break_due` / `debt_growing` 两档递进，要求模型自行措辞、指向 done 入口、自然语言已活动时执行捆绑 `walkout-ctl done`）；缺 capability 场景降级为空输出；阻断 reason 文案按 `reason_code` 确定性生成、**英文**单句（Master 2026-08-11 定：提示优先英文，i18n 延后）、只针对行为、始终同时指向活动确认与逃生入口——Claude Code → `/walkout:done` 与 `/walkout:continue [reason]`，Codex → `$walkout:done` 与 `$walkout:continue [reason]`，并附 `!walkout-ctl emergency-continue -reason "reason"` shell fallback（两 renderer 各自持独立 copy map，未共享）。
- hook CLI（`internal/hookcli` + `cmd/walkout-hook`）：`-provider`/`-host-version`/`-pipe`/`-timeout`；一切失败静默 fail-open；`assessment_status` 恒为 `missing`。`-host-version` **可选**：两 translator 空版本缺省为 `"unknown"`（仅遥测、无行为分支），修掉"缺版本 → `NewTranslator` 报错 → 静默 fail-open 关掉守卫"；打包插件的准确版本源见 `agent-lifecycle-hooks-explained.md §9.5` 与第 8 步①。
- hook CLI 的真机探针观察口：生产 plugin 不设置 `WALKOUT_EVENT_PROBE_OUTPUT`；仅版本化 probe 给其 Codex 子进程临时设置。daemon 成功接受事件后，hook 追加最终 translated event 的 `schema_version` / `provider` / `host_version` / `event_type` 四字段 JSONL，不记录 prompt、session、cwd、reason 或 payload；观察写入失败保持静默且不改变守卫决定。
- Codex 对话内控制标记：仅 Codex `UserPromptSubmit.prompt` 首字符开始、区分大小写、完整 token 边界的 `$walkout:continue` 匹配；只接受单行可选 reason，先复用 `execute_command/emergency_continue`，再处理同一 `prompt_submit`。不匹配时返回零控制请求，普通 prompt 不进入 `HealthEvent.Payload`、协议或 core；控制命令失败仍继续普通事件决策，避免非法 reason 意外放行。
- 控制 CLI（`internal/ctlcli` + `cmd/walkout-ctl`）：`status`（只读，走 `status` RPC）/ `done` / `emergency-continue`（走 `execute_command` RPC）子命令，`emergency-continue` 支持 `-reason "..."`；成功打印状态并 exit 0，失败给用户可见错误并 exit 非 0（控制面不套用静默 fail-open）；`done` 从任一状态使连续工作与债务归零、`activity_count` 增一并持久化、状态回到 `working`（改版前两步 `start-break`/`confirm-recovery` 的真实二进制端到端证据按文首三态说明读取）。
- 插件按宿主**拆成两个独立目录**（Claude Code `plugins/walkout/`、Codex `plugins/walkout-codex/`）：Claude Code 按约定自动扫描插件根的 `commands/`·`skills/`·`hooks/`，共享目录会把 Codex 的 `skills/continue/` 漏加载成 Claude skill（真机 `claude plugin details` 实见"重复 continue"），故拆分隔离。
  - Claude 插件 `plugins/walkout/`：根 `.claude-plugin/marketplace.json`（marketplace `lexicon`）列出 `walkout → ./plugins/walkout`；`commands/continue.md`（`/walkout:continue [reason]`）与 `commands/status.md`（`/walkout:status`）均 `disable-model-invocation: true`、转调 `${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe`、`$ARGUMENTS`→`-reason`，`continue` 保留"一句话告知、勿做他事"约束；`hooks/hooks.json` 打包 4 事件守卫。契约由 `internal/adapters/claudecode/plugin_contract_test.go` 固定。
  - Codex 插件 `plugins/walkout-codex/`：`.codex-plugin/plugin.json` 同时声明 `skills: ./skills/` 与 `hooks: ./.codex/hooks.json`；hook 事件集固定为 `SessionStart` / `UserPromptSubmit` / `SessionEnd`，其中 `SessionEnd` timeout 为 3 秒，路径字面量统一正斜杠且不传 `-pipe`（使用默认 SID 管道）。Windows 命令经 `hooks/run-hook.ps1` 启动 `bin/walkout-hook.exe -provider codex`；wrapper 从 `$CODEX_MANAGED_PACKAGE_ROOT/package.json` 读取版本，读取失败传空值触发 translator 的 `"unknown"` 回落，并维持静默 fail-open。`build.ps1` 构建 Windows x64 hook binary；仓库 `.agents/plugins/marketplace.json`（marketplace `lexicon`）固定 `walkout → ./plugins/walkout-codex`、`AVAILABLE` / `ON_INSTALL` / `Productivity`。首次安装须按三个事件逐条审核，升级后命令 hash 变化的事件须重新授权；契约由 `internal/adapters/codex/plugin_contract_test.go` 固定。
- `HealthStatus` 显式暴露紧急 lease：`emergency_continue_active` + `emergency_continue_remaining_seconds` + `emergency_continue_reason`（引擎快照按 clock 计算剩余，均为纯附加字段向后兼容）；真实二进制验证授予前 `false/0/空`、授予后 `true/≈899/原样中文原因`、状态与债务不变。
- 紧急继续（`emergency_continue` 命令 + 引擎 lease）：授予后引擎持有绝对到期时间 `emergencyLeaseUntil = now + EmergencyExtension`（15 分钟）与"最近一次原因"`emergencyLeaseReason`；命令携带可选 `reason`（`reason,omitempty`，≤256 rune，可空以免拖慢逃生口；非 `emergency_continue` 命令带 reason 会被 `invalid_payload` 拒绝），原因随 `EngineState.emergency_lease_reason` 跨重启**逐字保留不 rebase**、只留最近一次。lease 有效期内 `service_paused` 的 `prompt_submit` 决定为 `allow` / `emergency_continue`，到期回到 `pause_prompt`；lease **不赦免累计债务**，且以绝对时间跨 daemon 重启判定有效/过期（restore 不 rebase）。真实二进制已端到端验证"暂停中阻止 → 授予 lease（债务不变）→ 有效期内放行"，及"带原因授予 → 原因经持久化边界仍可读"。
- 共享 runner 与 Windows IPC 契约不变：统一静默 fail-open，默认 75 ms 端到端 deadline。

## 未决问题

- Codex 版本边界：`0.146.0` 的源码调查、真实 TUI 主入口/兜底与控制 fixture 均已闭合；前序基础 hook fixture 为 `0.147.0` headless。若正式发布版本不同，仍须原样复跑版本化 probe，不能把本次行为外推为永久宿主契约。
- 两宿主的上下文注入、Stop continuation、工具事件字段与时序，以及 Claude Code 实际 hook timeout 下限。
- WorkBuddy Desktop 的安装位置、可执行入口和目标版本。
- 是否接受乱序 `occurred_at`；`skip`、累计债务跨日期边界的最终策略。
- reason 文案的多语言与 A/B persona 如何与确定性 renderer 输出共存；**英文单句为 V1 基线（Master 2026-08-11 定：提示优先英文，i18n 延后）**。产品文档（README/product-spec/agent-prompt 等）仍是中文，其中引用的示例文案与英文运行时文案的一致性待发布前统一。

## 改变方向的条件

如果向 `HealthStatus` 增加 lease 字段会破坏既有 `status` RPC 的向后兼容（旧调用方或旧持久化），保持纯附加字段并补协议兼容测试，不改动既有字段语义。

如果状态展示需要暴露到期时间，优先给出"剩余秒数"而非绝对时间戳，避免时区与时钟同步歧义影响用户判断。

## 已固定的命名裁定（第 9 步闭合，2026-08-25）

- 品牌词 **Walkout**（Master 2026-08-17）：一词双关——Agent 罢工（walkout）与用户起身走出去（walk out）恰好都是产品机制本身；无 desk / health 品类绑定。伞品牌 marketplace **`lexicon`**（Lex 的项目辞典，容纳后续各类项目，不限休息/健康类）。Tagline 候选：*"Your agent walks out until you do."* ／「你不动，它罢工」。
- 最终映射：Go module `github.com/LexLuc/walkout`（2026-09-28 随开源发布切换）；`walkoutd.exe` / `walkout-hook.exe` / `walkout-ctl.exe`；管道 `\\.\pipe\walkout-<SID>`；数据目录 `%LocalAppData%\Walkout\walkout.db`；观察口 env `WALKOUT_EVENT_PROBE_OUTPUT`；Claude Code `plugins/walkout/` + `/walkout:done`·`/walkout:continue`·`/walkout:status`；Codex `plugins/walkout-codex/` + `walkout@lexicon` + `$walkout:done`·`$walkout:continue`；兜底 `!walkout-ctl emergency-continue -reason "reason"`。协议字段与状态机名称未随品牌改名（状态机本身于 2026-08-26 收敛为三态）。
- 迁移策略：发布前直接切换，无旧名兼容层。仓库目录/远端改名为 `walkout` 属外部动作，随发布执行。

## 发布范围裁定（Master 2026-08-25）

- **V1 发布基线为 Codex + Claude Code 两宿主。**
- 摄像头验证移出开发主线，降为可选优化（V1.x bounded spike，见 `v1-scope.md` §3）；无摄像头确认路径就是正式通路，不是替代品。
- WorkBuddy 适配器是 Claude Code 与 Codex 发布并稳定使用后再考虑的增强项。
- 当前阶段：**发布前冲刺**。

## 下一动作 —— ▶ 第 10 步 发布前冲刺

**门槛对照盘点已完成（2026-08-25，代码证据核实）**，确认缺口如下：

1. **提醒注入链路未交付（门槛 1「提醒」、5）**：policy 会产出 `inject_reminder`/`require_assessment`，但两宿主 renderer 对这两个 action 均**刻意静默放行**（注释明示"context injection 在目标宿主未经验证探针证实"）。后果：到达休息点后用户看不到任何提醒，直到债务触顶才见到暂停——V1 闭环的前半段（温和提醒→升级→延后）当前不可见。
2. **延后（snooze）无用户入口（门槛 1「延后」）**：`engine.Snooze()` 已实现且有单测，但协议命令集只有 `start_break`/`confirm_recovery`/`emergency_continue`，`walkout-ctl` 把 `snooze` 判为未知子命令——引擎能力从协议层起就不可达。
3. **恢复流程缺宿主主入口（门槛 1「恢复」、13）**：两宿主插件的对话内命令只有 continue/status；`start-break`/`confirm-recovery` 仅剩 `walkout-ctl` 兜底——与「宿主内主动控制入口是主交付」的产品方向裁定（2026-08-11）不符。
4. **`health.assess` 未接入宿主（门槛 6）**：RPC 与 policy 均支持 `assessment_status`，但 hook 恒发 `missing`，故 break-due 时恒走 `require_assessment` 再被 renderer 静默——评估回路端到端未交付。
5. **多 session 并集仅引擎级验证（门槛 2/3/4）**：tracker 有跨 session 并集单测；两宿主并行真机实测未做，归入冲刺人工验收。
6. **升级验证不完整（门槛 11）**：Codex 侧命令 hash 变化重授权已真机验证；Claude Code 侧版本升级（0.1.0 → 新版）流程未实测。

其余门槛（7 暂停不破坏当前 turn、8 逃生口、9 隐私默认、10 fail-open、12 摄像头不阻塞）已有真机或设计级证据闭合。

盘点结论：**V1 闭环的后半段（暂停→逃生→恢复机制）已交付且真机闭合；前半段（提醒→升级→延后）对用户完全不可见**。Master 2026-08-25 裁定**全量补齐缺口 1–4**。

**三态产品简化已落地（Master 2026-08-26 裁定，`2238cb6` 文档 + `72e6540` 代码）**：

- 状态机收敛为 `working → overtime → walkout`（三态均用户可见，劳动叙事一条线）；显式 snooze 与两步 break/done 取消——不理会提醒即自然入债，用户反馈"已活动"（`confirm_activity`）单步清零回 working。
- 控制面收敛为 `walkout-ctl <status|done|emergency-continue>`；宿主入口收敛为 `/walkout:done`·`/walkout:continue`·`/walkout:status`（Claude）与 `$walkout:done`·`$walkout:continue`（Codex 标记 + skill）。
- 提醒触发**不依赖评估**（Master 裁定）：overtime 每条 prompt 放行并注入 `inject_reminder`；注入内容是给模型的指令——用当前工作语境自行措辞提醒（解决"文案机械呆板"）、指向 `/walkout:done`，并在用户以自然语言表示已活动时执行捆绑 `walkout-ctl done`（自然语言反馈=非阻断期增强；walkout 阻断期仅命令，因提示词到不了模型）。语气按债务深度两档递进（`break_due`/`debt_growing`）。
- `health.assess` 降为可选增强（不再有 `require_assessment` 决策路径）；hookcli 将 ctl 绝对路径（hook 同目录）注入 renderer。

冲刺剩余项：

- **真机验收轮（2026-09-25 → 09-28，Claude Code `2.1.281` / Codex `0.156.1`）已闭合的结论**：两宿主 `additionalContext` 注入均真实进入模型上下文（overtime 每条 prompt 放行并追加模型自拟提醒）；`/walkout:done`·`$walkout:done` 可发现、单步清零回 `working`；walkout 阻断文案同时给出 done 与 continue 入口；`$walkout:continue <中文 reason>` 与 `!walkout-ctl` 兜底在 0.156.1（比 fixture 新十个小版本）正常；Claude 被阻后 ↑ 原样回填且 `2.1.281` 回显 `Original prompt`；Claude 插件升级 `0.1.0 → 0.1.1`（`marketplace update` + `plugin update`）通过，升级须同步 `plugins/walkout/.claude-plugin/plugin.json` 与 `.claude-plugin/marketplace.json` 两处版本（待加契约测试断言一致）。自然语言活动确认：Claude 成立（auto mode 由分类器放行、无弹窗；默认权限模式应预期一次性授权）；Codex 模型 shell 默认在沙箱内、打开 daemon 命名管道 `Access is denied`（只读 `status` 同样失败，沙箱原因已证实），脱沙箱申请在 auto 模式静默通过。 验收后落地并复验通过的改动：注入、done/continue/status 三入口、阻断文案、↑ 回填、插件升级（静默、不重新信任）、两宿主并集、自然语言确认（Claude 直接执行；Codex 须脱沙箱执行，auto 模式静默）、去术语化提醒与第二档语气、status 原样输出、`done` 结束 lease、lease 未激活时不显示 reason、Codex `$walkout:status` skill——全部真机通过。插件清单与 marketplace 目录版本一致性已由契约测试固定。
- 探针操作纪律已写入两份探针 README「重跑」段：卸载插件不影响已启动的宿主进程（须重启工作会话）；本机所有启用 Walkout 的进程共享默认管道，预置后先核对 `walkout-ctl status` 再进 TUI。
- **开源发布已执行（2026-09-28，Master 裁定：MIT、个人账号、直接公开、压缩历史）**：Go module `github.com/LexLuc/walkout`；MIT（Copyright 2026 Lex）；双语 README（英文在前、中文在后，tagline *Your agent walks out until you do.* / 「你不动，它罢工」，含 AI 辅助声明与产品命题）；公开仓库 https://github.com/LexLuc/walkout，`main` 为单提交初始发布，完整历史仅保留于本地 `backup/full-history`。探针环境已按 `uninstall-and-cleanup.md` 完成零残留终验（2026-09-28，指南步骤与实际残留完全吻合）。本地目录已改名为 `walkout`（2026-09-28）。GitHub marketplace 形式安装已实测（2026-09-28）：两宿主 `marketplace add LexLuc/walkout` 与插件安装均成功，但缓存中无插件二进制（Claude `bin/` 只有 `.gitkeep`，Codex 无 `bin/`），守卫静默失效；README 已改为明确劝阻该方式、仅推荐本地检出安装。

**下一动作 —— V1.1 首项：让 GitHub 形式安装可用（待 Master 选定方案）**。候选：① CI 在打 tag 时构建两宿主插件二进制并推送到专用发布分支，marketplace 指向该分支（二进制不进 `main`；需先验证两宿主 marketplace 是否支持指定 git ref）；② 直接把预构建 `.exe` 提交到 `plugins/*/bin/`（最简单，但仓库携带二进制、每版增约 8 MB，公开仓库内可执行文件需可复现构建说明）；③ hook wrapper 首次运行时从 GitHub Release 下载二进制（违背"无网络"隐私承诺，不建议）。

### 已知重定向条件

- 若真机验收发现 additionalContext 注入在目标宿主版本不生效，提醒链路降级方案与 Master 复核（候选：Stop 事件注入、宿主通知、或 V1.x 跟进），不阻塞暂停/逃生主链发布。
- 若自然语言反馈的模型遵从度或权限弹窗体验不可接受，将其从注入指令中移除，保留命令通路（产品语义不变）。
