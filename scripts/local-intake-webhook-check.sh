#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${AGENCY_BASE_URL:-http://127.0.0.1:8200}"
CONFIG_PATH="${AGENCY_CONFIG_PATH:-$HOME/.agency/config.yaml}"
LOCAL_SOURCE_NAME="${AGENCY_LOCAL_HUB_SOURCE_NAME:-workspace-local-hub}"
LOCAL_SOURCE_PATH="${AGENCY_LOCAL_HUB_SOURCE_PATH:-/Users/geoffbelknap/Documents/GitHub/agency-workspace/agency-hub}"
CONNECTOR_COMPONENT="${AGENCY_LOCAL_CONNECTOR_COMPONENT:-local-intake-webhook}"
RUN_ID="${AGENCY_LOCAL_CHECK_RUN_ID:-$(date +%s)}"
CONNECTOR_INSTANCE="${AGENCY_LOCAL_CONNECTOR_INSTANCE:-local-intake-webhook-check-${RUN_ID}}"
CHANNEL_NAME="${AGENCY_LOCAL_CHECK_CHANNEL:-local-intake-check-${RUN_ID}}"
WEBHOOK_KIND="${AGENCY_LOCAL_WEBHOOK_KIND:-local-intake-check}"
WAIT_SECONDS="${AGENCY_LOCAL_CHECK_WAIT_SECONDS:-90}"
CACHE_DIR="${AGENCY_HUB_CACHE_DIR:-$HOME/.agency/hub-cache}"

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "Missing Agency config at $CONFIG_PATH" >&2
  exit 1
fi

if [[ ! -d "$LOCAL_SOURCE_PATH/.git" ]]; then
  echo "Local hub source is not a git repo: $LOCAL_SOURCE_PATH" >&2
  exit 1
fi

TOKEN="$(python3 -c "import re,sys; data=open(sys.argv[1], encoding='utf-8').read(); m=re.search(r'(?m)^token:\s*([^#\n]+)', data); print(m.group(1).strip().strip(\"\\\"'\") if m else '')" "$CONFIG_PATH")"
if [[ -z "$TOKEN" ]]; then
  echo "Could not read Agency token from $CONFIG_PATH" >&2
  exit 1
fi

api() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  if [[ -n "$body" ]]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: application/json" \
      --data "$body" \
      "$BASE_URL$path"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer $TOKEN" \
      "$BASE_URL$path"
  fi
}

ensure_local_source() {
  python3 - "$CONFIG_PATH" "$LOCAL_SOURCE_NAME" "$LOCAL_SOURCE_PATH" <<'PY'
from pathlib import Path
import sys

config_path = Path(sys.argv[1])
source_name = sys.argv[2]
source_path = sys.argv[3]
lines = config_path.read_text(encoding="utf-8").splitlines()

if any(line.strip() == f"- name: {source_name}" for line in lines):
    raise SystemExit(0)

block = [
    "        - name: " + source_name,
    "          url: " + source_path,
    "          branch: main",
]

hub_idx = next((i for i, line in enumerate(lines) if line.strip() == "hub:"), None)
if hub_idx is None:
    if lines and lines[-1] != "":
        lines.append("")
    lines.extend(["hub:", "    sources:", *block])
    config_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    raise SystemExit(0)

sources_idx = next((i for i in range(hub_idx + 1, len(lines)) if lines[i].strip() == "sources:"), None)
if sources_idx is None:
    lines[slice(hub_idx + 1, hub_idx + 1)] = ["    sources:", *block]
    config_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
    raise SystemExit(0)

insert_at = len(lines)
for i in range(sources_idx + 1, len(lines)):
    stripped = lines[i].strip()
    if stripped and not lines[i].startswith("        ") and not lines[i].startswith("          "):
        insert_at = i
        break

lines[insert_at:insert_at] = block
config_path.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
}

local_source_cache_current() {
  local cache_repo="$CACHE_DIR/$LOCAL_SOURCE_NAME"
  if [[ ! -d "$cache_repo/.git" ]]; then
    return 1
  fi
  local source_head cache_head
  source_head="$(git -C "$LOCAL_SOURCE_PATH" rev-parse HEAD 2>/dev/null || true)"
  cache_head="$(git -C "$cache_repo" rev-parse HEAD 2>/dev/null || true)"
  [[ -n "$source_head" && "$source_head" == "$cache_head" ]]
}

