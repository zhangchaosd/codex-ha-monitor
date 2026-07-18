# Codex Monitor Agent

[English](#english) | [简体中文](#简体中文)

## English

Codex Monitor Agent (CMA) is a small Go service that exposes per-thread Codex activity to local-network clients. It combines three sources, in priority order:

1. Recent Codex Hook events for low-latency state.
2. The agent-owned Codex App Server for exact loaded-thread and account data.
3. Local `CODEX_HOME` session files for threads owned by Desktop, CLI, or IDE processes.

It keeps different `session_id`/thread IDs separate, preserves subagent parent/root relationships, and reports both active root workflows and active workers.

### Run

```bash
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

Useful flags:

```text
--bind ::                         HTTP bind address
--port 8765                      HTTP port
--token TOKEN                    required API bearer token
--codex auto                     auto or stdio App Server connection
--codex-bin codex                Codex executable
--codex-home PATH                CODEX_HOME override
--poll-interval 10s              App Server reconciliation interval
--filesystem-active-window 60s   filesystem activity inference window
--stale-after 30s                stale snapshot threshold
--max-threads 100                retained recent threads
--usage-history-days 90          retained usage buckets, 0..365
--mdns=true                      advertise _codex-monitor._tcp.local.
```

Verify the process:

```bash
curl -H 'Authorization: Bearer replace-with-a-long-random-token' \
  http://[::1]:8765/healthz
curl -H 'Authorization: Bearer replace-with-a-long-random-token' \
  http://[::1]:8765/api/v1/status
```

### API summary

CMA exposes schema `1.1` JSON over HTTP and live Server-Sent Events. The primary reads are `/api/v1/version`, `/status`, `/threads`, `/requests`, `/usage`, and `/rate-limits`. `/api/v1/events` sends complete `snapshot` messages and sequenced `task_activity` messages; reconnecting clients can supply `Last-Event-ID` for replay. `/api/v1/hooks/codex` receives native Hook payloads.

Control routes are `/api/v1/actions/approve`, `/reject`, `/submit-input`, and `/interrupt`. They require exact request/thread/turn IDs and return `404` for an expired request, `409` for mismatched or non-controllable data, and `503` if an App Server is unavailable.

See [the integration contract](../docs/agent-integration-contract.md) and [OpenAPI](../docs/agent-openapi.yaml) for complete payloads.

### Control boundary

CMA owns one App Server child process. Only requests delivered by this process can be answered by CMA. A separate Codex Desktop process normally owns a different App Server connection; its saved threads are observable through the filesystem but its live approval channel is not shared. Check `controllable` before showing an action. This restriction comes from the connection ownership model, not from missing thread granularity.

### Enable Codex Hooks

Generate the recommended configuration:

```bash
./bin/codex-monitor-agent print-hook-config \
  --token 'replace-with-a-long-random-token'
```

Merge the printed `hooks` object into the existing `~/.codex/hooks.json`, then use `/hooks` in Codex CLI to inspect and trust it. Do not overwrite existing Hooks.

Each configured hook invokes:

```text
codex-monitor-agent hook-forward --token TOKEN http://127.0.0.1:8765/api/v1/hooks/codex
```

`hook-forward` passes Codex's stdin JSON through unchanged, preserving `session_id`, `turn_id`, `cwd`, and tool metadata. It is best-effort with a short timeout so a stopped monitor does not hold up Codex.

Default mapping:

| Hook | State | Authority TTL |
|---|---|---:|
| `PermissionRequest` | `WAITING_APPROVAL` | 5 minutes |
| `Elicitation` | `WAITING_INPUT` | 5 minutes |
| `UserPromptSubmit`, tool/compaction lifecycle | `RUNNING` | 10 minutes |
| `Stop`, `SessionStart`, `SessionEnd` | `IDLE` | 1 minute |

After the TTL, App Server or filesystem state becomes authoritative again. Adjust the TTLs with `--hook-running-ttl`, `--hook-idle-ttl`, and `--hook-attention-ttl`.

Repeated account-read failures cause CMA to restart only the App Server child it owns. `codex.consecutive_failures` and `codex.last_recovery_at` expose this recovery behavior.

### Build and test

```bash
go test -race ./...
go vet ./...
go build ./cmd/cma
```

The release workflow builds `linux`, `darwin`, and `windows` for `amd64` and `arm64` with `CGO_ENABLED=0`.

## 简体中文

Codex Monitor Agent（CMA）是一个局域网 Go 服务。它按优先级组合 Codex Hook、代理自己启动的 App Server，以及本机 `CODEX_HOME` 会话文件，按 thread/session 分别输出状态，并保留子代理的父级与根工作流关系。

构建和启动：

```bash
go build -o ./bin/codex-monitor-agent ./cmd/cma
./bin/codex-monitor-agent --token 'replace-with-a-long-random-token'
```

默认监听 `[::]:8765`，并通过 `_codex-monitor._tcp.local.` 发布 mDNS 服务。Schema `1.1` 提供版本、状态、会话、待处理请求、用量、限额接口；SSE 同时推送完整快照与可重放任务事件；控制接口支持批准、拒绝、提交输入和精确中断。

控制有明确边界：只有通过代理自有 App Server 到达的请求才可操作。Codex Desktop 通常使用另一条 App Server 连接，因此其多会话可以通过文件系统被监控，但 Desktop 持有的实时批准请求不能由代理代答。客户端必须检查 `controllable`。

生成 Hook 配置：

```bash
./bin/codex-monitor-agent print-hook-config \
  --token 'replace-with-a-long-random-token'
```

把输出合并到现有 `~/.codex/hooks.json`，再通过 Codex CLI 的 `/hooks` 检查并信任。`hook-forward` 使用短超时和 best-effort 语义，不会因为代理临时离线而阻塞 Codex。

完整接口见 [对接契约](../docs/agent-integration-contract.md) 和 [OpenAPI](../docs/agent-openapi.yaml)。
