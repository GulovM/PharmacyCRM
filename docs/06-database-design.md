# PharmacyCRM — Database Design

**Статус документа:** Draft  
**Версия:** 1.1  
**Дата:** 2026-07-17  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`  
**Связанные ADR:** ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет целевую PostgreSQL-модель PharmacyCRM. Он объединяет identity, роли, назначения аптекарей, сессии, глобальный каталог, аптеки, ассортимент, импорты, поступления, лоты, продажи, возвраты, списания, корректировки, идемпотентность, предупреждения и аудит.

DDL является проектным контрактом, а не одной готовой migration. Реализация обязана разложить его на последовательные миграции, сохранив ограничения, индексы, внешние ключи, module ownership и транзакционные инварианты.

`docs/06-01-database-design-return-allocations.md` полностью интегрирован. Основным источником истины по возвратным аллокациям является настоящий документ.

## 2. Базовые принципы

1. PostgreSQL является источником истины для identity, authorization state, каталога, остатков, документов, идемпотентности и аудита.
2. Остаток хранится целым числом базовых единиц отпуска.
3. Любое изменение остатка сопровождается append-only записью в `inventory_movements`.
4. Остаток, движения, бизнес-документ, idempotency record и обязательный audit event фиксируются атомарно, если принадлежат одной критической операции.
5. Деньги хранятся как `bigint` в дирамах. `float` для денег запрещён.
6. Исторические документы сохраняют snapshots цен, коэффициентов упаковки и иных данных, влияющих на расчёт.
7. Проведённые документы, движения и audit events не редактируются и не удаляются обычным CRUD.
8. Пользователь, аптека и справочник архивируются логически; бизнес-история использует `ON DELETE RESTRICT`.
9. Access token не является источником актуального authorization state: backend повторно проверяет пользователя, роль, session и назначение.
10. Критические команды используют общую таблицу идемпотентности.
11. Транзакции используют `READ COMMITTED` и детерминированные пессимистические блокировки.
12. Внешние сетевые вызовы внутри транзакционного callback запрещены.
13. Ограничения БД должны предотвращать локально проверяемые противоречия; межтабличные агрегатные инварианты обеспечиваются application layer и integration tests.

## 3. Module ownership

| Модуль | Таблицы |
|---|---|
| Identity | `users`, `roles`, `user_roles`, `user_sessions`, `pharmacy_assignments` |
| Pharmacy | `pharmacies` |
| Catalog | `products`, `product_presentations`, `product_barcodes`, `product_requests` |
| Import | `import_jobs`, `import_rows` |
| Assortment | `pharmacy_products` |
| Inventory | `inventory_operations`, `inventory_movements`, `stock_lots` |
| Receipt | `receipts`, `receipt_items` |
| Sales | `sales`, `sale_items`, `sale_item_allocations` |
| Returns | `sale_returns`, `sale_return_items`, `sale_return_item_allocations` |
| Adjustments | `write_offs`, `write_off_items`, `inventory_adjustments`, `inventory_adjustment_items` |
| Reliability | `idempotency_records` |
| Audit | `audit_events` |
| Alerts | `alerts` |

Один модуль не изменяет таблицы другого модуля в обход его repository/application contract. Межмодульные бизнес-транзакции выполняются через Unit of Work.

## 4. Общие соглашения DDL

- Primary key: `uuid DEFAULT gen_random_uuid()`.
- Время: `timestamptz`, формируемое сервером.
- Срок годности: `date`.
- Статусы: `varchar` с `CHECK`; PostgreSQL enum не используется.
- Для изменяемых справочников: `version bigint NOT NULL DEFAULT 1 CHECK (version > 0)`.
- `updated_at` обновляется единообразно application layer или общим trigger.
- Причины и обязательные текстовые значения проверяются через `btrim(...) <> ''`.
- Nullable-колонки не используются внутри обычного `UNIQUE`, если `NULL` является частью логического scope: применяется partial/expression index или отдельный non-null `scope_key`.
- Исторические связи не используют `ON DELETE CASCADE`.
- JSONB не заменяет нормализованные поля, по которым строятся права, уникальности, блокировки или денежные расчёты.

## 5. Проектный DDL

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ================================================================
-- Identity and authorization
-- ================================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login varchar(150) NOT NULL CHECK (btrim(login) <> ''),
    password_hash text NOT NULL CHECK (btrim(password_hash) <> ''),
    display_name varchar(255) NOT NULL CHECK (btrim(display_name) <> ''),
    phone varchar(50),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')),
    failed_login_attempts integer NOT NULL DEFAULT 0 CHECK (failed_login_attempts >= 0),
    locked_until timestamptz,
    password_changed_at timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    blocked_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT chk_user_status_timestamps CHECK (
        (status = 'ACTIVE' AND archived_at IS NULL)
        OR (status = 'BLOCKED' AND blocked_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'ARCHIVED' AND archived_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_users_login_active
ON users (lower(btrim(login))) WHERE status <> 'ARCHIVED';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(50) NOT NULL UNIQUE CHECK (code IN ('CLIENT', 'PHARMACIST', 'ADMIN')),
    name varchar(100) NOT NULL CHECK (btrim(name) <> ''),
    description text,
    is_system boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    revoked_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    revoked_at timestamptz,
    revoke_reason text,
    CONSTRAINT chk_user_role_revocation CHECK (
        (revoked_at IS NULL AND revoked_by_user_id IS NULL AND revoke_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_at >= assigned_at
            AND revoked_by_user_id IS NOT NULL AND btrim(revoke_reason) <> '')
    )
);

CREATE UNIQUE INDEX uq_user_single_active_role
ON user_roles (user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_user_roles_history ON user_roles (user_id, assigned_at DESC, id DESC);

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    refresh_token_hash bytea NOT NULL UNIQUE,
    token_family_id uuid NOT NULL,
    rotated_from_session_id uuid REFERENCES user_sessions(id) ON DELETE RESTRICT,
    user_agent text,
    ip_address inet,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoke_reason varchar(100),
    CONSTRAINT chk_session_expiration CHECK (expires_at > created_at),
    CONSTRAINT chk_session_last_used CHECK (last_used_at >= created_at),
    CONSTRAINT chk_session_rotation_self CHECK (rotated_from_session_id IS NULL OR rotated_from_session_id <> id),
    CONSTRAINT chk_session_revocation CHECK (
        (revoked_at IS NULL AND revoke_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_at >= created_at AND btrim(revoke_reason) <> '')
    )
);

CREATE UNIQUE INDEX uq_user_session_rotated_from
ON user_sessions (rotated_from_session_id) WHERE rotated_from_session_id IS NOT NULL;
CREATE INDEX idx_user_sessions_user_active
ON user_sessions (user_id, expires_at DESC, id DESC) WHERE revoked_at IS NULL;
CREATE INDEX idx_user_sessions_family ON user_sessions (token_family_id, created_at, id);

-- ================================================================
-- Pharmacies and pharmacist assignments
-- ================================================================

CREATE TABLE pharmacies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(255) NOT NULL CHECK (btrim(name) <> ''),
    address text NOT NULL CHECK (btrim(address) <> ''),
    landmark text,
    latitude numeric(9,6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude numeric(9,6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    phone varchar(50),
    working_hours varchar(255),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    blocked_at timestamptz,
    archived_at timestamptz,
    CONSTRAINT chk_pharmacy_status_timestamps CHECK (
        (status = 'ACTIVE' AND archived_at IS NULL)
        OR (status = 'BLOCKED' AND blocked_at IS NOT NULL AND archived_at IS NULL)
        OR (status = 'ARCHIVED' AND archived_at IS NOT NULL)
    )
);

CREATE TABLE pharmacy_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    ended_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    ended_at timestamptz,
    end_reason text,
    CONSTRAINT chk_assignment_end CHECK (
        (ended_at IS NULL AND ended_by_user_id IS NULL AND end_reason IS NULL)
        OR (ended_at IS NOT NULL AND ended_at >= assigned_at
            AND ended_by_user_id IS NOT NULL AND btrim(end_reason) <> '')
    )
);

CREATE UNIQUE INDEX uq_pharmacy_assignment_active_user
ON pharmacy_assignments (user_id) WHERE ended_at IS NULL;
CREATE INDEX idx_pharmacy_assignments_pharmacy_active
ON pharmacy_assignments (pharmacy_id, user_id) WHERE ended_at IS NULL;
CREATE INDEX idx_pharmacy_assignments_history
ON pharmacy_assignments (user_id, assigned_at DESC, id DESC);

-- ================================================================
-- Global catalog
-- ================================================================

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title varchar(255) NOT NULL CHECK (btrim(title) <> ''),
    inn varchar(255),
    dosage varchar(100),
    form varchar(100) NOT NULL CHECK (btrim(form) <> ''),
    manufacturer varchar(255) NOT NULL CHECK (btrim(manufacturer) <> ''),
    country varchar(100),
    is_prescription_required boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_products_search_title ON products (lower(title));
CREATE INDEX idx_products_search_inn ON products (lower(inn));

CREATE TABLE product_presentations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    package_name varchar(100) NOT NULL CHECK (btrim(package_name) <> ''),
    inner_unit_name varchar(100),
    base_units_per_package bigint NOT NULL CHECK (base_units_per_package > 0),
    package_description text,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_presentation_inner_unit CHECK (
        (base_units_per_package = 1)
        OR (inner_unit_name IS NOT NULL AND btrim(inner_unit_name) <> '')
    )
);
CREATE INDEX idx_presentations_product ON product_presentations (product_id, status, id);

CREATE TABLE product_barcodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
    barcode varchar(100) NOT NULL UNIQUE CHECK (btrim(barcode) <> ''),
    barcode_type varchar(30) NOT NULL DEFAULT 'EAN13',
    is_primary boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_archived_barcode_not_primary CHECK (status = 'ACTIVE' OR is_primary = false)
);
CREATE UNIQUE INDEX uq_product_barcodes_primary
ON product_barcodes (product_presentation_id)
WHERE is_primary = true AND status = 'ACTIVE';

CREATE TABLE product_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    requested_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    raw_name varchar(255) NOT NULL CHECK (btrim(raw_name) <> ''),
    raw_details jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(30) NOT NULL DEFAULT 'OPEN'
        CHECK (status IN ('OPEN', 'APPROVED', 'REJECTED', 'DUPLICATE')),
    resolved_product_presentation_id uuid REFERENCES product_presentations(id) ON DELETE RESTRICT,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolution_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT chk_product_request_resolution CHECK (
        (status = 'OPEN' AND resolved_product_presentation_id IS NULL
            AND resolved_by_user_id IS NULL AND resolved_at IS NULL AND resolution_note IS NULL)
        OR (status <> 'OPEN' AND resolved_by_user_id IS NOT NULL
            AND resolved_at IS NOT NULL AND btrim(resolution_note) <> '')
    ),
    CONSTRAINT chk_product_request_target CHECK (
        status NOT IN ('APPROVED', 'DUPLICATE') OR resolved_product_presentation_id IS NOT NULL
    )
);

-- ================================================================
-- Imports and staging
-- ================================================================

CREATE TABLE import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_type varchar(50) NOT NULL CHECK (import_type IN ('CATALOG', 'INITIAL_STOCK')),
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    original_filename varchar(255) NOT NULL CHECK (btrim(original_filename) <> ''),
    content_type varchar(150) NOT NULL,
    storage_key text NOT NULL CHECK (btrim(storage_key) <> ''),
    file_sha256 bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'UPLOADED' CHECK (
        status IN ('UPLOADED', 'VALIDATING', 'READY', 'HAS_ERRORS', 'CONFIRMING', 'COMPLETED', 'FAILED')
    ),
    total_rows bigint NOT NULL DEFAULT 0 CHECK (total_rows >= 0),
    valid_rows bigint NOT NULL DEFAULT 0 CHECK (valid_rows >= 0),
    error_rows bigint NOT NULL DEFAULT 0 CHECK (error_rows >= 0),
    result_resource_type varchar(100),
    result_resource_id uuid,
    failure_code varchar(100),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT chk_import_scope CHECK (
        (import_type = 'CATALOG' AND pharmacy_id IS NULL)
        OR (import_type = 'INITIAL_STOCK' AND pharmacy_id IS NOT NULL)
    ),
    CONSTRAINT chk_import_counts CHECK (valid_rows + error_rows <= total_rows),
    CONSTRAINT chk_import_terminal CHECK (
        (status IN ('COMPLETED', 'FAILED') AND completed_at IS NOT NULL)
        OR (status NOT IN ('COMPLETED', 'FAILED') AND completed_at IS NULL)
    ),
    CONSTRAINT chk_import_failure CHECK (
        (status = 'FAILED' AND failure_code IS NOT NULL)
        OR (status <> 'FAILED' AND failure_code IS NULL)
    )
);

CREATE TABLE import_rows (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_job_id uuid NOT NULL REFERENCES import_jobs(id) ON DELETE RESTRICT,
    row_number bigint NOT NULL CHECK (row_number > 0),
    raw_data jsonb NOT NULL,
    normalized_data jsonb,
    status varchar(30) NOT NULL CHECK (
        status IN ('PENDING', 'VALID', 'ERROR', 'MATCHED', 'CREATE_NEW', 'REJECTED', 'PUBLISHED')
    ),
    matched_product_presentation_id uuid REFERENCES product_presentations(id) ON DELETE RESTRICT,
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_import_row_number UNIQUE (import_job_id, row_number),
    CONSTRAINT chk_import_row_match CHECK (
        status <> 'MATCHED' OR matched_product_presentation_id IS NOT NULL
    )
);
CREATE INDEX idx_import_rows_status ON import_rows (import_job_id, status, row_number);

-- ================================================================
-- Assortment, inventory, receipts and lots
-- ================================================================

CREATE TABLE pharmacy_products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
    is_inner_unit_sale_allowed boolean NOT NULL DEFAULT false,
    default_package_price_dirams bigint NOT NULL CHECK (default_package_price_dirams >= 0),
    default_inner_unit_price_dirams bigint CHECK (default_inner_unit_price_dirams >= 0),
    min_stock_level_base_units bigint NOT NULL DEFAULT 0 CHECK (min_stock_level_base_units >= 0),
    target_stock_level_base_units bigint NOT NULL DEFAULT 0 CHECK (target_stock_level_base_units >= 0),
    inventory_changed_at timestamptz,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_pharmacy_product UNIQUE (pharmacy_id, product_presentation_id),
    CONSTRAINT chk_inner_unit_price CHECK (
        (is_inner_unit_sale_allowed AND default_inner_unit_price_dirams IS NOT NULL)
        OR (NOT is_inner_unit_sale_allowed AND default_inner_unit_price_dirams IS NULL)
    ),
    CONSTRAINT chk_stock_targets CHECK (target_stock_level_base_units >= min_stock_level_base_units)
);

CREATE TABLE inventory_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_type varchar(50) NOT NULL CHECK (operation_type IN (
        'RECEIPT', 'INITIAL_STOCK', 'SALE', 'RETURN_TO_STOCK',
        'RETURN_WRITE_OFF', 'RETURN_QUARANTINE', 'WRITE_OFF',
        'INVENTORY_ADJUSTMENT', 'REVERSAL'
    )),
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (status IN ('POSTED', 'REVERSED')),
    initiated_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    is_system_initiated boolean NOT NULL DEFAULT false,
    reversal_of_operation_id uuid REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_inventory_actor CHECK (
        (is_system_initiated AND initiated_by_user_id IS NULL)
        OR (NOT is_system_initiated AND initiated_by_user_id IS NOT NULL)
    ),
    CONSTRAINT chk_inventory_reversal CHECK (
        (operation_type = 'REVERSAL' AND reversal_of_operation_id IS NOT NULL)
        OR (operation_type <> 'REVERSAL' AND reversal_of_operation_id IS NULL)
    ),
    CONSTRAINT chk_inventory_reversal_self CHECK (reversal_of_operation_id IS NULL OR reversal_of_operation_id <> id)
);
CREATE UNIQUE INDEX uq_inventory_reversal
ON inventory_operations (reversal_of_operation_id) WHERE reversal_of_operation_id IS NOT NULL;

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    source_import_job_id uuid REFERENCES import_jobs(id) ON DELETE RESTRICT,
    receipt_number varchar(100) NOT NULL CHECK (btrim(receipt_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (status IN ('POSTED', 'REVERSED')),
    supplier_name varchar(255),
    received_at timestamptz NOT NULL,
    posted_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    posted_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_receipt_number_per_pharmacy UNIQUE (pharmacy_id, receipt_number)
);

CREATE TABLE receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id uuid NOT NULL REFERENCES receipts(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_packages bigint NOT NULL CHECK (quantity_packages > 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    purchase_price_package_dirams bigint NOT NULL CHECK (purchase_price_package_dirams >= 0),
    retail_price_package_dirams bigint NOT NULL CHECK (retail_price_package_dirams >= 0),
    retail_price_inner_unit_dirams bigint CHECK (retail_price_inner_unit_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_receipt_quantity CHECK (
        quantity_base_units = quantity_packages * base_units_per_package_snapshot
    )
);

CREATE TABLE stock_lots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    receipt_item_id uuid REFERENCES receipt_items(id) ON DELETE RESTRICT,
    origin_type varchar(30) NOT NULL CHECK (origin_type IN ('RECEIPT', 'INITIAL_STOCK', 'RETURN')),
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_base_units bigint NOT NULL DEFAULT 0 CHECK (quantity_base_units >= 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    purchase_price_package_dirams bigint CHECK (purchase_price_package_dirams >= 0),
    package_retail_price_dirams bigint NOT NULL CHECK (package_retail_price_dirams >= 0),
    inner_unit_retail_price_dirams bigint CHECK (inner_unit_retail_price_dirams >= 0),
    received_at timestamptz NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_lot_origin CHECK (
        (origin_type IN ('RECEIPT', 'INITIAL_STOCK') AND receipt_item_id IS NOT NULL)
        OR (origin_type = 'RETURN' AND receipt_item_id IS NULL)
    ),
    CONSTRAINT chk_lot_status_quantity CHECK (
        status <> 'DEPLETED' OR quantity_base_units = 0
    )
);
CREATE UNIQUE INDEX uq_stock_lot_receipt_item
ON stock_lots (receipt_item_id) WHERE receipt_item_id IS NOT NULL;
CREATE INDEX idx_stock_lots_fefo
ON stock_lots (pharmacy_product_id, expiration_date, received_at, id)
WHERE status = 'ACTIVE' AND quantity_base_units > 0;

CREATE TABLE inventory_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id uuid NOT NULL REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    quantity_after_base_units bigint NOT NULL CHECK (quantity_after_base_units >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_movement_operation_lot UNIQUE (operation_id, stock_lot_id)
);
CREATE INDEX idx_inventory_movements_lot_history
ON inventory_movements (stock_lot_id, created_at, id);

-- ================================================================
-- Sales and FEFO allocations
-- ================================================================

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    sale_number varchar(100) NOT NULL CHECK (btrim(sale_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REVERSED')),
    payment_method varchar(50) NOT NULL CHECK (payment_method IN ('CASH', 'CARD', 'MOBILE', 'MIXED')),
    subtotal_amount_dirams bigint NOT NULL CHECK (subtotal_amount_dirams >= 0),
    discount_amount_dirams bigint NOT NULL DEFAULT 0 CHECK (discount_amount_dirams >= 0),
    total_amount_dirams bigint NOT NULL CHECK (total_amount_dirams >= 0),
    prescription_confirmed boolean NOT NULL DEFAULT false,
    sold_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    sold_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_number_per_pharmacy UNIQUE (pharmacy_id, sale_number),
    CONSTRAINT chk_sale_total CHECK (
        total_amount_dirams = subtotal_amount_dirams - discount_amount_dirams
        AND discount_amount_dirams <= subtotal_amount_dirams
    )
);

CREATE TABLE sale_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    sale_unit varchar(30) NOT NULL CHECK (sale_unit IN ('PACKAGE', 'INNER_UNIT')),
    display_quantity bigint NOT NULL CHECK (display_quantity > 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    unit_price_dirams bigint NOT NULL CHECK (unit_price_dirams >= 0),
    line_subtotal_dirams bigint NOT NULL CHECK (line_subtotal_dirams >= 0),
    line_discount_dirams bigint NOT NULL DEFAULT 0 CHECK (line_discount_dirams >= 0),
    line_total_dirams bigint NOT NULL CHECK (line_total_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_product_unit UNIQUE (sale_id, pharmacy_product_id, sale_unit),
    CONSTRAINT chk_sale_item_total CHECK (
        line_subtotal_dirams = display_quantity * unit_price_dirams
        AND line_total_dirams = line_subtotal_dirams - line_discount_dirams
        AND line_discount_dirams <= line_subtotal_dirams
    ),
    CONSTRAINT chk_sale_item_quantity CHECK (
        (sale_unit = 'PACKAGE' AND quantity_base_units = display_quantity * base_units_per_package_snapshot)
        OR (sale_unit = 'INNER_UNIT' AND quantity_base_units = display_quantity)
    )
);

CREATE TABLE sale_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_allocation UNIQUE (sale_item_id, stock_lot_id)
);
CREATE INDEX idx_sale_item_allocations_lot ON sale_item_allocations (stock_lot_id, sale_item_id);

-- ================================================================
-- Returns and incorporated return allocations
-- ================================================================

CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    operation_id uuid UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (status IN ('COMPLETED', 'REVERSED')),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    returned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    returned_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_sale_returns_sale ON sale_returns (sale_id, returned_at, id);

CREATE TABLE sale_return_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_id uuid NOT NULL REFERENCES sale_returns(id) ON DELETE RESTRICT,
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL CHECK (returned_quantity_base_units > 0),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    return_action varchar(50) NOT NULL CHECK (
        return_action IN ('RESTOCK', 'WRITE_OFF', 'QUARANTINE', 'NO_PHYSICAL_RETURN')
    ),
    item_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item UNIQUE (sale_return_id, sale_item_id),
    CONSTRAINT chk_sale_return_item_reason CHECK (item_reason IS NULL OR btrim(item_reason) <> '')
);
CREATE INDEX idx_sale_return_items_sale_item ON sale_return_items (sale_item_id);

CREATE TABLE sale_return_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_item_id uuid NOT NULL REFERENCES sale_return_items(id) ON DELETE RESTRICT,
    sale_item_allocation_id uuid NOT NULL REFERENCES sale_item_allocations(id) ON DELETE RESTRICT,
    target_stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL CHECK (returned_quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item_allocation UNIQUE (sale_return_item_id, sale_item_allocation_id)
);
CREATE INDEX idx_return_allocations_source
ON sale_return_item_allocations (sale_item_allocation_id, sale_return_item_id);

-- ================================================================
-- Write-offs and inventory adjustments
-- ================================================================

CREATE TABLE write_offs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (status IN ('COMPLETED', 'REVERSED')),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE write_off_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    write_off_id uuid NOT NULL REFERENCES write_offs(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_write_off_lot UNIQUE (write_off_id, stock_lot_id)
);

CREATE TABLE inventory_adjustments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (status IN ('COMPLETED', 'REVERSED')),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    approved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_adjustment_distinct_approver CHECK (
        approved_by_user_id IS NULL OR approved_by_user_id <> created_by_user_id
    )
);

CREATE TABLE inventory_adjustment_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    inventory_adjustment_id uuid NOT NULL REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    expected_quantity_base_units bigint NOT NULL CHECK (expected_quantity_base_units >= 0),
    actual_quantity_base_units bigint NOT NULL CHECK (actual_quantity_base_units >= 0),
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_adjustment_lot UNIQUE (inventory_adjustment_id, stock_lot_id),
    CONSTRAINT chk_inventory_adjustment_delta CHECK (
        delta_base_units = actual_quantity_base_units - expected_quantity_base_units
    )
);

-- ================================================================
-- Idempotency
-- ================================================================

CREATE TABLE idempotency_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    scope_key text GENERATED ALWAYS AS (
        actor_user_id::text || ':' || coalesce(pharmacy_id::text, 'GLOBAL') || ':' || operation
    ) STORED,
    operation varchar(150) NOT NULL CHECK (btrim(operation) <> ''),
    idempotency_key varchar(128) NOT NULL CHECK (btrim(idempotency_key) <> ''),
    request_hash bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'IN_PROGRESS'
        CHECK (status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED_RETRYABLE')),
    response_status integer CHECK (response_status BETWEEN 100 AND 599),
    response_body jsonb,
    resource_type varchar(100),
    resource_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CONSTRAINT uq_idempotency_scope UNIQUE (scope_key, idempotency_key),
    CONSTRAINT chk_idempotency_result CHECK (
        (status = 'COMPLETED' AND response_status IS NOT NULL
            AND response_body IS NOT NULL AND completed_at IS NOT NULL)
        OR (status = 'IN_PROGRESS' AND response_status IS NULL
            AND response_body IS NULL AND completed_at IS NULL)
        OR (status = 'FAILED_RETRYABLE' AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_idempotency_expiry CHECK (expires_at > created_at)
);
CREATE INDEX idx_idempotency_expiration ON idempotency_records (expires_at);
CREATE INDEX idx_idempotency_resource ON idempotency_records (resource_type, resource_id)
WHERE resource_id IS NOT NULL;

-- ================================================================
-- Audit
-- ================================================================

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    actor_session_id uuid REFERENCES user_sessions(id) ON DELETE RESTRICT,
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    actor_type varchar(30) NOT NULL CHECK (actor_type IN ('USER', 'SYSTEM')),
    action varchar(150) NOT NULL CHECK (btrim(action) <> ''),
    object_type varchar(100) NOT NULL CHECK (btrim(object_type) <> ''),
    object_id uuid,
    result varchar(30) NOT NULL CHECK (result IN ('SUCCESS', 'DENIED', 'FAILURE')),
    request_id varchar(128),
    trace_id varchar(128),
    ip_address inet,
    user_agent text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_audit_actor CHECK (
        (actor_type = 'USER' AND actor_user_id IS NOT NULL)
        OR (actor_type = 'SYSTEM' AND actor_user_id IS NULL AND actor_session_id IS NULL)
    ),
    CONSTRAINT chk_audit_session_actor CHECK (
        actor_session_id IS NULL OR actor_user_id IS NOT NULL
    )
);
CREATE INDEX idx_audit_events_time ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_actor ON audit_events (actor_user_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_pharmacy ON audit_events (pharmacy_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_object ON audit_events (object_type, object_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_request ON audit_events (request_id) WHERE request_id IS NOT NULL;

-- ================================================================
-- Alerts
-- ================================================================

CREATE TABLE alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    alert_type varchar(50) NOT NULL CHECK (alert_type IN (
        'LOW_STOCK', 'EXPIRED', 'EXPIRING_7_DAYS', 'EXPIRING_30_DAYS', 'RECONCILIATION_MISMATCH'
    )),
    deduplication_key varchar(255) NOT NULL CHECK (btrim(deduplication_key) <> ''),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'ACKNOWLEDGED', 'RESOLVED')),
    detected_at timestamptz NOT NULL,
    last_confirmed_at timestamptz NOT NULL,
    acknowledged_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    acknowledged_at timestamptz,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolved_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_alert_times CHECK (last_confirmed_at >= detected_at),
    CONSTRAINT chk_alert_acknowledgement CHECK (
        (acknowledged_at IS NULL AND acknowledged_by_user_id IS NULL)
        OR (acknowledged_at IS NOT NULL AND acknowledged_by_user_id IS NOT NULL)
    ),
    CONSTRAINT chk_alert_resolution CHECK (
        (status = 'RESOLVED' AND resolved_at IS NOT NULL)
        OR (status <> 'RESOLVED' AND resolved_at IS NULL AND resolved_by_user_id IS NULL)
    )
);
CREATE UNIQUE INDEX uq_alert_open_dedup
ON alerts (pharmacy_id, deduplication_key)
WHERE status IN ('ACTIVE', 'ACKNOWLEDGED');
CREATE INDEX idx_alerts_pharmacy_status
ON alerts (pharmacy_id, status, detected_at DESC, id DESC);
```

