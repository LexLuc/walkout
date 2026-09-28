# Claude Code 插件安装真机探针

本探针验证 **打包形态** 的 `walkout` 插件在真实 Claude Code 中的完整链路：从本地 marketplace 安装后，① 斜杠命令 `/walkout:done` `/walkout:continue` `/walkout:status` 可发现；② 插件**打包的** `hooks/hooks.json`（`${CLAUDE_PLUGIN_ROOT}/bin/walkout-hook.exe -host-version "$CLAUDE_CODE_VERSION"`）真实执行，暂停态阻止普通提示词；③ `/walkout:continue` 的内联 `` !`…` `` 在预处理阶段先授 lease、同一提交自授权放行；④ `$CLAUDE_CODE_VERSION` 是否在 hook 命令串里展开。

完整的可复用验收用例（含 2026-09 实测结果与操作纪律）见 [`../acceptance-test-cases.md`](../acceptance-test-cases.md)；本文负责环境准备、脚本与清理。

## 为什么必须用真机安装

单元与契约测试证明清单结构、命令 frontmatter 与 hooks.json 的 wiring；`claude plugin validate` 证明清单合法。都无法证明目标 Claude Code 版本在**安装后**是否：把插件的 `bin/*.exe`（被 `.gitignore` 忽略）一并落地、在插件 `hooks.json` 里替换 `${CLAUDE_PLUGIN_ROOT}`、在命令串里展开 `$CLAUDE_CODE_VERSION`、以及打包形态下自授权时序是否仍成立。这些是版本化宿主能力，必须真机验证而非长期假设。

## 隐私边界

- 本探针不采集工作内容。插件 hook 只把宿主事件转发给本地 `walkoutd`，域核心内容中立。
- 唯一落盘的观测是 `version-echo.txt`，仅含 Claude Code 版本号与"命令串变量是否展开"，无敏感信息。
- 所有运行产物在仓库内 `.tmp/walkout-plugin-probe/`，不提交 Git。

## 准备

