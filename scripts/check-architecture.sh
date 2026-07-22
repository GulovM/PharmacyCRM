#!/usr/bin/env bash
set -euo pipefail

readonly ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

fail() {
  printf 'architecture check: %s\n' "$*" >&2
  exit 1
}

require_directory() {
  [[ -d "$1" ]] || fail "required directory is missing: $1"
}

for directory in \
  backend \
  backend/cmd/api backend/cmd/worker backend/cmd/migrate \
  backend/internal/bootstrap backend/internal/platform backend/internal/shared \
  backend/internal/orchestration backend/internal/modules backend/migrations backend/test \
  frontend frontend/src/app frontend/src/pages frontend/src/widgets frontend/src/features \
  frontend/src/entities frontend/src/shared frontend/src/test frontend/e2e \
  deploy docs; do
  require_directory "$directory"
done

for forbidden in \
  backend/internal/handlers backend/internal/services backend/internal/repositories \
  backend/internal/models backend/internal/utils frontend/src/api.ts; do
  [[ ! -e "$forbidden" ]] || fail "forbidden path exists: $forbidden"
done

if find backend -type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.jsx' -o -name '*.vue' \) -print -quit | grep -q .; then
  fail 'frontend source must not be placed in backend/'
fi

if find frontend -path '*/node_modules' -prune -o -type f -name '*.go' -print -quit | grep -q .; then
  fail 'backend Go source must not be placed in frontend/'
fi

if rg -n --glob '*.go' '"[^"\n]*frontend/' backend >/dev/null; then
  fail 'backend Go source must not import frontend source'
fi

if rg -n --pcre2 --glob '*.go' '"github\.com/GulovM/PharmacyCRM/backend/(?!internal/bootstrap(?:/|"))' backend/cmd >/dev/null; then
  fail 'cmd entry points may import project code only from internal/bootstrap'
fi

if rg -n --glob '*.{ts,tsx,js,jsx}' "from ['\"][^'\"]*backend/|import\(['\"][^'\"]*backend/" frontend >/dev/null; then
  fail 'frontend source must not import backend source'
fi

if [[ ! -f backend/go.mod ]]; then
  fail 'backend must remain an independent Go module'
fi

if [[ ! -f frontend/package.json ]]; then
  fail 'frontend must remain an independent JavaScript application root'
fi

if [[ -e frontend/package-lock.json || -e frontend/yarn.lock ]]; then
  fail 'frontend must use pnpm only; npm and Yarn lockfiles are forbidden'
fi

readonly mandatory_repositories=(
  backend/internal/modules/audit/infrastructure/postgres/repository.go
  backend/internal/modules/reliability/infrastructure/postgres/idempotency_repository.go
  backend/internal/modules/reliability/infrastructure/postgres/outbox_repository.go
)
if rg -n 'database\.DBTX|NewRepository\(|NewIdempotencyRepository\(|NewOutboxRepository\(' "${mandatory_repositories[@]}" >/dev/null; then
  fail 'mandatory reliability repositories must be transaction-only'
fi

if rg -n --glob '*.go' 'NewTransactional(Audit|Idempotency|Outbox)Repository\([^\n]*(pool|Pool)' backend >/dev/null; then
  fail 'transactional reliability repository constructed from a pool'
fi

if rg -n --glob '**/application/*.go' --glob '**/application/**/*.go' --glob '**/domain/*.go' --glob '**/domain/**/*.go' 'github\.com/jackc/pgx' backend/internal/modules >/dev/null; then
  fail 'application and domain packages must not import pgx'
fi

while IFS= read -r -d '' source; do
  if grep -Eq 'Code generated .* DO NOT EDIT\.' "$source"; then
    continue
  fi
  lines=$(wc -l < "$source")
  if (( lines > 400 )); then
    fail "handwritten source exceeds 400 lines: $source ($lines)"
  fi
done < <(find backend frontend scripts \
  \( -path '*/node_modules/*' -o -path '*/vendor/*' -o -path '*/dist/*' -o -path '*/build/*' -o -path '*/coverage/*' -o -path '*/tmp/*' -o -path '*/generated/*' -o -path 'frontend/src/shared/api/generated/*' \) -prune -o \
  -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' -o -name '*.sql' -o -name '*.sh' -o -name '*.ps1' \) -print0)

if find backend frontend scripts -type f \( -name 'utils.go' -o -name 'helpers.go' -o -name 'common.go' -o -name 'misc.go' -o -name 'manager.go' -o -name 'service_all.go' -o -name 'repository_all.go' \) -not -name '*_test.go' -print -quit | grep -q .; then
  fail 'production source must not use a generic filename'
fi

printf 'architecture check: passed\n'
