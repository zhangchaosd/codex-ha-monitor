"""Repository metadata and machine-readable contract tests."""

from __future__ import annotations

import json
import re
from pathlib import Path

import yaml

ROOT = Path(__file__).parents[1]


class _BlueprintLoader(yaml.SafeLoader):
    """Safe YAML loader that preserves Home Assistant's !input scalar."""


_BlueprintLoader.add_constructor(
    "!input",
    lambda loader, node: loader.construct_scalar(node),
)


def test_json_and_yaml_artifacts_are_parseable() -> None:
    """Translations, API documents, services, and blueprint must stay valid."""
    for path in (
        ROOT / "custom_components/codex_monitor/manifest.json",
        ROOT / "custom_components/codex_monitor/strings.json",
        ROOT / "custom_components/codex_monitor/translations/en.json",
        ROOT / "custom_components/codex_monitor/translations/zh-Hans.json",
    ):
        assert isinstance(json.loads(path.read_text(encoding="utf-8")), dict)

    for path in (
        ROOT / "custom_components/codex_monitor/services.yaml",
        ROOT / "docs/agent-openapi.yaml",
    ):
        assert isinstance(yaml.safe_load(path.read_text(encoding="utf-8")), dict)

    blueprint = ROOT / "blueprints/automation/codex_monitor_attention.yaml"
    assert isinstance(
        yaml.load(blueprint.read_text(encoding="utf-8"), Loader=_BlueprintLoader),
        dict,
    )


def test_published_versions_match_source() -> None:
    """Release automation and manifests must describe the implemented versions."""
    main_go = (ROOT / "agent/cmd/cma/main.go").read_text(encoding="utf-8")
    agent_version = re.search(r'const version = "([^"]+)"', main_go)
    assert agent_version is not None

    openapi = yaml.safe_load((ROOT / "docs/agent-openapi.yaml").read_text(encoding="utf-8"))
    assert openapi["info"]["version"] == agent_version.group(1)

    manifest = json.loads(
        (ROOT / "custom_components/codex_monitor/manifest.json").read_text(encoding="utf-8")
    )
    pyproject = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
    assert f'version = "{manifest["version"]}"' in pyproject