以下命令中的 `<repo-root>` 指本仓库检出目录的绝对路径。普通 PowerShell：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/prepare.ps1
```

`prepare.ps1` 构建插件二进制到 `plugins/walkout/bin/`、把 daemon 与 seeder 构建到探针 `bin/`、写入自洽 `walkout` 数据库、并在探针项目 `.claude/settings.json` 放一个旁路 `SessionStart` echo hook。它**不**修改 Claude Code 用户配置。随后按脚本末尾提示显式安装：

```powershell
claude plugin marketplace add "<repo-root>"
claude plugin install walkout@lexicon
claude plugin details walkout@lexicon   # 预期 Skills (3) continue, done, status 与 Hooks (4)
```

> `bin/*.exe` 被 `.gitignore` 忽略但物理存在（`build.ps1` 生成）。Claude Code `2.1.281` 的 `details` 只列组件清单、不再列文件，二进制是否落地改由执行路径证明：TUI 序列中首次 `/walkout:status` 的 Bash 行会打印解析后的 `walkout-ctl.exe` 绝对路径，该路径即真实的 `${CLAUDE_PLUGIN_ROOT}`，确认两个 exe 存在于此。真机实测（2026-09-25）：本地路径 marketplace 在 `2.1.281` 上按引用运行仓库源目录 `plugins/walkout/bin/` 内的二进制（`~/.claude/plugins/cache/lexicon/walkout/<version>/bin/` 同时存有副本）。因此"按 gitignore 过滤漏装"这一打包 gotcha 只对非本地（如 GitHub）分发有意义，该路径**尚未验证**。

## 作用域与副作用

| 动作 | 生命周期与影响范围 |
| --- | --- |
| `prepare.ps1` | 只写仓库内 `.tmp/walkout-plugin-probe/` 与 `plugins/walkout/bin/`，不改 Claude Code 用户配置或系统 PATH |
| `claude plugin marketplace add` | 持久写入 Claude Code 用户配置，指向本仓库路径，直至显式移除 |
| `claude plugin install` | 持久安装 `walkout` 插件（含其打包 hooks），所有 Claude Code 会话生效，直至卸载 |
| `start-daemon.ps1` | 前台存活期间占用当前用户的默认 SID 命名管道；其他启用 Walkout hook 的会话可能连到这份测试状态，探测期间勿并行使用 |
| 探针项目 `.claude/settings.json` | 只在探针项目目录加载的旁路 echo hook；离开该目录不执行 |
| 插件 hook 信任 | 首次运行需在 TUI 授权插件 hooks 与项目 settings hook |

## 两个终端

终端 A 保持 daemon 运行（seeded `walkout`）：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/start-daemon.ps1
```

终端 B 在探针项目内启动真实 Claude Code：

```powershell
cd <repo-root>/.tmp/walkout-plugin-probe/project
claude
```

首次运行按提示信任插件 hooks 与项目 settings hook。

## TUI 内验证序列

1. 输入 `/walkout`，确认列表出现 `walkout:done`、`walkout:continue` 与 `walkout:status`（可发现性）。
2. 输入普通提示词 `hello`；预期被 `walkout` 态阻止，英文单句文案同时指向 `/walkout:done`（确认已活动）与 `/walkout:continue [reason]`（紧急继续）。**这证明打包 hook 端到端执行、`${CLAUDE_PLUGIN_ROOT}` 与 exe 路径成立。** `2.1.281` 还会在阻断下回显 `Original prompt: <文本>`，且 ↑ 可原样调回被阻提示词（2026-09-25 实测）。
3. 输入 `/walkout:continue production-hotfix`；预期**不被阻止**——内联 `!` 在预处理阶段先授 lease，同一 `UserPromptSubmit` 复查自授权放行，模型只回一句确认、不读文件。**这证明命令的 `${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe` 与自授权在打包形态成立。**
4. 输入 `/walkout:status`；此时处于 lease 有效期内，放行并显示 `walkout` + `emergency_continue_active: true` + reason 原样。（注：暂停中、无 lease 时该只读命令本身会被阻——与斜杠逃生口 Path A 时序一致。另：当前构建下从 `walkout` 态执行 `/walkout:done` 回到 `working` 后，lease 仍显示激活直至到期，这是已知且已排期修改的行为——`confirm_activity` 将改为同时结束 lease——不是缺陷。）
5. 读 `version-echo.txt` 判断 `$CLAUDE_CODE_VERSION`：

```powershell
Get-Content <repo-root>/.tmp/walkout-plugin-probe/version-echo.txt
```

- `cmd_string_expansion=` 为真实版本（如 `2.1.228`）→ 展开成立，插件 hook 的 `-host-version` 拿到准确版本。
- `cmd_string_expansion=$CLAUDE_CODE_VERSION`（字面）而 `env_CLAUDE_CODE_VERSION` 有值 → 命令串不展开，但 env 有；应改为让 hook 自读 `$CLAUDE_CODE_VERSION`（`translator` 已把空/无效缺省为 `"unknown"`，功能不受影响）。
- 两者皆空 → 记为 `"unknown"`，guard 仍工作。

## Overtime 提醒注入与活动确认序列（三态新增验收点）

验证 `hookSpecificOutput.additionalContext` 注入是否真的进入模型上下文，以及 `/walkout:done` 与自然语言反馈是否可用。终端 A 按 Ctrl+C 停 daemon，重置为 overtime 态并重启：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/reset.ps1 -State overtime
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/start-daemon.ps1
```

终端 B 在探针项目内重新启动 `claude` 后：

1. 输入 `/walkout`，确认列表出现 `walkout:done`。
2. 输入普通提示词 `What is 2+2? Answer briefly.`；预期**不被阻断**，且模型在回答末尾用自己的话追加一句只针对久坐行为、结合当前任务的起身提醒，并提到 `/walkout:done`。若完全没有提醒，说明 additionalContext 注入在该 Claude Code 版本未生效——记录版本号，这是预置的降级讨论触发条件。
3. 再输入一条普通提示词；预期提醒语气升级（债务累积中，措辞更坚决）。
4. 输入 `/walkout:done`；预期模型只回一句确认。
5. 输入 `/walkout:status`；预期 `state: working`、`activity_count: 1`。
6. （自然语言反馈）停 daemon → `reset.ps1 -State overtime` → 重启 daemon 与 TUI，先发一条普通提示词收到提醒后，输入 `我刚才已经起来活动了五分钟`；预期模型只用 Bash 工具执行插件捆绑的 `walkout-ctl.exe done` 并一句话确认。重点观察 Bash 权限弹窗体验是否可接受。随后 `/walkout:status` 应显示 `state: working`。2026-09-25 实测：auto mode 下该 Bash 调用由分类器直接放行、无弹窗；默认权限模式应预期对该命令的一次性授权提示。ctl 路径解析到 `plugins/walkout/bin/`（本地按引用运行，见「准备」注记）；非本地分发是否走缓存目录尚未验证，ctl 同目录解析对两种位置均成立（单元测试覆盖）。若模型不执行或权限体验不可接受，记录现象——预案是从注入指令中移除自然语言分支，仅保留命令通道。

## 升级流程（门槛 11，真机验证 2026-09-25）

退出 Claude、停 daemon 后，把 `plugins/walkout/.claude-plugin/plugin.json` 与 `.claude-plugin/marketplace.json` 两处 `version` 同步升到新版本（假设：`plugin update` 以 marketplace 目录条目判断是否有更新，只改清单可能报"已是最新"——未做对照实验，故建议两处同步），然后：

```powershell
claude plugin marketplace update lexicon
claude plugin update walkout@lexicon      # 预期 updated from 0.1.0 to 0.1.1
claude plugin details walkout@lexicon     # 预期显示新版本
```

重启 daemon 与 `claude`，输入 `/walkout:status` 确认命令仍可用。验证完成后回退两个版本文件。

## 重跑

先退出终端 B 的 Claude，再在终端 A `Ctrl+C` 停 daemon——daemon 仍在运行时 `reset.ps1` 会因 `state.db` 被占用而失败（`being used by another process`）。两条操作纪律（2026-09-28 踩坑）：插件是用户级安装，本机任何已启动的 Claude Code 进程（包括你用来记录结果的工作会话）都会带着 hook 与 `/walkout:*` 命令连到探针 daemon 并改写其状态；卸载插件不影响已启动的进程，须重启该进程。预置后先用探针 `bin\walkout-ctl.exe status` 核对状态与预期一致，再进入 TUI。然后：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/reset.ps1
```

## 清理

1. 退出终端 B 的 Claude Code。
2. 终端 A `Ctrl+C` 停 probe daemon（脚本检测到仍运行会拒绝继续）。

默认清理卸载插件、移除 marketplace，保留 `.tmp` 产物：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/cleanup.ps1
```

确认产物不再需要后，零残留删除：

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/cleanup.ps1 -RemoveArtifacts
```

`-RemoveArtifacts` 只递归删除经绝对路径校验、位于本仓库 `.tmp/` 下的 `walkout-plugin-probe`。`plugins/walkout/bin/*.exe` 由 `.gitignore` 忽略，无需清理。

## 彻底清理清单（零残留验收，真机验证 2026-08-25）

`cleanup.ps1 -RemoveArtifacts` 之外的已知残留与检查点：

1. **插件缓存**：`claude plugin uninstall` + `claude plugin marketplace remove` 均不清 `~/.claude/plugins/cache/<marketplace 名>/`，须手动删除该目录。
2. **项目信任记录**：探针项目目录的信任状态记录在 `~/.claude.json`；删除 `.tmp` 探针目录后应 `grep` 该文件确认无探针项目路径残留（真机实测卸载后为零残留，此项通常只需验收，不需动手）。

零残留验收命令：缓存目录不存在、`claude plugin marketplace list` 中无该 marketplace、`~/.claude.json` 中 `grep` 不到探针目录名、无 `walkoutd` 进程存活、仓库 `.tmp/` 下无 probe 目录。
