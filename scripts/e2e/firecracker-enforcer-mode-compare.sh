#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/../.." && pwd)"
CONFIG_PATH="${AGENCY_CONFIG_PATH:-$HOME/.agency/config.yaml}"
RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${AGENCY_FIRECRACKER_COMPARE_OUT_DIR:-$ROOT_DIR/test-results/firecracker-enforcer-mode-compare/$RUN_ID}"
METRICS_FILE="$OUT_DIR/metrics.jsonl"
REPORT_FILE="$OUT_DIR/report.md"
MODES=(host-process microvm)
SMOKES=(manage recover cleanup)

usage() {
  cat <<'EOF'
Usage: ./scripts/e2e/firecracker-enforcer-mode-compare.sh [mode...]

Runs the same Firecracker Web UI smoke tests for each enforcer mode and writes:
  test-results/firecracker-enforcer-mode-compare/<run-id>/metrics.jsonl
  test-results/firecracker-enforcer-mode-compare/<run-id>/report.md

Modes default to:
  host-process microvm
EOF
}

if [ "$#" -gt 0 ]; then
  case "$1" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      MODES=("$@")
      ;;
  esac
fi

mkdir -p "$OUT_DIR"
CONFIG_BACKUP="$OUT_DIR/config.yaml.before"
cp "$CONFIG_PATH" "$CONFIG_BACKUP"

restore_config() {
  cp "$CONFIG_BACKUP" "$CONFIG_PATH"
  agency serve restart >/dev/null
}
trap restore_config EXIT

set_mode() {
  local mode="$1"
  if ! grep -q 'enforcement_mode:' "$CONFIG_PATH"; then
    echo "Config missing hub.deployment_backend_config.enforcement_mode: $CONFIG_PATH" >&2
    exit 1
  fi
  perl -0pi -e "s/enforcement_mode: [A-Za-z0-9_-]+/enforcement_mode: $mode/" "$CONFIG_PATH"
}

append_report_header() {
  cat >"$REPORT_FILE" <<EOF
# Firecracker Enforcer Mode Comparison

Run: $RUN_ID

| Mode | Smoke | Result | Wall ms |
| --- | --- | --- | ---: |
EOF
}

append_metric_summary() {
  cat >>"$REPORT_FILE" <<'EOF'

## Lifecycle Metrics

| Mode | Test | Agent | Create ms | DM ms | Restart recover ms | Cleanup ms | Workload RSS KiB | Enforcer RSS KiB | Workload task bytes | Enforcer task bytes |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
EOF
  if [ -s "$METRICS_FILE" ]; then
    python3 - "$METRICS_FILE" >>"$REPORT_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    for line in handle:
        metric = json.loads(line)
        dm_ms = metric.get("dm_ms") or metric.get("pre_restart_dm_ms") or ""
        print(
            "| {mode} | {test} | {agent} | {create_ms} | {dm_ms} | {restart_recover_ms} | {cleanup_ms} | {workload_rss_kib} | {enforcer_rss_kib} | {workload_task_bytes} | {enforcer_task_bytes} |".format(
                mode=metric.get("mode", ""),
                test=metric.get("test", ""),
                agent=metric.get("agent", ""),
                create_ms=metric.get("create_ms", ""),
                dm_ms=dm_ms,
                restart_recover_ms=metric.get("restart_recover_ms", ""),
                cleanup_ms=metric.get("cleanup_ms", ""),
                workload_rss_kib=metric.get("workload_rss_kib", ""),
                enforcer_rss_kib=metric.get("enforcer_rss_kib", ""),
                workload_task_bytes=metric.get("workload_task_bytes", ""),
                enforcer_task_bytes=metric.get("enforcer_task_bytes", ""),
            )
        )
PY
  fi

  cat >>"$REPORT_FILE" <<'EOF'

## Security Evidence

| Mode | Test | Transport | Endpoint | Enforcer state | Bridge state | Body WS | Host target envs | Host-only envs | Mediation audit | LLM audit |
| --- | --- | --- | --- | --- | --- | --- | ---: | ---: | --- | --- |
EOF
  if [ -s "$METRICS_FILE" ]; then
    python3 - "$METRICS_FILE" >>"$REPORT_FILE" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    for line in handle:
        metric = json.loads(line)
        print(
            "| {mode} | {test} | {transport_type} | {transport_endpoint} | {enforcer_component_state} | {vsock_bridge_state} | {body_ws_connected} | {host_targets} | {host_only} | {mediation} | {llm} |".format(
                mode=metric.get("mode", ""),
                test=metric.get("test", ""),
                transport_type=metric.get("transport_type", ""),
                transport_endpoint=metric.get("transport_endpoint", ""),
                enforcer_component_state=metric.get("enforcer_component_state", ""),
                vsock_bridge_state=metric.get("vsock_bridge_state", ""),
                body_ws_connected=metric.get("body_ws_connected", ""),
                host_targets=metric.get("workload_host_service_target_env_count", ""),
                host_only=metric.get("workload_host_only_env_count", ""),
                mediation=metric.get("mediation_audit_seen", ""),
                llm=metric.get("llm_audit_seen", ""),
            )
        )
PY
  fi
}

append_report_header
failures=0

for mode in "${MODES[@]}"; do
  case "$mode" in
    host-process|microvm)
      ;;
    *)
      echo "Unsupported mode: $mode" >&2
      exit 1
      ;;
  esac

  echo "==> Firecracker enforcer mode: $mode"
  set_mode "$mode"
  agency serve restart

  for smoke in "${SMOKES[@]}"; do
    echo "==> Running $smoke smoke in $mode mode"
    started="$(date +%s%3N)"
    if AGENCY_E2E_FIRECRACKER_ENFORCEMENT_MODE="$mode" \
      AGENCY_E2E_FIRECRACKER_METRICS_FILE="$METRICS_FILE" \
      "$ROOT_DIR/scripts/e2e/firecracker-webui-smoke.sh" "$smoke"; then
      result="pass"
    else
      result="fail"
    fi
    ended="$(date +%s%3N)"
    wall_ms=$((ended - started))
    printf '| %s | %s | %s | %s |\n' "$mode" "$smoke" "$result" "$wall_ms" >>"$REPORT_FILE"
    if [ "$result" != "pass" ]; then
      failures=$((failures + 1))
    fi
  done
done

append_metric_summary

echo "Comparison complete."
echo "Report: $REPORT_FILE"
echo "Metrics: $METRICS_FILE"

if [ "$failures" -gt 0 ]; then
  echo "Comparison completed with $failures failing smoke(s)." >&2
  exit 1
fi
