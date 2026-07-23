#!/usr/bin/env bash
set -euo pipefail

cat > backend/migrations/000024_outbox_retention_cutoff_guard.up.sql <<'SQL'
-- E2-FIX-045: enforce retention windows inside SECURITY DEFINER functions.
-- Verification query: SELECT position('30 days' in pg_get_functiondef('public.delete_processed_outbox_events_before(timestamptz,integer)'::regprocedure)) > 0 AND position('180 days' in pg_get_functiondef('public.delete_dead_letter_outbox_events_before(timestamptz,integer)'::regprocedure)) > 0 AND has_function_privilege('pharmacycrm_worker_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE') AND NOT has_function_privilege('pharmacycrm_api_runtime','public.delete_processed_outbox_events_before(timestamptz,integer)','EXECUTE');
-- Lock/rewrite assessment: replaces two functions only; no table scan or rewrite.
-- Compatibility: signatures are unchanged; unsafe caller-supplied cutoffs now fail with SQLSTATE 22023.
-- Forward-fix policy: published migrations remain immutable; later corrections require another forward migration.

CREATE OR REPLACE FUNCTION public.delete_processed_outbox_events_before(
    p_before timestamptz,
    p_limit integer
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
    newest_allowed timestamptz := statement_timestamp() - interval '30 days';
BEGIN
    IF p_before IS NULL OR p_before > newest_allowed OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid processed outbox retention request' USING ERRCODE = '22023';
    END IF;
    WITH candidates AS (
        SELECT id FROM public.outbox_events
        WHERE status = 'PROCESSED' AND processed_at < p_before
        ORDER BY processed_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT p_limit
    ), deleted AS (
        DELETE FROM public.outbox_events AS event
        USING candidates
        WHERE event.id = candidates.id AND event.status = 'PROCESSED'
        RETURNING 1
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    RETURN deleted_count;
END;
$$;

CREATE OR REPLACE FUNCTION public.delete_dead_letter_outbox_events_before(
    p_before timestamptz,
    p_limit integer
) RETURNS bigint
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
DECLARE
    deleted_count bigint;
    newest_allowed timestamptz := statement_timestamp() - interval '180 days';
BEGIN
    IF p_before IS NULL OR p_before > newest_allowed OR p_limit < 1 OR p_limit > 1000 THEN
        RAISE EXCEPTION 'invalid dead-letter outbox retention request' USING ERRCODE = '22023';
    END IF;
    WITH candidates AS (
        SELECT id FROM public.outbox_events
        WHERE status = 'DEAD_LETTER' AND dead_lettered_at < p_before
        ORDER BY dead_lettered_at, id
        FOR UPDATE SKIP LOCKED
        LIMIT p_limit
    ), deleted AS (
        DELETE FROM public.outbox_events AS event
        USING candidates
        WHERE event.id = candidates.id AND event.status = 'DEAD_LETTER'
        RETURNING 1
    )
    SELECT COUNT(*) INTO deleted_count FROM deleted;
    RETURN deleted_count;
END;
$$;

REVOKE ALL ON FUNCTION public.delete_processed_outbox_events_before(timestamptz, integer) FROM PUBLIC, pharmacycrm_runtime, pharmacycrm_api_runtime;
REVOKE ALL ON FUNCTION public.delete_dead_letter_outbox_events_before(timestamptz, integer) FROM PUBLIC, pharmacycrm_runtime, pharmacycrm_api_runtime;
GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz, integer),
    public.delete_dead_letter_outbox_events_before(timestamptz, integer)
    TO pharmacycrm_worker_runtime;
SQL

cat > backend/internal/orchestration/outboxreplay/service.go <<'GO'
package outboxreplay

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
)

const (
	AuditAction = "outbox.manual_replayed"
	Operation   = "outbox.manual_replay"
)

var ErrInvalidReplayState = errors.New("invalid manual outbox replay state")

func AuditMetadataPolicy() audit.MetadataPolicy {
	return audit.MetadataPolicy{AuditAction: {"reason": audit.MetadataString}}
}

