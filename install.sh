#!/usr/bin/env bash
# Agency Platform — install.sh
#
# This script does not install anything. It exists to gently object to
# the fact that you piped a URL from the internet into your shell, then
# show you the actual install commands so you can copy, inspect, and
# run them yourself.
#
# Nyancat frame and color palette adapted from klange/nyancat (NCSA
# license, github.com/klange/nyancat). Original Nyan Cat artwork by
# prguitarman.

set -u

# ---------- color detection ----------

detect_color_mode() {
    if [ -n "${NO_COLOR-}" ]; then echo "mono"; return; fi
    case "${COLORTERM-}" in
        truecolor|24bit) echo "256"; return ;;  # truecolor terms also accept 256
    esac
    local n=0
    if command -v tput >/dev/null 2>&1; then
        n=$(tput colors 2>/dev/null || echo 0)
    fi
    case "${TERM-}" in
        *-256color|xterm-kitty|alacritty) [ "$n" -lt 256 ] && n=256 ;;
    esac
    if   [ "$n" -ge 256 ]; then echo "256"
    elif [ "$n" -ge 16  ]; then echo "16"
    else echo "mono"
    fi
}

COLOR_MODE="$(detect_color_mode)"

# ---------- color tables ----------
#
# 256-color palette is taken verbatim from klange/nyancat's case 1
# (src/nyancat.c) so the cat reads identically to the canonical version
# in any 256-color terminal. The 16-color fallback maps each rainbow
# stripe and pop-tart hue to the nearest classic ANSI background that
# every terminal since the 1980s renders.

declare -A BG
RESET=$'\033[0m'

case "$COLOR_MODE" in
    256)
        BG[',']=$'\033[48;5;17m'   # dark-blue background
        BG['.']=$'\033[48;5;231m'  # white star
        BG["'"]=$'\033[48;5;16m'   # black outline
        BG['@']=$'\033[48;5;230m'  # tan pop-tart edge
        BG['$']=$'\033[48;5;175m'  # pink pop-tart fill
        BG['-']=$'\033[48;5;162m'  # magenta sprinkle
        BG['>']=$'\033[48;5;196m'  # red rainbow
        BG['&']=$'\033[48;5;214m'  # orange rainbow
        BG['+']=$'\033[48;5;226m'  # yellow rainbow
        BG['#']=$'\033[48;5;118m'  # green rainbow
        BG['=']=$'\033[48;5;33m'   # light-blue rainbow
        BG[';']=$'\033[48;5;19m'   # dark-blue rainbow
        BG['*']=$'\033[48;5;240m'  # gray cat face
        BG['%']=$'\033[48;5;175m'  # pink cheek
        CELL='  '
        ;;
    16)
        BG[',']=$'\033[44m'        # blue
        BG['.']=$'\033[107m'       # bright white
        BG["'"]=$'\033[40m'        # black
        BG['@']=$'\033[47m'        # white (tan-ish)
        BG['$']=$'\033[105m'       # bright magenta
        BG['-']=$'\033[101m'       # bright red
        BG['>']=$'\033[101m'       # bright red
        BG['&']=$'\033[43m'        # yellow (orange-ish)
        BG['+']=$'\033[103m'       # bright yellow
        BG['#']=$'\033[102m'       # bright green
        BG['=']=$'\033[104m'       # bright blue
        BG[';']=$'\033[44m'        # blue
        BG['*']=$'\033[100m'       # bright black (gray)
        BG['%']=$'\033[105m'       # bright magenta
        CELL='  '
        ;;
    *)
        # Mono fallback: render the frame as plain ASCII so the shape
        # is still legible without any color support.
        CELL=''
        ;;
esac

# ---------- the cat ----------
#
# 20 rows × 50 cols, lifted from frame0 of klange/nyancat's animation.c
# (cat-and-rainbow region, trimmed to keep the rendered width under
# ~100 cells). Each row is one string; each character maps to a color
# via the BG table above.

