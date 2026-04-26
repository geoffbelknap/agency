#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 [--gateway|--web] /api/v1/path-or-relative-path" >&2
}

target="gateway"
if [ "${1:-}" = "--gateway" ]; then
  target="gateway"
  shift
elif [ "${1:-}" = "--web" ]; then
  target="web"
  shift
fi

if [ "$#" -ne 1 ]; then
  usage
  exit 2
fi

agency_home="${AGENCY_HOME:-$HOME/.agency}"
agency_config="$agency_home/config.yaml"
path="$1"

case "$target" in
  gateway)
    origin="http://127.0.0.1:8200"
    ;;
  web)
    origin="http://127.0.0.1:8280"
    ;;
  *)
    usage
    exit 2
    ;;
esac

if [ ! -f "$agency_config" ]; then
  echo "agency config not found: $agency_config" >&2
  exit 1
fi

token="$(awk -F':[[:space:]]*' '$1 == "token" { print $2; exit }' "$agency_config")"
if [ -z "$token" ]; then
  echo "agency token not found in $agency_config" >&2
  exit 1
fi

if [[ "$path" == /api/v1/* ]]; then
  url="${origin}${path}"
else
  url="${origin}/api/v1/${path#/}"
fi

curl_config="$(mktemp)"
chmod 600 "$curl_config"
cleanup() {
  rm -f "$curl_config"
}
trap cleanup EXIT

{
  printf 'silent\n'
  printf 'show-error\n'
  printf 'header = "X-Agency-Token: %s"\n' "$token"
  printf 'url = "%s"\n' "$url"
} > "$curl_config"

curl --config "$curl_config"
