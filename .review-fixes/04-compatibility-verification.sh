#!/usr/bin/env bash
set -euo pipefail
python3 - <<'PY'
from pathlib import Path

path = Path('backend/migrations/000024_outbox_retention_cutoff_guard.up.sql')
text = path.read_text()
text = text.replace(
    "AND has_function_privilege('pharmacycrm_worker_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND NOT has_function_privilege('pharmacycrm_api_runtime'",
    "AND has_function_privilege('pharmacycrm_worker_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND has_function_privilege('pharmacycrm_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND NOT has_function_privilege('pharmacycrm_api_runtime'",
)
text = text.replace(
    'FROM PUBLIC, pharmacycrm_runtime, pharmacycrm_api_runtime;',
    'FROM PUBLIC, pharmacycrm_api_runtime;',
)
text = text.replace(
    '    TO pharmacycrm_worker_runtime;\n',
    '    TO pharmacycrm_worker_runtime, pharmacycrm_runtime;\n',
)
path.write_text(text)

path = Path('deploy/scripts/provision-postgres-role-contract.sql')
text = path.read_text()
old = """        GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz,integer),
            public.delete_dead_letter_outbox_events_before(timestamptz,integer)
            TO pharmacycrm_worker_runtime;
"""
new = """        -- pharmacycrm_runtime remains NOLOGIN and memberless, but keeps this
        -- historical capability so immutable migration 000019 can be reverified.
        GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz,integer),
            public.delete_dead_letter_outbox_events_before(timestamptz,integer)
            TO pharmacycrm_worker_runtime, pharmacycrm_runtime;
"""
if old not in text:
    raise SystemExit('provisioning retention grant anchor not found')
path.write_text(text.replace(old, new, 1))

path = Path('deploy/scripts/tests/test-e1-role-upgrade.sh')
text = path.read_text()
old = 'grep -Fx "f|t|0|0|0|0"'
new = 'grep -Fx "f|t|0|0|0|2"'
if old not in text:
    raise SystemExit('compatibility role assertion anchor not found')
path.write_text(text.replace(old, new, 1))

path = Path('.github/workflows/ci.yml')
text = path.read_text()
anchor = """      - name: Run mandatory E2 PostgreSQL integration tests
        working-directory: backend
"""
insert = """      - name: Verify schema 24 no-op after role reconciliation
        working-directory: backend
        run: go run ./cmd/migrate
      - name: Run mandatory E2 PostgreSQL integration tests
        working-directory: backend
"""
if anchor not in text:
    raise SystemExit('CI no-op anchor not found')
path.write_text(text.replace(anchor, insert, 1))

for name in ['docs/09-security-design.md', 'docs/12-deployment.md', 'deploy/README.md']:
    path = Path(name)
    text = path.read_text()
    marker = 'pharmacycrm_runtime'
    if marker not in text:
        continue
    note = ('\nThe inert `pharmacycrm_runtime` compatibility role remains `NOLOGIN` and memberless. '
            'It retains only `EXECUTE` on the two server-guarded outbox retention functions so immutable '
            'migration `000019` can be reverified during later no-op deployments; it has no table privileges '
            'and cannot be used as a credential.\n')
    if 'server-guarded outbox retention functions so immutable' not in text:
        text += note
    path.write_text(text)
PY
