# Daemon codemap

`internal/daemon/` — single-writer state store, pub/sub fan-out, approval broker.

## daemon.go (392 lines)

- `Daemon` struct: `socketPath`, mutex-guarded `sessions map[sid]*Session`, `subs map[chan Snapshot]struct{}`, `pending map[rid]chan Decision`.
- `New(socketPath)` constructor.
- `Run(ctx)` — `net.Listen("unix", …)`, chmod 0600, spawn `livenessProbe`, accept loop.
- Per-connection: read first JSON line → role field → dispatch: `publish` / `subscribe` / `decide` / `command`.
- `publish`: decode `Event` → apply to session map → compute `Snapshot` → fan-out to all subs → if `PermissionRequest`, delegate to approval flow and write `Decision` back.
- `subscribe`: register chan, stream JSON snapshots until client disconnects. `--format polybar` handled client-side (`notchctl`).
- `livenessProbe` (2s tick): for each session PID, `syscall.Kill(pid, 0)` → prune if ESRCH.
- Worst-of aggregation: iterate sessions, pick max of `awaiting > running > idle`.

Constants: `livenessProbeInterval=2s`, `approvalTimeout=30s`.

## approval.go (49 lines)

- `(d *Daemon).handlePermissionRequest(ev Event) Decision`
- Register `ch := make(chan Decision, 1)` keyed by `ev.RequestID` in `d.pending`.
- Spawn `dunstify` with Allow/Deny actions (external process, capture action name).
- Wait on `select { case dec := <-ch: ; case <-time.After(30s): Decision{Allow:false, Reason:"timeout"} }`.
- Separate `decide` role writes `Decision` into the pending chan.

## Invariants

- Mutex `d.mu` held for any read/write of `sessions`, `subs`, `pending`.
- Subscribers receive snapshots on buffered chan; slow sub dropped (no backpressure into publisher).
- Socket path injectable (testable) but default `$XDG_RUNTIME_DIR/i3-notch.sock`.
