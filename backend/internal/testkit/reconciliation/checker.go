// Package reconciliation provides read-only correctness oracles for PostgreSQL
// integration tests. It reports divergence and never mutates business data.
package reconciliation

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
)

type Kind string

const (
	BalanceMismatch       Kind = "BALANCE_MISMATCH"
	OrphanAllocation      Kind = "ORPHAN_ALLOCATION"
	DuplicateEffect       Kind = "DUPLICATE_EFFECT"
	MissingDocumentEffect Kind = "MISSING_DOCUMENT_EFFECT"
	MissingAudit          Kind = "MISSING_AUDIT"
	MissingOutbox         Kind = "MISSING_OUTBOX"
	InvalidMovementState  Kind = "INVALID_MOVEMENT_STATE"
	InvalidOperationState Kind = "INVALID_OPERATION_STATE"
	InvalidLotState       Kind = "INVALID_LOT_STATE"
)

var ErrInvalidScope = errors.New("invalid reconciliation scope")

type Scope struct {
	OperationIDs           []uuid.UUID
	RequiredAuditEventIDs  []uuid.UUID
	RequiredOutboxEventIDs []uuid.UUID
}

type Violation struct {
	Kind       Kind
	EntityType string
	EntityID   uuid.UUID
	Expected   int64
	Actual     int64
}

type Report struct{ Violations []Violation }

func (r Report) Clean() bool { return len(r.Violations) == 0 }

type Checker struct{ database database.DBTX }

func NewChecker(executor database.DBTX) *Checker { return &Checker{database: executor} }

func (c *Checker) Check(ctx context.Context, scope Scope) (Report, error) {
	operationIDs, err := normalizeRequiredIDs(scope.OperationIDs)
	if err != nil || c == nil || c.database == nil {
		return Report{}, ErrInvalidScope
	}
	report := Report{}
	if err := c.requiredRows(ctx, operationIDs, "inventory_operations", MissingDocumentEffect, "inventory_operation", &report); err != nil {
		return Report{}, err
	}
	checks := []func(context.Context, []uuid.UUID) ([]Violation, error){
		c.balanceViolations,
		c.receiptEffectViolations,
		c.allocationViolations,
		c.documentEffectViolations,
		c.movementStateViolations,
		c.operationStateViolations,
		c.lotStateViolations,
	}
	for _, check := range checks {
		violations, err := check(ctx, operationIDs)
		if err != nil {
			return Report{}, err
		}
		report.Violations = append(report.Violations, violations...)
	}
	if err := c.requiredRows(ctx, scope.RequiredAuditEventIDs, "audit_events", MissingAudit, "audit_event", &report); err != nil {
		return Report{}, err
	}
	if err := c.requiredRows(ctx, scope.RequiredOutboxEventIDs, "outbox_events", MissingOutbox, "outbox_event", &report); err != nil {
		return Report{}, err
	}
	sort.Slice(report.Violations, func(i, j int) bool {
		left, right := report.Violations[i], report.Violations[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.EntityType != right.EntityType {
			return left.EntityType < right.EntityType
		}
		return left.EntityID.String() < right.EntityID.String()
	})
	return report, nil
}

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

func (c *Checker) allocationViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH allocation_effects AS (
			SELECT sale.operation_id, allocation.stock_lot_id,
			       SUM(allocation.quantity_base_units)::bigint AS allocated,
			       (ARRAY_AGG(allocation.id ORDER BY allocation.id))[1] AS representative_id,
			       BOOL_OR(product.pharmacy_id <> sale.pharmacy_id
			           OR lot.pharmacy_product_id <> item.pharmacy_product_id) AS orphaned
			FROM sales sale
			JOIN sale_items item ON item.sale_id = sale.id
			JOIN sale_item_allocations allocation ON allocation.sale_item_id = item.id
			JOIN pharmacy_products product ON product.id = item.pharmacy_product_id
			JOIN stock_lots lot ON lot.id = allocation.stock_lot_id
			WHERE sale.operation_id = ANY($1::uuid[])
			GROUP BY sale.operation_id, allocation.stock_lot_id
		)
		SELECT CASE
		         WHEN effect.orphaned THEN 'ORPHAN_ALLOCATION'
		         WHEN movement.delta_base_units IS NOT NULL AND -movement.delta_base_units > effect.allocated THEN 'DUPLICATE_EFFECT'
		         ELSE 'ORPHAN_ALLOCATION'
		       END,
		       'sale_item_allocation', effect.representative_id,
		       effect.allocated, COALESCE(-movement.delta_base_units, 0)
		FROM allocation_effects effect
		LEFT JOIN inventory_movements movement
		  ON movement.operation_id = effect.operation_id AND movement.stock_lot_id = effect.stock_lot_id
		WHERE effect.orphaned OR movement.id IS NULL OR -movement.delta_base_units <> effect.allocated`, operationIDs)
}

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

func (c *Checker) movementStateViolations(ctx context.Context, operationIDs []uuid.UUID) ([]Violation, error) {
	return c.queryViolations(ctx, `
		WITH target_lots AS (
			SELECT DISTINCT stock_lot_id FROM inventory_movements WHERE operation_id = ANY($1::uuid[])
		), ordered AS (
			SELECT movement.id, movement.stock_lot_id, movement.quantity_after_base_units,
			       SUM(movement.delta_base_units) OVER (
			         PARTITION BY movement.stock_lot_id ORDER BY movement.created_at, movement.id
			         ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
			       )::bigint AS calculated
			FROM inventory_movements movement
			JOIN target_lots target ON target.stock_lot_id = movement.stock_lot_id
		)
		SELECT 'INVALID_MOVEMENT_STATE', 'inventory_movement', id,
		       calculated, quantity_after_base_units
		FROM ordered WHERE calculated <> quantity_after_base_units`, operationIDs)
}

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

func (c *Checker) queryViolations(ctx context.Context, query string, operationIDs []uuid.UUID) ([]Violation, error) {
	rows, err := c.database.Query(ctx, query, operationIDs)
	if err != nil {
		return nil, fmt.Errorf("run reconciliation oracle: %w", err)
	}
	defer rows.Close()
	var violations []Violation
	for rows.Next() {
		var violation Violation
		if err := rows.Scan(&violation.Kind, &violation.EntityType, &violation.EntityID, &violation.Expected, &violation.Actual); err != nil {
			return nil, fmt.Errorf("scan reconciliation violation: %w", err)
		}
		violations = append(violations, violation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconciliation violations: %w", err)
	}
	return violations, nil
}

func (c *Checker) requiredRows(ctx context.Context, ids []uuid.UUID, table string, kind Kind, entityType string, report *Report) error {
	ids, err := normalizeOptionalIDs(ids)
	if err != nil {
		return ErrInvalidScope
	}
	for _, id := range ids {
		var found bool
		query := "SELECT EXISTS (SELECT 1 FROM " + table + " WHERE id = $1)" // table is an internal constant.
		if err := c.database.QueryRow(ctx, query, id).Scan(&found); err != nil {
			return fmt.Errorf("check required %s: %w", entityType, err)
		}
		if !found {
			report.Violations = append(report.Violations, Violation{Kind: kind, EntityType: entityType, EntityID: id, Expected: 1})
		}
	}
	return nil
}

func normalizeRequiredIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	if len(ids) == 0 {
		return nil, ErrInvalidScope
	}
	return normalizeOptionalIDs(ids)
}

func normalizeOptionalIDs(ids []uuid.UUID) ([]uuid.UUID, error) {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, ErrInvalidScope
		}
		unique[id] = struct{}{}
	}
	result := make([]uuid.UUID, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, nil
}
