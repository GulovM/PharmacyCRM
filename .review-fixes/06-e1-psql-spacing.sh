#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
from pathlib import Path
path = Path('deploy/scripts/tests/test-e1-role-upgrade.sh')
text = path.read_text()
old = "member<>(SELECT oid FROM pg_roles WHERE rolname=:'api_role')"
new = "member <> (SELECT oid FROM pg_roles WHERE rolname = :'api_role')"
if old not in text:
    raise SystemExit('psql spacing anchor not found')
path.write_text(text.replace(old, new, 1))
PY
