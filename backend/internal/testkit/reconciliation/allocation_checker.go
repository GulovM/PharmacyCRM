package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) allocationViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH allocation_effects AS (
			SELECT sale.operation_id, allocation.stock_lot_id,
			       SUM(allocation.quantity_base_units)::bigint AS allocated,
			       (ARRAY_AGG(allocation.id ORDER BY allocation.id))[1] AS representative_id,
			       BOOL_OR(product.pharmacy_id <> sale.pharmacy_id OR lot.pharmacy_product_id <> item.pharmacy_product_id) AS orphaned
			FROM sales sale
			JOIN sale_items item ON item.sale_id = sale.id
			JOIN sale_item_allocations allocation ON allocation.sale_item_id = item.id
			JOIN pharmacy_products product ON product.id = item.pharmacy_product_id
			JOIN stock_lots lot ON lot.id = allocation.stock_lot_id
			WHERE sale.operation_id = ANY($1::uuid[])
			GROUP BY sale.operation_id, allocation.stock_lot_id
		)
		SELECT CASE WHEN effect.orphaned THEN 'ORPHAN_ALLOCATION'
		            WHEN movement.delta_base_units IS NOT NULL AND -movement.delta_base_units > effect.allocated THEN 'DUPLICATE_EFFECT'
		            ELSE 'ORPHAN_ALLOCATION' END,
		       'sale_item_allocation', effect.representative_id, effect.allocated, COALESCE(-movement.delta_base_units, 0)
		FROM allocation_effects effect
		LEFT JOIN inventory_movements movement ON movement.operation_id = effect.operation_id AND movement.stock_lot_id = effect.stock_lot_id
		WHERE effect.orphaned OR movement.id IS NULL OR -movement.delta_base_units <> effect.allocated`, operationIDs)
}
