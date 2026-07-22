-- E2-DB-001: inventory.
-- Verification query: SELECT to_regclass('public.stock_lots') IS NOT NULL AND to_regclass('public.inventory_movements') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

CREATE TABLE inventory_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_type varchar(50) NOT NULL
        CHECK (operation_type IN (
            'RECEIPT', 'INITIAL_STOCK', 'SALE', 'RETURN_TO_STOCK',
            'RETURN_WRITE_OFF', 'RETURN_QUARANTINE', 'WRITE_OFF',
            'INVENTORY_ADJUSTMENT', 'REVERSAL'
        )),
    status varchar(30) NOT NULL DEFAULT 'POSTED'
        CHECK (status IN ('POSTED', 'REVERSED')),
    initiated_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    is_system_initiated boolean NOT NULL DEFAULT false,
    reversal_of_operation_id uuid
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_inventory_actor CHECK (
        (is_system_initiated AND initiated_by_user_id IS NULL)
        OR (NOT is_system_initiated AND initiated_by_user_id IS NOT NULL)
    ),
    CONSTRAINT chk_inventory_reversal CHECK (
        (operation_type = 'REVERSAL' AND reversal_of_operation_id IS NOT NULL)
        OR (operation_type <> 'REVERSAL' AND reversal_of_operation_id IS NULL)
    ),
    CONSTRAINT chk_inventory_reversal_self CHECK (
        reversal_of_operation_id IS NULL OR reversal_of_operation_id <> id
    )
);

CREATE UNIQUE INDEX uq_inventory_reversal
ON inventory_operations (reversal_of_operation_id)
WHERE reversal_of_operation_id IS NOT NULL;

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    source_import_job_id uuid REFERENCES import_jobs(id) ON DELETE RESTRICT,
    receipt_number varchar(100) NOT NULL CHECK (btrim(receipt_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'POSTED'
        CHECK (status IN ('POSTED', 'REVERSED')),
    supplier_name varchar(255),
    received_at timestamptz NOT NULL,
    posted_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    posted_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_receipt_number_per_pharmacy
        UNIQUE (pharmacy_id, receipt_number)
);

CREATE TABLE receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id uuid NOT NULL REFERENCES receipts(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_packages bigint NOT NULL CHECK (quantity_packages > 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    purchase_price_package_dirams bigint NOT NULL
        CHECK (purchase_price_package_dirams >= 0),
    retail_price_package_dirams bigint NOT NULL
        CHECK (retail_price_package_dirams >= 0),
    retail_price_inner_unit_dirams bigint
        CHECK (retail_price_inner_unit_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_receipt_quantity CHECK (
        quantity_base_units = quantity_packages * base_units_per_package_snapshot
    )
);

CREATE TABLE stock_lots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    receipt_item_id uuid REFERENCES receipt_items(id) ON DELETE RESTRICT,
    source_return_allocation_id uuid,
    origin_type varchar(30) NOT NULL
        CHECK (origin_type IN ('RECEIPT', 'INITIAL_STOCK', 'RETURN')),
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_base_units bigint NOT NULL DEFAULT 0
        CHECK (quantity_base_units >= 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    purchase_price_package_dirams bigint
        CHECK (purchase_price_package_dirams >= 0),
    package_retail_price_dirams bigint NOT NULL
        CHECK (package_retail_price_dirams >= 0),
    inner_unit_retail_price_dirams bigint
        CHECK (inner_unit_retail_price_dirams >= 0),
    received_at timestamptz NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_lot_origin_reference CHECK (
        (origin_type IN ('RECEIPT', 'INITIAL_STOCK')
            AND receipt_item_id IS NOT NULL
            AND source_return_allocation_id IS NULL)
        OR (origin_type = 'RETURN'
            AND receipt_item_id IS NULL
            AND source_return_allocation_id IS NOT NULL)
    ),
    CONSTRAINT chk_lot_status_quantity CHECK (
        status <> 'DEPLETED' OR quantity_base_units = 0
    )
);

CREATE UNIQUE INDEX uq_stock_lot_receipt_item
ON stock_lots (receipt_item_id)
WHERE receipt_item_id IS NOT NULL;

CREATE UNIQUE INDEX uq_stock_lot_source_return
ON stock_lots (source_return_allocation_id)
WHERE source_return_allocation_id IS NOT NULL;

CREATE INDEX idx_stock_lots_fefo
ON stock_lots (
    pharmacy_product_id,
    expiration_date,
    received_at,
    id
)
WHERE status = 'ACTIVE' AND quantity_base_units > 0;

CREATE TABLE inventory_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id uuid NOT NULL
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    quantity_after_base_units bigint NOT NULL
        CHECK (quantity_after_base_units >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_movement_operation_lot
        UNIQUE (operation_id, stock_lot_id)
);

CREATE INDEX idx_inventory_movements_lot_history
ON inventory_movements (stock_lot_id, created_at, id);

-- =====================================================================

-- Write-offs and inventory adjustments
-- =====================================================================

CREATE TABLE write_offs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE write_off_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    write_off_id uuid NOT NULL REFERENCES write_offs(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_write_off_lot UNIQUE (write_off_id, stock_lot_id)
);

CREATE TABLE inventory_adjustments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    approved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_adjustment_distinct_approver CHECK (
        approved_by_user_id IS NULL OR approved_by_user_id <> created_by_user_id
    )
);

CREATE TABLE inventory_adjustment_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    inventory_adjustment_id uuid NOT NULL
        REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    expected_quantity_base_units bigint NOT NULL
        CHECK (expected_quantity_base_units >= 0),
    actual_quantity_base_units bigint NOT NULL
        CHECK (actual_quantity_base_units >= 0),
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_adjustment_lot
        UNIQUE (inventory_adjustment_id, stock_lot_id),
    CONSTRAINT chk_inventory_adjustment_delta CHECK (
        delta_base_units = actual_quantity_base_units - expected_quantity_base_units
    )
);

-- =====================================================================
