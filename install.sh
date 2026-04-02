#!/usr/bin/env bash
set -euo pipefail

# Agency Platform — Install Script
# Downloads the Go binary, checks Docker, initializes Agency.
# Safe to run multiple times.

INSTALL_DIR="$HOME/.agency/bin"
REPO="geoffbelknap/agency"

# ---------- helpers ----------

print_header() {
    echo ""
    echo "=== $1 ==="
    echo ""
}

print_ok() {
    echo "  [OK] $1"
}

print_fail() {
    echo "  [FAIL] $1" >&2
}

print_info() {
    echo "  [INFO] $1"
}

run_agency() {
    "${INSTALL_DIR}/agency" "$@"
}

# Run a command silently with a spinner, showing a status message.
# Usage: run_with_spinner "Building from source..." command arg1 arg2
run_with_spinner() {
    local msg="$1"
    shift
    local logfile
    logfile=$(mktemp)
    local spin_chars='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    local i=0

    printf "  %s %s" "${spin_chars:0:1}" "$msg"

    "$@" > "$logfile" 2>&1 &
    local pid=$!

    while kill -0 "$pid" 2>/dev/null; do
        i=$(( (i + 1) % ${#spin_chars} ))
        printf "\r  %s %s" "${spin_chars:$i:1}" "$msg"
        sleep 0.15
    done

    wait "$pid"
    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        printf "\r  [OK] %s\n" "$msg"
    else
        printf "\r  [FAIL] %s\n" "$msg" >&2
        echo ""
        echo "  Output:"
        sed 's/^/    /' "$logfile" | tail -20
        echo ""
    fi
    rm -f "$logfile"
    return $exit_code
}

# Run a command quietly in the foreground (for commands that spawn subprocesses
# and can't be safely backgrounded, like docker/daemon commands).
# Shows a status message, suppresses output, reports OK/FAIL.
run_quiet() {
    local msg="$1"
    shift
    local logfile
    logfile=$(mktemp)

    printf "  ...  %s" "$msg"

    if "$@" > "$logfile" 2>&1; then
        printf "\r  [OK] %s\n" "$msg"
        rm -f "$logfile"
        return 0
    else
        local exit_code=$?
        printf "\r  [FAIL] %s\n" "$msg" >&2
        echo ""
        echo "  Output:"
        sed 's/^/    /' "$logfile" | tail -20
        echo ""
        rm -f "$logfile"
        return $exit_code
    fi
}

# Detect package manager
detect_pkg_manager() {
    if command -v apt-get &>/dev/null; then
        echo "apt"
    elif command -v dnf &>/dev/null; then
        echo "dnf"
    elif command -v pacman &>/dev/null; then
        echo "pacman"
    elif command -v brew &>/dev/null; then
        echo "brew"
    else
        echo "unknown"
    fi
}

pkg_install() {
    local pkg_manager="$1"
    shift
    case "$pkg_manager" in
        apt)    sudo apt-get update -qq && sudo apt-get install -y -qq "$@" ;;
        dnf)    sudo dnf install -y -q "$@" ;;
        pacman) sudo pacman -S --noconfirm "$@" ;;
        brew)   brew install "$@" ;;
        *)      return 1 ;;
    esac
}

# Detect OS and architecture for binary download
detect_platform() {
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$arch" in
        x86_64)  arch="amd64" ;;
        aarch64) arch="arm64" ;;
        arm64)   arch="arm64" ;;
        *)
            print_fail "Unsupported architecture: $arch"
            exit 1
            ;;
    esac

    case "$os" in
        linux)  os="linux" ;;
        darwin) os="darwin" ;;
        *)
            print_fail "Unsupported OS: $os"
            exit 1
            ;;
    esac

    echo "${os}_${arch}"
}

# ---------- preflight checks ----------

# Check if ~/.agency has a complete configuration (config.yaml with token + .env with API key)
has_full_config() {
    [ -f "$HOME/.agency/config.yaml" ] \
        && grep -q 'token:' "$HOME/.agency/config.yaml" 2>/dev/null \
        && [ -f "$HOME/.agency/.env" ] \
        && grep -qE '_API_KEY=.+' "$HOME/.agency/.env" 2>/dev/null
}

