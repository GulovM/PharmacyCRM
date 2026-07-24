package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) balanceViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	// All immutable movements participate in the sum. A REVERSED source is not
	// filtered out: its separate POSTED REVERSAL operation contributes the
	// compensating delta, preserving the initial-zero ledger history.
	return c.queryViolations(ctx, `
		WITH target_lots AS (
			SELECT DISTINCT stock_lot_id FROM inventory_movements WHERE operation_id = ANY($1::uuid[])
		), movement_totals AS (
			SELECT movement.stock_lot_id, SUM(movement.delta_base_units)::bigint AS quantity
			FROM inventory_movements AS movement
			JOIN target_lots target ON target.stock_lot_id = movement.stock_lot_id
			GROUP BY movement.stock_lot_id
		)
		SELECT 'BALANCE_MISMATCH', 'stock_lot', lot.id,
		       COALESCE(total.quantity, 0), lot.quantity_base_units
		FROM target_lots target
		JOIN stock_lots lot ON lot.id = target.stock_lot_id
		LEFT JOIN movement_totals total ON total.stock_lot_id = lot.id
		WHERE COALESCE(total.quantity, 0) <> lot.quantity_base_units`, operationIDs)
}
