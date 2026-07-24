-- E2-FIX-025: validate and prove the immutable session-security contract.
-- E2-FIX-032 Verification query: SELECT (SELECT count(*) = 8 FROM (VALUES ('generation','bigint',true),('idle_expires_at','timestamp with time zone',true),('absolute_expires_at','timestamp with time zone',true),('authentication_method','character varying(30)',true),('mfa_level','character varying(30)',true),('rotated_from_user_id','uuid',false),('rotated_from_token_family_id','uuid',false),('rotated_from_generation','bigint',false)) required(name,expected_type,required_not_null) JOIN pg_attribute attribute_def ON attribute_def.attrelid = 'public.user_sessions'::regclass AND attribute_def.attname = required.name AND attribute_def.attnum > 0 AND NOT attribute_def.attisdropped WHERE format_type(attribute_def.atttypid, attribute_def.atttypmod) = required.expected_type AND attribute_def.attnotnull = required.required_not_null) AND (SELECT count(*) = 10 FROM pg_constraint constraint_def WHERE constraint_def.conrelid = 'public.user_sessions'::regclass AND constraint_def.contype = 'c' AND constraint_def.convalidated AND ((constraint_def.conname = 'chk_session_generation' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%generation>0%') OR (constraint_def.conname = 'chk_session_idle_expiration' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%idle_expires_at>created_at%') OR (constraint_def.conname = 'chk_session_absolute_expiration' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%absolute_expires_at>created_at%') OR (constraint_def.conname = 'chk_session_expiry_order' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%idle_expires_at<=absolute_expires_at%') OR (constraint_def.conname = 'chk_session_effective_expiration' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%expires_at=LEASTidle_expires_at,absolute_expires_at%') OR (constraint_def.conname = 'chk_session_authentication_method' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%authentication_method%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%PASSWORD%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%PASSWORD_MFA%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%RECOVERY%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%SYSTEM%') OR (constraint_def.conname = 'chk_session_mfa_level' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%mfa_level%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%NONE%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%TOTP%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%WEBAUTHN%' AND pg_get_expr(constraint_def.conbin, constraint_def.conrelid) LIKE '%RECOVERY%') OR (constraint_def.conname = 'chk_session_rotation_generation' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%generation=1%rotated_from_session_idISNULL%rotated_from_user_idISNULL%rotated_from_token_family_idISNULL%rotated_from_generationISNULL%' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%generation>1%rotated_from_session_idISNOTNULL%rotated_from_user_idISNOTNULL%rotated_from_token_family_idISNOTNULL%rotated_from_generation=generation-1%') OR (constraint_def.conname = 'chk_session_rotation_owner' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%rotated_from_user_idISNULLORrotated_from_user_id=user_id%') OR (constraint_def.conname = 'chk_session_rotation_family' AND regexp_replace(pg_get_expr(constraint_def.conbin, constraint_def.conrelid), '[[:space:]()]', '', 'g') LIKE '%rotated_from_token_family_idISNULLORrotated_from_token_family_id=token_family_id%'))) AND EXISTS (SELECT 1 FROM pg_constraint foreign_key WHERE foreign_key.conrelid = 'public.user_sessions'::regclass AND foreign_key.confrelid = 'public.user_sessions'::regclass AND foreign_key.conname = 'fk_session_rotation_ownership' AND foreign_key.contype = 'f' AND foreign_key.convalidated AND foreign_key.confdeltype = 'r' AND ARRAY(SELECT attribute_def.attname::text FROM unnest(foreign_key.conkey) WITH ORDINALITY key_column(attnum,position) JOIN pg_attribute attribute_def ON attribute_def.attrelid = foreign_key.conrelid AND attribute_def.attnum = key_column.attnum ORDER BY key_column.position) = ARRAY['rotated_from_session_id','rotated_from_user_id','rotated_from_token_family_id','rotated_from_generation'] AND ARRAY(SELECT attribute_def.attname::text FROM unnest(foreign_key.confkey) WITH ORDINALITY key_column(attnum,position) JOIN pg_attribute attribute_def ON attribute_def.attrelid = foreign_key.confrelid AND attribute_def.attnum = key_column.attnum ORDER BY key_column.position) = ARRAY['id','user_id','token_family_id','generation']) AND EXISTS (SELECT 1 FROM pg_constraint unique_constraint WHERE unique_constraint.conrelid = 'public.user_sessions'::regclass AND unique_constraint.conname = 'uq_session_family_generation' AND unique_constraint.contype = 'u' AND ARRAY(SELECT attribute_def.attname::text FROM unnest(unique_constraint.conkey) WITH ORDINALITY key_column(attnum,position) JOIN pg_attribute attribute_def ON attribute_def.attrelid = unique_constraint.conrelid AND attribute_def.attnum = key_column.attnum ORDER BY key_column.position) = ARRAY['token_family_id','generation']) AND EXISTS (SELECT 1 FROM pg_index index_def JOIN pg_class index_relation ON index_relation.oid = index_def.indexrelid WHERE index_def.indrelid = 'public.user_sessions'::regclass AND index_relation.relname = 'uq_user_session_rotated_from' AND index_def.indisunique AND index_def.indpred IS NOT NULL AND ARRAY(SELECT attribute_def.attname::text FROM unnest(index_def.indkey) WITH ORDINALITY key_column(attnum,position) JOIN pg_attribute attribute_def ON attribute_def.attrelid = index_def.indrelid AND attribute_def.attnum = key_column.attnum ORDER BY key_column.position) = ARRAY['rotated_from_session_id'] AND regexp_replace(pg_get_expr(index_def.indpred, index_def.indrelid), '[[:space:]()]', '', 'g') = 'rotated_from_session_idISNOTNULL');

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
