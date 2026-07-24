-- E2-DB-001: system role seeds and least-privilege database grants.
-- Verification query: SELECT (SELECT count(*) = 3 FROM roles WHERE code IN ('CLIENT','PHARMACIST','ADMIN') AND is_system) AND has_table_privilege('pharmacycrm_runtime','users','SELECT') AND has_table_privilege('pharmacycrm_runtime','users','INSERT') AND has_table_privilege('pharmacycrm_runtime','outbox_events','SELECT') AND has_table_privilege('pharmacycrm_runtime','outbox_events','INSERT') AND NOT has_table_privilege('pharmacycrm_runtime','inventory_movements','UPDATE,DELETE,TRUNCATE') AND NOT has_table_privilege('pharmacycrm_runtime','audit_events','UPDATE,DELETE,TRUNCATE');
-- Lock/rewrite assessment: three idempotent seed upserts and catalog privilege changes; no table rewrite.
-- Compatibility: additive baseline; grants target the NOLOGIN group provisioned before migrations.
-- Forward-fix policy: privilege and seed corrections use a new forward migration; destructive down is prohibited.

INSERT INTO roles (code, name, description, is_system)
VALUES
    ('CLIENT', 'Client', 'Customer-facing user', true),
    ('PHARMACIST', 'Pharmacist', 'Pharmacy-scoped operator', true),
    ('ADMIN', 'Administrator', 'Global administrative operator', true)
ON CONFLICT (code) DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    is_system = true;

GRANT USAGE ON SCHEMA public TO pharmacycrm_runtime;
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA public TO pharmacycrm_runtime;

GRANT UPDATE ON
    users,
    user_roles,
    user_sessions,
    pharmacies,
    pharmacy_assignments,
    products,
    product_presentations,
    product_barcodes,
    product_requests,
    import_jobs,
    import_rows,
    pharmacy_products,
    inventory_operations,
    stock_lots,
    receipts,
    sales,
    sale_returns,
    write_offs,
    inventory_adjustments,
    idempotency_records,
    outbox_events,
    alerts,
    public_availability_projection
TO pharmacycrm_runtime;

REVOKE UPDATE, DELETE ON inventory_movements, audit_events FROM pharmacycrm_runtime;
REVOKE DELETE ON ALL TABLES IN SCHEMA public FROM pharmacycrm_runtime;

ALTER DEFAULT PRIVILEGES IN SCHEMA public
GRANT SELECT, INSERT ON TABLES TO pharmacycrm_runtime;
