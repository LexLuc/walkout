# AI Agent 生命周期 Hooks：原理、术语与 Walkout 可行性详解

> **AI 辅助与人工审核声明**：本文由 AI 辅助起草。涉及产品决策、隐私政策、健康建议和正式发布时，应由人类负责人审核。

本文版本基于 **2026-08-12** 可获得的 Codex、Claude Code 与 WorkBuddy 官方资料，并纳入 Codex CLI `0.147.0`、Claude Code `2.1.224` 的本机实测结果及 Codex CLI `0.146.0` 的 release 源码核查。生命周期事件和字段仍可能随客户端版本变化，实现时应以目标版本的官方发布契约与版本化 capability probe 为准；官方仓库 `main` 分支的 schema 可能包含尚未发布的字段，不能单独作为当前版本依据。产品在发布前由工作名 Desk Health Agent / `deskhealth` 更名为 **Walkout**（2026-08-17），文中历史实测记录里的插件、命令与二进制标识已统一按新名规范化，实测事实本身未改动。

本文讨论的是通用知识工作 Agent：法律审阅、会计与财务核对、设计制作、研究分析、运营协作、咨询、行政管理和软件开发都在产品范围内。Codex、Claude Code 与 WorkBuddy 是第一版宿主，不是职业边界。

## 1. 先给结论

Walkout 的核心设想在技术上可行，因为 Codex、Claude Code 和 WorkBuddy 都允许在 Agent 运行过程中的特定节点执行本地程序。这种扩展机制称为 **lifecycle hooks（生命周期钩子）**。

Hooks 可以帮助我们：

- 在 Codex、Claude Code 或 WorkBuddy 会话启动时登记一段工作会话；
- 在用户发送新提示词之前检查连续工作时间；
- 在 Agent 调用工具时更新“用户仍在工作”的活动状态；
- 在 Agent 完成一次回答时，要求它结合当前工作上下文生成健康提醒；
- 达到强制休息阈值后，在当前 turn 结束后拦截下一条普通提示词；
- 在用户完成无摄像头确认或可选的本地摄像头离席实验后恢复服务。

Hooks 也有清晰的能力边界：

- Hook 只能在宿主定义的生命周期节点运行，不能天然地在任意一秒插入对话；
- 仅使用 hooks 时，长时间没有工具调用的模型生成通常要等到下一个 hook 节点才能干预；
- 用户拥有本地电脑，通常可以禁用插件、hooks 或守护进程，因此它是一种自我约束机制，不是不可绕过的安全控制；
- 摄像头可以判断人是否离开画面，不能证明用户实际喝了水。

第一版最合理的承诺是：**利用 lifecycle 事件计时，在安全的 turn 边界提醒，并在当前工作形成可恢复结果后暂停下一条普通请求。**

## 2. 理解 Agent 前，先区分五个角色

很多讨论会把模型、Agent、Codex、hooks 和 MCP 混在一起。它们实际上位于不同层级。

### 2.1 Model：模型

模型是接收上下文并生成下一段内容的推理系统。它可以：

- 阅读用户需求；
- 理解文档、数据、设计素材、代码和其他工作上下文；
- 决定是否调用工具；
- 生成自然语言回答或结构化参数。

模型自身通常不知道真实时间，也不会在没有请求的情况下主动醒来。模型看到的时间、会话状态和健康债务需要由宿主或工具提供。

### 2.2 Agent：智能体

Agent 是“模型 + 指令 + 工具 + 循环控制”组成的工作单元。

一个知识工作 Agent 的基本循环是：

```text
读取用户目标
    ↓
模型判断下一步
    ↓
直接回答，或调用工具
    ↓
宿主执行工具并返回结果
    ↓
模型继续判断
    ↓
达到完成条件后结束本轮
```

这个不断重复的过程称为 **agent loop（Agent 循环）**。

### 2.3 Agent Host：Agent 宿主

Codex、Claude Code 与 WorkBuddy 都属于 Agent 宿主。宿主负责：

- 管理会话和对话记录；
- 调用模型；
- 执行文档、表格、设计、浏览器、通信、shell、文件编辑和搜索等工具；
- 显示流式输出；
- 处理权限审批；
- 执行 hooks；
- 决定 Agent 何时继续或停止。

生命周期 hooks 是宿主能力。模型可以响应 hook 注入的信息，真正调用 hook 程序的是 Codex、Claude Code 或 WorkBuddy。

### 2.4 Hook Handler：Hook 处理器

Hook handler 是生命周期事件发生时，由宿主启动的本地程序、HTTP endpoint、MCP tool 或其他受支持处理器。

在最常见的 command hook 中：

1. 宿主启动一个脚本；
2. 通过标准输入 `stdin` 传入 JSON；
3. 脚本读取事件信息并执行判断；
4. 脚本通过退出码和标准输出 `stdout` 返回结果；
5. 宿主解释结果，决定继续、注入上下文或阻止动作。

### 2.5 walkoutd：跨 Agent 的本地状态服务

`walkoutd` 是我们方案中的常驻本地进程。它不取代任何主 Agent，也不负责生成完整回答。它负责：

- 分开维护 Agent runtime 与人的参与区间，并对多个 session 的人类参与时间取并集；
- 保存连续工作时间、延后次数和健康债务；
- 执行确定性的升级规则；
- 管理无摄像头恢复，并可选协调摄像头离席实验；
- 向不同 Agent 返回统一状态。

上下文理解由当前 Agent 完成。`walkoutd` 默认接收结构化判断，不需要永久保存原始聊天记录。

## 3. 生命周期中最重要的术语

### 3.1 Session：会话

Session 是一次可持续、可恢复的 Agent 使用过程。用户关闭终端后再次恢复同一对话，通常仍然对应同一个逻辑会话或可恢复会话。

Session 适合承载：

- 当前项目目录；
- 会话标识符；
- 模型和权限模式；
- 对话 transcript；
- 本次会话的开始、恢复和结束事件。

健康计时不能简单等同于 session 存活时间。用户可能打开会话后离开电脑一小时，因此需要结合活动事件和系统空闲状态。

### 3.2 Thread：对话线程

Codex App Server 使用 **thread** 表示一段对话。一个 thread 包含多个 turn，可以创建、恢复或分叉。

在日常讨论中，session 和 thread 有时会被宽泛地当作“一个聊天”。实现时需要遵循具体宿主的字段定义：

- hook 输入通常提供 `session_id`；
- Codex App Server API 主要操作 `threadId`；
- 健康状态应建立自己的跨宿主 `work_session_id`，不要直接假设二者完全相同。

### 3.3 Turn：轮次

Turn 通常从用户提交一次请求开始，到 Agent 完成该请求结束。

一个 turn 内可能包含多次模型请求和多次工具调用：

```text
用户提示词
  └─ 模型请求 1
       └─ 调用 rg
            └─ 模型请求 2
                 └─ 修改文件
                      └─ 模型请求 3
                           └─ 最终回答
```

健康提醒最好在 turn 边界出现，因为这通常意味着当前任务已经形成一个可恢复结果。

### 3.4 Model Request：模型请求

一次 model request 是宿主把当前上下文发送给模型，并等待模型返回下一步输出。

一个 turn 可以包含多个 model request。Hook 注入的 `additionalContext` 往往会在“下一次 model request”中被模型看到。

### 3.5 Tool Call / Tool Use：工具调用

模型请求宿主执行某个能力称为 tool call。例如：

- 运行 shell 命令；
- 读取或修改文件；
- 读取或编辑文档与表格；
- 操作设计素材、浏览器或业务连接器；
- 搜索代码或资料；
- 调用 MCP server；
- 启动 subagent。

工具调用通常分为三个阶段：

```text
PreToolUse → 权限判断或执行 → PostToolUse / PostToolUseFailure
```

这些阶段是更新活跃状态和执行强制暂停的重要边界。

### 3.6 Transcript：对话记录

Transcript 是宿主保存的对话和事件记录。Hook 输入可能提供 `transcript_path`，方便本地脚本访问。

