# 07 — Jump (focus the terminal)

`notchctl jump [SID]` resolves a Claude session to the X11 window owning its TTY and focuses it via i3. Optionally also runs `tmux select-pane`.

## Flow

```
notchctl jump [SID]
       │
       ▼
fetchSnapshot()  ── role=command cmd=list, one shot
       │
       ▼
target = SID match  OR  pickNotifyOrWorst(snapshot)
   pickNotifyOrWorst:
     1. first session with Notify=true            (done-unseen wins)
     2. else pickWorst (rank: awaiting>running>idle)
       │
       ▼
focusSession(target):
       │
       ├─▶ xdotoolFindWindow(pid)
       │       walk parent chain ≤8 hops:
       │         xdotool search --all --pid <cur>
       │         keep windows present in i3 tree (real clients only)
       │       returns first match xwin
       │
       ├─▶ i3ConIDForWindow(xwin)
       │       i3-msg -t get_tree → walk → match node["window"]==xwin
       │       returns con_id
       │
       ├─▶ i3-msg "[con_id=N] focus"
       │
       └─▶ if tmux info present:
             tmux select-window -t <s>:<w>
             tmux select-pane   -t <s>:<w>.<p>
       │
       ▼
ackSession(sid)  ── role=command cmd=ack sid=…
                    daemon clears Notify, re-publishes snapshot
                    → polybar drops yellow ✔
```

## Why the 8-hop walk

```
i3 → alacritty (xwin owner)
        └── shell (zsh)
              └── claude CLI       ← Session.PID points here
```

`xdotool search --pid` only matches the pid that owns the X11 `_NET_WM_PID` property — usually alacritty itself, not the terminal's child shell, and definitely not a grandchild. Walking up `/proc/<pid>/stat` ppid is the cheapest way to find the closest ancestor whose pid X server actually knows about. Cap=8 prevents pathological loops.

## Filter by i3 tree presence

`xdotool search` may return notification windows, splash screens, etc. — anything with the matching pid. `xdotoolFindWindow` cross-references against `buildWindowWorkspaceMap(tree)` so only windows that i3 actually owns as containers can win.

## Failure modes

| Cause | Behavior |
|-------|----------|
| no pid known (Session.PID=0) | error: "session has no pid" |
| pid alive but no X window (headless / Wayland) | error after walking 8 hops |
| xwin found, not in i3 tree (race: window just closed) | error from i3ConIDForWindow |
| tmux fields stale (pane killed) | tmux select-pane errors silently swallowed (`_ = run(...)`) |

The unseen-completed Notify glyph is **cleared by jump even on partial success of focus** — `ackSession` runs after `focusSession` returns nil. If focus fails, Notify stays.
