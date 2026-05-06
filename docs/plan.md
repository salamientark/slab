# VibeIsland White-Box Engineering — Plan v5 (NO PURCHASE)

**Date:** 2026-04-23
**Change from v4:** user will NOT buy the $19.99 app. No DMG access. No macOS install. No before/after settings diff on a real machine. This reshapes recon heavily.

**Target:** https://vibeisland.app/
**User stack:** Arch Linux + i3 + Alacritty
**Goal:** personal Linux reimplementation inspired by VibeIsland, not a bit-accurate port

## Impact of no-purchase constraint

| Lost | Replacement |
|------|-------------|
| DMG binary (otool/nm/strings/class-dump) | Public landing page + changelog + marketing screenshots |
| `settings.json` before/after diff | Infer from Claude Code hooks docs + community configs |
| Runtime fs_usage / lsof / Wireshark | None — no Mac to observe |
| Exact UI behavior | YouTube demos, landing-page animated notch, reviews |

**Consequence:** this is no longer reverse engineering. It is **clean-room reimplementation from a public feature spec**. Better legally, looser functionally. Accept drift from original — aim for *equivalent workflow*, not *pixel clone*.

## Revised thesis

Since binary is off-limits, **100% of the build relies on public surfaces**:
1. Claude Code hooks docs (PreToolUse, PostToolUse, Notification, Stop, UserPromptSubmit, SessionStart/End) — fully documented
2. JSONL transcript tail at `~/.claude/projects/*/*.jsonl` — public file format
3. Codex CLI / Gemini CLI public hook or log surfaces
4. Process + tty heartbeat
5. i3 IPC + EWMH for terminal jump

No need for `class-dump`, `otool`, binary recon. Entire mechanism derivable from docs.

## Skills (from /home/madlab/TOOLS/everything-claude-code/skills/)

Kept: `search-first`, `deep-research`, `product-lens`, `documentation-lookup`, `blueprint`, `architecture-decision-records`, `security-review`, `santa-method`, `verification-loop`, `context-budget`.

Dropped vs v4: binary-analysis tooling (no binary), runtime observation phase (no Mac).

External tools: i3-msg, wmctrl/swaymsg, polybar/waybar, dunst, paplay, systemd --user.

## Phases

### Ph -1 Ethics + scope
Not buying. Not cloning for resale. Personal use only. Feature-level inspiration from public landing page + docs = fair.

### Ph 0 Recon
- `search-first`: Linux clones, polybar `dynamic-island` modules, waybar AI-status modules, any existing Claude-Code-notification GUIs (e.g. dunst-based approval daemons on GitHub)
- `deep-research`: VibeIsland changelog page, HN/Reddit threads, Twitter @edwardluox, YouTube demos. Snapshot landing-page feature list.
- `product-lens`: feature inventory from landing page alone (already substantially captured)

### Ph 1 Mechanism derivation (public surfaces only)
- `documentation-lookup` via Context7:
  - Claude Code hooks: PreToolUse, PostToolUse, Notification, Stop, UserPromptSubmit, SessionStart, SessionEnd
  - Codex CLI hooks/logs
  - Gemini CLI hooks/logs
- Map JSONL transcript schema at `~/.claude/projects/*/*.jsonl` by tailing own sessions
- Define state machine: hook events (edge-triggered) + JSONL tail (level-triggered) + process scan (heartbeat)
- `security-review` on every hook script we will install (we write them, so audit our own)

**No binary recon. No runtime obs.** Both skipped permanently.

### Ph 2 WM-integration spike
- 50-line i3-msg + polybar prototype showing a single mock status line driven by a fake hook event
- Output of spike informs toolkit choice in Ph 3
- ADR: polybar custom module vs floating+sticky i3 scratchpad

### Ph 3 Linux build (blueprint)
- 3a: UI toolkit chosen from spike — polybar module + CLI state daemon is leanest
- 3b: unix-socket daemon (request IDs for concurrent agents), single-writer state store
- 3c: approval GUI — dunst actions for simple yes/no, GTK dialog for diff view
- 3d: pid→window resolver via `/proc/<pid>/environ` + `_NET_WM_PID` (X11) + future Wayland adapter
- 3e: terminal jump: `i3-msg '[con_id=…] focus'` with Alacritty + tmux/zellij pane routing ADR
- 3f: 8-bit sounds via `paplay`
- 3g: packaging: PKGBUILD + systemd --user unit + i3 `exec --no-startup-id`
- 3h: observability from line 1: structured logs via `journalctl --user`
- `ADR` per mechanism; `blueprint` for multi-session roadmap

### Ph 4 Verify
- `santa-method` + `verification-loop` **per agent integration** (Claude first, Codex next, Gemini last)
- `context-budget` continuous from Ph 0
- Because no original to diff against: verification is **"does it serve the workflow?"** not "is it identical?"

## Scope v1
- Claude Code (full hook coverage)
- Codex CLI (if hook surface exists; else log tail)
- Gemini CLI (same)

## Scope v2 or cut
- Cursor, Copilot, Qoder, Kiro, CodeBuddy, Droid — no public hook APIs, log-scrape or drop

## Why v5 beats v4 for this user
- No $19.99 spend
- Cleaner legal posture — clean-room reimpl from public docs, zero DMCA/EULA exposure
- Skips two phases (binary recon, runtime obs) that were gated/skippable anyway in v4
- Accepts we're building an inspired-by tool, not a port

## Risks vs v4
- UI details will drift (animations, exact notch geometry irrelevant on i3 anyway)
- May misjudge hook ordering without empirical Mac trace — mitigated by per-agent santa-method
- Non-Claude agents riskier to integrate without reference behavior