产品设计不应把 transcript 文件格式当成稳定 API：

- Codex 官方明确提示 transcript 格式可能变化；
- Claude Code 的部分事件直接提供 `last_assistant_message`，官方建议在这些场景优先使用事件字段；
- Walkout 应尽量通过稳定事件字段和 MCP tool 获取结构化信息。

### 3.7 Context Window：上下文窗口

Context window 是模型本次推理能看到的信息集合，可能包含：

- 用户提示词；
- 历史消息；
- `AGENTS.md` 或 `CLAUDE.md` 指令；
- 工具结果；
- hook 注入的上下文；
- MCP tool 返回的数据。

Hook 注入过多文本会挤占模型上下文，并可能降低任务质量。健康 hook 应传递简短、结构化、与当前决策直接相关的信息。

## 4. 一次典型生命周期

下面是一段简化的 Codex、Claude Code 或 WorkBuddy 交互生命周期：

```text
启动或恢复客户端
    │
    ├─ SessionStart
    │
用户提交提示词
    │
    ├─ UserPromptSubmit
    │     ├─ 允许：提示词进入 Agent
    │     ├─ 注入：附加上下文后进入 Agent
    │     └─ 阻止：提示词不进入 Agent
    │
    ▼
模型开始推理
    │
    ├─ 直接产生最终回答 ──────────────┐
    │                                 │
    └─ 请求调用工具                   │
          │                           │
          ├─ PreToolUse               │
          ├─ PermissionRequest（需要时）│
          ├─ 执行工具                 │
          ├─ PostToolUse / Failure    │
          └─ 返回模型继续推理 ────────┤
                                      │
                                      ▼
                                    Stop
                                      │
                         ┌────────────┴────────────┐
                         │                         │
                      允许结束                 阻止结束
                         │                  Agent 获得反馈并继续
                         ▼
                     等待下一条提示词
                         │
                     SessionEnd
```

`Stop` 事件名称容易误解。它通常表示“Agent 准备结束本轮”，不是“整个客户端进程已经退出”。Stop hook 可以允许本轮结束，也可以让 Agent 继续完成额外动作。

## 5. Hook 配置由哪些部分组成

### 5.1 Event：事件

Event 表示 hook 发生在生命周期的哪个节点，例如：

- `SessionStart`
- `UserPromptSubmit`
- `PreToolUse`
- `PostToolUse`
- `Stop`
- `SessionEnd`

### 5.2 Matcher：匹配器

Matcher 用来过滤事件。例如只在 shell 工具执行前运行：

```json
{
  "matcher": "Bash"
}
```

有些事件没有可匹配的细分类别。例如三个宿主的 `UserPromptSubmit` 和 `Stop` 通常对每次事件触发，配置中的 matcher 可能不受支持或被忽略。

### 5.3 Handler：处理器

Handler 是真正被执行的程序。常见字段包括：

- `type`：处理器类型；
- `command`：要执行的本地命令；
- `commandWindows` / `command_windows`：Windows 专用命令；
- `timeout`：最长执行时间；
- `statusMessage`：运行时显示给用户的状态；
- `additionalContextLimit`：允许注入模型上下文的大致上限。

Codex 当前文档说明，实际执行的是 `command` handlers；`prompt` 和 `agent` 类型虽然可被解析，当前会被跳过。Claude Code 的官方文档列出了 `command`、`http`、`mcp_tool`、`prompt` 和实验性的 `agent` handlers。WorkBuddy / CodeBuddy 文档列出 `command`、`http`、`prompt` 和 `agent`，其中 hook API 仍处于 Beta。跨产品实现应以 command hook 作为公共最低能力。

### 5.4 Hook Source / Configuration Layer：配置来源与层级

Hook 可以来自：

- 用户级配置；
- 项目级配置；
- 管理员策略；
- 已安装插件。

Codex 常见位置包括：

- `~/.codex/hooks.json`
- `~/.codex/config.toml`
- 项目内 `.codex/hooks.json`
- 项目内 `.codex/config.toml`

Claude Code 常见位置包括：

- `~/.claude/settings.json`
- `.claude/settings.json`
- `.claude/settings.local.json`
- plugin 的 `hooks/hooks.json`

WorkBuddy / CodeBuddy 适配器使用插件根目录下的 `hooks/hooks.json`，并通过 `.workbuddy-plugin` 或兼容 manifest 分发。

多个来源的匹配 hooks 通常会合并运行。不要依赖两个独立 hook 之间的执行顺序；三个宿主都可能并行运行多个匹配 handler。

这对 `walkoutd` 有直接影响：状态更新必须使用事务、文件锁或数据库并发控制，不能假设 SessionStart 一定先于另一个并发会话的工具事件完成。

## 6. Hook 如何输入和输出

### 6.1 stdin：标准输入

Command hook 通常从 `stdin` 接收一个 JSON 对象。以概念形式表示：

```json
{
  "session_id": "session-123",
  "cwd": "D:/Work/project",
  "hook_event_name": "UserPromptSubmit",
  "permission_mode": "default",
  "prompt": "修复生产环境支付失败问题"
}
```

常见字段包括：

- `session_id`：宿主会话标识；
- `turn_id`：当前 turn 标识，部分事件提供；
- `cwd`：当前工作目录；
- `hook_event_name`：事件名称；
- `permission_mode`：当前权限模式；
- `transcript_path`：对话记录路径，可能为空；
- `model`：Codex 当前模型标识；它不是每类事件都能依赖的必填字段；
- 事件专用字段，例如 `prompt`、`tool_name`、`tool_input`。

### 6.2 stdout：标准输出

Hook 可以通过 stdout 返回：

- 空输出：不作额外决定；
- 普通文本：在支持的事件中作为上下文或 hook 输出；
- JSON：提供结构化决策、上下文或用户提示。

当宿主要求 JSON 时，stdout 应只包含一个合法 JSON 对象。脚本调试日志应写入 stderr 或独立日志文件，避免破坏 JSON 解析。

### 6.3 Exit Code：退出码

退出码是进程向宿主表达结果的基础机制：

- `0`：脚本正常完成，宿主继续解释 stdout；
- `2`：在支持阻断的事件中表示“阻止当前动作”；
- 其他非零值：通常表示 hook 执行失败，具体后果依事件和宿主而不同。

退出码 `2` 的语义取决于事件：

- 在 `UserPromptSubmit` 中，阻止提示词处理；
- 在 `PreToolUse` 中，阻止工具调用；
- 在 `Stop` 中，阻止 Agent 停止，因此 Agent 会继续工作；
- 在已经发生的 `PostToolUse` 中，无法撤销已执行的工具，只能提供反馈。

产品实现应优先使用官方支持的结构化 JSON 决策，因为它能表达更明确的原因和后续行为。

## 7. 五种关键控制能力

### 7.1 additionalContext：向模型添加上下文

`additionalContext` 会在下一次模型请求中加入一段额外指令或事实。

