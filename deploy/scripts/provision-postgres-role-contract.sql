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
        IF declared_version IS NOT NULL AND declared_version <> 24 THEN
            RAISE EXCEPTION 'fresh mode cannot provision existing schema version %; use upgrade mode with the exact E1 runtime role', declared_version;
        END IF;
    ELSE
        IF btrim(config.legacy_runtime_role) = '' THEN
            RAISE EXCEPTION 'legacy_runtime_role must not be empty in upgrade mode';
        END IF;
        IF declared_version IS NOT NULL AND declared_version NOT IN (1, 19, 21, 23, 24) THEN
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
        PERFORM pg_temp.retire_role(config.legacy_runtime_role, config.database_name, true);
    END IF;
    PERFORM pg_temp.retire_role('pharmacycrm_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_api_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_worker_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_migration', config.database_name, false);
END
$$;

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'api_role', :'api_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'api_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'worker_role', :'worker_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'worker_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'migration_role', :'migration_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migration_role') \gexec

SELECT pg_temp.sanitize_role_capabilities(:'api_role', :'database_name', false);
SELECT pg_temp.sanitize_role_capabilities(:'worker_role', :'database_name', false);
SELECT pg_temp.revoke_all_memberships(:'migration_role', (SELECT oid FROM pg_roles WHERE rolname = :'migration_role'));

ALTER ROLE pharmacycrm_api_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_worker_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_migration WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"api_role" WITH LOGIN PASSWORD :'api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"worker_role" WITH LOGIN PASSWORD :'worker_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"migration_role" WITH LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

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

DO $$
BEGIN
    IF to_regclass('public.users') IS NULL THEN
        RETURN;
    END IF;

    GRANT SELECT ON pharmacycrm_schema_metadata, roles, pharmacies, pharmacy_assignments,
        products, product_presentations, product_barcodes, product_requests, import_jobs,
        import_rows, pharmacy_products, inventory_operations, receipts, receipt_items,
        stock_lots, inventory_movements, write_offs, write_off_items, inventory_adjustments,
        inventory_adjustment_items, sales, sale_items, sale_item_allocations, sale_returns,
        sale_return_items, sale_return_item_allocations, idempotency_records, outbox_events,
        alerts, public_availability_projection TO pharmacycrm_api_runtime;
    GRANT SELECT (id, login, display_name, phone, status, failed_login_attempts,
        locked_until, password_changed_at, last_login_at, version, created_at, updated_at,
        blocked_at, archived_at) ON users TO pharmacycrm_api_runtime;
    GRANT SELECT (id, user_id, token_family_id, generation, user_agent, ip_address,
        created_at, last_used_at, expires_at, idle_expires_at, absolute_expires_at,
        authentication_method, mfa_level, revoked_at, revoke_reason)
        ON user_sessions TO pharmacycrm_api_runtime;
    GRANT INSERT, UPDATE (last_used_at, revoked_at, revoke_reason)
        ON user_sessions TO pharmacycrm_api_runtime;
    GRANT INSERT ON users, user_roles, user_sessions, idempotency_records,
        outbox_events, audit_events TO pharmacycrm_api_runtime;
    GRANT UPDATE (status, response_status, response_body, resource_type,
        resource_id, completed_at, expires_at)
        ON idempotency_records TO pharmacycrm_api_runtime;

    GRANT SELECT ON pharmacycrm_schema_metadata, outbox_events TO pharmacycrm_worker_runtime;
    GRANT UPDATE (status, attempt_count, available_at, lease_token, lease_generation,
        leased_by, lease_expires_at, last_error_code, last_error_at,
        processed_at, dead_lettered_at)
        ON outbox_events TO pharmacycrm_worker_runtime;

    IF to_regprocedure('public.replay_dead_letter_outbox_event(uuid,timestamptz)') IS NOT NULL THEN
        GRANT EXECUTE ON FUNCTION public.replay_dead_letter_outbox_event(uuid,timestamptz)
            TO pharmacycrm_api_runtime;
    END IF;
    IF to_regprocedure('public.delete_processed_outbox_events_before(timestamptz,integer)') IS NOT NULL THEN
        -- pharmacycrm_runtime remains NOLOGIN and memberless, but keeps this
        -- historical capability so immutable migration 000019 can be reverified.
        GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz,integer),
            public.delete_dead_letter_outbox_events_before(timestamptz,integer)
            TO pharmacycrm_worker_runtime, pharmacycrm_runtime;
    END IF;
END
$$;

SELECT pg_temp.verify_exact_membership(:'api_role', 'pharmacycrm_api_runtime');
SELECT pg_temp.verify_exact_membership(:'worker_role', 'pharmacycrm_worker_runtime');
SELECT pg_temp.verify_exact_membership(:'migration_role', 'pharmacycrm_migration');
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'api_role', (SELECT oid FROM pg_roles WHERE rolname = :'api_role'), :'database_name', true
);
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'worker_role', (SELECT oid FROM pg_roles WHERE rolname = :'worker_role'), :'database_name', true
);
