# PharmacyCRM — Database Design

**Статус документа:** Draft  
**Версия:** 2.1  
**Дата:** 2026-07-21  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `07-domain-model.md`, `09-security-design.md`, `10-sequence-diagrams.md`  
**Связанные ADR:** ADR-0009, ADR-0010, ADR-0011, ADR-0013, ADR-0016, ADR-0017, ADR-0018

## 1. Назначение и нормативная роль

Документ определяет конечную целевую PostgreSQL-модель PharmacyCRM для MVP. Он является источником истины по:

- module ownership таблиц;
- идентификаторам и типам данных;
- состояниям persisted entities;
- внешним ключам, уникальностям и ограничениям;
- append-only данным;
- идемпотентности;
- transactional outbox;
- audit;
- глобальному порядку блокировок;
- migration и database-test требованиям.

DDL ниже является проектным контрактом, а не одной монолитной migration. Реализация обязана разделить его на небольшие forward migrations с безопасной последовательностью `expand → migrate/backfill → validate → contract`.

При изменении таблицы, статуса, ограничения, индекса, lock order или ownership в том же change set обновляются Domain Model, API Design, Sequence Diagrams, tests и соответствующий ADR.

## 2. Базовые принципы

1. PostgreSQL является authoritative source для identity, authorization state, каталога, документов, остатков, идемпотентности, outbox и audit.
2. Остаток хранится как целое количество базовых единиц отпуска.
3. `stock_lots.quantity_base_units` является materialized operational balance; каждое изменение сопровождается append-only `inventory_movements` в той же транзакции.
4. Деньги хранятся в `bigint` как количество дирамов. `float` для денежных значений запрещён.
5. Исторические документы сохраняют snapshots цен, упаковочных коэффициентов, названий и иных расчётно значимых данных.
6. Проведённые документы, allocations, movements и audit не изменяются обычным CRUD и не удаляются штатным runtime-процессом.
7. Business effect, обязательный audit, outbox rows и completed idempotency result одной критической команды commit-ятся атомарно.
8. Access token не является источником актуального authorization state. Mutation повторно читает current user, session, role, assignment и pharmacy state внутри транзакции.
9. Базовая isolation model — `READ COMMITTED` с явными row locks, constraints и детерминированным порядком блокировок.
10. Внешний network side effect внутри transaction callback запрещён.
11. Публичные projection/read models не используются для проведения продажи или изменения authoritative inventory.
12. Customer-return mutation production-disabled утверждённым Gate E0 baseline; partial refund path дополнительно остаётся disabled до утверждения allocation/rounding rules.

## 3. Module ownership

| Module owner | Таблицы / данные |
|---|---|
| `identity` | `users`, `roles`, `user_roles`, `user_sessions` |
| `pharmacy` | `pharmacies`, `pharmacy_assignments` |
| `catalog` | `products`, `product_presentations`, `product_barcodes`, `product_requests`, `import_jobs`, `import_rows` |
| `assortment` | `pharmacy_products` |
| `inventory` | `inventory_operations`, `inventory_movements`, `stock_lots`, `receipts`, `receipt_items`, `write_offs`, `write_off_items`, `inventory_adjustments`, `inventory_adjustment_items` |
| `sales` | `sales`, `sale_items`, `sale_item_allocations` |
| `returns` | `sale_returns`, `sale_return_items`, `sale_return_item_allocations` |
| `reliability` | `idempotency_records`, `outbox_events` |
| `audit` | `audit_events` |
| `alerts` | `alerts` |
| `search` | `public_availability_projection` и другие rebuildable read models |
| `replenishment` | вычисляемые read models; отдельная authoritative mutation-таблица не требуется |

Отдельные backend modules `import`, `receipt` и `adjustments` не создаются. Catalog import принадлежит `catalog`; initial-stock confirmation и receipts принадлежат `inventory`; raw/quarantine storage является infrastructure concern.

`pharmacy_assignments` принадлежит `pharmacy`, поскольку моделирует связь пользователя с конкретной аптекой. `identity` предоставляет current user/role/session state, но не владеет assignment history.

Другой модуль не изменяет таблицы owner-а напрямую. Межмодульная atomic operation координируется Application/Orchestration через use-case-specific Unit of Work.

## 4. Общие DDL-соглашения