## 6. Транзакционные и межтабличные инварианты

### 6.1 Identity и sessions

1. Активная роль определяется строкой `user_roles.revoked_at IS NULL`.
2. История назначения роли не перезаписывается; повторное назначение создаёт новую строку.
3. `PHARMACIST` может иметь не более одного активного назначения аптеке.
4. Назначение разрешено только активному пользователю с активной ролью `PHARMACIST`.
5. Refresh rotation создаёт новую session и отзывает предыдущую в одной транзакции.
6. `rotated_from_session_id` должен ссылаться на session того же пользователя и той же token family.
7. Обнаружение reuse старого refresh token отзывает всю token family.
8. Блокировка, архивирование или смена пароля отзывают применимые sessions согласно Security Design.

### 6.2 Imports

1. `CATALOG` import не имеет `pharmacy_id`; `INITIAL_STOCK` всегда scoped аптекой.
2. Переход job в `COMPLETED` атомарно связывается с созданным ресурсом и idempotency record.
3. Опубликованная строка не публикуется повторно.
4. Счётчики job пересчитываются или обновляются в той же транзакции, что и статусы строк.
5. Политика полной или частичной публикации каталога должна быть зафиксирована до реализации publish-use-case.

### 6.3 Поступления и остатки

1. `receipt_items.pharmacy_product_id` принадлежит аптеке receipt.
2. `receipt.operation_id` принадлежит той же аптеке и имеет тип `RECEIPT` либо `INITIAL_STOCK`.
3. Каждый receipt item создаёт ровно один исходный lot.
4. Проведение атомарно создаёт receipt, items, lots, movements, audit и idempotency result.
5. Просроченный товар не создаёт продаваемый `ACTIVE` lot.
6. `stock_lots.quantity_base_units` после движения равно предыдущему значению плюс `delta_base_units`.
7. `stock_lots.quantity_base_units = SUM(inventory_movements.delta_base_units)` для всей истории lot.

