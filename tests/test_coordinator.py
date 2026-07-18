"""Reliable stream cursor behavior tests."""

from custom_components.codex_monitor.coordinator import _advance_event_cursor


def test_contiguous_event_cursor_advances() -> None:
    """A normal next event should be delivered without reconnecting."""
    assert _advance_event_cursor("6", "7", None) == ("7", None, False)


def test_event_gap_retries_from_last_delivered_id() -> None:
    """The first gap should retain the cursor so retained events are replayed."""
    assert _advance_event_cursor("6", "9", None) == ("6", ("6", "9"), True)


def test_event_gap_outside_history_resumes_at_first_retained_event() -> None:
    """A repeated gap should reconcile and then continue without a reconnect loop."""
    assert _advance_event_cursor("6", "300", ("6", "300")) == ("299", None, True)


def test_agent_sequence_reset_is_accepted() -> None:
    """A lower ID after an agent restart becomes the new process cursor."""
    assert _advance_event_cursor("200", "1", None) == ("1", None, False)
