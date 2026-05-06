# 08 — Install / Deployment

## Filesystem layout (post-install)

```
~/PROJECTS/i3-notch/
├── bin/
│   ├── notchd                         (built)
│   └── notchctl                       (built)
~/.local/bin/
│   ├── notchd       → ../../PROJECTS/i3-notch/bin/notchd
│   └── notchctl     → ../../PROJECTS/i3-notch/bin/notchctl
~/.config/systemd/user/
│   └── i3-notch.service               (managed by install.sh, marker block-free)
~/.config/polybar/
│   ├── config.ini                     (apply_block:  ; >>> i3-notch >>> … ; <<< i3-notch <<<)
│   └── launch.sh                      (apply_block:  # >>> i3-notch >>> … # <<< i3-notch <<<)
~/.claude/settings.json                (optional, --with-hooks; not yet implemented)
~/.cache/i3-notch/state.json           (daemon, atomic writes via .tmp + rename)
$XDG_RUNTIME_DIR/i3-notch.sock         (mode 0600)
```

## install.sh state machine

```
                     ./install.sh [--with-hooks]
                                │
                                ▼
                    ┌────────────────────────┐
                    │ prereq check           │
                    │ go, polybar, i3,       │
                    │ systemctl (+jq if      │
                    │ --with-hooks)          │
                    └───────────┬────────────┘
                                │ missing → die
                                ▼
                    ┌────────────────────────┐
                    │ go build  notchd       │
                    │ go build  notchctl     │
                    └───────────┬────────────┘
                                ▼
                    ┌────────────────────────┐
                    │ symlink  ~/.local/bin  │
                    └───────────┬────────────┘
                                ▼
                    ┌────────────────────────┐
                    │ systemd unit:          │
                    │ substitute /usr/bin →  │
                    │ $REPO/bin path,        │
                    │ daemon-reload,         │
                    │ enable --now           │
                    └───────────┬────────────┘
                                ▼
                    ┌────────────────────────┐
                    │ apply_block to         │
                    │ polybar/config.ini     │
                    │ + launch.sh            │
                    │ (idempotent: marker    │
                    │  block replaced)       │
                    └───────────┬────────────┘
                                ▼
                       ─── --with-hooks?
                                │ no → done
                                │ yes → TODO (jq merge into ~/.claude/settings.json)
```

`apply_block` algorithm:

```
1. backup target → target.bak-<ts>
2. if MARKER_START found in file:
     sed delete from MARKER_START to MARKER_END
3. append blank line + content (which already wraps marker block)
```

→ reruns are safe; old block always replaced, nothing else touched.

## Systemd unit

`dist/systemd/i3-notch.service` ships with `ExecStart=/usr/bin/notchd`; the installer rewrites this to `$REPO/bin/notchd`. The unit runs as a **user** unit:

```
systemctl --user enable --now i3-notch.service
journalctl --user -u i3-notch -f
```

Restart policy is `on-failure`, so a crash respawns. SIGINT/SIGTERM trigger graceful shutdown via `cancel(ctx)` in `cmd/notchd/main.go`.

## Polybar wiring

`dist/polybar/notch.conf` defines `[bar/notch]` (top, transparent, single module) and `[module/i3notch]` (`type=custom/script`, `tail=true`, exec is a `while ! notchctl subscribe …; do sleep 2; done` loop so the bar self-recovers when `notchd` restarts).

`dist/polybar/launch-snippet.sh` adds a `polybar notch &` invocation to the user's existing `launch.sh`.

`scripts/raise-notch.sh` (optional) keeps the bar above all other windows by subscribing to i3 window events and re-raising every notch X window via `xdotool windowraise`.

## Removal

There is no `uninstall.sh` shipped (the install message references one but it isn't in the tree). Manual removal:

1. `systemctl --user disable --now i3-notch.service`
2. delete the unit file
3. revert `polybar/config.ini` and `launch.sh` to their `.bak-<ts>` siblings (or strip the marker block manually)
4. delete `~/.local/bin/notchd`, `~/.local/bin/notchctl`
5. delete `~/.cache/i3-notch/`
