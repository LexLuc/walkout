# Walkout Codex plugin

此目录是 Codex 的可分发插件，包含 `$walkout:done`、`$walkout:continue`、`$walkout:status` 三个 skill 与生命周期 hook，与 Claude Code 插件的 `/walkout:done`·`/walkout:continue`·`/walkout:status` 一一对应。`walkoutd` 与 `walkout-ctl` 仍作为独立后台服务安装（`walkout-ctl` 需在 PATH 上，`status` skill 与 `!walkout-ctl` 兜底都依赖它）。

`done` 与 `continue` 是 hook 在模型之前识别的控制标记；`status` 是只读 skill，由模型以脱沙箱权限执行 `walkout-ctl status` 并原样打印（Codex 沙箱无法打开 daemon 命名管道）。auto 模式下脱沙箱静默通过；on-request 审批策略下每次调用会弹一次批准，无弹窗路径仍是 `!walkout-ctl status`。`walkout` 态无 lease 时 `$walkout:status` 与 Claude 的 `/walkout:status` 一样会被阻断。

## 构建与安装

在仓库根目录运行：

```powershell
pwsh -NoProfile -File plugins/walkout-codex/build.ps1
codex plugin marketplace add D:/absolute/path/to/walkout
codex plugin add walkout@lexicon
```

安装后启动一个新的交互式 Codex 会话。输入 `$` 应能发现 `walkout:done`、`walkout:continue` 与 `walkout:status`。首次触发 `SessionStart`、`UserPromptSubmit` 或 `SessionEnd` 时，逐一批准每个事件（each event）的 hook 信任请求。

Codex 按事件和 command hash 保存信任。插件升级后，只要 hook 命令行发生变化，旧信任即失效；请在新的交互会话中 re-authorize 受影响事件。自动化场景只有在调用方已经独立校验 hook 来源时，才应使用 `--dangerously-bypass-hook-trust`。

## 版本与回落

由 npm、bun 或 pnpm 管理的 Codex 启动器会设置 `CODEX_MANAGED_PACKAGE_ROOT`。hook 从其 `package.json` 读取 `version`；独立二进制或文件不可读时传入空版本，既有 translator 将遥测值记为 `"unknown"`，守卫仍保持可用。

## 安装验收

1. 在新会话输入 `$`，确认 `walkout:done`、`walkout:continue` 与 `walkout:status` 可发现。
2. 在自洽的 `walkout` 状态提交普通提示词，确认 `UserPromptSubmit` 被阻止且 daemon 状态不漂移。
3. 提交 `$walkout:continue <原因>`，确认同一次提交自授权放行，lease 激活且原因逐字保留。
4. 检查捕获的 `HealthEvent.host_version`：托管安装应为实际版本；独立二进制应为 `"unknown"`。
