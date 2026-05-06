#!/usr/bin/env bash
# Statusline: print session topic (if any) + cwd basename.
set -euo pipefail

INPUT="$(timeout 2 cat 2>/dev/null || true)"
[ -z "$INPUT" ] && { printf '[?]'; exit 0; }
SID="$(printf '%s' "$INPUT" | jq -r '.session_id // ""')"
CWD="$(printf '%s' "$INPUT" | jq -r '.workspace.current_dir // .cwd // ""')"
PROJ="$(printf '%s' "$INPUT" | jq -r '.workspace.project_dir // empty')"
[ -z "$PROJ" ] && PROJ="${CLAUDE_PROJECT_DIR:-$PWD}"

TOPIC=""
FILE="$PROJ/.claude/topics/$SID.txt"
[ -s "$FILE" ] && TOPIC="$(cat "$FILE")"

BASE="$(basename "$CWD")"
if [ -n "$TOPIC" ]; then
    printf '[%s] %s' "$BASE" "$TOPIC"
else
    printf '[%s]' "$BASE"
fi
