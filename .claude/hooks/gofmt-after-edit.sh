#!/usr/bin/env bash
#
# Hook: after Edit/Write, if the file is .go, run gofmt + goimports.
#
# Claude Code invokes this script with the event serialized as JSON via stdin.
# tool_input contains .file_path.
#
# Output:
#   - exit 0 with no output: no blocking.
#   - To block or message Claude back, print JSON with hookSpecificOutput.

set -euo pipefail

payload=$(cat)
file=$(printf '%s' "$payload" | sed -n 's/.*"file_path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [[ -z "$file" ]]; then
  exit 0
fi

if [[ "$file" != *.go ]]; then
  exit 0
fi

if [[ ! -f "$file" ]]; then
  exit 0
fi

# gofmt is part of the standard toolchain.
gofmt -w "$file" 2>/dev/null || true

# goimports if available (sorts and prunes imports).
if command -v goimports >/dev/null 2>&1; then
  goimports -w "$file" 2>/dev/null || true
fi

exit 0
