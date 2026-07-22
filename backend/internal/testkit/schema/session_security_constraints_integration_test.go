package schema

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sessionRow struct {
	id, userID, familyID                                  uuid.UUID
	generation                                            int64
	predecessorID, predecessorUserID, predecessorFamilyID *uuid.UUID
	predecessorGeneration                                 *int64
	createdAt, lastUsedAt, expiresAt, idleAt, absoluteAt  time.Time
	authenticationMethod, mfaLevel                        string
}

func TestSessionSecurityConstraintsIntegration(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	userID, otherUserID := uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{userID, otherUserID} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Session Test')`, user, "session-"+user.String()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_sessions WHERE user_id IN ($1,$2)`, userID, otherUserID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, userID, otherUserID)
	})

	t.Run("accepts valid rotation chains and independent families", func(t *testing.T) {
		withSessionTransaction(t, pool, func(tx pgx.Tx) {
			familyA, familyB := uuid.New(), uuid.New()
			first := validSessionRow(userID, familyA, 1)
			mustInsertSession(t, tx, first)
			second := validSessionRow(userID, familyA, 2)
			linkSession(&second, first)
			mustInsertSession(t, tx, second)
			third := validSessionRow(userID, familyA, 3)
			linkSession(&third, second)
			third.authenticationMethod, third.mfaLevel = "PASSWORD_MFA", "TOTP"
			mustInsertSession(t, tx, third)
			mustInsertSession(t, tx, validSessionRow(userID, familyB, 1))
		})
	})

	negativeCases := []struct {
		name  string
		codes []string
		run   func(testing.TB, pgx.Tx)
	}{
		{"generation zero", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 0)
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"duplicate family generation", []string{"23505"}, func(t testing.TB, tx pgx.Tx) {
			family := uuid.New()
			mustInsertSession(t, tx, validSessionRow(userID, family, 1))
			assertSessionInsertState(t, insertSession(ctx, tx, validSessionRow(userID, family, 1)), "23505")
		}},
		{"generation one with predecessor", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			parent := validSessionRow(userID, uuid.New(), 1)
			mustInsertSession(t, tx, parent)
			child := validSessionRow(userID, uuid.New(), 1)
			linkSession(&child, parent)
			assertSessionInsertState(t, insertSession(ctx, tx, child), "23514")
		}},
		{"generation two without predecessor", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			assertSessionInsertState(t, insertSession(ctx, tx, validSessionRow(userID, uuid.New(), 2)), "23514")
		}},
		{"cross user predecessor", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			family := uuid.New()
			parent := validSessionRow(userID, family, 1)
			mustInsertSession(t, tx, parent)
			child := validSessionRow(otherUserID, family, 2)
			linkSession(&child, parent)
			assertSessionInsertState(t, insertSession(ctx, tx, child), "23514")
		}},
		{"cross family predecessor", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			parent := validSessionRow(userID, uuid.New(), 1)
			mustInsertSession(t, tx, parent)
			child := validSessionRow(userID, uuid.New(), 2)
			linkSession(&child, parent)
			assertSessionInsertState(t, insertSession(ctx, tx, child), "23514")
		}},
		{"generation gap", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			family := uuid.New()
			parent := validSessionRow(userID, family, 1)
			mustInsertSession(t, tx, parent)
			child := validSessionRow(userID, family, 3)
			linkSession(&child, parent)
			assertSessionInsertState(t, insertSession(ctx, tx, child), "23514")
		}},
		{"unknown predecessor snapshot", []string{"23503"}, func(t testing.TB, tx pgx.Tx) {
			family := uuid.New()
			child := validSessionRow(userID, family, 2)
			predecessorID, predecessorGeneration := uuid.New(), int64(1)
			child.predecessorID = &predecessorID
			child.predecessorUserID = &userID
			child.predecessorFamilyID = &family
			child.predecessorGeneration = &predecessorGeneration
			assertSessionInsertState(t, insertSession(ctx, tx, child), "23503")
		}},
		{"forked rotation", []string{"23505"}, func(t testing.TB, tx pgx.Tx) {
			family := uuid.New()
			parent := validSessionRow(userID, family, 1)
			mustInsertSession(t, tx, parent)
			firstChild := validSessionRow(userID, family, 2)
			linkSession(&firstChild, parent)
			mustInsertSession(t, tx, firstChild)
			secondChild := validSessionRow(userID, family, 2)
			linkSession(&secondChild, parent)
			assertSessionInsertState(t, insertSession(ctx, tx, secondChild), "23505")
		}},
		{"idle expiry not after creation", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.idleAt, row.expiresAt = row.createdAt, row.createdAt
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"absolute expiry not after creation", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.absoluteAt = row.createdAt
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"idle expiry after absolute expiry", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.idleAt = row.absoluteAt.Add(time.Minute)
			row.expiresAt = row.absoluteAt
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"effective expiry mismatch", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.expiresAt = row.absoluteAt
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"invalid authentication method", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.authenticationMethod = "UNKNOWN"
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
		{"invalid mfa level", []string{"23514"}, func(t testing.TB, tx pgx.Tx) {
			row := validSessionRow(userID, uuid.New(), 1)
			row.mfaLevel = "UNKNOWN"
			assertSessionInsertState(t, insertSession(ctx, tx, row), "23514")
		}},
	}

	for _, test := range negativeCases {
		t.Run(test.name, func(t *testing.T) {
			withSessionTransaction(t, pool, func(tx pgx.Tx) { test.run(t, tx) })
		})
	}
}

