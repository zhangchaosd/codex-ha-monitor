# Codex 只读监控代理项目规格说明书

> **已归档、非规范文档。** 这是最初只读代理的需求基线，其中无认证、IPv4 默认监听、Schema 1.0、无 mDNS、无控制接口等内容均已被 Agent 0.4 / Schema 1.1 取代。请勿按本文实现新客户端；当前行为只以 [agent-integration-contract.md](agent-integration-contract.md) 与 [agent-openapi.yaml](agent-openapi.yaml) 为准。

- 项目代号：Codex Monitor Agent（CMA）
- 文档版本：1.2
- 日期：2026-07-17
- 文档范围：仅定义运行在 Codex 主机上的只读监控代理，不定义 Home Assistant 集成
- 目标实现语言：Go

## 1. 项目目标

CMA 是运行在 macOS、Linux 或 Windows Codex 主机上的单文件程序。它从本机 Codex App Server、Codex 生命周期 Hooks 和本机 `CODEX_HOME` 会话元数据读取状态，通过局域网 HTTP API 对外提供 Codex 版本、连接状态、任务状态、线程、Token 活动和额度信息。

首版目标：

1. 每个平台交付一个不依赖额外运行时的单文件二进制。
2. 默认监听所有网络接口，局域网设备知道 URL 后即可直接访问。
3. 所有接口均无需认证，不提供 TLS、访问控制、来源限制或速率限制。
4. 对外接口只读，不提供启动任务、发送消息、批准请求、终止任务或修改 Codex 状态的能力。
5. 优先通过共享 App Server 获取精确状态；无法附加到现有客户端时，通过按会话关联的 Codex Hooks 获取实时状态，并以 `CODEX_HOME` 会话元数据兜底。
6. 明确返回数据来源和可见范围，不把“看不到任务”错误解释为“Codex 空闲”。

## 2. 非目标

首版不包含：

- Home Assistant 自定义集成代码。
- Codex 远程控制、批准、拒绝或用户输入。
- 启动、暂停、取消、归档或删除 Codex 线程。
- 修改 Codex 配置、账户、文件或工作区。
- 完整对话和推理内容接口。
- 账户登录、Codex 安装或自动升级。

未来若需要批准或其他控制能力，应设计独立的控制代理和独立 API，不改变 CMA 的只读语义。

## 3. 可见性模型

当前 Codex Desktop、IDE 扩展和 CLI 可能各自启动独立的 stdio App Server。新启动的 App Server 可以读取历史线程，但不能自动获得其他进程内存中的实时状态。因此 CMA 必须返回 `visibility`，并按以下模式运行。

| visibility | 含义 | 状态精度 |
|---|---|---|
| `shared_app_server` | 连接到用户任务实际使用的共享 App Server | 精确，可获得线程状态通知和等待标记 |
| `agent_owned_app_server` | CMA 自己启动了一个 App Server | 仅能保证读取历史；不得用它判断其他客户端空闲 |
| `agent_owned_with_filesystem_fallback` | CMA 自己启动 App Server，同时用会话文件补充现有客户端活动 | 运行/完成状态为推断值，等待批准可能未知 |
| `filesystem_fallback` | 未连接 App Server，只读取本机会话元数据 | 推断状态，不提供账户额度 |
| `unavailable` | App Server、Hook 和文件系统来源均不可用 | 无状态数据 |

当 visibility 不是 `shared_app_server` 时：

- `IDLE` 只能表示某个可见线程明确完成，不能表示主机上没有其他任务。
- 没有发现活动线程时，主机工作负载状态返回 `UNKNOWN`，不能返回 `IDLE`。
- API 必须同时返回 `state_source` 和 `state_confidence`。
- `hooks.active_sessions > 0` 时，相关会话的实时状态由 Hook 覆盖；visibility 仍描述 App Server 可见范围，不与 Hook 可见范围混为一谈。

## 4. 总体架构

```text
Codex Desktop / IDE / CLI
          │
          ├── 可共享 App Server ─────────────┐
          │                                  │
          ├── 生命周期 Hooks ─────────────────┤
          │                                  │
          └── 独立 stdio App Server           │
                    │                         │
                    └─ CODEX_HOME 元数据 ─────┤
                                               ▼
                                    Codex Monitor Agent
                                      ├─ App Server Client
                                      ├─ Hook Receiver
                                      ├─ Filesystem Collector
                                      ├─ State Aggregator
                                      ├─ Snapshot Cache
                                      └─ HTTP + SSE Server
                                               │
                                               ▼
                              浏览器 / Home Assistant / 其他局域网设备
```

### 4.1 模块

