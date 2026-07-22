# Deployment artifacts

Run `scripts/provision-postgres-roles.sql` once as the database owner before
running `backend/cmd/migrate`. Supply the `psql` variables `database_name`,
`api_role`, `api_password`, `worker_role`, `worker_password`, `migration_role`, and
`migration_password`; never place production passwords in this repository.

The script grants schema creation only to the migration role and makes the API
and worker login roles members of separate NOLOGIN groups. It also creates the
NOLOGIN `pharmacycrm_runtime` compatibility role required by immutable legacy
migrations; no runtime login is its member. The current E2 schema version is
`23`; CI verifies fresh, E1-to-23, 19-to-23 and 21-to-23 upgrades. The API role cannot
read migration history and receives only idempotency lifecycle columns plus
the `replay_dead_letter_outbox_event` capability function; it never receives a
table-level outbox update. Worker retention uses bounded terminal-only
security-definer functions instead of a table-level delete grant. The
migration smoke workflow verifies the resulting schema version and
least-privilege boundary.
