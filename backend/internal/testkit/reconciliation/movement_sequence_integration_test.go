package reconciliation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMovementSequenceOrderingAndConcurrencyIntegration(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID, pharmacyID, productID, presentationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pharmacyProductID, receiptOperationID, receiptID, receiptItemID, lotID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	operationOne, operationTwo, operationThree := uuid.New(), uuid.New(), uuid.New()
	reversalOperation, concurrentOne, concurrentTwo := uuid.New(), uuid.New(), uuid.New()
	movementOne, movementTwo, reversalMovement := uuid.New(), uuid.New(), uuid.New()
	concurrentMovementOne, concurrentMovementTwo := uuid.New(), uuid.New()
	operationIDs := []uuid.UUID{operationOne, operationTwo, operationThree, reversalOperation, concurrentOne, concurrentTwo}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inventory_movements WHERE id=ANY($1::uuid[])`, []uuid.UUID{movementOne, movementTwo, reversalMovement, concurrentMovementOne, concurrentMovementTwo})
		_, _ = pool.Exec(context.Background(), `DELETE FROM stock_lots WHERE id=$1`, lotID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM receipt_items WHERE id=$1`, receiptItemID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM receipts WHERE id=$1`, receiptID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM inventory_operations WHERE id=ANY($1::uuid[])`, append(operationIDs, receiptOperationID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM pharmacy_products WHERE id=$1`, pharmacyProductID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM product_presentations WHERE id=$1`, presentationID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, productID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM pharmacies WHERE id=$1`, pharmacyID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})

	if _, err := pool.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Sequence User')`, userID, "sequence-"+userID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pharmacies(id,name,address,latitude,longitude) VALUES($1,'Sequence Pharmacy','Test',0,0)`, pharmacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO products(id,title,form,manufacturer) VALUES($1,'Sequence Product','tablet','Test')`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO product_presentations(id,product_id,package_name,base_units_per_package) VALUES($1,$2,'box',1)`, presentationID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pharmacy_products(id,pharmacy_id,product_presentation_id,default_package_price_dirams) VALUES($1,$2,$3,100)`, pharmacyProductID, pharmacyID, presentationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_operations(id,pharmacy_id,operation_type,initiated_by_user_id,occurred_at) VALUES($1,$2,'RECEIPT',$3,now())`, receiptOperationID, pharmacyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipts(id,pharmacy_id,operation_id,receipt_number,received_at,posted_by_user_id,posted_at) VALUES($1,$2,$3,($1::uuid)::text,now(),$4,now())`, receiptID, pharmacyID, receiptOperationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipt_items(id,receipt_id,pharmacy_product_id,batch_number,expiration_date,quantity_packages,base_units_per_package_snapshot,quantity_base_units,purchase_price_package_dirams,retail_price_package_dirams) VALUES($1,$2,$3,'sequence','2035-01-01',10,1,10,50,100)`, receiptItemID, receiptID, pharmacyProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO stock_lots(id,pharmacy_product_id,receipt_item_id,origin_type,batch_number,expiration_date,quantity_base_units,base_units_per_package_snapshot,purchase_price_package_dirams,package_retail_price_dirams,received_at) VALUES($1,$2,$3,'RECEIPT','sequence','2035-01-01',10,1,50,100,now())`, lotID, pharmacyProductID, receiptItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_operations(id,pharmacy_id,operation_type,initiated_by_user_id,occurred_at) SELECT id,$1,'INVENTORY_ADJUSTMENT',$2,now() FROM unnest($3::uuid[]) AS id`, pharmacyID, userID, []uuid.UUID{operationOne, operationTwo, operationThree, concurrentOne, concurrentTwo}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_operations(id,pharmacy_id,operation_type,initiated_by_user_id,reversal_of_operation_id,occurred_at) VALUES($1,$2,'REVERSAL',$3,$4,now())`, reversalOperation, pharmacyID, userID, operationTwo); err != nil {
		t.Fatal(err)
	}

	sharedTimestamp := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units,created_at) VALUES($1,$2,$3,5,5,$4),($5,$6,$3,5,10,$4)`, movementOne, operationOne, lotID, sharedTimestamp, movementTwo, operationTwo); err != nil {
		t.Fatal(err)
	}
	checker := NewChecker(pool)
	var uniqueSequenceConstraint bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conname='uq_inventory_movement_lot_sequence' AND contype='u')`).Scan(&uniqueSequenceConstraint); err != nil || !uniqueSequenceConstraint {
		t.Fatalf("per-lot sequence unique constraint missing: %v", err)
	}
	if violations, err := checker.movementStateViolations(ctx, []uuid.UUID{operationOne, operationTwo}); err != nil || len(violations) != 0 {
		t.Fatalf("equal timestamps were not ordered by sequence: violations=%#v err=%v", violations, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_movements SET lot_sequence=1 WHERE id=$1`, movementTwo); err == nil {
		t.Fatal("duplicate lot sequence was accepted")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23505" || postgresError.ConstraintName != "uq_inventory_movement_lot_sequence" {
			t.Fatalf("expected lot sequence unique violation, got %v", err)
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE inventory_movements SET lot_sequence=3 WHERE id=$1`, movementTwo); err != nil {
		t.Fatal(err)
	}
	violations, err := checker.movementStateViolations(ctx, []uuid.UUID{operationOne, operationTwo})
	if err != nil || len(violations) == 0 {
		t.Fatalf("missing sequence was not detected: violations=%#v err=%v", violations, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory_movements SET lot_sequence=2 WHERE id=$1`, movementTwo); err != nil {
		t.Fatal(err)
	}

	for name, sequence := range map[string]int64{"duplicate": 2, "missing next": 4} {
		t.Run(name+" sequence is rejected", func(t *testing.T) {
			_, err := pool.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,lot_sequence,delta_base_units,quantity_after_base_units) VALUES(gen_random_uuid(),$1,$2,$3,1,11)`, operationThree, lotID, sequence)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "23514" {
				t.Fatalf("expected sequence check violation, got %v", err)
			}
		})
	}

	if _, err := pool.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units) VALUES($1,$2,$3,-2,8)`, reversalMovement, reversalOperation, lotID); err != nil {
		t.Fatal(err)
	}
	var reversalSequence int64
	if err := pool.QueryRow(ctx, `SELECT lot_sequence FROM inventory_movements WHERE id=$1`, reversalMovement).Scan(&reversalSequence); err != nil || reversalSequence != 3 {
		t.Fatalf("reversal sequence=%d err=%v", reversalSequence, err)
	}

	txOne, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := txOne.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units) VALUES($1,$2,$3,1,9)`, concurrentMovementOne, concurrentOne, lotID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units) VALUES($1,$2,$3,1,10)`, concurrentMovementTwo, concurrentTwo, lotID)
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("concurrent movement did not wait for lot lock: %v", err)
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
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent movement remained blocked")
	}
	var firstSequence, secondSequence int64
	if err := pool.QueryRow(ctx, `SELECT lot_sequence FROM inventory_movements WHERE id=$1`, concurrentMovementOne).Scan(&firstSequence); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT lot_sequence FROM inventory_movements WHERE id=$1`, concurrentMovementTwo).Scan(&secondSequence); err != nil {
		t.Fatal(err)
	}
	if firstSequence != 4 || secondSequence != 5 {
		t.Fatalf("concurrent sequences=(%d,%d)", firstSequence, secondSequence)
	}
}
