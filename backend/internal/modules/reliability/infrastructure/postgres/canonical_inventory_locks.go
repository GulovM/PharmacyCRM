package postgres

import (
	"context"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
)

func (r *CanonicalLockRepository) LockInventory(ctx context.Context, plan locking.InventoryPlan) (locking.InventoryLocks, error) {
	productIDs, lotIDs, err := validateInventoryPlan(plan.PharmacyID, plan.PharmacyProductIDs, plan.StockLotIDs, true)
	if err != nil {
		return locking.InventoryLocks{}, err
	}
	pharmacy, err := r.lockPharmacy(ctx, plan.PharmacyID)
	if err != nil {
		return locking.InventoryLocks{}, err
	}
	products, err := r.lockProducts(ctx, plan.PharmacyID, productIDs)
	if err != nil {
		return locking.InventoryLocks{}, err
	}
	lots, err := r.lockLots(ctx, productIDs, lotIDs)
	if err != nil {
		return locking.InventoryLocks{}, err
	}
	return locking.InventoryLocks{Pharmacy: pharmacy, PharmacyProducts: products, StockLots: lots}, nil
}