type Actor struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type UnitOfWork interface {
	ClaimIdempotency(context.Context, idempotency.Claim) (idempotency.ClaimResult, error)
	RevalidateAdmin(context.Context, Actor) error
	ReplayDeadLetter(context.Context, uuid.UUID, time.Time) error
	AppendAudit(context.Context, audit.Event) error
	CompleteIdempotency(context.Context, idempotency.Completion) error
}

type Transactor interface {
	WithinTransaction(context.Context, func(context.Context, UnitOfWork) error) error
}

type Command struct {
	EventID              uuid.UUID
	AuditEventID         uuid.UUID
	ActorUserID          uuid.UUID
	ActorSessionID       uuid.UUID
	Reason               string
	IdempotencyKey       string
	Fingerprint          idempotency.Fingerprint
	IdempotencyExpiresAt time.Time
	RequestID            string
	TraceID              string
	IPAddress            netip.Addr
	UserAgent            string
	OccurredAt           time.Time
}

type Result struct{ IdempotencyReplayed bool }

type Service struct{ transactor Transactor }

func NewService(transactor Transactor) *Service { return &Service{transactor: transactor} }

func (s *Service) Replay(ctx context.Context, command Command) (Result, error) {
	if err := validateCommand(s, command); err != nil {
		return Result{}, err
	}
	var result Result
	err := s.transactor.WithinTransaction(ctx, func(ctx context.Context, unitOfWork UnitOfWork) error {
		claim, err := unitOfWork.ClaimIdempotency(ctx, idempotency.Claim{
			Identity: idempotency.Identity{ActorID: command.ActorUserID, Operation: Operation, Key: command.IdempotencyKey},
			Fingerprint: command.Fingerprint,
			ExpiresAt:   command.IdempotencyExpiresAt,
		})
		if err != nil {
			return err
		}
		if err := unitOfWork.RevalidateAdmin(ctx, Actor{UserID: command.ActorUserID, SessionID: command.ActorSessionID}); err != nil {
			return err
		}
		if claim.State == idempotency.ReplayAvailable {
			if !validReplay(claim.Replay, command.EventID) {
				return errors.Join(ErrInvalidReplayState, apperror.ErrConflict)
			}
			result.IdempotencyReplayed = true
			return nil
		}
		if claim.State != idempotency.Claimed {
			return errors.Join(ErrInvalidReplayState, apperror.ErrConflict)
		}
		if err := unitOfWork.ReplayDeadLetter(ctx, command.EventID, command.OccurredAt); err != nil {
			return err
		}
		auditEvent := audit.Event{
			ID: command.AuditEventID, OccurredAt: command.OccurredAt,
			ActorUserID: &command.ActorUserID, ActorSessionID: &command.ActorSessionID,
			ActorType: audit.ActorUser, Action: AuditAction,
			ObjectType: "outbox_event", ObjectID: &command.EventID,
			Result: audit.ResultSuccess, RequestID: command.RequestID, TraceID: command.TraceID,
			IPAddress: command.IPAddress, UserAgent: command.UserAgent,
			Metadata: audit.Metadata{"reason": command.Reason},
		}
		if err := unitOfWork.AppendAudit(ctx, auditEvent); err != nil {
			return err
		}
		resourceID := command.EventID
		return unitOfWork.CompleteIdempotency(ctx, idempotency.Completion{
			RecordID: claim.RecordID,
			Result: idempotency.StoredResult{
				ResponseStatus: 200,
				ResponseBody:   json.RawMessage(`{"replayed":true}`),
				ResourceType:   "outbox_event",
				ResourceID:     &resourceID,
			},
		})
	})
	return result, err
}

