-- E2-FIX-005: make nullable lifecycle state constraints two-valued.
-- Verification query: SELECT count(*) = 5 AND bool_and(convalidated) FROM pg_constraint WHERE conname IN ('chk_user_role_revocation','chk_session_revocation','chk_assignment_end','chk_product_request_resolution','chk_alert_lifecycle');
-- Lock/rewrite assessment: constraint replacement takes brief ACCESS EXCLUSIVE locks; VALIDATE scans existing rows without a table rewrite.
-- Compatibility: the accepted states match the documented domain lifecycle; only previously invalid partial states are rejected.
-- Forward-fix policy: published migrations remain immutable; further corrections require another forward migration.

ALTER TABLE user_roles DROP CONSTRAINT chk_user_role_revocation;
ALTER TABLE user_roles ADD CONSTRAINT chk_user_role_revocation CHECK (
    (revoked_at IS NULL AND revoked_by_user_id IS NULL AND revoke_reason IS NULL)
    OR (
        revoked_at IS NOT NULL
        AND revoked_at >= assigned_at
        AND revoked_by_user_id IS NOT NULL
        AND revoke_reason IS NOT NULL
        AND btrim(revoke_reason) <> ''
    )
) NOT VALID;
ALTER TABLE user_roles VALIDATE CONSTRAINT chk_user_role_revocation;

ALTER TABLE user_sessions DROP CONSTRAINT chk_session_revocation;
ALTER TABLE user_sessions ADD CONSTRAINT chk_session_revocation CHECK (
    (revoked_at IS NULL AND revoke_reason IS NULL)
    OR (
        revoked_at IS NOT NULL
        AND revoked_at >= created_at
        AND revoke_reason IS NOT NULL
        AND btrim(revoke_reason) <> ''
    )
) NOT VALID;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_revocation;

ALTER TABLE pharmacy_assignments DROP CONSTRAINT chk_assignment_end;
ALTER TABLE pharmacy_assignments ADD CONSTRAINT chk_assignment_end CHECK (
    (ended_at IS NULL AND ended_by_user_id IS NULL AND end_reason IS NULL)
    OR (
        ended_at IS NOT NULL
        AND ended_at >= assigned_at
        AND ended_by_user_id IS NOT NULL
        AND end_reason IS NOT NULL
        AND btrim(end_reason) <> ''
    )
) NOT VALID;
ALTER TABLE pharmacy_assignments VALIDATE CONSTRAINT chk_assignment_end;

ALTER TABLE product_requests DROP CONSTRAINT chk_product_request_resolution;
ALTER TABLE product_requests ADD CONSTRAINT chk_product_request_resolution CHECK (
    (status = 'OPEN'
        AND resolved_product_presentation_id IS NULL
        AND resolved_by_user_id IS NULL
        AND resolved_at IS NULL
        AND resolution_note IS NULL)
    OR (status <> 'OPEN'
        AND resolved_by_user_id IS NOT NULL
        AND resolved_at IS NOT NULL
        AND resolution_note IS NOT NULL
        AND btrim(resolution_note) <> '')
) NOT VALID;
ALTER TABLE product_requests VALIDATE CONSTRAINT chk_product_request_resolution;

ALTER TABLE alerts DROP CONSTRAINT chk_alert_acknowledgement;
ALTER TABLE alerts DROP CONSTRAINT chk_alert_resolution;
ALTER TABLE alerts ADD CONSTRAINT chk_alert_lifecycle CHECK (
    (status = 'ACTIVE'
        AND acknowledged_by_user_id IS NULL
        AND acknowledged_at IS NULL
        AND resolved_by_user_id IS NULL
        AND resolved_at IS NULL)
    OR (status = 'ACKNOWLEDGED'
        AND acknowledged_by_user_id IS NOT NULL
        AND acknowledged_at IS NOT NULL
        AND resolved_by_user_id IS NULL
        AND resolved_at IS NULL)
    OR (status = 'RESOLVED'
        AND resolved_by_user_id IS NOT NULL
        AND resolved_at IS NOT NULL
        AND (
            (acknowledged_by_user_id IS NULL AND acknowledged_at IS NULL)
            OR (acknowledged_by_user_id IS NOT NULL AND acknowledged_at IS NOT NULL)
        ))
) NOT VALID;
ALTER TABLE alerts VALIDATE CONSTRAINT chk_alert_lifecycle;
