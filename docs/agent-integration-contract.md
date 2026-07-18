# Codex Monitor Agent integration contract

> Give this document to an AI or a third-party developer when building a client for Codex Monitor Agent (CMA). It is the integration contract for released agent versions `0.3.x` with `schema_version: "1.0"`.

## Scope and compatibility

CMA is a **read-only** local-network API. It exposes Codex runtime state, recent thread summaries, usage, rate limits, and Hook-derived status. It does not provide task creation, chat, approval, input, cancellation, or any other control operation.

Clients must:

- Ignore unknown JSON fields and unknown enum values.
- Treat missing optional fields as unknown, not zero.
- Preserve `installation_id` as the stable identity of one agent installation.
- Never infer that `UNKNOWN` means `IDLE`.
- Display `visibility`, `state_source`, and `state_confidence` when presenting task state.

The complete machine-readable endpoint description is [agent-openapi.yaml](agent-openapi.yaml).

## Connect

Start the agent with a sufficiently long random token:

```bash
codex-monitor-agent --token 'replace-with-a-long-random-token'
```

The default listener is IPv6 wildcard `[::]:8765`. A typical LAN URL is `http://[fd00::20]:8765`; an IPv4 URL can be used when the host/network supports it.

Every data endpoint requires this exact header:

```http
Authorization: Bearer <token>
```

Do not put the token in a query parameter or URL. A missing or invalid token returns `401 Unauthorized` and a `WWW-Authenticate: Bearer` header.

```bash
BASE_URL='http://[fd00::20]:8765'
TOKEN='replace-with-a-long-random-token'
curl -H "Authorization: Bearer $TOKEN" "$BASE_URL/api/v1/status"
```

`GET /` is a static dashboard shell. It asks for the token in the browser and then keeps it in that browser session. Programmatic clients should call the JSON endpoints directly.

## Discovery and polling sequence

1. Request `GET /api/v1/version` and `GET /api/v1/status` with the bearer token.
2. Require the same non-empty `installation_id` in both replies before creating a device/client record.
3. Poll `GET /api/v1/status` and `GET /api/v1/threads?limit=50` concurrently. A five-second interval is a reasonable default; do not poll faster than once per second.
4. Request `/api/v1/usage` and `/api/v1/rate-limits` only when the client needs those values. Their payloads may legitimately contain `"availability": "unavailable"`.
5. On network failure, 5xx, or invalid JSON, retain the last valid snapshot as stale and retry with bounded backoff. Do not retry authentication failures until credentials change.

## Endpoints

| Method | Path | Client use |
|---|---|---|
| `GET` | `/healthz` | Process liveness only. |
| `GET` | `/readyz` | Readiness/debug information. |
| `GET` | `/api/v1/version` | Agent identity and software versions. |
| `GET` | `/api/v1/status` | Current aggregate state; thread array omitted. |
| `GET` | `/api/v1/threads?limit=1..200` | Recent thread summaries; default limit is 100. |
| `GET` | `/api/v1/usage` | Account usage summary and daily buckets. |
| `GET` | `/api/v1/rate-limits` | Codex account rate-limit payload. |
| `GET` | `/api/v1/events` | SSE stream of complete snapshots. |
| `POST` | `/api/v1/hooks/codex` | Native Codex Hook relay receiver; not needed by normal readers. |

All timestamps are RFC 3339 UTC strings. JSON is UTF-8. The API never provides Codex control operations.

## Core payloads

### Version

```json
{
  "schema_version": "1.0",
  "installation_id": "b0f35c40-2e3e-4f06-9d6c-2f2627fa8fce",
  "agent": {"version": "0.3.0", "go_version": "go1.26.5", "uptime_seconds": 42},
  "codex_cli": {"version": "0.144.5"},
  "app_server": {"codex_home": "/home/user/.codex"}
}
```

### Status

`GET /api/v1/status` returns the snapshot without the potentially large `threads` field.

