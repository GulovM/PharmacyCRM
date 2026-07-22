-- E2-FIX-006: bind an audit session to the same user recorded as actor.
-- Verification query: SELECT count(*) = 2 FROM pg_constraint WHERE conname IN ('uq_user_sessions_id_user','fk_audit_actor_session_owner') AND convalidated;
-- Lock/rewrite assessment: the composite unique constraint builds a small index; FK validation scans audit_events without rewriting either table.
-- Compatibility: USER audit without a session and SYSTEM audit without actor identifiers remain valid.
-- Forward-fix policy: published migrations remain immutable; further corrections require another forward migration.

ALTER TABLE user_sessions
ADD CONSTRAINT uq_user_sessions_id_user UNIQUE (id, user_id);

ALTER TABLE audit_events
ADD CONSTRAINT fk_audit_actor_session_owner
FOREIGN KEY (actor_session_id, actor_user_id)
REFERENCES user_sessions (id, user_id)
ON DELETE RESTRICT
NOT VALID;

ALTER TABLE audit_events
VALIDATE CONSTRAINT fk_audit_actor_session_owner;
