package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) lotStateViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH target_lots AS (
			SELECT DISTINCT stock_lot_id FROM inventory_movements WHERE operation_id = ANY($1::uuid[])
		)
		SELECT 'INVALID_LOT_STATE', 'stock_lot', lot.id, 0, 1
		FROM stock_lots lot
		JOIN target_lots target ON target.stock_lot_id = lot.id
		WHERE (lot.quantity_base_units = 0 AND lot.status = 'ACTIVE')
		   OR (lot.quantity_base_units > 0 AND lot.status = 'DEPLETED')
		   OR (lot.status = 'ACTIVE' AND lot.expiration_date < CURRENT_DATE)`, operationIDs)
}
