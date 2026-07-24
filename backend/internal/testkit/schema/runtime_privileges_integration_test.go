package schema

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRuntimePrivilegeMatrixIntegration(t *testing.T) {
	ctx := context.Background()
	ownerPool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ownerPool.Close)
	runtimePool, err := pgxpool.New(ctx, postgrestest.RuntimeDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtimePool.Close)

	for name, statement := range map[string]string{
		"movement update":           `UPDATE inventory_movements SET delta_base_units=delta_base_units WHERE false`,
		"audit delete":              `DELETE FROM audit_events WHERE false`,
		"sale financial rewrite":    `UPDATE sales SET total_amount_dirams=total_amount_dirams WHERE false`,
		"receipt ownership rewrite": `UPDATE receipts SET pharmacy_id=pharmacy_id WHERE false`,
		"migration insert":          `INSERT INTO pharmacycrm_schema_migrations(version,name,checksum) VALUES(999999,'forbidden','forbidden')`,
		"migration history read":    `SELECT MAX(version) FROM pharmacycrm_schema_migrations`,
		"metadata update":           `UPDATE pharmacycrm_schema_metadata SET schema_version=schema_version WHERE singleton`,
		"business delete":           `DELETE FROM users WHERE false`,
	} {
		t.Run("denies "+name, func(t *testing.T) {
			_, err := runtimePool.Exec(ctx, statement)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
				t.Fatalf("expected insufficient privilege, got %v", err)
			}
		})
	}

	var schemaVersion int64
	if err := runtimePool.QueryRow(ctx, `SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton`).Scan(&schemaVersion); err != nil || schemaVersion != config.SupportedSchemaVersion {
		t.Fatalf("runtime readiness read failed: version=%d err=%v", schemaVersion, err)
	}
	// API runtime is intentionally read-mostly in E2. Worker-specific outbox
	// grants and secret-denial checks are exercised with its dedicated DSN in CI.
	if os.Getenv("RUN_LEGACY_RUNTIME_PRIVILEGE_MUTATION_TEST") == "" {
		return
	}

	userID, pharmacyID, productID := uuid.New(), uuid.New(), uuid.New()
	presentationID, pharmacyProductID := uuid.New(), uuid.New()
	operationID, receiptID, receiptItemID, lotID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	t.Cleanup(func() {
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM stock_lots WHERE id=$1`, lotID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM receipt_items WHERE id=$1`, receiptItemID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM receipts WHERE id=$1`, receiptID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM inventory_operations WHERE id=$1`, operationID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM pharmacy_products WHERE id=$1`, pharmacyProductID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM product_presentations WHERE id=$1`, presentationID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM products WHERE id=$1`, productID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM pharmacies WHERE id=$1`, pharmacyID)
		_, _ = ownerPool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, userID)
	})
	if _, err := ownerPool.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Privilege User')`, userID, "privilege-"+userID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO pharmacies(id,name,address,latitude,longitude) VALUES($1,'Privilege Pharmacy','Test',0,0)`, pharmacyID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO products(id,title,form,manufacturer) VALUES($1,'Privilege Product','tablet','Test')`, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO product_presentations(id,product_id,package_name,base_units_per_package) VALUES($1,$2,'box',1)`, presentationID, productID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO pharmacy_products(id,pharmacy_id,product_presentation_id,default_package_price_dirams) VALUES($1,$2,$3,100)`, pharmacyProductID, pharmacyID, presentationID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO inventory_operations(id,pharmacy_id,operation_type,initiated_by_user_id,occurred_at) VALUES($1,$2,'RECEIPT',$3,now())`, operationID, pharmacyID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO receipts(id,pharmacy_id,operation_id,receipt_number,received_at,posted_by_user_id,posted_at) VALUES($1,$2,$3,($1::uuid)::text,now(),$4,now())`, receiptID, pharmacyID, operationID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO receipt_items(id,receipt_id,pharmacy_product_id,batch_number,expiration_date,quantity_packages,base_units_per_package_snapshot,quantity_base_units,purchase_price_package_dirams,retail_price_package_dirams) VALUES($1,$2,$3,'batch','2030-01-01',10,1,10,50,100)`, receiptItemID, receiptID, pharmacyProductID); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO stock_lots(id,pharmacy_product_id,receipt_item_id,origin_type,batch_number,expiration_date,quantity_base_units,base_units_per_package_snapshot,purchase_price_package_dirams,package_retail_price_dirams,received_at) VALUES($1,$2,$3,'RECEIPT','batch','2030-01-01',10,1,50,100,now())`, lotID, pharmacyProductID, receiptItemID); err != nil {
		t.Fatal(err)
	}

	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	movementID, outboxID, auditID := uuid.New(), uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO inventory_movements(id,operation_id,stock_lot_id,delta_base_units,quantity_after_base_units) VALUES($1,$2,$3,10,10)`, movementID, operationID, lotID); err != nil {
		t.Fatalf("runtime movement insert rejected: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO outbox_events(id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,occurred_at) VALUES($1,'test.privilege','test',$2,($2::uuid)::text,($1::uuid)::text,'{}',now())`, outboxID, operationID); err != nil {
		t.Fatalf("runtime outbox insert rejected: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE outbox_events SET status='PROCESSING',attempt_count=1,lease_token=gen_random_uuid(),lease_generation=1,leased_by='privilege-test',lease_expires_at=now()+interval '1 minute' WHERE id=$1`, outboxID); err != nil {
		t.Fatalf("runtime outbox transition rejected: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events(id,occurred_at,actor_type,action,object_type,result) VALUES($1,now(),'SYSTEM','test.privilege','test','SUCCESS')`, auditID); err != nil {
		t.Fatalf("runtime audit insert rejected: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sales SET status=status WHERE false`); err != nil {
		t.Fatalf("approved root lifecycle column update rejected: %v", err)
	}
}

func TestCompatibilityRuntimeRoleExistsWithoutMembersIntegration(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var canLogin bool
	if err := pool.QueryRow(ctx, `SELECT rolcanlogin FROM pg_roles WHERE rolname='pharmacycrm_runtime'`).Scan(&canLogin); err != nil || canLogin {
		t.Fatalf("compatibility role login=%t err=%v", canLogin, err)
	}
	var members int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_auth_members membership JOIN pg_roles role ON role.oid=membership.roleid WHERE role.rolname='pharmacycrm_runtime'`).Scan(&members); err != nil || members != 0 {
		t.Fatalf("compatibility role members=%d err=%v", members, err)
	}
}
