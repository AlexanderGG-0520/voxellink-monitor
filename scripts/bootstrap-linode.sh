#!/usr/bin/env bash
set -euo pipefail

web_env=${1:-../web/.env}
if [[ ! -f "$web_env" ]]; then
  echo "Web environment file not found: $web_env" >&2
  exit 1
fi
if [[ -e .env ]]; then
  echo ".env already exists; it was not changed." >&2
  exit 1
fi

read_env() {
  local key=$1 value
  value=$(sed -n "s/^${key}=//p" "$web_env" | head -n 1)
  if [[ -z "$value" ]]; then
    echo "Missing ${key} in ${web_env}" >&2
    exit 1
  fi
  if [[ ${value:0:1} == '"' && ${value: -1} == '"' ]] || [[ ${value:0:1} == "'" && ${value: -1} == "'" ]]; then
    value=${value:1:${#value}-2}
  fi
  printf '%s' "$value"
}

postgres_password=$(read_env POSTGRES_PASSWORD)
client_id=$(read_env DISCORD_CLIENT_ID)
client_secret=$(read_env DISCORD_CLIENT_SECRET)
monitor_token=$(read_env VOXELLINK_MONITOR_TOKEN)

read -r -s -p "Discord bot token: " bot_token
printf '\n'
if [[ -z "$bot_token" ]]; then
  echo "Discord bot token is required." >&2
  exit 1
fi

umask 077
tmp_file=$(mktemp .env.tmp.XXXXXX)
trap 'rm -f "$tmp_file"' EXIT
cat >"$tmp_file" <<EOF
DATABASE_URL=postgres://voxellink:${postgres_password}@voxellink-postgres:5432/voxellink_monitor?sslmode=disable
HTTP_ADDR=:8080
MONITOR_INTERVAL=60s
STATUS_TIMEOUT=5s
FAILURE_RETRY_INTERVAL=10s
DISCORD_BOT_TOKEN=${bot_token}
DISCORD_CLIENT_ID=${client_id}
DISCORD_CLIENT_SECRET=${client_secret}
PUBLIC_BASE_URL=https://voxellink.alec-ofc.com/monitor
SESSION_SECRET=$(openssl rand -hex 32)
VOXELLINK_API_BASE_URL=http://voxellink-api:3000
VOXELLINK_API_TOKEN=${monitor_token}
INTEGRATION_SYNC_TOKEN=$(openssl rand -hex 32)
EOF
mv "$tmp_file" .env
trap - EXIT
echo "Created .env from ${web_env}."
