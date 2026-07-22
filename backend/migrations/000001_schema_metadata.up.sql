-- E2-DB-001. Verification query: SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto');
-- Lock/rewrite: empty-schema DDL only. Compatibility: additive. Forward-fix only.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE pharmacycrm_schema_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO pharmacycrm_schema_metadata (singleton, schema_version)
VALUES (true, 1);
