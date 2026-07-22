#!/usr/bin/env bash
set -euo pipefail
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
mkdir -p "$root/backend/internal" "$root/frontend/src" "$root/scripts"
seq 1 400 > "$root/backend/internal/at_limit.go"
seq 1 401 > "$root/backend/internal/a.go"
seq 1 450 > "$root/frontend/src/b.tsx"
if "$PWD/scripts/check-source-size.sh" "$root" >"$root/out" 2>&1; then exit 1; fi
grep -q 'backend/internal/a.go — 401' "$root/out"
grep -q 'frontend/src/b.tsx — 450' "$root/out"
[[ "$(grep -n 'backend/internal/a.go' "$root/out" | cut -d: -f1)" -lt "$(grep -n 'frontend/src/b.tsx' "$root/out" | cut -d: -f1)" ]]