preflight_api_key_notice() {
    # Skip if we already have an API key (full config or just .env)
    has_full_config && return 0
    if [ -f "$HOME/.agency/.env" ] && grep -qE '_API_KEY=.+' "$HOME/.agency/.env" 2>/dev/null; then
        return 0
    fi

    # Only show interactive prompt if stdin is a terminal
    if [ -t 0 ]; then
        echo ""
        echo "  Agency requires an API key from at least one LLM provider:"
        echo "    - Anthropic (Claude)    — recommended"
        echo "    - OpenAI (GPT)          — full support"
        echo "    - Google (Gemini)       — free tier available"
        echo ""
        echo "  Get a key in ~2 minutes: see docs/getting-api-keys.md"
        echo ""
        printf "  Have your key ready? Press Enter to continue..."
        read -r
        echo ""
    fi
}

preflight_disk_space() {
    print_info "Checking disk space..."
    local avail_gb=""
    if df -BG "$HOME" >/dev/null 2>&1; then
        # GNU coreutils (Linux)
        avail_gb=$(df -BG "$HOME" 2>/dev/null | tail -1 | awk '{gsub(/G/,"",$4); print $4}')
    fi
    if [ -z "$avail_gb" ]; then
        # Fallback: df -k works on macOS and all POSIX systems
        local avail_kb
        avail_kb=$(df -k "$HOME" 2>/dev/null | tail -1 | awk '{print $4}')
        if [ -n "$avail_kb" ]; then
            avail_gb=$((avail_kb / 1048576))
        fi
    fi
    if [ -n "$avail_gb" ] && [ "$avail_gb" -lt 2 ] 2>/dev/null; then
        print_fail "Insufficient disk space: ${avail_gb}GB available, 2GB required."
        echo ""
        echo "  Free up space in $HOME and re-run this script."
        exit 1
    fi
    print_ok "Disk space: ${avail_gb:-unknown}GB available"
}

preflight_ram_check() {
    print_info "Checking available RAM..."
    local total_mb=""
    if [ -f /proc/meminfo ]; then
        # Linux
        local total_kb
        total_kb=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
        if [ -n "$total_kb" ]; then
            total_mb=$((total_kb / 1024))
        fi
    elif command -v sysctl &>/dev/null; then
        # macOS
        local total_bytes
        total_bytes=$(sysctl -n hw.memsize 2>/dev/null || sysctl -n hw.memtotal 2>/dev/null || echo "")
        if [ -n "$total_bytes" ]; then
            total_mb=$((total_bytes / 1048576))
        fi
    fi
    if [ -n "$total_mb" ] && [ "$total_mb" -lt 2048 ] 2>/dev/null; then
        print_fail "Insufficient RAM: ${total_mb}MB detected, 2048MB (2GB) required."
        exit 1
    fi
    local total_gb=""
    if [ -n "$total_mb" ]; then
        total_gb=$(( (total_mb + 512) / 1024 ))
        print_ok "RAM: ${total_gb}GB available"
    else
        print_info "Could not detect RAM (non-fatal)"
    fi
}

preflight_network_check() {
    print_info "Checking network connectivity..."
    if ! command -v curl &>/dev/null; then
        print_info "curl not found — skipping network check (will install later)"
        return 0
    fi
    if curl -fsSL --max-time 5 https://github.com > /dev/null 2>&1; then
        print_ok "Network: github.com is reachable"
    else
        print_fail "Cannot reach github.com. Check your internet connection and try again."
        exit 1
    fi
}