func validSessionRow(userID, familyID uuid.UUID, generation int64) sessionRow {
	created := time.Now().UTC().Truncate(time.Microsecond)
	idle := created.Add(time.Hour)
	return sessionRow{
		id: uuid.New(), userID: userID, familyID: familyID, generation: generation,
		createdAt: created, lastUsedAt: created, expiresAt: idle, idleAt: idle,
		absoluteAt: created.Add(2 * time.Hour), authenticationMethod: "PASSWORD", mfaLevel: "NONE",
	}
}

func linkSession(child *sessionRow, parent sessionRow) {
	child.predecessorID = &parent.id
	child.predecessorUserID = &parent.userID
	child.predecessorFamilyID = &parent.familyID
	child.predecessorGeneration = &parent.generation
}

func insertSession(ctx context.Context, tx pgx.Tx, row sessionRow) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_sessions(
		id,user_id,refresh_token_hash,token_family_id,generation,rotated_from_session_id,
		rotated_from_user_id,rotated_from_token_family_id,rotated_from_generation,
		created_at,last_used_at,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level
	) VALUES($1,$2,convert_to($1::text,'UTF8'),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		row.id, row.userID, row.familyID, row.generation, row.predecessorID,
		row.predecessorUserID, row.predecessorFamilyID, row.predecessorGeneration,
		row.createdAt, row.lastUsedAt, row.expiresAt, row.idleAt, row.absoluteAt,
		row.authenticationMethod, row.mfaLevel)
	return err
}

func mustInsertSession(t testing.TB, tx pgx.Tx, row sessionRow) {
	t.Helper()
	if err := insertSession(context.Background(), tx, row); err != nil {
		t.Fatalf("insert valid session: %v", err)
	}
}

func withSessionTransaction(t testing.TB, pool *pgxpool.Pool, run func(pgx.Tx)) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	run(tx)
}

func assertSessionInsertState(t testing.TB, err error, expectedCodes ...string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		t.Fatalf("expected PostgreSQL error, got %v", err)
	}
	for _, code := range expectedCodes {
		if postgresError.Code == code {
			return
		}
	}
	t.Fatalf("expected SQLSTATE %v, got %s: %v", expectedCodes, postgresError.Code, err)
}