| 模块 | 职责 |
|---|---|
| CLI | 参数解析、启动、版本输出和信号处理 |
| App Server Client | JSON-RPC 初始化、请求关联、通知接收和重连 |
| Hook Relay / Receiver | 原样转发生命周期 Hook JSON，按 `session_id` 和 `turn_id` 记录实时状态 |
| Filesystem Collector | 扫描 `CODEX_HOME/sessions` 和 `session_index.jsonl`，推断独立客户端活动 |
| State Aggregator | 合并 App Server、Hook 和文件系统三个数据源，生成连接状态和工作负载状态 |
| Snapshot Cache | 保存最近一次快照和更新时间 |
| HTTP API | 提供 JSON、HTML 状态页和 SSE 事件流 |
| Identity | 生成并持久化稳定的 `installation_id` |

## 5. App Server 接入

### 5.1 支持的接入方式

| 模式 | MVP | 说明 |
|---|---:|---|
| `auto` | 是 | 启动 stdio App Server，并同时启用文件系统兼容采集；后续版本可自动发现共享 Socket |
| `stdio` | 是 | CMA 启动 `codex app-server --stdio`，退出时回收子进程 |
| `ws://` / `wss://` | M2 | 连接显式 WebSocket URL |
| `unix://PATH` | M2 | 在 Unix Socket 上完成 WebSocket HTTP Upgrade |
| Windows Named Pipe | 后续 | 官方版本提供稳定端点后实现 |

### 5.2 传输帧格式

- stdio：每行一个 JSON-RPC 消息，即 JSONL。
- WebSocket：每个 text frame 一个 JSON-RPC 消息。
- Unix Socket：先进行 WebSocket HTTP Upgrade，再按 WebSocket text frame 传输。
- 协议消息省略标准 JSON-RPC 的 `jsonrpc: "2.0"` 字段。

### 5.3 初始化

连接建立后必须：

1. 发送 `initialize` 请求，并声明客户端名 `codex_monitor_agent`。
2. 保存响应中的 `userAgent`、`codexHome`、`platformFamily` 和 `platformOs`。
3. 发送 `initialized` 通知。
4. 执行首次线程、Token 和额度校准。

### 5.4 只读方法

CMA 只调用：

- `initialize`
- `thread/list`
- `thread/loaded/list`
- `thread/read`，仅在校准确实需要时调用
- `account/read`
- `account/usage/read`
- `account/rateLimits/read`

CMA 接收并处理：

- `thread/status/changed`
- `thread/tokenUsage/updated`
- `turn/started`
- `turn/completed`
- `serverRequest/resolved`
- `account/rateLimits/updated`
- 批准和用户输入类 server request，仅用于状态观察，不发送决定

CMA 不调用任何会改变状态的方法。

## 6. 文件系统兼容采集

### 6.1 数据来源

- `$CODEX_HOME/session_index.jsonl`：线程名称和更新时间索引。
- `$CODEX_HOME/sessions/**/*.jsonl`：会话元数据、事件类型和任务完成标记。

`CODEX_HOME` 默认使用 App Server initialize 返回值；没有该值时使用环境变量 `CODEX_HOME`，再回退到用户目录下的 `.codex`。

### 6.2 采集规则

每次只扫描最近更新的有限数量文件。读取会话文件时只需要：

- `session_meta.payload.id`
- `session_meta.payload.cwd`
- `session_meta.payload.originator`
- `session_meta.payload.cli_version`
- `session_meta.payload.source`
- 顶层 `timestamp`
- `event_msg.payload.type`

推断规则：

1. 最后一个 `task_complete` 之后仍有新事件，且最后活动未超过 `filesystem_active_window`，状态为 `RUNNING`。
2. 最新事件为 `task_complete`，状态为 `IDLE`。
3. 文件最近更新但无法识别事件，状态为 `UNKNOWN`。
4. 文件系统模式无法可靠区分批准类型，除非将来 Codex 持久化相应事件；不得猜测为 `WAITING_APPROVAL`。

## 7. Codex Hooks 实时采集

### 7.1 原生事件转发

Codex command hook 会从 stdin 收到原生 JSON。CMA 提供 `hook-forward` 子命令，将 stdin 原样转发至本机代理：

```text
codex-monitor-agent hook-forward http://127.0.0.1:8765/api/v1/hooks/codex
```

转发器必须满足：

- 不修改或丢弃 `session_id`、`turn_id`、`cwd`、`hook_event_name`、`tool_name` 等字段。
- HTTP 超时不超过 2 秒，转发失败也以成功状态退出，不能阻塞 Codex。
- stdout 保持为空，避免影响 Hook 输出协议。
- `print-hook-config` 输出可合并进 `~/.codex/hooks.json` 的配置，但 CMA 不自动修改 Codex 配置。

