# Walkout 卸载与零残留清理

本文是 Walkout 在 Windows 上的完整卸载手册：覆盖两宿主插件、`walkoutd` daemon、数据文件、探针产物与仓库构建产物，并单独列出**宿主命令不会自动清除**的残留及其安全处理手法。所有路径、命令与残留均于 2026-08-25 与 2026-09-28 在 Claude Code `2.1.281` / Codex `0.156.1` 真机核实。

## 1. 足迹总览

| 组件 | 位置 | 由谁写入 | 由谁清除 |
| --- | --- | --- | --- |
| Claude Code 插件（用户级） | Claude 用户设置 `enabledPlugins`、`~/.claude/plugins/installed_plugins.json` | `claude plugin install` | `claude plugin uninstall` |
| Claude marketplace `lexicon` | Claude 用户设置 | `claude plugin marketplace add` | `claude plugin marketplace remove` |
| Claude 插件缓存 | `~/.claude/plugins/cache/lexicon/walkout/<version>/`（含 `bin/*.exe` 副本） | `claude plugin install` / `update` | **不会自动清除**，手动删除 |
| Claude 项目信任 | `~/.claude.json` 中的项目路径记录 | 首次在某目录启动 `claude` | 卸载不涉及；探针目录删除后只需核对无残留 |
| Codex 插件 | `~/.codex/plugins/cache/<marketplace>/walkout/<version>/` | `codex plugin add` | `codex plugin remove` 移除登记，**缓存目录不清** |
| Codex marketplace | `~/.codex/config.toml` | `codex plugin marketplace add` | `codex plugin marketplace remove` |
| GitHub 形式 marketplace 克隆 | Claude `~/.claude/plugins/marketplaces/<name>/`；Codex `~/.codex/.tmp/marketplaces/<name>/` | `marketplace add <owner>/<repo>` | 对应的 `marketplace remove` 会一并删除（2026-09-28 两宿主实测） |
| Codex hook 信任 | `~/.codex/config.toml` 的 `[hooks.state."walkout@<marketplace>:.codex/hooks.json:<event>:0:0"]` 段（每事件一段，含 `trusted_hash`） | 首次授权 hook | **任何 Codex 命令都不清除**，手动删除 |
| daemon 进程 | `walkoutd.exe`（V1 无服务注册、无开机自启；由用户或探针脚本前台/后台启动） | 手动 | Ctrl+C 或 `Stop-Process` |
| daemon 数据 | `%LocalAppData%\Walkout\walkout.db`（及 `-wal` / `-shm`） | daemon 首次运行 | 手动删除 |
| 命名管道 | `\\.\pipe\walkout-<SID>` | daemon 运行期 | 随进程退出消失，无残留 |
| PATH | 无自动写入；若用户手动把 `walkout-ctl.exe` 所在目录加入 PATH，须自行移除 | 用户 | 用户 |
| 探针产物 | 仓库 `.tmp/walkout-plugin-probe/`、`.tmp/codex-emergency-probe/` | `prepare.ps1` | `cleanup.ps1 -RemoveArtifacts` |
| 仓库构建产物 | `plugins/walkout/bin/*.exe`、`plugins/walkout-codex/bin/*.exe`（gitignore）、`.tmp/go-build/`、`.tmp/go-mod/` | `build.ps1` / go | 可选手动删除 |

## 2. 卸载顺序

顺序有依赖，倒序会留下无法定位的残留或删除失败：

1. 退出所有 Claude Code 与 Codex 会话（包括 IDE 内嵌与 headless 进程）。已启动的宿主进程在插件卸载后仍会保留内存中的 hook 与命令，**必须重启才真正脱离**。
2. 停止 daemon：前台 Ctrl+C；否则 `Get-Process walkoutd` 后 `Stop-Process -Id <pid>`。daemon 未停时数据库文件被占用、无法删除。
3. 卸载宿主插件与 marketplace（§3、§4）。
4. 清除宿主不会自动清除的缓存与信任残留（§3、§4）。
5. 删除 daemon 数据（§5）。
6. 删除探针产物与仓库构建产物（§6）。
7. 按 §7 逐项核验零残留。

## 3. Claude Code

```powershell
claude plugin uninstall walkout@lexicon
claude plugin marketplace remove lexicon
claude plugin list                    # 不应再出现 walkout
claude plugin marketplace list        # 不应再出现 lexicon
```

宿主不清除的残留：

1. **插件缓存**：`Remove-Item -Recurse -Force "$env:USERPROFILE\.claude\plugins\cache\lexicon"`。升级过的机器会有多个版本子目录（如 `0.1.0/`、`0.1.1/`），整目录删除。
2. **项目信任记录**：`Select-String -Path "$env:USERPROFILE\.claude.json" -Pattern 'walkout-plugin-probe'` 应无输出。真机实测卸载并删除探针目录后为零残留，通常只需核对。

注意：本地路径 marketplace（`claude plugin marketplace add <repo-root>`）在 `2.1.281` 上按引用运行仓库源目录内的插件，卸载后仓库内 `plugins/walkout/` 本身不受影响，也不需要清理。

