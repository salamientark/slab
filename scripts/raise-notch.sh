#!/usr/bin/env bash
# Keep the polybar 'notch' bar on top of all windows.
# Listens to i3 window events and re-raises the bar on every map/focus.

raise() {
  for w in $(xdotool search --name '^polybar-notch_' 2>/dev/null); do
    xdotool windowraise "$w" 2>/dev/null || true
  done
}

# Initial raise
raise

# Subscribe to i3 window events; raise on each.
i3-msg -t subscribe -m '[ "window" ]' 2>/dev/null | while read -r _; do
  raise
done
