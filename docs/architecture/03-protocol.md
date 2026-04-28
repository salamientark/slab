# 03 — Wire Protocol

Line-delimited JSON over `unix:$XDG_RUNTIME_DIR/i3-notch.sock` (mode `0600`).

First line is a **header** specifying the connection role. Subsequent framing depends on role.

## Roles

```
client                                       notchd
  │   {"role":"publish"}\n                    │
  ├─────────────────────────────────────────▶ │
  │                                           │
  │   {"kind":"PreToolUse",...}\n             │
  ├─────────────────────────────────────────▶ │
  │                          {"ok":true}\n    │
  │ ◀─────────────────────────────────────────┤
  │   …repeat (one Event per line)…           │
  │                                           │
  │   ─── EOF ────────────────────────────▶   │
```

```
client                                       notchd
  │   {"role":"subscribe"}\n                  │
  ├─────────────────────────────────────────▶ │
  │   {Snapshot initial}\n                    │
  │ ◀─────────────────────────────────────────┤
  │   {Snapshot on every state change}\n      │
  │ ◀─────────────────────────────────────────┤  (buffered 8, slow drops)
```

```
client (notchctl decide REQID allow|deny)    notchd
  │   {"role":"decide"}\n                     │
  ├─────────────────────────────────────────▶ │
  │   {"request_id":"…","allow":true}\n       │
  ├─────────────────────────────────────────▶ │
  │                                           │  signals waiter on
  │                                           │  d.pending[reqID]
```

```
client (notchctl list / jump / ack)          notchd
  │   {"role":"command","cmd":"list"}\n       │
  ├─────────────────────────────────────────▶ │
  │   {Snapshot}\n                            │
  │ ◀─────────────────────────────────────────┤  conn closes after one reply
```

## Special: blocking PermissionRequest

`servePublisher` notices `Kind=="PermissionRequest"` and inverts framing — instead of `{"ok":true}` it returns the `Decision` JSON. `hook.sh` consumes it on stdout and translates to Claude's `hookSpecificOutput.decision` schema.

```
hook.sh                  notchctl publish               notchd
  │ stdin:                  │                              │
  │  {Permission event}     │                              │
  ├────────────────────────▶│                              │
  │                         │ {"role":"publish"}           │
  │                         ├─────────────────────────────▶│
  │                         │ {Permission event}           │
  │                         ├─────────────────────────────▶│
  │                         │                              │ d.pending[req]=ch
  │                         │                              │ go askViaDunst()
  │                         │                              │
  │                         │                              │  dunstify ↘
  │                         │                              │  user click → "allow"
  │                         │                              │  ch <- Decision
  │                         │                              │
  │                         │ {"request_id":...,            │
  │                         │  "allow":true}               │
  │                         │ ◀─────────────────────────────┤
  │ stdout (jq translate):  │                              │
  │  {hookSpecificOutput…}  │                              │
  │ ◀───────────────────────┤                              │
```

If user does not click within 25s (dunstify timeout) **and** no `notchctl decide` arrives within 30s (`approvalTimeout`), daemon returns `Decision{Allow:false, Reason:"timeout"}`.

## Types (`internal/proto/proto.go`)

| Type | Direction | Purpose |
|------|-----------|---------|
| `Event` | client → daemon | one per hook firing |
| `Decision` | daemon → publish-client; or decide-client → daemon | approval response |
| `Session` | internal | per-sid record |
| `Snapshot` | daemon → subscriber | aggregate view, deep copy |

`Snapshot.AggStatus` is the worst-of across non-`ended` sessions, ranked **awaiting > running > idle**. `AggCount` excludes `ended`.
