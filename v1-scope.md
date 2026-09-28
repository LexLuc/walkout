# Walkout V1 Scope

> **AI 辅助与人工审核声明**：本文由 AI 辅助起草，并由产品负责人进行人工审核。涉及健康建议、隐私政策和正式发布时，还应完成相应的专业复核。

本文定义第一版的交付边界。V1 基础版的目的只有一个：**验证知识工作者在使用 AI Agent 工作时，对话内的上下文提醒、有限延期和服务暂停，能否促使用户开始并完成一次休息确认流程。** 可选摄像头实验进一步验证确认流程是否对应真实离开画面；没有该实验数据时，产品不能宣称提高了真实离席率。

> **发布基线裁定（Master 2026-08-25，2026 年 8 月 25 日）**：V1 发布基线为 **Codex 与 Claude Code 两宿主**。WorkBuddy 是两宿主发布并稳定使用后再考虑的增强项；本地摄像头验证移出开发主线，作为 V1.x 可选优化（见 §3）。下文出现"三宿主"处均按此裁定读作"两宿主 + WorkBuddy 延后增强"。
>
> **三态简化裁定（Master 2026-08-26，2026 年 8 月 26 日）**：用户可见状态收敛为 `working → overtime → walkout`（见 `product-spec.md` §3）。显式"延后 10 分钟"动作与两步恢复确认取消：不理会提醒即自然进入债务累计，用户反馈"已活动"单步清零回 working。下文涉及"延后/开始恢复/确认完成"的表述按此裁定读。

第一版支持 Codex 与 Claude Code（WorkBuddy 为发布后增强项），覆盖法律、会计与财务、设计、研究、运营、咨询、行政、管理和软件开发等知识工作。职业范围广，功能范围必须窄。

## 0. 开发就绪结论

当前产品机制已经足够开始 MVP 开发。第一阶段应冻结 `HealthEvent`、`HealthDecision`、`health.assess`、配置 schema 和七态状态机，并实现带假时钟的 daemon 与宿主 capability probe；不需要继续扩展产品功能或完善 Web 原型才能开工。

在统一协议和录制 hook fixtures 冻结前，三个宿主适配器不并行实现业务规则。Codex、Claude Code 与 WorkBuddy 的输入保留、transcript、阻止、重试、超时和 Stop continuation 行为以目标版本实测为准；WorkBuddy 在通过桌面端验收前保持 Beta 支持等级。

## 1. V1 必须闭环

```text
Agent 会话开始
  → 统一计时达到 45 分钟，进入 overtime
  → 之后每条普通请求照常放行并附带上下文提醒（语气随债务加深递进）
  → 不理会提醒继续参与即隐式延后，实际参与时间累计为健康债务
  → 债务达到 120 分钟进入 walkout，暂停下一条普通请求
  → 用户起身活动并反馈"已活动"（单步确认）
  → 清零本轮连续工作时间与健康债务，回到 working
  → 恢复原会话
```

只有直接影响这条链的能力才是 V1 发布项。

## 2. 必须交付