示例：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "UserPromptSubmit",
    "additionalContext": "用户已连续工作 52 分钟。完成当前回答后，用一句与当前任务相关的幽默提醒建议用户离席和喝水。"
  }
}
```

它适合：

- 告诉 Agent 已连续工作多久；
- 要求 Agent 使用当前工作语境写提醒；
- 告诉 Agent 当前处于健康保护窗口；
- 要求 Agent 在结束前调用 `health.assess`。

它不等同于用户消息。宿主通常把它作为系统提醒、开发者上下文或 hook feedback 交给模型。

### 7.2 systemMessage：给用户显示警告

`systemMessage` 通常用于在 UI 或事件流中显示警告。它主要面向用户，不能简单假设模型一定会把它视为任务上下文。

适合显示：

- `walkoutd` 当前不可用；
- 健康 hook 配置错误；
- 下一条普通请求将进入服务暂停。

### 7.3 decision: block：阻止当前生命周期动作

在 `UserPromptSubmit` 中：

```json
{
  "decision": "block",
  "reason": "已达到休息阈值。确认完成一次恢复活动后继续。"
}
```

这段 JSON 表达的是“请求宿主阻止当前提示词”。Codex CLI `0.147.0` 的 headless probe 已确认它会阻止提示词进入模型；输入框是否保留内容、提示词是否写入 transcript、用户能否无损重试，以及失败或超时后的默认行为仍必须在各宿主的交互界面和目标版本中分别实测。示例本身不能证明这些 UI 与数据行为，Codex 的已验证边界见 9.2.1。

建议做法：

- 在上一个 turn 结束时提前预告即将暂停；
- 用 capability probe 记录输入框、transcript、重试和超时行为；
- 如果实测证明宿主允许，再设计草稿保留体验；
- 健康控制命令永远放行；
- 恢复后提示用户重新提交原请求；
- 默认不把完整提示词复制到 `walkoutd` 的持久存储。

### 7.4 permissionDecision：控制工具调用

`PreToolUse` 在工具真正执行前触发。三个宿主都提供了面向工具的允许、询问或拒绝机制，字段形状存在差异。

这是可用的宿主能力，不是 V1 发布项。第一版的 `PreToolUse` 只上报活动心跳；逐工具分类和阻断会在不同职业工具上产生较高适配成本，暂停统一由下一次 `UserPromptSubmit` 执行。

后续版本如果确实需要逐工具暂停，可以拒绝新的非必要工具：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "deny",
    "permissionDecisionReason": "Walkout 已暂停新的工具执行，等待离席确认。"
  }
}
```

需要保留的操作包括：

- 保存或导出当前工作；
- 读取健康状态；
- 启动摄像头验证；
- 使用紧急延期；
- 完成已经进入不可安全中断阶段的恢复操作。

### 7.5 Stop continuation：阻止 Agent 结束并让它继续

在 `Stop` hook 中，`decision: "block"` 的意思是“当前 Agent 还不能结束”。宿主会把 `reason` 交给 Agent，让它继续一次或多次模型循环。

Codex 示例：

```json
{
  "decision": "block",
  "reason": "在结束本轮前，根据当前工作上下文调用 health.assess，然后给出健康提醒。"
}
```

Claude Code 同样支持在 Stop 时继续，并支持通过 `additionalContext` 作为非错误反馈让 Claude 继续。

Stop hook 必须检查类似 `stop_hook_active` 的字段或自行保存幂等状态，否则可能形成无限循环：

```text
Agent 准备结束
→ Stop hook 阻止
→ Agent 输出提醒并再次结束
→ Stop hook 再次阻止
→ 无限重复
```

正确规则是：每个提醒周期只允许 Stop hook 触发一次 continuation。

## 8. 与健康提醒最相关的事件

| Hook 事件 | 触发时机 | 能看到什么 | 能做什么 | 在本方案中的用途 |
| --- | --- | --- | --- | --- |
| `SessionStart` | 启动、恢复或清空会话后 | 会话、目录、启动来源 | 注入上下文、登记会话 | 告诉 Agent 健康协议，连接 `walkoutd` |
| `UserPromptSubmit` | 用户提示词进入模型前 | 提示词、会话状态 | 注入上下文或阻止提示词 | 判断软提醒、保护窗口和暂停下一条请求 |
| `PreToolUse` | 工具执行前 | 工具名与参数 | 允许、询问、拒绝或修改参数 | V1 更新 Agent runtime；不能单独证明人仍在场 |
| `PermissionRequest` | 工具需要权限时 | 工具与审批信息 | 参与审批判断 | 避免健康逻辑与权限弹窗互相冲突 |
| `PostToolUse` | 工具成功后 | 工具输入与结果 | 追加反馈；无法撤销工具 | 更新 Agent runtime，识别文档保存、报表导出、设计渲染、构建或测试完成等自然停顿点 |
| `PostToolUseFailure` | 工具失败后 | 错误信息 | 给 Agent 反馈 | 避免在关键恢复步骤中突然打断 |
| `Stop` | Agent 准备结束 turn | 最终消息、turn 状态 | 允许结束或要求继续 | 上下文评估与对话内提醒的核心节点 |
| `SessionEnd` | 会话退出或结束 | 结束原因 | 日志与清理 | 关闭该 session；其他 session 的参与时间不受影响 |
| `MessageDisplay` | Claude 文本流式显示时 | 当前显示增量 | 替换显示内容 | Claude Code 专有增强；不建议作为跨产品主链路 |
| `Notification` | Claude Code 发通知时 | 通知类型和内容 | 执行通知副作用 | 将健康状态转为终端或桌面提示 |

## 9. Codex、Claude Code 与 WorkBuddy 的关键差异

### 9.1 共同能力

三者都能支持第一版的核心路径：

- session 开始和结束；
- 用户提示词提交前检查；
- 工具执行前后检查；
- Agent 停止前检查；
- 上下文注入；
- 阻止提示词；工具阻止能力留给后续版本；
- 通过插件分发 hooks。

### 9.2 Codex 特点

- 当前官方文档中的 hooks handler 实际执行 `command` 类型；
- 非管理员 command hook 需要用户检查并信任；
- 多个匹配 hooks 会并发运行；
- `UserPromptSubmit` 可以注入额外开发者上下文或阻止提示词；
- `Stop` 的阻断会形成新的 continuation prompt；
- `PostToolUse` 支持反馈和停止后续 Agent 循环，但已经执行的工具不会被撤销；
- App Server 提供 thread、turn 和流式事件等更深层接口；自建宿主可以调用 `turn/interrupt` 中断正在进行的 turn。

Codex App Server 适合未来希望精确控制运行中 turn 的版本。仅安装 hooks 的第一版应以生命周期边界为干预点。

#### 9.2.1 Codex CLI `0.147.0` 实测校正

**证据范围**：2026-08-08 在 Windows x64 上，以无敏感内容的 `codex exec --ephemeral` 受控会话采集真实 hook 输入；版本化、脱敏后的原始形状保存在 `internal/adapters/codex/testdata/v0.147.0/`，采集边界记录在其中的 `manifest.json`。本轮覆盖 headless CLI 的 `SessionStart`、`UserPromptSubmit`、`Stop`、`SessionEnd` 与 `UserPromptSubmit` 阻断，没有覆盖交互式终端或桌面 UI。

| 议题 | 本文先前的文档推断 | `0.147.0` 实测 | 对实现的影响 |
| --- | --- | --- | --- |
| 核心事件能否触发 | 官方契约表明四个核心事件可用，开发前仍属待验证能力 | 四个事件都实际到达 recorder | Codex MVP 可以采用 hooks 主链路，无需为了这些边界提前引入 App Server |
| `turn_id` 与通用字段 | `turn_id` 只在部分事件出现；官方“通用字段”说明容易让实现误把 `model` 当成全事件必填 | `UserPromptSubmit`、`Stop` 有 `turn_id`；`SessionStart`、`SessionEnd` 没有；`SessionEnd` 同时缺少 `model` 与 `permission_mode` | 每类事件使用独立输入契约；`model`、`permission_mode` 只作可选诊断信息；缺少未承诺字段不能导致误拒绝 |
| `transcript_path` | 可能为空，且 transcript 格式不是稳定 API | 本次 ephemeral 会话四类事件的 `transcript_path` 都是 `null` | adapter 不读取 transcript，也不依赖它生成事件；工作语境由当前 Agent 理解，daemon 继续只处理结构化事实与确定性策略 |
| 自然语言字段 | `UserPromptSubmit` 可能提供 prompt，`Stop` 可能提供最后一条回答，可用于上下文相关提醒 | 两个正文分别真实存在 | translator 只校验必要字段存在，不把 prompt、assistant message、cwd、model 或 transcript 路径写入 `HealthEvent`；提醒语气和紧急性评估留在已拥有对话上下文的 Agent 侧 |
| `decision: block` | 官方描述可阻止提示词，输入保留与重试体验待测 | Codex 明确报告 `UserPromptSubmit Blocked`，模型没有处理该提示词 | `block_prompt` 只对已验证的 Codex `0.147.0` 宣告 capability；renderer 可输出该决定，产品仍不能承诺草稿保留或自动重试 |
| headless 阻断后的进程状态 | 原文没有给出被阻断非交互进程的等待上限 | 被阻断的 `codex exec` 在 30 秒内没有自行退出 | 自动化 probe 必须由父进程设置 deadline；不能把“被阻断的 exec 是否迅速退出”用作 daemon 健康检查或安装成功条件 |
| `SessionEnd` 的可观察性 | 可用于会话结束日志和清理 | recorder 收到 `SessionEnd(reason=other)`，但 exec 控制台生命周期 trace 没有显示它 | contract probe 读取 recorder 产物，不能只解析控制台；`SessionEnd` 仅作 advisory 信号，关键计时仍由最近人类交互窗口收敛 |
| `SessionEnd` 超时上限 | §15.3 记录 `SessionEnd` 缺省 1 秒、最大 3 秒，但缺少真实运行证据 | 声明 `timeout=10` 的 `SessionEnd` hook 触发 `clamping SessionEnd hook timeout to 3s` 警告并被强制截到 3 秒 | 安装器生成 Codex hooks 时必须给 `SessionEnd` 直接写入 ≤3 秒，否则真实用户每次会话都会看到 clamping 警告；`walkout-hook` 整链墙钟约 100–140 ms 远低于 3 秒，功能不受影响，但配置值不应超出宿主上限 |
| 项目与 hook 信任 | 已知非管理员 command hook 需要用户审核和信任 | 未信任项目不会加载项目级 `.codex` hooks；绕过 hook-definition trust 也没有让未加载的项目配置生效 | 安装器和 capability probe 分别检查项目配置层是否受信任、具体 hook 定义是否受信任，并以一次真实事件到达作为最终判据 |

