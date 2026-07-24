-- E2-DB-001: pharmacies and assignments.
-- Verification query: SELECT to_regclass('public.pharmacies') IS NOT NULL AND to_regclass('public.pharmacy_assignments') IS NOT NULL AND to_regclass('public.uq_pharmacy_assignment_active_user') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_assignment_end' AND convalidated);
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Pharmacy and assignments
-- =====================================================================

CREATE TABLE pharmacies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(255) NOT NULL CHECK (btrim(name) <> ''),
    address text NOT NULL CHECK (btrim(address) <> ''),
    landmark text,
    latitude numeric(9,6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude numeric(9,6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    phone varchar(50),
    working_hours varchar(255),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    blocked_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT chk_pharmacy_status_timestamps CHECK (
        (status = 'ACTIVE' AND archived_at IS NULL)
        OR (status = 'BLOCKED' AND blocked_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'ARCHIVED' AND archived_at IS NOT NULL)
    )
);

CREATE TABLE pharmacy_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    ended_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    ended_at timestamptz,
    end_reason text,
    CONSTRAINT chk_assignment_end CHECK (
        (ended_at IS NULL AND ended_by_user_id IS NULL AND end_reason IS NULL)
        OR (
            ended_at IS NOT NULL
            AND ended_at >= assigned_at
            AND ended_by_user_id IS NOT NULL
            AND btrim(end_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_pharmacy_assignment_active_user
ON pharmacy_assignments (user_id)
WHERE ended_at IS NULL;

CREATE INDEX idx_pharmacy_assignments_pharmacy_active
ON pharmacy_assignments (pharmacy_id, user_id)
WHERE ended_at IS NULL;

CREATE INDEX idx_pharmacy_assignments_history
ON pharmacy_assignments (user_id, assigned_at DESC, id DESC);

-- =====================================================================