```json
{
  "schema_version": "1.0",
  "generated_at": "2026-07-18T03:00:00Z",
  "stale": false,
  "installation_id": "b0f35c40-2e3e-4f06-9d6c-2f2627fa8fce",
  "codex": {
    "connection_state": "connected",
    "visibility": "agent_owned_with_filesystem_fallback",
    "consecutive_failures": 0
  },
  "summary": {
    "workload_state": "RUNNING",
    "state_source": "codex_hook",
    "state_confidence": "event_derived",
    "known_threads": 2,
    "active_threads": 1,
    "states": {"running": 1, "waiting_approval": 0, "waiting_input": 0, "idle": 1, "error": 0, "unknown": 0}
  },
  "usage": {"availability": "available"},
  "rate_limits": {"availability": "available"}
}
```

`connection_state` is one of `connecting`, `connected`, `disconnected`, or `error`. `workload_state` and per-thread `state` are one of `RUNNING`, `WAITING_APPROVAL`, `WAITING_INPUT`, `IDLE`, `ERROR`, or `UNKNOWN`.

`visibility` defines what CMA can observe:

| Value | Meaning |
|---|---|
| `shared_app_server` | Live app-server state is shared; state is precise. |
| `agent_owned_with_filesystem_fallback` | CMA owns its app server and augments it with session files; activity can be inferred. |
| `filesystem_fallback` | Only local session files are available; no account usage or rate limits. |
| `unavailable` | No usable source is currently available. |

When visibility is not `shared_app_server`, an absence of active threads must be displayed as `UNKNOWN`, not host-wide idle.

### Threads

```json
{
  "schema_version": "1.0",
  "generated_at": "2026-07-18T03:00:00Z",
  "threads": [{
    "id": "019f6ddd-0e10-7790-a9c1-d07b63557cbe",
    "name": "Monitor Codex",
    "preview": "Inspect agent status",
    "cwd": "/workspace/project",
    "source": "cli",
    "state": "RUNNING",
    "state_source": "codex_hook",
    "state_confidence": "event_derived",
    "loaded": false,
    "updated_at": "2026-07-18T02:59:58Z"
  }]
}
```

Thread selection priority for a single “current task” display is `WAITING_APPROVAL`, then `WAITING_INPUT`, then `RUNNING`, then `ERROR`; use most recent `updated_at` to break ties.

### Usage and rate limits

These endpoints preserve Codex App Server field names. A response can be valid but unavailable:

```json
{"availability":"unavailable","error":"context deadline exceeded"}
```

Treat this as an unknown usage/limit value, not zero and not an agent connection failure. CMA automatically rebuilds its app-server child after repeated account-read failures.

## SSE and Hooks

`GET /api/v1/events` is an authenticated Server-Sent Events stream. Each message has event name `snapshot` and a complete snapshot JSON object in `data:`. Browser `EventSource` cannot attach an Authorization header; use a fetch-based SSE client or polling in browser clients.

Hook forwarding is only for the Codex host. Generate the configuration with:

```bash
codex-monitor-agent print-hook-config --token 'replace-with-a-long-random-token'
```

The generated hook commands pass the bearer token to `POST /api/v1/hooks/codex`. A Hook body must include `session_id` and `hook_event_name`; success returns the mapped state, invalid input returns `400`.

## AI implementation prompt

```text
Build a read-only client for Codex Monitor Agent using docs/agent-integration-contract.md and docs/agent-openapi.yaml as the source of truth. Configure a base URL and bearer token. Send Authorization: Bearer <token> on every API request. On setup, verify that /api/v1/version and /api/v1/status return the same installation_id. Poll status and threads concurrently, preserve unknown fields, do not treat UNKNOWN as IDLE, and surface visibility/state_source/state_confidence. Treat usage or rate_limits availability=unavailable as unknown values. Do not implement task control, approval, user-input, or token-in-URL behavior.
```
