-- E2-FIX-011: expose bounded terminal-only outbox retention without table DELETE privilege.
-- Verification query: SELECT has_function_privilege('pharmacycrm_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND has_function_privilege('pharmacycrm_runtime','public.delete_dead_letter_outbox_events_before(timestamptz,integer)','EXECUTE') AND NOT has_table_privilege('pharmacycrm_runtime','outbox_events','DELETE');
-- Lock/rewrite assessment: creates functions and an index; index creation scans outbox_events and should be scheduled for a maintenance window on large installations.
-- Compatibility: existing outbox writes and claims are unchanged; runtime receives only terminal-state bounded cleanup functions.
-- Forward-fix policy: published migrations remain immutable; further corrections require another forward migration.

CREATE INDEX idx_outbox_processed_retention
ON outbox_events (processed_at, id)
WHERE status = 'PROCESSED';

CREATE INDEX idx_outbox_dead_letter_retention
ON outbox_events (dead_lettered_at, id)
WHERE status = 'DEAD_LETTER';

CREATE FUNCTION delete_processed_outbox_events_before(p_before timestamptz, p_limit integer)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
BEGIN
    IF p_before IS NULL OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid outbox retention request' USING ERRCODE = '22023';
    END IF;

    WITH candidates AS (
        SELECT id
        FROM public.outbox_events
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

CREATE FUNCTION delete_dead_letter_outbox_events_before(p_before timestamptz, p_limit integer)
RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
BEGIN
    IF p_before IS NULL OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid outbox retention request' USING ERRCODE = '22023';
    END IF;

    WITH candidates AS (
        SELECT id
        FROM public.outbox_events
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

REVOKE ALL ON FUNCTION delete_processed_outbox_events_before(timestamptz, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION delete_dead_letter_outbox_events_before(timestamptz, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION delete_processed_outbox_events_before(timestamptz, integer) TO pharmacycrm_runtime;
GRANT EXECUTE ON FUNCTION delete_dead_letter_outbox_events_before(timestamptz, integer) TO pharmacycrm_runtime;
