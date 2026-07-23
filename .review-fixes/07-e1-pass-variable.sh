#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
from pathlib import Path
path = Path('deploy/scripts/tests/test-e1-role-upgrade.sh')
text = path.read_text()
old = '''psql "$admin_database_dsn" -X -At -v ON_ERROR_STOP=1 <<'SQL' | grep -Fx "f|f|0|0|0"
'''
new = '''psql "$admin_database_dsn" -X -At -v ON_ERROR_STOP=1 \\
  -v api_role="$api_role" <<'SQL' | grep -Fx "f|f|0|0|0"
'''
if old not in text:
    raise SystemExit('group-role verification invocation anchor not found')
path.write_text(text.replace(old, new, 1))
PY
