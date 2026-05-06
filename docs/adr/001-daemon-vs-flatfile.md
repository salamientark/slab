# ADR 001 — Daemon + Unix Socket vs Flat-File Polling

**Date:** 2026-04-23
**Status:** Accepted

## Context

The notch needs a state store that:
- Accepts writes from N concurrent hook scripts (one per `claude` session)
- Pushes updates to polybar with low latency
- Survives multi-session scenarios without data races

Two options considered:

### A) Flat-file polling

- Hooks write `/run/user/$UID/i3-notch/state.json`
- Polybar polls every 500ms

### B) Daemon + unix socket (chosen)

- `notchd` owns `/run/user/$UID/i3-notch.sock`
- Hooks are thin clients: `echo '{...}' | socat - UNIX-CONNECT:...`
- Polybar `tail` module reads stdout of `notchctl subscribe` — push, not poll
- Single-writer = no locks, no torn JSON

## Decision

**Option B.** Go daemon, ~150 LoC expected.

## Rationale

- Concurrent hook writes to a flat file race. File locks are portable but add latency and complicate hook scripts.
- 500ms polling wakes polybar 2 Hz forever, wastes power, feels laggy on state changes.
- Push-based updates via `tail` module = instant repaint on event.
- Single static binary: no Python/Node runtime, easy systemd --user unit, easy Nix/Arch packaging.

## Consequences

- Extra moving part (daemon lifecycle). Mitigate with `systemd --user` auto-restart.
- Socket permissions must be per-user (`/run/user/$UID/`) — no cross-user leakage.
- Daemon crash = no status. Mitigate with restart policy and fallback "notchd down" badge.

## Rejected alternatives

- **Redis/sqlite**: overkill for N≤10 sessions.
- **D-Bus**: heavier, more codegen, less portable to non-GNOME i3 users.
- **Named pipe**: no multi-subscriber fan-out.
