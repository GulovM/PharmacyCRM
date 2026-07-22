-- E2-DB-001: sales.
-- Verification query: SELECT to_regclass('public.sales') IS NOT NULL AND to_regclass('public.sale_item_allocations') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='uq_sale_number_per_pharmacy' AND contype='u') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='uq_sale_item_allocation' AND contype='u') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_sale_item_quantity' AND convalidated);
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Sales
-- =====================================================================

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    sale_number varchar(100) NOT NULL CHECK (btrim(sale_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN (
            'COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REVERSED'
        )),
    payment_method varchar(50) NOT NULL
        CHECK (payment_method IN ('CASH', 'CARD', 'MOBILE', 'MIXED')),
    subtotal_amount_dirams bigint NOT NULL
        CHECK (subtotal_amount_dirams >= 0),
    discount_amount_dirams bigint NOT NULL DEFAULT 0
        CHECK (discount_amount_dirams >= 0),
    total_amount_dirams bigint NOT NULL CHECK (total_amount_dirams >= 0),
    prescription_confirmed boolean NOT NULL DEFAULT false,
    sold_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    sold_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_number_per_pharmacy UNIQUE (pharmacy_id, sale_number),
    CONSTRAINT chk_sale_total CHECK (
        total_amount_dirams = subtotal_amount_dirams - discount_amount_dirams
        AND discount_amount_dirams <= subtotal_amount_dirams
    )
);

CREATE TABLE sale_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    product_title_snapshot varchar(255) NOT NULL,
    presentation_snapshot varchar(255) NOT NULL,
    sale_unit varchar(30) NOT NULL
        CHECK (sale_unit IN ('PACKAGE', 'INNER_UNIT')),
    display_quantity bigint NOT NULL CHECK (display_quantity > 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    unit_price_dirams bigint NOT NULL CHECK (unit_price_dirams >= 0),
    line_subtotal_dirams bigint NOT NULL CHECK (line_subtotal_dirams >= 0),
    line_discount_dirams bigint NOT NULL DEFAULT 0
        CHECK (line_discount_dirams >= 0),
    line_total_dirams bigint NOT NULL CHECK (line_total_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_product_unit
        UNIQUE (sale_id, pharmacy_product_id, sale_unit),
    CONSTRAINT chk_sale_item_total CHECK (
        line_subtotal_dirams = display_quantity * unit_price_dirams
        AND line_total_dirams = line_subtotal_dirams - line_discount_dirams
        AND line_discount_dirams <= line_subtotal_dirams
    ),
    CONSTRAINT chk_sale_item_quantity CHECK (
        (sale_unit = 'PACKAGE'
            AND quantity_base_units = display_quantity * base_units_per_package_snapshot)
        OR (sale_unit = 'INNER_UNIT'
            AND quantity_base_units = display_quantity)
    )
);

CREATE TABLE sale_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_allocation UNIQUE (sale_item_id, stock_lot_id)
);

CREATE INDEX idx_sale_item_allocations_lot
ON sale_item_allocations (stock_lot_id, sale_item_id);

-- =====================================================================