| 能力 | V1 边界 | 保留原因 |
| --- | --- | --- |
| 宿主适配 | Codex 与 Claude Code 各一个薄适配器（WorkBuddy 延后为增强项） | 用户已明确要求兼容多宿主 |
| 共享健康核心 | 单个本地 `walkoutd`、七态状态机、SQLite 最小状态 | 避免多 Agent 重复计时与规则漂移 |
| 生命周期接入 | `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`Stop`、`SessionEnd` | 六个宿主共有事件足以形成稳定边界 |
| 活动计算 | Agent runtime 与人的参与时间分开；多 session 的人类参与区间取并集 | 防止后台 Agent 和并行 session 虚增久坐时间 |
| 上下文评估 | 当前主 Agent 调用一次 `health.assess` | 这是比普通通知更有情商的核心差异 |
| 提醒升级 | 温和、毒舌、债务、暂停；语气由有限模板和 Agent 上下文组合 | 直接验证产品假设 |
| 暂停策略 | 当前 turn 安全结束后，只阻止下一条普通用户请求 | 实现简单，避免破坏工作结果 |
| 健康控制 | 状态、单步活动确认、紧急继续 | 保证用户始终有出口 |
| 最小本地事件 | 时间、状态迁移、选择、低敏 `work_mode` | 支撑实验指标和故障恢复 |
| 故障降级 | daemon 或适配器异常时 fail-open | 防止健康组件锁死工作 |

### 2.1 V1 不做逐工具阻断

`PreToolUse` 和 `PostToolUse` 在 V1 只承担活动心跳与诊断，不执行跨行业工具语义分类，也不阻止单个工具。

Tool heartbeat 只能更新 `agent_runtime`，永远不能续期人的连续工作计时。人的计时只由可确认的人类交互事件打开可配置窗口，默认 5 分钟。

暂停只在当前 turn 已经完成后生效，由下一次 `UserPromptSubmit` 阻止新工作。这样可以自然允许当前合同修改、账务批次、设计导出、外部提交、构建或回滚完成到可恢复状态，无需维护一套容易误判的工具 allowlist。

健康控制通过独立命令或 MCP 工具保持可用，不依赖启动新的普通 Agent turn。

### 2.2 V1 上下文评估最小字段

只保留：

```json
{
  "work_mode": "drafting | review | calculation | design | research | live_communication | software_work | other",
  "urgency": "normal | critical",
  "interruptibility": "safe_now | finish_current_step",
  "breakpoint_kind": "now | after_current_step | after_external_commit | after_live_session | other",
  "estimated_minutes": 3,
  "reason_code": "ordinary_work | external_deadline | live_session | irreversible_operation | incident_response | presentation | accessibility | personal_safety",
  "context_sensitive": true,
  "safe_breakpoint_label": "可选的脱敏短描述",
  "reminder_seed": "脱敏的短文案线索"
}
```

`work_mode`、`urgency`、`interruptibility`、`breakpoint_kind`、`estimated_minutes`、`reason_code` 和 `context_sensitive` 是策略字段。`safe_breakpoint_label` 与 `reminder_seed` 只是可选的展示素材，不参与延期、暂停或恢复裁决。

V1 不引入独立上下文分类模型、第二个提醒 Agent、职业专用规则引擎或内容审核模型。当前主 Agent 负责理解自然语言并提交结构化评估；daemon 不解析自然语言语义，只校验 schema 并根据版本化确定性策略限制延期、累计债务和迁移状态。

### 2.3 V1 必须处理脱手运行和并行 session

- 每次 `UserPromptSubmit` 或宿主明确标记为用户本人完成的交互，在用户级时间线上打开 `interaction_window_minutes` 窗口，默认 5 分钟；
- 所有 session 的窗口取并集，重叠时间只计算一次；
- 连续一个完整窗口没有任何人类交互后，人的计时自动暂停，Agent 可以继续运行；
- 窗口到期只暂停累计，不自动清零连续工作时间或减少健康债务；
- `SessionStart`、工具事件、Stop、后台任务和完成通知不能打开或延长人的窗口；
- 下一次任意 session 的人类交互自动恢复计时，无需用户声明离开或返回；
- daemon 为每次提醒分配全局 `intervention_id`，只由 `last_human_session_id` 展示完整提醒；
- 没有人类交互窗口时，提醒排队到用户再次交互，不能污染后台 Agent 的完成消息；
- `walkout` 是全局状态；紧急保护窗口绑定具体 `session_id`，不能顺便解锁其他工作。

窗口长度由可配置参数 `interaction_window_minutes` 控制，默认 5 分钟。V1 可以继续用事件回放比较 3、5、10 分钟候选值并调整默认值。该机制只能推断“用户可能已离开或不再参与”，不能证明人体离席；长时间阅读也可能被误判为空闲。

### 2.4 V1 活动确认

- 用户反馈“已活动”产生 `activity_confirmed`（单步，无需先声明开始休息），立即清零本轮连续工作时间与健康债务并回到 working；
- 确认入口：宿主内命令（三态下均有效）与自然语言反馈（仅非阻断的 overtime 期，由主 Agent 识别后代为执行命令）；
- V1 不统计或校验休息时长，也不根据无交互窗口自动完成确认；
- 后续版本可以增加喝水、深蹲、原地快走、颈部拉伸、看向远处或自定义活动，而不改变 `activity_confirmed` 的状态语义；
- 所有活动都必须允许跳过或替换，并提供无障碍选项；产品不能把用户确认表述为已经客观完成动作或获得健康改善。

## 3. 有预算上限的实验

以下能力可以做 bounded spike，但不阻塞 V1 发布：

| 实验 | 允许范围 | Go / No-Go |
| --- | --- | --- |
| 本地摄像头离席验证 | 单机、按次授权、只输出 present/absent/uncertain；不录制、不上传、不识别身份 | 只有在目标平台上稳定，并明显优于无摄像头替代路径时进入正式 V1.x |
| 自然停顿点增强 | 利用 `PostToolUse` 的工具完成事件提供提示线索 | 只做通用完成信号，不为法律、财务、设计和开发分别造规则引擎 |
| 桌面通知 | 宿主已有通知能力的一层薄映射 | 不开发独立通知中心或系统托盘 |

摄像头实验保留 `20 秒离开画面` 参数。基础产品必须始终提供无摄像头恢复路径，并如实说明该路径只能证明用户执行了确认流程，不能证明离席或喝水。

因此基础版的主要指标是“提醒后的休息流程开始率与完成率”；“真实离席率”只使用摄像头实验或其他能够支持该结论的信号计算。

## 4. 明确排除

### 4.1 宿主与平台

- Codex、Claude Code、WorkBuddy 之外的其他 Agent；
- Codex App Server、Claude Agent SDK 或 WorkBuddy 私有深度接口；
- 模型流式生成中的任意时刻强行插话；
- 宿主专属的复杂 UI 增强；
- 第一轮验证平台之外的安装包。V1 首个验证平台为 Windows 11 x64，协议和脚本保持可移植，macOS 与 Linux 不作为发布门槛。

### 4.2 活动与上下文

- 系统级键鼠监控、应用窗口监控、屏幕内容识别；
- 日历、会议、邮件、IM 或工时系统连接器；
- 读取和解析完整 transcript；
- 自动识别用户职业、客户、案件、项目或具体应用；
- 为每个行业建立单独的紧急性模型；
- 跨设备、手机和可穿戴设备同步。

### 4.3 干预与验证

- 逐工具副作用分类和强制阻断；
- 不可绕过的电脑锁定、网络拦截或进程终止；
- 自动判断用户喝了多少水；
- 人脸识别、身份确认、姿势诊断或健康风险预测；
- 语音助手、虚拟形象、复杂动画和游戏化积分；
- 无限自定义毒舌人格或开放式提示词市场。

### 4.4 产品与运营

- 原生系统托盘、完整设置应用和数据仪表盘；
- 云端账号、云同步、团队排行榜或企业管理后台；
- 管理员强制策略、绩效报告或员工监控；
- 付费、订阅、推荐系统和增长体系；
- 医疗建议、诊断或疗效声明。

## 5. 最小技术形态

```text
两个插件目录（WorkBuddy 延后）
    └─ 共用一个 hook runner
          ├─ 上报六类事件
          └─ 查询一次本地状态

