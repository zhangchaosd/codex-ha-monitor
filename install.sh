#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
SOURCE_DIR="${SCRIPT_DIR}/custom_components/codex_monitor"
CONFIG_DIR="${HA_CONFIG_DIR:-/config}"
TARGET_DIR="${CONFIG_DIR}/custom_components/codex_monitor"

if [ ! -d "${SOURCE_DIR}" ]; then
    echo "Error: integration source not found: ${SOURCE_DIR}" >&2
    exit 1
fi

mkdir -p "${TARGET_DIR}"
cp -R "${SOURCE_DIR}/." "${TARGET_DIR}/"

echo "Codex Monitor installed to ${TARGET_DIR}"
echo "Restart Home Assistant to load the integration."
