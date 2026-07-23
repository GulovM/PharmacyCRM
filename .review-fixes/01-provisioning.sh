#!/usr/bin/env bash
set -euo pipefail

cat > deploy/scripts/provision-postgres-roles.sql <<'SQL'
\set ON_ERROR_STOP on
-- Required psql variables: provisioning_mode=fresh|upgrade, database_name,
-- api_role, api_password, worker_role, worker_password, migration_role and
-- migration_password. Upgrade mode additionally requires legacy_runtime_role.
\if :{?provisioning_mode}
\else
\set provisioning_mode ''
\endif
\if :{?legacy_runtime_role}
\else
\set legacy_runtime_role ''
\endif

BEGIN;
CREATE TEMP TABLE pharmacycrm_role_provisioning_config (
    provisioning_mode text NOT NULL,
    database_name text NOT NULL,
    api_role text NOT NULL,
    worker_role text NOT NULL,
    migration_role text NOT NULL,
    legacy_runtime_role text NOT NULL
) ON COMMIT DROP;
INSERT INTO pharmacycrm_role_provisioning_config
VALUES (:'provisioning_mode', :'database_name', :'api_role', :'worker_role',
        :'migration_role', :'legacy_runtime_role');

\ir provision-postgres-role-helpers.sql
\ir provision-postgres-role-contract.sql
COMMIT;
SQL

cat > deploy/scripts/provision-postgres-role-helpers.sql <<'SQL'
CREATE OR REPLACE FUNCTION pg_temp.role_owns_cluster_objects(target_oid oid)
RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT EXISTS (
        SELECT 1
        FROM pg_shdepend dependency
        WHERE dependency.refclassid = 'pg_authid'::regclass
          AND dependency.refobjid = target_oid
          AND dependency.deptype = 'o'
    )
$$;

