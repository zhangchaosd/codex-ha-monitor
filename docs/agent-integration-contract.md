# Codex Monitor Agent integration contract

> Client contract for Codex Monitor Agent `0.4.x`, protocol `schema_version: "1.1"`. The machine-readable companion is [agent-openapi.yaml](agent-openapi.yaml).

## Compatibility rules

- Require schema major version `1`; accept newer compatible minor versions and ignore unknown fields/enums.
- Use `installation_id` as the stable identity of one installed agent. URL and host address may change.
- Treat absent optional values as unknown, not zero. Never convert `UNKNOWN` into `IDLE`.
- Surface `state_source`, `state_confidence`, and `codex.visibility` when precision matters.
- Treat `active_workflows` as root user jobs and `active_workers` as currently active root/subagent threads. `active_threads` remains a compatibility alias for active workers.
- Running, waiting for attention, and failure counts are independent facts. Do not derive all UI behavior from the single aggregate `workload_state`.
- Never invoke a control action unless the exact event/request says `controllable: true`.

## Connection, authentication, and discovery

Start CMA with a token:

```bash
codex-monitor-agent --token 'replace-with-a-long-random-token'
```

All JSON, health, action, Hook, and SSE routes require:

```http
Authorization: Bearer <token>
```

A missing or invalid token returns `401` and `WWW-Authenticate: Bearer`. The static `/` dashboard shell is the only unauthenticated route; it asks for a token before fetching data. Do not place tokens in URLs. Normal responses are UTF-8 JSON and timestamps are RFC 3339 UTC strings.

When enabled, CMA advertises `_codex-monitor._tcp.local.` with TXT properties:

```text
installation_id=<stable id>
version=<agent version>
schema_version=1.1
auth_required=true
path=/
```

Discovery supplies an address, not credentials. A client must still collect a token, call `/api/v1/version` and `/api/v1/status`, and require matching `installation_id` values.

## Recommended client lifecycle

1. Probe version and status concurrently and verify identity/schema.
2. Fetch `/status` plus `/threads?limit=50` for the initial model.
3. Open `/events`. Apply `snapshot` messages as the current model and expose `task_activity` as discrete automation events.
4. Save the last task event ID in memory and send it as `Last-Event-ID` after reconnecting.
5. Use a 60-second reconciliation read by default; back off the SSE reconnect from 1 to 30 seconds.
6. Retain the last valid snapshot on transient failure, mark it stale/unavailable in client UI, and do not retry a rejected token until credentials change.
7. Read `/requests` immediately before a human-initiated control if the originating event is no longer current.

## Endpoints

| Method | Path | Result |
|---|---|---|
| `GET` | `/healthz` | Process liveness and uptime. |
| `GET` | `/readyz` | App Server/filesystem/Hook source readiness. |
| `GET` | `/api/v1/version` | Stable identity, schema, agent, Codex CLI, and App Server details. |
| `GET` | `/api/v1/status` | Current aggregate snapshot without `threads` or daily usage buckets. |
| `GET` | `/api/v1/threads?limit=1..200` | Recent per-thread state and hierarchy; default 100. |
| `GET` | `/api/v1/requests` | Pending approval/input request descriptors. |
| `GET` | `/api/v1/usage?days=0..365` | Usage summary and bounded daily buckets. |
| `GET` | `/api/v1/rate-limits` | Account rate-limit windows. |
| `GET` | `/api/v1/events` | Authenticated SSE snapshots and task activity. |
| `POST` | `/api/v1/hooks/codex` | Native Codex Hook receiver. |
| `POST` | `/api/v1/actions/approve` | Resolve one approval as accept/accept-for-session. |
| `POST` | `/api/v1/actions/reject` | Resolve one approval as decline/cancel. |
| `POST` | `/api/v1/actions/submit-input` | Answer one user-input request. |
| `POST` | `/api/v1/actions/interrupt` | Interrupt one exact turn. |

## Core payloads

### Version

```json
{
  "schema_version": "1.1",
  "installation_id": "b0f35c40-2e3e-4f06-9d6c-2f2627fa8fce",
  "agent": {"version": "0.4.0", "go_version": "go1.26.5", "uptime_seconds": 42},
  "codex_cli": {"binary": "codex", "version": "0.145.0"},
  "app_server": {"codex_home": "/Users/alice/.codex"}
}
```

### Aggregate status

```json
{
  "schema_version": "1.1",
  "generated_at": "2026-07-18T03:00:00Z",
  "stale": false,
  "installation_id": "b0f35c40-2e3e-4f06-9d6c-2f2627fa8fce",
  "codex": {
    "connection_state": "connected",
    "visibility": "agent_owned_with_filesystem_fallback",
    "consecutive_failures": 0
  },
  "summary": {
    "workload_state": "WAITING_APPROVAL",
    "state_source": "app_server_request",
    "state_confidence": "exact",
    "known_threads": 4,
    "active_threads": 3,
    "active_workflows": 1,
    "active_workers": 3,
    "states": {
      "running": 2,
      "waiting_approval": 1,
      "waiting_input": 0,
      "idle": 1,
      "error": 0,
      "unknown": 0
    }
  },
  "pending_requests": [],
  "usage": {"availability": "available"},
  "rate_limits": {"availability": "available"}
}
```

`connection_state` is `connecting`, `connected`, `recovering`, `disconnected`, `error`, or an unknown future value. Per-thread and workload state is `RUNNING`, `WAITING_APPROVAL`, `WAITING_INPUT`, `IDLE`, `ERROR`, or `UNKNOWN`.

