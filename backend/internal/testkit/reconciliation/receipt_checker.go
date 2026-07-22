package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) receiptEffectViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		SELECT CASE
		         WHEN movement.delta_base_units > item.quantity_base_units THEN 'DUPLICATE_EFFECT'
		         ELSE 'MISSING_DOCUMENT_EFFECT'
		       END,
		       'receipt_item', item.id,
		       item.quantity_base_units, COALESCE(movement.delta_base_units, 0)
		FROM receipts receipt
		JOIN receipt_items item ON item.receipt_id = receipt.id
		LEFT JOIN stock_lots lot ON lot.receipt_item_id = item.id
		LEFT JOIN inventory_movements movement
		  ON movement.operation_id = receipt.operation_id AND movement.stock_lot_id = lot.id
		WHERE receipt.operation_id = ANY($1::uuid[])
		  AND (lot.id IS NULL OR movement.id IS NULL OR movement.delta_base_units <> item.quantity_base_units)`, operationIDs)
}
