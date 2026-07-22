package reconciliation

import (
	"context"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCheckerDetectsInventoryDivergenceAndMissingEffectsIntegration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID, pharmacyID, productID, presentationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pharmacyProductID, operationID, receiptID, receiptItemID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	lotID, movementID, auditID, outboxID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	saleID, saleItemID, allocationID := uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM sale_item_allocations WHERE id = $1", allocationID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM sale_items WHERE id = $1", saleItemID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM sales WHERE id = $1", saleID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM audit_events WHERE id = $1", auditID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM outbox_events WHERE id = $1", outboxID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM inventory_movements WHERE id = $1", movementID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM stock_lots WHERE id = $1", lotID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM receipt_items WHERE id = $1", receiptItemID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM receipts WHERE id = $1", receiptID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM inventory_operations WHERE id = $1", operationID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM pharmacy_products WHERE id = $1", pharmacyProductID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM product_presentations WHERE id = $1", presentationID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM products WHERE id = $1", productID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM pharmacies WHERE id = $1", pharmacyID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	if _, err := pool.Exec(ctx, "INSERT INTO users (id,login,password_hash,display_name) VALUES ($1,$2,'hash','Reconciliation User')", userID, "reconcile-"+userID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO pharmacies (id,name,address,latitude,longitude) VALUES ($1,'Reconciliation Test','Test',0,0)", pharmacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO products (id,title,form,manufacturer) VALUES ($1,'Reconciliation Product','tablet','Test')", productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO product_presentations (id,product_id,package_name,base_units_per_package) VALUES ($1,$2,'box',1)", presentationID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO pharmacy_products (id,pharmacy_id,product_presentation_id,default_package_price_dirams) VALUES ($1,$2,$3,100)", pharmacyProductID, pharmacyID, presentationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO inventory_operations (id,pharmacy_id,operation_type,initiated_by_user_id,occurred_at) VALUES ($1,$2,'RECEIPT',$3,now())", operationID, pharmacyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipts (id,pharmacy_id,operation_id,receipt_number,received_at,posted_by_user_id,posted_at)
		VALUES ($1::uuid,$2,$3,($1::uuid)::text,now(),$4,now())`, receiptID, pharmacyID, operationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO receipt_items (
		id,receipt_id,pharmacy_product_id,batch_number,expiration_date,quantity_packages,
		base_units_per_package_snapshot,quantity_base_units,purchase_price_package_dirams,retail_price_package_dirams
	) VALUES ($1,$2,$3,'batch','2035-01-01',10,1,10,50,100)`, receiptItemID, receiptID, pharmacyProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO stock_lots (
		id,pharmacy_product_id,receipt_item_id,origin_type,batch_number,expiration_date,
		quantity_base_units,base_units_per_package_snapshot,purchase_price_package_dirams,
		package_retail_price_dirams,received_at
	) VALUES ($1,$2,$3,'RECEIPT','batch','2035-01-01',10,1,50,100,now())`, lotID, pharmacyProductID, receiptItemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO inventory_movements (id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units) VALUES ($1,$2,$3,10,10)", movementID, operationID, lotID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events (
		id,occurred_at,actor_user_id,actor_type,action,object_type,object_id,result,metadata
	) VALUES ($1,now(),$2,'USER','test.receipt.posted','inventory_operation',$3,'SUCCESS','{}')`, auditID, userID, operationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO outbox_events (
		id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,occurred_at
	) VALUES ($1::uuid,'test.receipt.posted','inventory_operation',$2::uuid,($2::uuid)::text,($1::uuid)::text,'{}',now())`, outboxID, operationID); err != nil {
		t.Fatal(err)
	}

	checker := NewChecker(pool)
	scope := Scope{OperationIDs: []uuid.UUID{operationID}, RequiredAuditEventIDs: []uuid.UUID{auditID}, RequiredOutboxEventIDs: []uuid.UUID{outboxID}}
	report, err := checker.Check(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Clean() {
		t.Fatalf("valid inventory did not reconcile: %#v", report.Violations)
	}
	missingOperationID := uuid.New()
	missingReport, err := checker.Check(ctx, Scope{OperationIDs: []uuid.UUID{missingOperationID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missingReport.Violations) != 1 || missingReport.Violations[0].Kind != MissingDocumentEffect || missingReport.Violations[0].EntityID != missingOperationID {
		t.Fatalf("missing operation was not reported: %#v", missingReport.Violations)
	}

	if _, err := pool.Exec(ctx, "UPDATE stock_lots SET quantity_base_units = 9, expiration_date = CURRENT_DATE - 1 WHERE id = $1", lotID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE inventory_movements SET quantity_after_base_units = 9 WHERE id = $1", movementID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE inventory_operations SET status = 'REVERSED' WHERE id = $1", operationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sales (
		id,pharmacy_id,operation_id,sale_number,payment_method,subtotal_amount_dirams,
		discount_amount_dirams,total_amount_dirams,sold_by_user_id,sold_at
	) VALUES ($1::uuid,$2,$3,($1::uuid)::text,'CASH',100,0,100,$4,now())`, saleID, pharmacyID, operationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sale_items (
		id,sale_id,pharmacy_product_id,product_title_snapshot,presentation_snapshot,sale_unit,
		display_quantity,base_units_per_package_snapshot,quantity_base_units,unit_price_dirams,
		line_subtotal_dirams,line_total_dirams
	) VALUES ($1,$2,$3,'Product','box','PACKAGE',1,1,1,100,100,100)`, saleItemID, saleID, pharmacyProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO sale_item_allocations (id,sale_item_id,stock_lot_id,quantity_base_units) VALUES ($1,$2,$3,1)", allocationID, saleItemID, lotID); err != nil {
		t.Fatal(err)
	}

	missingAuditID, missingOutboxID := uuid.New(), uuid.New()
	scope.RequiredAuditEventIDs = append(scope.RequiredAuditEventIDs, missingAuditID)
	scope.RequiredOutboxEventIDs = append(scope.RequiredOutboxEventIDs, missingOutboxID)
	report, err = checker.Check(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[Kind]bool{
		BalanceMismatch: false, OrphanAllocation: false, DuplicateEffect: false,
		MissingAudit: false, MissingOutbox: false, InvalidMovementState: false,
		InvalidOperationState: false, InvalidLotState: false,
	}
	for _, violation := range report.Violations {
		if _, tracked := wanted[violation.Kind]; tracked {
			wanted[violation.Kind] = true
		}
	}
	for kind, found := range wanted {
		if !found {
			t.Errorf("did not detect %s; report=%#v", kind, report.Violations)
		}
	}

	// The checker is diagnostic only: corrupt values remain unchanged.
	var quantity int64
	if err := pool.QueryRow(ctx, "SELECT quantity_base_units FROM stock_lots WHERE id = $1", lotID).Scan(&quantity); err != nil {
		t.Fatal(err)
	}
	if quantity != 9 {
		t.Fatalf("checker auto-fixed stock to %d", quantity)
	}
}