preflight_macos_provenance() {
    # macOS: if ~/.agency has com.apple.provenance, binaries placed there
    # will be SIGKILL'd on execution. Clean it up early, preserving .env.
    [ "$(uname -s)" = "Darwin" ] || return 0
    [ -d "$HOME/.agency" ] || return 0
    xattr -l "$HOME/.agency" 2>/dev/null | grep -q 'com.apple.provenance' || return 0

    print_info "Cleaning up macOS provenance attributes on ~/.agency..."
    # Save .env if it exists
    local saved_env=""
    if [ -f "$HOME/.agency/.env" ]; then
        saved_env=$(cat "$HOME/.agency/.env")
    fi
    rm -rf "$HOME/.agency"
    mkdir -p "$HOME/.agency"
    if [ -n "$saved_env" ]; then
        echo "$saved_env" > "$HOME/.agency/.env"
        chmod 600 "$HOME/.agency/.env"
    fi
    print_ok "Cleaned ~/.agency"
}

run_preflight() {
    print_header "Preflight Checks"
    preflight_macos_provenance
    preflight_api_key_notice
    preflight_disk_space
    preflight_ram_check
    preflight_network_check
}

# ---------- step functions ----------

ensure_curl() {
    if command -v curl &>/dev/null; then
        return 0
    fi
    print_info "Installing curl..."
    local pm
    pm=$(detect_pkg_manager)
    pkg_install "$pm" curl
}

install_agency_binary() {
    print_header "Installing Agency"

    mkdir -p "$INSTALL_DIR"

    local platform
    platform=$(detect_platform)

    # Try downloading a pre-built release binary
    local download_url=""
    local latest_version=""

    if command -v curl &>/dev/null; then
        # Get latest release tag
        latest_version=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
            | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//' || echo "")
    fi

    if [ -n "$latest_version" ]; then
        local version_num="${latest_version#v}"
        local archive_name="agency_${version_num}_${platform}.tar.gz"
        local checksums_name="agency_${version_num}_checksums.txt"
        download_url="https://github.com/${REPO}/releases/download/${latest_version}/${archive_name}"
        local checksums_url="https://github.com/${REPO}/releases/download/${latest_version}/${checksums_name}"

        local tmpdir
        tmpdir=$(mktemp -d)
        if run_with_spinner "Downloading Agency ${latest_version}..." \
                curl -fsSL "$download_url" -o "${tmpdir}/agency.tar.gz"; then

            # Verify checksum if available (supply-chain integrity)
            local checksum_verified=false
            if curl -fsSL "$checksums_url" -o "${tmpdir}/checksums.txt" 2>/dev/null; then
                local expected_hash
                expected_hash=$(grep "${archive_name}" "${tmpdir}/checksums.txt" | awk '{print $1}')
                if [ -n "$expected_hash" ]; then
                    local actual_hash
                    if command -v sha256sum &>/dev/null; then
                        actual_hash=$(sha256sum "${tmpdir}/agency.tar.gz" | awk '{print $1}')
                    elif command -v shasum &>/dev/null; then
                        actual_hash=$(shasum -a 256 "${tmpdir}/agency.tar.gz" | awk '{print $1}')
                    fi
                    if [ -n "$actual_hash" ]; then
                        if [ "$expected_hash" = "$actual_hash" ]; then
                            checksum_verified=true
                            print_ok "Checksum verified (SHA-256)"
                        else
                            rm -rf "$tmpdir"
                            print_fail "Checksum mismatch! Expected ${expected_hash:0:16}..., got ${actual_hash:0:16}..."
                            print_fail "The downloaded binary may be corrupted or tampered with."
                            print_info "Falling back to build from source..."
                            build_from_source
                            return $?
                        fi
                    fi
                fi
            fi
            if [ "$checksum_verified" = false ]; then
                print_info "No checksum file available — skipping verification"
            fi

            tar -xzf "${tmpdir}/agency.tar.gz" -C "$tmpdir"
            if [ -f "${tmpdir}/agency" ]; then
                mv "${tmpdir}/agency" "${INSTALL_DIR}/agency"
                chmod +x "${INSTALL_DIR}/agency"
                rm -rf "$tmpdir"
                print_ok "Installed to ${INSTALL_DIR}/agency"
                return 0
            fi
            rm -rf "$tmpdir"
        else
            rm -rf "$tmpdir"
            print_info "Release download failed — trying build from source..."
        fi
    else
        print_info "No release found — trying build from source..."
    fi

    # Fallback: build from source if Go is available
    build_from_source
}

