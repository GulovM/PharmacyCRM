package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CanonicalLockRepository deliberately requires pgx.Tx. This prevents its
// FOR UPDATE methods from silently running in auto-commit mode.
type CanonicalLockRepository struct{ tx pgx.Tx }

func NewCanonicalLockRepository(tx pgx.Tx) (*CanonicalLockRepository, error) {
	if tx == nil {
		return nil, errors.Join(locking.ErrInvalidLockPlan, apperror.ErrInvalidArgument)
	}
	return &CanonicalLockRepository{tx: tx}, nil
}

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

func (r *CanonicalLockRepository) lockSaleItems(ctx context.Context, saleID uuid.UUID, ids []uuid.UUID) ([]locking.SourceSaleItem, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT id, sale_id, pharmacy_product_id
		FROM sale_items
		WHERE sale_id = $1 AND id = ANY($2::uuid[])
		ORDER BY id
		FOR UPDATE`, saleID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock sale items: %w", err)
	}
	defer rows.Close()
	values := make([]locking.SourceSaleItem, 0, len(ids))
	for rows.Next() {
		var value locking.SourceSaleItem
		if err := rows.Scan(&value.ID, &value.SaleID, &value.PharmacyProductID); err != nil {
			return nil, fmt.Errorf("scan locked sale item: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked sale items: %w", err)
	}
	if len(values) != len(ids) {
		return nil, missingTarget()
	}
	return values, nil
}

func (r *CanonicalLockRepository) lockPharmacy(ctx context.Context, id uuid.UUID) (locking.Pharmacy, error) {
	var value locking.Pharmacy
	err := r.tx.QueryRow(ctx, `SELECT id, status, version FROM pharmacies WHERE id = $1 FOR UPDATE`, id).
		Scan(&value.ID, &value.Status, &value.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return locking.Pharmacy{}, missingTarget()
	}
	if err != nil {
		return locking.Pharmacy{}, fmt.Errorf("lock pharmacy: %w", err)
	}
	return value, nil
}

func (r *CanonicalLockRepository) lockSale(ctx context.Context, pharmacyID, saleID uuid.UUID) (locking.Sale, error) {
	var value locking.Sale
	err := r.tx.QueryRow(ctx, `SELECT id, pharmacy_id, status FROM sales WHERE id = $1 AND pharmacy_id = $2 FOR UPDATE`, saleID, pharmacyID).
		Scan(&value.ID, &value.PharmacyID, &value.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return locking.Sale{}, missingTarget()
	}
	if err != nil {
		return locking.Sale{}, fmt.Errorf("lock sale: %w", err)
	}
	return value, nil
}

func (r *CanonicalLockRepository) lockProducts(ctx context.Context, pharmacyID uuid.UUID, ids []uuid.UUID) ([]locking.PharmacyProduct, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.tx.Query(ctx, `
		SELECT id, pharmacy_id, product_presentation_id, is_inner_unit_sale_allowed,
		       default_package_price_dirams, default_inner_unit_price_dirams, status, version
		FROM pharmacy_products
		WHERE pharmacy_id = $1 AND id = ANY($2::uuid[])
		ORDER BY id
		FOR UPDATE`, pharmacyID, ids)
	if err != nil {
		return nil, fmt.Errorf("lock pharmacy products: %w", err)
	}
	defer rows.Close()
	values := make([]locking.PharmacyProduct, 0, len(ids))
	for rows.Next() {
		var value locking.PharmacyProduct
		if err := rows.Scan(&value.ID, &value.PharmacyID, &value.ProductPresentationID, &value.InnerUnitSaleAllowed,
			&value.DefaultPackagePriceDirams, &value.DefaultInnerUnitPriceDirams, &value.Status, &value.Version); err != nil {
			return nil, fmt.Errorf("scan locked pharmacy product: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked pharmacy products: %w", err)
	}
	if len(values) != len(ids) {
		return nil, missingTarget()
	}
	return values, nil
}

func (r *CanonicalLockRepository) lockAllocations(ctx context.Context, saleID uuid.UUID, saleItemIDs, ids []uuid.UUID) ([]locking.SourceAllocation, error) {
	rows, err := r.tx.Query(ctx, `
		SELECT allocation.id, allocation.sale_item_id, allocation.stock_lot_id, allocation.quantity_base_units
		FROM sale_item_allocations AS allocation
		JOIN sale_items AS item ON item.id = allocation.sale_item_id
		WHERE item.sale_id = $1
		  AND item.id = ANY($2::uuid[])
		  AND allocation.id = ANY($3::uuid[])
		ORDER BY allocation.id
		FOR UPDATE OF allocation`, saleID, saleItemIDs, ids)
	if err != nil {
		return nil, fmt.Errorf("lock sale allocations: %w", err)
	}
	defer rows.Close()
	values := make([]locking.SourceAllocation, 0, len(ids))
	for rows.Next() {
		var value locking.SourceAllocation
		if err := rows.Scan(&value.ID, &value.SaleItemID, &value.StockLotID, &value.QuantityBaseUnits); err != nil {
			return nil, fmt.Errorf("scan locked sale allocation: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked sale allocations: %w", err)
	}
	if len(values) != len(ids) {
		return nil, missingTarget()
	}
	return values, nil
}

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

func (r *CanonicalLockRepository) lockLots(ctx context.Context, productIDs, ids []uuid.UUID) ([]locking.StockLot, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.tx.Query(ctx, `
		SELECT id, pharmacy_product_id, expiration_date, received_at,
		       quantity_base_units, base_units_per_package_snapshot,
		       package_retail_price_dirams, inner_unit_retail_price_dirams,
		       status, version
		FROM stock_lots
		WHERE pharmacy_product_id = ANY($1::uuid[]) AND id = ANY($2::uuid[])
		ORDER BY expiration_date, received_at, id
		FOR UPDATE`, productIDs, ids)
	if err != nil {
		return nil, fmt.Errorf("lock stock lots: %w", err)
	}
	defer rows.Close()
	values := make([]locking.StockLot, 0, len(ids))
	for rows.Next() {
		var value locking.StockLot
		if err := rows.Scan(&value.ID, &value.PharmacyProductID, &value.ExpirationDate, &value.ReceivedAt,
			&value.QuantityBaseUnits, &value.BaseUnitsPerPackage, &value.PackageRetailPriceDirams,
			&value.InnerUnitRetailPriceDirams, &value.Status, &value.Version); err != nil {
			return nil, fmt.Errorf("scan locked stock lot: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked stock lots: %w", err)
	}
	if len(values) != len(ids) {
		return nil, missingTarget()
	}
	return values, nil
}

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

var _ locking.Repository = (*CanonicalLockRepository)(nil)
