#!/usr/bin/env bash
# Tests for hooks/hook.sh socket-presence guard.
# Run: bash tests/hook_test.sh

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"
HOOK="$ROOT/hooks/hook.sh"

PASS=0
FAIL=0
FAILS=()

run_case() {
  local name="$1"; shift
  if "$@"; then
    PASS=$((PASS+1))
    printf '  ok  %s\n' "$name"
  else
    FAIL=$((FAIL+1))
    FAILS+=("$name")
    printf '  FAIL %s\n' "$name"
  fi
}

# Setup isolated XDG_RUNTIME_DIR with no socket present.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export XDG_RUNTIME_DIR="$TMP"
# Ensure no socket file exists.
rm -f "$TMP/i3-notch.sock"

INPUT='{"session_id":"s1","tool_name":"Bash","cwd":"/tmp"}'

# Case 1: socket missing + KIND=SessionStart → exit 0, empty stdout.
case_session_start_no_socket() {
  local out rc
  out="$(printf '%s' "$INPUT" | "$HOOK" SessionStart 2>/dev/null)"
  rc=$?
  [ "$rc" -eq 0 ] && [ -z "$out" ]
}

# Case 2: socket missing + KIND=PermissionRequest → exit 0, NO decision JSON.
# Critical: emitting deny here would auto-deny every prompt when daemon is off.
case_permission_no_socket_no_decision() {
  local out rc
  out="$(printf '%s' "$INPUT" | "$HOOK" PermissionRequest 2>/dev/null)"
  rc=$?
  if [ "$rc" -ne 0 ]; then return 1; fi
  # Must NOT emit decision JSON. If output is non-empty, fail.
  if [ -n "$out" ]; then
    printf '    unexpected stdout: %s\n' "$out"
    return 1
  fi
}

# Case 3: socket missing + KIND=Notification (permission-style) → no decision.
case_notification_promoted_no_socket() {
  local out rc
  out="$(printf '{"session_id":"s1","message":"Claude needs your permission"}' | "$HOOK" Notification 2>/dev/null)"
  rc=$?
  [ "$rc" -eq 0 ] && [ -z "$out" ]
}

echo "hook.sh socket-guard tests"
run_case "SessionStart with no socket exits 0 silently" case_session_start_no_socket
run_case "PermissionRequest with no socket emits no decision" case_permission_no_socket_no_decision
run_case "Notification (permission-style) with no socket emits no decision" case_notification_promoted_no_socket

echo
echo "passed: $PASS  failed: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failures:\n'
  for f in "${FAILS[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
