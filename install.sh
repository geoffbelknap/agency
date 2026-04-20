#!/usr/bin/env bash
# Agency Platform — install.sh
#
# You got here because something piped this script into your shell.
# This script does not install anything. It politely suggests you
# stop piping random URLs into bash and points you at the actual
# install paths, which you can read before running.

set -u

# ---------- color detection ----------
#
# Decide once how many colors the current terminal can render, then
# hand out matching ANSI escapes. No color → plain text; the art
# still renders legibly in grayscale via block characters and layout.

detect_color_mode() {
    if [ -n "${NO_COLOR-}" ]; then echo "mono"; return; fi
    case "${COLORTERM-}" in
        truecolor|24bit) echo "truecolor"; return ;;
    esac
    local n=0
    if command -v tput >/dev/null 2>&1; then
        n=$(tput colors 2>/dev/null || echo 0)
    fi
    case "${TERM-}" in
        *-256color|xterm-kitty|alacritty) n=${n:-256} ;;
    esac
    if   [ "$n" -ge 16777216 ]; then echo "truecolor"
    elif [ "$n" -ge 256       ]; then echo "256"
    elif [ "$n" -ge 8         ]; then echo "16"
    else echo "mono"
    fi
}

COLOR_MODE="$(detect_color_mode)"

# ANSI helpers — the truecolor path uses canonical nyan palette, the
# 256 path picks the closest xterm palette entries, and the 16 path
# falls back to named ANSI colors that exist on every terminal since
# the 1980s. "mono" keeps only bold/dim so the art is still readable.
esc()  { printf '\033[%sm' "$1"; }
if [ "$COLOR_MODE" = "mono" ]; then
    R=""; B=""; D=""
else
    R=$(esc 0)   # reset
    B=$(esc 1)   # bold
    D=$(esc 2)   # dim
fi

case "$COLOR_MODE" in
    truecolor)
        C_RED=$(esc   '38;2;255;0;51')
        C_ORA=$(esc   '38;2;255;127;0')
        C_YEL=$(esc   '38;2;255;255;0')
        C_GRN=$(esc   '38;2;51;204;0')
        C_BLU=$(esc   '38;2;51;153;255')
        C_PUR=$(esc   '38;2;153;51;255')
        C_PINK=$(esc  '38;2;255;153;204')
        C_SPR=$(esc   '38;2;255;51;153')
        C_CAT=$(esc   '38;2;204;204;204')
        C_FACE=$(esc  '38;2;51;51;51')
        C_STAR=$(esc  '38;2;221;221;221')
        ;;
    256)
        C_RED=$(esc   '38;5;196')
        C_ORA=$(esc   '38;5;208')
        C_YEL=$(esc   '38;5;226')
        C_GRN=$(esc   '38;5;46')
        C_BLU=$(esc   '38;5;39')
        C_PUR=$(esc   '38;5;93')
        C_PINK=$(esc  '38;5;219')
        C_SPR=$(esc   '38;5;199')
        C_CAT=$(esc   '38;5;252')
        C_FACE=$(esc  '38;5;236')
        C_STAR=$(esc  '38;5;253')
        ;;
    16)
        C_RED=$(esc   '31')
        C_ORA=$(esc   '33')   # no orange in 16 — yellow is the closest
        C_YEL=$(esc   '33')
        C_GRN=$(esc   '32')
        C_BLU=$(esc   '34')
        C_PUR=$(esc   '35')
        C_PINK=$(esc  '95')   # bright magenta
        C_SPR=$(esc   '35')
        C_CAT=$(esc   '37')
        C_FACE=$(esc  '90')
        C_STAR=$(esc  '97')
        ;;
    *)
        C_RED=""; C_ORA=""; C_YEL=""; C_GRN=""; C_BLU=""; C_PUR=""
        C_PINK=""; C_SPR=""; C_CAT=""; C_FACE=""; C_STAR=""
        ;;
esac

