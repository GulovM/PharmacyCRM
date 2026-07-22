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

func TestSessionSecurityConstraintsIntegration(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, postgrestest.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	userID, otherUserID, familyID := uuid.New(), uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{userID, otherUserID} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Session Test')`, user, "session-"+user.String()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM user_sessions WHERE user_id IN ($1,$2)`, userID, otherUserID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, userID, otherUserID)
	})

	insert := func(tx *pgxpool.Conn, id, user, family uuid.UUID, generation int, predecessor *uuid.UUID, predecessorUser *uuid.UUID, predecessorFamily *uuid.UUID, predecessorGeneration *int) error {
		_, err := tx.Exec(ctx, `INSERT INTO user_sessions(
			id,user_id,refresh_token_hash,token_family_id,generation,rotated_from_session_id,
			rotated_from_user_id,rotated_from_token_family_id,rotated_from_generation,
			created_at,last_used_at,expires_at,idle_expires_at,absolute_expires_at,authentication_method,mfa_level
		) VALUES($1,$2,convert_to($1::text,'UTF8'),$3,$4,$5,$6,$7,$8,now(),now(),now()+interval '1 hour',now()+interval '1 hour',now()+interval '1 hour','PASSWORD','NONE')`,
			id, user, family, generation, predecessor, predecessorUser, predecessorFamily, predecessorGeneration)
		return err
	}

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Release()
	firstID := uuid.New()
	if err := insert(connection, firstID, userID, familyID, 1, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	secondGeneration := 2
	if err := insert(connection, uuid.New(), userID, familyID, 2, &firstID, &userID, &familyID, &secondGeneration); err == nil {
		t.Fatal("rotation predecessor generation mismatch was accepted")
	} else {
		assertConstraintState(t, err, "23514")
	}
	if err := insert(connection, uuid.New(), userID, familyID, 0, nil, nil, nil, nil); err == nil {
		t.Fatal("generation zero was accepted")
	} else {
		assertConstraintState(t, err, "23514")
	}
	if err := insert(connection, uuid.New(), otherUserID, familyID, 2, &firstID, &userID, &familyID, intPointer(1)); err == nil {
		t.Fatal("cross-user predecessor was accepted")
	} else {
		assertConstraintState(t, err, "23514")
	}
	if err := insert(connection, uuid.New(), userID, familyID, 2, &firstID, &userID, &familyID, intPointer(1)); err != nil {
		t.Fatalf("valid rotation chain rejected: %v", err)
	}
}

func intPointer(value int) *int { return &value }

func assertConstraintState(t testing.TB, err error, expected string) {
	t.Helper()
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != expected {
		t.Fatalf("expected SQLSTATE %s, got %v", expected, err)
	}
}