### 7.2 事件状态映射

| Hook 事件 | 状态 | 默认 TTL |
|---|---|---:|
| `PermissionRequest` | `WAITING_APPROVAL` | 5 分钟 |
| `Elicitation`（兼容事件） | `WAITING_INPUT` | 5 分钟 |
| `UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`PreCompact`、`PostCompact`、`SubagentStart`、`SubagentStop` | `RUNNING` | 10 分钟 |
| `Stop`、`SessionEnd`、`SessionStart` | `IDLE` | 60 秒 |

未知事件计入接收统计但不改变状态。TTL 到期后自动回落到 App Server 或文件系统状态。

### 7.3 会话关联和优先级

- `session_id` 是必填字段，用于关联 App Server thread ID 和会话文件 ID。
- `turn_id` 原样保留，供 Home Assistant 或未来控制层区分同一会话内的回合。
- 有效 Hook 状态覆盖相同会话的 App Server 校准和文件系统推断。
- Hook 仅覆盖对应会话，不创建全局 working/idle 开关。
- Hook 状态使用 `state_source=codex_hook` 和 `state_confidence=event_derived`。
- `hooks.received_events`、`hooks.active_sessions` 和 `hooks.last_event_at` 暴露采集健康度。

## 8. 状态模型

连接状态与工作负载状态必须分开。

### 8.1 connection_state

- `connected`
- `connecting`
- `disconnected`
- `error`

### 8.2 workload_state

- `RUNNING`
- `WAITING_APPROVAL`
- `WAITING_INPUT`
- `IDLE`
- `ERROR`
- `UNKNOWN`

### 8.3 状态来源

- `app_server_event`
- `app_server_reconcile`
- `codex_hook`
- `filesystem_inference`
- `none`

### 8.4 置信度

- `exact`：共享 App Server 的实时事件或校准结果。
- `event_derived`：最近一次 Hook 事件及其 TTL 推导出的当前状态。
- `inferred`：根据文件更新时间和 `task_complete` 推断。
- `unknown`：数据不足。

### 8.5 汇总优先级

仅在同一连接状态下汇总工作负载：

```text
ERROR > WAITING_APPROVAL > WAITING_INPUT > RUNNING > IDLE > UNKNOWN
```

上游断开不会覆盖最后工作负载状态，而是通过 `connection_state` 和 `stale` 单独表达。

## 9. 版本模型

CMA 必须同时获取：

- 代理自身版本。
- `codex --version` 输出和解析后的 Codex CLI 版本。
- App Server initialize 返回的 `userAgent`。
- 创建线程时记录的 `cliVersion`，作为线程字段返回。

Codex CLI 版本获取失败时返回 `null` 和错误文本，不影响代理 HTTP 服务启动。

## 10. HTTP API

### 10.1 通用约定

- 默认监听：`0.0.0.0:8765`。
- 无认证、无 TLS、无来源限制、无速率限制。
- 返回 `Access-Control-Allow-Origin: *`。
- JSON 基础路径：`/api/v1`。
- 时间使用 RFC 3339 UTC。
- 未知数值使用 `null`，不使用 `0` 代替未知。
- `schema_version` 首版为 `1.0`。

### 10.2 GET /healthz

表示 CMA HTTP 服务存活。

```json
{
  "status": "ok",
  "agent_version": "0.2.0",
  "uptime_seconds": 3600
}
```

### 10.3 GET /readyz

```json
{
  "ready": true,
  "snapshot_available": true,
  "app_server_connection": "connected",
  "filesystem_available": true,
  "hook_events_received": 12
}
```

### 10.4 GET /api/v1/version

新增的 Codex 版本接口：

```json
{
  "schema_version": "1.0",
  "installation_id": "5b4b1f4c-43b9-4a35-8cbf-e4d4e09b2e9a",
  "agent": {
    "version": "0.2.0",
    "go_version": "go1.26.5"
  },
  "codex_cli": {
    "binary": "/Applications/ChatGPT.app/Contents/Resources/codex",
    "raw": "codex-cli 0.144.5",
    "version": "0.144.5"
  },
  "app_server": {
    "user_agent": "Codex Desktop/0.144.5 (...)"
  }
}
```

### 10.5 GET /api/v1/status

```json
{
  "schema_version": "1.0",
  "generated_at": "2026-07-16T15:30:00Z",
  "stale": false,
  "installation_id": "5b4b1f4c-43b9-4a35-8cbf-e4d4e09b2e9a",
  "host": {
    "name": "mac-mini",
    "os": "darwin",
    "arch": "arm64"
  },
  "codex": {
    "connection_state": "connected",
    "visibility": "agent_owned_with_filesystem_fallback",
    "last_success_at": "2026-07-16T15:29:59Z"
  },
  "hooks": {
    "received_events": 12,
    "active_sessions": 1,
    "last_event_at": "2026-07-17T00:10:00Z"
  },
  "summary": {
    "workload_state": "RUNNING",
    "state_source": "codex_hook",
    "state_confidence": "event_derived",
    "known_threads": 8,
    "active_threads": 1,
    "states": {
      "running": 1,
      "waiting_approval": 0,
      "waiting_input": 0,
      "idle": 7,
      "error": 0,
      "unknown": 0
    }
  }
}
```

### 10.6 GET /api/v1/threads

返回最近线程摘要，可通过 `limit=1..200` 控制数量。

```json
{
  "schema_version": "1.0",
  "generated_at": "2026-07-16T15:30:00Z",
  "threads": [
    {
      "id": "019f...",
      "turn_id": "turn-...",
      "name": "开发 Codex 监控代理",
      "cwd": "/Users/me/workspace/project",
      "source": "vscode",
      "cli_version": "0.144.5",
      "state": "RUNNING",
      "state_source": "codex_hook",
      "state_confidence": "event_derived",
      "last_hook_event": "PreToolUse",
      "updated_at": "2026-07-16T15:29:59Z"
    }
  ]
}
```

### 10.7 GET /api/v1/usage

返回 `account/usage/read` 的数值摘要和每日桶。不可用时返回：

```json
{
  "availability": "unavailable",
  "summary": null,
  "daily_usage_buckets": null
}
```

### 10.8 GET /api/v1/rate-limits

返回 `rateLimitsByLimitId`；不存在时回退到 `rateLimits`。字段保留 Codex 服务端语义，不自行换算剩余 Token。

### 10.9 GET /api/v1/events

单向 Server-Sent Events 接口。客户端连接后立即收到一个 `snapshot`，之后每次快照变化继续发送：

```text
event: snapshot
data: {"schema_version":"1.0", ...}
```

### 10.10 POST /api/v1/hooks/codex

接收 Codex command hook 原生 JSON。至少要求：

```json
{
  "session_id": "019f...",
  "turn_id": "turn-...",
  "cwd": "/Users/me/project",
  "hook_event_name": "PermissionRequest",
  "tool_name": "Bash"
}
```

成功响应包含映射后的状态和接收时间。该接口不返回 Hook 决策，不批准或拒绝操作。

### 10.11 GET /

返回轻量只读 HTML 状态页，显示：

- 主机和安装 ID
- Agent 与 Codex 版本
- App Server 连接状态和 visibility
- 工作负载状态和置信度
- 活跃线程和最近线程
- Token 与额度

页面使用 `/api/v1/events`，SSE 不可用时每 5 秒轮询 `/api/v1/status`。

## 11. 命令行

```text
cma [PORT]
cma hook-forward [HOOK_URL]
cma print-hook-config [HOOK_URL]

