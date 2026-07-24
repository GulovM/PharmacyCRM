package reconciliation

import (
	"context"

	"github.com/google/uuid"
)

func (c *Checker) documentEffectViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH owners AS (
			SELECT operation_id, id, 'RECEIPT' AS owner_type FROM receipts
			UNION ALL SELECT operation_id, id, 'SALE' FROM sales
			UNION ALL SELECT operation_id, id, 'RETURN' FROM sale_returns WHERE operation_id IS NOT NULL
			UNION ALL SELECT operation_id, id, 'WRITE_OFF' FROM write_offs
			UNION ALL SELECT operation_id, id, 'ADJUSTMENT' FROM inventory_adjustments
		), counts AS (
			SELECT operation.id, operation.operation_type, COUNT(owner.id)::bigint AS owner_count,
			       COUNT(owner.id) FILTER (WHERE
			         (operation.operation_type IN ('RECEIPT','INITIAL_STOCK') AND owner.owner_type = 'RECEIPT') OR
			         (operation.operation_type = 'SALE' AND owner.owner_type = 'SALE') OR
			         (operation.operation_type IN ('RETURN_TO_STOCK','RETURN_WRITE_OFF','RETURN_QUARANTINE') AND owner.owner_type = 'RETURN') OR
			         (operation.operation_type = 'WRITE_OFF' AND owner.owner_type = 'WRITE_OFF') OR
			         (operation.operation_type = 'INVENTORY_ADJUSTMENT' AND owner.owner_type = 'ADJUSTMENT')
			       )::bigint AS compatible_count
			FROM inventory_operations operation
			LEFT JOIN owners owner ON owner.operation_id = operation.id
			WHERE operation.id = ANY($1::uuid[])
			GROUP BY operation.id, operation.operation_type
		)
		SELECT CASE
		         WHEN owner_count > 1 THEN 'DUPLICATE_EFFECT'
		         WHEN owner_count = 1 AND compatible_count = 0 THEN 'INVALID_OPERATION_STATE'
		         ELSE 'MISSING_DOCUMENT_EFFECT'
		       END,
		       'inventory_operation', id,
		       CASE WHEN operation_type = 'REVERSAL' THEN 0 ELSE 1 END,
		       CASE WHEN owner_count = 1 AND compatible_count = 0 THEN compatible_count ELSE owner_count END
		FROM counts
		WHERE (operation_type = 'REVERSAL' AND owner_count <> 0)
		   OR (operation_type <> 'REVERSAL' AND (owner_count <> 1 OR compatible_count <> 1))`, operationIDs)
}