### 6.4 Продажи

1. Все sale items принадлежат аптеке sale.
2. `sales.operation_id` принадлежит той же аптеке и имеет тип `SALE`.
3. Уникальные `pharmacy_product_id` блокируются в порядке UUID; lots — `expiration_date, received_at, id`.
4. Сумма allocations строки равна `sale_items.quantity_base_units`.
5. Сумма строк согласована с subtotal, discount и total sale.
6. Для рецептурной позиции требуется `prescription_confirmed = true` и audit event.
7. Недостаток любой позиции отклоняет продажу целиком.

### 6.5 Возвраты

1. Return item относится к sale item исходной продажи.
2. Return allocation относится к allocation той же sale item.
3. Сумма return allocations равна количеству return item.
4. Суммарный возврат по sale allocation не превышает проданное количество.
5. `target_stock_lot_id` обязателен для `RESTOCK` и `QUARANTINE`; для `NO_PHYSICAL_RETURN` отсутствует.
6. Target lot принадлежит той же аптеке и соответствует исходному product presentation.
7. Только `RESTOCK` создаёт положительное движение в продаваемый остаток.
8. `WRITE_OFF`, `QUARANTINE` и `NO_PHYSICAL_RETURN` не увеличивают доступный остаток.
9. Refund рассчитывается по snapshots исходной продажи.
10. Return, allocations, refund, operation, movements, sale status, idempotency и audit фиксируются атомарно.

