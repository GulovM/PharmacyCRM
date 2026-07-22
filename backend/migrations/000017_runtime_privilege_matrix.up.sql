-- E2-FIX-007: replace blanket runtime writes with an explicit least-privilege matrix.
-- Verification query: SELECT has_table_privilege('pharmacycrm_runtime','pharmacycrm_schema_metadata','SELECT') AND NOT has_table_privilege('pharmacycrm_runtime','pharmacycrm_schema_metadata','INSERT,UPDATE,DELETE') AND NOT has_table_privilege('pharmacycrm_runtime','inventory_movements','UPDATE,DELETE') AND has_table_privilege('pharmacycrm_runtime','inventory_movements','INSERT') AND NOT has_table_privilege('pharmacycrm_runtime','sales','UPDATE') AND has_column_privilege('pharmacycrm_runtime','sales','status','UPDATE');
-- Lock/rewrite assessment: catalog-only GRANT/REVOKE operations; no table scans or rewrites.
-- Compatibility: runtime retains reads, explicit inserts, reliability state transitions, and approved lifecycle-column updates.
-- Forward-fix policy: every new table receives privileges explicitly in its creating migration.

REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM pharmacycrm_runtime;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
REVOKE ALL PRIVILEGES ON TABLES FROM pharmacycrm_runtime;

GRANT SELECT ON ALL TABLES IN SCHEMA public TO pharmacycrm_runtime;

GRANT INSERT ON
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
    receipts,
    receipt_items,
    stock_lots,
    inventory_movements,
    write_offs,
    write_off_items,
    inventory_adjustments,
    inventory_adjustment_items,
    sales,
    sale_items,
    sale_item_allocations,
    sale_returns,
    sale_return_items,
    sale_return_item_allocations,
    idempotency_records,
    outbox_events,
    audit_events,
    alerts,
    public_availability_projection
TO pharmacycrm_runtime;

GRANT UPDATE (
    password_hash, display_name, phone, status, failed_login_attempts,
    locked_until, password_changed_at, last_login_at, version, updated_at,
    blocked_at, archived_at
) ON users TO pharmacycrm_runtime;
GRANT UPDATE (revoked_by_user_id, revoked_at, revoke_reason)
ON user_roles TO pharmacycrm_runtime;
GRANT UPDATE (last_used_at, revoked_at, revoke_reason)
ON user_sessions TO pharmacycrm_runtime;
GRANT UPDATE (
    name, address, landmark, latitude, longitude, phone, working_hours,
    status, version, updated_at, blocked_at, archived_at
) ON pharmacies TO pharmacycrm_runtime;
GRANT UPDATE (ended_by_user_id, ended_at, end_reason)
ON pharmacy_assignments TO pharmacycrm_runtime;

GRANT UPDATE (
    title, inn, dosage, form, manufacturer, country,
    is_prescription_required, status, version, updated_at
) ON products TO pharmacycrm_runtime;
GRANT UPDATE (package_name, inner_unit_name, status, version, updated_at)
ON product_presentations TO pharmacycrm_runtime;
GRANT UPDATE (is_primary, status)
ON product_barcodes TO pharmacycrm_runtime;
GRANT UPDATE (
    status, resolved_product_presentation_id, resolved_by_user_id,
    resolution_note, resolved_at
) ON product_requests TO pharmacycrm_runtime;
GRANT UPDATE (
    status, total_rows, valid_rows, error_rows, result_resource_type,
    result_resource_id, failure_code, started_at, completed_at
) ON import_jobs TO pharmacycrm_runtime;
GRANT UPDATE (
    normalized_data, status, matched_product_presentation_id,
    validation_errors, version, updated_at
) ON import_rows TO pharmacycrm_runtime;
GRANT UPDATE (
    is_inner_unit_sale_allowed, default_package_price_dirams,
    default_inner_unit_price_dirams, min_stock_level_base_units,
    target_stock_level_base_units, inventory_changed_at,
    status, version, updated_at
) ON pharmacy_products TO pharmacycrm_runtime;

GRANT UPDATE (quantity_base_units, status, version, updated_at)
ON stock_lots TO pharmacycrm_runtime;
GRANT UPDATE (status) ON inventory_operations TO pharmacycrm_runtime;
GRANT UPDATE (status) ON receipts TO pharmacycrm_runtime;
GRANT UPDATE (status) ON sales TO pharmacycrm_runtime;
GRANT UPDATE (status) ON sale_returns TO pharmacycrm_runtime;
GRANT UPDATE (status) ON write_offs TO pharmacycrm_runtime;
GRANT UPDATE (status) ON inventory_adjustments TO pharmacycrm_runtime;

GRANT UPDATE (
    status, response_status, response_body, resource_type,
    resource_id, completed_at
) ON idempotency_records TO pharmacycrm_runtime;
GRANT UPDATE (
    status, attempt_count, available_at, lease_token, lease_generation,
    leased_by, lease_expires_at, last_error_code, last_error_at,
    processed_at, dead_lettered_at
) ON outbox_events TO pharmacycrm_runtime;
GRANT UPDATE (
    status, last_confirmed_at, acknowledged_by_user_id, acknowledged_at,
    resolved_by_user_id, resolved_at, metadata
) ON alerts TO pharmacycrm_runtime;
GRANT UPDATE (
    package_price_dirams, inner_unit_price_dirams, availability_status,
    source_inventory_changed_at, projected_at, projection_version
) ON public_availability_projection TO pharmacycrm_runtime;

REVOKE DELETE ON ALL TABLES IN SCHEMA public FROM pharmacycrm_runtime;
