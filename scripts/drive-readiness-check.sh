#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
AGENCY_BIN="${AGENCY_BIN:-}"
AGENCY_HOME_DIR="${AGENCY_HOME:-$HOME/.agency}"
WEB_URL="http://127.0.0.1:8280"
GATEWAY_URL="http://127.0.0.1:8200"
SUFFIX="$(date +%s)"
INSTANCE_ID="inst_drive_gate_${SUFFIX}"
INSTANCE_NAME="drive-gate-${SUFFIX}"
NODE_ID="google_drive_admin"
DEPLOYMENT_ID="dep-drive-gate-${SUFFIX}"
TOKEN_CRED_NAME="drive-gate-bearer-${SUFFIX}"
UPSTREAM_PORT="${AGENCY_DRIVE_GATE_PORT:-18459}"
UPSTREAM_URL="http://127.0.0.1:${UPSTREAM_PORT}"
PYTHON_SERVER=""
PYTHON_SERVER_SCRIPT=""
HELPER_GO=""
INSTANCE_JSON=""
REQUEST_LOG=""
POLL_INTERVAL=2
START_TIMEOUT="${AGENCY_DRIVE_START_TIMEOUT:-60}"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

resolve_agency_bin() {
  if [ -n "$AGENCY_BIN" ]; then
    printf '%s\n' "$AGENCY_BIN"
    return 0
  fi
  if [ -x "$ROOT_DIR/agency" ]; then
    printf '%s\n' "$ROOT_DIR/agency"
    return 0
  fi
  if command -v agency >/dev/null 2>&1; then
    command -v agency
    return 0
  fi
  return 1
}

run_agency() {
  "$AGENCY_BIN" -q "$@"
}

