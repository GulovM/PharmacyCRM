-- E2-FIX-046: verify the inert compatibility runtime role on every no-op migration.
-- Verification query: SELECT EXISTS (SELECT 1 FROM pg_roles role WHERE role.rolname = 'pharmacycrm_runtime' AND NOT (role.rolcanlogin OR role.rolsuper OR role.rolcreatedb OR role.rolcreaterole OR role.rolinherit OR role.rolreplication OR role.rolbypassrls)) AND NOT EXISTS (SELECT 1 FROM pg_auth_members membership WHERE membership.roleid = 'pharmacycrm_runtime'::regrole OR membership.member = 'pharmacycrm_runtime'::regrole) AND NOT EXISTS (SELECT 1 FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole UNION ALL SELECT 1 FROM pg_database d CROSS JOIN LATERAL aclexplode(d.datacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole UNION ALL SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole UNION ALL SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole UNION ALL SELECT 1 FROM pg_attribute c CROSS JOIN LATERAL aclexplode(c.attacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole UNION ALL SELECT 1 FROM pg_type t CROSS JOIN LATERAL aclexplode(t.typacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole) AND (SELECT count(*) FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole) = 2 AND NOT EXISTS (SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a WHERE a.grantee = 'pharmacycrm_runtime'::regrole AND (a.privilege_type <> 'EXECUTE' OR p.oid NOT IN ('public.delete_processed_outbox_events_before(timestamptz,integer)'::regprocedure, 'public.delete_dead_letter_outbox_events_before(timestamptz,integer)'::regprocedure)));
-- Lock/rewrite assessment: catalog-only verification; no user table lock or rewrite.
-- Compatibility: validates the compatibility role used by E1 upgrades without changing its capabilities.
-- Forward-fix policy: published migrations remain immutable; later corrections require another forward migration.

DO $$
DECLARE
    runtime_oid oid;
BEGIN
    SELECT oid INTO runtime_oid FROM pg_roles WHERE rolname = 'pharmacycrm_runtime';
    IF runtime_oid IS NULL OR EXISTS (
        SELECT 1 FROM pg_roles WHERE oid = runtime_oid
          AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR rolinherit OR rolreplication OR rolbypassrls)
    ) THEN
        RAISE EXCEPTION 'pharmacycrm_runtime role attributes violate the compatibility contract';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_auth_members WHERE roleid = runtime_oid OR member = runtime_oid) THEN
        RAISE EXCEPTION 'pharmacycrm_runtime retains memberships';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) a WHERE a.grantee = runtime_oid)
       OR EXISTS (SELECT 1 FROM pg_database d CROSS JOIN LATERAL aclexplode(d.datacl) a WHERE a.grantee = runtime_oid)
       OR EXISTS (SELECT 1 FROM pg_namespace n CROSS JOIN LATERAL aclexplode(n.nspacl) a WHERE a.grantee = runtime_oid)
       OR EXISTS (SELECT 1 FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) a WHERE a.grantee = runtime_oid)
       OR EXISTS (SELECT 1 FROM pg_attribute a CROSS JOIN LATERAL aclexplode(a.attacl) x WHERE x.grantee = runtime_oid)
       OR EXISTS (SELECT 1 FROM pg_type t CROSS JOIN LATERAL aclexplode(t.typacl) a WHERE a.grantee = runtime_oid) THEN
        RAISE EXCEPTION 'pharmacycrm_runtime retains direct ACL capabilities';
    END IF;
    IF EXISTS (
        SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
        WHERE a.grantee = runtime_oid AND (a.privilege_type <> 'EXECUTE' OR p.oid NOT IN (
            'public.delete_processed_outbox_events_before(timestamptz,integer)'::regprocedure,
            'public.delete_dead_letter_outbox_events_before(timestamptz,integer)'::regprocedure
        ))
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
        WHERE a.grantee = runtime_oid AND a.privilege_type = 'EXECUTE'
          AND p.oid = 'public.delete_processed_outbox_events_before(timestamptz,integer)'::regprocedure
    ) OR NOT EXISTS (
        SELECT 1 FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
        WHERE a.grantee = runtime_oid AND a.privilege_type = 'EXECUTE'
          AND p.oid = 'public.delete_dead_letter_outbox_events_before(timestamptz,integer)'::regprocedure
    ) THEN
        RAISE EXCEPTION 'pharmacycrm_runtime function ACL violates the compatibility contract';
    END IF;
END
$$;
