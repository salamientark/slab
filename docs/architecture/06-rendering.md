# 06 — Polybar Rendering

`notchctl subscribe --format polybar` consumes the `Snapshot` stream and emits one polybar tail line per change. Polybar's `tail = true` means each line replaces the bar contents.

## Empty bar

```
%{F#6c7086}󰚩 idle%{F-}
```

(grey robot, "idle"). Shown when `AggCount == 0`.

## Populated bar layout

```
[bot icon] [chip] [chip] [chip] [+overflow]
```

ASCII model:

```
 ┌─────────────────────────────────────────────────────────────────┐
 │ %{A1:notchctl jump:}󰚩%{A}   ws | ⚡ | name   ws | ⚠ | name  +2 │
 │ └─prefix (color = agg)─┘   └────── chip (clickable) ──────┘    │
 └─────────────────────────────────────────────────────────────────┘
```

### Chip composition (`renderChip`, polybar.go:122)

```
%{A1:<exe> jump <sid>:}                       ── click target
  %{F#89b4fa}<ws>%{F-}                        ── workspace number, blue
  | (muted separator)                         ──
  %{F<status>}<icon>%{F-}                     ── ⚡ / ⚠ / ⏾ / ✔
  | (muted separator)
  [%{F#f9e2af}%{u#f9e2af}%{+u}]<name>[%{-u}%{F-}]  ── name, underlined+yellow if Notify
%{A}
```

### Status palette

| Status | Glyph | Color | Notes |
|--------|-------|-------|-------|
| awaiting | ⚠ | `#fab387` orange | one-of-N pending approval |
| running | ⚡ | `#a6e3a1` green | between PreToolUse and Stop |
| idle | ⏾ | `#6c7086` grey | quiet |
| idle + Notify | ✔ | `#f9e2af` yellow | done-unseen, sorts above plain idle |

### Sort order (`sortRank`)

```
awaiting (4) > running (3) > idle+Notify (2) > idle (1)
```

Tie-break: name ascending. Truncated to `maxChips=5`; remainder rendered as `+N`.

## Workspace lookup

Each chip shows the i3 workspace name where the terminal lives. Resolution:

```
        Session.PID
            │
            ▼
   xdotool search --all --pid → [xwin1, xwin2, …]
            │
            ▼
   ──── walk parent pids up to 8 hops if no match ────
            │
            ▼
   i3-msg -t get_tree → walk → map<xwin, (workspace, output)>
            │
            ▼
   first xwin in map → wsInfo
```

The `xdotool` lookup catches alacritty-spawned shells where the terminal X window is owned by a parent shell pid. Non-X11 sessions (e.g. systemd-spawned headless tests) fall through silently → chip shows `?` for workspace.

## Project icon (`projectIcon`)

Probes manifest files in `cwd` to pick a nerd-font glyph. First match wins; defaults to a folder glyph. Currently **computed but not embedded** in chip output (function returns the glyph; `chipView.project` is set but no template uses it). Hook for future use.

## Bot prefix click

```
%{A1:<self-exe> jump:}󰚩%{A}
```

Click → `notchctl jump` (no SID) → `pickNotifyOrWorst` → focus that terminal. `<self-exe>` is the absolute path to `notchctl` resolved by `os.Executable()` at process start; defends against `$PATH` differences when polybar is launched by i3.

## Title resolution chain (chip name)

```
displayName(sid, cwd):
   sessionTitle(sid)     ─── cached read of ~/.claude/projects/<proj>/<sid>.jsonl
       │ latest custom-title entry, else
       │ first user prompt (skipping <command-name>, slash, etc.)
       ▼
   shortName(cwd)        ─── basename(cwd), max 22 runes
```

Title cache is keyed by `(sid, mtime)` — re-extracts only when the transcript file mtime advances.
