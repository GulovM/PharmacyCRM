-- E2-FIX-015: make refresh-token rotation ownership and expiry explicit.
-- Verification query: SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_session_generation') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_session_rotation_ownership') AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_session_effective_expiration');
-- Lock/rewrite assessment: updates existing development/test rows before adding validated constraints; schedule outside peak traffic for populated databases.
-- Compatibility: expires_at remains the effective expiry alias and equals LEAST(idle_expires_at, absolute_expires_at).
-- Forward-fix policy: published migrations remain immutable; corrections use a new forward migration.

ALTER TABLE user_sessions
    ADD COLUMN generation bigint,
    ADD COLUMN idle_expires_at timestamptz,
    ADD COLUMN absolute_expires_at timestamptz,
    ADD COLUMN authentication_method varchar(30),
    ADD COLUMN mfa_level varchar(30),
    ADD COLUMN rotated_from_user_id uuid,
    ADD COLUMN rotated_from_token_family_id uuid,
    ADD COLUMN rotated_from_generation bigint;

WITH numbered AS (
    SELECT id, row_number() OVER (PARTITION BY token_family_id ORDER BY created_at, id) AS generation
    FROM user_sessions
)
UPDATE user_sessions AS session
SET generation = numbered.generation,
    idle_expires_at = session.expires_at,
    absolute_expires_at = session.expires_at,
    authentication_method = 'PASSWORD',
    mfa_level = 'NONE'
FROM numbered
WHERE session.id = numbered.id;

UPDATE user_sessions AS session
SET rotated_from_user_id = previous.user_id,
    rotated_from_token_family_id = previous.token_family_id,
    rotated_from_generation = previous.generation
FROM user_sessions AS previous
WHERE session.rotated_from_session_id = previous.id;

ALTER TABLE user_sessions
    ALTER COLUMN generation SET NOT NULL,
    ALTER COLUMN idle_expires_at SET NOT NULL,
    ALTER COLUMN absolute_expires_at SET NOT NULL,
    ALTER COLUMN authentication_method SET NOT NULL,
    ALTER COLUMN mfa_level SET NOT NULL,
    ADD CONSTRAINT chk_session_generation CHECK (generation > 0),
    ADD CONSTRAINT chk_session_idle_expiration CHECK (idle_expires_at > created_at),
    ADD CONSTRAINT chk_session_absolute_expiration CHECK (absolute_expires_at > created_at),
    ADD CONSTRAINT chk_session_expiry_order CHECK (idle_expires_at <= absolute_expires_at),
    ADD CONSTRAINT chk_session_effective_expiration CHECK (expires_at = LEAST(idle_expires_at, absolute_expires_at)),
    ADD CONSTRAINT chk_session_authentication_method CHECK (authentication_method IN ('PASSWORD', 'PASSWORD_MFA', 'RECOVERY', 'SYSTEM')),
    ADD CONSTRAINT chk_session_mfa_level CHECK (mfa_level IN ('NONE', 'TOTP', 'WEBAUTHN', 'RECOVERY')),
    ADD CONSTRAINT chk_session_rotation_generation CHECK (
        (generation = 1 AND rotated_from_session_id IS NULL AND rotated_from_user_id IS NULL AND rotated_from_token_family_id IS NULL AND rotated_from_generation IS NULL)
        OR (generation > 1 AND rotated_from_session_id IS NOT NULL AND rotated_from_user_id IS NOT NULL AND rotated_from_token_family_id IS NOT NULL AND rotated_from_generation = generation - 1)
    ),
    ADD CONSTRAINT chk_session_rotation_owner CHECK (rotated_from_user_id IS NULL OR rotated_from_user_id = user_id),
    ADD CONSTRAINT chk_session_rotation_family CHECK (rotated_from_token_family_id IS NULL OR rotated_from_token_family_id = token_family_id),
    ADD CONSTRAINT uq_session_family_generation UNIQUE (token_family_id, generation),
    ADD CONSTRAINT uq_session_rotation_ownership UNIQUE (id, user_id, token_family_id, generation),
    ADD CONSTRAINT fk_session_rotation_ownership FOREIGN KEY (rotated_from_session_id, rotated_from_user_id, rotated_from_token_family_id, rotated_from_generation)
        REFERENCES user_sessions (id, user_id, token_family_id, generation) ON DELETE RESTRICT;