cleanup() {
  api DELETE "/api/v1/hub/${CONNECTOR_INSTANCE}" >/dev/null 2>&1 || true
  api POST "/api/v1/comms/channels/${CHANNEL_NAME}/archive" '{}' >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Ensuring infrastructure is up"
infra_status="$(./agency -q infra status 2>/dev/null || true)"
if [[ "$infra_status" == *"intake         running"* ]] && [[ "$infra_status" == *"comms          running"* ]] && [[ "$infra_status" == *"knowledge      running"* ]]; then
  echo "Reusing healthy core infrastructure"
else
  ./agency -q infra up >/dev/null
fi

if ! ./agency -q hub list-sources | grep -Fq "$LOCAL_SOURCE_NAME"; then
  echo "Adding local hub source $LOCAL_SOURCE_NAME -> $LOCAL_SOURCE_PATH"
  ensure_local_source
fi

if local_source_cache_current; then
  echo "Reusing current local hub cache"
else
  echo "Refreshing hub sources"
  ./agency -q hub update >/dev/null
fi

echo "Preparing channel $CHANNEL_NAME"
api POST "/api/v1/comms/channels" "$(printf '{"name":"%s","topic":"Local intake webhook route check"}' "$CHANNEL_NAME")" >/dev/null

echo "Installing connector instance $CONNECTOR_INSTANCE from $LOCAL_SOURCE_NAME"
api DELETE "/api/v1/hub/${CONNECTOR_INSTANCE}" >/dev/null 2>&1 || true
api POST "/api/v1/hub/install" "$(printf '{"component":"%s","kind":"connector","source":"%s","as":"%s"}' "$CONNECTOR_COMPONENT" "$LOCAL_SOURCE_NAME" "$CONNECTOR_INSTANCE")" >/dev/null

echo "Configuring connector target channel"
api PUT "/api/v1/hub/${CONNECTOR_INSTANCE}/config" "$(printf '{"config":{"target_channel":"%s"}}' "$CHANNEL_NAME")" >/dev/null

sleep 2

PAYLOAD="$(printf '{"kind":"%s","title":"Local intake webhook check","body":"Validate local webhook route to %s","connector":"%s"}' "$WEBHOOK_KIND" "$CHANNEL_NAME" "$CONNECTOR_INSTANCE")"
echo "Posting webhook payload"
api POST "/api/v1/events/intake/webhook" "$PAYLOAD" >/dev/null

deadline=$((SECONDS + WAIT_SECONDS))
assigned_item_json=""
while (( SECONDS < deadline )); do
  items_json="$(api GET "/api/v1/events/intake/items?connector=${CONNECTOR_INSTANCE}")"
  assigned_item_json="$(python3 -c 'import json,sys; items=json.load(sys.stdin); assigned=[item for item in items if item.get("status")=="assigned"]; match=max(assigned, key=lambda item: item.get("created_at", "")) if assigned else None; print(json.dumps(match) if match else "")' <<<"$items_json")"
  if [[ -n "$assigned_item_json" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "$assigned_item_json" ]]; then
  echo "Connector never produced an assigned work item" >&2
  api GET "/api/v1/events/intake/items?connector=${CONNECTOR_INSTANCE}" >&2 || true
  exit 1
fi

item_summary="$(python3 -c 'import json,sys; item=json.load(sys.stdin); print(item.get("summary",""))' <<<"$assigned_item_json")"
target_name="$(python3 -c 'import json,sys; item=json.load(sys.stdin); print(item.get("target_name",""))' <<<"$assigned_item_json")"
if [[ "$target_name" != "$CHANNEL_NAME" ]]; then
  echo "Assigned work item targeted $target_name, expected $CHANNEL_NAME" >&2
  echo "$assigned_item_json" >&2
  exit 1
fi

deadline=$((SECONDS + WAIT_SECONDS))
dm_match=""
while (( SECONDS < deadline )); do
  messages_json="$(api GET "/api/v1/comms/channels/${CHANNEL_NAME}/messages?limit=20&reader=operator")"
  dm_match="$(python3 -c 'import json,sys; messages=json.load(sys.stdin); match=next((m for m in messages if "Local intake webhook check" in str(m.get("content",""))), None); print(json.dumps(match) if match else "")' <<<"$messages_json")"
  if [[ -n "$dm_match" ]]; then
    break
  fi
  sleep 2
done

if [[ -z "$dm_match" ]]; then
  echo "Target channel did not receive the routed webhook task" >&2
  api GET "/api/v1/comms/channels/${CHANNEL_NAME}/messages?limit=20&reader=operator" >&2 || true
  exit 1
fi

echo "Local intake webhook route check passed"
echo "  connector: $CONNECTOR_INSTANCE"
echo "  channel: $CHANNEL_NAME"
echo "  work item summary: $item_summary"
echo "  target channel: $CHANNEL_NAME"
