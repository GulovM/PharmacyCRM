package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNullableLifecycleConstraintsIntegration(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	tests := []struct {
		name      string
		statement string
		rejected  bool
	}{
		{"user role active", `INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id) VALUES($4,$1,(SELECT id FROM roles WHERE code='CLIENT'),$2)`, false},
		{"user role revoked", `INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id,revoked_at,revoked_by_user_id,revoke_reason) VALUES($4,$1,(SELECT id FROM roles WHERE code='CLIENT'),$2,now(),$2,'duplicate assignment')`, false},
		{"user role null reason", `INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id,revoked_at,revoked_by_user_id,revoke_reason) VALUES($4,$1,(SELECT id FROM roles WHERE code='CLIENT'),$2,now(),$2,NULL)`, true},
		{"user role blank reason", `INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id,revoked_at,revoked_by_user_id,revoke_reason) VALUES($4,$1,(SELECT id FROM roles WHERE code='CLIENT'),$2,now(),$2,'   ')`, true},
		{"user role partial actor pair", `INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id,revoked_at,revoked_by_user_id,revoke_reason) VALUES($4,$1,(SELECT id FROM roles WHERE code='CLIENT'),$2,now(),NULL,'duplicate assignment')`, true},

		{"session active", `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level) VALUES($4,$1,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),1,now()+interval '1 day',now()+interval '1 day',now()+interval '1 day','PASSWORD','NONE')`, false},
		{"session revoked", `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level,revoked_at,revoke_reason) VALUES($4,$1,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),1,now()+interval '1 day',now()+interval '1 day',now()+interval '1 day','PASSWORD','NONE',now(),'rotation')`, false},
		{"session null reason", `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level,revoked_at,revoke_reason) VALUES($4,$1,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),1,now()+interval '1 day',now()+interval '1 day',now()+interval '1 day','PASSWORD','NONE',now(),NULL)`, true},
		{"session blank reason", `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level,revoked_at,revoke_reason) VALUES($4,$1,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),1,now()+interval '1 day',now()+interval '1 day',now()+interval '1 day','PASSWORD','NONE',now(),'   ')`, true},
		{"session partial state", `INSERT INTO user_sessions(id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level,revoked_at,revoke_reason) VALUES($4,$1,convert_to(gen_random_uuid()::text,'UTF8'),gen_random_uuid(),1,now()+interval '1 day',now()+interval '1 day',now()+interval '1 day','PASSWORD','NONE',NULL,'rotation')`, true},

		{"assignment active", `INSERT INTO pharmacy_assignments(id,user_id,pharmacy_id,assigned_by_user_id) VALUES($4,$1,$3,$2)`, false},
		{"assignment ended", `INSERT INTO pharmacy_assignments(id,user_id,pharmacy_id,assigned_by_user_id,ended_at,ended_by_user_id,end_reason) VALUES($4,$1,$3,$2,now(),$2,'transfer')`, false},
		{"assignment null reason", `INSERT INTO pharmacy_assignments(id,user_id,pharmacy_id,assigned_by_user_id,ended_at,ended_by_user_id,end_reason) VALUES($4,$1,$3,$2,now(),$2,NULL)`, true},
		{"assignment blank reason", `INSERT INTO pharmacy_assignments(id,user_id,pharmacy_id,assigned_by_user_id,ended_at,ended_by_user_id,end_reason) VALUES($4,$1,$3,$2,now(),$2,'   ')`, true},
		{"assignment partial actor pair", `INSERT INTO pharmacy_assignments(id,user_id,pharmacy_id,assigned_by_user_id,ended_at,ended_by_user_id,end_reason) VALUES($4,$1,$3,$2,now(),NULL,'transfer')`, true},

		{"product request open", `INSERT INTO product_requests(id,pharmacy_id,requested_by_user_id,raw_name) VALUES($4,$3,$1,'missing product')`, false},
		{"product request terminal", `INSERT INTO product_requests(id,pharmacy_id,requested_by_user_id,raw_name,status,resolved_by_user_id,resolved_at,resolution_note) VALUES($4,$3,$1,'missing product','REJECTED',$2,now(),'not supplied')`, false},
		{"product request null reason", `INSERT INTO product_requests(id,pharmacy_id,requested_by_user_id,raw_name,status,resolved_by_user_id,resolved_at,resolution_note) VALUES($4,$3,$1,'missing product','REJECTED',$2,now(),NULL)`, true},
		{"product request blank reason", `INSERT INTO product_requests(id,pharmacy_id,requested_by_user_id,raw_name,status,resolved_by_user_id,resolved_at,resolution_note) VALUES($4,$3,$1,'missing product','REJECTED',$2,now(),'   ')`, true},
		{"product request partial actor pair", `INSERT INTO product_requests(id,pharmacy_id,requested_by_user_id,raw_name,status,resolved_by_user_id,resolved_at,resolution_note) VALUES($4,$3,$1,'missing product','REJECTED',NULL,now(),'not supplied')`, true},

		{"alert active", `INSERT INTO alerts(id,pharmacy_id,alert_type,deduplication_key,status,detected_at,last_confirmed_at) VALUES($4,$3,'LOW_STOCK',($4::uuid)::text,'ACTIVE',now(),now())`, false},
		{"alert acknowledged", `INSERT INTO alerts(id,pharmacy_id,alert_type,deduplication_key,status,detected_at,last_confirmed_at,acknowledged_by_user_id,acknowledged_at) VALUES($4,$3,'LOW_STOCK',($4::uuid)::text,'ACKNOWLEDGED',now(),now(),$2,now())`, false},
		{"alert directly resolved", `INSERT INTO alerts(id,pharmacy_id,alert_type,deduplication_key,status,detected_at,last_confirmed_at,resolved_by_user_id,resolved_at) VALUES($4,$3,'LOW_STOCK',($4::uuid)::text,'RESOLVED',now(),now(),$2,now())`, false},
		{"alert partial acknowledgement", `INSERT INTO alerts(id,pharmacy_id,alert_type,deduplication_key,status,detected_at,last_confirmed_at,acknowledged_by_user_id) VALUES($4,$3,'LOW_STOCK',($4::uuid)::text,'ACKNOWLEDGED',now(),now(),$2)`, true},
		{"alert partial resolution", `INSERT INTO alerts(id,pharmacy_id,alert_type,deduplication_key,status,detected_at,last_confirmed_at,resolved_at) VALUES($4,$3,'LOW_STOCK',($4::uuid)::text,'RESOLVED',now(),now(),now())`, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			userID, actorID, pharmacyID, rowID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES
				($1,'lifecycle-' || gen_random_uuid()::text,'hash','Lifecycle User'),
				($2,'lifecycle-' || gen_random_uuid()::text,'hash','Lifecycle Actor')`, userID, actorID); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pharmacies(id,name,address,latitude,longitude) VALUES($1,'Lifecycle Pharmacy','Test',0,0)`, pharmacyID); err != nil {
				t.Fatal(err)
			}

			statement := `WITH test_parameters AS (SELECT $1::uuid, $2::uuid, $3::uuid, $4::uuid) ` + test.statement
			_, err = tx.Exec(ctx, statement, userID, actorID, pharmacyID, rowID)
			if !test.rejected {
				if err != nil {
					t.Fatalf("valid state rejected: %v", err)
				}
				return
			}
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "23514" {
				t.Fatalf("expected check violation, got %v", err)
			}
		})
	}
}