read_gateway_token() {
  local config_path="$AGENCY_HOME_DIR/config.yaml"
  if [ ! -f "$config_path" ]; then
    return 0
  fi
  awk -F: '
    $1 == "token" {
      gsub(/[ "]/, "", $2)
      print $2
      exit
    }
  ' "$config_path"
}

api_json() {
  local method="$1"
  local path="$2"
  local content_type="${3:-application/json}"
  local data_file="${4:-}"

  if [ -n "$data_file" ]; then
    curl -fsS -X "$method" \
      -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
      -H "Content-Type: ${content_type}" \
      --data-binary @"$data_file" \
      "${GATEWAY_URL}${path}"
  else
    curl -fsS -X "$method" \
      -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
      "${GATEWAY_URL}${path}"
  fi
}

api_json_inline() {
  local method="$1"
  local path="$2"
  local payload="$3"
  curl -fsS -X "$method" \
    -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
    -H "Content-Type: application/json" \
    --data "$payload" \
    "${GATEWAY_URL}${path}"
}

api_json_capture() {
  local method="$1"
  local path="$2"
  local payload="$3"
  local body_file
  body_file="$(mktemp /tmp/drive-gate-api-body.XXXXXX)"
  local status
  status="$(
    curl -sS -o "$body_file" -w "%{http_code}" -X "$method" \
      -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
      -H "Content-Type: application/json" \
      --data "$payload" \
      "${GATEWAY_URL}${path}"
  )"
  local body
  body="$(cat "$body_file")"
  rm -f "$body_file"
  printf '%s\n%s\n' "$status" "$body"
}

wait_for_node_running() {
  local deadline=$((SECONDS + START_TIMEOUT))
  local detail
  while [ "$SECONDS" -lt "$deadline" ]; do
    detail="$(api_json GET "/api/v1/instances/${INSTANCE_ID}/runtime/nodes/${NODE_ID}" 2>/dev/null || true)"
    if printf '%s\n' "$detail" | grep -q '"state":"active"'; then
      return 0
    fi
    sleep "$POLL_INTERVAL"
  done
  return 1
}

cleanup() {
  set +e
  if [ -n "${PYTHON_SERVER:-}" ]; then
    kill "$PYTHON_SERVER" >/dev/null 2>&1 || true
    wait "$PYTHON_SERVER" >/dev/null 2>&1 || true
  fi
  if [ -n "${HELPER_GO:-}" ] && [ -f "$HELPER_GO" ]; then
    rm -f "$HELPER_GO"
  fi
  if [ -n "${PYTHON_SERVER_SCRIPT:-}" ] && [ -f "$PYTHON_SERVER_SCRIPT" ]; then
    rm -f "$PYTHON_SERVER_SCRIPT"
  fi
  if [ -n "${INSTANCE_JSON:-}" ] && [ -f "$INSTANCE_JSON" ]; then
    rm -f "$INSTANCE_JSON"
  fi
  if [ -n "${REQUEST_LOG:-}" ] && [ -f "$REQUEST_LOG" ]; then
    rm -f "$REQUEST_LOG"
  fi
  rm -rf "${AGENCY_HOME_DIR}/instances/${INSTANCE_ID}" >/dev/null 2>&1 || true
  rm -rf "${AGENCY_HOME_DIR}/deployments/${DEPLOYMENT_ID}" >/dev/null 2>&1 || true
  run_agency creds delete "$TOKEN_CRED_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM HUP

if ! AGENCY_BIN="$(resolve_agency_bin)"; then
  fail "Could not find agency binary. Run make build or install agency first."
fi

GATEWAY_TOKEN="$(read_gateway_token)"
if [ -z "$GATEWAY_TOKEN" ]; then
  fail "No gateway token found in config.yaml"
fi

log "Checking daemon, infrastructure, and Web UI"
run_agency serve restart >/dev/null
run_agency infra up
curl -fsS "$WEB_URL" >/dev/null ||
  fail "Web UI did not return 200 at $WEB_URL"

log "Creating disposable Drive test credential"
run_agency creds set "$TOKEN_CRED_NAME" \
  --kind service \
  --scope platform \
  --protocol bearer \
  --service drive-gate \
  --value drive-gate-token >/dev/null

REQUEST_LOG="$(mktemp /tmp/drive-gate-requests.XXXXXX)"
PYTHON_SERVER_SCRIPT="$(mktemp /tmp/drive-gate-server.XXXXXX)"
cat > "$PYTHON_SERVER_SCRIPT" <<'PY'
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse

log_path = sys.argv[1]
port = int(sys.argv[2])

def append_log(entry):
    with open(log_path, "a", encoding="utf-8") as fh:
        fh.write(json.dumps(entry) + "\n")

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        parsed = urlparse(self.path)
        append_log({
            "method": "GET",
            "path": parsed.path,
            "query": parse_qs(parsed.query),
            "authorization": self.headers.get("Authorization"),
        })
        if self.headers.get("Authorization") != "Bearer drive-gate-token":
            self.send_response(401)
            self.end_headers()
            self.wfile.write(b'{"error":"unauthorized"}')
            return
        if parsed.path != "/drive/v3/files":
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'{"error":"not_found"}')
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({
            "files": [
                {"id": "file-123", "name": "Alpha Drive Readiness Doc"},
                {"id": "file-456", "name": "Another Doc"},
            ],
            "nextPageToken": None,
        }).encode("utf-8"))

    def do_POST(self):
        parsed = urlparse(self.path)
        content_length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(content_length).decode("utf-8") if content_length else ""
        try:
            body = json.loads(raw) if raw else {}
        except json.JSONDecodeError:
            body = {"_raw": raw}
        append_log({
            "method": "POST",
            "path": parsed.path,
            "query": parse_qs(parsed.query),
            "authorization": self.headers.get("Authorization"),
            "body": body,
        })
        if self.headers.get("Authorization") != "Bearer drive-gate-token":
            self.send_response(401)
            self.end_headers()
            self.wfile.write(b'{"error":"unauthorized"}')
            return
        if parsed.path != "/drive/v3/files/file-123/permissions":
            self.send_response(404)
            self.end_headers()
            self.wfile.write(b'{"error":"not_found"}')
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps({
            "id": "permission-789",
            "role": body.get("role"),
            "type": body.get("type"),
            "emailAddress": body.get("emailAddress"),
        }).encode("utf-8"))

    def log_message(self, fmt, *args):
        return

ThreadingHTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

log "Starting fake Drive upstream"
python3 "$PYTHON_SERVER_SCRIPT" "$REQUEST_LOG" "$UPSTREAM_PORT" &
PYTHON_SERVER=$!
sleep 1
curl -fsS "$UPSTREAM_URL/drive/v3/files?q=warmup" >/dev/null 2>&1 || true