# ---------- art ----------
#
# Layout: a trailing rainbow on the left, a pop-tart cat on the right.
# The rainbow extends under the cat in the bottom two rows so the
# shape reads as motion. Sprinkles in magenta, face in dark gray.
# Width is intentionally ~60 columns to fit narrow terminals without
# line-wrapping chopping up the rainbow.

print_art() {
    local rb='━━━━━━━━━━━━━━━━━━━━━━━━━━━━━'      # 29 horizontal bars
    printf "\n"
    printf "   ${C_STAR}·${R}  ${C_STAR}✦${R}   ${C_STAR}*${R}    ${C_STAR}.${R}   ${C_STAR}✦${R}     ${C_STAR}·${R}  ${C_STAR}·${R}      ${C_STAR}⭒${R}\n"
    printf "                               ${C_CAT}╭────────╮${R}\n"
    printf "                               ${C_CAT}│${R}  ${C_FACE}◕ᴗ◕${R}   ${C_CAT}│${R}\n"
    printf "${C_RED}${rb}${R}  ${C_YEL}┏${R}${C_PINK}━━━━━━━━${R}${C_YEL}┓${R}\n"
    printf "${C_ORA}${rb}${R}  ${C_YEL}┃${R}${C_PINK} ${C_SPR}★${C_PINK}  ${C_SPR}★${C_PINK} ${C_SPR}★${C_PINK} ${C_YEL}┃${R}\n"
    printf "${C_YEL}${rb}${R}  ${C_YEL}┃${R}${C_PINK}${C_SPR}★${C_PINK}  ${C_SPR}★${C_PINK} ${C_SPR}★${C_PINK}  ${C_YEL}┃${R}\n"
    printf "${C_GRN}${rb}${R}  ${C_YEL}┃${R}${C_PINK} ${C_SPR}★${C_PINK}  ${C_SPR}★${C_PINK} ${C_SPR}★${C_PINK} ${C_YEL}┃${R}\n"
    printf "${C_BLU}${rb}${R}  ${C_YEL}┗${R}${C_PINK}━━━━━━━━${R}${C_YEL}┛${R}\n"
    printf "${C_PUR}${rb}${R}   ${C_CAT}╵${R}${D}  ${R}${C_CAT}╵${R}  ${C_CAT}╵${R}${D}  ${R}${C_CAT}╵${R}\n"
    printf "   ${C_STAR}⭒${R}   ${C_STAR}.${R}  ${C_STAR}✦${R}  ${C_STAR}.${R} ${C_STAR}*${R}  ${C_STAR}✦${R}    ${C_STAR}.${R}     ${C_STAR}·${R}\n"
    printf "\n"
}

# ---------- message ----------
#
# Scold is friendly but specific — name the risk (arbitrary code
# execution as the invoking user), then show the real commands the
# reader can inspect before running.

print_message() {
    printf "${B}You just piped an untrusted URL into your shell.${R}\n"
    printf "\n"
    printf "That gives whoever hosts this script the ability to run\n"
    printf "arbitrary commands as you, on your machine, right now.\n"
    printf "We love the enthusiasm, but please don't do this — not for\n"
    printf "us, and especially not for anyone else. Read scripts first.\n"
    printf "\n"
    printf "${B}Real install paths for Agency${R} (copy, inspect, run):\n"
    printf "\n"
    printf "  ${C_GRN}# Homebrew (macOS / Linux):${R}\n"
    printf "  brew install geoffbelknap/tap/agency\n"
    printf "\n"
    printf "  ${C_GRN}# From source:${R}\n"
    printf "  git clone https://github.com/geoffbelknap/agency.git\n"
    printf "  cd agency && make install\n"
    printf "\n"
    printf "Either path leaves you with an ${B}agency${R} binary on PATH.\n"
    printf "Then run ${B}agency setup${R} — it detects your container\n"
    printf "backend (podman, docker, containerd) and configures itself.\n"
    printf "\n"
    printf "Docs: https://github.com/geoffbelknap/agency\n"
    printf "\n"
}

print_art
print_message
exit 0
