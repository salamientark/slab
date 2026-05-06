# 05 — Claude Code Hooks

Project-scoped in `.claude/settings.json`. Each event invokes one or more shell commands; Claude pipes a JSON object to stdin.

## Event → script map

| Event | Scripts (in order) | Daemon effect |
|-------|--------------------|----------------|
| `SessionStart` | `hook.sh SessionStart` | new session, status=idle |
| `UserPromptSubmit` | `hook.sh UserPromptSubmit` → `session-topic.sh` | status=running, Notify=false; topic file written |
| `PreToolUse` | `hook.sh PreToolUse` | status=running, LastTool=… |
| `PostToolUse` | `hook.sh PostToolUse` | status=running, LastTool=… |
| `Stop` | `hook.sh Stop` | status=idle, Notify=true if was running/awaiting |
| `SessionEnd` | `hook.sh SessionEnd` | status=ended (filtered out of snapshot) |
| `PermissionRequest` *(implicit via `Notification` rewrite)* | `hook.sh Notification` | blocks → dunstify Allow/Deny |

`PermissionRequest` is **not** a real Claude hook event in v0 — Claude sends `Notification` for permission prompts. `hook.sh` inspects the `.message` field and rewrites kind→`PermissionRequest` for permission-style messages. This is what makes the approval flow blocking.

## hook.sh dispatcher (hooks/hook.sh)

```
┌──────────────────────────────────────────────────────────────┐
│ entrypoint: ./hook.sh <KIND>                                 │
│                                                              │
│ 1. recursion guard: I3NOTCH_HOOK_INHIBIT=1 → exit 0          │
│    (set by session-topic.sh before invoking `claude -p`)     │
│                                                              │
│ 2. locate notchctl: script-relative ../bin/notchctl, then    │
│    PATH, then hard-coded fallback                            │
│                                                              │
│ 3. INPUT = `timeout 2 cat`   (bounded — never hang)          │
│    [ -z $INPUT ] && exit 0                                   │
│                                                              │
│ 4. if KIND==Notification && message looks like permission:   │
│       KIND := PermissionRequest                              │
│                                                              │
│ 5. extract via jq:                                           │
│    SESSION_ID, TOOL_NAME, TOOL_USE_ID, CWD                   │
│                                                              │
│ 6. enrich: PPID = claude CLI pid                             │
│            TTY  = readlink /proc/$PPID/fd/0                  │
│            TMUX_S/W/P from `tmux display-message`            │
│                                                              │
│ 7. if KIND==PermissionRequest: REQUEST_ID = uuid             │
│                                                              │
│ 8. build Event JSON (jq -cn)                                 │
│                                                              │
│ 9. RESP = `printf … | timeout 5 notchctl publish`            │
│                                                              │
│ 10. if KIND==PermissionRequest:                              │
│       translate Decision → hookSpecificOutput on stdout      │
└──────────────────────────────────────────────────────────────┘
```

### Why timeouts everywhere

`hook.sh` runs **synchronously inside Claude's event loop**. Hang here = stall the user's session. Defenses:

- `timeout 2 cat` — stdin can't block forever.
- `timeout 5 notchctl publish` — daemon down ≠ wedged Claude.
- recursion guard — `claude -p` invocations from `session-topic.sh` would otherwise refire `UserPromptSubmit` and pollute topic files / spawn another haiku call.

## session-topic.sh

UserPromptSubmit-only sibling. Writes a 3–5 word title for the statusline.

```
                  UserPromptSubmit JSON
                          │
                          ▼
                ┌──────────────────────┐
                │ recursion guard      │
                │ (INHIBIT=1 → exit)   │
                └──────────┬───────────┘
                           │
                           ▼
                ┌──────────────────────┐
                │ stdin (timeout 2)    │
                │ extract sid, prompt  │
                └──────────┬───────────┘
                           │
                .claude/topics/<sid>.txt absent?
                           │ no → exit
                           │ yes
                           ▼
                ┌──────────────────────┐
                │ FAST: first line of  │
                │ prompt, 50 chars →   │
                │ topic file (sync)    │
                └──────────┬───────────┘
                           │
                           ▼
                ┌──────────────────────────────────────┐
                │ setsid -f bash -c '…'                │
                │   I3NOTCH_HOOK_INHIBIT=1             │
                │   timeout 20 claude -p --model       │
                │     haiku-4-5 "Summarize 3-5 words"  │
                │   overwrite topic file               │
                │ </dev/null >/dev/null 2>&1           │
                └──────────────────────────────────────┘
                  ↑ detached, reparented to PID 1
```

Process-leak prevention rules (memory: `feedback_hooks.md`):

- bounded stdin read
- bounded refine
- `setsid -f` → daemon-style detach so closing the terminal doesn't keep stdin/out alive

## statusline.sh

Renders the in-Claude statusline footer:

```
[<cwd-basename>] <topic>
```

Reads `<project>/.claude/topics/<sid>.txt`. Empty → just `[basename]`.

## set-title.sh

Shipped target for a `/title` slash command (or manual). Writes to the **newest** topic file under `.claude/topics/` so that the in-progress session is updated. Falls back to a `manual-<ts>.txt` file if none exist.
