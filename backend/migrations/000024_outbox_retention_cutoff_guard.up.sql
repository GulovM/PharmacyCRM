-- E2-FIX-045: enforce retention windows inside SECURITY DEFINER functions.
-- Verification query: SELECT position('30 days' in pg_get_functiondef('public.delete_processed_outbox_events_before(timestamptz,integer)'::regprocedure)) > 0 AND position('180 days' in pg_get_functiondef('public.delete_dead_letter_outbox_events_before(timestamptz,integer)'::regprocedure)) > 0 AND has_function_privilege('pharmacycrm_worker_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND NOT has_function_privilege('pharmacycrm_api_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE');
-- Lock/rewrite assessment: replaces two functions only; no table scan or rewrite.
-- Compatibility: signatures are unchanged; unsafe caller-supplied cutoffs now fail with SQLSTATE 22023.
-- Forward-fix policy: published migrations remain immutable; later corrections require another forward migration.

CREATE OR REPLACE FUNCTION public.delete_processed_outbox_events_before(
    p_before timestamptz,
    p_limit integer
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
    newest_allowed timestamptz := statement_timestamp() - interval '30 days';
BEGIN
    IF p_before IS NULL OR p_before > newest_allowed OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid processed outbox retention request' USING ERRCODE = '22023';
    END IF;
    WITH candidates AS (
        SELECT id FROM public.outbox_events
        WHERE status = 'PROCESSED' AND processed_at < p_before
        ORDER BY processed_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT p_limit
    ), deleted AS (
        DELETE FROM public.outbox_events AS event
        USING candidates
        WHERE event.id = candidates.id AND event.status = 'PROCESSED'
        RETURNING 1
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    RETURN deleted_count;
END;
$$;

CREATE OR REPLACE FUNCTION public.delete_dead_letter_outbox_events_before(
    p_before timestamptz,
    p_limit integer
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
    newest_allowed timestamptz := statement_timestamp() - interval '180 days';
BEGIN
    IF p_before IS NULL OR p_before > newest_allowed OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid dead-letter outbox retention request' USING ERRCODE = '22023';
    END IF;
    WITH candidates AS (
        SELECT id FROM public.outbox_events
        WHERE status = 'DEAD_LETTER' AND dead_lettered_at < p_before
        ORDER BY dead_lettered_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT p_limit
    ), deleted AS (
        DELETE FROM public.outbox_events AS event
        USING candidates
        WHERE event.id = candidates.id AND event.status = 'DEAD_LETTER'
        RETURNING 1
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    RETURN deleted_count;
END;
$$;

REVOKE ALL ON FUNCTION public.delete_processed_outbox_events_before(timestamptz, integer) FROM PUBLIC, pharmacycrm_runtime, pharmacycrm_api_runtime;
REVOKE ALL ON FUNCTION public.delete_dead_letter_outbox_events_before(timestamptz, integer) FROM PUBLIC, pharmacycrm_runtime, pharmacycrm_api_runtime;
GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz, integer),
    public.delete_dead_letter_outbox_events_before(timestamptz, integer)
    TO pharmacycrm_worker_runtime;
