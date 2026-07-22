-- E2-DB-001: returns.
-- Verification query: SELECT to_regclass('public.sale_returns') IS NOT NULL AND to_regclass('public.sale_return_item_allocations') IS NOT NULL;
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Returns
-- =====================================================================

CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    operation_id uuid UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    returned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    returned_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_sale_returns_sale
ON sale_returns (sale_id, returned_at, id);

CREATE TABLE sale_return_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_id uuid NOT NULL
        REFERENCES sale_returns(id) ON DELETE RESTRICT,
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL
        CHECK (returned_quantity_base_units > 0),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    return_action varchar(50) NOT NULL
        CHECK (return_action IN (
            'RESTOCK', 'WRITE_OFF', 'QUARANTINE', 'NO_PHYSICAL_RETURN'
        )),
    item_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item UNIQUE (sale_return_id, sale_item_id),
    CONSTRAINT chk_sale_return_item_reason CHECK (
        item_reason IS NULL OR btrim(item_reason) <> ''
    )
);

CREATE INDEX idx_sale_return_items_sale_item
ON sale_return_items (sale_item_id);

CREATE TABLE sale_return_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_item_id uuid NOT NULL
        REFERENCES sale_return_items(id) ON DELETE RESTRICT,
    sale_item_allocation_id uuid NOT NULL
        REFERENCES sale_item_allocations(id) ON DELETE RESTRICT,
    target_stock_lot_id uuid
        REFERENCES stock_lots(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    returned_quantity_base_units bigint NOT NULL
        CHECK (returned_quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item_allocation
        UNIQUE (sale_return_item_id, sale_item_allocation_id)
);

CREATE INDEX idx_return_allocations_source
ON sale_return_item_allocations (
    sale_item_allocation_id,
    sale_return_item_id
);

ALTER TABLE stock_lots
ADD CONSTRAINT fk_stock_lot_source_return_allocation
FOREIGN KEY (source_return_allocation_id)
REFERENCES sale_return_item_allocations(id)
ON DELETE RESTRICT
DEFERRABLE INITIALLY DEFERRED;

-- =====================================================================
