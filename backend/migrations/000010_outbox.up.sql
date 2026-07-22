-- E2-DB-001: outbox.
-- Verification query: SELECT to_regclass('public.outbox_events') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name varchar(150) NOT NULL CHECK (btrim(event_name) <> ''),
    event_version smallint NOT NULL DEFAULT 1 CHECK (event_version > 0),
    aggregate_type varchar(100) NOT NULL CHECK (btrim(aggregate_type) <> ''),
    aggregate_id uuid NOT NULL,
    partition_key varchar(200) NOT NULL CHECK (btrim(partition_key) <> ''),
    deduplication_key varchar(255) NOT NULL UNIQUE CHECK (btrim(deduplication_key) <> ''),
    payload jsonb NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(30) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'DEAD_LETTER')),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts smallint NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 20),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    leased_by varchar(150),
    lease_expires_at timestamptz,
    last_error_code varchar(100),
    last_error_at timestamptz,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    dead_lettered_at timestamptz,
    CONSTRAINT chk_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_outbox_payload_size CHECK (octet_length(payload::text) <= 262144),
    CONSTRAINT chk_outbox_lease CHECK ((status = 'PROCESSING' AND lease_token IS NOT NULL AND leased_by IS NOT NULL AND lease_expires_at IS NOT NULL) OR (status <> 'PROCESSING' AND lease_token IS NULL AND leased_by IS NULL AND lease_expires_at IS NULL)),
    CONSTRAINT chk_outbox_terminal CHECK ((status = 'PROCESSED' AND processed_at IS NOT NULL AND dead_lettered_at IS NULL) OR (status = 'DEAD_LETTER' AND dead_lettered_at IS NOT NULL AND processed_at IS NULL) OR (status IN ('PENDING', 'PROCESSING') AND processed_at IS NULL AND dead_lettered_at IS NULL))
);
CREATE INDEX idx_outbox_claim ON outbox_events (available_at, created_at, id) WHERE status = 'PENDING';
CREATE INDEX idx_outbox_processing_lease ON outbox_events (lease_expires_at, id) WHERE status = 'PROCESSING';
CREATE INDEX idx_outbox_partition ON outbox_events (partition_key, occurred_at, id);
CREATE INDEX idx_outbox_aggregate ON outbox_events (aggregate_type, aggregate_id, occurred_at, id);

-- =====================================================================
