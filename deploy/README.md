# Deployment artifacts

`deploy/scripts/provision-postgres-roles.sql` is an idempotent privileged
PostgreSQL operation. The execution principal must own the target database,
be allowed to create and alter roles, and be able to read `pg_authid`; in most
installations this means a controlled PostgreSQL administrator or superuser.
An ordinary application database owner is not sufficient.

Always supply the explicit `provisioning_mode` variable together with
`database_name`, `api_role`, `api_password`, `worker_role`, `worker_password`,
`migration_role`, and `migration_password`. Never place production passwords
in this repository.

## Fresh installation

Use `-v provisioning_mode=fresh` and omit `legacy_runtime_role`. Fresh mode is
accepted only for an empty database or an already reconciled schema version
`23`. It fails closed when schema metadata identifies an older E1/E2 version,
so an operator cannot accidentally leave an E1 credential active by choosing
the wrong procedure.

Run provisioning before migrations, apply the embedded migrations to schema
version `23`, then run the same fresh provisioning command again. The second
run removes direct grants that immutable migrations `000014–000019`
temporarily assign to the `pharmacycrm_runtime` compatibility role.

Supported fresh and no-op paths are `0 → 24` and `23 → no-op`.

## E1 upgrade

Stop every E1 API and worker process before touching credentials. Create a
backup or restore point, then run provisioning with the exact old E1 login:

```bash
psql "$POSTGRES_ADMIN_DSN" \
  -v provisioning_mode=upgrade \
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

Upgrade mode fails closed when the explicit mode or legacy role is missing,
empty, reserved, conflicting, or unsupported by the declared schema version.
Cluster-wide ownership dependencies are checked through `pg_shdepend`. A
legacy role that owns objects, belongs to an owning parent role, or still has
privileges in another database must be remediated explicitly before retirement.

Within the target database, provisioning discovers and revokes default ACLs,
direct object/database privileges, and memberships, then sets `NOLOGIN` and
clears the password. The legacy role is retained for audit; it is never dropped
automatically.

Apply migrations through `POSTGRES_MIGRATION_DSN`, then run the same upgrade
provisioning command again. Verify the retired credential cannot connect before
starting E2 processes. Supported upgrade paths are `1 → 24`, `19 → 24`, and
`21 → 24`.

## Runtime identities

The API, worker, and migration processes use separate credentials:

- `POSTGRES_API_RUNTIME_DSN`;
- `POSTGRES_WORKER_RUNTIME_DSN`;
- `POSTGRES_MIGRATION_DSN`.

Every provisioning pass sanitizes the API and worker login roles before it
assigns the single approved `NOLOGIN` group. Unexpected memberships, direct
table/column/function/schema/database ACLs, and default ACLs are removed. The
migration login remains the controlled owner/migrator identity and is never
injected into API or worker processes.

`pharmacycrm_runtime` exists only because immutable migrations reference that
name. After the final provisioning pass it is `NOLOGIN`, has no password,
members, memberships, default ACLs, or direct access to target database
objects. New API and worker logins never inherit it.

The E2 worker has no domain consumers yet. Its empty protocol registry runs in
an explicit maintenance-only mode: it can terminalize expired exhausted
leases and execute retention, but it cannot claim an unknown business event.

The cluster-role integration test is destructive by design and must run only
against a disposable PostgreSQL cluster with
`ALLOW_DESTRUCTIVE_CLUSTER_ROLE_TEST=true`. It refuses to start when reserved
PharmacyCRM roles already exist. CI verifies schema version `24`, the
least-privilege matrix, real E1 credential retirement, polluted service-role
reconciliation, and production worker maintenance wiring.