- Primary key: `uuid DEFAULT gen_random_uuid()`.
- Внешний API передаёт ID как строки и не обещает клиенту внутренний генератор или сортируемость.
- Время: `timestamptz`, формируемое сервером.
- Business date и expiration: `date`.
- Деньги: `bigint` с суффиксом `_dirams`.
- Количество: `bigint` с явным суффиксом `_base_units`, `_packages` и т. п.
- Статусы: `varchar` + именованный `CHECK`; PostgreSQL enum не используется.
- Mutable reference entities имеют `version bigint NOT NULL DEFAULT 1 CHECK (version > 0)`.
- Исторические связи используют `ON DELETE RESTRICT`.
- `ON DELETE CASCADE` не используется для проведённых документов, allocations, movements, audit и outbox.
- Nullable scope внутри uniqueness выражается generated `scope_key`, expression/partial index либо отдельным non-null полем.
- JSONB не заменяет поля, по которым строятся authorization, uniqueness, locks, status transitions, quantities или money calculations.
- Constraints получают стабильные имена.
- Runtime application role не получает unrestricted DDL и superuser privileges.

## 5. Целевой DDL

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =====================================================================
-- Identity
-- =====================================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login varchar(150) NOT NULL CHECK (btrim(login) <> ''),
    password_hash text NOT NULL CHECK (btrim(password_hash) <> ''),
    display_name varchar(255) NOT NULL CHECK (btrim(display_name) <> ''),
    phone varchar(50),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')),
    failed_login_attempts integer NOT NULL DEFAULT 0
        CHECK (failed_login_attempts >= 0),
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
ON users (lower(btrim(login)))
WHERE status <> 'ARCHIVED';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(50) NOT NULL UNIQUE
        CHECK (code IN ('CLIENT', 'PHARMACIST', 'ADMIN')),
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
        OR (
            revoked_at IS NOT NULL
            AND revoked_at >= assigned_at
            AND revoked_by_user_id IS NOT NULL
            AND btrim(revoke_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_user_single_active_role
ON user_roles (user_id)
WHERE revoked_at IS NULL;

CREATE INDEX idx_user_roles_history
ON user_roles (user_id, assigned_at DESC, id DESC);

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
    CONSTRAINT chk_session_rotation_self CHECK (
        rotated_from_session_id IS NULL OR rotated_from_session_id <> id
    ),
    CONSTRAINT chk_session_revocation CHECK (
        (revoked_at IS NULL AND revoke_reason IS NULL)
        OR (
            revoked_at IS NOT NULL
            AND revoked_at >= created_at
            AND btrim(revoke_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_user_session_rotated_from
ON user_sessions (rotated_from_session_id)
WHERE rotated_from_session_id IS NOT NULL;

CREATE INDEX idx_user_sessions_user_active
ON user_sessions (user_id, expires_at DESC, id DESC)
WHERE revoked_at IS NULL;

CREATE INDEX idx_user_sessions_family
ON user_sessions (token_family_id, created_at, id);

-- =====================================================================
-- Pharmacy and assignments
-- =====================================================================

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
        OR (
            ended_at IS NOT NULL
            AND ended_at >= assigned_at
            AND ended_by_user_id IS NOT NULL
            AND btrim(end_reason) <> ''
        )
    )
);

CREATE UNIQUE INDEX uq_pharmacy_assignment_active_user
ON pharmacy_assignments (user_id)
WHERE ended_at IS NULL;

CREATE INDEX idx_pharmacy_assignments_pharmacy_active
ON pharmacy_assignments (pharmacy_id, user_id)
WHERE ended_at IS NULL;

CREATE INDEX idx_pharmacy_assignments_history
ON pharmacy_assignments (user_id, assigned_at DESC, id DESC);

-- =====================================================================
-- Catalog and staging imports
-- =====================================================================

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
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_presentation_inner_unit CHECK (
        base_units_per_package = 1
        OR (inner_unit_name IS NOT NULL AND btrim(inner_unit_name) <> '')
    )
);

CREATE TABLE product_barcodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    barcode varchar(64) NOT NULL CHECK (btrim(barcode) <> ''),
    is_primary boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_product_barcodes_value_active
ON product_barcodes (barcode)
WHERE status = 'ACTIVE';

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
    resolved_product_presentation_id uuid
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolution_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT chk_product_request_resolution CHECK (
        (status = 'OPEN'
            AND resolved_product_presentation_id IS NULL
            AND resolved_by_user_id IS NULL
            AND resolved_at IS NULL
            AND resolution_note IS NULL)
        OR (status <> 'OPEN'
            AND resolved_by_user_id IS NOT NULL
            AND resolved_at IS NOT NULL
            AND btrim(resolution_note) <> '')
    ),
    CONSTRAINT chk_product_request_target CHECK (
        status NOT IN ('APPROVED', 'DUPLICATE')
        OR resolved_product_presentation_id IS NOT NULL
    )
);

CREATE TABLE import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_type varchar(50) NOT NULL
        CHECK (import_type IN ('CATALOG', 'INITIAL_STOCK')),
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    original_filename varchar(255) NOT NULL CHECK (btrim(original_filename) <> ''),
    content_type varchar(150) NOT NULL CHECK (btrim(content_type) <> ''),
    storage_key text NOT NULL CHECK (btrim(storage_key) <> ''),
    file_sha256 bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'UPLOADED'
        CHECK (status IN (
            'UPLOADED', 'VALIDATING', 'READY', 'HAS_ERRORS',
            'CONFIRMING', 'COMPLETED', 'FAILED'
        )),
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
    status varchar(30) NOT NULL
        CHECK (status IN (
            'PENDING', 'VALID', 'ERROR', 'MATCHED',
            'CREATE_NEW', 'REJECTED', 'PUBLISHED'
        )),
    matched_product_presentation_id uuid
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_import_row_number UNIQUE (import_job_id, row_number),
    CONSTRAINT chk_import_row_match CHECK (
        status <> 'MATCHED' OR matched_product_presentation_id IS NOT NULL
    )
);