## 4. Codex

```powershell
codex plugin remove walkout@lexicon          # 探针安装则为 walkout@walkout-probe
codex plugin marketplace remove lexicon      # 探针安装则为 walkout-probe
codex plugin list
codex plugin marketplace list
```

宿主不清除的残留：

1. **插件缓存**：`Remove-Item -Recurse -Force "$env:USERPROFILE\.codex\plugins\cache\<marketplace 名>"`。
2. **hook 信任孤儿段**：Codex 按插件、事件与命令 hash 持久保存信任，`plugin remove` 与 `marketplace remove` 都不删除；`0.146.0`+ 的 `/hooks` 设置只能禁用、不能撤销信任，且已卸载插件的孤儿段因无可加载目标不会出现在 `/hooks` 中。它们是无行为影响的孤立元数据，但会让下次安装同名插件时跳过审核（若命令 hash 未变）。若要求字节级零残留，按以下手法手动删除：
   - 备份：`Copy-Item "$env:USERPROFILE\.codex\config.toml" "$env:USERPROFILE\.codex\config.toml.bak"`。
   - 只读定位：`Select-String -Path "$env:USERPROFILE\.codex\config.toml" -Pattern 'hooks\.state\.".*walkout'`，每个事件（`SessionStart` / `UserPromptSubmit` / `SessionEnd`）一段，形如 `[hooks.state."walkout@lexicon:.codex/hooks.json:user_prompt_submit:0:0"]` + `trusted_hash = "…"` + 相邻空行。
   - 用编辑器只删除这些段（段头 + `trusted_hash` 行 + 空行），确认无其他配置混入。
   - 校验：`codex plugin marketplace list` 退出码为 0（配置仍可解析）。
   - 校验通过后删除备份。

## 5. daemon 与数据

```powershell
Get-Process walkoutd -ErrorAction SilentlyContinue | Stop-Process -Force
Remove-Item -Recurse -Force "$env:LOCALAPPDATA\Walkout"
```

V1 不注册 Windows 服务、计划任务或开机自启，`Get-Service`、`Get-ScheduledTask` 中不应有 Walkout 条目；若曾手动把 `walkoutd.exe` / `walkout-ctl.exe` 所在目录加入 PATH 或写入 `Startup`，须自行移除。命名管道随进程退出消失，不需要清理。

## 6. 探针产物与仓库构建产物

两份探针各有 `cleanup.ps1`：默认只卸载临时插件与 marketplace、保留 `.tmp` 产物；`-RemoveArtifacts` 才删除产物。脚本检测到对应探针 daemon 仍在运行时会拒绝执行；插件或 marketplace 已不存在时视为已完成，不阻止后续删除。

```powershell
pwsh -NoProfile -File <repo-root>/manual-probes/codex-emergency-continue/cleanup.ps1 -RemoveArtifacts
pwsh -NoProfile -File <repo-root>/manual-probes/claude-plugin-install/cleanup.ps1 -RemoveArtifacts
```

`-RemoveArtifacts` 只允许递归删除经绝对路径校验、位于仓库 `.tmp/` 下的探针目录。探针脚本不修改 `config.toml` 或 `~/.claude.json`，§3、§4 的残留仍需手动处理。

仓库构建产物可选清理：`plugins/walkout/bin/`、`plugins/walkout-codex/bin/`（gitignore，重建即得）；`.tmp/go-build/`、`.tmp/go-mod/` 是可复用的构建缓存，保留可加速下次构建。

## 7. 零残留核验清单

```powershell
claude plugin list | Select-String -Pattern walkout                      # 无输出
claude plugin marketplace list | Select-String -Pattern lexicon          # 无输出
Test-Path "$env:USERPROFILE\.claude\plugins\cache\lexicon"               # False
Select-String -Path "$env:USERPROFILE\.claude.json" -Pattern 'walkout-plugin-probe'   # 无输出
codex plugin list | Select-String -Pattern walkout                       # 无输出
codex plugin marketplace list | Select-String -Pattern 'lexicon|walkout-probe'        # 无输出
Test-Path "$env:USERPROFILE\.codex\plugins\cache\lexicon"                # False（探针为 walkout-probe）
Select-String -Path "$env:USERPROFILE\.codex\config.toml" -Pattern walkout            # 无输出
Get-Process walkoutd -ErrorAction SilentlyContinue                       # 无输出
Test-Path "$env:LOCALAPPDATA\Walkout"                                    # False
Get-ChildItem <repo-root>\.tmp -Directory | Select-String -Pattern probe # 无输出
```

## 8. 重新安装时的信任行为

- Claude Code：重装或版本升级后新进程静默启动，不重新要求信任插件 hooks（`2.1.281` 实测）。
- Codex：首次安装按 `SessionStart` / `UserPromptSubmit` / `SessionEnd` 三个事件逐条审核；hook 命令行未变且信任段仍在时不再询问；命令 hash 变化则受影响事件重新审核。若 §4 已删除信任段，重装会再次逐条审核——这是预期行为。
