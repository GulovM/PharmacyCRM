#!/usr/bin/env bash
set -Eeuo pipefail

if [[ -z "${POSTGRES_ADMIN_TEST_DSN:-}" ]]; then
  if [[ "${CI_INTEGRATION_REQUIRED:-}" == "true" ]]; then
    echo "POSTGRES_ADMIN_TEST_DSN is required by the CI integration gate" >&2
    exit 1
  fi
  echo "POSTGRES_ADMIN_TEST_DSN is not set; skipping E1 role-upgrade integration test"
  exit 0
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
provisioning_script="$repo_root/deploy/scripts/provision-postgres-roles.sql"
suffix="${RANDOM}_$$_$(date +%s)"
test_database="e1_upgrade_${suffix}"
legacy_migrator="e1_migrator_${suffix}"
legacy_runtime="e1_runtime_${suffix}"
api_role="e2_api_${suffix}"
worker_role="e2_worker_${suffix}"
migration_password="migration_${suffix}"
legacy_password="legacy_${suffix}"
api_password="api_${suffix}"
worker_password="worker_${suffix}"
compatibility_password="compatibility_${suffix}"

make_dsn() {
  local base_dsn="$1" username="$2" password="$3" database="$4"
  python3 - "$base_dsn" "$username" "$password" "$database" <<'PY'
import sys
from urllib.parse import quote, urlsplit, urlunsplit

base, username, password, database = sys.argv[1:]
parts = urlsplit(base)
host = parts.hostname or "localhost"
if ":" in host and not host.startswith("["):
    host = f"[{host}]"
if parts.port:
    host = f"{host}:{parts.port}"
netloc = f"{quote(username, safe='')}:{quote(password, safe='')}@{host}"
print(urlunsplit((parts.scheme, netloc, f"/{quote(database, safe='')}", parts.query, parts.fragment)))
PY
}

admin_username="$(python3 -c 'import sys; from urllib.parse import unquote,urlsplit; print(unquote(urlsplit(sys.argv[1]).username or ""))' "$POSTGRES_ADMIN_TEST_DSN")"
admin_password="$(python3 -c 'import sys; from urllib.parse import unquote,urlsplit; print(unquote(urlsplit(sys.argv[1]).password or ""))' "$POSTGRES_ADMIN_TEST_DSN")"
admin_database_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "$admin_username" "$admin_password" "$test_database")"
migrator_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "$legacy_migrator" "$migration_password" "$test_database")"
legacy_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "$legacy_runtime" "$legacy_password" "$test_database")"
api_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "$api_role" "$api_password" "$test_database")"
worker_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "$worker_role" "$worker_password" "$test_database")"
compatibility_dsn="$(make_dsn "$POSTGRES_ADMIN_TEST_DSN" "pharmacycrm_runtime" "$compatibility_password" "$test_database")"

cleanup() {
  set +e
  psql "$POSTGRES_ADMIN_TEST_DSN" -X -v ON_ERROR_STOP=1 \
    -v database_name="$test_database" \
    -v api_role="$api_role" \
    -v worker_role="$worker_role" \
    -v migration_role="$legacy_migrator" \
    -v legacy_role="$legacy_runtime" <<'SQL' >/dev/null 2>&1
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = :'database_name' AND pid <> pg_backend_pid();
SELECT format('DROP DATABASE IF EXISTS %I', :'database_name') \gexec
SELECT format('DROP ROLE IF EXISTS %I', :'api_role') \gexec
SELECT format('DROP ROLE IF EXISTS %I', :'worker_role') \gexec
SELECT format('DROP ROLE IF EXISTS %I', :'migration_role') \gexec
SELECT format('DROP ROLE IF EXISTS %I', :'legacy_role') \gexec
DROP ROLE IF EXISTS pharmacycrm_runtime;
DROP ROLE IF EXISTS pharmacycrm_api_runtime;
DROP ROLE IF EXISTS pharmacycrm_worker_runtime;
DROP ROLE IF EXISTS pharmacycrm_migration;
SQL
}
trap cleanup EXIT

psql "$POSTGRES_ADMIN_TEST_DSN" -X -v ON_ERROR_STOP=1 \
  -v database_name="$test_database" \
  -v migration_role="$legacy_migrator" \
  -v migration_password="$migration_password" \
  -v runtime_role="$legacy_runtime" \
  -v runtime_password="$legacy_password" <<'SQL'
SELECT format(
    'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT',
    :'migration_role', :'migration_password'
) \gexec
SELECT format(
    'CREATE ROLE %I LOGIN PASSWORD %L NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT',
    :'runtime_role', :'runtime_password'
) \gexec
SELECT format('CREATE DATABASE %I OWNER %I', :'database_name', :'migration_role') \gexec
SQL

