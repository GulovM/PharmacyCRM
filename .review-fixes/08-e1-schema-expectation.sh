#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
from pathlib import Path
path = Path('deploy/scripts/tests/test-e1-role-upgrade.sh')
text = path.read_text()
old = 'SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton" | grep -Fx "23"'
new = 'SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton" | grep -Fx "24"'
if old not in text:
    raise SystemExit('E1 API schema expectation anchor not found')
path.write_text(text.replace(old, new, 1))
PY