func validateCommand(service *Service, command Command) error {
	var zeroFingerprint idempotency.Fingerprint
	if service == nil || service.transactor == nil || command.EventID == uuid.Nil || command.AuditEventID == uuid.Nil ||
		command.ActorUserID == uuid.Nil || command.ActorSessionID == uuid.Nil || command.OccurredAt.IsZero() ||
		command.IdempotencyExpiresAt.IsZero() || command.Fingerprint == zeroFingerprint ||
		strings.TrimSpace(command.IdempotencyKey) == "" || command.IdempotencyKey != strings.TrimSpace(command.IdempotencyKey) || len(command.IdempotencyKey) > 128 ||
		strings.TrimSpace(command.Reason) == "" || command.Reason != strings.TrimSpace(command.Reason) || len(command.Reason) > 512 {
		return errors.Join(apperror.ErrInvalidArgument, errors.New("invalid outbox replay command"))
	}
	return nil
}

func validReplay(replay *idempotency.StoredResult, eventID uuid.UUID) bool {
	return replay != nil && replay.ResponseStatus == 200 && replay.ResourceType == "outbox_event" &&
		replay.ResourceID != nil && *replay.ResourceID == eventID && json.Valid(replay.ResponseBody)
}
GO

cat > backend/internal/orchestration/outboxreplay/postgres/transactor.go <<'GO'
package postgres

import (
	"context"
	"errors"
	"time"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	auditpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	outboxpostgres "github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/infrastructure/postgres"
	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type unitOfWork struct {
	executor    database.TransactionExecutor
	idempotency *idempotency.Service
	outbox      *outboxpostgres.TransactionalOutboxRepository
	audit       *audit.Writer
}

func (u *unitOfWork) ClaimIdempotency(ctx context.Context, claim idempotency.Claim) (idempotency.ClaimResult, error) {
	if u == nil || u.idempotency == nil {
		return idempotency.ClaimResult{}, database.ErrDependencyMissing
	}
	return u.idempotency.Claim(ctx, claim)
}

func (u *unitOfWork) CompleteIdempotency(ctx context.Context, completion idempotency.Completion) error {
	if u == nil || u.idempotency == nil {
		return database.ErrDependencyMissing
	}
	return u.idempotency.Complete(ctx, completion)
}

func (u *unitOfWork) RevalidateAdmin(ctx context.Context, actor outboxreplay.Actor) error {
	if u == nil || u.executor == nil {
		return database.ErrDependencyMissing
	}
	var allowed bool
	err := u.executor.QueryRow(ctx, `
		SELECT true
		FROM users AS actor
		JOIN user_sessions AS session
		  ON session.id = $2 AND session.user_id = actor.id
		JOIN user_roles AS assignment
		  ON assignment.user_id = actor.id AND assignment.revoked_at IS NULL
		JOIN roles AS role ON role.id = assignment.role_id
		WHERE actor.id = $1
		  AND actor.status = 'ACTIVE'
		  AND session.revoked_at IS NULL
		  AND session.expires_at > statement_timestamp()
		  AND session.idle_expires_at > statement_timestamp()
		  AND session.absolute_expires_at > statement_timestamp()
		  AND session.mfa_level IN ('TOTP', 'WEBAUTHN', 'RECOVERY')
		  AND role.code = 'ADMIN'
		FOR UPDATE OF actor, session, assignment`, actor.UserID, actor.SessionID).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return &apperror.Typed{Category: apperror.ErrForbidden}
	}
	if err != nil {
		return err
	}
	if !allowed {
		return &apperror.Typed{Category: apperror.ErrForbidden}
	}
	return nil
}

func (u *unitOfWork) ReplayDeadLetter(ctx context.Context, id uuid.UUID, at time.Time) error {
	if u == nil || u.outbox == nil {
		return database.ErrDependencyMissing
	}
	return u.outbox.ReplayDeadLetter(ctx, id, at)
}

func (u *unitOfWork) AppendAudit(ctx context.Context, event audit.Event) error {
	if u == nil || u.audit == nil {
		return database.ErrDependencyMissing
	}
	return u.audit.Append(ctx, event)
}

type Transactor struct{ runner *database.TransactionRunner[outboxreplay.UnitOfWork] }

