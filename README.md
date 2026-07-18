# Codex HA Monitor

[![CI](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/ci.yml/badge.svg)](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/ci.yml)
[![Agent Release](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/release-agent.yml/badge.svg)](https://github.com/zhangchaosd/codex-ha-monitor/actions/workflows/release-agent.yml)

[English](#english) | [简体中文](#简体中文)

## English

Expose the runtime state of Codex Desktop and Codex CLI to Home Assistant over a local network.

This repository contains two components:

- `agent/`: a read-only Go monitoring agent that runs on the Codex computer.
- `custom_components/codex_monitor/`: a Home Assistant custom integration.

The current release is read-only and does not provide approval or control operations. The agent requires a bearer token for every API request.

### Features

- Reports whether Codex is running, idle, waiting for approval, waiting for input, or in an error state.
- Reports the current task, active task count, and app-server connection state.
- Reports the Codex CLI version, agent version, token usage, and rate-limit reset time.
- Combines Codex Hooks, Codex app-server data, and session-filesystem inference.
- Provides Home Assistant UI configuration, multiple hosts, English/Chinese translations, and diagnostics.
- Uses GitHub Actions to test the project and publish agent binaries for macOS, Linux, and Windows.

### Architecture

```mermaid
flowchart LR
    C["Codex Desktop / CLI"] --> A["Go Monitor Agent"]
    F["Codex session files"] --> A
    H["Codex Hooks"] --> A
    A -->|"LAN HTTP API"| I["Home Assistant integration"]
    I --> E["Entities / automations / dashboards"]
```

### 1. Install the agent

Download the archive for your platform from [Releases](https://github.com/zhangchaosd/codex-ha-monitor/releases), extract it, and run:

```bash
chmod +x codex-monitor-agent
./codex-monitor-agent --token 'replace-with-a-long-random-token'
```

Or build it from source:

```bash
cd agent
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

Verify the service:

```bash
curl -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/healthz
curl -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/api/v1/status
```

The agent listens on `[::]:8765` by default. See [`agent/README.md`](agent/README.md) for Hook setup and command-line options, and see the [AI/client integration contract](docs/agent-integration-contract.md) plus [OpenAPI description](docs/agent-openapi.yaml) when building another client.

### Agent API and protocols

The agent exposes a versioned HTTP API. Every data endpoint requires `Authorization: Bearer <token>`. Normal API responses use UTF-8 JSON; timestamps use RFC 3339; workload states use uppercase values such as `RUNNING`, `WAITING_APPROVAL`, `WAITING_INPUT`, `IDLE`, `ERROR`, and `UNKNOWN`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Built-in HTML status dashboard. |
| `GET` | `/healthz` | Process liveness, agent version, and uptime. |
| `GET` | `/readyz` | Snapshot, app-server, filesystem, and Hook readiness details. |
| `GET` | `/api/v1/version` | Schema version, stable installation ID, agent version, Codex CLI version, and app-server details. |
| `GET` | `/api/v1/status` | Current host, connection, workload summary, Hooks, usage, and rate limits. The thread list is omitted. |
| `GET` | `/api/v1/threads?limit=100` | Recent Codex threads. `limit` accepts `1`–`200` and defaults to `100`. |
| `GET` | `/api/v1/usage` | Token usage summary and daily buckets when available. |
| `GET` | `/api/v1/rate-limits` | Primary/secondary rate-limit windows and reset information when available. |
| `GET` | `/api/v1/events` | Server-Sent Events stream. Each update is an `event: snapshot` carrying a full JSON snapshot in `data:`. |
| `POST` | `/api/v1/hooks/codex` | Receives a native Codex Hook JSON payload, up to 1 MiB, and returns the derived task state. |

The JSON snapshot schema is currently `1.0`. A snapshot contains a stable `installation_id`, generation time, host and version information, Codex connectivity, state provenance/confidence, workload counts, thread data, Hook activity, usage, and rate-limit data. Consumers should ignore unknown fields for forward compatibility.

Stream live updates with SSE:

```bash
curl -N -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/api/v1/events
```

Forward a Hook event:

```bash
curl -X POST http://127.0.0.1:8765/api/v1/hooks/codex \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer replace-with-a-long-random-token' \
  -d '{
    "session_id": "example-session",
    "turn_id": "example-turn",
    "cwd": "/path/to/project",
    "hook_event_name": "PermissionRequest",
    "tool_name": "Bash"
  }'
```

`session_id` and `hook_event_name` are required. Invalid Hook payloads return HTTP `400`; unsupported methods return `405`. The project does not currently expose a WebSocket or a control/approval API.

### 2. Install the Home Assistant integration

#### HACS

1. Add `https://github.com/zhangchaosd/codex-ha-monitor` as a custom integration repository in HACS.
2. Install **Codex Monitor**.
3. Restart Home Assistant.

#### Manual installation

Copy [`custom_components/codex_monitor`](custom_components/codex_monitor) into Home Assistant's `/config/custom_components/` directory and restart Home Assistant.

### 3. Configure Home Assistant

1. Open **Settings → Devices & services → Add integration**.
2. Search for **Codex Monitor**.
3. Enter the agent's LAN URL, for example `http://[fd00::20]:8765`, and the API token passed with `--token`.

Each agent installation creates one device using its stable `installation_id`. Home Assistant polls every 5 seconds by default; the interval can be changed to 5–300 seconds in the integration options.

`127.0.0.1` works only when Home Assistant and the agent share the same network namespace. Home Assistant OS, Docker, and separate-host installations normally need the Codex computer's LAN IP address.

### Home Assistant entities

Default entities include workload state, current task, active task count, Codex connection, Codex/agent versions, lifetime tokens, rate-limit usage/reset, running state, and attention required.

Known task count, usage streak, secondary rate limit, Hook count, and stale-data state are disabled by default and can be enabled from the device page. Full thread JSON is included only in diagnostics to avoid frequent large Recorder entries.

See [`docs/ha-integration-architecture.md`](docs/ha-integration-architecture.md) for the HA design. For third-party clients and AI-assisted development, use the [integration contract](docs/agent-integration-contract.md) and [OpenAPI description](docs/agent-openapi.yaml) as the source of truth.

### Development

Agent:

```bash
cd agent
go test ./...
go vet ./...
go build ./cmd/cma
```

Home Assistant integration:

```bash
uv run --python 3.13 --with aiohttp --with pytest --with pytest-asyncio pytest -q
uvx ruff check .
uvx ruff format --check .
```

### Publishing agent binaries

`.github/workflows/release-agent.yml` uses GitHub CLI to create or update a release. Push an `agent-v*` tag or run the workflow manually:

```bash
gh workflow run release-agent.yml -f tag=agent-v0.3.0
```

The workflow validates the tag against the source version, cross-compiles all supported targets, creates archives and `SHA256SUMS.txt`, and publishes them with `gh release create` or `gh release upload`.

### License

[MIT](LICENSE)

---

## 简体中文

在局域网中把 Codex Desktop/CLI 的运行状态接入 Home Assistant。

本仓库包含两个组件：

- `agent/`：运行在 Codex 电脑上的 Go 只读监控代理。
- `custom_components/codex_monitor/`：Home Assistant 自定义集成。

当前版本只读取状态，不提供批准或控制操作。代理的每个 API 请求都必须携带 Bearer Token。

## 功能

- 展示 Codex 是否运行、空闲、等待批准、等待输入或发生错误。
- 展示当前任务、活动任务数和连接状态。
- 展示 Codex CLI 版本、代理版本、Token 用量和限额重置时间。
- 同时支持 Hook、Codex app-server 和会话文件系统数据源。
- Home Assistant UI 配置、多主机、中文/英文翻译和诊断下载。
- GitHub Actions 自动测试并发布 macOS、Linux 和 Windows 代理二进制。

## 架构

```mermaid
flowchart LR
    C["Codex Desktop / CLI"] --> A["Go Monitor Agent"]
    F["Codex 会话文件"] --> A
    H["Codex Hook"] --> A
    A -->|"LAN HTTP API"| I["Home Assistant Integration"]
    I --> E["传感器 / 自动化 / 仪表盘"]
```

## 1. 安装代理

从 [Releases](https://github.com/zhangchaosd/codex-ha-monitor/releases) 下载对应平台的压缩包，解压后运行：

```bash
chmod +x codex-monitor-agent
./codex-monitor-agent --token 'replace-with-a-long-random-token'
```

也可以从源码构建：

```bash
cd agent
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

验证接口：

```bash
curl -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/healthz
curl -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/api/v1/status
```

代理默认监听 `[::]:8765`。Hook 配置和更多参数见 [`agent/README.md`](agent/README.md)。第三方程序或 AI 对接请以 [对接契约](docs/agent-integration-contract.md) 和 [OpenAPI 描述](docs/agent-openapi.yaml) 为准。

### 代理接口与协议

代理提供使用 Bearer Token 认证的版本化 HTTP API。所有数据接口必须携带 `Authorization: Bearer <token>`。普通接口返回 UTF-8 JSON，时间使用 RFC 3339，任务状态使用 `RUNNING`、`WAITING_APPROVAL`、`WAITING_INPUT`、`IDLE`、`ERROR` 和 `UNKNOWN` 等大写枚举值。

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/` | 内置 HTML 状态页面。 |
| `GET` | `/healthz` | 进程健康、代理版本和运行时长。 |
| `GET` | `/readyz` | 快照、app-server、文件系统和 Hook 就绪信息。 |
| `GET` | `/api/v1/version` | Schema、安装 ID、代理版本、Codex CLI 版本及 app-server 信息。 |
| `GET` | `/api/v1/status` | 主机、连接、工作摘要、Hook、用量和限额；不包含任务数组。 |
| `GET` | `/api/v1/threads?limit=100` | 最近任务，`limit` 范围为 `1`–`200`，默认 `100`。 |
| `GET` | `/api/v1/usage` | Token 用量摘要和每日用量桶。 |
| `GET` | `/api/v1/rate-limits` | 主/次限额窗口及重置时间。 |
| `GET` | `/api/v1/events` | SSE 实时流；事件名为 `snapshot`，`data:` 携带完整 JSON 快照。 |
| `POST` | `/api/v1/hooks/codex` | 接收最大 1 MiB 的 Codex 原生 Hook JSON，并返回推导出的任务状态。 |

当前快照 Schema 为 `1.0`。Hook 请求必须包含 `session_id` 和 `hook_event_name`；无效请求返回 HTTP `400`，不支持的方法返回 `405`。当前没有 WebSocket、批准或其他控制接口。

SSE 和 Hook 示例：

```bash
curl -N -H 'Authorization: Bearer replace-with-a-long-random-token' http://[::1]:8765/api/v1/events

curl -X POST http://127.0.0.1:8765/api/v1/hooks/codex \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer replace-with-a-long-random-token' \
  -d '{"session_id":"example-session","hook_event_name":"PermissionRequest"}'
```

## 2. 安装 Home Assistant 集成

### HACS

1. 在 HACS 中添加自定义存储库：`https://github.com/zhangchaosd/codex-ha-monitor`。
2. 类别选择“集成”。
3. 安装 **Codex Monitor** 并重启 Home Assistant。

### 手动安装

把 [`custom_components/codex_monitor`](custom_components/codex_monitor) 复制到 Home Assistant 的 `/config/custom_components/`，然后重启 Home Assistant。

## 3. 配置 Home Assistant

1. 打开“设置 → 设备与服务 → 添加集成”。
2. 搜索 **Codex Monitor** 或 **Codex 监控**。
3. 输入代理的局域网 URL（例如 `http://[fd00::20]:8765`）以及启动代理时通过 `--token` 传入的 API Token。

每套代理安装根据稳定的 `installation_id` 创建一个设备。默认每 5 秒更新，可在集成选项中调整到 5–300 秒。

`127.0.0.1` 只有在 Home Assistant 和代理处于同一网络命名空间时才有效；Home Assistant OS、Docker 或独立主机通常需要填写 Codex 电脑的局域网 IP。

## Home Assistant 实体

默认实体包括：

- 工作状态、当前任务、活动任务数
- Codex 连接状态和连接二元传感器
- Codex/代理版本
- 累计 Token、限额已用比例和重置时间
- 运行中、需要处理二元传感器

已知任务数、连续使用天数、次级限额、Hook 计数和数据过期状态默认禁用，可在设备页面手动启用。完整任务 JSON 只出现在诊断下载中，避免 Home Assistant Recorder 频繁保存大块属性。

HA 集成的详细设计见 [`docs/ha-integration-architecture.md`](docs/ha-integration-architecture.md)，代理规格见 [`docs/agent-spec-v1.2.zh-CN.md`](docs/agent-spec-v1.2.zh-CN.md)。

## 开发

代理：

```bash
cd agent
go test ./...
go vet ./...
go build ./cmd/cma
```

HA 集成：

```bash
uv run --python 3.13 --with aiohttp --with pytest --with pytest-asyncio pytest -q
uvx ruff check .
uvx ruff format --check .
```

## 发布代理二进制

`.github/workflows/release-agent.yml` 使用 GitHub CLI 创建 Release。推送 `agent-v*` 标签会自动交叉编译并上传所有平台制品；也可以手动运行工作流并输入标签：

```bash
gh workflow run release-agent.yml -f tag=agent-v0.3.0
```

工作流会验证标签版本与代理源码版本一致，生成压缩包、`SHA256SUMS.txt`，并通过 `gh release create` 或 `gh release upload` 发布。

## License

[MIT](LICENSE)
