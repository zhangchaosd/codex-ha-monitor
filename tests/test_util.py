"""Tests for URL normalisation."""

import pytest

from custom_components.codex_monitor.util import normalize_url


@pytest.mark.parametrize(
    ("value", "expected"),
    [
        ("192.168.1.20:8765/", "http://192.168.1.20:8765"),
        ("https://codex.local:8765/base/", "https://codex.local:8765/base"),
        (" http://[::1]:8765/ ", "http://[::1]:8765"),
    ],
)
def test_normalize_url(value, expected):
    assert normalize_url(value) == expected


@pytest.mark.parametrize(
    "value",
    [
        "",
        "ftp://codex.local:8765",
        "http://bad host:8765",
        "http://codex.local:invalid",
        "http://codex.local:70000",
        "http://codex.local:8765?query=1",
        "http://codex.local:8765/#fragment",
    ],
)
def test_normalize_url_rejects_invalid_values(value):
    with pytest.raises(ValueError):
        normalize_url(value)