func NewTransactor(pool *database.Pool, observer database.RollbackErrorObserver) (*Transactor, error) {
	runner, err := database.NewTransactionRunner(
		pool,
		func(executor database.TransactionExecutor) (outboxreplay.UnitOfWork, error) {
			idempotencyRepository, err := outboxpostgres.NewTransactionalIdempotencyRepository(executor)
			if err != nil {
				return nil, err
			}
			idempotencyService, err := idempotency.NewService(idempotencyRepository)
			if err != nil {
				return nil, err
			}
			outboxRepository, err := outboxpostgres.NewTransactionalOutboxRepository(executor)
			if err != nil {
				return nil, err
			}
			auditRepository, err := auditpostgres.NewTransactionalAuditRepository(executor)
			if err != nil {
				return nil, err
			}
			writer, err := audit.NewWriter(auditRepository, outboxreplay.AuditMetadataPolicy())
			if err != nil {
				return nil, err
			}
			return &unitOfWork{executor: executor, idempotency: idempotencyService, outbox: outboxRepository, audit: writer}, nil
		},
		observer,
	)
	if err != nil {
		return nil, err
	}
	return &Transactor{runner: runner}, nil
}

func (t *Transactor) WithinTransaction(ctx context.Context, fn func(context.Context, outboxreplay.UnitOfWork) error) error {
	if t == nil || t.runner == nil {
		return database.ErrDependencyMissing
	}
	return t.runner.WithinTransaction(ctx, fn)
}

var _ outboxreplay.Transactor = (*Transactor)(nil)
GO

