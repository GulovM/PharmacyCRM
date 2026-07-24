// Package reconciliation provides read-only correctness oracles for PostgreSQL
// integration tests. It reports divergence and never mutates business data.
package reconciliation

import (
	"context"
	"sort"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/google/uuid"
)

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
