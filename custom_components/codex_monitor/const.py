"""Constants for the Codex Monitor integration."""

DOMAIN = "codex_monitor"

PLATFORMS = ("sensor", "binary_sensor", "event")

CONF_URL = "url"
CONF_TOKEN = "token"
CONF_NAME = "name"
CONF_SCAN_INTERVAL = "scan_interval"

DEFAULT_URL = "http://127.0.0.1:8765"
DEFAULT_SCAN_INTERVAL = 60
MIN_SCAN_INTERVAL = 5
MAX_SCAN_INTERVAL = 300
THREAD_LIMIT = 50

WORKLOAD_STATES = (
    "running",
    "waiting_approval",
    "waiting_input",
    "idle",
    "error",
    "unknown",
)

CONNECTION_STATES = (
    "connected",
    "connecting",
    "disconnected",
    "error",
    "recovering",
    "unknown",
)

TASK_EVENT_TYPES = (
    "task_started",
    "task_completed",
    "approval_required",
    "input_required",
    "task_failed",
    "task_interrupted",
    "task_resumed",
    "agent_recovered",
)

SERVICE_APPROVE_REQUEST = "approve_request"
SERVICE_REJECT_REQUEST = "reject_request"
SERVICE_SUBMIT_INPUT = "submit_input"
SERVICE_INTERRUPT_TURN = "interrupt_turn"

ATTR_DEVICE_ID = "device_id"
ATTR_REQUEST_ID = "request_id"
ATTR_THREAD_ID = "thread_id"
ATTR_TURN_ID = "turn_id"
ATTR_FOR_SESSION = "for_session"
ATTR_CANCEL_TURN = "cancel_turn"
ATTR_TEXT = "text"
ATTR_ANSWERS = "answers"
