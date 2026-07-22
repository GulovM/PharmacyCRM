# Deployment artifacts

Run `scripts/provision-postgres-roles.sql` once as the database owner before
running `backend/cmd/migrate`. Supply the `psql` variables `database_name`,
`runtime_role`, `runtime_password`, `migration_role`, and
`migration_password`; never place production passwords in this repository.

The script grants schema creation only to the migration role and makes the
runtime login a member of the fixed `pharmacycrm_runtime` NOLOGIN group. The
current E2 schema version is `19`; CI verifies migration from zero and the
immutable E1 version `1` upgrade path. Privilege migrations grant the group
the documented table privileges; append-only inventory movements and audit
events remain non-updatable and ordinary table deletes remain denied. Outbox
retention uses bounded terminal-only security-definer functions instead of a
table-level delete grant. The migration smoke workflow verifies the resulting
schema version and least-privilege boundary.
