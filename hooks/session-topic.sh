#!/usr/bin/env bash
# UserPromptSubmit hook: derive concise session topic from first prompt.
# Writes .claude/topics/<session_id>.txt for statusline to read.
#
# Behavior:
#   - First prompt of session only (file absent) -> generate.
#   - Fast path: truncated first line written immediately (non-blocking).
#   - Background: claude -p (haiku) refines to 3-5 words, overwrites.

set -euo pipefail

INPUT="$(cat)"
SID="$(printf '%s' "$INPUT" | jq -r '.session_id // ""')"
PROMPT="$(printf '%s' "$INPUT" | jq -r '.prompt // ""')"
PROJ="${CLAUDE_PROJECT_DIR:-$PWD}"
DIR="$PROJ/.claude/topics"
mkdir -p "$DIR"
FILE="$DIR/$SID.txt"

[ -z "$SID" ] && exit 0
[ -s "$FILE" ] && exit 0
[ -z "$PROMPT" ] && exit 0

# Fast path: first line, trimmed to 50 chars.
FAST="$(printf '%s' "$PROMPT" | head -n1 | cut -c1-50)"
printf '%s' "$FAST" > "$FILE"

# Background refine via claude haiku. Detach fully.
if command -v claude >/dev/null 2>&1; then
    (
        TITLE="$(printf '%s' "$PROMPT" | claude -p \
            --model claude-haiku-4-5-20251001 \
            "Summarize this task in 3-5 words. Output only the title, no quotes, no punctuation at end." \
            2>/dev/null | head -n1 | cut -c1-50)"
        if [ -n "$TITLE" ]; then
            printf '%s' "$TITLE" > "$FILE"
        fi
    ) </dev/null >/dev/null 2>&1 &
    disown 2>/dev/null || true
fi

exit 0
