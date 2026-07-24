\set ON_ERROR_STOP on
-- Required psql variables: provisioning_mode=fresh|upgrade, database_name,
-- api_role, api_password, worker_role, worker_password, migration_role and
-- migration_password. Upgrade mode additionally requires legacy_runtime_role.
\if :{?provisioning_mode}
\else
\set provisioning_mode ''
\endif
\if :{?legacy_runtime_role}
\else
\set legacy_runtime_role ''
\endif

BEGIN;
CREATE TEMP TABLE pharmacycrm_role_provisioning_config (
    provisioning_mode text NOT NULL,
    database_name text NOT NULL,
    api_role text NOT NULL,
    worker_role text NOT NULL,
    migration_role text NOT NULL,
    legacy_runtime_role text NOT NULL
) ON COMMIT PRESERVE ROWS;
INSERT INTO pharmacycrm_role_provisioning_config
VALUES (:'provisioning_mode', :'database_name', :'api_role', :'worker_role',
        :'migration_role', :'legacy_runtime_role');

\ir provision-postgres-role-helpers.sql
\ir provision-postgres-role-contract.sql
COMMIT;
