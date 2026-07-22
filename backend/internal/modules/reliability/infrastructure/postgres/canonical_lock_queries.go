package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

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
