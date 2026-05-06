# i3-notch Install Procedure

Reproducible install across machines. Vendored configs in `dist/`, idempotent `install.sh`. No `uninstall.sh` ships yet — see [Uninstall](#uninstall) for manual removal.

## Principles

- **Everything in repo.** No config lives only on author's machine. `dist/` holds source of truth for polybar, systemd, hook templates.
- **Idempotent.** Re-running `install.sh` upgrades, not duplicates.
- **Marker blocks.** Appended config uses `# >>> i3-notch >>>` / `# <<< i3-notch <<<` sentinels. Installer (and any future uninstaller) identifies managed sections by markers only — never free-form edits.
- **Backup first.** Every touched user file gets `.bak-<epoch>` before any write.
- **Placeholder substitution.** Templates use `__I3NOTCH_BIN__`, `__I3NOTCH_ROOT__`. Installer substitutes at copy time.
- **Fail loud.** Missing prereqs (go, polybar, i3, systemd user bus) → abort with explicit message.

## Layout

```
i3-notch/
├── bin/                          # built binaries (gitignored)
├── cmd/notchd, cmd/notchctl      # Go sources
├── internal/                     # daemon + proto
├── hooks/hook.sh                 # claude hook dispatcher (template)
├── scripts/raise-notch.sh        # polybar raise loop
├── dist/
│   ├── polybar/
│   │   ├── notch.conf            # [bar/notch] + [module/i3notch] block
│   │   └── launch-snippet.sh     # polybar launch additions
│   ├── systemd/
│   │   └── i3-notch.service      # user unit
│   └── arch/PKGBUILD             # (optional) Arch package
├── install.sh                    # curl | bash target
└── docs/install.md               # this file
```

## Install methods

### Method A — clone (recommended, auditable)

```bash
git clone https://github.com/<user>/i3-notch ~/.local/share/i3-notch
cd ~/.local/share/i3-notch
./install.sh
```

### Method B — curl pipe (convenience, trust caveat)

```bash
curl -fsSL https://raw.githubusercontent.com/<user>/i3-notch/<TAG>/install.sh | bash
```

**Always pin to a release tag, never `main`.** The piped script only bootstraps: it clones the tagged commit into `~/.local/share/i3-notch` then runs the real `install.sh` from disk.

## What `install.sh` does

1. **Prereq check.** `go`, `polybar`, `i3`, `systemctl --user` available. Abort if missing.
2. **Build.** `go build -o bin/notchd ./cmd/notchd && go build -o bin/notchctl ./cmd/notchctl`.
3. **Install binaries.** Symlink (not copy) `bin/notchd`, `bin/notchctl` → `~/.local/bin/`.
4. **Systemd user unit.** Copy `dist/systemd/i3-notch.service` → `~/.config/systemd/user/`. `systemctl --user daemon-reload && systemctl --user enable --now i3-notch`.
5. **Polybar config.** Backup `~/.config/polybar/config.ini`. Append substituted `dist/polybar/notch.conf` between markers. If marker block already present → replace, don't duplicate.
6. **Polybar launch.** Backup `~/.config/polybar/launch.sh`. Append substituted `dist/polybar/launch-snippet.sh`.
7. **Claude hooks (optional, `--with-hooks` flag).** Backup `~/.claude/settings.json`. Merge hooks block pointing to `<repo>/hooks/hook.sh`. JSON merge via `jq`.
8. **Report.** Print every path touched + backup location.

## Placeholder substitution

Templates contain `__I3NOTCH_BIN__` (absolute path to `bin/`) and `__I3NOTCH_ROOT__` (repo root). Installer resolves these from its own location (`$(cd "$(dirname "$0")" && pwd)`) and `sed`s them before writing.

## Uninstall

No `uninstall.sh` ships yet. Manual removal, in order:

1. `systemctl --user disable --now i3-notch.service` then remove the unit file at `~/.config/systemd/user/i3-notch.service`.
2. Strip marker blocks from `~/.config/polybar/config.ini` and `~/.config/polybar/launch.sh` (two passes — see [Marker block contract](#marker-block-contract)), or restore the `.bak-<epoch>` siblings written by the installer.
3. Restore `~/.claude/settings.json` from its most recent `.bak-*` (or strip the hooks block by marker if merged in-place).
4. Remove symlinks from `~/.local/bin/notchd`, `~/.local/bin/notchctl`.
5. Optionally `rm -rf ~/.cache/i3-notch/`. The repo clone is left in place.

Backups are **never auto-deleted**. User removes manually after verifying revert.

## Marker block contract

```
# >>> i3-notch >>>
# Managed by i3-notch installer. Do not edit between markers.
<content>
# <<< i3-notch <<<
```

Rules:
- Exactly one block per managed file.
- Install = replace block if present, else append.
- Uninstall = delete block via two passes (one per comment char):
  - `sed -i '/# >>> i3-notch >>>/,/# <<< i3-notch <<</d' <file>`  # sh/ini
  - `sed -i '/; >>> i3-notch >>>/,/; <<< i3-notch <<</d' <file>`  # polybar
- Comment char varies per file type (`#` for sh/ini, `;` for polybar INI — polybar accepts both; use `;` there).

## Prereq matrix

| Tool              | Check                         | Required for  |
|-------------------|-------------------------------|---------------|
| go ≥ 1.21         | `go version`                  | build         |
| polybar           | `command -v polybar`          | runtime       |
| i3                | `command -v i3`               | runtime       |
| systemctl --user  | `systemctl --user status`     | daemon mgmt   |
| jq                | `command -v jq`               | `--with-hooks`|

## Testing the install

On a fresh user/VM:
1. Run `install.sh`. Expect zero errors, binaries in `~/.local/bin/`, service active.
2. `notchctl status` → connected.
3. Polybar shows notch bar.
4. Run `install.sh` again. Expect no duplicate marker blocks, no extra backups beyond first run.
5. Run the manual uninstall steps above. Expect `config.ini` byte-identical to pre-install backup.

## Open questions

- Do we vendor a PKGBUILD for Arch (already present in `dist/arch/`) and publish to AUR? Simplifies install on Arch to `yay -S i3-notch`.
- Windows/Wayland support: out of scope; document as X11+i3 only.
