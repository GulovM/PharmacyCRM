package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) movementStateViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH target_lots AS (
			SELECT DISTINCT stock_lot_id FROM inventory_movements WHERE operation_id = ANY($1::uuid[])
		), ordered AS (
			SELECT movement.id, movement.stock_lot_id, movement.lot_sequence,
			       movement.quantity_after_base_units,
			       ROW_NUMBER() OVER (
			         PARTITION BY movement.stock_lot_id ORDER BY movement.lot_sequence
			       )::bigint AS expected_sequence,
			       SUM(movement.delta_base_units) OVER (
			         PARTITION BY movement.stock_lot_id ORDER BY movement.lot_sequence
			         ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
			       )::bigint AS calculated
			FROM inventory_movements movement
			JOIN target_lots target ON target.stock_lot_id = movement.stock_lot_id
		)
		SELECT 'INVALID_MOVEMENT_STATE', 'inventory_movement', id,
		       calculated, quantity_after_base_units
		FROM ordered WHERE calculated <> quantity_after_base_units
		UNION ALL
		SELECT 'INVALID_MOVEMENT_STATE', 'inventory_movement', id,
		       expected_sequence, lot_sequence
		FROM ordered WHERE expected_sequence <> lot_sequence`, operationIDs)
}
