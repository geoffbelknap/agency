#!/usr/bin/env bash
set -euo pipefail

MODE="install"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VENV_DIR="${AGENCY_PYTHON_VENV:-$ROOT_DIR/.venv}"
VENV_PYTHON="$VENV_DIR/bin/python"
VENV_MITMDUMP="$VENV_DIR/bin/mitmdump"
PYTHON_DEPS=(
  "mitmproxy==12.2.1"
  "pyyaml==6.0.3"
  "PyJWT==2.12.1"
  "cryptography==46.0.6"
  "requests==2.32.3"
)

usage() {
  cat <<'EOF'
Usage: scripts/install/host-dependencies.sh [--check|--dry-run]

Installs host tools required by Agency's supported microVM runtime path:
  - mitmproxy/mitmdump plus addon Python packages for host-managed egress
  - e2fsprogs/mke2fs for microVM rootfs image creation

Supported package managers:
  - macOS/Linux Homebrew: brew
  - Debian/Ubuntu/WSL: apt-get
  - Fedora/RHEL family: dnf or yum
  - Arch family: pacman
  - openSUSE family: zypper
EOF
}

log() {
  printf '[host-deps] %s\n' "$*" >&2
}

have() {
  command -v "$1" >/dev/null 2>&1
}

sudo_cmd() {
  if [ "$(id -u)" -eq 0 ]; then
    "$@"
  else
    sudo "$@"
  fi
}

missing_tools() {
	missing=()
	if [ ! -x "$VENV_MITMDUMP" ]; then
		missing+=("venv mitmdump")
	fi
	if ! have mke2fs && [ ! -x /opt/homebrew/opt/e2fsprogs/sbin/mke2fs ]; then
		missing+=("mke2fs")
	fi
  if [ "${#missing[@]}" -gt 0 ]; then
    printf '%s\n' "${missing[@]}"
  fi
}

read_missing_tools() {
  missing=()
  while IFS= read -r tool; do
    [ -n "$tool" ] && missing+=("$tool")
  done < <(missing_tools)
}

package_manager() {
  if have brew; then
    echo "brew"
    return 0
  fi
  for candidate in apt-get dnf yum pacman zypper; do
    if have "$candidate"; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}

packages_for() {
	case "$1" in
	brew)
		echo "python e2fsprogs"
		;;
	apt-get)
		echo "e2fsprogs python3 python3-venv python3-pip"
		;;
	dnf|yum)
		echo "e2fsprogs python3 python3-pip"
		;;
	pacman)
		echo "e2fsprogs python python-pip"
		;;
	zypper)
		echo "e2fsprogs python3 python3-pip"
		;;
    *)
      return 1
      ;;
  esac
}

python_bin() {
	if have python3; then
		command -v python3
	elif have python; then
		command -v python
	else
		return 1
	fi
}

venv_ready() {
	[ -x "$VENV_PYTHON" ] || return 1
	[ -x "$VENV_MITMDUMP" ] || return 1
	"$VENV_PYTHON" - <<'PY' >/dev/null 2>&1
import cryptography
import jwt
import requests
import yaml
PY
}

install_python_deps() {
	py="$(python_bin)" || {
		echo "python3 is required to create $VENV_DIR" >&2
		return 1
	}
	if [ ! -x "$VENV_PYTHON" ]; then
		"$py" -m venv "$VENV_DIR"
	fi
	"$VENV_PYTHON" -m pip install --upgrade pip
	"$VENV_PYTHON" -m pip install "${PYTHON_DEPS[@]}"
}

install_packages() {
  manager="$1"
  # shellcheck disable=SC2046
  set -- $(packages_for "$manager")
  case "$manager" in
    brew)
      brew install "$@"
      ;;
    apt-get)
      sudo_cmd apt-get update
      sudo_cmd apt-get install -y "$@"
      ;;
    dnf)
      sudo_cmd dnf install -y "$@"
      ;;
    yum)
      sudo_cmd yum install -y "$@"
      ;;
    pacman)
      sudo_cmd pacman -Sy --needed --noconfirm "$@"
      ;;
    zypper)
      sudo_cmd zypper --non-interactive install "$@"
      ;;
    *)
      return 1
      ;;
  esac
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --check)
      MODE="check"
      shift
      ;;
    --dry-run)
      MODE="dry-run"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 64
      ;;
  esac
done

read_missing_tools
if [ "${#missing[@]}" -eq 0 ] && venv_ready; then
	log "host dependencies are present"
	exit 0
fi

if ! manager="$(package_manager)"; then
  printf 'missing host tools: %s\n' "${missing[*]}" >&2
  printf 'Install mitmproxy and e2fsprogs with your system package manager, then rerun agency setup.\n' >&2
  exit 1
fi

packages="$(packages_for "$manager")"
case "$MODE" in
	check)
		if [ "${#missing[@]}" -gt 0 ]; then
			printf 'missing host tools: %s\n' "${missing[*]}" >&2
		fi
		if ! venv_ready; then
			printf 'missing host egress Python environment: %s\n' "$VENV_DIR" >&2
		fi
		printf 'install with %s packages: %s\n' "$manager" "$packages" >&2
		printf 'then install Python dependencies into %s\n' "$VENV_DIR" >&2
		exit 1
		;;
	dry-run)
		printf '%s install packages: %s\n' "$manager" "$packages"
		printf 'python venv: %s\n' "$VENV_DIR"
		printf 'python packages: %s\n' "${PYTHON_DEPS[*]}"
		exit 0
		;;
esac

log "installing host dependencies with $manager: $packages"
install_packages "$manager"
log "installing host egress Python dependencies into $VENV_DIR"
install_python_deps

missing_after=()
while IFS= read -r tool; do
  [ -n "$tool" ] && missing_after+=("$tool")
done < <(missing_tools)
if [ "${#missing_after[@]}" -gt 0 ] || ! venv_ready; then
	printf 'host dependency install completed, but these tools are still missing: %s\n' "${missing_after[*]}" >&2
	exit 1
fi
log "host dependencies are present"
