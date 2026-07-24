package reconciliation

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

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
