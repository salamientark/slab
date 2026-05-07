#!/usr/bin/env bash
# Tests for bin/i3-notch-toggle subcommand dispatch.
# Strategy: stub systemctl/polybar/pkill/notify-send/pgrep on PATH so we
# observe what the script tries to do without touching the real system.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"
TOGGLE="$ROOT/scripts/i3-notch-toggle"

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

setup_stubs() {
  STUBDIR="$(mktemp -d)"
  LOG="$STUBDIR/calls.log"
  : > "$LOG"
  # Configurable stub responses via env vars.
  for cmd in systemctl polybar pkill notify-send pgrep nohup; do
    cat > "$STUBDIR/$cmd" <<EOF
#!/usr/bin/env bash
printf '%s %s\n' "$cmd" "\$*" >> "$LOG"
case "$cmd" in
  systemctl)
    if [ "\$*" = "--user is-active --quiet i3-notch" ]; then
      exit \${STUB_SYSTEMCTL_ACTIVE:-1}
    fi
    exit 0
    ;;
  pgrep)
    exit \${STUB_PGREP_RC:-1}
    ;;
  *)
    exit 0
    ;;
esac
EOF
    chmod +x "$STUBDIR/$cmd"
  done
  export PATH="$STUBDIR:$PATH"
}

teardown_stubs() {
  rm -rf "$STUBDIR"
  unset STUB_SYSTEMCTL_ACTIVE STUB_PGREP_RC
}

case_status_inactive() {
  setup_stubs
  export STUB_SYSTEMCTL_ACTIVE=1 STUB_PGREP_RC=1
  local out rc
  out="$("$TOGGLE" status 2>&1)"; rc=$?
  teardown_stubs
  # systemctl-style: inactive returns 3.
  [ "$rc" -eq 3 ] && printf '%s' "$out" | grep -qE '^i3-notch: inactive$'
}

case_status_active() {
  setup_stubs
  export STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0
  local out rc
  out="$("$TOGGLE" status 2>&1)"; rc=$?
  teardown_stubs
  [ "$rc" -eq 0 ] && printf '%s' "$out" | grep -qE '^i3-notch: active$'
}

case_start_when_stopped() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=1 STUB_PGREP_RC=1 \
    "$TOGGLE" start >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user start i3-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_stop_when_running() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0 \
    "$TOGGLE" stop >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user stop i3-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_toggle_starts_when_stopped() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=1 STUB_PGREP_RC=1 \
    "$TOGGLE" toggle >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user start i3-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_toggle_no_arg_starts_when_stopped() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=1 STUB_PGREP_RC=1 \
    "$TOGGLE" >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user start i3-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_start_starts_raiser_unit() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=1 STUB_PGREP_RC=1 \
    "$TOGGLE" start >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user start .*i3-notch-raise' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_stop_stops_raiser_unit() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0 \
    "$TOGGLE" stop >/dev/null 2>&1
  local rc=$?
  grep -q 'systemctl --user stop .*i3-notch-raise' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_stop_pkill_polybar_uses_exact_match() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0 \
    "$TOGGLE" stop >/dev/null 2>&1
  local rc=$?
  # Must invoke pkill with -fx for polybar (exact full-cmdline match).
  grep -q 'pkill -fx polybar notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_stop_pkill_raise_anchored() {
  setup_stubs
  STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0 \
    "$TOGGLE" stop >/dev/null 2>&1
  local rc=$?
  # Pattern must require a bash invocation prefix so editors with the file
  # open don't match. Reject the loose `pkill -f raise-notch.sh` form.
  if grep -qE 'pkill -f raise-notch\.sh\b' "$LOG"; then
    teardown_stubs; return 1
  fi
  grep -qE 'pkill -f .*bash.*raise-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_stop_skips_raise_when_unit_missing() {
  setup_stubs
  # Override systemctl: list-unit-files exits 1 (no such unit); is-active 0.
  cat > "$STUBDIR/systemctl" <<EOF
#!/usr/bin/env bash
printf '%s %s\n' "systemctl" "\$*" >> "$LOG"
case "\$*" in
  "--user is-active --quiet i3-notch") exit \${STUB_SYSTEMCTL_ACTIVE:-1} ;;
  "--user list-unit-files i3-notch-raise.service") exit 1 ;;
  *) exit 0 ;;
esac
EOF
  chmod +x "$STUBDIR/systemctl"
  STUB_SYSTEMCTL_ACTIVE=0 STUB_PGREP_RC=0 \
    "$TOGGLE" stop >/dev/null 2>&1
  local rc=$?
  if grep -qE 'systemctl --user stop +i3-notch-raise' "$LOG"; then
    teardown_stubs; return 1
  fi
  grep -q 'systemctl --user stop i3-notch' "$LOG" || { teardown_stubs; return 1; }
  teardown_stubs
  [ "$rc" -eq 0 ]
}

case_unknown_subcommand_errors() {
  setup_stubs
  "$TOGGLE" frobnicate >/dev/null 2>&1
  local rc=$?
  teardown_stubs
  [ "$rc" -ne 0 ]
}

echo "i3-notch-toggle subcommand tests"
run_case "status reports inactive when off" case_status_inactive
run_case "status reports active when on" case_status_active
run_case "start when stopped invokes systemctl start" case_start_when_stopped
run_case "stop when running invokes systemctl stop" case_stop_when_running
run_case "toggle starts when stopped" case_toggle_starts_when_stopped
run_case "no-arg toggles (back-compat)" case_toggle_no_arg_starts_when_stopped
run_case "start invokes raiser unit" case_start_starts_raiser_unit
run_case "stop invokes raiser unit" case_stop_stops_raiser_unit
run_case "stop pkill polybar uses exact match" case_stop_pkill_polybar_uses_exact_match
run_case "stop pkill raise-notch.sh anchored on bash invocation" case_stop_pkill_raise_anchored
run_case "stop skips raise unit when not installed" case_stop_skips_raise_when_unit_missing
run_case "unknown subcommand exits non-zero" case_unknown_subcommand_errors

echo
echo "passed: $PASS  failed: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  printf 'failures:\n'
  for f in "${FAILS[@]}"; do printf '  - %s\n' "$f"; done
  exit 1
fi
