"""Constants for the Codex Monitor integration."""

DOMAIN = "codex_monitor"

PLATFORMS = ("sensor", "binary_sensor")

CONF_URL = "url"
CONF_TOKEN = "token"
CONF_NAME = "name"
CONF_SCAN_INTERVAL = "scan_interval"

DEFAULT_URL = "http://127.0.0.1:8765"
DEFAULT_SCAN_INTERVAL = 5
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
    "unknown",
)
