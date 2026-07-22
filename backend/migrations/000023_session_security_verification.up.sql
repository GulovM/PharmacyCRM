-- E2-FIX-025: validate and prove the immutable session-security contract.

ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_generation;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_idle_expiration;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_absolute_expiration;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_expiry_order;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_effective_expiration;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_authentication_method;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_mfa_level;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_rotation_generation;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_rotation_owner;
ALTER TABLE user_sessions VALIDATE CONSTRAINT chk_session_rotation_family;
ALTER TABLE user_sessions VALIDATE CONSTRAINT fk_session_rotation_ownership;

DO $$
DECLARE
    required_constraints text[] := ARRAY[
        'chk_session_generation',
        'chk_session_idle_expiration',
        'chk_session_absolute_expiration',
        'chk_session_expiry_order',
        'chk_session_effective_expiration',
        'chk_session_authentication_method',
        'chk_session_mfa_level',
        'chk_session_rotation_generation',
        'chk_session_rotation_owner',
        'chk_session_rotation_family',
        'fk_session_rotation_ownership',
        'uq_session_family_generation'
    ];
BEGIN
    IF EXISTS (
        SELECT 1
        FROM unnest(required_constraints) AS required(name)
        LEFT JOIN pg_constraint constraint_def ON constraint_def.conname = required.name
        LEFT JOIN pg_class relation ON relation.oid = constraint_def.conrelid
        LEFT JOIN pg_namespace namespace_def ON namespace_def.oid = relation.relnamespace
        WHERE relation.relname IS DISTINCT FROM 'user_sessions'
           OR namespace_def.nspname IS DISTINCT FROM 'public'
           OR NOT constraint_def.convalidated
           OR (required.name = 'fk_session_rotation_ownership' AND constraint_def.contype <> 'f')
           OR (required.name = 'uq_session_family_generation' AND constraint_def.contype <> 'u')
           OR (required.name NOT IN ('fk_session_rotation_ownership', 'uq_session_family_generation') AND constraint_def.contype <> 'c')
    ) THEN
        RAISE EXCEPTION 'user_sessions constraint verification failed';
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint constraint_def
        WHERE constraint_def.conrelid = 'public.user_sessions'::regclass
          AND constraint_def.conname = 'fk_session_rotation_ownership'
          AND constraint_def.confrelid = 'public.user_sessions'::regclass
          AND cardinality(constraint_def.conkey) = 4
          AND cardinality(constraint_def.confkey) = 4
    ) THEN
        RAISE EXCEPTION 'user_sessions composite rotation ownership foreign key verification failed';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (VALUES
            ('generation', 'bigint'),
            ('idle_expires_at', 'timestamp with time zone'),
            ('absolute_expires_at', 'timestamp with time zone'),
            ('authentication_method', 'character varying(30)'),
            ('mfa_level', 'character varying(30)'),
            ('rotated_from_user_id', 'uuid'),
            ('rotated_from_token_family_id', 'uuid'),
            ('rotated_from_generation', 'bigint')
        ) AS required(name, expected_type)
        LEFT JOIN pg_attribute attribute_def
          ON attribute_def.attrelid = 'public.user_sessions'::regclass
         AND attribute_def.attname = required.name
         AND attribute_def.attnum > 0
         AND NOT attribute_def.attisdropped
        WHERE attribute_def.attname IS NULL
           OR (required.name IN ('generation', 'idle_expires_at', 'absolute_expires_at', 'authentication_method', 'mfa_level') AND NOT attribute_def.attnotnull)
           OR format_type(attribute_def.atttypid, attribute_def.atttypmod) <> required.expected_type
    ) THEN
        RAISE EXCEPTION 'user_sessions column contract verification failed';
    END IF;
END;
$$;
