# i3-notch

Status-notch for AI coding agents on i3 + polybar + dunst. Clean-room Linux reimagining of [VibeIsland](https://vibeisland.app/), driven entirely by public Claude Code hook surfaces (no binary RE).

Claude Code first. Codex / Gemini planned for v0.1+.

---

## What it does

- **Status badge** — a top polybar bar shows a nerd-font glyph for each Claude Code session (idle / running / awaiting). Multi-session aggregate: count + worst state (awaiting > running > idle).
- **Approval prompts** — `PermissionRequest` hooks pop a `dunstify` notification with Allow/Deny actions. Decision is fed back to Claude via the hook protocol.
- **Terminal jump** — click the badge (or approve a prompt) → `xdotool` + `i3-msg` focus the exact terminal (+ tmux pane) that owns the session.

Target stack: Arch Linux + i3 + Alacritty + polybar + dunst + tmux (optional).

---

## Architecture

```
~/.claude/settings.json hooks                ~/.claude/projects/**/*.jsonl
 ├─ SessionStart                             (reserved, v0.1+ fallback)
 ├─ UserPromptSubmit
 ├─ PreToolUse / PostToolUse
 ├─ PermissionRequest  ─┐
 ├─ Stop                │  JSON on stdin
 └─ SessionEnd          ▼
                  hooks/hook.sh
                        │  publish (unix socket)
                        ▼
                  notchd (Go daemon)
                        │  pub/sub fan-out
   ┌────────────────────┼────────────────────────┐
   ▼                    ▼                        ▼
 notchctl subscribe  dunstify (approval)   notchctl jump
 --format polybar    blocks until          xdotool → i3-msg
 (polybar tail)      user clicks Allow/Deny (+ tmux select-pane)
```

- **Socket:** `$XDG_RUNTIME_DIR/i3-notch.sock` (`/run/user/$UID/i3-notch.sock`).
- **Protocol:** line-delimited JSON. Roles: `publish`, `subscribe`, `decide`, `command`. See `internal/proto/proto.go`.
- **State:** keyed by Claude `session_id`; dead PIDs pruned by a 2s liveness probe.

ADR: [docs/adr/001-daemon-vs-flatfile.md](docs/adr/001-daemon-vs-flatfile.md).

---

## Layout

```
cmd/notchd       daemon entry
cmd/notchctl     client CLI (publish / subscribe / jump / decide / list)
internal/proto   wire types
internal/daemon  socket server, pub/sub, approval, liveness
hooks/hook.sh    Claude Code hook dispatcher (bash + jq)
dist/systemd/    user unit (packaged)
dist/arch/       PKGBUILD
.claude/         project-scoped hooks for dogfooding
bin/             built binaries (gitignored)
```

---

## Dependencies

**Required runtime:**

| Package | Role |
|---------|------|
| `i3-wm` | Window manager + IPC (`i3-msg`) |
| `polybar` | Bar renderer, `custom/script` + `tail=true` |
| `dunst` | Notification daemon, provides `dunstify` for approval prompts |
| `xdotool` | PID→X window lookup, window raise for bar stacking |
| `jq` | JSON parsing in `hook.sh` |
| `bash` | Hook dispatcher |
| Nerd Font (e.g. `ttf-meslo-nerd`) | Glyphs (󰚩 idle, 󰜎 running, 󰀧 awaiting) in the badge |
| Claude Code CLI | Hook source |

**Build-time only:** `go` (1.21+).

**Optional:**

| Package | Role |
|---------|------|
| `tmux` | Pane-level jump (silently skipped if absent) |
| `systemd` (user) | Auto-start daemon via user unit (manual launch works otherwise) |
| `socat` | Manual socket probing (dev) |
| `dmenu` / `rofi` | Multi-session picker (v0.1+) |

**Assumptions:** Linux, X11 (no Wayland yet), `/proc/<pid>/fd/0` for TTY resolution, `$XDG_RUNTIME_DIR` for socket path.

### Arch Linux one-liner

```bash
sudo pacman -S go i3-wm polybar dunst xdotool jq ttf-meslo-nerd tmux
```

### Debian / Ubuntu

```bash
sudo apt install golang i3 polybar dunst xdotool jq fonts-meslo-lg tmux
```

(install a Nerd Font variant manually if your distro lacks one — polybar glyphs need it.)

## Install (dev, from repo)

```bash
cd ~/PROJECTS/i3-notch
go build -o bin/notchd   ./cmd/notchd
go build -o bin/notchctl ./cmd/notchctl
```

### 1. Run the daemon

**Systemd user unit (recommended)** — already installed at `~/.config/systemd/user/i3-notch.service`:

```bash
systemctl --user daemon-reload
systemctl --user enable --now i3-notch.service
systemctl --user status i3-notch
journalctl --user -u i3-notch -f     # live logs
```

**Manual:**

```bash
./bin/notchd           # foreground
# or detached:
setsid ./bin/notchd < /dev/null > /tmp/notchd.log 2>&1 &
```

### 2. Wire polybar

`~/.config/polybar/config.ini` already has a dedicated `[bar/notch]` top bar and `[module/i3notch]` — it shells out to:

```
bash -c 'while ! ~/PROJECTS/i3-notch/bin/notchctl subscribe --format polybar; do sleep 2; done'
```

`tail = true` + `click-left = notchctl jump` make the badge push-updated and clickable.

`~/.config/polybar/launch.sh` launches both `polybar example` (bottom) and `polybar notch` (top).

### 3. Wire Claude Code hooks

v0 uses **project-scoped** hooks in `/home/madlab/PROJECTS/i3-notch/.claude/settings.json`. Claude Code picks them up automatically whenever you run `claude` inside this directory. Event → `hooks/hook.sh` → `notchctl publish`.

`PermissionRequest` is **intentionally omitted** in v0 — the hook would wedge Claude if notchd were down. Enable once you trust the daemon.

For global usage later, copy the `hooks` block into `~/.claude/settings.json` with absolute paths.

---

## Usage

Open a new terminal:

```bash
cd ~/PROJECTS/i3-notch
claude
```

You should see:

- Idle glyph appear in the top polybar bar (󰚩).
- Glyph flip to running (󰜎) when Claude hits a tool.
- Badge count bump if you open a second `claude` session.
- Click the badge → focus jumps to that terminal (and tmux pane if any).

### Manual probes

```bash
# Tail raw events
./bin/notchctl subscribe --format json

# Snapshot state
./bin/notchctl list

# Inject a fake event
echo '{"kind":"SessionStart","session_id":"demo","pid":'"$$"'}' | ./bin/notchctl publish

# Force a jump to a known session
./bin/notchctl jump --session demo
```

---

## Packaging

- `dist/systemd/i3-notch.service` — user unit for `/usr/bin/notchd`.
- `dist/arch/PKGBUILD` — Arch package: builds both binaries, installs the systemd unit and `hooks/hook.sh` under `/usr/share/i3-notch/`.

Build locally: `cd dist/arch && makepkg -si`.

---

## Roadmap

**v0** — done ✅

- notchd + unix socket
- notchctl (publish / subscribe / jump / decide / list)
- Claude Code hook script
- polybar tail module
- dunst approval flow
- i3-msg terminal jump (+ tmux pane)
- systemd user unit + PKGBUILD

**v0.1+**

- Codex CLI + Gemini CLI adapters (hook-based or JSONL-tail fallback)
- 8-bit sound cues via `paplay`
- GTK diff viewer for Edit approvals
- Wayland / sway port (swaymsg + mako)
- Multi-session dmenu / rofi picker

---

## License

Personal project. Inspired-by, not derived-from, VibeIsland. No DMCA/EULA exposure.
