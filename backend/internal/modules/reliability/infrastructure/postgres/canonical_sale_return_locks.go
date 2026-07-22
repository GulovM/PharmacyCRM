package postgres

import (
    "context"

    "github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
    "github.com/google/uuid"
)

func (r *CanonicalLockRepository) LockSaleReturn(ctx context.Context, plan locking.SaleReturnPlan) (locking.SaleReturnLocks, error) {
	if plan.SaleID == uuid.Nil {
		return locking.SaleReturnLocks{}, invalidPlan()
	}
	productIDs, lotIDs, err := validateInventoryPlan(plan.PharmacyID, plan.PharmacyProductIDs, plan.StockLotIDs, true)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	if len(lotIDs) == 0 {
		return locking.SaleReturnLocks{}, invalidPlan()
	}
	saleItemIDs, err := normalizedIDs(plan.SourceSaleItemIDs, true)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	allocationIDs, err := normalizedIDs(plan.SourceAllocationIDs, true)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	pharmacy, err := r.lockPharmacy(ctx, plan.PharmacyID)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	sale, err := r.lockSale(ctx, plan.PharmacyID, plan.SaleID)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	saleItems, err := r.lockSaleItems(ctx, plan.SaleID, saleItemIDs)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	allocations, err := r.lockAllocations(ctx, plan.SaleID, saleItemIDs, allocationIDs)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	if !sameIDs(saleItemIDs, allocationSaleItemIDs(allocations)) ||
		!sameIDs(productIDs, saleItemProductIDs(saleItems)) ||
		!sameIDs(lotIDs, allocationLotIDs(allocations)) {
		return locking.SaleReturnLocks{}, missingTarget()
	}
	products, err := r.lockProducts(ctx, plan.PharmacyID, productIDs)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	lots, err := r.lockLots(ctx, productIDs, lotIDs)
	if err != nil {
		return locking.SaleReturnLocks{}, err
	}
	return locking.SaleReturnLocks{
		Pharmacy: pharmacy, Sale: sale, PharmacyProducts: products,
		SourceSaleItems: saleItems, SourceAllocations: allocations, StockLots: lots,
	}, nil
}