FRAMES=(
",,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,"
",,,,,,,,,,,,,,,,,,,,,,,,,''''''''''''''',,,,,,,,,,"
",,>>>>>>>>,,,,,,,,>>>>>>'@@@@@@@@@@@@@@@',,,,,,,,,"
">>>>>>>>>>>>>>>>>>>>>>>'@@@\$\$\$\$\$\$\$\$\$\$\$@@@',,,,,,,,"
">>&&&&&&&&>>>>>>>>&&&&&'@@\$\$\$\$\$-\$\$-\$\$\$\$@@',,,,,,,,"
"&&&&&&&&&&&&&&&&&&&&&&&'@\$\$-\$\$\$\$\$\$''\$-\$\$@','',,,,,"
"&&&&&&&&&&&&&&&&&&&&&&&'@\$\$\$\$\$\$\$\$'**'\$\$\$@''**',,,,"
"&&++++++++&&&&&&&&'''++'@\$\$\$\$\$-\$\$'***\$\$\$@'***',,,,"
"++++++++++++++++++**''+'@\$\$\$\$\$\$\$\$'***''''****',,,,"
"++++++++++++++++++'**'''@\$\$\$\$\$\$\$\$'***********',,,,"
"++########++++++++''**''@\$\$\$\$\$\$-'*************',,,"
"###################''**'@\$-\$\$\$\$\$'***.'****.'**',,,"
"####################''''@\$\$\$\$\$\$\$'***''**'*''**',,,"
"##========########====''@@\$\$\$-\$\$'*%%********%%',,,"
"======================='@@@\$\$\$\$\$\$'***''''''**',,,,"
"==;;;;;;;;.=======;;;;'''@@@@@@@@@'*********',,,,,"
";;;;;;;;;;;;;;;;;;;;;'***''''''''''''''''''',,,,,,"
";;;;;;;;;;;;;;;;;;;;;'**'','*',,,,,'*','**',,,,,,,"
";;,,,,,.,,;;;.;;;;,,,'''',,'',,,,,,,'',,'',,,,,,,,"
",,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,,"
)

render_cat() {
    local row out i ch
    for row in "${FRAMES[@]}"; do
        if [ "$COLOR_MODE" = "mono" ]; then
            printf '%s\n' "$row"
            continue
        fi
        out=""
        for ((i=0; i<${#row}; i++)); do
            ch="${row:$i:1}"
            out+="${BG[$ch]:-}${CELL}"
        done
        printf '%s%s\n' "$out" "$RESET"
    done
}

render_message() {
    local B="" R=""
    if [ "$COLOR_MODE" != "mono" ]; then
        B=$'\033[1m'
        R=$'\033[0m'
    fi
    printf '\n'
    printf '%sYou just piped an untrusted URL into your shell.%s\n' "$B" "$R"
    printf '\n'
    printf 'That gives whoever hosts this script the ability to run\n'
    printf 'arbitrary commands as you, on your machine, right now.\n'
    printf "We love the enthusiasm, but please don't do this — not for\n"
    printf 'us, and especially not for anyone else. Read scripts first.\n'
    printf '\n'
    printf '%sReal install paths for Agency%s (copy, inspect, run):\n' "$B" "$R"
    printf '\n'
    printf '  # Homebrew (macOS / Linux):\n'
    printf '  brew install geoffbelknap/tap/agency\n'
    printf '\n'
    printf '  # From source (installs host dependencies with brew/apt/dnf/yum/pacman/zypper when needed):\n'
    printf '  git clone https://github.com/geoffbelknap/agency.git\n'
    printf '  cd agency && make install\n'
    printf '\n'
    printf 'Either path leaves you with an %sagency%s binary on PATH.\n' "$B" "$R"
    printf 'Then run %sagency quickstart%s — it selects firecracker on Linux/WSL\n' "$B" "$R"
    printf 'and apple-vf-microvm on macOS Apple silicon.\n'
    printf '\n'
    printf 'Docs: https://github.com/geoffbelknap/agency\n'
    printf 'Cat:  klange/nyancat (NCSA), original art by prguitarman.\n'
    printf '\n'
}

render_cat
render_message
exit 0
