\set ON_ERROR_STOP on

-- Execute as the database owner before cmd/migrate. The psql variables make
-- identities explicit and avoid placing passwords in repository files.
CREATE ROLE pharmacycrm_runtime NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE :"runtime_role" LOGIN PASSWORD :'runtime_password' NOSUPERUSER NOCREATEDB NOCREATEROLE INHERIT;
CREATE ROLE :"migration_role" LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;

GRANT pharmacycrm_runtime TO :"runtime_role" WITH INHERIT TRUE;

GRANT CONNECT ON DATABASE :"database_name" TO :"runtime_role", :"migration_role";
GRANT CREATE ON DATABASE :"database_name" TO :"migration_role";
GRANT USAGE ON SCHEMA public TO pharmacycrm_runtime, :"runtime_role", :"migration_role";
GRANT CREATE ON SCHEMA public TO :"migration_role";
REVOKE CREATE ON SCHEMA public FROM :"runtime_role";

-- Tables are owned by migration_role. Runtime receives baseline SELECT/INSERT
-- through the group; migration 000013 narrows immutable tables and grants the
-- approved UPDATE set explicitly.
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public GRANT SELECT, INSERT ON TABLES TO pharmacycrm_runtime;
