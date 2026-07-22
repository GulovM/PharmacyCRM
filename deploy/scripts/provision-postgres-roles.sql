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
) ON COMMIT DROP;
INSERT INTO pharmacycrm_role_provisioning_config
VALUES (:'provisioning_mode', :'database_name', :'api_role', :'worker_role',
        :'migration_role', :'legacy_runtime_role');

CREATE OR REPLACE FUNCTION pg_temp.role_owns_cluster_objects(target_oid oid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1
        FROM pg_shdepend dependency
        WHERE dependency.refclassid = 'pg_authid'::regclass
          AND dependency.refobjid = target_oid
          AND dependency.deptype = 'o'
    )
$$;

CREATE OR REPLACE FUNCTION pg_temp.assert_role_can_be_sanitized(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS oid LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
    unsafe_parent text;
    current_database_oid oid;
BEGIN
    SELECT oid INTO target_oid FROM pg_roles WHERE rolname = target_role;
    IF target_oid IS NULL THEN
        RAISE EXCEPTION 'role % does not exist', target_role;
    END IF;
    IF pg_temp.role_owns_cluster_objects(target_oid) THEN
        RAISE EXCEPTION 'role % owns cluster or database objects; reassign ownership before provisioning', target_role;
    END IF;

    SELECT oid INTO current_database_oid FROM pg_database WHERE datname = current_database();
    IF EXISTS (
        SELECT 1
        FROM pg_shdepend dependency
        WHERE dependency.refclassid = 'pg_authid'::regclass
          AND dependency.refobjid = target_oid
          AND dependency.deptype = 'a'
          AND dependency.dbid NOT IN (0, current_database_oid)
    ) OR EXISTS (
        SELECT 1
        FROM pg_database database
        CROSS JOIN LATERAL aclexplode(database.datacl) privilege
        WHERE database.datname <> target_database
          AND privilege.grantee = target_oid
    ) THEN
        RAISE EXCEPTION 'role % has privileges in another database; retire them from that database first', target_role;
    END IF;

    IF fail_on_owning_membership THEN
        SELECT parent.rolname INTO unsafe_parent
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
          AND pg_temp.role_owns_cluster_objects(parent.oid)
        ORDER BY parent.rolname
        LIMIT 1;
        IF unsafe_parent IS NOT NULL THEN
            RAISE EXCEPTION 'role % is a member of owning role %; review ownership before retirement',
                target_role, unsafe_parent;
        END IF;
    END IF;
    RETURN target_oid;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.revoke_default_acl_grants(target_role text, target_oid oid)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    item record;
    object_keyword text;
    schema_clause text;
BEGIN
    FOR item IN
        SELECT DISTINCT owner.rolname AS owner_name,
               namespace.nspname AS schema_name,
               default_acl.defaclnamespace = 0 AS global_acl,
               default_acl.defaclobjtype AS object_type
        FROM pg_default_acl default_acl
        JOIN pg_roles owner ON owner.oid = default_acl.defaclrole
        LEFT JOIN pg_namespace namespace ON namespace.oid = default_acl.defaclnamespace
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE privilege.grantee = target_oid
    LOOP
        object_keyword := CASE item.object_type
            WHEN 'r' THEN 'TABLES'
            WHEN 'S' THEN 'SEQUENCES'
            WHEN 'f' THEN 'FUNCTIONS'
            WHEN 'T' THEN 'TYPES'
            WHEN 'n' THEN 'SCHEMAS'
            ELSE NULL
        END;
        IF object_keyword IS NULL THEN
            RAISE EXCEPTION 'unsupported default ACL object type % for role %', item.object_type, target_role;
        END IF;
        schema_clause := CASE
            WHEN item.global_acl OR item.object_type = 'n' THEN ''
            ELSE format(' IN SCHEMA %I', item.schema_name)
        END;
        EXECUTE format(
            'ALTER DEFAULT PRIVILEGES FOR ROLE %I%s REVOKE ALL PRIVILEGES ON %s FROM %I',
            item.owner_name, schema_clause, object_keyword, target_role
        );
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.revoke_all_memberships(target_role text, target_oid oid)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT parent.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', item.role_name, target_role);
    END LOOP;
    FOR item IN
        SELECT member.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles member ON member.oid = membership.member
        WHERE membership.roleid = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', target_role, item.role_name);
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.verify_role_has_no_direct_capabilities(
    target_role text,
    target_oid oid,
    target_database text,
    allow_login boolean
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    remaining text[] := ARRAY[]::text[];
BEGIN
    IF NOT allow_login AND EXISTS (SELECT 1 FROM pg_roles WHERE oid = target_oid AND rolcanlogin) THEN
        remaining := array_append(remaining, 'login');
    END IF;
    IF NOT allow_login AND EXISTS (SELECT 1 FROM pg_authid WHERE oid = target_oid AND rolpassword IS NOT NULL) THEN
        remaining := array_append(remaining, 'password');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_default_acl default_acl
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'default_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_database database
        CROSS JOIN LATERAL aclexplode(database.datacl) privilege
        WHERE database.datname = target_database AND privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'database_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_namespace namespace
        CROSS JOIN LATERAL aclexplode(namespace.nspacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'schema_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_class relation
        CROSS JOIN LATERAL aclexplode(relation.relacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'relation_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_attribute attribute
        CROSS JOIN LATERAL aclexplode(attribute.attacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'column_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_proc procedure
        CROSS JOIN LATERAL aclexplode(procedure.proacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'function_acl');
    END IF;
    IF cardinality(remaining) > 0 THEN
        RAISE EXCEPTION 'role % retains direct capabilities: %', target_role, array_to_string(remaining, ',');
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.retire_runtime_role(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
BEGIN
    target_oid := pg_temp.assert_role_can_be_sanitized(target_role, target_database, fail_on_owning_membership);
    PERFORM pg_temp.revoke_default_acl_grants(target_role, target_oid);
    EXECUTE format('DROP OWNED BY %I', target_role);
    PERFORM pg_temp.revoke_all_memberships(target_role, target_oid);
    EXECUTE format(
        'ALTER ROLE %I WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
        target_role
    );
    IF EXISTS (SELECT 1 FROM pg_auth_members WHERE roleid = target_oid OR member = target_oid) THEN
        RAISE EXCEPTION 'retired role % retains memberships', target_role;
    END IF;
    PERFORM pg_temp.verify_role_has_no_direct_capabilities(target_role, target_oid, target_database, false);
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.sanitize_runtime_login(target_role text, target_database text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
BEGIN
    target_oid := pg_temp.assert_role_can_be_sanitized(target_role, target_database, false);
    PERFORM pg_temp.revoke_default_acl_grants(target_role, target_oid);
    EXECUTE format('DROP OWNED BY %I', target_role);
    PERFORM pg_temp.revoke_all_memberships(target_role, target_oid);
    PERFORM pg_temp.verify_role_has_no_direct_capabilities(target_role, target_oid, target_database, true);
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.keep_only_group_member(group_role text, expected_member text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    group_oid oid;
    member record;
    parent record;
BEGIN
    SELECT oid INTO STRICT group_oid FROM pg_roles WHERE rolname = group_role;
    FOR parent IN
        SELECT role.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles role ON role.oid = membership.roleid
        WHERE membership.member = group_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', parent.role_name, group_role);
    END LOOP;
    FOR member IN
        SELECT role.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles role ON role.oid = membership.member
        WHERE membership.roleid = group_oid
          AND role.rolname <> expected_member
    LOOP
        EXECUTE format('REVOKE %I FROM %I', group_role, member.role_name);
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.verify_exact_membership(member_role text, group_role text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    member_oid oid;
    group_oid oid;
    parent_count integer;
    child_count integer;
BEGIN
    SELECT oid INTO STRICT member_oid FROM pg_roles WHERE rolname = member_role;
    SELECT oid INTO STRICT group_oid FROM pg_roles WHERE rolname = group_role;
    SELECT count(*) INTO parent_count FROM pg_auth_members WHERE member = member_oid;
    SELECT count(*) INTO child_count FROM pg_auth_members WHERE roleid = member_oid;
    IF parent_count <> 1 OR child_count <> 0 OR NOT EXISTS (
        SELECT 1 FROM pg_auth_members WHERE roleid = group_oid AND member = member_oid
    ) THEN
        RAISE EXCEPTION 'role % does not have the exact expected membership in %', member_role, group_role;
    END IF;
END
$$;

DO $$
DECLARE
    config pharmacycrm_role_provisioning_config%ROWTYPE;
    reserved text;
    declared_version bigint;
BEGIN
    SELECT * INTO STRICT config FROM pharmacycrm_role_provisioning_config;
    IF config.provisioning_mode NOT IN ('fresh', 'upgrade') THEN
        RAISE EXCEPTION 'provisioning_mode must be explicitly set to fresh or upgrade';
    END IF;
    IF btrim(config.database_name) = '' OR btrim(config.api_role) = ''
       OR btrim(config.worker_role) = '' OR btrim(config.migration_role) = '' THEN
        RAISE EXCEPTION 'database and service role names must not be empty';
    END IF;
    IF config.database_name <> current_database() THEN
        RAISE EXCEPTION 'database_name % does not match current database %', config.database_name, current_database();
    END IF;
    IF config.api_role = current_user OR config.worker_role = current_user
       OR config.migration_role = current_user OR config.legacy_runtime_role = current_user THEN
        RAISE EXCEPTION 'provisioning principal must not be a managed service or legacy role';
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

    IF to_regclass('public.pharmacycrm_schema_metadata') IS NOT NULL THEN
        SELECT schema_version INTO STRICT declared_version
        FROM pharmacycrm_schema_metadata WHERE singleton;
    END IF;
    IF config.provisioning_mode = 'fresh' THEN
        IF btrim(config.legacy_runtime_role) <> '' THEN
            RAISE EXCEPTION 'legacy_runtime_role is valid only in upgrade mode';
        END IF;
        IF declared_version IS NOT NULL AND declared_version <> 23 THEN
            RAISE EXCEPTION 'fresh mode cannot provision existing schema version %; use upgrade mode with the exact E1 runtime role', declared_version;
        END IF;
    ELSE
        IF btrim(config.legacy_runtime_role) = '' THEN
            RAISE EXCEPTION 'legacy_runtime_role must not be empty in upgrade mode';
        END IF;
        IF declared_version IS NOT NULL AND declared_version NOT IN (1, 19, 21, 23) THEN
            RAISE EXCEPTION 'schema version % is not a supported E1/E2 upgrade state', declared_version;
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

    IF config.provisioning_mode = 'upgrade' THEN
        PERFORM pg_temp.retire_runtime_role(config.legacy_runtime_role, config.database_name, true);
    END IF;
    -- Immutable migrations 000014-000019 need this name during migration. Every
    -- pre- and post-migration provisioning run reconciles it to an inert role.
    PERFORM pg_temp.retire_runtime_role('pharmacycrm_runtime', config.database_name, false);
END
$$;

ALTER ROLE pharmacycrm_api_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_worker_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_migration WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'api_role', :'api_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'api_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'worker_role', :'worker_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'worker_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'migration_role', :'migration_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migration_role') \gexec

SELECT pg_temp.sanitize_runtime_login(:'api_role', :'database_name');
SELECT pg_temp.sanitize_runtime_login(:'worker_role', :'database_name');

ALTER ROLE :"api_role" WITH LOGIN PASSWORD :'api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"worker_role" WITH LOGIN PASSWORD :'worker_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"migration_role" WITH LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

SELECT pg_temp.revoke_all_memberships(:'migration_role', (SELECT oid FROM pg_roles WHERE rolname = :'migration_role'));
SELECT pg_temp.keep_only_group_member('pharmacycrm_api_runtime', :'api_role');
SELECT pg_temp.keep_only_group_member('pharmacycrm_worker_runtime', :'worker_role');
SELECT pg_temp.keep_only_group_member('pharmacycrm_migration', :'migration_role');

GRANT pharmacycrm_api_runtime TO :"api_role" WITH INHERIT TRUE;
GRANT pharmacycrm_worker_runtime TO :"worker_role" WITH INHERIT TRUE;
GRANT pharmacycrm_migration TO :"migration_role" WITH INHERIT TRUE;

GRANT CONNECT ON DATABASE :"database_name" TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime;
GRANT USAGE ON SCHEMA public TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime, pharmacycrm_migration;
GRANT CONNECT, CREATE ON DATABASE :"database_name" TO :"migration_role";
GRANT USAGE, CREATE ON SCHEMA public TO :"migration_role";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;

SELECT pg_temp.verify_exact_membership(:'api_role', 'pharmacycrm_api_runtime');
SELECT pg_temp.verify_exact_membership(:'worker_role', 'pharmacycrm_worker_runtime');
SELECT pg_temp.verify_exact_membership(:'migration_role', 'pharmacycrm_migration');
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'api_role', (SELECT oid FROM pg_roles WHERE rolname = :'api_role'), :'database_name', true
);
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'worker_role', (SELECT oid FROM pg_roles WHERE rolname = :'worker_role'), :'database_name', true
);
COMMIT;
