package reconciliation

import (
	"context"
	"fmt"
	"sort"

	"github.com/google/uuid"
)

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
