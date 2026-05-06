#!/usr/bin/env bash
# UserPromptSubmit hook: derive concise session topic from first prompt.
# Writes .claude/topics/<session_id>.txt for statusline to read.
#
# Behavior:
#   - First prompt of session only (file absent) -> generate.
#   - Fast path: truncated first line written immediately (non-blocking).
#   - Background: claude -p (haiku) refines to 3-5 words, overwrites.
#
# Lifecycle guarantees (process-leak prevention):
#   - Bounded stdin read (timeout 2) -> never hang on slow/dead parent.
#   - Bounded claude -p (timeout 20) -> bg refine cannot stall.
#   - setsid -f -> detach into new session, reparent to init.
set -euo pipefail

# Recursion guard: inner `claude -p` invocations from this hook would otherwise
# fire UserPromptSubmit again with our own system-instruction string as the
# prompt, polluting the topic file. See feedback_hooks memory.
[ "${I3NOTCH_HOOK_INHIBIT:-0}" = "1" ] && exit 0

INPUT="$(timeout 2 cat 2>/dev/null || true)"
[ -z "$INPUT" ] && exit 0

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

# Background refine via claude haiku. Fully detached, time-bounded.
if command -v claude >/dev/null 2>&1; then
    setsid -f bash -c '
        export I3NOTCH_HOOK_INHIBIT=1
        PROMPT=$1; FILE=$2
        TITLE="$(printf "%s" "$PROMPT" | timeout 20 claude -p \
            --model claude-haiku-4-5-20251001 \
            "Summarize this task in 3-5 words. Output only the title, no quotes, no punctuation at end." \
            2>/dev/null | head -n1 | cut -c1-50)"
        [ -n "$TITLE" ] && printf "%s" "$TITLE" > "$FILE"
    ' _ "$PROMPT" "$FILE" </dev/null >/dev/null 2>&1
fi

exit 0