migration_one_checksum="$(sha256sum "$repo_root/backend/migrations/000001_schema_metadata.up.sql" | awk '{print $1}')"

psql "$migrator_dsn" -X -v ON_ERROR_STOP=1 \
  -v runtime_role="$legacy_runtime" \
  -v migration_checksum="$migration_one_checksum" <<'SQL'
CREATE TABLE pharmacycrm_schema_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO pharmacycrm_schema_metadata (singleton, schema_version) VALUES (true, 1);
CREATE TABLE pharmacycrm_schema_migrations (
    version bigint PRIMARY KEY,
    name text NOT NULL,
    checksum text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO pharmacycrm_schema_migrations(version, name, checksum)
VALUES (1, 'schema_metadata', :'migration_checksum');
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO :"runtime_role";
CREATE TABLE e1_runtime_visible (id bigint PRIMARY KEY, value text NOT NULL);
INSERT INTO e1_runtime_visible VALUES (1, 'visible-before-retirement');
SQL

psql "$POSTGRES_ADMIN_TEST_DSN" -X -v ON_ERROR_STOP=1 \
  -v database_name="$test_database" -v runtime_role="$legacy_runtime" <<'SQL'
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'database_name', :'runtime_role') \gexec
SQL
psql "$admin_database_dsn" -X -v ON_ERROR_STOP=1 -v runtime_role="$legacy_runtime" <<'SQL'
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'runtime_role') \gexec
SQL

psql "$legacy_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT value FROM e1_runtime_visible WHERE id=1" | grep -Fx "visible-before-retirement"

run_provisioning() {
  local legacy_role="$1"
  psql "$admin_database_dsn" -X -v ON_ERROR_STOP=1 \
    -v database_name="$test_database" \
    -v api_role="$api_role" -v api_password="$api_password" \
    -v worker_role="$worker_role" -v worker_password="$worker_password" \
    -v migration_role="$legacy_migrator" -v migration_password="$migration_password" \
    -v legacy_runtime_role="$legacy_role" \
    -f "$provisioning_script"
}

run_provisioning "$legacy_runtime"

if psql "$legacy_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT 1" >/dev/null 2>&1; then
  echo "retired E1 runtime credential still connects" >&2
  exit 1
fi

psql "$admin_database_dsn" -X -At -v ON_ERROR_STOP=1 -v legacy_role="$legacy_runtime" <<'SQL' | grep -Fx "f|t|0|0|0"
SELECT role.rolcanlogin,
       NOT EXISTS (
           SELECT 1 FROM pg_authid auth
           WHERE auth.oid=role.oid AND auth.rolpassword IS NOT NULL
       ),
       (SELECT count(*) FROM pg_auth_members WHERE roleid=role.oid OR member=role.oid),
       (SELECT count(*)
        FROM pg_default_acl default_acl
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE default_acl.defaclobjtype='r' AND privilege.grantee=role.oid),
       (SELECT count(*)
        FROM pg_class relation
        JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace
        CROSS JOIN LATERAL aclexplode(relation.relacl) privilege
        WHERE namespace.nspname='public' AND privilege.grantee=role.oid)
FROM pg_roles role
WHERE role.rolname=:'legacy_role';
SQL

(
  cd "$repo_root/backend"
  POSTGRES_MIGRATION_DSN="$migrator_dsn" go run ./cmd/migrate
)

# Immutable migrations intentionally reference pharmacycrm_runtime. Re-run the
# idempotent provisioning contract after migration to strip their compatibility
# grants without editing already-published migration files.
run_provisioning "$legacy_runtime"

if psql "$legacy_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT 1" >/dev/null 2>&1; then
  echo "retired E1 runtime credential reconnects after migrations" >&2
  exit 1
fi

psql "$admin_database_dsn" -X -At -v ON_ERROR_STOP=1 -v legacy_role="$legacy_runtime" <<'SQL' | grep -Fx "23|f|f|f|f|f|f|f|f|f|0|0"
SELECT
    (SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton),
    has_table_privilege(:'legacy_role', 'users', 'SELECT'),
    has_column_privilege(:'legacy_role', 'users', 'password_hash', 'SELECT'),
    has_table_privilege(:'legacy_role', 'user_sessions', 'SELECT'),
    has_column_privilege(:'legacy_role', 'user_sessions', 'refresh_token_hash', 'SELECT'),
    has_table_privilege(:'legacy_role', 'audit_events', 'SELECT'),
    has_table_privilege(:'legacy_role', 'pharmacycrm_schema_migrations', 'SELECT'),
    has_column_privilege(:'legacy_role', 'stock_lots', 'purchase_price_package_dirams', 'SELECT'),
    has_column_privilege(:'legacy_role', 'receipt_items', 'purchase_price_package_dirams', 'SELECT'),
    has_table_privilege(:'legacy_role', 'outbox_events', 'SELECT'),
    (SELECT count(*)
     FROM pg_namespace namespace
     CROSS JOIN LATERAL aclexplode(namespace.nspacl) privilege
     JOIN pg_roles role ON role.oid=privilege.grantee
     WHERE namespace.nspname='public' AND role.rolname=:'legacy_role'),
    (SELECT count(*)
     FROM pg_database database
     CROSS JOIN LATERAL aclexplode(database.datacl) privilege
     JOIN pg_roles role ON role.oid=privilege.grantee
     WHERE database.datname=current_database() AND role.rolname=:'legacy_role');
