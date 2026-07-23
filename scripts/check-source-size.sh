#!/usr/bin/env bash
set -euo pipefail
root_dir="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$root_dir"
violations=()
while IFS= read -r -d '' source; do
  grep -Eq 'Code generated .* DO NOT EDIT\.' "$source" && continue
  lines=$(wc -l < "$source")
  (( lines > 400 )) && violations+=("$source"$'\t'"$lines")
done < <(find backend frontend deploy scripts \
  \( -path '*/node_modules/*' -o -path '*/vendor/*' -o -path '*/dist/*' -o -path '*/build/*' -o -path '*/coverage/*' -o -path '*/tmp/*' -o -path '*/generated/*' -o -path '*/frontend/src/shared/api/generated/*' \) -prune -o \
  -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' -o -name '*.sql' -o -name '*.sh' -o -name '*.ps1' \) -print0)
if (( ${#violations[@]} > 0 )); then
  printf 'architecture check: handwritten source exceeds 400 lines:\n' >&2
  printf '%s\n' "${violations[@]}" | LC_ALL=C sort | while IFS=$'\t' read -r source lines; do
    printf '%s — %s\n' "$source" "$lines" >&2
  done
  exit 1
fi