INSTANCE_JSON="$(mktemp /tmp/drive-gate-instance.XXXXXX)"
cat > "$INSTANCE_JSON" <<EOF
{
  "id": "${INSTANCE_ID}",
  "name": "${INSTANCE_NAME}",
  "source": {
    "package": {
      "kind": "connector",
      "name": "google-drive-admin",
      "version": "1.1.0"
    }
  },
  "config": {
    "consent_deployment_id": "${DEPLOYMENT_ID}"
  },
  "nodes": [
    {
      "id": "${NODE_ID}",
      "kind": "connector.authority",
      "package": {
        "kind": "connector",
        "name": "google-drive-admin",
        "version": "1.1.0"
      },
      "config": {
        "tools": [
          "drive_list_folder_contents",
          "drive_share_file"
        ],
        "credential_bindings": [
          "${TOKEN_CRED_NAME}"
        ],
        "executor": {
          "kind": "http_json",
          "base_url": "${UPSTREAM_URL}",
          "actions": {
            "drive_list_folder_contents": {
              "method": "GET",
              "path": "/drive/v3/files",
              "query": {
                "q": "q",
                "pageSize": "pageSize",
                "pageToken": "pageToken",
                "fields": "fields",
                "includeItemsFromAllDrives": "includeItemsFromAllDrives",
                "supportsAllDrives": "supportsAllDrives"
              }
            },
            "drive_share_file": {
              "method": "POST",
              "path": "/drive/v3/files/{file_id}/permissions",
              "query": {
                "sendNotificationEmail": "sendNotificationEmail"
              },
              "body": {
                "role": "role",
                "type": "type",
                "emailAddress": "emailAddress"
              }
            }
          },
          "auth": {
            "type": "bearer",
            "binding": "${TOKEN_CRED_NAME}"
          }
        }
      }
    }
  ],
  "credentials": {
    "${TOKEN_CRED_NAME}": {
      "type": "credref",
      "target": "credref:${TOKEN_CRED_NAME}"
    }
  },
  "grants": [
    {
      "principal": "alpha-tester",
      "action": "drive_list_folder_contents",
      "resource": "${NODE_ID}"
    },
    {
      "principal": "alpha-tester",
      "action": "drive_share_file",
      "resource": "${NODE_ID}",
      "config": {
        "requires_consent_token": {
          "operation_kind": "grant_drive_viewer",
          "token_input_field": "consent_token",
          "target_input_field": "file_id"
        }
      }
    }
  ]
}
EOF

log "Creating disposable Drive runtime instance"
api_json POST "/api/v1/instances" "application/json" "$INSTANCE_JSON" >/dev/null
api_json POST "/api/v1/instances/${INSTANCE_ID}/runtime/manifest" >/dev/null
api_json POST "/api/v1/instances/${INSTANCE_ID}/runtime/reconcile" >/dev/null

