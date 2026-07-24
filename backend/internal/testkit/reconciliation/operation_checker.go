package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) operationStateViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		SELECT 'INVALID_OPERATION_STATE', 'inventory_operation', source.id,
		       CASE WHEN source.status = 'REVERSED' THEN 1 ELSE 0 END,
		       COUNT(reversal.id)::bigint
		FROM inventory_operations source
		LEFT JOIN inventory_operations reversal ON reversal.reversal_of_operation_id = source.id
		WHERE source.id = ANY($1::uuid[])
		GROUP BY source.id, source.status, source.operation_type
		HAVING (source.operation_type = 'REVERSAL' AND (source.status <> 'POSTED' OR COUNT(reversal.id) <> 0))
		    OR (source.operation_type <> 'REVERSAL' AND source.status = 'REVERSED' AND COUNT(reversal.id) <> 1)
		    OR (source.operation_type <> 'REVERSAL' AND source.status = 'POSTED' AND COUNT(reversal.id) <> 0)
		UNION ALL
		SELECT 'INVALID_OPERATION_STATE', 'inventory_movement', movement.id, 1, 0
		FROM inventory_movements movement
		JOIN inventory_operations operation ON operation.id = movement.operation_id
		JOIN stock_lots lot ON lot.id = movement.stock_lot_id
		JOIN pharmacy_products product ON product.id = lot.pharmacy_product_id
		WHERE operation.id = ANY($1::uuid[])
		  AND (operation.pharmacy_id <> product.pharmacy_id
		       OR (operation.operation_type IN ('RECEIPT','INITIAL_STOCK','RETURN_TO_STOCK') AND movement.delta_base_units < 0)
		       OR (operation.operation_type IN ('SALE','WRITE_OFF') AND movement.delta_base_units > 0))`, operationIDs)
}
