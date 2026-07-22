-- E2-DB-001: assortment.
-- Verification query: SELECT to_regclass('public.pharmacy_products') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='uq_pharmacy_product' AND contype='u') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_stock_targets' AND convalidated);
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Assortment and inventory
-- =====================================================================

CREATE TABLE pharmacy_products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    is_inner_unit_sale_allowed boolean NOT NULL DEFAULT false,
    default_package_price_dirams bigint NOT NULL
        CHECK (default_package_price_dirams >= 0),
    default_inner_unit_price_dirams bigint
        CHECK (default_inner_unit_price_dirams >= 0),
    min_stock_level_base_units bigint NOT NULL DEFAULT 0
        CHECK (min_stock_level_base_units >= 0),
    target_stock_level_base_units bigint NOT NULL DEFAULT 0
        CHECK (target_stock_level_base_units >= 0),
    inventory_changed_at timestamptz,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_pharmacy_product UNIQUE (pharmacy_id, product_presentation_id),
    CONSTRAINT chk_inner_unit_price CHECK (
        (is_inner_unit_sale_allowed AND default_inner_unit_price_dirams IS NOT NULL)
        OR (NOT is_inner_unit_sale_allowed AND default_inner_unit_price_dirams IS NULL)
    ),
    CONSTRAINT chk_stock_targets CHECK (
        target_stock_level_base_units >= min_stock_level_base_units
    )
);