HELPER_GO="$(mktemp "$ROOT_DIR/drive-consent-helper.XXXXXX.go")"
cat > "$HELPER_GO" <<'EOF'
package main

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"time"

	agencyconsent "github.com/geoffbelknap/agency/internal/consent"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintf(os.Stderr, "usage: %s <agency_home> <deployment_id> <operation_kind> <target>\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	home := os.Args[1]
	deploymentID := os.Args[2]
	operationKind := os.Args[3]
	target := os.Args[4]

	pub, priv, err := agencyconsent.GenerateSigningKeyPair()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	keyID := deploymentID + ":v1"
	cfg := &agencyconsent.VerificationConfig{
		DeploymentID:     deploymentID,
		MaxTTLSeconds:    900,
		ClockSkewMillis:  int((30 * time.Second).Milliseconds()),
		VerificationKeys: map[string]string{keyID: agencyconsent.EncodePublicKey(pub)},
	}
	if err := cfg.Write(agencyconsent.DeploymentDir(home, deploymentID)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	now := time.Now().UTC()
	token := agencyconsent.Token{
		Version:         1,
		DeploymentID:    deploymentID,
		OperationKind:   operationKind,
		OperationTarget: []byte(target),
		Issuer:          "drive-readiness-check",
		Witnesses:       []string{"operator"},
		IssuedAt:        now.UnixMilli(),
		ExpiresAt:       now.Add(5 * time.Minute).UnixMilli(),
		Nonce:           []byte("drive-readiness!"),
		SigningKeyID:    keyID,
	}
	raw, err := token.MarshalCanonical()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoded, err := agencyconsent.EncodeSignedToken(agencyconsent.SignedToken{
		Token:     token,
		Signature: ed25519.Sign(priv, raw),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(encoded)
}
EOF

log "Generating consent token for Drive mutation"
consent_token="$(go run "$HELPER_GO" "$AGENCY_HOME_DIR" "$DEPLOYMENT_ID" "grant_drive_viewer" "file-123")"
[ -n "$consent_token" ] || fail "Consent token helper did not return a token"

api_json POST "/api/v1/instances/${INSTANCE_ID}/runtime/nodes/${NODE_ID}/start" >/dev/null
wait_for_node_running || fail "Drive authority node did not reach active state"

log "Checking positive read path"
read_result="$(api_json_inline POST "/api/v1/instances/${INSTANCE_ID}/runtime/nodes/${NODE_ID}/invoke" '{"subject":"alpha-tester","node_id":"google_drive_admin","action":"drive_list_folder_contents","input":{"q":"'\''folder-123'\'' in parents and trashed=false","pageSize":2}}')"
printf '%s\n' "$read_result" | grep -q '"allowed":true' ||
  fail "Drive read path was not allowed: $read_result"
printf '%s\n' "$read_result" | grep -q '"status_code":200' ||
  fail "Drive read path did not execute upstream: $read_result"
printf '%s\n' "$read_result" | grep -q 'Alpha Drive Readiness Doc' ||
  fail "Drive read path did not return the expected upstream payload: $read_result"

log "Checking consent-gated mutation denial"
deny_capture="$(api_json_capture POST "/api/v1/instances/${INSTANCE_ID}/runtime/nodes/${NODE_ID}/invoke" '{"subject":"alpha-tester","node_id":"google_drive_admin","action":"drive_share_file","input":{"file_id":"file-123","role":"reader","type":"user","emailAddress":"tester@example.com","sendNotificationEmail":false}}')"
deny_status="$(printf '%s\n' "$deny_capture" | sed -n '1p')"
deny_result="$(printf '%s\n' "$deny_capture" | sed -n '2,$p')"
[ "$deny_status" = "403" ] ||
  fail "Drive mutation denial returned unexpected status ${deny_status}: $deny_result"
printf '%s\n' "$deny_result" | grep -q '"consent_needed":true' ||
  fail "Drive mutation denial did not require consent: $deny_result"
if grep -q '/drive/v3/files/file-123/permissions' "$REQUEST_LOG"; then
  share_calls_before_allow="$(grep -c '/drive/v3/files/file-123/permissions' "$REQUEST_LOG")"
else
  share_calls_before_allow=0
fi
[ "$share_calls_before_allow" -eq 0 ] ||
  fail "Drive mutation denial unexpectedly called the upstream"

log "Checking consent-authorized mutation path"
allow_payload="$(mktemp /tmp/drive-gate-allow.XXXXXX)"
cat > "$allow_payload" <<EOF
{"subject":"alpha-tester","node_id":"google_drive_admin","action":"drive_share_file","input":{"file_id":"file-123","role":"reader","type":"user","emailAddress":"tester@example.com","sendNotificationEmail":false,"consent_token":"${consent_token}"}}
EOF
allow_capture="$(
  {
    status="$(
      curl -sS -o /tmp/drive-gate-allow-body.$$ -w "%{http_code}" -X POST \
        -H "Authorization: Bearer ${GATEWAY_TOKEN}" \
        -H "Content-Type: application/json" \
        --data-binary @"$allow_payload" \
        "${GATEWAY_URL}/api/v1/instances/${INSTANCE_ID}/runtime/nodes/${NODE_ID}/invoke"
    )"
    body="$(cat /tmp/drive-gate-allow-body.$$)"
    rm -f /tmp/drive-gate-allow-body.$$
    printf '%s\n%s\n' "$status" "$body"
  }
)"
rm -f "$allow_payload"
allow_status="$(printf '%s\n' "$allow_capture" | sed -n '1p')"
allow_result="$(printf '%s\n' "$allow_capture" | sed -n '2,$p')"
[ "$allow_status" = "200" ] ||
  fail "Drive mutation allow path returned unexpected status ${allow_status}: $allow_result"
printf '%s\n' "$allow_result" | grep -q '"allowed":true' ||
  fail "Drive mutation allow path was not allowed: $allow_result"
printf '%s\n' "$allow_result" | grep -q '"status_code":200' ||
  fail "Drive mutation allow path did not execute upstream: $allow_result"
grep -q '/drive/v3/files/file-123/permissions' "$REQUEST_LOG" ||
  fail "Drive mutation allow path did not hit the fake upstream"
grep -q '"emailAddress": "tester@example.com"' "$REQUEST_LOG" ||
  fail "Drive mutation allow path did not send the expected body"

log "Drive authority runtime readiness checks passed"
printf 'PASS: Drive read path, consent-gated mutation denial, and consent-authorized mutation are working.\n'
