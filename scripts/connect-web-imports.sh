#!/usr/bin/env bash
set -euo pipefail

monitor_env=${1:-.env}
web_env=${2:-../web/.env}
if [[ ! -f "$monitor_env" || ! -f "$web_env" ]]; then
  echo "Expected Monitor and Web environment files." >&2
  exit 1
fi

read_env() {
  local key=$1 value
  value=$(sed -n "s/^${key}=//p" "$monitor_env" | head -n 1)
  if [[ -z "$value" ]]; then
    echo "Missing ${key} in ${monitor_env}" >&2
    exit 1
  fi
  printf '%s' "$value"
}

upsert() {
  local key=$1 value=$2 tmp
  tmp=$(mktemp "${web_env}.tmp.XXXXXX")
  awk -v key="$key" -v value="$value" '
    BEGIN { updated = 0 }
    index($0, key "=") == 1 { print key "=" value; updated = 1; next }
    { print }
    END { if (!updated) print key "=" value }
  ' "$web_env" >"$tmp"
  chmod 600 "$tmp"
  mv "$tmp" "$web_env"
}

upsert VOXELLINK_MONITOR_IMPORT_URL "https://voxellink.alec-ofc.com/monitor"
upsert VOXELLINK_MONITOR_IMPORT_TOKEN "$(read_env INTEGRATION_SYNC_TOKEN)"
echo "Connected VoxelLink Web's durable import queue to Monitor."