cat > backend/internal/orchestration/outboxreplay/postgres/transactor_integration_test.go <<'GO'
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/idempotency"
	"github.com/GulovM/PharmacyCRM/backend/internal/orchestration/outboxreplay"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/apperror"
	"github.com/GulovM/PharmacyCRM/backend/internal/testkit/postgrestest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestManualReplayRevalidatesAdminAndIsIdempotentIntegration(t *testing.T) {
	ctx := context.Background()
	dsn := postgrestest.DSN(t)
	rawPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rawPool.Close)

	actorID, sessionID, eventID, deniedEventID, auditID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	var adminRoleID uuid.UUID
	if err := rawPool.QueryRow(ctx, "SELECT id FROM roles WHERE code='ADMIN'").Scan(&adminRoleID); err != nil {
		t.Fatal(err)
	}
	roleAssignmentID := uuid.New()
	t.Cleanup(func() {
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM audit_events WHERE actor_user_id=$1", actorID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM idempotency_records WHERE actor_user_id=$1", actorID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM outbox_events WHERE id IN ($1,$2)", eventID, deniedEventID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM user_sessions WHERE id=$1", sessionID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM user_roles WHERE id=$1", roleAssignmentID)
		_, _ = rawPool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", actorID)
	})
	if _, err := rawPool.Exec(ctx, "INSERT INTO users(id,login,password_hash,display_name) VALUES($1,$2,'hash','Replay Administrator')", actorID, "replay-"+actorID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, "INSERT INTO user_roles(id,user_id,role_id,assigned_by_user_id) VALUES($1,$2,$3,$2)", roleAssignmentID, actorID, adminRoleID); err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour)
	if _, err := rawPool.Exec(ctx, `INSERT INTO user_sessions(
		id,user_id,refresh_token_hash,token_family_id,generation,expires_at,idle_expires_at,
		absolute_expires_at,authentication_method,mfa_level
	) VALUES($1,$2,$3,$4,1,$5,$5,$5,'PASSWORD_MFA','TOTP')`, sessionID, actorID, []byte(uuid.NewString()), uuid.New(), expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := rawPool.Exec(ctx, `INSERT INTO outbox_events(
		id,event_name,aggregate_type,aggregate_id,partition_key,deduplication_key,payload,
		status,max_attempts,occurred_at,dead_lettered_at
	) VALUES
		($1,'test.replay','test',$1,($1::uuid)::text,($1::uuid)::text,'{}','DEAD_LETTER',1,now(),now()),
		($2,'test.replay','test',$2,($2::uuid)::text,($2::uuid)::text,'{}','DEAD_LETTER',1,now(),now())`, eventID, deniedEventID); err != nil {
		t.Fatal(err)
	}

	pool, err := database.NewAPI(ctx, config.APIPostgresConfig{DSN: dsn, PoolConfig: config.PoolConfig{
		MinConnections: 1, MaxConnections: 2, AcquireTimeout: time.Second,
		MaxConnectionLife: time.Minute, MaxConnectionIdle: time.Minute, HealthCheckPeriod: time.Minute,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	transactor, err := NewTransactor(pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := outboxreplay.NewService(transactor)
	command := outboxreplay.Command{
		EventID: eventID, AuditEventID: auditID, ActorUserID: actorID, ActorSessionID: sessionID,
		Reason: "operator approved replay", IdempotencyKey: "manual-replay-" + eventID.String(),
		Fingerprint: idempotency.NewFingerprint([]byte(eventID.String() + "|operator approved replay")),
		IdempotencyExpiresAt: time.Now().Add(time.Hour), OccurredAt: time.Now(),
	}
	first, err := service.Replay(ctx, command)
	if err != nil || first.IdempotencyReplayed {
		t.Fatalf("first replay=%#v err=%v", first, err)
	}
	second, err := service.Replay(ctx, command)
	if err != nil || !second.IdempotencyReplayed {
		t.Fatalf("idempotent replay=%#v err=%v", second, err)
	}

	var status, idempotencyStatus string
	var auditCount int
	if err := rawPool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id=$1", eventID).Scan(&status); err != nil || status != "PENDING" {
		t.Fatalf("event status=%q err=%v", status, err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT count(*) FROM audit_events WHERE id=$1", auditID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT status FROM idempotency_records WHERE actor_user_id=$1 AND operation=$2 AND idempotency_key=$3", actorID, outboxreplay.Operation, command.IdempotencyKey).Scan(&idempotencyStatus); err != nil || idempotencyStatus != "COMPLETED" {
		t.Fatalf("idempotency status=%q err=%v", idempotencyStatus, err)
	}

	if _, err := rawPool.Exec(ctx, "UPDATE user_sessions SET revoked_at=now(), revoke_reason='TEST_REVOKE' WHERE id=$1", sessionID); err != nil {
		t.Fatal(err)
	}
	denied := command
	denied.EventID = deniedEventID
	denied.AuditEventID = uuid.New()
	denied.IdempotencyKey = "manual-replay-" + deniedEventID.String()
	denied.Fingerprint = idempotency.NewFingerprint([]byte(deniedEventID.String()))
	if _, err := service.Replay(ctx, denied); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("expected forbidden replay, got %v", err)
	}
	if err := rawPool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE id=$1", deniedEventID).Scan(&status); err != nil || status != "DEAD_LETTER" {
		t.Fatalf("denied event status=%q err=%v", status, err)
	}
}
GO

python3 - <<'PY'
from pathlib import Path

replacements = {
    'backend/internal/platform/config/types.go': [
        ('SupportedSchemaVersion  = 23', 'SupportedSchemaVersion  = 24'),
        ('default:"23"', 'default:"24"'),
    ],
    'backend/.env.example': [
        ('APP_MIN_SCHEMA_VERSION=23', 'APP_MIN_SCHEMA_VERSION=24'),
        ('APP_MAX_SCHEMA_VERSION=23', 'APP_MAX_SCHEMA_VERSION=24'),
    ],
    'backend/internal/platform/config/config_test.go': [
        ('AppConfig{MinSchemaVersion: 23, MaxSchemaVersion: 23}', 'AppConfig{MinSchemaVersion: 24, MaxSchemaVersion: 24}'),
    ],
    'backend/internal/platform/migration/embedded_migrations_test.go': [
        ('len(items) != 23 || items[0].Version != 1 || items[len(items)-1].Version != 23', 'len(items) != 24 || items[0].Version != 1 || items[len(items)-1].Version != 24'),
    ],
    'deploy/scripts/tests/test-e1-role-upgrade.sh': [
        ('grep -Fx "23|f|f|f|f|f|f|f|f|f|0|0"', 'grep -Fx "24|f|f|f|f|f|f|f|f|f|0|0"'),
    ],
}
for name, items in replacements.items():
    path = Path(name)
    text = path.read_text()
    for old, new in items:
        if old not in text:
            raise SystemExit(f'missing replacement in {name}: {old}')
        text = text.replace(old, new)
    path.write_text(text)

path = Path('backend/internal/platform/migration/runner_integration_test.go')
text = path.read_text()
for old, new in [
    ('len(loaded) != 23', 'len(loaded) != 24'),
    ('result.SchemaVersion != 23 || len(result.Applied) != 22 || result.Applied[0] != 2 || result.Applied[len(result.Applied)-1] != 23', 'result.SchemaVersion != 24 || len(result.Applied) != 23 || result.Applied[0] != 2 || result.Applied[len(result.Applied)-1] != 24'),
    ('replayed.SchemaVersion != 23', 'replayed.SchemaVersion != 24'),
    ('result.SchemaVersion != 23 || len(result.Applied) != 4 || result.Applied[0] != 20 || result.Applied[3] != 23', 'result.SchemaVersion != 24 || len(result.Applied) != 5 || result.Applied[0] != 20 || result.Applied[4] != 24'),
    ('replayed.SchemaVersion != 23', 'replayed.SchemaVersion != 24'),
    ('schema 23 replay=', 'schema 24 replay='),
    ('result.SchemaVersion != 23 || len(result.Applied) != 2 || result.Applied[0] != 22 || result.Applied[1] != 23', 'result.SchemaVersion != 24 || len(result.Applied) != 3 || result.Applied[0] != 22 || result.Applied[1] != 23 || result.Applied[2] != 24'),
]:
    if old in text:
        text = text.replace(old, new)
path.write_text(text)

path = Path('backend/internal/modules/reliability/infrastructure/postgres/outbox_retention_repository_integration_test.go')
text = path.read_text()
anchor = '\t_, err = worker.Exec(ctx, `DELETE FROM outbox_events WHERE aggregate_id=$1`, aggregateID)\n'
addition = '''\tfor name, query := range map[string]string{
\t\t"processed future cutoff": `SELECT public.delete_processed_outbox_events_before(now() + interval '1 day', 1000)`,
\t\t"dead-letter future cutoff": `SELECT public.delete_dead_letter_outbox_events_before(now() + interval '1 day', 1000)`,
\t} {
\t\tt.Run(name, func(t *testing.T) {
\t\t\tvar deleted int64
\t\t\terr := worker.QueryRow(ctx, query).Scan(&deleted)
\t\t\tvar postgresError *pgconn.PgError
\t\t\tif !errors.As(err, &postgresError) || postgresError.Code != "22023" {
\t\t\t\tt.Fatalf("expected guarded retention cutoff, deleted=%d err=%v", deleted, err)
\t\t\t}
\t\t})
\t}

''' + anchor
if anchor not in text:
    raise SystemExit('retention test anchor not found')
path.write_text(text.replace(anchor, addition, 1))

for path in list(Path('docs').glob('*.md')) + [Path('deploy/README.md')]:
    text = path.read_text()
    text = text.replace('schema version `23`', 'schema version `24`')
    text = text.replace('schema version 23', 'schema version 24')
    text = text.replace('schema `23`', 'schema `24`')
    text = text.replace('0 → 23', '0 → 24').replace('1 → 23', '1 → 24').replace('19 → 23', '19 → 24').replace('21 → 23', '21 → 24')
    text = text.replace('000001–000023', '000001–000024').replace('000001..000023', '000001..000024')
    path.write_text(text)
PY

gofmt -w backend/internal/orchestration/outboxreplay/service.go \
  backend/internal/orchestration/outboxreplay/postgres/transactor.go \
  backend/internal/orchestration/outboxreplay/postgres/transactor_integration_test.go
