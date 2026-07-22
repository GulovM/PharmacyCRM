\set ON_ERROR_STOP on
-- Required psql variables: database_name, api_role, api_password, worker_role,
-- worker_password, migration_role and migration_password. Define
-- legacy_runtime_role only for an E1 -> E2 upgrade; an empty value fails closed.
\if :{?legacy_runtime_role}
\set pharmacycrm_upgrade_mode true
\else
\set legacy_runtime_role ''
\set pharmacycrm_upgrade_mode false
\endif

BEGIN;
CREATE TEMP TABLE pharmacycrm_role_provisioning_config (
    database_name text NOT NULL,
    api_role text NOT NULL,
    worker_role text NOT NULL,
    migration_role text NOT NULL,
    legacy_runtime_role text NOT NULL,
    upgrade_mode boolean NOT NULL
) ON COMMIT DROP;
INSERT INTO pharmacycrm_role_provisioning_config
VALUES (:'database_name', :'api_role', :'worker_role', :'migration_role',
        :'legacy_runtime_role', :'pharmacycrm_upgrade_mode'::boolean);

CREATE OR REPLACE FUNCTION pg_temp.retire_runtime_role(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
    item record;
    unsafe_owner text;
    remaining_capabilities text[] := ARRAY[]::text[];
BEGIN
    SELECT oid INTO target_oid FROM pg_roles WHERE rolname = target_role;
    IF target_oid IS NULL THEN
        RAISE EXCEPTION 'runtime role % does not exist', target_role;
    END IF;

    IF EXISTS (SELECT 1 FROM pg_database WHERE datdba = target_oid)
       OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspowner = target_oid)
       OR EXISTS (SELECT 1 FROM pg_class WHERE relowner = target_oid)
       OR EXISTS (SELECT 1 FROM pg_proc WHERE proowner = target_oid) THEN
        RAISE EXCEPTION 'runtime role % owns database objects; reassign ownership before retirement', target_role;
    END IF;

    IF fail_on_owning_membership THEN
        SELECT parent.rolname INTO unsafe_owner
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
          AND (
              EXISTS (SELECT 1 FROM pg_database WHERE datdba = parent.oid)
              OR EXISTS (SELECT 1 FROM pg_namespace WHERE nspowner = parent.oid)
              OR EXISTS (SELECT 1 FROM pg_class WHERE relowner = parent.oid)
              OR EXISTS (SELECT 1 FROM pg_proc WHERE proowner = parent.oid)
          )
        ORDER BY parent.rolname LIMIT 1;
        IF unsafe_owner IS NOT NULL THEN
            RAISE EXCEPTION 'runtime role % is a member of owning role %; review ownership before retirement',
                target_role, unsafe_owner;
        END IF;
    END IF;

    FOR item IN
        SELECT DISTINCT owner.rolname AS owner_name,
               namespace.nspname AS schema_name,
               default_acl.defaclnamespace = 0 AS global_acl
        FROM pg_default_acl default_acl
        JOIN pg_roles owner ON owner.oid = default_acl.defaclrole
        LEFT JOIN pg_namespace namespace ON namespace.oid = default_acl.defaclnamespace
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE default_acl.defaclobjtype = 'r' AND privilege.grantee = target_oid
    LOOP
        EXECUTE format(
            'ALTER DEFAULT PRIVILEGES FOR ROLE %I%s REVOKE ALL PRIVILEGES ON TABLES FROM %I',
            item.owner_name,
            CASE WHEN item.global_acl THEN '' ELSE format(' IN SCHEMA %I', item.schema_name) END,
            target_role
        );
    END LOOP;

    FOR item IN
        SELECT namespace.nspname AS schema_name, relation.relname AS relation_name,
               string_agg(format('%I', attribute.attname), ', ' ORDER BY attribute.attnum) AS columns
        FROM pg_attribute attribute
        JOIN pg_class relation ON relation.oid = attribute.attrelid
        JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
        CROSS JOIN LATERAL aclexplode(attribute.attacl) privilege
        WHERE namespace.nspname = 'public'
          AND attribute.attnum > 0 AND NOT attribute.attisdropped
          AND privilege.grantee = target_oid
        GROUP BY namespace.nspname, relation.relname
    LOOP
        EXECUTE format('REVOKE ALL PRIVILEGES (%s) ON TABLE %I.%I FROM %I',
                       item.columns, item.schema_name, item.relation_name, target_role);
    END LOOP;

    EXECUTE format('REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM %I', target_role);
    EXECUTE format('REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM %I', target_role);
    EXECUTE format('REVOKE ALL PRIVILEGES ON ALL FUNCTIONS IN SCHEMA public FROM %I', target_role);
    EXECUTE format('REVOKE ALL PRIVILEGES ON SCHEMA public FROM %I', target_role);
    EXECUTE format('REVOKE ALL PRIVILEGES ON DATABASE %I FROM %I', target_database, target_role);

    FOR item IN
        SELECT parent.rolname AS parent_name
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', item.parent_name, target_role);
    END LOOP;
    FOR item IN
        SELECT member.rolname AS member_name
        FROM pg_auth_members membership
        JOIN pg_roles member ON member.oid = membership.member
        WHERE membership.roleid = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', target_role, item.member_name);
    END LOOP;

    EXECUTE format(
        'ALTER ROLE %I WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
        target_role
    );

    IF EXISTS (SELECT 1 FROM pg_roles WHERE oid = target_oid AND rolcanlogin) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'login');
    END IF;
    IF EXISTS (SELECT 1 FROM pg_authid WHERE oid = target_oid AND rolpassword IS NOT NULL) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'password');
    END IF;
    IF EXISTS (SELECT 1 FROM pg_auth_members WHERE roleid = target_oid OR member = target_oid) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'membership');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_default_acl default_acl
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE default_acl.defaclobjtype = 'r' AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'default_table_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_database database
        CROSS JOIN LATERAL aclexplode(database.datacl) privilege
        WHERE database.datname = target_database AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'database_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_namespace namespace
        CROSS JOIN LATERAL aclexplode(namespace.nspacl) privilege
        WHERE namespace.nspname = 'public' AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'schema_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_class relation
        JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
        CROSS JOIN LATERAL aclexplode(relation.relacl) privilege
        WHERE namespace.nspname = 'public' AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'relation_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_attribute attribute
        JOIN pg_class relation ON relation.oid = attribute.attrelid
        JOIN pg_namespace namespace ON namespace.oid = relation.relnamespace
        CROSS JOIN LATERAL aclexplode(attribute.attacl) privilege
        WHERE namespace.nspname = 'public' AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'column_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_proc procedure
        JOIN pg_namespace namespace ON namespace.oid = procedure.pronamespace
        CROSS JOIN LATERAL aclexplode(procedure.proacl) privilege
        WHERE namespace.nspname = 'public' AND privilege.grantee = target_oid
    ) THEN
        remaining_capabilities := array_append(remaining_capabilities, 'function_acl');
    END IF;
    IF cardinality(remaining_capabilities) > 0 THEN
        RAISE EXCEPTION 'runtime role % was not fully retired; remaining capabilities: %',
            target_role, array_to_string(remaining_capabilities, ',');
    END IF;
