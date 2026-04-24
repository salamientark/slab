#!/usr/bin/env bash
# i3-notch hook dispatcher.
#
# Installed in ~/.claude/settings.json hooks config. One invocation per hook
# event. Reads Claude's hook JSON from stdin, enriches it with process/tmux
# context, forwards to notchd via notchctl, and (for PermissionRequest)
# emits the decision JSON back on stdout.
#
# Usage (from settings.json):
#   { "type": "command", "command": "/path/to/hook.sh SessionStart" }

set -euo pipefail

# Locate notchctl relative to this script so hooks don't depend on PATH.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NOTCHCTL="${SCRIPT_DIR}/../bin/notchctl"
[ -x "$NOTCHCTL" ] || NOTCHCTL="$(command -v notchctl || echo /home/madlab/PROJECTS/i3-notch/bin/notchctl)"

KIND="${1:-unknown}"
INPUT="$(cat)"

# Required: jq and notchctl on PATH.
SESSION_ID="$(printf '%s' "$INPUT" | jq -r '.session_id // .sessionId // ""')"
TOOL_NAME="$(printf '%s' "$INPUT" | jq -r '.tool_name // ""')"
TOOL_USE_ID="$(printf '%s' "$INPUT" | jq -r '.tool_use_id // ""')"
CWD="$(printf '%s' "$INPUT" | jq -r '.cwd // ""')"
[ -z "$CWD" ] && CWD="$PWD"

# PPID = the claude CLI process invoking the hook. That's what we jump to.
CLAUDE_PID="$PPID"
TTY_PATH="$(readlink -f /proc/$CLAUDE_PID/fd/0 2>/dev/null || true)"

TMUX_S="" TMUX_W="" TMUX_P=""
if [ -n "${TMUX:-}" ] && command -v tmux >/dev/null 2>&1; then
    TMUX_S="$(tmux display-message -p '#{session_name}' 2>/dev/null || true)"
    TMUX_W="$(tmux display-message -p '#{window_index}' 2>/dev/null || true)"
    TMUX_P="$(tmux display-message -p '#{pane_index}' 2>/dev/null || true)"
fi

NOW="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"

# Construct event. PermissionRequest needs a request_id so daemon can block.
REQUEST_ID=""
if [ "$KIND" = "PermissionRequest" ]; then
    REQUEST_ID="$(cat /proc/sys/kernel/random/uuid 2>/dev/null || date +%s%N)"
fi

EVENT="$(jq -cn \
    --arg kind "$KIND" \
    --arg sid  "$SESSION_ID" \
    --arg cwd  "$CWD" \
    --arg tty  "$TTY_PATH" \
    --arg tool "$TOOL_NAME" \
    --arg tuid "$TOOL_USE_ID" \
    --arg rid  "$REQUEST_ID" \
    --arg tms  "$TMUX_S" \
    --arg tmw  "$TMUX_W" \
    --arg tmp  "$TMUX_P" \
    --argjson pid "$CLAUDE_PID" \
    --arg now "$NOW" \
    '{kind:$kind, session_id:$sid, pid:$pid, cwd:$cwd, tty:$tty,
      tool_name:$tool, tool_use_id:$tuid, request_id:$rid,
      tmux_session:$tms, tmux_window:$tmw, tmux_pane:$tmp,
      timestamp:$now}')"

RESP="$(printf '%s\n' "$EVENT" | "$NOTCHCTL" publish 2>/dev/null || echo '{}')"

if [ "$KIND" = "PermissionRequest" ]; then
    # Translate Decision → hookSpecificOutput JSON expected by Claude Code.
    ALLOW="$(printf '%s' "$RESP" | jq -r '.allow // false')"
    REASON="$(printf '%s' "$RESP" | jq -r '.reason // ""')"
    if [ "$ALLOW" = "true" ]; then
        DECISION="allow"
    else
        DECISION="deny"
    fi
    jq -cn --arg d "$DECISION" --arg r "$REASON" \
        '{hookSpecificOutput:{hookEventName:"PermissionRequest",
                              decision:{behavior:$d, reason:$r}}}'
fi

exit 0
