"""Pure utility helpers for Codex Monitor."""

from urllib.parse import urlsplit, urlunsplit


def normalize_url(value: str) -> str:
    """Validate and normalise an HTTP(S) agent URL."""
    value = value.strip()
    if not value or any(character.isspace() for character in value):
        raise ValueError("agent URL cannot be empty or contain whitespace")
    if "://" not in value:
        value = f"http://{value}"

    parsed = urlsplit(value)
    try:
        _ = parsed.port
    except ValueError as err:
        raise ValueError("agent URL contains an invalid port") from err

    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise ValueError("agent URL must use http or https and include a host")
    if parsed.query or parsed.fragment:
        raise ValueError("agent URL cannot contain a query or fragment")

    path = parsed.path.rstrip("/")
    return urlunsplit((parsed.scheme, parsed.netloc, path, "", ""))