CREATE OR REPLACE FUNCTION pg_temp.assert_role_can_be_sanitized(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS oid LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
    unsafe_parent text;
    current_database_oid oid;
BEGIN
    SELECT oid INTO target_oid FROM pg_roles WHERE rolname = target_role;
    IF target_oid IS NULL THEN
        RAISE EXCEPTION 'role % does not exist', target_role;
    END IF;
    IF pg_temp.role_owns_cluster_objects(target_oid) THEN
        RAISE EXCEPTION 'role % owns cluster or database objects; reassign ownership before provisioning', target_role;
    END IF;

    SELECT oid INTO current_database_oid FROM pg_database WHERE datname = current_database();
    IF EXISTS (
        SELECT 1
        FROM pg_shdepend dependency
        WHERE dependency.refclassid = 'pg_authid'::regclass
          AND dependency.refobjid = target_oid
          AND dependency.deptype = 'a'
          AND dependency.dbid NOT IN (0, current_database_oid)
    ) OR EXISTS (
        SELECT 1
        FROM pg_database database
        CROSS JOIN LATERAL aclexplode(database.datacl) privilege
        WHERE database.datname <> target_database
          AND privilege.grantee = target_oid
    ) THEN
        RAISE EXCEPTION 'role % has privileges in another database; retire them from that database first', target_role;
    END IF;

    IF fail_on_owning_membership THEN
        SELECT parent.rolname INTO unsafe_parent
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
          AND pg_temp.role_owns_cluster_objects(parent.oid)
        ORDER BY parent.rolname
        LIMIT 1;
        IF unsafe_parent IS NOT NULL THEN
            RAISE EXCEPTION 'role % is a member of owning role %; review ownership before retirement',
                target_role, unsafe_parent;
        END IF;
    END IF;
    RETURN target_oid;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.revoke_default_acl_grants(target_role text, target_oid oid)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    item record;
    object_keyword text;
    schema_clause text;
BEGIN
    FOR item IN
        SELECT DISTINCT owner.rolname AS owner_name,
               namespace.nspname AS schema_name,
               default_acl.defaclnamespace = 0 AS global_acl,
               default_acl.defaclobjtype AS object_type
        FROM pg_default_acl default_acl
        JOIN pg_roles owner ON owner.oid = default_acl.defaclrole
        LEFT JOIN pg_namespace namespace ON namespace.oid = default_acl.defaclnamespace
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE privilege.grantee = target_oid
    LOOP
        object_keyword := CASE item.object_type
            WHEN 'r' THEN 'TABLES'
            WHEN 'S' THEN 'SEQUENCES'
            WHEN 'f' THEN 'FUNCTIONS'
            WHEN 'T' THEN 'TYPES'
            WHEN 'n' THEN 'SCHEMAS'
            ELSE NULL
        END;
        IF object_keyword IS NULL THEN
            RAISE EXCEPTION 'unsupported default ACL object type % for role %', item.object_type, target_role;
        END IF;
        schema_clause := CASE
            WHEN item.global_acl OR item.object_type = 'n' THEN ''
            ELSE format(' IN SCHEMA %I', item.schema_name)
        END;
        EXECUTE format(
            'ALTER DEFAULT PRIVILEGES FOR ROLE %I%s REVOKE ALL PRIVILEGES ON %s FROM %I',
            item.owner_name, schema_clause, object_keyword, target_role
        );
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.revoke_all_memberships(target_role text, target_oid oid)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    item record;
BEGIN
    FOR item IN
        SELECT parent.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles parent ON parent.oid = membership.roleid
        WHERE membership.member = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', item.role_name, target_role);
    END LOOP;
    FOR item IN
        SELECT member.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles member ON member.oid = membership.member
        WHERE membership.roleid = target_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', target_role, item.role_name);
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.verify_role_has_no_direct_capabilities(
    target_role text,
    target_oid oid,
    target_database text,
    allow_login boolean
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    remaining text[] := ARRAY[]::text[];
BEGIN
    IF NOT allow_login AND EXISTS (SELECT 1 FROM pg_roles WHERE oid = target_oid AND rolcanlogin) THEN
        remaining := array_append(remaining, 'login');
    END IF;
    IF NOT allow_login AND EXISTS (SELECT 1 FROM pg_authid WHERE oid = target_oid AND rolpassword IS NOT NULL) THEN
        remaining := array_append(remaining, 'password');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_default_acl default_acl
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'default_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_database database
        CROSS JOIN LATERAL aclexplode(database.datacl) privilege
        WHERE database.datname = target_database AND privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'database_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_namespace namespace
        CROSS JOIN LATERAL aclexplode(namespace.nspacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'schema_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_class relation
        CROSS JOIN LATERAL aclexplode(relation.relacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'relation_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_attribute attribute
        CROSS JOIN LATERAL aclexplode(attribute.attacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'column_acl');
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_proc procedure
        CROSS JOIN LATERAL aclexplode(procedure.proacl) privilege
        WHERE privilege.grantee = target_oid
    ) THEN
        remaining := array_append(remaining, 'function_acl');
    END IF;
    IF cardinality(remaining) > 0 THEN
        RAISE EXCEPTION 'role % retains direct capabilities: %', target_role, array_to_string(remaining, ',');
    END IF;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.sanitize_role_capabilities(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS oid LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
BEGIN
    target_oid := pg_temp.assert_role_can_be_sanitized(target_role, target_database, fail_on_owning_membership);
    PERFORM pg_temp.revoke_default_acl_grants(target_role, target_oid);
    EXECUTE format('DROP OWNED BY %I', target_role);
    PERFORM pg_temp.revoke_all_memberships(target_role, target_oid);
    PERFORM pg_temp.verify_role_has_no_direct_capabilities(target_role, target_oid, target_database, true);
    RETURN target_oid;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.retire_role(
    target_role text,
    target_database text,
    fail_on_owning_membership boolean
) RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    target_oid oid;
BEGIN
    target_oid := pg_temp.sanitize_role_capabilities(target_role, target_database, fail_on_owning_membership);
    EXECUTE format(
        'ALTER ROLE %I WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS',
        target_role
    );
    IF EXISTS (SELECT 1 FROM pg_auth_members WHERE roleid = target_oid OR member = target_oid) THEN
        RAISE EXCEPTION 'retired role % retains memberships', target_role;
    END IF;
    PERFORM pg_temp.verify_role_has_no_direct_capabilities(target_role, target_oid, target_database, false);
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.keep_only_group_member(group_role text, expected_member text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    group_oid oid;
    member record;
    parent record;
BEGIN
    SELECT oid INTO STRICT group_oid FROM pg_roles WHERE rolname = group_role;
    FOR parent IN
        SELECT role.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles role ON role.oid = membership.roleid
        WHERE membership.member = group_oid
    LOOP
        EXECUTE format('REVOKE %I FROM %I', parent.role_name, group_role);
    END LOOP;
    FOR member IN
        SELECT role.rolname AS role_name
        FROM pg_auth_members membership
        JOIN pg_roles role ON role.oid = membership.member
        WHERE membership.roleid = group_oid
          AND role.rolname <> expected_member
    LOOP
        EXECUTE format('REVOKE %I FROM %I', group_role, member.role_name);
    END LOOP;
END
$$;

CREATE OR REPLACE FUNCTION pg_temp.verify_exact_membership(member_role text, group_role text)
RETURNS void LANGUAGE plpgsql AS $$
DECLARE
    member_oid oid;
    group_oid oid;
    parent_count integer;
    child_count integer;
    group_parent_count integer;
    group_child_count integer;
BEGIN
    SELECT oid INTO STRICT member_oid FROM pg_roles WHERE rolname = member_role;
    SELECT oid INTO STRICT group_oid FROM pg_roles WHERE rolname = group_role;
    SELECT count(*) INTO parent_count FROM pg_auth_members WHERE member = member_oid;
    SELECT count(*) INTO child_count FROM pg_auth_members WHERE roleid = member_oid;
    SELECT count(*) INTO group_parent_count FROM pg_auth_members WHERE member = group_oid;
    SELECT count(*) INTO group_child_count FROM pg_auth_members WHERE roleid = group_oid;
    IF parent_count <> 1 OR child_count <> 0 OR group_parent_count <> 0 OR group_child_count <> 1 OR NOT EXISTS (
        SELECT 1 FROM pg_auth_members WHERE roleid = group_oid AND member = member_oid
    ) THEN
        RAISE EXCEPTION 'roles % and % do not have the exact expected membership edge', member_role, group_role;
    END IF;
END
$$;
SQL

cat > deploy/scripts/provision-postgres-role-contract.sql <<'SQL'
DO $$
DECLARE
    config pharmacycrm_role_provisioning_config%ROWTYPE;
    reserved text;
    declared_version bigint;
BEGIN
    SELECT * INTO STRICT config FROM pharmacycrm_role_provisioning_config;
    IF config.provisioning_mode NOT IN ('fresh', 'upgrade') THEN
        RAISE EXCEPTION 'provisioning_mode must be explicitly set to fresh or upgrade';
    END IF;
    IF btrim(config.database_name) = '' OR btrim(config.api_role) = ''
       OR btrim(config.worker_role) = '' OR btrim(config.migration_role) = '' THEN
        RAISE EXCEPTION 'database and service role names must not be empty';
    END IF;
    IF config.database_name <> current_database() THEN
        RAISE EXCEPTION 'database_name % does not match current database %', config.database_name, current_database();
    END IF;
    IF config.api_role = current_user OR config.worker_role = current_user
       OR config.migration_role = current_user OR config.legacy_runtime_role = current_user THEN
        RAISE EXCEPTION 'provisioning principal must not be a managed service or legacy role';
    END IF;
    IF config.api_role = config.worker_role OR config.api_role = config.migration_role
       OR config.worker_role = config.migration_role THEN
        RAISE EXCEPTION 'API, worker and migration login roles must be distinct';
    END IF;
    FOREACH reserved IN ARRAY ARRAY[
        'pharmacycrm_api_runtime', 'pharmacycrm_worker_runtime',
        'pharmacycrm_migration', 'pharmacycrm_runtime'
    ] LOOP
        IF config.api_role = reserved OR config.worker_role = reserved OR config.migration_role = reserved THEN
            RAISE EXCEPTION 'service login role % collides with reserved role', reserved;
        END IF;
    END LOOP;

    IF to_regclass('public.pharmacycrm_schema_metadata') IS NOT NULL THEN
        SELECT schema_version INTO STRICT declared_version
        FROM pharmacycrm_schema_metadata WHERE singleton;
    END IF;
    IF config.provisioning_mode = 'fresh' THEN
        IF btrim(config.legacy_runtime_role) <> '' THEN
            RAISE EXCEPTION 'legacy_runtime_role is valid only in upgrade mode';
        END IF;
        IF declared_version IS NOT NULL AND declared_version <> 24 THEN
            RAISE EXCEPTION 'fresh mode cannot provision existing schema version %; use upgrade mode with the exact E1 runtime role', declared_version;
        END IF;
    ELSE
        IF btrim(config.legacy_runtime_role) = '' THEN
            RAISE EXCEPTION 'legacy_runtime_role must not be empty in upgrade mode';
        END IF;
        IF declared_version IS NOT NULL AND declared_version NOT IN (1, 19, 21, 23, 24) THEN
            RAISE EXCEPTION 'schema version % is not a supported E1/E2 upgrade state', declared_version;
        END IF;
        IF config.legacy_runtime_role IN (
            config.api_role, config.worker_role, config.migration_role,
            'pharmacycrm_api_runtime', 'pharmacycrm_worker_runtime', 'pharmacycrm_migration'
        ) THEN
            RAISE EXCEPTION 'legacy runtime role % collides with an E2 service role', config.legacy_runtime_role;
        END IF;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = config.legacy_runtime_role) THEN
            RAISE EXCEPTION 'legacy runtime role % does not exist', config.legacy_runtime_role;
        END IF;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_api_runtime') THEN
        CREATE ROLE pharmacycrm_api_runtime NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_worker_runtime') THEN
        CREATE ROLE pharmacycrm_worker_runtime NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_migration') THEN
        CREATE ROLE pharmacycrm_migration NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pharmacycrm_runtime') THEN
        CREATE ROLE pharmacycrm_runtime NOLOGIN;
    END IF;

    IF config.provisioning_mode = 'upgrade' THEN
        PERFORM pg_temp.retire_role(config.legacy_runtime_role, config.database_name, true);
    END IF;
    PERFORM pg_temp.retire_role('pharmacycrm_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_api_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_worker_runtime', config.database_name, false);
    PERFORM pg_temp.retire_role('pharmacycrm_migration', config.database_name, false);
END
$$;

SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'api_role', :'api_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'api_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'worker_role', :'worker_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'worker_role') \gexec
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'migration_role', :'migration_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'migration_role') \gexec

SELECT pg_temp.sanitize_role_capabilities(:'api_role', :'database_name', false);
SELECT pg_temp.sanitize_role_capabilities(:'worker_role', :'database_name', false);
SELECT pg_temp.sanitize_role_capabilities(:'migration_role', :'database_name', false);

ALTER ROLE pharmacycrm_api_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_worker_runtime WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE pharmacycrm_migration WITH NOLOGIN PASSWORD NULL NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"api_role" WITH LOGIN PASSWORD :'api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"worker_role" WITH LOGIN PASSWORD :'worker_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT NOREPLICATION NOBYPASSRLS;
ALTER ROLE :"migration_role" WITH LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

SELECT pg_temp.keep_only_group_member('pharmacycrm_api_runtime', :'api_role');
SELECT pg_temp.keep_only_group_member('pharmacycrm_worker_runtime', :'worker_role');
SELECT pg_temp.keep_only_group_member('pharmacycrm_migration', :'migration_role');
GRANT pharmacycrm_api_runtime TO :"api_role" WITH INHERIT TRUE;
GRANT pharmacycrm_worker_runtime TO :"worker_role" WITH INHERIT TRUE;
GRANT pharmacycrm_migration TO :"migration_role" WITH INHERIT TRUE;

GRANT CONNECT ON DATABASE :"database_name" TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime;
GRANT USAGE ON SCHEMA public TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime, pharmacycrm_migration;
GRANT CONNECT, CREATE ON DATABASE :"database_name" TO :"migration_role";
GRANT USAGE, CREATE ON SCHEMA public TO :"migration_role";
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;

DO $$
BEGIN
    IF to_regclass('public.users') IS NULL THEN
        RETURN;
    END IF;

    GRANT SELECT ON pharmacycrm_schema_metadata, roles, pharmacies, pharmacy_assignments,
        products, product_presentations, product_barcodes, product_requests, import_jobs,
        import_rows, pharmacy_products, inventory_operations, receipts, receipt_items,
        stock_lots, inventory_movements, write_offs, write_off_items, inventory_adjustments,
        inventory_adjustment_items, sales, sale_items, sale_item_allocations, sale_returns,
        sale_return_items, sale_return_item_allocations, idempotency_records, outbox_events,
        alerts, public_availability_projection TO pharmacycrm_api_runtime;
    GRANT SELECT (id, login, display_name, phone, status, failed_login_attempts,
        locked_until, password_changed_at, last_login_at, version, created_at, updated_at,
        blocked_at, archived_at) ON users TO pharmacycrm_api_runtime;
    GRANT SELECT (id, user_id, token_family_id, generation, user_agent, ip_address,
        created_at, last_used_at, expires_at, idle_expires_at, absolute_expires_at,
        authentication_method, mfa_level, revoked_at, revoke_reason)
        ON user_sessions TO pharmacycrm_api_runtime;
    GRANT INSERT, UPDATE (last_used_at, revoked_at, revoke_reason)
        ON user_sessions TO pharmacycrm_api_runtime;
    GRANT INSERT ON users, user_roles, user_sessions, idempotency_records,
        outbox_events, audit_events TO pharmacycrm_api_runtime;
    GRANT UPDATE (status, response_status, response_body, resource_type,
        resource_id, completed_at, expires_at)
        ON idempotency_records TO pharmacycrm_api_runtime;

    GRANT SELECT ON pharmacycrm_schema_metadata, outbox_events TO pharmacycrm_worker_runtime;
    GRANT UPDATE (status, attempt_count, available_at, lease_token, lease_generation,
        leased_by, lease_expires_at, last_error_code, last_error_at,
        processed_at, dead_lettered_at)
        ON outbox_events TO pharmacycrm_worker_runtime;

    IF to_regprocedure('public.replay_dead_letter_outbox_event(uuid,timestamptz)') IS NOT NULL THEN
        GRANT EXECUTE ON FUNCTION public.replay_dead_letter_outbox_event(uuid,timestamptz)
            TO pharmacycrm_api_runtime;
    END IF;
    IF to_regprocedure('public.delete_processed_outbox_events_before(timestamptz,integer)') IS NOT NULL THEN
        GRANT EXECUTE ON FUNCTION public.delete_processed_outbox_events_before(timestamptz,integer),
            public.delete_dead_letter_outbox_events_before(timestamptz,integer)
            TO pharmacycrm_worker_runtime;
    END IF;
END
$$;

SELECT pg_temp.verify_exact_membership(:'api_role', 'pharmacycrm_api_runtime');
SELECT pg_temp.verify_exact_membership(:'worker_role', 'pharmacycrm_worker_runtime');
SELECT pg_temp.verify_exact_membership(:'migration_role', 'pharmacycrm_migration');
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'api_role', (SELECT oid FROM pg_roles WHERE rolname = :'api_role'), :'database_name', true
);
SELECT pg_temp.verify_role_has_no_direct_capabilities(
    :'worker_role', (SELECT oid FROM pg_roles WHERE rolname = :'worker_role'), :'database_name', true
);
SQL

cat > scripts/check-source-size.sh <<'SH'
#!/usr/bin/env bash
set -euo pipefail
root_dir="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$root_dir"
violations=()
while IFS= read -r -d '' source; do
  grep -Eq 'Code generated .* DO NOT EDIT\.' "$source" && continue
  lines=$(wc -l < "$source")
  (( lines > 400 )) && violations+=("$source"$'\t'"$lines")
done < <(find backend frontend deploy scripts \
  \( -path '*/node_modules/*' -o -path '*/vendor/*' -o -path '*/dist/*' -o -path '*/build/*' -o -path '*/coverage/*' -o -path '*/tmp/*' -o -path '*/generated/*' -o -path '*/frontend/src/shared/api/generated/*' \) -prune -o \
  -type f \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' -o -name '*.sql' -o -name '*.sh' -o -name '*.ps1' \) -print0)
if (( ${#violations[@]} > 0 )); then
  printf 'architecture check: handwritten source exceeds 400 lines:\n' >&2
  printf '%s\n' "${violations[@]}" | LC_ALL=C sort | while IFS=$'\t' read -r source lines; do
    printf '%s — %s\n' "$source" "$lines" >&2
  done
  exit 1
fi
SH

cat > scripts/check-source-size.ps1 <<'PS1'
param([string] $Root = (Split-Path -Parent $PSScriptRoot))
$ErrorActionPreference = 'Stop'
$Root = [IO.Path]::GetFullPath($Root).TrimEnd([char]92, [char]47, [char]58)
$sourceExtensions = @('.go', '.ts', '.tsx', '.js', '.jsx', '.sql', '.sh', '.ps1')
$ignoredSegments = @('node_modules', 'vendor', 'dist', 'build', 'coverage', 'tmp', 'generated')
$violations = @()
$roots = @('backend', 'frontend', 'deploy', 'scripts') | ForEach-Object { Join-Path $Root $_ }
Get-ChildItem -Path $roots -Recurse -File | ForEach-Object {
    $relativePath = [IO.Path]::GetRelativePath($Root, $_.FullName) -replace '\\', '/'
    $relativeSegments = $relativePath -split '/'
    $isIgnored = $relativeSegments | Where-Object { $_ -in $ignoredSegments }
    $isGeneratedAPI = $relativePath -like 'frontend/src/shared/api/generated/*'
    if ($_.Extension -in $sourceExtensions -and -not $isIgnored -and -not $isGeneratedAPI) {
        if (-not (Select-String -LiteralPath $_.FullName -Pattern 'Code generated .* DO NOT EDIT\.' -Quiet)) {
            $lines = (Get-Content -LiteralPath $_.FullName | Measure-Object -Line).Lines
            if ($lines -gt 400) { $violations += [PSCustomObject]@{ Path = $relativePath; Lines = $lines } }
        }
    }
}
if ($violations.Count -gt 0) {
    [Console]::Error.WriteLine('architecture check: handwritten source exceeds 400 lines:')
    foreach ($violation in $violations | Sort-Object Path) {
        [Console]::Error.WriteLine(('{0} {1} {2}' -f $violation.Path, [char]0x2014, $violation.Lines))
    }
    exit 1
}
PS1

python3 - <<'PY'
from pathlib import Path

path = Path('scripts/tests/check-source-size.sh')
text = path.read_text()
text = text.replace('mkdir -p "$root/backend/internal" "$root/frontend/src" "$root/scripts"', 'mkdir -p "$root/backend/internal" "$root/frontend/src" "$root/deploy/scripts" "$root/scripts"')
text = text.replace('make_lines 450 "$root/docs/ignored.md"', 'make_lines 450 "$root/docs/ignored.md"\nmake_lines 450 "$root/deploy/scripts/provision.sql"')
text = text.replace("grep -q 'frontend/src/b.tsx — 450' \"$root/out\"", "grep -q 'deploy/scripts/provision.sql — 450' \"$root/out\"\ngrep -q 'frontend/src/b.tsx — 450' \"$root/out\"")
text = text.replace('mkdir -p "$extensions_root/backend" "$extensions_root/frontend" "$extensions_root/scripts"', 'mkdir -p "$extensions_root/backend" "$extensions_root/frontend" "$extensions_root/deploy" "$extensions_root/scripts"')
path.write_text(text)

path = Path('scripts/tests/check-source-size.ps1')
text = path.read_text()
text = text.replace("'backend', 'frontend', 'scripts'", "'backend', 'frontend', 'deploy', 'scripts'")
if 'deploy/oversized.sql' not in text:
    text = text.replace("New-FixtureFile 'frontend/src/oversized.tsx' 450", "New-FixtureFile 'frontend/src/oversized.tsx' 450\nNew-FixtureFile 'deploy/oversized.sql' 450")
    text = text.replace("Assert-Contains 'frontend/src/oversized.tsx", "Assert-Contains 'deploy/oversized.sql — 450'\nAssert-Contains 'frontend/src/oversized.tsx")
path.write_text(text)

path = Path('deploy/scripts/tests/test-e1-role-upgrade.sh')
text = path.read_text()
needle = "SELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO %I', :'migration_role', :'api_role') \\gexec\n"
addition = needle + "SELECT format('GRANT pg_read_all_data TO %I', 'pharmacycrm_worker_runtime') \\gexec\nGRANT SELECT (password_hash) ON users TO pharmacycrm_worker_runtime;\nSELECT format('GRANT CREATE ON DATABASE %I TO %I', current_database(), 'pharmacycrm_api_runtime') \\gexec\nSELECT format('ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public GRANT SELECT ON TABLES TO %I', :'migration_role', 'pharmacycrm_api_runtime') \\gexec\nSELECT format('GRANT %I TO %I', :'polluted_parent', 'pharmacycrm_api_runtime') \\gexec\n"
if needle not in text:
    raise SystemExit('pollution insertion anchor not found')
text = text.replace(needle, addition, 1)
anchor = "SQL\n\nif psql \"$legacy_dsn\""
check = "SQL\n\npsql \"$admin_database_dsn\" -X -At -v ON_ERROR_STOP=1 <<'SQL' | grep -Fx \"f|f|0|0|0\"\nSELECT\n    has_column_privilege('pharmacycrm_worker_runtime', 'users', 'password_hash', 'SELECT'),\n    has_database_privilege('pharmacycrm_api_runtime', current_database(), 'CREATE'),\n    (SELECT count(*) FROM pg_default_acl acl CROSS JOIN LATERAL aclexplode(acl.defaclacl) privilege WHERE privilege.grantee='pharmacycrm_api_runtime'::regrole),\n    (SELECT count(*) FROM pg_auth_members WHERE member='pharmacycrm_api_runtime'::regrole),\n    (SELECT count(*) FROM pg_auth_members WHERE roleid='pharmacycrm_api_runtime'::regrole AND member<>:'api_role'::regrole);\nSQL\n\nif psql \"$legacy_dsn\""
if anchor not in text:
    raise SystemExit('group verification anchor not found')
text = text.replace(anchor, check, 1)
path.write_text(text)
PY

chmod +x scripts/check-source-size.sh
