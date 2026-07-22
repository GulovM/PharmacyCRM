package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	audit "github.com/GulovM/PharmacyCRM/backend/internal/modules/audit/application"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/database"
)

type Repository struct{ executor database.DBTX }

func NewRepository(executor database.DBTX) *Repository { return &Repository{executor: executor} }

func (r *Repository) Append(ctx context.Context, event audit.Event) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	var ipAddress any
	if event.IPAddress.IsValid() {
		ipAddress = event.IPAddress.String()
	}
	_, err = r.executor.Exec(ctx, `
		INSERT INTO audit_events (
			id, occurred_at, actor_user_id, actor_session_id, pharmacy_id,
			actor_type, action, object_type, object_id, result,
			request_id, trace_id, ip_address, user_agent, metadata
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)`,
		event.ID, event.OccurredAt, event.ActorUserID, event.ActorSessionID, event.PharmacyID,
		event.ActorType, event.Action, event.ObjectType, event.ObjectID, event.Result,
		nullIfEmpty(event.RequestID), nullIfEmpty(event.TraceID), ipAddress, nullIfEmpty(event.UserAgent), string(metadata))
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
