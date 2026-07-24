-- E2-DB-001: required PostgreSQL extensions.
-- Verification query: SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pgcrypto');
-- Lock/rewrite assessment: extension catalog DDL only; no application-row rewrite.
-- Compatibility: additive and safe after the immutable E1 schema metadata migration.
-- Forward-fix policy: destructive down migrations are prohibited.

CREATE EXTENSION IF NOT EXISTS pgcrypto;
