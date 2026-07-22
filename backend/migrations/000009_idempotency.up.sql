-- E2-DB-001: idempotency.
-- Verification query: SELECT to_regclass('public.idempotency_records') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Reliability: idempotency and outbox
-- =====================================================================

CREATE TABLE idempotency_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation varchar(150) NOT NULL CHECK (btrim(operation) <> ''),
    idempotency_key varchar(128) NOT NULL CHECK (btrim(idempotency_key) <> ''),
    scope_key text GENERATED ALWAYS AS (
        actor_user_id::text
        || ':' || operation
        || ':' || coalesce(pharmacy_id::text, 'GLOBAL')
    ) STORED,
    request_hash bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'IN_PROGRESS'
        CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED_RETRYABLE')),
    response_status integer CHECK (response_status BETWEEN 100 AND 599),
    response_body jsonb,
    resource_type varchar(100),
    resource_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CONSTRAINT uq_idempotency_scope UNIQUE (scope_key, idempotency_key),
    CONSTRAINT chk_idempotency_result CHECK (
        (status = 'COMPLETED'
            AND response_status IS NOT NULL
            AND response_body IS NOT NULL
            AND completed_at IS NOT NULL)
        OR (status = 'IN_PROGRESS'
            AND response_status IS NULL
            AND response_body IS NULL
            AND completed_at IS NULL)
        OR (status = 'FAILED_RETRYABLE'
            AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_idempotency_expiry CHECK (expires_at > created_at)
);

CREATE INDEX idx_idempotency_expiration
ON idempotency_records (expires_at);

CREATE INDEX idx_idempotency_resource
ON idempotency_records (resource_type, resource_id)
WHERE resource_id IS NOT NULL;
