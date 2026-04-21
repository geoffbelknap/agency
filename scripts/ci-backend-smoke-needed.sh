#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: ./scripts/ci-backend-smoke-needed.sh <docker|podman|containerd> [BASE [HEAD]]
       printf '%s\n' <files> | ./scripts/ci-backend-smoke-needed.sh <backend> --stdin

Exits 0 when a PR touched files that should run the selected backend smoke.
Exits 1 when the smoke can be skipped. This intentionally ignores docs,
README-only changes, Apple Container-only smoke changes, and unrelated web test
files so required smoke checks do not spend CI minutes on unrelated work.
EOF
}

backend="${1:-}"
case "$backend" in
  docker|podman|containerd)
    shift
    ;;
  -h|--help)
    usage
    exit 0
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac

changed_files() {
  if [[ "${1:-}" == "--stdin" ]]; then
    cat
    return 0
  fi
  local base="${1:-origin/main}"
  local head="${2:-HEAD}"
  git diff --name-only "${base}...${head}"
}

backend_script_pattern() {
  case "$backend" in
    docker)
      printf '%s\n' '^scripts/(docker-readiness-check|runtime-contract-smoke|cleanup-live-test-runtimes)\.sh$|^\.github/workflows/docker-readiness\.yml$'
      ;;
    podman)
      printf '%s\n' '^scripts/(podman-readiness-check|runtime-contract-smoke|cleanup-live-test-runtimes)\.sh$|^\.github/workflows/podman-readiness\.yml$'
      ;;
    containerd)
      printf '%s\n' '^scripts/(containerd-readiness-check|containerd-rootless-readiness-check|containerd-rootful-readiness-check|with-containerd-env|with-containerd-rootful-env|runtime-contract-smoke|cleanup-live-test-runtimes)\.sh$|^\.github/workflows/containerd-readiness\.yml$'
      ;;
  esac
}

common_code_pattern='^cmd/gateway/|^internal/(api/(admin|agents|infra)/|cli/|config/|daemon/|hostadapter/|images/|infratier/|orchestrate/|runtime/contract/|services/)|^Makefile$|^go\.(mod|sum)$'
web_image_pattern='^web/(Dockerfile|nginx\.conf|agency-entrypoint\.sh|index\.html|package(-lock)?\.json|vite\.config\.ts|tsconfig\.json)$|^web/(src|public)/'
specific_pattern="$(backend_script_pattern)"

matched=0
while IFS= read -r path; do
  [[ -n "$path" ]] || continue
  case "$path" in
    scripts/apple-container-smoke.sh)
      continue
      ;;
  esac
  if [[ "$path" =~ $common_code_pattern || "$path" =~ $web_image_pattern || "$path" =~ $specific_pattern ]]; then
    printf 'backend-smoke-relevant: %s\n' "$path"
    matched=1
  fi
done < <(changed_files "$@")

if [[ "$matched" == "1" ]]; then
  exit 0
fi

printf 'No %s smoke-relevant changes detected.\n' "$backend"
exit 1