一个 walkoutd 进程
    ├─ state_snapshot
    ├─ append-only events
    ├─ timer / debt / policy
    └─ Health MCP / CLI
```

各适配器向 daemon 发送统一 `HealthEvent`，daemon 返回统一 `HealthDecision`。正常决定只能从版本化确定性策略产生；daemon 不可达时，适配器可以合成 `fail_open`。适配器负责把 `allow`、`require_assessment`、`inject_reminder`、`pause_prompt` 或 `fail_open` 映射为各宿主的 hook 输出。

SQLite 第一版只需要两类逻辑数据：

- `state_snapshot`：当前状态、deadline、债务、`last_human_interaction_at`、`last_human_session_id`、全局参与区间、revision；
- `events`：幂等键、宿主、session、Agent runtime、可靠人类交互、时间和低敏决定字段。

分析报表直接由事件导出生成，不建设本地 dashboard。数据保留策略仍遵循 `privacy-and-safety.md`。

## 6. V1 发布门槛

1. 两个发布宿主（Codex、Claude Code）都能完成会话登记、提醒、延后、暂停下一条请求和恢复；
2. 两个发布宿主同时运行时，活跃时间不会重复累计；
3. 两个 Agent 并行运行 30 分钟时，全局最多累计 30 分钟人的参与时间；
4. 使用默认 5 分钟窗口时，连续 5 分钟没有人类交互后，后台 Agent 工具事件不会继续增加人的时间；
5. 同一健康阈值只展示一个全局提醒；
6. 法律审阅、财务核对、设计导出和软件任务四类录制场景都能产生合理的安全停顿点；
7. 暂停不会破坏当前 turn 已生成的工作结果；
8. 健康控制、紧急继续和无障碍出口始终可用；
9. 默认不保存原始聊天、专业工作内容或摄像头帧；
10. daemon 故障时 Agent 继续工作；
11. Windows 11 x64 上完成两个发布宿主的安装、升级和卸载验证；
12. 摄像头实验失败或未完成不会阻止基础版本发布。
13. 用户确认“已休息”后，两个发布宿主都清零本轮连续工作时间与健康债务，不要求达到最低休息时长。

## 7. Scope 决策规则

新特性只有同时满足以下条件才进入 V1：

1. 直接影响“提醒后是否离席”或必要护栏指标；
2. 三个宿主可以用同一语义实现；
3. 不需要采集新的敏感数据类别；
4. 没有更简单的生命周期边界方案；
5. 有明确的失败降级路径。

如果一个特性主要提升技术完整度、管理能力或视觉精致度，而不能改变核心实验结论，则进入 V1.x 或后续版本。
