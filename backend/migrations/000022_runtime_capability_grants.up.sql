-- E2-FIX-022..024: grant narrow API capabilities after runtime role split.
-- E2-FIX-032 Verification query: SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_runtime' AND NOT rolcanlogin) AND NOT EXISTS (SELECT 1 FROM pg_auth_members membership JOIN pg_roles role ON role.oid = membership.roleid WHERE role.rolname = 'pharmacycrm_runtime') AND NOT pg_has_role('pharmacycrm_api_runtime', 'pharmacycrm_runtime', 'member') AND NOT pg_has_role('pharmacycrm_worker_runtime', 'pharmacycrm_runtime', 'member') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'status', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'response_status', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'response_body', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'resource_type', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'resource_id', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'completed_at', 'UPDATE') AND has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'expires_at', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'id', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'actor_user_id', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'pharmacy_id', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'operation', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'idempotency_key', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'scope_key', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'request_hash', 'UPDATE') AND NOT has_column_privilege('pharmacycrm_api_runtime', 'public.idempotency_records', 'created_at', 'UPDATE') AND EXISTS (SELECT 1 FROM pg_proc procedure JOIN pg_roles owner ON owner.oid = procedure.proowner WHERE procedure.oid = 'public.replay_dead_letter_outbox_event(uuid,timestamptz)'::regprocedure AND procedure.prosecdef AND owner.rolname NOT IN ('pharmacycrm_api_runtime', 'pharmacycrm_worker_runtime') AND EXISTS (SELECT 1 FROM unnest(COALESCE(procedure.proconfig, ARRAY[]::text[])) setting WHERE regexp_replace(setting, '[[:space:]]', '', 'g') = 'search_path=pg_catalog,public')) AND has_function_privilege('pharmacycrm_api_runtime', 'public.replay_dead_letter_outbox_event(uuid,timestamptz)', 'EXECUTE') AND NOT has_function_privilege('pharmacycrm_worker_runtime', 'public.replay_dead_letter_outbox_event(uuid,timestamptz)', 'EXECUTE') AND NOT EXISTS (SELECT 1 FROM pg_proc procedure CROSS JOIN LATERAL aclexplode(COALESCE(procedure.proacl, acldefault('f', procedure.proowner))) privilege WHERE procedure.oid = 'public.replay_dead_letter_outbox_event(uuid,timestamptz)'::regprocedure AND privilege.grantee = 0 AND privilege.privilege_type = 'EXECUTE') AND NOT has_table_privilege('pharmacycrm_api_runtime', 'public.outbox_events', 'UPDATE') AND NOT has_table_privilege('pharmacycrm_api_runtime', 'public.outbox_events', 'DELETE') AND NOT has_table_privilege('pharmacycrm_api_runtime', 'public.outbox_events', 'TRUNCATE');
-- This migration deliberately never grants the legacy pharmacycrm_runtime role.

GRANT UPDATE (
    status,
    response_status,
    response_body,
    resource_type,
    resource_id,
    completed_at,
    expires_at
) ON idempotency_records TO pharmacycrm_api_runtime;

CREATE OR REPLACE FUNCTION public.replay_dead_letter_outbox_event(
    p_event_id uuid,
    p_available_at timestamptz
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    affected_rows integer;
BEGIN
    UPDATE public.outbox_events
    SET status = 'PENDING',
        attempt_count = 0,
        available_at = p_available_at,
        last_error_code = NULL,
        last_error_at = NULL,
        dead_lettered_at = NULL,
        lease_token = NULL,
        leased_by = NULL,
        lease_expires_at = NULL
    WHERE id = p_event_id
      AND status = 'DEAD_LETTER';

    GET DIAGNOSTICS affected_rows = ROW_COUNT;
    RETURN affected_rows = 1;
END;
$$;

REVOKE ALL ON FUNCTION public.replay_dead_letter_outbox_event(uuid, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.replay_dead_letter_outbox_event(uuid, timestamptz) TO pharmacycrm_api_runtime;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_runtime' AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'legacy pharmacycrm_runtime compatibility role is missing or may log in';
    END IF;
    IF EXISTS (
        SELECT 1
        FROM pg_auth_members membership
        JOIN pg_roles role ON role.oid = membership.roleid
        WHERE role.rolname = 'pharmacycrm_runtime'
    ) THEN
        RAISE EXCEPTION 'legacy pharmacycrm_runtime compatibility role must not have members';
    END IF;
END;
$$;
