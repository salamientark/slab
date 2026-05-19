#!/usr/bin/env bash
# i3-notch installer — idempotent, marker-block based.
# See docs/install.md for full design.

set -euo pipefail

# ---- paths & constants ----------------------------------------------------
readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly BIN_DIR="${REPO_ROOT}/bin"
readonly DIST_DIR="${REPO_ROOT}/dist"

readonly USER_BIN="${HOME}/.local/bin"
readonly POLYBAR_DIR="${HOME}/.config/polybar"
readonly SYSTEMD_USER_DIR="${HOME}/.config/systemd/user"
readonly CLAUDE_SETTINGS="${HOME}/.claude/settings.json"

readonly MARKER_START_HASH="# >>> i3-notch >>>"
readonly MARKER_END_HASH="# <<< i3-notch <<<"
readonly MARKER_START_SEMI="; >>> i3-notch >>>"
readonly MARKER_END_SEMI="; <<< i3-notch <<<"

readonly TS="$(date +%Y%m%d-%H%M%S)"

WITH_HOOKS=0
for arg in "$@"; do
  case "$arg" in
    --with-hooks) WITH_HOOKS=1 ;;
    -h|--help)
      cat <<EOF
Usage: $0 [--with-hooks]
  --with-hooks    Also install Claude Code hooks block into ~/.claude/settings.json
EOF
      exit 0 ;;
    *) echo "unknown arg: $arg" >&2; exit 2 ;;
  esac
done

# ---- helpers --------------------------------------------------------------
log()  { printf '[i3-notch] %s\n' "$*"; }
die()  { printf '[i3-notch] ERROR: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "missing prereq: $1"; }

backup() {
  local f="$1"
  [[ -f "$f" ]] || return 0
  local bak="${f}.bak-${TS}"
  cp -p "$f" "$bak"
  log "backup: $f → $bak"
}

# Replace-or-append marker block. Args: target_file, content_file, comment_char
# comment_char: '#' or ';'
apply_block() {
  local target="$1" content="$2" cc="$3"
  local start end
  if [[ "$cc" == ";" ]]; then
    start="$MARKER_START_SEMI"; end="$MARKER_END_SEMI"
  else
    start="$MARKER_START_HASH"; end="$MARKER_END_HASH"
  fi

  mkdir -p "$(dirname "$target")"
  touch "$target"
  backup "$target"

  if grep -qF "$start" "$target"; then
    # strip old block
    sed -i "/$(printf '%s' "$start" | sed 's/[][\/.*^$]/\\&/g')/,/$(printf '%s' "$end" | sed 's/[][\/.*^$]/\\&/g')/d" "$target"
    log "replaced existing block in $target"
  fi

  # trim trailing blank then append
  printf '\n' >> "$target"
  cat "$content" >> "$target"
  log "wrote block → $target"
}

# Substitute placeholders into a template, emit to stdout.
substitute() {
  local tpl="$1"
  sed -e "s|__I3NOTCH_BIN__|${BIN_DIR}|g" \
      -e "s|__I3NOTCH_ROOT__|${REPO_ROOT}|g" \
      "$tpl"
}

# ---- prereq checks --------------------------------------------------------
log "checking prereqs…"
need go
need polybar
need i3
need xdotool
need systemctl
[[ $WITH_HOOKS -eq 1 ]] && need jq

# ---- build ----------------------------------------------------------------
log "building binaries…"
( cd "$REPO_ROOT" && go build -o "$BIN_DIR/notchd" ./cmd/notchd )
( cd "$REPO_ROOT" && go build -o "$BIN_DIR/notchctl" ./cmd/notchctl )

# ---- install binaries (symlink) ------------------------------------------
mkdir -p "$USER_BIN"
ln -sfn "$BIN_DIR/notchd"   "$USER_BIN/notchd"
ln -sfn "$BIN_DIR/notchctl" "$USER_BIN/notchctl"
log "symlinked binaries → $USER_BIN"

# ---- systemd user unit ----------------------------------------------------
mkdir -p "$SYSTEMD_USER_DIR"
# Substitute binary path in unit (unit references /usr/bin/notchd by default).
sed -e "s|/usr/bin/notchd|${BIN_DIR}/notchd|g" \
    "$DIST_DIR/systemd/i3-notch.service" \
    > "$SYSTEMD_USER_DIR/i3-notch.service"
substitute "$DIST_DIR/systemd/i3-notch-raise.service" \
    > "$SYSTEMD_USER_DIR/i3-notch-raise.service"
systemctl --user daemon-reload
systemctl --user enable --now i3-notch.service
systemctl --user enable --now i3-notch-raise.service
log "systemd units enabled + started"

# ---- polybar config -------------------------------------------------------
tmp_notch="$(mktemp)"
tmp_launch="$(mktemp)"
trap 'rm -f "$tmp_notch" "$tmp_launch"' EXIT

substitute "$DIST_DIR/polybar/notch.conf"        > "$tmp_notch"
substitute "$DIST_DIR/polybar/launch-snippet.sh" > "$tmp_launch"

apply_block "$POLYBAR_DIR/config.ini" "$tmp_notch"  ";"
apply_block "$POLYBAR_DIR/launch.sh"  "$tmp_launch" "#"
chmod +x "$POLYBAR_DIR/launch.sh"

# ---- claude hooks (optional) ---------------------------------------------
if [[ $WITH_HOOKS -eq 1 ]]; then
  die "--with-hooks is not yet implemented; see docs/install.md and merge the hooks block manually for now"
fi

# ---- done -----------------------------------------------------------------
cat <<EOF

[i3-notch] install complete.
  binaries:  $USER_BIN/notchd, $USER_BIN/notchctl
  service:   systemctl --user status i3-notch
  polybar:   reload with \`$POLYBAR_DIR/launch.sh\`
  uninstall: see docs/install.md (no uninstall.sh yet — manual marker-block removal)

Backups suffixed .bak-$TS
EOF
