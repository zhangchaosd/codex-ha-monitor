"""Test bootstrap that loads pure integration modules without Home Assistant."""

import sys
from pathlib import Path
from types import ModuleType

ROOT = Path(__file__).parents[1] / "custom_components"

custom_components = ModuleType("custom_components")
custom_components.__path__ = [str(ROOT)]
sys.modules.setdefault("custom_components", custom_components)

codex_monitor = ModuleType("custom_components.codex_monitor")
codex_monitor.__path__ = [str(ROOT / "codex_monitor")]
sys.modules.setdefault("custom_components.codex_monitor", codex_monitor)
