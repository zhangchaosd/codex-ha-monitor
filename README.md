# Codex HA Monitor

[![CI](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/ci.yml)
[![Agent Release](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/release-agent.yml/badge.svg)](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/release-agent.yml)

[English](#english) | [简体中文](#简体中文)

## English

Codex HA Monitor exposes Codex Desktop and CLI activity to Home Assistant over the local network. It is designed around the way an AI-agent user works: one top-level workflow may have several concurrent subagents, attention requests must remain visible while other tasks keep running, and important transitions must be automatable rather than inferred from a frequently changing text sensor.

The repository contains:

- `agent/`: a Go service that runs on every computer using Codex.
- `custom_components/codex_monitor/`: a Home Assistant local-push integration.

### What it provides

- Exact or inferred per-thread states: `RUNNING`, `WAITING_APPROVAL`, `WAITING_INPUT`, `IDLE`, `ERROR`, and `UNKNOWN`.
- Root-workflow and active-worker counts, including Codex subagent parent/root relationships.
- Independent running, attention-required, and failure facts. One approval no longer hides another running worker.
- Codex and agent versions, token usage, rate limits, Hooks, source, confidence, and staleness.
- Authenticated SSE snapshots plus replayable, sequenced task events.
- Zeroconf discovery with stable installation identity and manual URL configuration as a fallback.
- Optional actions to approve/reject a request, submit requested input, or interrupt an exact turn.

```mermaid
flowchart LR
    C["Codex Desktop / CLI"] --> A["Go monitor agent"]
    F["Session files"] --> A
    H["Codex Hooks"] --> A
    A -->|"JSON + replayable SSE"| I["Home Assistant integration"]
    I --> E["Entities / task events / automations"]
    I -->|"Exact request or turn action"| A
```

### Screenshots

Home Assistant device, entity, event, and diagnostic views:

![Codex Monitor device and entities in Home Assistant](docs/images/home-assistant-device.png)

Codex Monitor entities arranged on a Home Assistant dashboard:

![Codex Monitor Home Assistant dashboard](docs/images/home-assistant-dashboard.png)

### Control boundary

The agent starts its own Codex App Server connection. A request is actionable only when that request arrived on this connection; it is then reported with `controllable: true` and a `request_id`. Existing Codex Desktop windows normally use a separate App Server process. Their session files can still be monitored, including multiple concurrent threads, but Desktop-owned approval prompts cannot be answered through the agent and are reported as non-controllable. The API rejects stale, mismatched, and non-controllable requests instead of guessing.

### Install and run the agent

Download a binary from [GitHub Releases](https://github.com/zhangchaosd/codex-ha-monitor/releases), or build it:

```bash
cd agent
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

The default listener is `[::]:8765`; mDNS advertises `_codex-monitor._tcp.local.`. Verify it with:

```bash
curl -H 'Authorization: Bearer replace-with-a-long-random-token' \
  http://[::1]:8765/api/v1/version
curl -H 'Authorization: Bearer replace-with-a-long-random-token' \
  http://[::1]:8765/api/v1/status
```

See [agent/README.md](agent/README.md) for flags and Hook setup.

### Agent API and protocol

The current schema is `1.1`. API responses are UTF-8 JSON, timestamps are RFC 3339, and API routes require `Authorization: Bearer <token>`. Clients must ignore unknown fields and must not treat `UNKNOWN` as `IDLE`.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/` | Built-in status page. |
| `GET` | `/healthz`, `/readyz` | Liveness and source readiness. |
| `GET` | `/api/v1/version` | Schema, stable installation ID, agent and Codex versions. |
| `GET` | `/api/v1/status` | Host, aggregate states, connectivity, Hooks, usage summary, limits, and pending requests. |
| `GET` | `/api/v1/threads?limit=100` | Per-thread state, hierarchy, request ID, and controllability. |
| `GET` | `/api/v1/requests` | Pending approval/input requests. |
| `GET` | `/api/v1/usage?days=30` | Bounded usage history. |
| `GET` | `/api/v1/rate-limits` | Primary and secondary rate-limit windows. |
| `GET` | `/api/v1/events` | SSE `snapshot` and replayable `task_activity` events. |
| `POST` | `/api/v1/hooks/codex` | Native Codex Hook receiver. |
| `POST` | `/api/v1/actions/approve` | Approve one exact pending request. |
| `POST` | `/api/v1/actions/reject` | Reject one exact pending request. |
| `POST` | `/api/v1/actions/submit-input` | Answer one exact input request. |
| `POST` | `/api/v1/actions/interrupt` | Interrupt one exact thread and turn. |

SSE task events use increasing numeric IDs. Reconnect with `Last-Event-ID`; the agent replays retained events before sending the current snapshot. Timestamp-only reconciliations are deduplicated, and daily usage history remains on `/usage` rather than bloating each live snapshot:

```bash
curl -N -H 'Authorization: Bearer replace-with-a-long-random-token' \
  -H 'Last-Event-ID: 41' http://[::1]:8765/api/v1/events
```

Task event types are `task_started`, `task_completed`, `approval_required`, `input_required`, `task_failed`, `task_interrupted`, `task_resumed`, and `agent_recovered`. The in-memory replay window retains the latest 256 task events.

Example approval body:

```json
{
  "request_id": "req-42",
  "thread_id": "thread-id",
  "turn_id": "turn-id",
  "for_session": false
}
```

The [client contract](docs/agent-integration-contract.md) and [OpenAPI document](docs/agent-openapi.yaml) are the protocol source of truth.

### Install the Home Assistant integration

With HACS, add `https://github.com/zhangchaosd/codex-ha-monitor` as a custom integration repository, install **Codex Monitor**, and restart Home Assistant. For manual installation, run `./install.sh`; it copies `custom_components/codex_monitor` into `/config/custom_components/`. Set `HA_CONFIG_DIR` only when your Home Assistant configuration directory is somewhere other than `/config`.

Then open **Settings → Devices & services → Add integration → Codex Monitor**. A discovered agent can be selected automatically, or enter a LAN URL such as `http://192.168.1.20:8765` and the token. `127.0.0.1` works only when Home Assistant shares the agent's network namespace.

The integration uses SSE for live updates and a 60-second reconciliation poll by default. The fallback interval is configurable from 5 to 300 seconds.

### Home Assistant model

One agent installation is one HA device. Default entities include workload state, current task, active workflows, connection state, Codex/agent versions, primary rate limit, running, attention required, task problem, and a `task_activity` event entity. Worker and per-state counters, usage details, Hooks, secondary limits, and stale data are available as disabled-by-default diagnostic entities.

#### HomeKit Bridge and Desktop attention limits

- [HomeKit Bridge](https://www.home-assistant.io/integrations/homekit/) does not expose arbitrary Home Assistant entities. With its default filtering, Codex Monitor normally contributes only the three enabled, non-diagnostic binary sensors: **Running**, **Attention required**, and **Task problem**. Apple Home may render them as generic occupancy sensors. Text, enum, counter, version, timestamp, and standalone `task_activity` event entities have no matching HomeKit accessory type; diagnostic entities remain in Home Assistant and are excluded by default.
- **Attention required** turns on only when the agent observes `WAITING_APPROVAL` or `WAITING_INPUT`. Codex Desktop normally owns a separate App Server, while saved session files cannot reliably identify approval/input waits. Without configured and trusted [Codex Hooks](https://developers.openai.com/codex/config-advanced#hooks), Desktop prompts may therefore remain invisible and this sensor may stay off. A completed task waiting for the next ordinary prompt is not considered attention-required. See [Enable Codex Hooks](agent/README.md#enable-codex-hooks) for the optional setup.

The event entity is the preferred automation trigger. Four HA actions are registered:

- `codex_monitor.approve_request`
- `codex_monitor.reject_request`
- `codex_monitor.submit_input`
- `codex_monitor.interrupt_turn`

Each action targets a Codex Monitor device and requires the exact IDs from the task event/current-task attributes. See the [HA architecture](docs/ha-integration-architecture.md) and the included [attention notification blueprint](blueprints/automation/codex_monitor_attention.yaml).

### Development and release

```bash
cd agent
go test -race ./...
go vet ./...
go build ./cmd/cma

cd ..
uv run --isolated --python 3.13 \
  --with pytest-homeassistant-custom-component --with ruff pytest -q
uv run --isolated --python 3.13 \
  --with pytest-homeassistant-custom-component --with ruff ruff check .
```

Push an `agent-v*` tag or run `release-agent.yml` manually to cross-compile macOS, Linux, and Windows archives and publish them with GitHub CLI.

License: [MIT](LICENSE)

---

## 简体中文

Codex HA Monitor 把局域网内 Codex Desktop/CLI 的运行状态接入 Home Assistant。设计以 AI Agent 使用者为中心：一个根工作流可以同时运行多个子代理；等待批准不能遮蔽仍在运行的其他任务；关键状态变化应当成为可自动化的事件，而不是依赖高频轮询一个文本传感器。

仓库包含运行在 Codex 电脑上的 Go 代理，以及 `custom_components/codex_monitor` Home Assistant 本地推送集成。

### 主要能力

- 按会话展示运行、等待批准、等待输入、空闲、失败和未知状态。
- 区分根工作流与活动 worker，并保留子代理的父/根会话关系。
- 独立表达“正在运行”“需要处理”“任务失败”，多会话不会互相覆盖。
- 展示 Codex/代理版本、Token 用量、限额、Hook、来源、置信度和数据陈旧状态。
- 使用带重放序号的 SSE 推送任务事件，断线后自动恢复，并用低频轮询校准。
- 通过 Zeroconf 自动发现，也支持手动填写 URL。
- 对可控请求执行批准、拒绝、提交输入和精确中断 turn。

设备、实体、事件、诊断信息和仪表盘效果见英文部分的 [Screenshots](#screenshots)。

### 控制能力边界

代理会启动自己的 Codex App Server。只有通过这条连接到达代理的请求才会标记为 `controllable: true`，并提供 `request_id`。Codex Desktop 通常使用另一个独立 App Server 进程，因此代理能通过会话文件监控 Desktop 的多个并发任务，但不能替 Desktop 回答它持有的批准弹窗。不可控、已过期或 ID 不匹配的操作会被明确拒绝，不会猜测目标。

### 安装代理

从 [Releases](https://github.com/zhangchaosd/codex-ha-monitor/releases) 下载二进制，或从源码构建：

```bash
cd agent
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

默认监听 `[::]:8765`，并发布 `_codex-monitor._tcp.local.` mDNS 服务。详细参数和 Hook 配置见 [agent/README.md](agent/README.md)。

### 接口与协议

当前 Schema 为 `1.1`。API 使用 UTF-8 JSON 和 RFC 3339 时间；所有 API 路由都要求 `Authorization: Bearer <token>`。主要接口包括版本、状态、会话、待处理请求、用量、限额、SSE、Codex Hook，以及批准、拒绝、输入和中断操作。完整字段和错误语义见 [客户端契约](docs/agent-integration-contract.md) 与 [OpenAPI](docs/agent-openapi.yaml)。

`/api/v1/events` 推送 `snapshot` 和带递增 ID 的 `task_activity`。客户端重连时发送 `Last-Event-ID`，代理会重放内存中保留的最近 256 个任务事件。事件类型包括任务开始/完成/失败/中断/恢复、需要批准、需要输入和代理恢复。

### 安装 Home Assistant 集成

在 HACS 中把 `https://github.com/zhangchaosd/codex-ha-monitor` 添加为自定义“集成”仓库，安装 **Codex Monitor** 并重启；手动安装时运行 `./install.sh`，它会把 `custom_components/codex_monitor` 复制到 `/config/custom_components/`。仅当 HA 配置目录不在 `/config` 时才需要设置 `HA_CONFIG_DIR`。

打开“设置 → 设备与服务 → 添加集成 → Codex Monitor”。可以选择自动发现的代理，也可以填写局域网 URL 和 Token。集成使用 SSE 实时更新，默认每 60 秒轮询校准一次，可调整为 5–300 秒。

每个代理安装对应一个 HA 设备。默认实体覆盖工作负载状态、当前任务、活动工作流、连接、版本、主限额、运行、需要处理、任务异常和 `task_activity` 事件；worker 数量、各状态计数、用量、Hook、次限额和陈旧状态作为默认关闭的诊断实体提供。

#### HomeKit Bridge 与 Desktop 待处理状态限制

- [HomeKit Bridge](https://www.home-assistant.io/integrations/homekit/) 不能映射任意 HA 实体。使用默认过滤规则时，Codex Monitor 通常只会向 Apple 家庭提供三个已启用、非诊断类的二进制感应器：**正在运行**、**需要处理**和**任务异常**；Apple 家庭可能把它们显示为普通占用感应器。文本、枚举、计数、版本、时间戳以及独立的 `task_activity` 事件没有对应的 HomeKit 配件类型；诊断实体仍保留在 HA 中，并且默认不桥接。
- **需要处理**仅在代理观察到 `WAITING_APPROVAL` 或 `WAITING_INPUT` 时打开。Codex Desktop 通常使用独立的 App Server，而会话文件无法可靠区分等待批准或输入。没有配置并信任 [Codex Hooks](https://developers.openai.com/codex/config-advanced#hooks) 时，Desktop 提示可能对代理不可见，因而该感应器可能保持关闭。任务已经完成、只是等待下一条普通提示词，不属于“需要处理”。可选配置方法见 [启用 Codex Hooks](agent/README.md#enable-codex-hooks)。

集成注册四个 Action：`approve_request`、`reject_request`、`submit_input`、`interrupt_turn`。每次操作都需要事件或当前任务属性中的精确 ID。设计细节见 [HA 架构文档](docs/ha-integration-architecture.md)，通知示例见 [自动化蓝图](blueprints/automation/codex_monitor_attention.yaml)。

本项目使用 [MIT License](LICENSE)。