CREATE INDEX idx_import_rows_status
ON import_rows (import_job_id, status, row_number);

-- =====================================================================
-- Assortment and inventory
-- =====================================================================

CREATE TABLE pharmacy_products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    is_inner_unit_sale_allowed boolean NOT NULL DEFAULT false,
    default_package_price_dirams bigint NOT NULL
        CHECK (default_package_price_dirams >= 0),
    default_inner_unit_price_dirams bigint
        CHECK (default_inner_unit_price_dirams >= 0),
    min_stock_level_base_units bigint NOT NULL DEFAULT 0
        CHECK (min_stock_level_base_units >= 0),
    target_stock_level_base_units bigint NOT NULL DEFAULT 0
        CHECK (target_stock_level_base_units >= 0),
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
    CONSTRAINT chk_stock_targets CHECK (
        target_stock_level_base_units >= min_stock_level_base_units
    )
);

CREATE TABLE inventory_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_type varchar(50) NOT NULL
        CHECK (operation_type IN (
            'RECEIPT', 'INITIAL_STOCK', 'SALE', 'RETURN_TO_STOCK',
            'RETURN_WRITE_OFF', 'RETURN_QUARANTINE', 'WRITE_OFF',
            'INVENTORY_ADJUSTMENT', 'REVERSAL'
        )),
    status varchar(30) NOT NULL DEFAULT 'POSTED'
        CHECK (status IN ('POSTED', 'REVERSED')),
    initiated_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    is_system_initiated boolean NOT NULL DEFAULT false,
    reversal_of_operation_id uuid
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
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
    CONSTRAINT chk_inventory_reversal_self CHECK (
        reversal_of_operation_id IS NULL OR reversal_of_operation_id <> id
    )
);

