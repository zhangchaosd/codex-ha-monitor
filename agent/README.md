# Codex Monitor Agent

一个无认证的局域网只读 Codex 状态代理。当前 MVP 使用 Go 标准库实现，支持：

- 自动启动 stdio Codex App Server，并读取线程、Token 和额度。
- 接收 Codex 原生生命周期 Hooks，按 `session_id` / `turn_id` 提供低延迟任务和审批状态。
- 扫描本机 `CODEX_HOME` 会话元数据，兼容独立运行的 Desktop/IDE 客户端。
- `/api/v1/version`、`/status`、`/threads`、`/usage`、`/rate-limits`。
- `POST /api/v1/hooks/codex` 原生 Hook 接口。
- `/api/v1/events` Server-Sent Events。
- 内置只读状态页面。

状态数据源优先级为：有效 Hook 事件 → App Server 状态 → 文件系统推断。Hook 只覆盖相同 `session_id` 的任务，不会把多个 Codex 任务合并成一个全局开关。

## 开发运行

```bash
go run ./cmd/cma 8765
```

然后访问：

```text
http://127.0.0.1:8765/
http://127.0.0.1:8765/api/v1/version
http://127.0.0.1:8765/api/v1/status
http://127.0.0.1:8765/api/v1/threads
```

默认监听 `0.0.0.0`，不需要认证。

## 启用 Codex Hooks

构建代理后生成推荐配置：

```bash
./bin/codex-monitor-agent print-hook-config
```

将输出的 `hooks` 对象合并到现有 `~/.codex/hooks.json`，不要覆盖已有 Hooks。然后在 Codex CLI 中运行 `/hooks`，检查并信任新命令。

配置里的每个 command hook 会执行：

```text
codex-monitor-agent hook-forward http://127.0.0.1:8765/api/v1/hooks/codex
```

`hook-forward` 将 Codex 写到 stdin 的 JSON 原样转发，因此代理能够保留 `session_id`、`turn_id`、`cwd` 和工具名称。转发采用短超时和 best-effort 语义；代理暂时不可用时不会阻塞 Codex。

默认状态映射：

| 事件 | 状态 | TTL |
|---|---|---:|
| `PermissionRequest` | `WAITING_APPROVAL` | 5 分钟 |
| `Elicitation`（兼容接收，当前生成配置不主动注册） | `WAITING_INPUT` | 5 分钟 |
| `UserPromptSubmit`、工具和压缩生命周期事件 | `RUNNING` | 10 分钟 |
| `Stop`、`SessionStart`、`SessionEnd` | `IDLE` | 1 分钟 |

TTL 到期后自动恢复 App Server 或文件系统状态。可用 `--hook-running-ttl`、`--hook-idle-ttl` 和 `--hook-attention-ttl` 调整。

> `print-hook-config` 只打印配置，不修改用户的 Codex 文件。Hook 必须经过 Codex 的 `/hooks` 信任流程才会运行。

## 构建与测试

```bash
go test ./...
go vet ./...
go build -o ./bin/codex-monitor-agent ./cmd/cma
```

当前 M1.1 实现 `auto`、`stdio`、按会话 Hook 关联和文件系统回退；WebSocket、Unix socket、mDNS 和服务文件属于下一里程碑。
