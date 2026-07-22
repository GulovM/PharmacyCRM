-- E2-DB-001: identity.
-- Verification query: SELECT to_regclass('public.users') IS NOT NULL AND to_regclass('public.user_sessions') IS NOT NULL AND to_regclass('public.uq_users_login_active') IS NOT NULL AND to_regclass('public.uq_user_single_active_role') IS NOT NULL AND EXISTS (SELECT 1 FROM pg_constraint WHERE conname='chk_session_expiration' AND convalidated);
-- Lock/rewrite assessment: new baseline objects only; no existing-row rewrite.
-- Compatibility: additive baseline; application traffic starts after the complete baseline.
-- Forward-fix policy: destructive down migrations are prohibited.

-- Identity
-- =====================================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login varchar(150) NOT NULL CHECK (btrim(login) <> ''),
    password_hash text NOT NULL CHECK (btrim(password_hash) <> ''),
    display_name varchar(255) NOT NULL CHECK (btrim(display_name) <> ''),
    phone varchar(50),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')),
    failed_login_attempts integer NOT NULL DEFAULT 0
        CHECK (failed_login_attempts >= 0),
    locked_until timestamptz,
    password_changed_at timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    blocked_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT chk_user_status_timestamps CHECK (
        (status = 'ACTIVE' AND archived_at IS NULL)
        OR (status = 'BLOCKED' AND blocked_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'ARCHIVED' AND archived_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_users_login_active
ON users (lower(btrim(login)))
WHERE status <> 'ARCHIVED';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(50) NOT NULL UNIQUE
        CHECK (code IN ('CLIENT', 'PHARMACIST', 'ADMIN')),
    name varchar(100) NOT NULL CHECK (btrim(name) <> ''),
    description text,
    is_system boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    revoked_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    revoked_at timestamptz,
    revoke_reason text,
    CONSTRAINT chk_user_role_revocation CHECK (
        (revoked_at IS NULL AND revoked_by_user_id IS NULL AND revoke_reason IS NULL)
        OR (
            revoked_at IS NOT NULL
            AND revoked_at >= assigned_at
            AND revoked_by_user_id IS NOT NULL
            AND btrim(revoke_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_user_single_active_role
ON user_roles (user_id)
WHERE revoked_at IS NULL;

CREATE INDEX idx_user_roles_history
ON user_roles (user_id, assigned_at DESC, id DESC);

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    refresh_token_hash bytea NOT NULL UNIQUE,
    token_family_id uuid NOT NULL,
    rotated_from_session_id uuid REFERENCES user_sessions(id) ON DELETE RESTRICT,
    user_agent text,
    ip_address inet,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason varchar(100),
    CONSTRAINT chk_session_expiration CHECK (expires_at > created_at),
    CONSTRAINT chk_session_last_used CHECK (last_used_at >= created_at),
    CONSTRAINT chk_session_rotation_self CHECK (
        rotated_from_session_id IS NULL OR rotated_from_session_id <> id
    ),
    CONSTRAINT chk_session_revocation CHECK (
        (revoked_at IS NULL AND revoke_reason IS NULL)
        OR (
            revoked_at IS NOT NULL
            AND revoked_at >= created_at
            AND btrim(revoke_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_user_session_rotated_from
ON user_sessions (rotated_from_session_id)
WHERE rotated_from_session_id IS NOT NULL;

CREATE INDEX idx_user_sessions_user_active
ON user_sessions (user_id, expires_at DESC, id DESC)
WHERE revoked_at IS NULL;

CREATE INDEX idx_user_sessions_family
ON user_sessions (token_family_id, created_at, id);

-- =====================================================================
