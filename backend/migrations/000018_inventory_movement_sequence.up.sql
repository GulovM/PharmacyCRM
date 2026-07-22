-- E2-FIX-010: assign a strict per-lot sequence to immutable inventory movements.
-- Verification query: SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='inventory_movements' AND column_name='lot_sequence' AND is_nullable='NO') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='uq_inventory_movement_lot_sequence' AND convalidated) AND EXISTS (SELECT 1 FROM pg_trigger WHERE tgname='trg_assign_inventory_movement_lot_sequence' AND NOT tgisinternal);
-- Lock/rewrite assessment: existing movement rows are backfilled once; NOT NULL and unique validation scan the ledger and require a maintenance window for large ledgers.
-- Compatibility: callers may omit lot_sequence; the database assigns it while holding the stock_lot row lock.
-- Forward-fix policy: published migrations remain immutable; further corrections require another forward migration.

ALTER TABLE inventory_movements
ADD COLUMN lot_sequence bigint;

WITH sequenced AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY stock_lot_id
               ORDER BY created_at, id
           )::bigint AS lot_sequence
    FROM inventory_movements
)
UPDATE inventory_movements AS movement
SET lot_sequence = sequenced.lot_sequence
FROM sequenced
WHERE movement.id = sequenced.id;

ALTER TABLE inventory_movements
ALTER COLUMN lot_sequence SET NOT NULL;

ALTER TABLE inventory_movements
ADD CONSTRAINT chk_inventory_movement_lot_sequence
CHECK (lot_sequence > 0);

ALTER TABLE inventory_movements
ADD CONSTRAINT uq_inventory_movement_lot_sequence
UNIQUE (stock_lot_id, lot_sequence);

CREATE OR REPLACE FUNCTION assign_inventory_movement_lot_sequence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    expected_sequence bigint;
BEGIN
    PERFORM 1
    FROM public.stock_lots
    WHERE id = NEW.stock_lot_id
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'inventory movement stock lot is missing'
            USING ERRCODE = '23503';
    END IF;

    SELECT COALESCE(MAX(lot_sequence), 0) + 1
    INTO expected_sequence
    FROM public.inventory_movements
    WHERE stock_lot_id = NEW.stock_lot_id;

    IF NEW.lot_sequence IS NULL THEN
        NEW.lot_sequence := expected_sequence;
    ELSIF NEW.lot_sequence <> expected_sequence THEN
        RAISE EXCEPTION 'inventory movement lot sequence is not next'
            USING ERRCODE = '23514';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_assign_inventory_movement_lot_sequence
BEFORE INSERT ON inventory_movements
FOR EACH ROW
EXECUTE FUNCTION assign_inventory_movement_lot_sequence();

DROP INDEX idx_inventory_movements_lot_history;
