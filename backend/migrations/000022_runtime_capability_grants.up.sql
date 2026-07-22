-- E2-FIX-022..024: grant narrow API capabilities after runtime role split.
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
