# Deployment artifacts

Run `scripts/provision-postgres-roles.sql` once as the database owner before
running `backend/cmd/migrate`. Supply the `psql` variables `database_name`,
`runtime_role`, `runtime_password`, `migration_role`, and
`migration_password`; never place production passwords in this repository.

The script grants schema creation only to the migration role and establishes
default read-only `SELECT` privileges for the runtime role on all tables that
migrations create. The migration smoke workflow executes this artifact and
verifies that the runtime identity can read schema metadata but cannot create
schema objects.