build_from_source() {
    local script_dir
    script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
    local gateway_dir="${script_dir}/agency-gateway"

    if [ ! -d "$gateway_dir" ]; then
        print_fail "agency-gateway/ directory not found. Run this script from the cloned agency repository root."
        exit 1
    fi

    if ! command -v go &>/dev/null; then
        print_info "Go is not installed. Required to build from source."
        if command -v brew &>/dev/null; then
            if [ -t 0 ]; then
                printf "  Install Go via Homebrew? [Y/n] "
                read -r answer
                answer="${answer:-Y}"
            else
                answer="Y"
            fi
            if [[ "$answer" =~ ^[Yy] ]]; then
                run_with_spinner "Installing Go via Homebrew..." brew install go
            else
                print_fail "Go is required to build from source."
                exit 1
            fi
        else
            print_fail "Go is not installed and no pre-built release is available."
            echo ""
            echo "  Options:"
            echo "    1. Install Go from https://go.dev/dl/ and re-run this script"
            echo "    2. Download a release binary from https://github.com/${REPO}/releases"
            exit 1
        fi
    fi

    local go_version
    go_version=$(go version | awk '{print $3}')

    run_with_spinner "Building from source (${go_version})..." bash -c "cd '$gateway_dir' && make build"
    cp "${gateway_dir}/agency" "${INSTALL_DIR}/agency"
    chmod +x "${INSTALL_DIR}/agency"
    print_ok "Installed to ${INSTALL_DIR}/agency"
}

ensure_docker() {
    print_header "Checking Docker"

    if command -v docker &>/dev/null; then
        if docker info &>/dev/null 2>&1; then
            print_ok "$(docker --version | head -1)"
            return 0
        fi
        # Docker exists but daemon not running or no permissions
        print_info "Docker is installed but the daemon is not running or you lack permissions."
        if [ "$(uname -s)" = "Darwin" ]; then
            # macOS — Docker Desktop manages the daemon
            print_fail "Docker Desktop is not running."
            echo ""
            echo "  Open Docker Desktop and wait for it to start, then re-run this script."
            exit 1
        elif command -v systemctl &>/dev/null; then
            print_info "Starting Docker daemon..."
            sudo systemctl start docker
            # Add user to docker group if not already
            if ! groups | grep -q docker; then
                print_info "Adding $USER to docker group..."
                sudo usermod -aG docker "$USER"
                print_info "Group added. You may need to log out and back in, then re-run this script."
                print_info "Or run: newgrp docker && ./install.sh"
                exit 1
            fi
            if docker info &>/dev/null 2>&1; then
                print_ok "$(docker --version | head -1)"
                return 0
            fi
        fi
        print_fail "Could not start Docker. Start it manually and re-run this script."
        exit 1
    fi

    print_info "Docker not found. Installing..."

    local pm
    pm=$(detect_pkg_manager)
    case "$pm" in
        apt)
            # Docker official convenience script (works on Ubuntu, Debian, WSL)
            print_info "Installing Docker via official install script..."
            curl -fsSL https://get.docker.com | sudo sh
            sudo usermod -aG docker "$USER"
            sudo systemctl start docker || true
            ;;
        dnf)
            sudo dnf install -y -q dnf-plugins-core
            sudo dnf config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo
            sudo dnf install -y -q docker-ce docker-ce-cli containerd.io
            sudo systemctl start docker
            sudo usermod -aG docker "$USER"
            ;;
        pacman)
            pkg_install pacman docker
            sudo systemctl start docker
            sudo usermod -aG docker "$USER"
            ;;
        brew)
            print_fail "Docker Desktop is required on macOS."
            echo ""
            echo "Install it from https://docs.docker.com/desktop/install/mac-install/"
            exit 1
            ;;
        *)
            print_fail "Could not install Docker automatically."
            echo ""
            echo "Install it from https://docs.docker.com/get-docker/"
            exit 1
            ;;
    esac

    if command -v docker &>/dev/null; then
        print_ok "$(docker --version | head -1)"
        if ! docker info &>/dev/null 2>&1; then
            print_info "Docker installed. You need to log out and back in for group permissions."
            print_info "Then re-run: ./install.sh"
            exit 1
        fi
    else
        print_fail "Docker installation failed."
        exit 1
    fi
}

