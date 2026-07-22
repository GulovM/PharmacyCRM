\set ON_ERROR_STOP on

-- Execute as the database owner before cmd/migrate. The psql variables make
-- identities explicit and avoid placing passwords in repository files.
CREATE ROLE pharmacycrm_api_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE pharmacycrm_worker_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE pharmacycrm_migration NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE :"api_role" LOGIN PASSWORD :'api_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT;
CREATE ROLE :"worker_role" LOGIN PASSWORD :'worker_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT;
CREATE ROLE :"migration_role" LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;

GRANT pharmacycrm_api_runtime TO :"api_role" WITH INHERIT TRUE;
GRANT pharmacycrm_worker_runtime TO :"worker_role" WITH INHERIT TRUE;
GRANT pharmacycrm_migration TO :"migration_role" WITH INHERIT TRUE;

GRANT CONNECT ON DATABASE :"database_name" TO :"api_role", :"worker_role", :"migration_role";
GRANT CREATE ON DATABASE :"database_name" TO :"migration_role";
GRANT USAGE ON SCHEMA public TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime, pharmacycrm_migration, :"api_role", :"worker_role", :"migration_role";
GRANT CREATE ON SCHEMA public TO :"migration_role";
REVOKE CREATE ON SCHEMA public FROM :"api_role", :"worker_role";

-- Tables are owned by migration_role. Runtime receives baseline SELECT/INSERT
-- through the group; migration 000013 narrows immutable tables and grants the
-- approved UPDATE set explicitly.
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public REVOKE ALL ON TABLES FROM PUBLIC;
