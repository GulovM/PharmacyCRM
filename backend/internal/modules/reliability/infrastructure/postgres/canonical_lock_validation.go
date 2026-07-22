package postgres

import (
	"bytes"
	"errors"
	"slices"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

func saleItemProductIDs(items []locking.SourceSaleItem) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.PharmacyProductID)
	}
	result, _ := normalizedIDs(ids, false)
	return result
}

func allocationLotIDs(allocations []locking.SourceAllocation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(allocations))
	for _, allocation := range allocations {
		ids = append(ids, allocation.StockLotID)
	}
	result, _ := normalizedIDs(ids, false)
	return result
}

func allocationSaleItemIDs(allocations []locking.SourceAllocation) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(allocations))
	for _, allocation := range allocations {
		ids = append(ids, allocation.SaleItemID)
	}
	result, _ := normalizedIDs(ids, false)
	return result
}

func sameIDs(left, right []uuid.UUID) bool { return slices.Equal(left, right) }

func validateInventoryPlan(pharmacyID uuid.UUID, productIDs, lotIDs []uuid.UUID, productsRequired bool) ([]uuid.UUID, []uuid.UUID, error) {
	if pharmacyID == uuid.Nil {
		return nil, nil, invalidPlan()
	}
	products, err := normalizedIDs(productIDs, productsRequired)
	if err != nil {
		return nil, nil, err
	}
	lots, err := normalizedIDs(lotIDs, false)
	if err != nil {
		return nil, nil, err
	}
	if len(lots) > 0 && len(products) == 0 {
		return nil, nil, invalidPlan()
	}
	return products, lots, nil
}

func normalizedIDs(ids []uuid.UUID, required bool) ([]uuid.UUID, error) {
	if required && len(ids) == 0 {
		return nil, invalidPlan()
	}
	unique := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, invalidPlan()
		}
		unique[id] = struct{}{}
	}
	result := make([]uuid.UUID, 0, len(unique))
	for id := range unique {
		result = append(result, id)
	}
	slices.SortFunc(result, func(left, right uuid.UUID) int { return bytes.Compare(left[:], right[:]) })
	return result, nil
}

func invalidPlan() error {
	return errors.Join(locking.ErrInvalidLockPlan, apperror.ErrInvalidArgument)
}

func missingTarget() error {
	return errors.Join(locking.ErrLockTargetMissing, apperror.ErrNotFound)
}