--port 8765
--bind 0.0.0.0
--codex auto|stdio|ws://HOST:PORT|unix://PATH
--codex-bin codex
--codex-home PATH
--poll-interval 10s
--stale-after 30s
--filesystem-active-window 60s
--hook-running-ttl 10m
--hook-idle-ttl 1m
--hook-attention-ttl 5m
--max-threads 100
--version
```

优先级：命令行参数 > 环境变量 > 内置默认值。首版可以不实现配置文件。

## 12. 状态同步和容错

1. HTTP 服务应先启动，Codex 连接在后台完成。
2. App Server 连接失败后采用 1、2、4、8、16、30 秒指数退避。
3. App Server 重连后重新 initialize 并执行全量校准。
4. 默认每 10 秒校准线程、usage 和 rate limit。
5. 文件系统兼容采集默认每 2 秒扫描最近线程。
6. Hook 接收后立即更新对应会话快照并触发 SSE；TTL 到期后自动回落。
7. 上游不可用时保留最后快照，`stale=true`。
8. HTTP 查询请求只读取缓存，不同步等待 App Server。
9. 退出时关闭连接并回收 CMA 自己启动的 App Server 子进程。

## 13. 版本兼容

- JSON 解析忽略新增字段。
- 关键字段缺失时降级为 `UNKNOWN` 或 `null`。
- HTTP API 使用独立 `schema_version`。
- CI 使用目标 Codex CLI 生成 App Server JSON schema 并保存兼容快照。
- 每次 CMA 发布声明已验证的 Codex CLI 版本。

## 14. Go 项目结构

```text
codex-monitor-agent/
├── go.mod
├── cmd/cma/main.go
├── internal/
│   ├── appserver/
│   ├── collector/
│   ├── hookrelay/
│   ├── identity/
│   ├── model/
│   ├── monitor/
│   └── httpapi/
├── README.md
└── packaging/
    ├── systemd/
    ├── launchd/
    └── windows/
