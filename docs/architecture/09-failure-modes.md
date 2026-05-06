# 09 — Failure Modes & Recovery

## Hook-side hangs (would wedge Claude)

| Scenario | Defense |
|----------|---------|
| stdin never closes | `timeout 2 cat` |
| notchd down | `timeout 5 notchctl publish` → empty resp; PermissionRequest defaults to **deny** because `.allow // false` |
| haiku refine takes forever | `timeout 20 claude -p` inside `setsid -f` detached child |
| recursive hook fire from `claude -p` | `I3NOTCH_HOOK_INHIBIT=1` env in inner exec |
| zombie hook child holding TTY open | `setsid -f` detaches and reparents to init |

Memory: `feedback_hooks.md` enshrines these as required for any hook script.

## Daemon-side state drift

| Symptom | Cause | Recovery |
|---------|-------|----------|
| chip stuck on `running` | hook for `Stop` lost | livenessProbe demotes to `idle` after 5m |
| chip stuck on `awaiting` | dunstify died, decide-role never came | `approvalTimeout=30s` returns deny; if state still drifted, livenessProbe at 10m |
| chip lingers after terminal closed | `Stop`/`SessionEnd` lost | `kill(pid,0) != nil` → delete from map (every 2s) |
| sessions lost on daemon restart | n/a | `loadState` reads `state.json`; `BootstrapFromProc` re-seeds from `/proc` + transcripts |
| stale socket file from crash | n/a | `os.Remove(socketPath)` before `Listen` |

## Approval race: dunst vs notchctl decide

Both can land a Decision into the same channel. Channel is buffered 1; `select { case ch<-dec: default: }` means second writer drops harmlessly. Pending map is checked-and-deleted under `d.mu`, so only one delete-claimer wins.

## Bootstrap heuristic limits

`BootstrapFromProc` matches Claude project dirs by `cwd` with `/`/`.` → `-` substitution. Fragile if Claude changes its encoding. Worst case: bootstrap finds nothing → daemon starts cold; first hook event populates it normally.

PID-to-transcript pairing is best-effort:

```
sort pids ascending  ↔  sort transcripts mtime descending
zip i:i  → assign sid; falls back to pid-N placeholder if transcripts < pids
```

If wrong sid is paired to a pid, the next real hook event with the actual `session_id` will create a *new* session entry (the placeholder is leaked but pruned by livenessProbe when the Claude pid dies).

## Subscriber backpressure

Subscriber chan is buffered 8. On overflow, snapshot is **silently dropped**. Polybar will catch up on the next state change; in the (rare) case there is none, the bar shows stale state until next event. Acceptable per design.

## Known traps documented in memory

- **Hook process leak** (1117, 1130) — fixed by `setsid -f` + bounded timeouts.
- **Sessions stuck running** (1119, S6/S7/S8) — root cause: missing `Stop`/`Notification` hooks; mitigations:
  1. `Notification` hook present (rewrite path)
  2. `Stop` always wired
  3. stale demotion in livenessProbe
- **Process-tree-walk loops** — capped at 8 hops in both `xdotoolFindWindow` and `resolvePIDWorkspace`.

## What is *not* defended

- Wayland: `xdotool` and `xdotoolSearchAll` are pure X11. No fallback.
- Multiple notchd instances: socket bind would fail; second instance dies. No leader election.
- Disk full: `saveStateLocked` silently swallows write errors.
- Snapshot publish never blocks → permanently slow subscriber loses state without log.
- `dunstify` not installed: `askViaDunst` returns error, decision channel gets `Decision{}` (Allow=false, no reason). Effectively a deny — fine but silent.
