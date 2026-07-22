-- E2-DB-001: alerts and public projection.
-- Verification query: SELECT to_regclass('public.alerts') IS NOT NULL AND to_regclass('public.public_availability_projection') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Alerts and public projection
-- =====================================================================

CREATE TABLE alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    alert_type varchar(50) NOT NULL
        CHECK (alert_type IN (
            'LOW_STOCK', 'EXPIRED', 'EXPIRING_7_DAYS',
            'EXPIRING_30_DAYS', 'RECONCILIATION_MISMATCH'
        )),
    deduplication_key varchar(255) NOT NULL
        CHECK (btrim(deduplication_key) <> ''),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'ACKNOWLEDGED', 'RESOLVED')),
    detected_at timestamptz NOT NULL,
    last_confirmed_at timestamptz NOT NULL,
    acknowledged_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    acknowledged_at timestamptz,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolved_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_alert_times CHECK (last_confirmed_at >= detected_at),
    CONSTRAINT chk_alert_acknowledgement CHECK (
        (acknowledged_at IS NULL AND acknowledged_by_user_id IS NULL)
        OR (acknowledged_at IS NOT NULL AND acknowledged_by_user_id IS NOT NULL)
    ),
    CONSTRAINT chk_alert_resolution CHECK (
        (status = 'RESOLVED' AND resolved_at IS NOT NULL)
        OR (status <> 'RESOLVED'
            AND resolved_at IS NULL
            AND resolved_by_user_id IS NULL)
    )
);

CREATE UNIQUE INDEX uq_alert_open_dedup
ON alerts (pharmacy_id, deduplication_key)
WHERE status IN ('ACTIVE', 'ACKNOWLEDGED');

CREATE INDEX idx_alerts_pharmacy_status
ON alerts (pharmacy_id, status, detected_at DESC, id DESC);

CREATE TABLE public_availability_projection (
    pharmacy_product_id uuid PRIMARY KEY
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    package_price_dirams bigint NOT NULL CHECK (package_price_dirams >= 0),
    inner_unit_price_dirams bigint CHECK (inner_unit_price_dirams >= 0),
    availability_status varchar(30) NOT NULL
        CHECK (availability_status IN ('AVAILABLE', 'LOW_STOCK', 'UNAVAILABLE')),
    source_inventory_changed_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL,
    projection_version bigint NOT NULL CHECK (projection_version > 0)
);

CREATE INDEX idx_public_availability_search
ON public_availability_projection (
    product_presentation_id,
    availability_status,
    package_price_dirams,
    pharmacy_id
);