The aggregate state uses attention-first priority: approval, input, error, running, idle, unknown. This is useful for one-line display only. For example, the payload above intentionally reports both two running workers and one pending approval.

Visibility values:

| Value | Interpretation |
|---|---|
| `shared_app_server` | Live state comes from a shared App Server endpoint. |
| `agent_owned_with_filesystem_fallback` | CMA owns an App Server and augments it with all session files. |
| `filesystem_fallback` | Only session files are available. |
| `unavailable` | No useful source is currently available. |

Without shared visibility, no observed active thread does not prove host-wide idle, so the aggregate may remain `UNKNOWN`.

### Threads and hierarchy

```json
{
  "schema_version": "1.1",
  "generated_at": "2026-07-18T03:00:00Z",
  "threads": [
    {
      "id": "child-thread",
      "session_id": "child-thread",
      "turn_id": "turn-7",
      "parent_thread_id": "root-thread",
      "root_thread_id": "root-thread",
      "thread_role": "subagent",
      "agent_nickname": "reviewer",
      "agent_role": "default",
      "name": "Review API behavior",
      "cwd": "/workspace/project",
      "state": "WAITING_APPROVAL",
      "state_source": "app_server_request",
      "state_confidence": "exact",
      "attention_type": "approval",
      "request_id": "req-42",
      "loaded": true,
      "controllable": true,
      "updated_at": "2026-07-18T02:59:58Z"
    }
  ]
}
```

Root threads have `thread_role: root` and normally `root_thread_id == id`. Subagents have `parent_thread_id`, `thread_role: subagent`, and the transitive root ID. Unknown/broken parent chains fall back to the nearest known parent; clients must not assume every parent is present in a bounded recent-thread response.

For one current-task display, choose the most recently updated thread within this priority: approval, input, running, error.

### Pending requests

```json
{
  "requests": [
    {
      "id": "req-42",
      "type": "approval",
      "method": "item/commandExecution/requestApproval",
      "thread_id": "child-thread",
      "turn_id": "turn-7",
      "item_id": "item-9",
      "summary": "go test ./...",
      "cwd": "/workspace/project",
      "available_decisions": ["accept", "acceptForSession", "decline", "cancel"],
      "controllable": true,
      "created_at": "2026-07-18T02:59:58Z"
    }
  ]
}
```

Command/file approval and `item/tool/requestUserInput` requests delivered to CMA's own App Server connection are currently controllable. Permission and MCP elicitation requests may be visible but non-controllable until their result schemas are supported. In the default monitor-only deployment, `/requests` will usually be empty because CMA does not start turns, and it cannot attach to the live approval channel owned by a separate Desktop App Server. A request disappears after resolution or App Server disconnect.

## SSE event semantics

The stream sends two event names:

```text
event: snapshot
data: { ...complete snapshot including threads... }

id: 42
event: task_activity
data: {"sequence":42,"event_id":"42","type":"approval_required",...}
```

Task event types:

- `task_started`, `task_completed`, `task_failed`, `task_interrupted`, `task_resumed`
- `approval_required`, `input_required`
- `agent_recovered`

`task_activity` includes the IDs, hierarchy, state transition, source/confidence, request ID, controllability, and `occurred_at` known at emission. IDs increase for the life of the agent process. CMA retains the latest 256 events in memory and replays retained IDs greater than `Last-Event-ID`, followed by a current snapshot. Timestamp-only reconciliations do not emit another snapshot. Live snapshots contain the usage summary but omit `dailyUsageBuckets`; clients request usage history explicitly from `/usage`. A restart resets the in-memory sequence; the reconciliation snapshot remains authoritative.

## Controls and errors

Approve:

```json
{"request_id":"req-42","thread_id":"child-thread","turn_id":"turn-7","for_session":false}
```

Reject uses the same IDs plus `"cancel_turn": false`. Submit input accepts either `"text": "answer"` for a single-question request or structured answers:

```json
{
  "request_id": "req-43",
  "thread_id": "child-thread",
  "turn_id": "turn-7",
  "answers": {"question-id": ["first answer", "second answer"]}
}
```

Interrupt requires only exact `thread_id` and `turn_id`. Successful actions return:

```json
{"ok":true,"action":"approve","request_id":"req-42","thread_id":"child-thread","turn_id":"turn-7"}
```

| HTTP | Meaning |
|---:|---|
| `400` | Malformed JSON or invalid query. |
| `401` | Missing/rejected bearer token. |
| `404` | Pending request no longer exists. |
| `405` | Wrong HTTP method. |
| `409` | IDs/type/decision mismatch or request is not controllable. |
| `503` | No agent-owned App Server connection. |
| `502` | Codex App Server rejected/failed the exact operation. |

The client must not automatically approve, reject, answer, or interrupt without an explicit automation or user action. An operation must carry IDs from the same event/request; a thread name is never a control identity.

## Usage, rate limits, and Hooks

Usage/rate-limit endpoints preserve Codex App Server field names. `{"availability":"unavailable"}` is valid and means unknown, not zero. CMA retains at most 90 daily buckets by default and caps `days` to its configured retention.

Hook forwarding is a host-side state input, not a normal reader action. Generate configuration with `codex-monitor-agent print-hook-config --token TOKEN`. A Hook body requires `session_id` and `hook_event_name`; invalid input returns `400`.
