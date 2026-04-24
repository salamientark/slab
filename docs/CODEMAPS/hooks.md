# Hooks codemap

## hooks/hook.sh

One invocation per Claude hook event. Installed in `~/.claude/settings.json` hooks block.

Flow:
1. `NOTCHCTL` resolved relative to script dir (fallback to PATH, then hardcoded `/home/madlab/PROJECTS/i3-notch/bin/notchctl`).
2. Parse stdin JSON with `jq`: `session_id`, `tool_name`, `tool_use_id`, `cwd`.
3. Enrich: `PPID` (claude CLI), `readlink /proc/$PPID/fd/0` for TTY, `tmux display-message` for session/window/pane.
4. For `PermissionRequest`, generate `REQUEST_ID` (uuid or date+ns).
5. Build `Event` JSON with `jq -cn`, pipe to `notchctl publish`, capture response.
6. On `PermissionRequest`: translate Decision → `hookSpecificOutput{hookEventName, decision:{behavior, reason}}` JSON on stdout.
7. `exit 0` always (publish failure silenced with `|| echo '{}'`).

### Gotcha

Publish failure → empty `{}` → `.allow` falsy → `decision:"deny"`. When daemon offline, **all permission prompts auto-deny**. Needs socket-existence guard to early-exit before jq compose so Claude falls back to native prompt.

### Installed events

`SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`, `PermissionRequest`.

## scripts/raise-notch.sh

17 lines. Keeps polybar notch bar on top — periodic `wmctrl` / `xdotool` raise. Backgrounded from polybar `launch.sh`.