```

## 15. 测试要求

### 15.1 单元测试

- App Server 状态到规范状态的映射。
- connection_state 与 workload_state 独立变化。
- 文件系统 `task_complete` 和活动窗口推断。
- Hook 事件映射、TTL、`session_id`/`turn_id` 关联和过期回退。
- Hook 配置生成与原生 stdin JSON 无损转发。
- 多来源合并优先级。
- Codex 版本字符串解析。
- HTTP JSON schema 和 SSE 首事件。
- stale 和重连状态。

### 15.2 集成测试

- 模拟 stdio JSONL App Server。
- 模拟 WebSocket App Server。
- 初始化、线程列表、usage 和 rate limit 请求。
- 真实 HTTP Hook payload、线程状态覆盖和 SSE 更新。
- 连接中断和重连。
- 在真实 macOS Codex Desktop/IDE 环境中验证：
  - 新 App Server 无法把当前独立客户端误报为 IDLE。
  - 文件系统兼容采集能发现当前活跃会话。
  - `/api/v1/version` 返回本机 Codex `0.144.5` 或实际安装版本。

### 15.3 跨平台测试

- macOS arm64
- Linux x86_64、arm64
- Windows x86_64
- `go test ./...`
- `go vet ./...`
- 各目标平台交叉编译

## 16. 验收标准

1. `cma 8765` 启动后，局域网设备无需认证即可访问。
2. `/healthz` 在 2 秒内可用。
3. `/api/v1/version` 返回 Agent 和 Codex CLI 版本。
4. 连接共享 App Server 时能显示精确 `RUNNING`、`WAITING_APPROVAL`、`WAITING_INPUT`、`IDLE` 和 `ERROR`。
5. 当前 Codex 客户端使用独立 stdio App Server 时，不把不可见状态误报成 `IDLE`。
6. 文件系统兼容模式能检测最近活跃会话和 `task_complete`。
7. 原生和兼容 Hook payload 能按会话显示 `RUNNING`、`WAITING_APPROVAL`、`WAITING_INPUT` 和 `IDLE`。
8. Hook 转发失败不阻塞 Codex，Hook 过期后恢复其他数据源。
9. `/api/v1/events` 在状态变化时推送新快照。
10. App Server 断开后代理不退出，并自动重连。
11. `/api/v1/usage` 和 `/api/v1/rate-limits` 不可用时返回 `null`/availability，而不是虚构数值。
12. 程序退出后不残留其启动的 App Server 子进程。
13. `go test ./...` 和 `go vet ./...` 通过。

## 17. 首版里程碑

| 阶段 | 内容 |
|---|---|
| M0 | 本机协议验证、状态模型、版本接口和文件系统推断 |
| M1 | Go MVP：stdio/auto、HTTP API、SSE、HTML 页面、真实 Mac 测试 |
| M1.1 | 按会话关联的 Codex Hooks、配置生成、审批/输入实时状态 |
| M2 | WebSocket/Unix socket、mDNS、服务文件、CI 和跨平台构建 |
| M3 | 与 Home Assistant 集成联调 |

## 18. 当前已知限制

1. 代理无法附加到没有共享端点的既有 stdio App Server。
2. 文件系统兼容模式只能推断运行/完成，不能保证区分等待批准和等待输入。
3. Hooks 仅在用户配置并通过 `/hooks` 信任后运行；不支持的工具路径仍可能没有生命周期事件。
4. Hook 的事件在到达时真实，但持续状态由 TTL 推导，因此标记为 `event_derived`。
5. Codex App Server 接口仍可能随 Codex CLI 版本变化。
6. 账户 usage 和 rate limits 是账户范围数据，多台主机使用同一账户时不能在上层直接相加。
7. Windows 本地共享传输和 Hook 命令需要在目标平台继续验证。

## 19. 官方参考

- [Codex App Server](https://learn.chatgpt.com/docs/app-server)
- [Codex Hooks](https://learn.chatgpt.com/docs/hooks)
- [Codex developer commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli)
- [OpenAI Codex app-server source](https://github.com/openai/codex/tree/main/codex-rs/app-server)
