package postgres

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCanonicalLocksSerializeAndReturnSortedProductsIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	pharmacyID, productID, userID := uuid.New(), uuid.New(), uuid.New()
	presentationOne, presentationTwo := uuid.New(), uuid.New()
	first, second := uuid.New(), uuid.New()
	if bytes.Compare(first[:], second[:]) > 0 {
		first, second = second, first
	}
	receiptOperationID, saleOperationID, receiptID, saleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	receiptItemOne, receiptItemTwo, saleItemOne, saleItemTwo := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	lotLater, lotEarlier := uuid.New(), uuid.New()
	allocationFirst, allocationSecond := uuid.New(), uuid.New()
	if bytes.Compare(allocationFirst[:], allocationSecond[:]) > 0 {
		allocationFirst, allocationSecond = allocationSecond, allocationFirst
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sale_item_allocations WHERE id IN ($1,$2)", allocationFirst, allocationSecond)
		_, _ = pool.Exec(context.Background(), "DELETE FROM sale_items WHERE id IN ($1,$2)", saleItemOne, saleItemTwo)
		_, _ = pool.Exec(context.Background(), "DELETE FROM sales WHERE id = $1", saleID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM stock_lots WHERE id IN ($1,$2)", lotLater, lotEarlier)
		_, _ = pool.Exec(context.Background(), "DELETE FROM receipt_items WHERE id IN ($1,$2)", receiptItemOne, receiptItemTwo)
		_, _ = pool.Exec(context.Background(), "DELETE FROM receipts WHERE id = $1", receiptID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM inventory_operations WHERE id IN ($1,$2)", receiptOperationID, saleOperationID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM pharmacy_products WHERE id IN ($1,$2)", first, second)
		_, _ = pool.Exec(context.Background(), "DELETE FROM product_presentations WHERE id IN ($1,$2)", presentationOne, presentationTwo)
		_, _ = pool.Exec(context.Background(), "DELETE FROM products WHERE id = $1", productID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM pharmacies WHERE id = $1", pharmacyID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})
	if _, err := pool.Exec(ctx, "INSERT INTO users (id,login,password_hash,display_name) VALUES ($1,$2,'hash','Lock User')", userID, "lock-"+userID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO pharmacies (id,name,address,latitude,longitude) VALUES ($1,'Lock Test','Test',0,0)", pharmacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO products (id,title,form,manufacturer) VALUES ($1,'Lock Product','tablet','Test')", productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_presentations (id,product_id,package_name,base_units_per_package) VALUES
		($1,$3,'box',1),($2,$3,'bottle',1)`, presentationOne, presentationTwo, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pharmacy_products (id,pharmacy_id,product_presentation_id,default_package_price_dirams) VALUES
		($1,$3,$4,100),($2,$3,$5,200)`, first, second, pharmacyID, presentationOne, presentationTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_operations (id,pharmacy_id,operation_type,initiated_by_user_id,occurred_at) VALUES
		($1,$3,'RECEIPT',$4,now()),($2,$3,'SALE',$4,now())`, receiptOperationID, saleOperationID, pharmacyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipts (id,pharmacy_id,operation_id,receipt_number,received_at,posted_by_user_id,posted_at)
		VALUES ($1::uuid,$2,$3,($1::uuid)::text,now(),$4,now())`, receiptID, pharmacyID, receiptOperationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipt_items (
		id,receipt_id,pharmacy_product_id,batch_number,expiration_date,quantity_packages,
		base_units_per_package_snapshot,quantity_base_units,purchase_price_package_dirams,retail_price_package_dirams
	) VALUES
		($1,$3,$4,'later','2031-01-01',10,1,10,50,100),
		($2,$3,$5,'earlier','2030-01-01',10,1,10,60,200)`, receiptItemOne, receiptItemTwo, receiptID, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO stock_lots (
		id,pharmacy_product_id,receipt_item_id,origin_type,batch_number,expiration_date,
		quantity_base_units,base_units_per_package_snapshot,purchase_price_package_dirams,
		package_retail_price_dirams,received_at
	) VALUES
		($1,$3,$5,'RECEIPT','later','2031-01-01',10,1,50,100,'2029-02-01'),
		($2,$4,$6,'RECEIPT','earlier','2030-01-01',10,1,60,200,'2029-01-01')`, lotLater, lotEarlier, first, second, receiptItemOne, receiptItemTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sales (
		id,pharmacy_id,operation_id,sale_number,payment_method,subtotal_amount_dirams,
		discount_amount_dirams,total_amount_dirams,sold_by_user_id,sold_at
		) VALUES ($1::uuid,$2,$3,($1::uuid)::text,'CASH',300,0,300,$4,now())`, saleID, pharmacyID, saleOperationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sale_items (
		id,sale_id,pharmacy_product_id,product_title_snapshot,presentation_snapshot,sale_unit,
		display_quantity,base_units_per_package_snapshot,quantity_base_units,unit_price_dirams,
		line_subtotal_dirams,line_total_dirams
	) VALUES
		($1,$3,$4,'First','box','PACKAGE',1,1,1,100,100,100),
		($2,$3,$5,'Second','bottle','PACKAGE',1,1,1,200,200,200)`, saleItemOne, saleItemTwo, saleID, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sale_item_allocations (id,sale_item_id,stock_lot_id,quantity_base_units) VALUES
		($1,$3,$5,1),($2,$4,$6,1)`, allocationFirst, allocationSecond, saleItemOne, saleItemTwo, lotLater, lotEarlier); err != nil {
		t.Fatal(err)
	}

	txOne, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	repositoryOne, err := NewCanonicalLockRepository(txOne)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := repositoryOne.LockInventory(ctx, locking.InventoryPlan{
		PharmacyID: pharmacyID, PharmacyProductIDs: []uuid.UUID{second, first, second},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(locks.PharmacyProducts) != 2 || locks.PharmacyProducts[0].ID != first || locks.PharmacyProducts[1].ID != second {
		t.Fatalf("products not locked in UUID order: %#v", locks.PharmacyProducts)
	}

	result := make(chan error, 1)
	go func() {
		txTwo, err := pool.Begin(ctx)
		if err != nil {
			result <- err
			return
		}
		repositoryTwo, err := NewCanonicalLockRepository(txTwo)
		if err == nil {
			_, err = repositoryTwo.LockInventory(ctx, locking.InventoryPlan{
				PharmacyID: pharmacyID, PharmacyProductIDs: []uuid.UUID{first, second},
			})
		}
		if err != nil {
			_ = txTwo.Rollback(ctx)
			result <- err
			return
		}
		result <- txTwo.Commit(ctx)
	}()

	select {
	case err := <-result:
		_ = txOne.Rollback(ctx)
		t.Fatalf("second transaction bypassed pharmacy/product locks: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := txOne.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second transaction did not resume after canonical locks were released")
	}

	txMissing, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	missingRepository, _ := NewCanonicalLockRepository(txMissing)
	_, err = missingRepository.LockInventory(ctx, locking.InventoryPlan{
		PharmacyID: pharmacyID, PharmacyProductIDs: []uuid.UUID{first, uuid.New()},
	})
	_ = txMissing.Rollback(ctx)
	if !errors.Is(err, locking.ErrLockTargetMissing) {
		t.Fatalf("missing target silently ignored: %v", err)
	}

	txReturn, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	returnRepository, _ := NewCanonicalLockRepository(txReturn)
	returnLocks, err := returnRepository.LockSaleReturn(ctx, locking.SaleReturnPlan{
		PharmacyID: pharmacyID, SaleID: saleID,
		PharmacyProductIDs:  []uuid.UUID{second, first},
		SourceAllocationIDs: []uuid.UUID{allocationSecond, allocationFirst},
		StockLotIDs:         []uuid.UUID{lotLater, lotEarlier},
	})
	if err != nil {
		_ = txReturn.Rollback(ctx)
		t.Fatal(err)
	}
	if err := txReturn.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if returnLocks.Sale.ID != saleID ||
		returnLocks.PharmacyProducts[0].ID != first ||
		returnLocks.SourceAllocations[0].ID != allocationFirst ||
		returnLocks.StockLots[0].ID != lotEarlier {
		t.Fatalf("sale return locks do not follow canonical order: %#v", returnLocks)
	}
}