这次实测确认的是特定版本、平台与运行方式，而不是所有 Codex 客户端的永久契约。解析器因此接受未知的新增字段，对缺少真正必需字段或未知事件静默 fail open；capability 由版本化 probe 或显式配置声明，不能只根据 `provider=codex` 推断。

上述 `0.147.0` headless probe 没有验证交互界面；后续 `0.146.0` TUI probe 已验证暂停反馈、plugin skill 固定标记、自授权时序与 shell 兜底，见下一节。仍未验证的 Codex 行为包括 `additionalContext` 注入、Stop continuation、工具事件字段与时序，以及各事件更完整的失败/超时细节；这些能力在验证前不能进入 Codex V1 的可承诺能力清单。

#### 9.2.2 Codex CLI `0.146.0` 对话内紧急继续实测

**证据范围**：2026-08-12 在 Windows x64 的真实 Codex TUI 中安装仓库内 plugin（当时的工作名为 `deskhealth`，产品在发布前更名为 Walkout；当时的暂停态名为 `service_paused`，2026-08-26 三态简化后对应 `walkout`），以自洽暂停态数据库、正式 hook 二进制、真实命名管道 daemon 和隐私过滤 recorder 验证。脱敏控制 payload 与采集边界保存在 `internal/adapters/codex/testdata/v0.146.0/`；recorder 只允许固定控制标记落盘，普通 prompt 不记录。

| 验证点 | 真实结果 | 产品/实现结论 |
| --- | --- | --- |
| 暂停基线 | 普通 `hello` 被 `UserPromptSubmit hook (blocked)` 阻止，完整显示 `$walkout:continue [reason]` 主入口与固定 `!walkout-ctl ...` 兜底 | 新文案与提示阻止在真实 TUI 可见 |
| `$` 主入口 | `$walkout:continue 生产事故热修复` 在暂停态未被阻止；skill 只输出一句请求确认，没有读文件或调用工具 | 固定标记 hook 自授权与显式 skill 的模型 turn 约束成立 |
| lease 语义 | 状态保持暂停态（当时名 `service_paused`，现 `walkout`），lease active，reason 原样记录，债务继续累计；后续普通 prompt 正常放行 | 沿用既有 15 分钟 lease，没有赦免债务或新增 core 语义 |
| `!` 兜底 | `!walkout-ctl emergency-continue -reason "fallback probe"` 直接执行并把 lease 续回约 900 秒，最近 reason 更新 | shell escape 可在 prompt hook 外承担故障兜底；它仍是显式全权限 shell，不是安全边界 |
| payload 与隐私 | TUI `UserPromptSubmit.prompt` 精确保留 `$walkout:continue <reason>`；`transcript_path` 非空、`permission_mode=default`；第二轮 recorder 仅有 1 条控制提交 | 识别可停留在 Codex adapter/hookcli；普通工作内容不进入 fixture、协议或 core |

该结论只适用于被测 `0.146.0`、Windows x64、Codex TUI 与当前 plugin/hook command hash。宿主升级后必须复跑同一探针，并更新版本化 fixture，不能仅依赖源码或历史结果。

**2026-08-16 分发形态校正**：同版本真机进一步确认 plugin manifest 可用 `hooks: "./.codex/hooks.json"` 打包三个 lifecycle hook；安装缓存把 `${PLUGIN_ROOT}` 替换为插件根，hook 来源显示为 plugin，无需向全局或项目写 hook。Windows hook shell 是 PowerShell，带引号的 exe 路径不能直接充当命令首项，生产配置因此经 `pwsh -File hooks/run-hook.ps1` 启动 binary。`codex plugin marketplace add <repo-root>` + `codex plugin add walkout@lexicon` 是 `0.146.0` 的实际安装路径；真人逐事件/command hash 授权后，skill 发现、暂停阻止、同提交自授权、lease 内放行与 `SessionEnd` 3 秒上限均通过。脱敏事件元数据复验还直接观察到 npm launcher 的三个最终事件版本均为 `0.146.0`，direct native binary 的三个最终事件均回落 `"unknown"`。

#### 9.2.3 可复用的真实宿主探针方法

宿主边界不能只靠官方文档、源码阅读或 mock 推断。可复用探针采用同一条证据链：先用专用 seeder 写入与 tracker 事实自洽的目标状态，再启动真实 daemon 和正式 hook 二进制；由真实宿主界面提交一个无敏感普通 prompt 验证阻止基线，再提交目标控制输入、回读 daemon 状态，并用后续普通 prompt 证明状态变化已经影响真实宿主行为。每个时间相关断言在自动测试中使用 fake clock，真机探针只验证宿主加载、时序、呈现和输入保留等无法注入的边界。

recorder 应位于正式 hook 前面，并继续把同一份 stdin 原样交给正式 hook；它只捕获精确固定控制标记，普通工作 prompt 不落盘。原始记录只保存在 Git 忽略的 `.tmp/`，固化前脱敏 session、turn、cwd、model 等字段，并在版本化 `manifest.json` 中记录宿主版本、采集方式、清洗规则、已验证能力和未覆盖界面。控制台 trace 与 recorder 产物必须交叉确认，因为某些事件会到达 hook 却不显示在控制台。

需要验证 translator 补入、宿主原始 stdin 不含的字段时，现有 Codex 探针可给单个 Codex 子进程设置 `WALKOUT_EVENT_PROBE_OUTPUT`。正式 hook 只在 daemon 成功接受事件后追加最终 `HealthEvent` 的 schema/provider/version/event type 四字段，不记录 prompt、session、cwd、reason 或 payload；该观察口默认关闭，写入失败不改变 fail-open/guard 行为。

探针必须设置父进程 deadline，按目标宿主分别处理信任、PATH、shell、超时和阻止后的进程状态；一次真实事件到达才算安装成功。完整的 Codex 紧急继续 harness、隐私边界、重跑与清理步骤见 [`manual-probes/codex-emergency-continue/README.md`](manual-probes/codex-emergency-continue/README.md)。运行产物不进入版本控制，脚本和判定标准进入版本控制，以便版本升级后原样复跑并比较 fixture。

### 9.3 Claude Code 特点

