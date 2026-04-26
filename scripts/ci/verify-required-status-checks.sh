#!/usr/bin/env bash
set -euo pipefail

repo="${1:-${AGENCY_REQUIRED_CHECKS_REPO:-geoffbelknap/agency}}"
branch="${2:-${AGENCY_REQUIRED_CHECKS_BRANCH:-main}}"

expected_checks=(
  "go-test"
  "python-unit-test"
  "python-knowledge-test"
  "web-test"
  "docker-smoke"
  "podman-smoke"
  "containerd-smoke"
)

actual_checks="$(gh api "repos/${repo}/branches/${branch}/protection/required_status_checks" --jq '.checks[].context' | sort)"
expected_sorted="$(printf '%s\n' "${expected_checks[@]}" | sort)"

if [[ "${actual_checks}" != "${expected_sorted}" ]]; then
  echo "required status checks drift detected for ${repo}@${branch}" >&2
  echo "expected:" >&2
  printf '%s\n' "${expected_checks[@]}" | sort >&2
  echo "actual:" >&2
  printf '%s\n' "${actual_checks}" >&2
  exit 1
fi

echo "required status checks verified for ${repo}@${branch}"
