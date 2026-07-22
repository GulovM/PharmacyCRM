-- E2-FIX-016: separate API and outbox-worker runtime privileges.
-- Verification query: SELECT has_table_privilege('pharmacycrm_api_runtime', 'pharmacycrm_schema_metadata', 'SELECT') AND has_table_privilege('pharmacycrm_worker_runtime', 'outbox_events', 'SELECT,UPDATE') AND NOT has_column_privilege('pharmacycrm_worker_runtime', 'users', 'password_hash', 'SELECT') AND NOT has_column_privilege('pharmacycrm_worker_runtime', 'user_sessions', 'refresh_token_hash', 'SELECT') AND has_function_privilege('pharmacycrm_worker_runtime', 'public.delete_processed_outbox_events_before(timestamptz,integer)', 'EXECUTE');
-- Lock/rewrite assessment: catalog-only role grants and revocations; no table scan or rewrite.
-- Compatibility: deployment provisioning creates the NOLOGIN group roles before this migration; missing roles fail closed.
-- Forward-fix policy: every new table must receive explicit API or worker grants in its creating migration.

GRANT USAGE ON SCHEMA public TO pharmacycrm_api_runtime, pharmacycrm_worker_runtime;
GRANT SELECT ON pharmacycrm_schema_metadata, roles, pharmacies, pharmacy_assignments, products, product_presentations, product_barcodes, product_requests, import_jobs, import_rows, pharmacy_products, inventory_operations, receipts, receipt_items, stock_lots, inventory_movements, write_offs, write_off_items, inventory_adjustments, inventory_adjustment_items, sales, sale_items, sale_item_allocations, sale_returns, sale_return_items, sale_return_item_allocations, idempotency_records, outbox_events, alerts, public_availability_projection TO pharmacycrm_api_runtime;
GRANT SELECT (id, login, display_name, phone, status, failed_login_attempts, locked_until, password_changed_at, last_login_at, version, created_at, updated_at, blocked_at, archived_at) ON users TO pharmacycrm_api_runtime;
GRANT SELECT (id, user_id, token_family_id, generation, user_agent, ip_address, created_at, last_used_at, expires_at, idle_expires_at, absolute_expires_at, authentication_method, mfa_level, revoked_at, revoke_reason) ON user_sessions TO pharmacycrm_api_runtime;
GRANT INSERT, UPDATE (last_used_at, revoked_at, revoke_reason) ON user_sessions TO pharmacycrm_api_runtime;
GRANT INSERT ON users, user_roles, user_sessions, idempotency_records, outbox_events, audit_events TO pharmacycrm_api_runtime;
GRANT SELECT ON pharmacycrm_schema_metadata, outbox_events TO pharmacycrm_worker_runtime;
GRANT UPDATE (status, attempt_count, available_at, lease_token, lease_generation, leased_by, lease_expires_at, last_error_code, last_error_at, processed_at, dead_lettered_at) ON outbox_events TO pharmacycrm_worker_runtime;
GRANT EXECUTE ON FUNCTION delete_processed_outbox_events_before(timestamptz, integer), delete_dead_letter_outbox_events_before(timestamptz, integer) TO pharmacycrm_worker_runtime;