- handler 类型更丰富，包括 command、HTTP、MCP tool、prompt 和实验性 agent；
- `UserPromptSubmit` 可以注入上下文或阻止提示词；
- `Stop` 可以让 Claude 继续执行，并提供 `last_assistant_message`；
- `MessageDisplay` 可以替换屏幕正在显示的文本，但不会改变 transcript，也不会改变 Claude 看到的内容；
- `terminalSequence` 可以通过受限终端控制序列发送桌面通知或响铃；
- 官方文档明确描述了多种事件的超时和阻断行为。

`MessageDisplay` 很适合做 Claude Code 的视觉增强，同时会造成“用户看到的显示内容”和“模型及 transcript 中的内容”不同。跨 Agent 产品的正式提醒应优先进入真实对话链路，避免状态不一致。

#### 9.3.1 Claude Code `2.1.224` 实测校正

**证据范围**：2026-08-08 在 Windows x64 上，以无敏感内容的 `claude -p` 受控会话（临时 `--settings` hooks、`--max-turns 1` 与 `--max-budget-usd` 预算上限）采集真实 hook 输入；版本化、脱敏后的原始形状保存在 `internal/adapters/claudecode/testdata/v2.1.224/`，采集边界记录在其中的 `manifest.json`。本轮覆盖 headless CLI 的 `SessionStart`、`UserPromptSubmit`、`Stop`、`SessionEnd` 与 `UserPromptSubmit` 阻断，没有覆盖交互式终端或 IDE 界面。

| 议题 | 本文先前的文档推断 | `2.1.224` 实测 | 对实现的影响 |
| --- | --- | --- | --- |
| 核心事件能否触发 | 官方契约表明四个核心事件可用，开发前仍属待验证能力 | 四个事件都实际到达 recorder | Claude Code MVP 可以采用 hooks 主链路 |
| 轮次标识 | Claude Code 与 Codex 的 hook JSON 看似相近，容易推断两宿主共用 `turn_id` 的同构 payload | 没有 `turn_id`；轮次标识是 `prompt_id`，且 `SessionEnd` 也携带 `prompt_id`（包括 prompt 被阻止的会话） | 两宿主 payload 不同构；translator 与宿主 payload struct 按宿主独立维护，`prompt_id` 映射为 `HealthEvent` 的轮次标识；`SessionEnd` 上它保持可选，因为无 prompt 的会话未被证明会携带 |
| `model` 与 `permission_mode` | “通用字段”说明容易让实现把 `model` 当成可依赖字段 | 四类事件都没有 `model`；`permission_mode` 只出现在 `UserPromptSubmit` 与 `Stop` | 每类事件使用独立输入契约；这些字段不能进入必填校验，缺失不能导致误拒绝 |
| `transcript_path` | 可能为空，且 transcript 格式不是稳定 API | 四类事件都携带真实文件路径，与 Codex ephemeral probe 的 `null` 相反 | adapter 不读取 transcript；路径按敏感信息处理，只在 hook 进程内短暂存在，不进入 `HealthEvent`、fixture 或错误输出 |
| `decision: block` | 官方描述可阻止提示词，进程与后续事件行为待测 | 阻止后 `num_turns=0`、零模型调用；headless `-p` 约 6 秒内 exit 0；**不触发 `Stop`**；`SessionEnd` 照常到达 | `block_prompt` 只对已验证的 `2.1.224` 宣告 capability；计时与状态逻辑不能假设每个 prompt 必有配对的 `Stop`；与 Codex 被阻止后 30 秒不退出相反，headless probe 的等待策略必须按宿主区分 |
| 宿主专有字段 | 原文未列出 | `Stop` 携带 `background_tasks` 与 `session_crons` | 解析器容忍未知与宿主专有字段，不据此拒绝事件 |
| hook command 的解析环境 | 原文未指明 Windows 下的命令解析方式 | command 由 POSIX 风格 shell 解析，反斜杠路径被吞掉并导致 hook 静默失败 | 安装器生成的 hook 命令在 Windows 上必须使用正斜杠路径；安装验证以一次真实事件到达为最终判据 |
| 探针可观察性 | 原文未给出 headless 观测手段 | `--print` 模式的 `--include-hook-events` 会在 stream-json 中输出 `hook_started`/`hook_response` 记录 | capability probe 与安装验证可同时读取 recorder 产物和 stream-json 观测记录，交叉确认 hook 已执行 |

这轮实测触发了开发状态中预设的改变方向条件：Claude Code 的真实 payload 与 Codex 不同构（`turn_id` 对 `prompt_id`、`model` 缺失、阻止后进程行为相反），因此两宿主保留各自的 translator 与宿主 payload struct，只有经两组真实 fixture 证明稳定相同的校验辅助逻辑才考虑抽取。与 Codex 一节相同，这次实测确认的是特定版本、平台与运行方式；解析器接受未知新增字段，对缺少真正必需字段或未知事件静默 fail open，capability 由版本化 probe 或显式配置声明。

仍未验证的 Claude Code 行为包括：交互界面在 prompt 被阻止后的输入保留、被阻止 prompt 的 transcript 表现、无损重试、`additionalContext` 注入、Stop continuation、工具事件字段与时序，以及实际 hook timeout 下限。这些能力在验证前不能进入 Claude Code V1 的可承诺能力清单。

### 9.4 WorkBuddy 特点