### 6.6 Idempotency

1. Scope уникален и для pharmacy-команд, и для global/admin-команд; nullable `pharmacy_id` не должен допускать дубликаты.
2. Новый key создаётся как `IN_PROGRESS` внутри той же транзакции, которая сериализует конкурентные повторы.
3. Тот же key и тот же semantic hash возвращает исходные status/body без повторного эффекта.
4. Тот же key и другой hash отклоняется конфликтом.
5. Business effect и `COMPLETED` result commit-ятся атомарно.
6. `FAILED_RETRYABLE` используется только когда доказано отсутствие committed business effect.
7. Истечение API replay window не разрешает удалять доказательную связь юридически значимого документа с key раньше retention policy.

### 6.7 Audit

1. Для `actor_type = USER` session, если указана, принадлежит `actor_user_id`.
2. SYSTEM event не содержит user/session.
3. Обязательный audit event критической операции создаётся в её транзакции.
4. Audit metadata не содержит password, token, raw sensitive payload, SQL или stack trace.
5. Audit append-only; исправление выполняется новым событием.

## 7. Глобальный порядок блокировок

1. idempotency scope/key;
2. user и session при security-sensitive операции;
3. pharmacy;
4. sales или иной корневой документ;
5. `pharmacy_products` в порядке `id`;
6. sale items / allocations в порядке `id`;
7. stock lots в FEFO-порядке либо `id`, установленном сценарием;
8. вставка документов, movements, audit и idempotency result;
9. commit.

