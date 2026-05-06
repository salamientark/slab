# 04 — State Machines

Three FSMs: per-session status, per-approval lifecycle, daemon process lifecycle.

## A. Session status FSM

Drives the chip color / glyph in polybar. Implemented in `daemon.go:applyEvent` + `livenessProbe`.

```
                  ┌──────────────────────────┐
                  │                          │
                  │      (no record)         │
                  │                          │
                  └─────────────┬────────────┘
                                │ SessionStart
                                │  / BootstrapFromProc
                                ▼
        ┌────────────────────────────────────────────────┐
        │                    idle                        │
        │  (default; also: Stop sets Notify=true)        │
        └─┬───────────┬──────────┬──────────────────┬────┘
          │           │          │                  │
          │ User-     │ PreTool/ │ Permission-      │ pid dies
          │ Prompt-   │ PostTool │ Request          │ /5m no event
          │ Submit    │          │                  │ (running)
          │           │          │                  │ /10m no event
          ▼           ▼          ▼                  │ (awaiting)
   ┌──────────────────────────┐  ┌───────────────┐  │
   │         running          │  │   awaiting    │  │
   │  (Notify cleared on      │  │  (blocking;   │  │
   │   UserPromptSubmit)      │  │   one-of-N    │  │
   │                          │  │   pending)    │  │
   └─┬────────────────┬───────┘  └───┬───────────┘  │
     │                │              │              │
     │ Stop           │ Permission-  │ Decision     │
     │ → Notify=true  │ Request      │ received     │
     ▼                ▼              ▼              │
   ┌────────────────────────────────────────────────┘
   │                   idle (Notify=true if from running/awaiting)
   │
   │ SessionEnd
   ▼
 ┌───────────┐
 │   ended   │  (excluded from snapshot; eligible for GC via livenessProbe)
 └───────────┘
```

Notes:

- `Notification` events are intentionally inert — `hook.sh` rewrites permission-style notifications to `PermissionRequest` (see `hooks/hook.sh:31-38`) before the daemon ever sees them.
- `Notify` (the "completed-unseen" yellow ✔ chip) is set by `Stop` if the prior status was running/awaiting. Cleared by `UserPromptSubmit`, `notchctl jump` (via `cmd=ack`), or `cmd=ack` direct.
- `livenessProbe` runs every 2s: `kill(pid, 0)` to detect dead pids → delete; otherwise demote stale running/awaiting back to idle.

### Status rank (snapshot worst-of)

```
awaiting (3) > running (2) > idle (1) > ended (0; filtered)
```

## B. Approval FSM (per PermissionRequest)

Implemented in `daemon.go:handleApproval`.

```
                ┌────────────────────────┐
                │  Event arrives on      │
                │  publish conn          │
                └───────────┬────────────┘
                            │
                            ▼
              ┌──────────────────────────┐
              │  pending[reqID] = ch     │
              │  applyEvent(ev)          │  → session goes awaiting,
              │  go askViaDunst(ev)      │    snapshot fanned out
              └────────────┬─────────────┘
                           │
        ┌──────────────────┼──────────────────┐
        │                  │                  │
        ▼                  ▼                  ▼
  ┌──────────────┐  ┌──────────────┐   ┌──────────────┐
  │ user clicks  │  │ user runs    │   │ 30s passes,  │
  │ Allow / Deny │  │ notchctl     │   │ no decision  │
  │ in dunst     │  │ decide …     │   │ (approval-   │
  │              │  │              │   │  Timeout)    │
  └──────┬───────┘  └──────┬───────┘   └──────┬───────┘
         │ allow/deny      │ allow/deny       │ deny "timeout"
         ▼                 ▼                  ▼
              ┌────────────────────────────┐
              │ delete pending[reqID]      │
              │ updateStatus → running     │
              │ return Decision to         │
              │ publish-client stdout      │
              └────────────────────────────┘
```

Race: `dunstify` and `notchctl decide` can both produce a Decision. First one to land in `ch` wins; second is dropped by the `default` arm of the `select` send. The dunstify goroutine guards with `_, stillPending := d.pending[…]` to avoid double-delivering after a decide-role override.

## C. Daemon lifecycle

```
                main()
                  │
                  │ MkdirAll(socketDir, statePath)
                  ▼
                daemon.New(socket, state)
                  │
                  └─▶ loadState()      ── filter dead pids on read
                  ▼
                d.Run(ctx)
                  │ os.Remove(stale socket)
                  │ net.Listen("unix",…)
                  │ Chmod 0600
                  │ BootstrapFromProc()  ── /proc scan, sid from transcript
                  │ go livenessProbe(ctx)
                  │
                  └─▶ for { Accept → go handleConn(ctx, conn) }
                              │
                              │ first line = role header
                              ▼
                      switch role:
                          publish | subscribe | decide | command
                  
                  ┌─── SIGINT / SIGTERM ───┐
                  │                         │
                  ▼                         │
                cancel(ctx) → Listener.Close → for-loop exits → Run returns
```

State persisted on every `publishLocked` via `saveStateLocked` (atomic write: `state.json.tmp` → rename). Boot path:

```
loadState  →  BootstrapFromProc  →  livenessProbe ticks
(file)        (/proc walk)            (continuous)
```

Conflict rule on load: any sid already in memory wins, so `BootstrapFromProc` only seeds new sids.
