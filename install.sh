#!/usr/bin/env bash
set -euo pipefail

REPO="geoffbelknap/agency"
VERSION="latest"
BIN_DIR="${AGENCY_BIN_DIR:-$HOME/.local/bin}"
SHARE_DIR=""
INSTALL_SOURCE=0
INSTALL_HOST_DEPS=1
DRY_RUN=0
ASSUME_YES="${AGENCY_INSTALL_YES:-0}"
TMP_CLEANUP=""

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Installs the Agency release archive directly. This script does not install
Agency through Homebrew; use Homebrew explicitly if that is the path you want:

  brew tap geoffbelknap/tap
  brew install agency

Options:
  --version <version>  Install a release tag, for example v0.2.0. Default: latest.
  --bin-dir <path>    Install the agency binary here. Default: ~/.local/bin.
  --share-dir <path>  Install runtime assets here. Default: <prefix>/share/agency.
  --skip-host-deps    Do not install/check host runtime dependencies.
  --source            Clone the source repo and run make install. Last-resort path.
  -y, --yes           Skip the interactive curl-to-shell warning.
  --dry-run           Print the commands that would run.
  -h, --help          Show this help.
EOF
}

log() {
  printf '[agency-install] %s\n' "$*" >&2
}

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    printf '+'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

have() {
  command -v "$1" >/dev/null 2>&1
}

confirm_script_install() {
  [ "$ASSUME_YES" = "1" ] && return 0
  [ "$DRY_RUN" -eq 1 ] && return 0
  cat >&2 <<'EOF'

  [=^.^=]  The rainbow install mascot has a compliance note.

You are about to run an installer script from the internet.

That works, but it is still curl-to-shell, which deserves a raised eyebrow.
Homebrew is easier to audit, easier to uninstall, and usually the better path:

  brew tap geoffbelknap/tap
  brew install agency
  agency quickstart

Press Enter to continue with this installer anyway, or press Ctrl-C to stop
and use Homebrew.
EOF
  if [ -r /dev/tty ]; then
    IFS= read -r _ </dev/tty
  else
    cat >&2 <<'EOF'

No interactive terminal is available for confirmation.
Rerun with --yes only if you intentionally want the script installer.
EOF
    exit 1
  fi
}

host_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *)
      echo "unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

host_arch() {
  case "$(uname -m)" in
    arm64|aarch64) printf 'arm64' ;;
    x86_64|amd64) printf 'amd64' ;;
    *)
      echo "unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

release_tag() {
  if [ "$VERSION" != "latest" ]; then
    case "$VERSION" in
      v*) printf '%s' "$VERSION" ;;
      *) printf 'v%s' "$VERSION" ;;
    esac
    return 0
  fi
  if ! have curl; then
    echo "curl is required to resolve the latest Agency release" >&2
    exit 1
  fi
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n 1
}

default_share_dir() {
  local prefix
  prefix="$(dirname "$BIN_DIR")"
  printf '%s/share/agency' "$prefix"
}

copy_asset_dir() {
  local src="$1"
  local dst="$2"
  if [ "$DRY_RUN" -eq 1 ] || [ -d "$src" ]; then
    run mkdir -p "$(dirname "$dst")"
    run rm -rf "$dst"
    run cp -R "$src" "$dst"
  fi
}

install_host_dependencies() {
  local script="$1"
  local asset_root
  asset_root="$(dirname "$(dirname "$(dirname "$script")")")"
  if [ "$INSTALL_HOST_DEPS" -eq 0 ]; then
    return 0
  fi
  if [ "$DRY_RUN" -eq 1 ]; then
    run env "AGENCY_PYTHON_VENV=$asset_root/.venv" "$script"
    return 0
  fi
  if [ ! -x "$script" ]; then
    log "host dependency helper unavailable; skipping dependency install"
    return 0
  fi
  log "installing host runtime dependencies"
  run env "AGENCY_PYTHON_VENV=$asset_root/.venv" "$script"
}