setup_path() {
    print_header "Configuring PATH"

    # Add install dir to PATH in shell profile if not already there
    local shell_rc=""
    case "${SHELL:-}" in
        */zsh)  shell_rc="$HOME/.zshrc" ;;
        */bash) shell_rc="$HOME/.bashrc" ;;
        */fish) shell_rc="$HOME/.config/fish/config.fish" ;;
    esac
    # Fall back to file existence if SHELL is unset or unrecognised
    if [ -z "$shell_rc" ] || [ ! -f "$shell_rc" ]; then
        if [ -f "$HOME/.zshrc" ]; then
            shell_rc="$HOME/.zshrc"
        elif [ -f "$HOME/.bashrc" ]; then
            shell_rc="$HOME/.bashrc"
        elif [ -f "$HOME/.profile" ]; then
            shell_rc="$HOME/.profile"
        fi
    fi

    if [ -n "$shell_rc" ]; then
        if ! grep -qF "$INSTALL_DIR" "$shell_rc" 2>/dev/null; then
            echo "" >> "$shell_rc"
            echo "# Agency Platform" >> "$shell_rc"
            if [[ "$shell_rc" == *"fish"* ]]; then
                echo "fish_add_path $INSTALL_DIR" >> "$shell_rc"
            else
                echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$shell_rc"
            fi
            print_info "Added $INSTALL_DIR to PATH in $shell_rc"
        fi
    fi

    # Expose for print_next_steps
    DETECTED_SHELL_RC="$shell_rc"

    # Make agency available for the rest of this script
    export PATH="$INSTALL_DIR:$PATH"

    if ! command -v agency &>/dev/null; then
        print_fail "agency command not found after install."
        exit 1
    fi
    # Verify the binary actually runs (catches macOS xattr issues)
    if run_agency --help &>/dev/null; then
        print_ok "agency command available"
    else
        print_fail "agency binary found but cannot execute."
        echo ""
        echo "  Try: codesign -s - --force ${INSTALL_DIR}/agency"
        exit 1
    fi

    # Check for conflicting agency binaries from old installs
    local other_agency
    other_agency=$(command -v agency 2>/dev/null || true)
    if [ -n "$other_agency" ] && [ "$other_agency" != "${INSTALL_DIR}/agency" ]; then
        echo ""
        print_info "Found another 'agency' at: $other_agency"
        if [[ "$other_agency" == *venv* ]] || file "$other_agency" 2>/dev/null | grep -q "text\|script\|Python"; then
            print_info "This looks like an old Python install and should be removed."
            if [ -t 0 ]; then
                local venv_dir
                venv_dir=$(dirname "$(dirname "$other_agency")")
                printf "  Remove old install at %s? [Y/n] " "$venv_dir"
                read -r answer
                answer="${answer:-Y}"
                if [[ "$answer" =~ ^[Yy] ]]; then
                    rm -rf "$venv_dir"
                    print_ok "Removed $venv_dir"
                fi
            fi
        else
            print_info "Make sure $INSTALL_DIR appears before $(dirname "$other_agency") in your PATH."
        fi
    fi
}

