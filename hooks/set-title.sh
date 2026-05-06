#!/usr/bin/env bash
# Write/overwrite session topic for /title command.
# Targets the current session's topic file deterministically using
# CLAUDE_SESSION_ID so a write in session B never clobbers session A.
set -euo pipefail

TITLE="${*:-}"
[ -z "$TITLE" ] && { echo "usage: set-title.sh <title>" >&2; exit 1; }

SID="${CLAUDE_SESSION_ID:-}"
[ -z "$SID" ] && { echo "set-title.sh: CLAUDE_SESSION_ID is required" >&2; exit 1; }

PROJ="${CLAUDE_PROJECT_DIR:-$PWD}"
DIR="$PROJ/.claude/topics"
mkdir -p "$DIR"

TITLE="$(printf '%s' "$TITLE" | head -n1 | cut -c1-50)"

printf '%s' "$TITLE" > "$DIR/$SID.txt"
echo "title set: $TITLE"
