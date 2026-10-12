#!/bin/sh
# Spike: refine one ticket through headless Claude Code, then send one follow-up.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
out=${1:-"$here/out"}
mkdir -p "$out"

cd "$HOME/creativex/dating-backend"

claude -p "$(cat "$here/ticket.md")" \
  --append-system-prompt "$(cat "$here/rules.md")" \
  --add-dir "$HOME/creativex/dating" "$HOME/creativex/starvault.tech" "$HOME/develop/starvault-php-sdk" \
  --allowedTools Read Grep Glob \
  --json-schema "$(cat "$here/schema.json")" \
  --output-format json >"$out/draft.json"

session=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["session_id"])' "$out/draft.json")

claude -p "D23 to D25 are out of scope for now; refine only D01 and the session header. What changes?" \
  --resume "$session" \
  --allowedTools Read Grep Glob \
  --json-schema "$(cat "$here/schema.json")" \
  --output-format json >"$out/followup.json"

echo "session $session"