- WorkBuddy 5.3.5 加入了扩展插件 Hook；
- 当前公开的详细 hook 契约位于 CodeBuddy Code 文档，并被官方标记为 Beta；
- 支持 `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`Stop`、`SessionEnd` 等第一版所需事件；
- 插件可以打包 hooks、Skills 与 MCP servers，并兼容 `.workbuddy-plugin` manifest 目录；
- Windows command hook 强制使用 Git Bash，脚本不能依赖 PowerShell 或 cmd 语法；
- 发布前需要在目标 WorkBuddy Desktop 版本实测提示词阻止、Stop continuation、MCP 启动与恢复行为。

因此 WorkBuddy 可以进入第一版架构，同时应作为独立 Beta 适配器隔离。核心状态机和协议不能依赖其私有字段。

### 9.5 宿主版本对 hook 的暴露（打包 hook 的版本来源）

**证据范围**：2026-08-13 调查"分发插件里的 hook 如何在运行时、低成本地拿到宿主版本"。Claude Code 侧依据官方 hooks 文档、`internal/adapters/claudecode/testdata/v2.1.224/` 真机 fixture，以及 2026-08-14 的真机安装探针校正（`manual-probes/claude-plugin-install/`，Claude Code `2.1.228` / Windows）；Codex 侧依据官方 tag `rust-v0.146.0` 源码核查与 2026-08-16 的 plugin-bundled hook 真机安装探针。结论按宿主、平台与安装方式版本化，不外推为永久契约。

| 议题 | Claude Code | Codex 0.146.0 |
| --- | --- | --- |
| hook stdin JSON 是否含版本 | **否**（CONFIRMED）。字段为 session_id / prompt_id / transcript_path / cwd / permission_mode / hook_event_name / effort / agent_id / agent_type 等，无 version | **否**（CONFIRMED，源码 `codex-rs/hooks/src/schema.rs` `UserPromptSubmitCommandInput` / `SessionStartCommandInput`）。UserPromptSubmit 为 session_id / turn_id / transcript_path / cwd / hook_event_name / model / permission_mode / prompt，无 version |
| 是否注入版本环境变量 | **文档称有、真机为空**：官方文档列 `CLAUDE_CODE_VERSION` 对 hook 可用，但 2026-08-14 真机安装探针（`2.1.228` / Windows / SessionStart）实测 hook 进程内 `$env:CLAUDE_CODE_VERSION` **为空**、该 env 未注入；`${CLAUDE_PLUGIN_ROOT}` 则确实可用（阻止文案已验证） | **否**（CONFIRMED）。`.codex/hooks.json` 的 config 层 hook 注入 env 为空（`engine/discovery.rs`）；plugin-local hook 仅得 `PLUGIN_ROOT`/`CLAUDE_PLUGIN_ROOT`/`PLUGIN_DATA`/`CLAUDE_PLUGIN_DATA`；无 `CODEX_VERSION`/`CODEX_CLI_VERSION` |
| 最省的免子进程版本源 | **无可用准确源**：`-host-version "$CLAUDE_CODE_VERSION"` 的命令串 `$VAR` 展开**已验证成立**（探针得空值而非字面量 `$CLAUDE_CODE_VERSION`），但该 env 本身为空，故拿不到真值；stdin 也无版本。Claude Code 遥测记 `"unknown"`（Master 2026-08-14 接受，后续再想办法） | 读 `$CODEX_MANAGED_PACKAGE_ROOT/package.json` 的 `version`（npm 托管形态真机最终事件 CONFIRMED 为 `0.146.0`；hook 继承自 JS 启动器 `codex-cli/bin/codex.js`，无子进程、随升级自动更新）。独立 binary 绕过启动器且无该 env；当前 V1 wrapper 传空值，真机最终事件 CONFIRMED 回落 `"unknown"`；安装时固化 `codex --version` 可作为后续可选增强 |

**避坑**：Codex 的 `~/.codex/version.json` 是更新检查缓存（`latest_version`/`last_checked_at`，`tui/src/updates.rs`），本机现为 `0.147.0` 而实际 CLI 为 `0.146.0`——**不是当前版本源**。转录首条 `session_meta.cli_version`（`rollout/src/recorder.rs`）在 resume 时不刷新（`meta: None`），只对新会话可靠，不能当主源。

**对实现的影响**：`HealthEvent.HostVersion` 仅遥测、无行为分支（capability 目前由 hookcli 硬编码声明，不按版本推）。因此两 translator 的 `NewTranslator` 已把空 host-version 缺省为 `"unknown"`，避免"缺版本 → 构造失败 → 静默 fail-open 关掉守卫"这一健壮性问题；准确版本按上表按宿主 best-effort 填。**这个 guard 放宽在真机是承重设计**：2026-08-14 探针中 Claude Code 的 `$CLAUDE_CODE_VERSION` 为空，正是因为空版本缺省为 `"unknown"`、`NewTranslator` 才成功、暂停守卫才在真实 TUI 生效（`hello` 被拦）——否则空版本会 fail-open 关掉守卫。Codex 侧 2026-08-16 的最终事件元数据确认 npm launcher 三事件均为 `0.146.0`；direct native binary 缺失 env 时三事件均为 `"unknown"`，两侧暂停守卫行为不变。

## 10. Hook、指令、Skill、Plugin、MCP 和 Daemon 的区别

| 机制 | 主要作用 | 是否确定性执行 | 是否理解当前对话 | 在本方案中的职责 |
| --- | --- | --- | --- | --- |
| `AGENTS.md` / `CLAUDE.md` | 给模型长期行为指令 | 否，模型可能理解偏差 | 是 | 告诉 Agent 如何评估紧急性和写提醒 |
| Skill | 可复用任务流程和知识 | 由 Agent 选择或触发 | 是 | 提供健康评估方法、文案规则 |
| Hook | 生命周期事件上的机械执行 | 较强 | 事件字段有限，可注入给模型 | 计时、检查、阻断、强制评估 |
| Plugin | 打包 hooks、skills、MCP、脚本 | 取决于内部组件 | 取决于内部组件 | 安装和分发 Walkout |
| MCP Server | 给 Agent 提供结构化工具 | 工具调用是确定接口 | Agent 调用前拥有上下文 | `health.assess`、`health.status`、`health.break` |
| Daemon | 跨进程保存和协调状态 | 是 | 默认只知道传入的结构化数据 | 全局计时、债务、恢复、并发；摄像头为可选实验 |
| App Server / Agent SDK | 自建或深度控制 Agent 宿主 | 最强 | 可访问完整事件流 | 未来实现运行中中断、草稿恢复和统一 UI |

一个优秀实现需要组合这些机制：

```text
指令定义“如何判断和表达”
Hook 保证“何时一定检查”
MCP 传递“Agent 对上下文的结构化判断”
Daemon 决定“规则上应该提醒、延期还是锁定”
Plugin 负责“让用户安装这一整套能力”
```

## 11. 上下文感知如何实现

### 11.1 为什么 hook 脚本不应自己解析整份 transcript

直接读取 transcript 看起来简单，会带来几个问题：

- 文件格式不是稳定接口；
- 脚本需要重新理解自然语言，可能需要调用额外模型；
- 原始对话可能含有合同与案件内容、财务数据、未发布设计、代码、密钥、客户信息和事故细节；
- 多个 Agent 和 subagent 的对话边界难以统一；
- 容易把健康组件变成一个隐蔽的工作监控系统。

当前主 Agent 已经理解对话。更合理的路径是让它提交结构化评估。

### 11.2 health.assess MCP tool

当计时接近阈值时，hook 向 Agent 注入指令，要求调用：

```json
{
  "name": "health.assess",
  "arguments": {
    "work_mode": "calculation",
    "urgency": "critical",
    "interruptibility": "finish_current_step",
    "breakpoint_kind": "after_external_commit",
    "estimated_minutes": 3,
    "reason_code": "irreversible_operation",
    "context_sensitive": true,
    "safe_breakpoint_label": "当前账务批次提交后",
    "reminder_seed": "当前账务批次提交后提醒离席"
  }
}
```

字段含义：

- `work_mode`：写作、审阅、核对、设计、研究、实时沟通、软件工作等交互形态；
- `urgency`：工作的现实紧急程度；
- `interruptibility`：此刻是否安全中断；
- `breakpoint_kind`：下一个自然停顿点的结构化类别；
- `estimated_minutes`：预计到安全停顿点的时间；
- `reason_code`：外部截止时间、实时会话、不可逆操作、事故处理等结构化原因，避免持久化敏感全文；
- `context_sensitive`：提醒是否应隐藏具体任务信息；
- `safe_breakpoint_label`：可选的脱敏展示文本；
- `reminder_seed`：可选的脱敏短文案线索。

前六项是 daemon 可以用于策略的结构化字段；`safe_breakpoint_label` 与 `reminder_seed` 只用于显示或帮助当前 Agent 表达，不能批准延期、解除暂停、清除债务或完成恢复。

### 11.3 Agent 判断，Daemon 裁决

Agent 的判断提供语义信息，最终延期规则由 daemon 确定执行：

```text
Agent：当前账务批次正在提交，预计 3 分钟后安全暂停
    ↓
walkoutd：允许 5 分钟保护窗口，本日已使用 0 次
    ↓
