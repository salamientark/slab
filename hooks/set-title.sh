#!/usr/bin/env bash
# Write/overwrite session topic for /title command.
# Finds the newest topic file in .claude/topics/ (current session) and
# overwrites it. Creates one if none exist.
set -euo pipefail

TITLE="${*:-}"
[ -z "$TITLE" ] && { echo "usage: set-title.sh <title>" >&2; exit 1; }

PROJ="${CLAUDE_PROJECT_DIR:-$PWD}"
DIR="$PROJ/.claude/topics"
mkdir -p "$DIR"

TITLE="$(printf '%s' "$TITLE" | head -n1 | cut -c1-50)"

NEWEST="$(ls -t "$DIR"/*.txt 2>/dev/null | head -n1 || true)"
if [ -z "$NEWEST" ]; then
    NEWEST="$DIR/manual-$(date +%s).txt"
fi
printf '%s' "$TITLE" > "$NEWEST"
echo "title set: $TITLE"
