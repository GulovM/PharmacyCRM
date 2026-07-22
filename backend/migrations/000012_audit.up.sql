-- E2-DB-001: audit.
-- Verification query: SELECT to_regclass('public.audit_events') IS NOT NULL AND to_regclass('public.idx_audit_events_time') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='public.audit_events'::regclass AND conname='chk_audit_actor' AND convalidated) AND NOT has_table_privilege('pharmacycrm_runtime','audit_events','UPDATE,DELETE,TRUNCATE');
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Audit
-- =====================================================================

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    actor_session_id uuid REFERENCES user_sessions(id) ON DELETE RESTRICT,
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    actor_type varchar(30) NOT NULL CHECK (actor_type IN ('USER', 'SYSTEM')),
    action varchar(150) NOT NULL CHECK (btrim(action) <> ''),
    object_type varchar(100) NOT NULL CHECK (btrim(object_type) <> ''),
    object_id uuid,
    result varchar(30) NOT NULL CHECK (result IN ('SUCCESS', 'DENIED', 'FAILURE')),
    request_id varchar(128),
    trace_id varchar(128),
    ip_address inet,
    user_agent text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_audit_actor CHECK (
        (actor_type = 'USER' AND actor_user_id IS NOT NULL)
        OR (actor_type = 'SYSTEM'
            AND actor_user_id IS NULL
            AND actor_session_id IS NULL)
    ),
    CONSTRAINT chk_audit_session_actor CHECK (
        actor_session_id IS NULL OR actor_user_id IS NOT NULL
    )
);

CREATE INDEX idx_audit_events_time
ON audit_events (occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_actor
ON audit_events (actor_user_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_pharmacy
ON audit_events (pharmacy_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_object
ON audit_events (object_type, object_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_request
ON audit_events (request_id)
WHERE request_id IS NOT NULL;

-- =====================================================================