END
$$;

DO $$
DECLARE
    config pharmacycrm_role_provisioning_config%ROWTYPE;
    reserved text;
BEGIN
    SELECT * INTO STRICT config FROM pharmacycrm_role_provisioning_config;
    IF btrim(config.database_name) = '' OR btrim(config.api_role) = ''
       OR btrim(config.worker_role) = '' OR btrim(config.migration_role) = '' THEN
        RAISE EXCEPTION 'database and service role names must not be empty';
    END IF;
    IF config.api_role = config.worker_role OR config.api_role = config.migration_role
       OR config.worker_role = config.migration_role THEN
        RAISE EXCEPTION 'API, worker and migration login roles must be distinct';
    END IF;
    FOREACH reserved IN ARRAY ARRAY[
        'pharmacycrm_api_runtime', 'pharmacycrm_worker_runtime',
        'pharmacycrm_migration', 'pharmacycrm_runtime'
    ] LOOP
        IF config.api_role = reserved OR config.worker_role = reserved OR config.migration_role = reserved THEN
            RAISE EXCEPTION 'service login role % collides with reserved role', reserved;
        END IF;
    END LOOP;
    IF config.upgrade_mode THEN
        IF btrim(config.legacy_runtime_role) = '' THEN
            RAISE EXCEPTION 'legacy_runtime_role must not be empty in upgrade mode';
        END IF;
        IF config.legacy_runtime_role IN (
            config.api_role, config.worker_role, config.migration_role,
            'pharmacycrm_api_runtime', 'pharmacycrm_worker_runtime', 'pharmacycrm_migration'
        ) THEN
            RAISE EXCEPTION 'legacy runtime role % collides with an E2 service role', config.legacy_runtime_role;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = config.legacy_runtime_role) THEN
            RAISE EXCEPTION 'legacy runtime role % does not exist', config.legacy_runtime_role;
        END IF;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_api_runtime') THEN
        CREATE ROLE pharmacycrm_api_runtime NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_worker_runtime') THEN
        CREATE ROLE pharmacycrm_worker_runtime NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_migration') THEN
        CREATE ROLE pharmacycrm_migration NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_runtime') THEN
        CREATE ROLE pharmacycrm_runtime NOLOGIN;
    END IF;

    IF config.upgrade_mode THEN
        PERFORM pg_temp.retire_runtime_role(config.legacy_runtime_role, config.database_name, true);
    END IF;
    -- Immutable migrations 000014-000019 need this name during migration. Every
    -- pre- and post-migration provisioning run reconciles it to an inert role.
    PERFORM pg_temp.retire_runtime_role('pharmacycrm_runtime', config.database_name, false);
END
$$;

ALTER ROLE pharmacycrm_api_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_worker_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_migration WITH NOLOGIN PASSORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'api_role', :'api_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'api_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'worker_role', :'worker_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'worker_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'migration_role', :'migration_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migration_role') \gexec

ALTER ROLE :"api_role" WITH LOGIN PASSWORD :'api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"worker_role" WITH LOGIN PASSWORD :'worker_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"migration_role" WITH LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

REVOKE pharmacycrm_worker_runtime, pharmacycrm_migration, pharmacycrm_runtime FROM :"api_role";
REVOKE pharmacycrm_api_runtime, pharmacycrm_migration, pharmacycrm_runtime FROM :"worker_role";
REVOKE pharmacycrm_api_runtime, pharmacycrm_worker_runtime, pharmacycrm_runtime FROM :"migration_role";
GRANT pharmacycrm_api_runtime TO :"api_role" WITH INHERIT TRUE;
GRANT pharmacycrm_worker_runtime TO :"worker_role" WITH INHERIT TRUE;
GRANT pharmacycrm_migration TO :"migration_role" WITH INHERIT TRUE;

GRANT CONNECT ON DATABASE :"database_name" TO :"api_role", :"worker_role", :"migration_role";
GRANT CREATE ON DATABASE :"database_name" TO :"migration_role";
GRANT USAGE ON SCHEMA public TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime,
    pharmacycrm_migration, :"api_role", :"worker_role", :"migration_role";
GRANT CREATE ON SCHEMA public TO :"migration_role";
REVOKE CREATE ON SCHEMA public FROM :"api_role", :"worker_role";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;
COMMIT;
