# Deployment artifacts

`deploy/scripts/provision-postgres-roles.sql` is an idempotent database-owner
operation. Supply the `psql` variables `database_name`, `api_role`,
`api_password`, `worker_role`, `worker_password`, `migration_role`, and
`migration_password`; never place production passwords in this repository.

## Fresh installation

Omit `legacy_runtime_role`. Run provisioning before migrations, apply the
embedded migrations to schema version `23`, then run provisioning again. The
second run removes direct grants that immutable migrations `000014–000019`
temporarily assign to the `pharmacycrm_runtime` compatibility role.

Supported fresh and no-op paths are `0 → 23` and `23 → no-op`.

## E1 upgrade

Stop every E1 API and worker process before touching credentials. Create a
backup or restore point, then run provisioning with the exact old E1 login:

```bash
psql "$POSTGRES_ADMIN_DSN" \
  -v database_name=pharmacycrm \
  -v api_role=pharmacycrm_api \
  -v api_password="$POSTGRES_API_PASSWORD" \
  -v worker_role=pharmacycrm_worker \
  -v worker_password="$POSTGRES_WORKER_PASSWORD" \
  -v migration_role=pharmacycrm_migrator \
  -v migration_password="$POSTGRES_MIGRATION_PASSWORD" \
  -v legacy_runtime_role="$E1_RUNTIME_ROLE" \
  -f deploy/scripts/provision-postgres-roles.sql
```

The upgrade mode fails closed for an empty, missing, reserved, or conflicting
legacy role. It discovers and revokes table default ACLs through PostgreSQL
catalogs, removes direct object and database privileges, removes memberships,
sets `NOLOGIN`, and clears the password. The role is retained for audit and
ownership review; it is never dropped automatically.

Apply migrations through `POSTGRES_MIGRATION_DSN`, then run the same
provisioning command again with `legacy_runtime_role`. Verify the retired
credential cannot connect before starting E2 processes. Supported upgrade
paths are `1 → 23`, `19 → 23`, and `21 → 23`.

## Runtime identities

The API, worker, and migration processes use separate credentials:

- `POSTGRES_API_RUNTIME_DSN`;
- `POSTGRES_WORKER_RUNTIME_DSN`;
- `POSTGRES_MIGRATION_DSN`.

The script grants schema creation only to the migration login and assigns the
API and worker logins to separate `NOLOGIN` groups. `pharmacycrm_runtime`
exists only because immutable migrations reference that name. After the final
provisioning pass it is `NOLOGIN`, has no password, members, memberships,
default ACLs, or direct access to tables, sequences, functions, schema, or the
database. New API and worker logins never inherit it.

The API role cannot read migration history and receives only approved API
capabilities. The worker role receives the bounded outbox processing and
retention capabilities required by the worker process. CI verifies schema
version `23`, the least-privilege matrix, and a real isolated E1 credential
retirement through `deploy/scripts/tests/test-e1-role-upgrade.sh`.