Agent：向用户说明保护窗口和下一次检查点
```

这里的 daemon 是确定性状态与策略核心，不是第二个自然语言模型。当前主 Agent 已经读过对话，负责把自然语言上下文转换成枚举、数值、布尔值和脱敏展示素材；daemon 只校验 schema、读取当前状态与历史额度，并根据版本化规则生成 `HealthDecision`。相同配置、状态和结构化输入必须得到相同决定。

`HealthDecision` 是内部统一协议，不是某个宿主的原生 JSON：

```json
{
  "schema_version": "1.0",
  "decision_id": "uuid",
  "source_event_id": "uuid",
  "state_revision": 42,
  "state": "overtime",
  "action": "allow | inject_reminder | pause_prompt | fail_open",
  "reason_code": "break_due",
  "intervention_id": "uuid-or-null",
  "protected_until": null,
  "next_check_at": "RFC3339-timestamp-or-null"
}
```

适配器再把统一 action 映射为各宿主实际支持的 stdout JSON、上下文注入或退出码。daemon 不需要理解自由文本；如果必需的结构化字段缺失，它应拒绝评估或使用保守默认值，而不是猜测自然语言含义。daemon 不可达时，适配器可以合成 `action=fail_open`，这属于传输故障降级，不是 daemon 的业务裁决。

### 11.4 最近交互窗口与多个并行 session

Agent 能运行不代表用户仍在电脑前。V1 不要求用户显式声明离开或返回，而是把所有 session 中可确认的人类交互合并为一条时间线。

每个人类交互事件 `hᵢ` 打开 `[hᵢ, hᵢ + interaction_window_minutes]` 窗口；默认窗口为 5 分钟，重叠窗口取并集。连续一个完整窗口没有新的人类交互，人的计时自动暂停。Agent 的 tool、Stop、输出和完成通知只更新 runtime，不能续期窗口。

daemon 保存最近交互时间和对应 session。健康提醒使用全局唯一的 `intervention_id`，交给最近发生人类交互的 session 展示；其他 session 只同步状态。如果所有窗口已经到期，提醒等待下一次用户交互。`walkout` 是用户级状态，而紧急 `protected_window` 绑定具体 session，不能让其他工作一起绕过暂停。

窗口长度由可配置参数 `interaction_window_minutes` 控制，默认 5 分钟。较短会漏算阅读和审阅时间，较长会在用户离开后继续计时；后续可以用录制事件比较 3、5、10 分钟并调整默认值。它只能推断用户“可能已经离开或不再参与”，不能证明人体离席。

这样可以减少两类风险：

- 完全依赖固定计时，导致在关键操作中机械打断；
- 完全依赖模型，导致 Agent 把每个任务都判断为“不适合休息”。

## 12. 健康提醒的完整事件链

### 12.1 普通软提醒

```text
1. SessionStart：连接 walkoutd
2. UserPromptSubmit：登记用户活动
3. Pre/PostToolUse：持续更新活动时间
4. 连续活跃时间达到 45 分钟，状态进入 overtime
5. 之后每条 UserPromptSubmit 照常放行，walkoutd 返回 `action=inject_reminder`
6. 注入内容指示 Agent 结合当前工作语境自行措辞提醒（语气随债务加深递进），并指向 /walkout:done
7. 若用户在消息中以自然语言表示已经活动，Agent 直接执行捆绑的 walkout-ctl done 并确认
8.（可选增强）Agent 提交过 health.assess 时，提醒措辞可利用安全停顿点信息
```

示例：

> 设计稿正在导出，正好够你去接杯水。别坐在这里用眼神给进度条加速。

### 12.2 紧急工作保护窗口

```text
1. 用户正在处理法定提交、实时客户会话、不可逆财务操作、演示或生产事故
2. 健康阈值到达
3. Agent 调用 health.assess：critical + finish_current_step
4. walkoutd 根据规则批准 10 分钟保护窗口
5. Agent 在对话中承认现实紧急性并说明下一检查点
6. 提交、会话、操作或恢复完成后，PostToolUse 更新 `breakpoint_kind`
7. 下一次 Stop 强制生成离席提醒
```

示例：

> 当前批次还没有提交完成，这一刻确实不适合离开。我先保留 5 分钟；提交完成后，你和报表都该喘口气。

### 12.3 服务暂停与恢复

```text
1. 健康债务达到上限
2. 当前 turn 先完成到可恢复状态
3. Stop hook 要求 Agent 预告下一条请求将锁定
4. 下一次 UserPromptSubmit 检测到 walkout（Agent 罢工）
5. 健康控制命令：放行
6. 普通工作提示词：阻止，阻止文案直接给出命令入口
7. 用户起身活动后以命令反馈已活动（/walkout:done、$walkout:done 或 !walkout-ctl done；
   阻断态下自然语言到不了模型，只有命令有效）
8. 产生 `activity_confirmed`；可选实验可在本地检测离开画面 20 秒
9. walkoutd 不校验休息时长，原子清零本轮连续工作时间与健康债务并回到 working
10. Codex、Claude Code 或 WorkBuddy 的下一条普通提示词恢复处理
```

## 13. 为什么先完成当前 turn，再暂停下一步

在任何知识工作中，任意时刻强制中断都可能造成：

- 合同条款、报告或设计稿只修改一半；
- 账务批次、渲染、上传、迁移或部署处于中间状态；
- Agent 还没有告诉用户执行结果；
- 用户不知道从哪里恢复；
- 外部沟通尚未完成，或工具子进程仍在后台运行。

因此需要区分：

- **提醒到期**：健康计时已经达到阈值；
- **安全停顿点**：当前工作可以离开而不增加丢失工作、错误提交、对外沟通或系统运行风险；
- **暂停生效**：Agent 完成必要收尾后不再接受下一项工作。

硬暂停之前，Agent 应输出一个最小恢复检查点：

- 已完成什么；
- 尚未完成什么；
- 有哪些提交、导出、通信或进程仍在运行；
- 回来后建议执行什么；
- 工作材料和外部系统是否处于安全状态。

这会把健康干预从粗暴断电，变成一次带恢复说明的任务交接。

## 14. 状态机

跨 Agent 状态严格沿用 `product-spec.md`（2026-08-26 三态简化裁定）：

| 状态 | 含义 | 允许的 Agent 行为 |
| --- | --- | --- |
| `working` | 正常活跃工作 | 全部允许，累计时间 |
| `overtime` | 连续工作达到 45 分钟仍在参与 | 普通请求照常放行，每条附带递进提醒；实际参与的超时时间累计为健康债务 |
| `walkout` | 健康债务达到上限，Agent 罢工 | 只允许健康控制、安全收尾和紧急出口；用户命令确认已活动后清零回 `working` |

`idle`、`due`、`protected_window` 和 `lock_pending` 是运行期标志或限时 lease，不新增为产品状态。状态必须由 daemon 原子更新。多个 Codex、Claude Code、WorkBuddy 和 subagent 同时上报事件时，不能重复累计工作时间或互相覆盖暂停状态。

## 15. 幂等、并发与失败策略

### 15.1 Idempotency：幂等

同一事件被重复处理，结果应保持一致。例如同一个 `turn_id` 的 Stop hook 重试两次，只能产生一次提醒。

推荐事件键：

```text
provider + session_id + turn_id + hook_event_name + tool_use_id
```

### 15.2 Concurrency：并发

多个匹配 hooks 可能并行运行；多个 Agent 会话也可能同时写状态。`walkoutd` 应采用：

- SQLite transaction 或等价原子存储；
- 每个事件唯一键；
- Agent runtime 与人的参与区间分开存储；
- 多 session 的人类参与区间取并集，不按会话相加；
- tool heartbeat 不能直接增加人的时间；
- 每个 intervention 使用全局唯一 ID 和一个 reminder owner；
- 明确的状态版本号或 compare-and-swap。

### 15.3 Timeout：超时

Hook 位于用户交互路径上，运行太慢会直接让 Agent 显得卡顿。

建议预算：

- 本地状态查询：目标 10–30 ms；
- `UserPromptSubmit` 总 hook 时间：目标低于 100 ms；
- 不在同步 hook 中启动摄像头模型或下载依赖；
- 摄像头验证由 daemon 异步管理；
- 上下文语义判断利用当前 Agent，不再额外调用一个远程模型。

Claude Code 的 command `UserPromptSubmit` hook 超时后会取消 hook，并继续处理提示词而不采用该 hook 的上下文。Codex 的配置超时以秒为单位，多数 hook 缺省为 600 秒，`SessionEnd` 缺省为 1 秒且最大为 3 秒；这些宿主级超时只能作为粗粒度兜底。Walkout 的同步 IPC 调用使用 75 ms 默认端到端 deadline，并在解析失败、daemon 不可用、schema 不兼容或超时时静默 **fail open**。Codex 各事件的宿主失败/超时细节仍需按目标版本验证。

### 15.4 Crash Recovery：崩溃恢复

客户端或 daemon 崩溃后：

- 不能把无人使用的时间计为连续工作；
- 应根据最后一个人类交互窗口关闭推断参与区间，Agent tool heartbeat 不能延长它；
- 暂停状态可以保留，但要有本地恢复和诊断命令；
- 摄像头实验进程如果存在，必须释放设备句柄；
- 不应要求用户删除数据库才能恢复 Agent。

## 16. 能力边界与诚实表述

### 16.1 Hooks 无法保证任意时刻插话

如果 Agent 正在执行一个长时间、没有工具边界的模型请求，普通 hook 只能等待下一个生命周期节点。

更强的实时控制需要：

- Codex App Server 的事件流和 `turn/interrupt`；
- Claude Agent SDK callback；
- 或一个由我们控制的统一 Agent host。

第一版使用 turn 边界干预、tool 事件只做活动心跳，已经足以验证产品价值，并且对工作安全更友好。

### 16.2 Hooks 不是不可绕过的强制策略

个人电脑上的用户可以：

- 禁用 hooks；
- 禁用插件；
- 结束 daemon；
- 启动未安装健康适配器的 Agent；
- 使用绕过 hook trust 的特殊启动方式。

企业可以通过 managed hooks 和设备策略提高强制性，个人产品应把它定位为用户主动选择的 commitment device（自我承诺工具）。

### 16.3 上下文紧急性判断会误判

模型可能：

- 把普通 deadline 夸大成严重事故；
- 低估真实风险；
- 错误估计任务剩余时间；
- 在上下文不足时做出自信判断。

因此采用：

- Agent 提交语义判断；
- daemon 执行确定性上限；
- 用户保留明确的紧急出口；
- 所有自动延期具有过期时间；
- 提醒用户“这是工作节奏判断，不是医学建议”。

### 16.4 摄像头只能确认离席

人体存在检测可以确认用户是否离开工作位置。它无法可靠证明：

- 用户喝了水；
- 用户进行了有效运动；
- 用户的身体状态已经改善；
- 用户没有使用照片或其他方式绕过检测。

基础产品目标应表述为“完成一次恢复确认”。摄像头实验可以额外表述为“检测到离开画面”，喝水和具体活动只作为行为建议出现。

摄像头在 V1 中是 bounded spike，不是基础版本发布门槛。无摄像头路径必须始终存在，并明确它只能证明用户完成了确认流程。

## 17. 推荐的第一版技术范围

### 阶段 A：状态核心

- `walkoutd` 本地进程；
- SQLite 状态和事件表；
- `walkout status`、`skip`、`break`、`confirm-recovery`、`emergency` 命令；
- Agent runtime 与人的参与时间分离，多 session 的人类交互窗口取并集；
- 人类交互窗口可配置，默认 5 分钟；
- 不接入摄像头，先用用户反馈“已休息”的显式完成命令验证流程；确认后不校验休息时长，直接清零本轮连续工作时间与健康债务。

### 阶段 B：Codex 适配器

- plugin 打包 `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`Stop`、`SessionEnd`；
- MCP tools：`health.status`、`health.assess`、`health.confirm_activity`、`health.emergency_continue`；
- 上下文提醒和下一条普通提示词暂停；
- 项目信任与 hook-definition trust 分层说明；
- 用真实事件到达验证安装，不以配置文件存在或 headless 阻断后的进程退出作为成功条件；
- 对未经 probe 验证的上下文注入、Stop continuation 与工具事件保持 capability 降级。