run_init() {
    print_header "Initializing Agency"

    if has_full_config; then
        # Full working config — config.yaml with token and .env with API key
        print_ok "Agency is already configured."
        if [ -t 0 ]; then
            printf "  Start fresh with a new configuration? [y/N] "
            read -r answer
            if [[ ! "$answer" =~ ^[Yy] ]]; then
                return 0
            fi
        else
            return 0
        fi
    elif [ -f "$HOME/.agency/.env" ] && grep -qE '_API_KEY=.+' "$HOME/.agency/.env" 2>/dev/null; then
        # .env has keys but no config.yaml — incomplete state
        print_info "Configuration is incomplete, but found existing API keys:"
        # Show which providers are configured
        local found_providers=""
        grep -oE '^[A-Z_]+_API_KEY' "$HOME/.agency/.env" 2>/dev/null | while read -r key_name; do
            case "$key_name" in
                ANTHROPIC_API_KEY) echo "    - Anthropic (Claude)" ;;
                OPENAI_API_KEY)    echo "    - OpenAI (GPT)" ;;
                GOOGLE_API_KEY)    echo "    - Google (Gemini)" ;;
            esac
        done
        if [ -t 0 ]; then
            printf "  Use existing keys and complete setup? [Y/n] "
            read -r answer
            answer="${answer:-Y}"
            if [[ "$answer" =~ ^[Yy] ]]; then
                # Source the .env so we can pass keys to agency init
                set +u
                source "$HOME/.agency/.env"
                set -u
            fi
        else
            set +u
            source "$HOME/.agency/.env"
            set -u
        fi
    fi

    # Pass any available API key to agency init to skip interactive prompts
    local -a init_args=()
    if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
        init_args+=(--provider anthropic --api-key "$ANTHROPIC_API_KEY")
    elif [ -n "${OPENAI_API_KEY:-}" ]; then
        init_args+=(--provider openai --api-key "$OPENAI_API_KEY")
    elif [ -n "${GOOGLE_API_KEY:-}" ]; then
        init_args+=(--provider google --api-key "$GOOGLE_API_KEY")
    fi
    # Pass --no-infra because the install script runs infra up separately
    # with its own progress reporting and error handling.
    init_args+=(--no-infra)
    if run_agency setup "${init_args[@]+"${init_args[@]}"}"; then
        print_ok "agency setup complete"
    else
        local ec=$?
        if [ $ec -eq 137 ] || [ $ec -eq 139 ]; then
            print_fail "agency binary was killed by the system (exit $ec)."
            echo ""
            echo "  On macOS this usually means Gatekeeper blocked the unsigned binary."
            echo "  Try: codesign -s - --force ${INSTALL_DIR}/agency"
            echo "  Then re-run: ./install.sh"
            exit 1
        fi
        print_fail "agency init failed (exit $ec)."
        exit 1
    fi
}

run_infra_up() {
    print_header "Starting Infrastructure"

    print_info "Pulling and starting containers (this may take a few minutes)..."
    if run_agency infra up; then
        print_ok "Infrastructure is running."
    else
        local ec=$?
        if [ $ec -eq 137 ] || [ $ec -eq 139 ]; then
            print_fail "agency binary was killed by the system (exit $ec)."
            echo ""
            echo "  On macOS this usually means Gatekeeper blocked the unsigned binary."
            echo "  Try: codesign -s - --force ${INSTALL_DIR}/agency"
            echo "  Then re-run: ./install.sh"
            exit 1
        fi
        print_info "Infrastructure failed to start. You can retry later with:"
        print_info "  agency infra up"
    fi
}

print_next_steps() {
    print_header "Done"

    echo "  Agency is installed."
    echo ""
    echo "  IMPORTANT: To use 'agency' in this terminal, run:"
    echo ""
    echo "    source ${DETECTED_SHELL_RC:-$HOME/.profile}"
    echo ""
    echo "  (New terminal windows will have it automatically.)"
    echo ""
    echo "  Next step — connect Agency to your AI coding tool:"
    echo ""
    echo "    claude mcp add agency -- ${INSTALL_DIR}/agency mcp-server"
    echo ""
    echo "  This gives Claude Code access to all Agency operations."
    echo "  After adding, Agency tools appear automatically in Claude Code."
    echo ""
    echo "  Quick start:"
    echo "    agency create my-agent"
    echo "    agency start my-agent"
    echo ""

    # Post-install suggestion for interactive sessions
    if [ -t 0 ]; then
        echo "  Installation complete! Run 'agency setup' to get started."
        echo ""
    fi
}

# ---------- main ----------

main() {
    echo ""
    echo "Agency Platform Installer"
    echo "========================="

    run_preflight
    ensure_curl
    install_agency_binary
    ensure_docker
    setup_path
    run_init
    run_infra_up
    print_next_steps
}

main