CREATE UNIQUE INDEX uq_inventory_reversal
ON inventory_operations (reversal_of_operation_id)
WHERE reversal_of_operation_id IS NOT NULL;

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    source_import_job_id uuid REFERENCES import_jobs(id) ON DELETE RESTRICT,
    receipt_number varchar(100) NOT NULL CHECK (btrim(receipt_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'POSTED'
        CHECK (status IN ('POSTED', 'REVERSED')),
    supplier_name varchar(255),
    received_at timestamptz NOT NULL,
    posted_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    posted_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_receipt_number_per_pharmacy
        UNIQUE (pharmacy_id, receipt_number)
);

CREATE TABLE receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id uuid NOT NULL REFERENCES receipts(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_packages bigint NOT NULL CHECK (quantity_packages > 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    purchase_price_package_dirams bigint NOT NULL
        CHECK (purchase_price_package_dirams >= 0),
    retail_price_package_dirams bigint NOT NULL
        CHECK (retail_price_package_dirams >= 0),
    retail_price_inner_unit_dirams bigint
        CHECK (retail_price_inner_unit_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_receipt_quantity CHECK (
        quantity_base_units = quantity_packages * base_units_per_package_snapshot
    )
);

CREATE TABLE stock_lots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    receipt_item_id uuid REFERENCES receipt_items(id) ON DELETE RESTRICT,
    source_return_allocation_id uuid,
    origin_type varchar(30) NOT NULL
        CHECK (origin_type IN ('RECEIPT', 'INITIAL_STOCK', 'RETURN')),
    batch_number varchar(100) NOT NULL CHECK (btrim(batch_number) <> ''),
    expiration_date date NOT NULL,
    quantity_base_units bigint NOT NULL DEFAULT 0
        CHECK (quantity_base_units >= 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    purchase_price_package_dirams bigint
        CHECK (purchase_price_package_dirams >= 0),
    package_retail_price_dirams bigint NOT NULL
        CHECK (package_retail_price_dirams >= 0),
    inner_unit_retail_price_dirams bigint
        CHECK (inner_unit_retail_price_dirams >= 0),
    received_at timestamptz NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE'
        CHECK (status IN ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'ARCHIVED')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_lot_origin_reference CHECK (
        (origin_type IN ('RECEIPT', 'INITIAL_STOCK')
            AND receipt_item_id IS NOT NULL
            AND source_return_allocation_id IS NULL)
        OR (origin_type = 'RETURN'
            AND receipt_item_id IS NULL
            AND source_return_allocation_id IS NOT NULL)
    ),
    CONSTRAINT chk_lot_status_quantity CHECK (
        status <> 'DEPLETED' OR quantity_base_units = 0
    )
);

CREATE UNIQUE INDEX uq_stock_lot_receipt_item
ON stock_lots (receipt_item_id)
WHERE receipt_item_id IS NOT NULL;

CREATE UNIQUE INDEX uq_stock_lot_source_return
ON stock_lots (source_return_allocation_id)
WHERE source_return_allocation_id IS NOT NULL;

CREATE INDEX idx_stock_lots_fefo
ON stock_lots (
    pharmacy_product_id,
    expiration_date,
    received_at,
    id
)
WHERE status = 'ACTIVE' AND quantity_base_units > 0;

CREATE TABLE inventory_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id uuid NOT NULL
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    quantity_after_base_units bigint NOT NULL
        CHECK (quantity_after_base_units >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_movement_operation_lot
        UNIQUE (operation_id, stock_lot_id)
);

CREATE INDEX idx_inventory_movements_lot_history
ON inventory_movements (stock_lot_id, created_at, id);

-- =====================================================================
-- Sales
-- =====================================================================

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    sale_number varchar(100) NOT NULL CHECK (btrim(sale_number) <> ''),
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN (
            'COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REVERSED'
        )),
    payment_method varchar(50) NOT NULL
        CHECK (payment_method IN ('CASH', 'CARD', 'MOBILE', 'MIXED')),
    subtotal_amount_dirams bigint NOT NULL
        CHECK (subtotal_amount_dirams >= 0),
    discount_amount_dirams bigint NOT NULL DEFAULT 0
        CHECK (discount_amount_dirams >= 0),
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
    pharmacy_product_id uuid NOT NULL
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    product_title_snapshot varchar(255) NOT NULL,
    presentation_snapshot varchar(255) NOT NULL,
    sale_unit varchar(30) NOT NULL
        CHECK (sale_unit IN ('PACKAGE', 'INNER_UNIT')),
    display_quantity bigint NOT NULL CHECK (display_quantity > 0),
    base_units_per_package_snapshot bigint NOT NULL
        CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    unit_price_dirams bigint NOT NULL CHECK (unit_price_dirams >= 0),
    line_subtotal_dirams bigint NOT NULL CHECK (line_subtotal_dirams >= 0),
    line_discount_dirams bigint NOT NULL DEFAULT 0
        CHECK (line_discount_dirams >= 0),
    line_total_dirams bigint NOT NULL CHECK (line_total_dirams >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_product_unit
        UNIQUE (sale_id, pharmacy_product_id, sale_unit),
    CONSTRAINT chk_sale_item_total CHECK (
        line_subtotal_dirams = display_quantity * unit_price_dirams
        AND line_total_dirams = line_subtotal_dirams - line_discount_dirams
        AND line_discount_dirams <= line_subtotal_dirams
    ),
    CONSTRAINT chk_sale_item_quantity CHECK (
        (sale_unit = 'PACKAGE'
            AND quantity_base_units = display_quantity * base_units_per_package_snapshot)
        OR (sale_unit = 'INNER_UNIT'
            AND quantity_base_units = display_quantity)
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

CREATE INDEX idx_sale_item_allocations_lot
ON sale_item_allocations (stock_lot_id, sale_item_id);

-- =====================================================================
-- Returns
-- =====================================================================

CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    operation_id uuid UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    returned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    returned_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_sale_returns_sale
ON sale_returns (sale_id, returned_at, id);

CREATE TABLE sale_return_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_id uuid NOT NULL
        REFERENCES sale_returns(id) ON DELETE RESTRICT,
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL
        CHECK (returned_quantity_base_units > 0),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    return_action varchar(50) NOT NULL
        CHECK (return_action IN (
            'RESTOCK', 'WRITE_OFF', 'QUARANTINE', 'NO_PHYSICAL_RETURN'
        )),
    item_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item UNIQUE (sale_return_id, sale_item_id),
    CONSTRAINT chk_sale_return_item_reason CHECK (
        item_reason IS NULL OR btrim(item_reason) <> ''
    )
);

CREATE INDEX idx_sale_return_items_sale_item
ON sale_return_items (sale_item_id);

CREATE TABLE sale_return_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_item_id uuid NOT NULL
        REFERENCES sale_return_items(id) ON DELETE RESTRICT,
    sale_item_allocation_id uuid NOT NULL
        REFERENCES sale_item_allocations(id) ON DELETE RESTRICT,
    target_stock_lot_id uuid
        REFERENCES stock_lots(id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
    returned_quantity_base_units bigint NOT NULL
        CHECK (returned_quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item_allocation
        UNIQUE (sale_return_item_id, sale_item_allocation_id)
);

CREATE INDEX idx_return_allocations_source
ON sale_return_item_allocations (
    sale_item_allocation_id,
    sale_return_item_id
);

ALTER TABLE stock_lots
ADD CONSTRAINT fk_stock_lot_source_return_allocation
FOREIGN KEY (source_return_allocation_id)
REFERENCES sale_return_item_allocations(id)
ON DELETE RESTRICT
DEFERRABLE INITIALLY DEFERRED;

-- =====================================================================
-- Write-offs and inventory adjustments
-- =====================================================================

CREATE TABLE write_offs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
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
    operation_id uuid NOT NULL UNIQUE
        REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED'
        CHECK (status IN ('COMPLETED', 'REVERSED')),
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
    inventory_adjustment_id uuid NOT NULL
        REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    expected_quantity_base_units bigint NOT NULL
        CHECK (expected_quantity_base_units >= 0),
    actual_quantity_base_units bigint NOT NULL
        CHECK (actual_quantity_base_units >= 0),
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_adjustment_lot
        UNIQUE (inventory_adjustment_id, stock_lot_id),
    CONSTRAINT chk_inventory_adjustment_delta CHECK (
        delta_base_units = actual_quantity_base_units - expected_quantity_base_units
    )
);

-- =====================================================================
-- Reliability: idempotency and outbox
-- =====================================================================

CREATE TABLE idempotency_records (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation varchar(150) NOT NULL CHECK (btrim(operation) <> ''),
    idempotency_key varchar(128) NOT NULL CHECK (btrim(idempotency_key) <> ''),
    scope_key text GENERATED ALWAYS AS (
        actor_user_id::text
        || ':' || operation
        || ':' || coalesce(pharmacy_id::text, 'GLOBAL')
    ) STORED,
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
        (status = 'COMPLETED'
            AND response_status IS NOT NULL
            AND response_body IS NOT NULL
            AND completed_at IS NOT NULL)
        OR (status = 'IN_PROGRESS'
            AND response_status IS NULL
            AND response_body IS NULL
            AND completed_at IS NULL)
        OR (status = 'FAILED_RETRYABLE'
            AND completed_at IS NOT NULL)
    ),
    CONSTRAINT chk_idempotency_expiry CHECK (expires_at > created_at)
);

CREATE INDEX idx_idempotency_expiration
ON idempotency_records (expires_at);

CREATE INDEX idx_idempotency_resource
ON idempotency_records (resource_type, resource_id)
WHERE resource_id IS NOT NULL;

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    event_name varchar(150) NOT NULL CHECK (btrim(event_name) <> ''),
    event_version smallint NOT NULL DEFAULT 1 CHECK (event_version > 0),
    aggregate_type varchar(100) NOT NULL CHECK (btrim(aggregate_type) <> ''),
    aggregate_id uuid NOT NULL,
    partition_key varchar(200) NOT NULL CHECK (btrim(partition_key) <> ''),
    deduplication_key varchar(255) NOT NULL UNIQUE CHECK (btrim(deduplication_key) <> ''),
    payload jsonb NOT NULL,
    headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(30) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'PROCESSING', 'PROCESSED', 'DEAD_LETTER')),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts smallint NOT NULL DEFAULT 8 CHECK (max_attempts BETWEEN 1 AND 20),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    leased_by varchar(150),
    lease_expires_at timestamptz,
    last_error_code varchar(100),
    last_error_at timestamptz,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    dead_lettered_at timestamptz,
    CONSTRAINT chk_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_outbox_payload_size CHECK (octet_length(payload::text) <= 262144),
    CONSTRAINT chk_outbox_lease CHECK ((status = 'PROCESSING' AND lease_token IS NOT NULL AND leased_by IS NOT NULL AND lease_expires_at IS NOT NULL) OR (status <> 'PROCESSING' AND lease_token IS NULL AND leased_by IS NULL AND lease_expires_at IS NULL)),
    CONSTRAINT chk_outbox_terminal CHECK ((status = 'PROCESSED' AND processed_at IS NOT NULL AND dead_lettered_at IS NULL) OR (status = 'DEAD_LETTER' AND dead_lettered_at IS NOT NULL AND processed_at IS NULL) OR (status IN ('PENDING', 'PROCESSING') AND processed_at IS NULL AND dead_lettered_at IS NULL))
);
CREATE INDEX idx_outbox_claim ON outbox_events (available_at, created_at, id) WHERE status = 'PENDING';
CREATE INDEX idx_outbox_processing_lease ON outbox_events (lease_expires_at, id) WHERE status = 'PROCESSING';
CREATE INDEX idx_outbox_partition ON outbox_events (partition_key, occurred_at, id);
CREATE INDEX idx_outbox_aggregate ON outbox_events (aggregate_type, aggregate_id, occurred_at, id);