### 阶段 C：Claude Code 适配器

- 复用相同 daemon 和 MCP schema；
- 映射 Claude Code hooks；
- 可选 `terminalSequence` 通知；
- 验证被阻止提示词后的输入框、transcript、重试、超时行为和 Stop continuation 的实际 UI。

### 阶段 D：WorkBuddy 适配器

- 复用相同 daemon、统一事件协议和 MCP schema；
- 映射 WorkBuddy / CodeBuddy hooks；
- Windows hook runner 使用 Git Bash 兼容脚本；
- 启动时执行版本检查与 capability probe；
- 验证 WorkBuddy Desktop 中插件安装、提示词阻止后的输入框与 transcript、Stop continuation、MCP 与恢复行为。

### 阶段 E：可选本地离席验证实验

- 复用最小本地页面或命令入口，不开发完整系统托盘；
- 摄像头按次授权；
- 本地人体存在检测；
- 连续离开计时；
- 无图像上传、录制或身份识别；
- 无摄像头和无障碍替代路径；
- 实验失败或未完成不阻塞基础版本发布。

### 阶段 F：深度宿主集成

只有在 hooks MVP 已验证行为价值后，再考虑：

- Codex App Server；
- Claude Agent SDK；
- WorkBuddy 后续稳定宿主接口；
- 运行中 turn 中断；
- 用户草稿保存和解锁后自动恢复；
- 统一跨 Agent 的原生 UI。

## 18. 最小验收标准

第一版达到以下条件即可证明方案成立：

1. Codex、Claude Code 与 WorkBuddy 同时工作时，连续工作时间不会重复累计；
2. 使用默认 5 分钟窗口时，所有 session 连续 5 分钟没有人类交互后，Agent 可以继续工作而人的计时暂停；
3. 达到阈值后的第一个安全 hook 边界只产生一次全局对话内提醒；
4. 提醒能够引用 reminder owner 当前工作的脱敏上下文；
5. 法定提交、实时客户会话、不可逆操作和生产事故等场景可以获得绑定 session 的有限保护窗口；
6. 当前 turn 会先形成可恢复结果，再进入暂停；
7. 暂停后下一条普通提示词会被拦截，V1 不要求逐工具阻断；
8. 健康控制命令和紧急出口保持可用；
9. 用户反馈“已休息”后，daemon 清零本轮连续工作时间与健康债务，原会话可以继续；
10. daemon 故障不会导致用户永久失去 Agent；
11. 默认不持久化原始聊天、摄像头帧或键入内容；
12. 摄像头实验失败或未完成不会阻塞基础版本发布。

## 19. 术语速查表

| 英文术语 | 中文解释 |
| --- | --- |
| Agent | 由模型、指令、工具和循环控制构成的智能体 |
| Agent host | 承载 Agent、工具、权限、UI 与会话的客户端程序 |
| Agent loop | 模型判断、工具执行、结果返回、继续判断的循环 |
| Session | 一段可启动、恢复和结束的 Agent 会话 |
| Thread | 一段包含多个 turn 的对话线程 |
| Turn | 从一次用户请求到 Agent 完成该请求的工作轮次 |
| Model request | 宿主把上下文发给模型并获得下一步输出的一次请求 |
| Tool call / tool use | 模型请求宿主执行某个工具 |
| Hook | 在生命周期节点触发扩展逻辑的机制 |
| Hook event | 触发 hook 的生命周期事件 |
| Matcher | 用于筛选事件、工具名或启动来源的匹配规则 |
| Handler | 事件匹配后真正执行的脚本或服务 |
| stdin / stdout / stderr | 进程的标准输入、标准输出和错误输出 |
| Exit code | 本地进程返回给宿主的数字状态 |
| additionalContext | Hook 注入给模型的额外上下文 |
| systemMessage | Hook 显示给用户的系统警告或状态消息 |
| decision: block | 阻止当前提示词、工具或停止动作的结构化决定 |
| Stop hook | Agent 准备结束当前 turn 时触发的 hook |
| Transcript | 宿主保存的对话和事件记录 |
| MCP | Agent 调用外部结构化工具和数据的协议 |
| Daemon | 在本机后台持续运行、跨会话协调状态的进程 |
| Idempotency | 同一事件重复处理不会产生重复副作用 |
| Fail open | 组件故障时保留主要服务能力 |
| Fail closed | 组件故障时阻止主要动作 |
| Safe breakpoint | 当前任务可以安全离开和恢复的检查点 |
| Protected window | 因真实紧急工作而获得的有限延期窗口 |
| Commitment device | 用户主动采用、帮助约束自身行为的机制 |

## 20. 官方资料

- [OpenAI Codex Hooks](https://learn.chatgpt.com/docs/hooks.md)
- [OpenAI Codex App Server](https://learn.chatgpt.com/docs/app-server.md)
- [OpenAI Codex Manual](https://developers.openai.com/codex/codex-manual.md)
- [Claude Code Hooks Reference](https://code.claude.com/docs/en/hooks)
- [Claude Code Plugins Reference](https://code.claude.com/docs/en/plugins-reference)
- [WorkBuddy 更新日志](https://www.workbuddy.cn/docs/workbuddy/Changelog)
- [CodeBuddy Hooks Reference](https://www.codebuddy.ai/docs/cli/hooks)
- [CodeBuddy Plugin Reference](https://www.codebuddy.ai/docs/cli/plugins-reference)
