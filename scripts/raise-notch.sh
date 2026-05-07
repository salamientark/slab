#!/usr/bin/env bash
# Keep the polybar 'notch' bar on top of all windows.
#
# Polybar's notch uses override-redirect=true, so the WM ignores its stacking.
# Without this raiser, fullscreen / newly-mapped windows draw over it. Listen
# to i3 IPC and re-raise on any window or workspace event.

set -u

raise() {
  local ids w
  ids=$(timeout 1 xdotool search --name '^polybar-notch_' 2>/dev/null) || return 0
  for w in $ids; do
    timeout 1 xdotool windowraise "$w" 2>/dev/null || true
  done
}

# Initial raise so the bar pops above whatever was already on screen.
raise

# Workspace switches don't always fire a window event; subscribe to both so
# fullscreen toggles and workspace navigation also trigger a re-raise.
exec i3-msg -t subscribe -m '[ "window", "workspace" ]' 2>/dev/null | while read -r _; do
  raise
done