SQL

psql "$api_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT schema_version FROM pharmacycrm_schema_metadata WHERE singleton" | grep -Fx "23"
if psql "$api_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT MAX(version) FROM pharmacycrm_schema_migrations" >/dev/null 2>&1; then
  echo "API role unexpectedly reads migration history" >&2
  exit 1
fi
psql "$api_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT has_column_privilege(current_user, 'users', 'password_hash', 'SELECT')" | grep -Fx "f"
psql "$api_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT has_column_privilege(current_user, 'user_sessions', 'refresh_token_hash', 'SELECT')" | grep -Fx "f"

psql "$worker_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT has_table_privilege(current_user, 'outbox_events', 'SELECT') AND has_column_privilege(current_user, 'outbox_events', 'status', 'UPDATE')" | grep -Fx "t"
psql "$worker_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT has_column_privilege(current_user, 'users', 'password_hash', 'SELECT')" | grep -Fx "f"
psql "$worker_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT has_column_privilege(current_user, 'user_sessions', 'refresh_token_hash', 'SELECT')" | grep -Fx "f"
if psql "$worker_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT MAX(version) FROM pharmacycrm_schema_migrations" >/dev/null 2>&1; then
  echo "worker role unexpectedly reads migration history" >&2
  exit 1
fi

# Special E1 case: the old LOGIN itself used the compatibility name.
psql "$admin_database_dsn" -X -v ON_ERROR_STOP=1 \
  -v password="$compatibility_password" -v database_name="$test_database" \
  -v default_owner="$admin_username" <<'SQL'
ALTER ROLE pharmacycrm_runtime LOGIN PASSWORD :'password';
GRANT CONNECT ON DATABASE :"database_name" TO pharmacycrm_runtime;
GRANT USAGE ON SCHEMA public TO pharmacycrm_runtime;
GRANT SELECT ON users, user_sessions, audit_events, stock_lots, receipt_items, pharmacycrm_schema_migrations TO pharmacycrm_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE :"default_owner" IN SCHEMA public GRANT SELECT ON TABLES TO pharmacycrm_runtime;
SQL

if ! psql "$compatibility_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT 1" | grep -Fxq "1"; then
  echo "failed to reproduce pharmacycrm_runtime E1 LOGIN" >&2
  exit 1
fi
run_provisioning "pharmacycrm_runtime"
run_provisioning "pharmacycrm_runtime"

if psql "$compatibility_dsn" -X -At -v ON_ERROR_STOP=1 -c "SELECT 1" >/dev/null 2>&1; then
  echo "pharmacycrm_runtime compatibility role still connects" >&2
  exit 1
fi
psql "$admin_database_dsn" -X -At -v ON_ERROR_STOP=1 <<'SQL' | grep -Fx "f|t|0|0|0|0"
SELECT role.rolcanlogin,
       NOT EXISTS (
           SELECT 1 FROM pg_authid auth
           WHERE auth.oid=role.oid AND auth.rolpassword IS NOT NULL
       ),
       (SELECT count(*) FROM pg_auth_members WHERE roleid=role.oid OR member=role.oid),
       (SELECT count(*)
        FROM pg_default_acl default_acl
        CROSS JOIN LATERAL aclexplode(default_acl.defaclacl) privilege
        WHERE default_acl.defaclobjtype='r' AND privilege.grantee=role.oid),
       (SELECT count(*)
        FROM pg_class relation
        JOIN pg_namespace namespace ON namespace.oid=relation.relnamespace
        CROSS JOIN LATERAL aclexplode(relation.relacl) privilege
        WHERE namespace.nspname='public' AND privilege.grantee=role.oid),
       (SELECT count(*)
        FROM pg_proc procedure
        JOIN pg_namespace namespace ON namespace.oid=procedure.pronamespace
        CROSS JOIN LATERAL aclexplode(procedure.proacl) privilege
        WHERE namespace.nspname='public' AND privilege.grantee=role.oid)
FROM pg_roles role
WHERE role.rolname='pharmacycrm_runtime';
SQL

echo "E1 runtime credential retirement verified"