Любой use case, блокирующий одинаковые сущности, обязан использовать совместимый порядок. Изменение порядка требует синхронизации ADR-0011, тестов и этого документа.

## 8. Append-only и database privileges

Production application role не получает `DELETE` для операционных таблиц. Для `inventory_movements` и `audit_events` также не выдаётся обычный `UPDATE`. Если техническое обновление состояния действительно требуется, оно выполняется узкой функцией или отдельной ролью и оформляется ADR.

Retention cleanup допускается только для истёкших sessions, временных import-файлов и технических idempotency response bodies после утверждённого срока. Бизнес-документы, movements, allocations и audit history не удаляются обычным cleanup worker.

## 9. Требования к миграциям

1. Одна migration решает одну логическую задачу.
2. Migration содержит forward и rollback policy; необратимость фиксируется явно.
3. Constraints и индексы получают стабильные имена.
4. Большие индексы production создаются с планом `CONCURRENTLY`, где применимо.
5. Добавление `NOT NULL` к существующим данным выполняется через backfill и validation.
6. Seed системных ролей идемпотентен.
7. Изменение status/check синхронизируется с domain, API и tests.
8. Нельзя полагаться только на ORM auto-migrate.

## 10. Обязательные database/integration tests

- повторное назначение ранее отозванной роли;
- запрет двух активных ролей и двух активных pharmacy assignments;
- refresh rotation, token reuse и family revocation;
- global idempotency uniqueness при `pharmacy_id IS NULL`;
- параллельный idempotency replay и payload conflict;
- FEFO при конкурирующих продажах;
- полный rollback многострочной продажи при нехватке;
- параллельные возвраты одной allocation;
- согласованность return action и target lot;
- append-only movements и audit;
- создание нового alert после `RESOLVED` старого;
- reconciliation lot quantity против суммы movements;
- блокировка горизонтального доступа к чужой аптеке.

## 11. Открытые решения

До production необходимо отдельно утвердить:

1. юридические правила возврата лекарств;
2. discount allocation и rounding policy;
3. retention sessions, idempotency bodies, import files и audit;
4. password/session security policy;
5. формат и хранение рабочих часов аптеки;
6. стратегию полнотекстового/триграммного поиска каталога;
7. политику партиционирования audit при подтверждённом объёме;
8. необходимость отдельного карантинного lot вместо переиспользования исходного.

Документ считается реализованным только после появления миграций, repository-кода и обязательных integration/concurrency tests.