install_release() {
  confirm_script_install

  if ! have curl || ! have tar; then
    echo "install requires curl and tar" >&2
    exit 1
  fi

  local tag version os arch url tmp archive share
  tag="$(release_tag)"
  if [ -z "$tag" ]; then
    echo "could not resolve Agency release tag" >&2
    exit 1
  fi
  version="${tag#v}"
  os="$(host_os)"
  arch="$(host_arch)"
  url="https://github.com/$REPO/releases/download/$tag/agency_${version}_${os}_${arch}.tar.gz"
  tmp="${TMPDIR:-/tmp}/agency-install.$$"
  archive="$tmp/agency.tar.gz"
  share="${SHARE_DIR:-$(default_share_dir)}"

  log "installing Agency $tag for $os/$arch"
  run mkdir -p "$tmp"
  TMP_CLEANUP="$tmp"
  trap 'if [ -n "$TMP_CLEANUP" ]; then rm -rf "$TMP_CLEANUP"; fi' EXIT
  run curl -fL "$url" -o "$archive"
  run tar -xzf "$archive" -C "$tmp"

  if [ ! -f "$tmp/agency" ] && [ "$DRY_RUN" -eq 0 ]; then
    echo "release archive did not contain agency binary" >&2
    exit 1
  fi

  run mkdir -p "$BIN_DIR"
  run cp "$tmp/agency" "$BIN_DIR/agency"
  run chmod 0755 "$BIN_DIR/agency"

  copy_asset_dir "$tmp/images" "$share/images"
  copy_asset_dir "$tmp/services" "$share/services"
  copy_asset_dir "$tmp/web" "$share/web"
  copy_asset_dir "$tmp/scripts" "$share/scripts"

  install_host_dependencies "$share/scripts/install/host-dependencies.sh"

  log "verifying agency"
  run "$BIN_DIR/agency" --version

  cat <<EOF

Agency is installed.

Binary:
  $BIN_DIR/agency

Runtime assets:
  $share

Start here:

  $BIN_DIR/agency quickstart

EOF
}

install_from_source() {
  if ! have git || ! have make || ! have go; then
    cat >&2 <<'EOF'
Source install requires git, make, and Go.
Use a packaged install when possible:

  brew tap geoffbelknap/tap
  brew install agency

or:

  curl -fsSL https://raw.githubusercontent.com/geoffbelknap/agency/main/install.sh | bash
EOF
    exit 1
  fi

  local dest
  dest="${AGENCY_SOURCE_DIR:-$HOME/src/agency}"
  if [ -d "$dest/.git" ]; then
    log "updating source checkout at $dest"
    run git -C "$dest" pull --ff-only
  else
    log "cloning source checkout to $dest"
    run mkdir -p "$(dirname "$dest")"
    run git clone "https://github.com/$REPO.git" "$dest"
  fi
  log "installing from source"
  run make -C "$dest" install
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      if [ -z "$VERSION" ]; then
        echo "--version requires a value" >&2
        exit 64
      fi
      shift 2
      ;;
    --bin-dir)
      BIN_DIR="${2:-}"
      if [ -z "$BIN_DIR" ]; then
        echo "--bin-dir requires a value" >&2
        exit 64
      fi
      shift 2
      ;;
    --share-dir)
      SHARE_DIR="${2:-}"
      if [ -z "$SHARE_DIR" ]; then
        echo "--share-dir requires a value" >&2
        exit 64
      fi
      shift 2
      ;;
    --skip-host-deps)
      INSTALL_HOST_DEPS=0
      shift
      ;;
    --source)
      INSTALL_SOURCE=1
      shift
      ;;
    -y|--yes)
      ASSUME_YES=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
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

if [ "$INSTALL_SOURCE" -eq 1 ]; then
  install_from_source
else
  install_release
fi
