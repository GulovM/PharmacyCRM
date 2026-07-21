\set ON_ERROR_STOP on

-- Execute as the database owner before cmd/migrate. The psql variables make
-- identities explicit and avoid placing passwords in repository files.
CREATE ROLE :"runtime_role" LOGIN PASSWORD :'runtime_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
CREATE ROLE :"migration_role" LOGIN PASSWORD :'migration_password' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;

GRANT CONNECT ON DATABASE :"database_name" TO :"runtime_role", :"migration_role";
GRANT USAGE ON SCHEMA public TO :"runtime_role", :"migration_role";
GRANT CREATE ON SCHEMA public TO :"migration_role";
REVOKE CREATE ON SCHEMA public FROM :"runtime_role";

-- Tables are owned by migration_role. Runtime receives read-only access to
-- current and future migration-created tables, including schema metadata.
ALTER DEFAULT PRIVILEGES FOR ROLE :"migration_role" IN SCHEMA public GRANT SELECT ON TABLES TO :"runtime_role";