-- =====================================================================
-- Audit
-- =====================================================================

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
        OR (actor_type = 'SYSTEM'
            AND actor_user_id IS NULL
            AND actor_session_id IS NULL)
    ),
    CONSTRAINT chk_audit_session_actor CHECK (
        actor_session_id IS NULL OR actor_user_id IS NOT NULL
    )
);

CREATE INDEX idx_audit_events_time
ON audit_events (occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_actor
ON audit_events (actor_user_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_pharmacy
ON audit_events (pharmacy_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_object
ON audit_events (object_type, object_id, occurred_at DESC, id DESC);

CREATE INDEX idx_audit_events_request
ON audit_events (request_id)
WHERE request_id IS NOT NULL;

-- =====================================================================
-- Alerts and public projection
-- =====================================================================

CREATE TABLE alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    alert_type varchar(50) NOT NULL
        CHECK (alert_type IN (
            'LOW_STOCK', 'EXPIRED', 'EXPIRING_7_DAYS',
            'EXPIRING_30_DAYS', 'RECONCILIATION_MISMATCH'
        )),
    deduplication_key varchar(255) NOT NULL
        CHECK (btrim(deduplication_key) <> ''),
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
        OR (status <> 'RESOLVED'
            AND resolved_at IS NULL
            AND resolved_by_user_id IS NULL)
    )
);

CREATE UNIQUE INDEX uq_alert_open_dedup
ON alerts (pharmacy_id, deduplication_key)
WHERE status IN ('ACTIVE', 'ACKNOWLEDGED');

CREATE INDEX idx_alerts_pharmacy_status
ON alerts (pharmacy_id, status, detected_at DESC, id DESC);

CREATE TABLE public_availability_projection (
    pharmacy_product_id uuid PRIMARY KEY
        REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL
        REFERENCES product_presentations(id) ON DELETE RESTRICT,
    package_price_dirams bigint NOT NULL CHECK (package_price_dirams >= 0),
    inner_unit_price_dirams bigint CHECK (inner_unit_price_dirams >= 0),
    availability_status varchar(30) NOT NULL
        CHECK (availability_status IN ('AVAILABLE', 'LOW_STOCK', 'UNAVAILABLE')),
    source_inventory_changed_at timestamptz NOT NULL,
    projected_at timestamptz NOT NULL,
    projection_version bigint NOT NULL CHECK (projection_version > 0)
);

CREATE INDEX idx_public_availability_search
ON public_availability_projection (
    product_presentation_id,
    availability_status,
    package_price_dirams,
    pharmacy_id
);
```

## 6. Межтабличные и транзакционные инварианты

### 6.1 Identity и authorization

1. Current role — единственная `user_roles` row с `revoked_at IS NULL`.
2. Пользователь имеет не более одной active role.
3. `PHARMACIST` имеет не более одного active pharmacy assignment в MVP.
4. Assignment создаётся только active пользователю с current role `PHARMACIST` и active pharmacy.
5. Assignment lifecycle: `ACTIVE → ENDED`; окончание задаётся `ended_at`, `ended_by_user_id`, `end_reason`.
6. Актуальность доступа определяется current user/session/role/assignment/pharmacy records.
7. Refresh rotation создаёт новую session и отзывает/помечает использованной предыдущую в одной транзакции.
8. Reuse старого refresh token отзывает всю token family.
9. Block/archive/password change/role revoke отзывают применимые sessions согласно Security Design.

### 6.2 Imports

1. `CATALOG` import имеет global scope; `INITIAL_STOCK` всегда имеет `pharmacy_id`.
2. Persisted states: `UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`.
3. Любые transport-only job labels не являются persisted `ImportJob` states и не расширяют этот enum.
4. Worker parsing выполняется вне долгой DB transaction; batch persistence ограничена.
5. Публикация catalog rows и confirmation initial stock выполняются явными application commands с authorization, idempotency и audit.
6. `INITIAL_STOCK` confirmation создаёт `Receipt`, lots, movements и operation через `inventory`.

### 6.3 Inventory

1. `receipt_items.pharmacy_product_id` относится к pharmacy receipt.
2. Receipt operation имеет type `RECEIPT` или `INITIAL_STOCK` и ту же pharmacy.
3. Каждый receipt item создаёт ровно один source lot.
4. Stock lot не создаётся произвольным CRUD endpoint. Он появляется только из receipt/initial stock либо разрешённого return workflow.
5. `quantity_after_base_units` каждого movement совпадает с resulting materialized lot balance.
6. Reconciliation formula для lot:

```text
initial zero balance + SUM(inventory_movements.delta_base_units)
= stock_lots.quantity_base_units
```

7. Просроченный или непригодный lot не становится sellable `ACTIVE`.
8. `inventory_changed_at` обновляется в той же транзакции, что физическое изменение stock.

### 6.4 Sales и FEFO

1. Все sale items принадлежат pharmacy sale.
2. Sale operation имеет type `SALE` и ту же pharmacy.
3. Server блокирует `pharmacy_products` по `id`, затем lots по `expiration_date`, `received_at`, `id`.
4. После locks повторно проверяются assortment status, prices, sellability и quantity.
5. Сумма allocations строки равна `sale_items.quantity_base_units`.
6. Недостаток любой строки отклоняет всю sale.
7. Client price, total и lot selection не являются authoritative.
8. Prescription-required item требует server-side policy check и audit.

### 6.5 Returns

1. Return относится к одной sale той же pharmacy.
2. Return item относится к item этой sale.
3. Return allocation относится к source allocation той же item.
4. Cumulative completed non-reversed returned quantity по source allocation не превышает sold quantity.
5. Сумма return allocations равна returned quantity item.
6. `ReturnAction`: `RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`.
7. `RETURN_TO_STOCK`, `RETURN_WRITE_OFF`, `RETURN_QUARANTINE` — `InventoryOperation` types, а не `ReturnAction`.
8. Только `RESTOCK` увеличивает sellable stock.
9. `QUARANTINE` может иметь target lot, но не увеличивает sellable balance.
10. Separate return lot требует provenance через `source_return_allocation_id`; обе стороны circular relation создаются с pre-generated IDs и deferred constraints.
11. Refund рассчитывается backend по immutable sale snapshots.
12. Sale status использует `COMPLETED`, `PARTIALLY_REFUNDED`, `REFUNDED`, `REVERSED`.
13. Customer-return command production-disabled утверждённым legal baseline; partial refund execution дополнительно требует утверждённых allocation/rounding rules.

### 6.6 Idempotency

1. Полная identity:

```text
actor + operation + effective_scope + idempotency_key
```

2. Для pharmacy command `effective_scope = pharmacy_id`; для global/admin command — `GLOBAL`.
3. Semantic fingerprint включает path/resource IDs, effective scope, применимую resource version и смысловой payload.
4. Request ID, JSON key order и transport-only metadata не входят в fingerprint.
5. Same identity + same fingerprint возвращает committed result после current authorization/visibility revalidation.
6. Same identity + different fingerprint возвращает conflict.
7. `COMPLETED` и business effect commit-ятся атомарно.
8. `FAILED_RETRYABLE` разрешён только при доказанном отсутствии committed business effect.

### 6.7 Transactional outbox

1. Outbox row создаётся в business transaction до commit.
2. Delivery semantics — at-least-once.
3. Claim использует bounded batch и `FOR UPDATE SKIP LOCKED`.
4. Worker переводит row в `PROCESSING` с уникальным `lease_token`.
5. Completion/retry выполняется guarded update по `id + lease_token`; stale worker не завершает событие за нового owner-а.
6. Side effect выполняется вне transaction claim-а.
7. Consumer обязан быть idempotent; irreversible external operation использует provider idempotency key или отдельный dedup protocol.
8. Exhausted attempts переводят event в `DEAD_LETTER` и создают operational signal.
9. Projection имеет rebuild/reconciliation path.

### 6.8 Audit

1. Mandatory audit критической mutation находится в той же transaction.
2. Audit failure приводит к rollback business effect.
3. `USER` event требует actor user; session при наличии принадлежит этому user.
4. `SYSTEM` event не маскируется под user/admin.
5. Metadata не содержит password, raw token, secret, unrestricted payload, SQL или stack trace.
6. Audit append-only; исправление представлено новым событием.

## 7. Канонический порядок блокировок

Для use cases, затрагивающих одинаковые ресурсы:

1. idempotency scope/key;
2. current actor user/session/role;
3. target user, если он изменяется;
4. pharmacy;
5. root business document;
6. `pharmacy_products` по `id`;
7. sale items/source allocations по `id`;
8. stock lots по `expiration_date`, `received_at`, `id` либо более узкому документированному порядку, совместимому с этим порядком;
9. append-only inserts: documents, allocations, movements, audit, outbox, completed idempotency result;
10. commit.

Use case пропускает ненужный уровень, но не меняет взаимный порядок остальных locks.

Первым сериализующим lock authenticated critical mutation является idempotency record. После него current authorization revalidation выполняется до business mutation. Pre-lock read не является основанием для final stock, price, authorization или return decision.

## 8. Runtime database privileges

Production runtime role:

- не является owner/superuser;
- не создаёт schema/role/extension;
- не выполняет unrestricted DDL;
- не получает обычный `DELETE` для business history;
- не получает обычный `UPDATE/DELETE` для `inventory_movements` и `audit_events`;
- изменяет outbox только через narrow repository operations;
- не обходит constraints и transaction protocol.

Migration role отделена от runtime role. Backup и read-only diagnostics используют отдельные credentials.

Retention cleanup допускается только для утверждённых temporary/security/technical данных. Business documents, allocations, movements и audit не удаляются обычным worker-ом.

## 9. Migration requirements

1. Одна migration решает одну логическую задачу.
2. Migration имеет forward strategy, verification query и rollback/forward-fix policy.
3. Destructive down migration по умолчанию запрещена.
4. `NOT NULL` на существующих данных вводится через nullable column, backfill, validation и только затем contract.
5. Большие indexes создаются с lock/WAL/disk plan; `CONCURRENTLY` используется, где применимо.
6. Constraint может добавляться `NOT VALID`, затем проверяться отдельно, если это уменьшает production risk.
7. Seed roles идемпотентен.
8. Status/check change синхронизируется с Domain/API/tests.
9. Outbox и idempotency migrations появляются до бизнес-команд, которые от них зависят.
10. Application startup не применяет production migrations автоматически.
11. Migration rehearsal выполняется на production-like volume с измерением duration, locks, WAL и disk growth.
12. Contract migration запускается только после исчезновения старых readers/writers.

## 10. Обязательные database/integration tests

- migration from zero;
- upgrade с предыдущей schema version;
- role seed replay;
- запрет двух active roles и assignments;
- refresh rotation, reuse и family revocation;
- global idempotency uniqueness при `pharmacy_id IS NULL`;
- same-key concurrent replay и payload conflict;
- replay после authorization revoke;
- FEFO при конкурирующих sales;
- multi-line sale full rollback;
- negative stock constraint;
- movement/balance reconciliation;
- partial и конкурентные returns одной source allocation;
- separate return lot deferred-FK transaction;
- return actions без неправильного sellable increase;
- sale status `COMPLETED → PARTIALLY_REFUNDED → REFUNDED`;
- audit failure rollback;
- outbox failure rollback;
- two-worker claim race;
- stale lease guarded completion;
- dead-letter transition;
- runtime role cannot mutate immutable rows;
- public projection cannot be used as command source;
- horizontal pharmacy-scope denial.

## 11. Remaining non-E0 decisions
1. Catalog import atomic/partial publication semantics.
2. ETag/resource-version transport policy.
3. Exact elevated approval model for adjustments/reversals.
4. Production search implementation after measured PostgreSQL limits.
5. Audit partitioning only after measured volume.
6. Jurisdiction-specific retention extension and legal hold procedure.
Эти вопросы не разрешают альтернативный module ownership, transaction order, outbox protocol, token transport или frontend tooling path.
