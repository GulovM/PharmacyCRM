# PharmacyCRM — Database Design

**Статус документа:** Draft  
**Версия:** 1.0  
**Дата:** 2026-07-17  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`  
**Связанные ADR:** ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет целевую PostgreSQL-модель PharmacyCRM. Он объединяет identity, роли, назначения аптекарей, сессии, глобальный каталог, аптеки, ассортимент, поступления, лоты, продажи, возвраты, списания, корректировки, идемпотентность и аудит.

DDL является проектным контрактом, а не одной готовой migration. Реализация должна разложить его на последовательные миграции, сохранив ограничения, индексы, внешние ключи, ownership модулей и транзакционные инварианты.

`docs/06-01-database-design-return-allocations.md` полностью интегрирован в эту редакцию. Основным источником истины по возвратным аллокациям является настоящий документ.

## 2. Базовые принципы

1. PostgreSQL является источником истины для identity, authorization state, каталога, остатков, документов, идемпотентности и аудита.
2. Остаток хранится целым числом базовых единиц отпуска.
3. Любое изменение остатка сопровождается append-only записью в `inventory_movements`.
4. Текущий остаток, движение, бизнес-документ, idempotency record и обязательный audit event фиксируются атомарно, если принадлежат одной критической операции.
5. Деньги хранятся как `bigint` в дирамах. `float` для денег запрещён.
6. Исторические документы сохраняют snapshots цен, коэффициентов упаковки и иных данных, влияющих на расчёт.
7. Проведённые документы, движения и audit events не удаляются и не редактируются как обычные CRUD-ресурсы.
8. Все ссылки на пользователей сохраняются через FK с `ON DELETE RESTRICT`; пользователь архивируется логически.
9. Auth token не является источником актуального authorization state: backend повторно проверяет пользователя, роль, сессию и назначение.
10. Идемпотентность критических команд реализуется общей таблицей, а не уникальным полем в каждом бизнес-документе.
11. Транзакции используют `READ COMMITTED` и явные детерминированные блокировки согласно ADR-0011 и ADR-0013.
12. Внешние сетевые вызовы внутри транзакционного callback запрещены.

## 3. Схемы и module ownership

На первом этапе допускается единая PostgreSQL schema `public`, однако логическое владение таблицами остаётся модульным.

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

Один модуль не должен напрямую изменять таблицы другого модуля в обход его repository/application contract. Межмодульные операции выполняются через Unit of Work.

## 4. Концептуальная модель

```text
users ──< user_roles >── roles
  │
  ├──< user_sessions
  ├──< pharmacy_assignments >── pharmacies
  ├──< audit_events
  └──< idempotency_records

products
└── product_presentations
    └── product_barcodes

pharmacies
└── pharmacy_products
    └── stock_lots
        └── inventory_movements
            └── inventory_operations

receipts
└── receipt_items
    └── stock_lots

sales
└── sale_items
    └── sale_item_allocations
        └── stock_lots

sale_returns
└── sale_return_items
    └── sale_return_item_allocations
        └── sale_item_allocations
```

## 5. Общие соглашения DDL

- Primary keys: `uuid DEFAULT gen_random_uuid()`.
- Внешние ID API передаются как строки; внутренний тип UUID не является частью HTTP-контракта.
- Время: `timestamptz`, создаваемое сервером.
- Дата срока годности: `date`.
- Статусы: `varchar` с `CHECK`; PostgreSQL enum не используется, чтобы миграции статусов оставались управляемыми.
- Для изменяемых справочников используется `version bigint NOT NULL DEFAULT 1` для optimistic concurrency.
- `updated_at` обновляется application layer или общим trigger, выбранным единообразно для проекта.
- Все строки, используемые как причины, проходят `CHECK (btrim(value) <> '')`.
- `ON DELETE CASCADE` не применяется к бизнес-истории. Для технических дочерних session data допустимо удаление только отдельной retention migration.

## 6. Проектный DDL

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ================================================================
-- Identity and authorization
-- ================================================================

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    login varchar(150) NOT NULL,
    password_hash text NOT NULL,
    display_name varchar(255) NOT NULL,
    phone varchar(50),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')
    ),
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
        (status <> 'BLOCKED' OR blocked_at IS NOT NULL)
        AND (status <> 'ARCHIVED' OR archived_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_users_login_active
ON users (lower(login))
WHERE status <> 'ARCHIVED';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code varchar(50) NOT NULL UNIQUE CHECK (code IN ('CLIENT', 'PHARMACIST', 'ADMIN')),
    name varchar(100) NOT NULL,
    description text,
    is_system boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT chk_user_role_dates CHECK (revoked_at IS NULL OR revoked_at >= assigned_at)
);

CREATE UNIQUE INDEX uq_user_single_active_role
ON user_roles (user_id)
WHERE revoked_at IS NULL;

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    refresh_token_hash bytea NOT NULL,
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
    CONSTRAINT chk_session_revocation CHECK (
        (revoked_at IS NULL AND revoke_reason IS NULL)
        OR (revoked_at IS NOT NULL AND revoke_reason IS NOT NULL)
    )
);

CREATE UNIQUE INDEX uq_user_sessions_refresh_hash ON user_sessions (refresh_token_hash);
CREATE INDEX idx_user_sessions_user_active ON user_sessions (user_id, expires_at DESC)
WHERE revoked_at IS NULL;
CREATE INDEX idx_user_sessions_family ON user_sessions (token_family_id);

-- ================================================================
-- Pharmacy and assignments
-- ================================================================

CREATE TABLE pharmacies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(255) NOT NULL,
    address text NOT NULL,
    landmark text,
    latitude numeric(9,6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude numeric(9,6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    phone varchar(50),
    working_hours varchar(255),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);

CREATE TABLE pharmacy_assignments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    assigned_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz,
    end_reason text,
    CONSTRAINT chk_assignment_end CHECK (
        (ended_at IS NULL AND end_reason IS NULL)
        OR (ended_at IS NOT NULL AND ended_at >= assigned_at AND btrim(end_reason) <> '')
    )
);

CREATE UNIQUE INDEX uq_pharmacy_assignment_active_user
ON pharmacy_assignments (user_id)
WHERE ended_at IS NULL;

CREATE INDEX idx_pharmacy_assignments_pharmacy_active
ON pharmacy_assignments (pharmacy_id, user_id)
WHERE ended_at IS NULL;

-- ================================================================
-- Global catalog
-- ================================================================

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title varchar(255) NOT NULL,
    inn varchar(255),
    dosage varchar(100),
    form varchar(100) NOT NULL,
    manufacturer varchar(255) NOT NULL,
    country varchar(100),
    is_prescription_required boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_products_search_title ON products (lower(title));
CREATE INDEX idx_products_search_inn ON products (lower(inn));

CREATE TABLE product_presentations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id uuid NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    package_name varchar(100) NOT NULL,
    inner_unit_name varchar(100),
    base_units_per_package bigint NOT NULL CHECK (base_units_per_package > 0),
    package_description text,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_presentation_inner_unit CHECK (
        base_units_per_package = 1 OR inner_unit_name IS NOT NULL
    )
);

CREATE INDEX idx_presentations_product ON product_presentations (product_id, status, id);

CREATE TABLE product_barcodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
    barcode varchar(100) NOT NULL UNIQUE,
    barcode_type varchar(30) NOT NULL DEFAULT 'EAN13',
    is_primary boolean NOT NULL DEFAULT false,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'ARCHIVED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_product_barcodes_primary
ON product_barcodes (product_presentation_id)
WHERE is_primary = true AND status = 'ACTIVE';

CREATE TABLE product_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    requested_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    raw_name varchar(255) NOT NULL,
    raw_details jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(30) NOT NULL DEFAULT 'OPEN' CHECK (
        status IN ('OPEN', 'APPROVED', 'REJECTED', 'DUPLICATE')
    ),
    resolved_product_presentation_id uuid REFERENCES product_presentations(id) ON DELETE RESTRICT,
    resolved_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    resolution_note text,
    created_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

-- ================================================================
-- Import jobs and staging
-- ================================================================

CREATE TABLE import_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    import_type varchar(50) NOT NULL CHECK (
        import_type IN ('CATALOG', 'INITIAL_STOCK')
    ),
    pharmacy_id uuid REFERENCES pharmacies(id) ON DELETE RESTRICT,
    created_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    original_filename varchar(255) NOT NULL,
    content_type varchar(150) NOT NULL,
    storage_key text NOT NULL,
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
    CONSTRAINT uq_import_row_number UNIQUE (import_job_id, row_number)
);

CREATE INDEX idx_import_rows_status ON import_rows (import_job_id, status, row_number);

-- ================================================================
-- Pharmacy assortment
-- ================================================================

CREATE TABLE pharmacy_products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
    is_inner_unit_sale_allowed boolean NOT NULL DEFAULT false,
    default_package_price_dirams bigint NOT NULL CHECK (default_package_price_dirams >= 0),
    default_inner_unit_price_dirams bigint CHECK (
        default_inner_unit_price_dirams IS NULL OR default_inner_unit_price_dirams >= 0
    ),
    min_stock_level_base_units bigint NOT NULL DEFAULT 0 CHECK (min_stock_level_base_units >= 0),
    target_stock_level_base_units bigint NOT NULL DEFAULT 0 CHECK (target_stock_level_base_units >= 0),
    inventory_changed_at timestamptz,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'INACTIVE', 'ARCHIVED')
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_pharmacy_product UNIQUE (pharmacy_id, product_presentation_id),
    CONSTRAINT chk_inner_unit_price CHECK (
        NOT is_inner_unit_sale_allowed OR default_inner_unit_price_dirams IS NOT NULL
    ),
    CONSTRAINT chk_stock_targets CHECK (
        target_stock_level_base_units >= min_stock_level_base_units
    )
);

-- ================================================================
-- Inventory operations, receipts and lots
-- ================================================================

CREATE TABLE inventory_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_type varchar(50) NOT NULL CHECK (
        operation_type IN (
            'RECEIPT', 'INITIAL_STOCK', 'SALE', 'RETURN_TO_STOCK',
            'RETURN_WRITE_OFF', 'RETURN_QUARANTINE', 'WRITE_OFF',
            'INVENTORY_ADJUSTMENT', 'REVERSAL'
        )
    ),
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (
        status IN ('POSTED', 'REVERSED')
    ),
    initiated_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    is_system_initiated boolean NOT NULL DEFAULT false,
    reversal_of_operation_id uuid REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_inventory_actor CHECK (
        initiated_by_user_id IS NOT NULL OR is_system_initiated
    ),
    CONSTRAINT chk_inventory_reversal CHECK (
        (operation_type = 'REVERSAL' AND reversal_of_operation_id IS NOT NULL)
        OR (operation_type <> 'REVERSAL' AND reversal_of_operation_id IS NULL)
    )
);

CREATE UNIQUE INDEX uq_inventory_reversal
ON inventory_operations (reversal_of_operation_id)
WHERE reversal_of_operation_id IS NOT NULL;

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    source_import_job_id uuid REFERENCES import_jobs(id) ON DELETE RESTRICT,
    receipt_number varchar(100) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (status IN ('POSTED', 'REVERSED')),
    supplier_name varchar(255),
    received_at timestamptz NOT NULL,
    posted_by_user_id uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    posted_at timestamptz NOT NULL,
    CONSTRAINT uq_receipt_number_per_pharmacy UNIQUE (pharmacy_id, receipt_number)
);

CREATE TABLE receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id uuid NOT NULL REFERENCES receipts(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    batch_number varchar(100) NOT NULL,
    expiration_date date NOT NULL,
    quantity_packages bigint NOT NULL CHECK (quantity_packages > 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    purchase_price_package_dirams bigint NOT NULL CHECK (purchase_price_package_dirams >= 0),
    retail_price_package_dirams bigint NOT NULL CHECK (retail_price_package_dirams >= 0),
    retail_price_inner_unit_dirams bigint CHECK (
        retail_price_inner_unit_dirams IS NULL OR retail_price_inner_unit_dirams >= 0
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_receipt_quantity CHECK (
        quantity_base_units = quantity_packages * base_units_per_package_snapshot
    )
);

CREATE TABLE stock_lots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    receipt_item_id uuid REFERENCES receipt_items(id) ON DELETE RESTRICT,
    origin_type varchar(30) NOT NULL CHECK (
        origin_type IN ('RECEIPT', 'INITIAL_STOCK', 'RETURN')
    ),
    batch_number varchar(100) NOT NULL,
    expiration_date date NOT NULL,
    quantity_base_units bigint NOT NULL DEFAULT 0 CHECK (quantity_base_units >= 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    purchase_price_package_dirams bigint CHECK (
        purchase_price_package_dirams IS NULL OR purchase_price_package_dirams >= 0
    ),
    package_retail_price_dirams bigint NOT NULL CHECK (package_retail_price_dirams >= 0),
    inner_unit_retail_price_dirams bigint CHECK (
        inner_unit_retail_price_dirams IS NULL OR inner_unit_retail_price_dirams >= 0
    ),
    received_at timestamptz NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'ARCHIVED')
    ),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_lot_origin_receipt CHECK (
        (origin_type IN ('RECEIPT', 'INITIAL_STOCK') AND receipt_item_id IS NOT NULL)
        OR (origin_type = 'RETURN')
    )
);

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
    sale_number varchar(100) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (
        status IN ('COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REVERSED')
    ),
    payment_method varchar(50) NOT NULL CHECK (
        payment_method IN ('CASH', 'CARD', 'MOBILE', 'MIXED')
    ),
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
        line_total_dirams = line_subtotal_dirams - line_discount_dirams
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

CREATE INDEX idx_sale_item_allocations_lot
ON sale_item_allocations (stock_lot_id, sale_item_id);

-- ================================================================
-- Returns and return allocations (incorporated amendment)
-- ================================================================

CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    operation_id uuid UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (
        status IN ('COMPLETED', 'REVERSED')
    ),
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
    CONSTRAINT chk_sale_return_item_reason CHECK (
        item_reason IS NULL OR btrim(item_reason) <> ''
    )
);

CREATE INDEX idx_sale_return_items_sale_item ON sale_return_items (sale_item_id);

CREATE TABLE sale_return_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_item_id uuid NOT NULL REFERENCES sale_return_items(id) ON DELETE RESTRICT,
    sale_item_allocation_id uuid NOT NULL REFERENCES sale_item_allocations(id) ON DELETE RESTRICT,
    target_stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL CHECK (returned_quantity_base_units > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item_allocation UNIQUE (
        sale_return_item_id, sale_item_allocation_id
    )
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
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE inventory_adjustment_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    inventory_adjustment_id uuid NOT NULL REFERENCES inventory_adjustments(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    expected_quantity_base_units bigint NOT NULL CHECK (expected_quantity_base_units >= 0),
    actual_quantity_base_units bigint NOT NULL CHECK (actual_quantity_base_units >= 0),
    delta_base_units bigint NOT NULL,
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
    operation varchar(150) NOT NULL,
    idempotency_key varchar(128) NOT NULL CHECK (btrim(idempotency_key) <> ''),
    request_hash bytea NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'IN_PROGRESS' CHECK (
        status IN ('IN_PROGRESS', 'COMPLETED', 'FAILED_RETRYABLE')
    ),
    response_status integer CHECK (response_status BETWEEN 100 AND 599),
    response_body jsonb,
    resource_type varchar(100),
    resource_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at timestamptz NOT NULL,
    CONSTRAINT uq_idempotency_scope UNIQUE (
        actor_user_id, pharmacy_id, operation, idempotency_key
    ),
    CONSTRAINT chk_idempotency_result CHECK (
        (status = 'COMPLETED' AND response_status IS NOT NULL AND completed_at IS NOT NULL)
        OR status <> 'COMPLETED'
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
    action varchar(150) NOT NULL,
    object_type varchar(100) NOT NULL,
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
        OR (actor_type = 'SYSTEM' AND actor_user_id IS NULL)
    )
);

CREATE INDEX idx_audit_events_time ON audit_events (occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_actor ON audit_events (actor_user_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_pharmacy ON audit_events (pharmacy_id, occurred_at DESC, id DESC);
CREATE INDEX idx_audit_events_object ON audit_events (object_type, object_id, occurred_at DESC);
CREATE INDEX idx_audit_events_request ON audit_events (request_id) WHERE request_id IS NOT NULL;

-- ================================================================
-- Alerts
-- ================================================================

CREATE TABLE alerts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    pharmacy_product_id uuid REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    stock_lot_id uuid REFERENCES stock_lots(id) ON DELETE RESTRICT,
    alert_type varchar(50) NOT NULL CHECK (
        alert_type IN ('LOW_STOCK', 'EXPIRED', 'EXPIRING_7_DAYS', 'EXPIRING_30_DAYS', 'RECONCILIATION_MISMATCH')
    ),
    deduplication_key varchar(255) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'ACKNOWLEDGED', 'RESOLVED')
    ),
    detected_at timestamptz NOT NULL,
    last_confirmed_at timestamptz NOT NULL,
    acknowledged_by_user_id uuid REFERENCES users(id) ON DELETE RESTRICT,
    acknowledged_at timestamptz,
    resolved_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_alert_active_dedup UNIQUE (pharmacy_id, deduplication_key)
);
```

## 7. Транзакционные инварианты

Следующие правила не выражаются полностью простыми constraints и обязательны в application layer и integration tests.

### 7.1 Identity и authorization

1. Активная роль пользователя определяется только активной строкой `user_roles.revoked_at IS NULL`.
2. Пользователь с `BLOCKED` или `ARCHIVED` не может создавать новые сессии и выполнять защищённые команды.
3. Активная session должна принадлежать активному пользователю и быть неистёкшей и неотозванной.
4. Refresh rotation создаёт новую session и отзывает предыдущую в одной транзакции.
5. Обнаружение повторного использования старого refresh token отзывает всю `token_family_id`.
6. `PHARMACIST` может иметь не более одного активного назначения; роль и назначение проверяются повторно внутри критической транзакции.
7. Назначение разрешено только пользователю с активной ролью `PHARMACIST`.

### 7.2 Поступления и остатки

1. `receipt_items.pharmacy_product_id` должен принадлежать той же аптеке, что и `receipts.pharmacy_id`.
2. Количество базовых единиц равно количеству упаковок, умноженному на snapshot коэффициента.
3. Проведение поступления атомарно создаёт receipt, items, lots, inventory operation, movements, обновляет `inventory_changed_at`, idempotency record и audit event.
4. Просроченный товар не создаёт продаваемый активный лот.
5. Значение `stock_lots.quantity_base_units` после операции равно предыдущему значению плюс движение.

### 7.3 Продажи

1. Все `pharmacy_products` продажи принадлежат аптеке продажи.
2. Уникальные `pharmacy_product_id` блокируются в детерминированном порядке.
3. FEFO: `expiration_date ASC, received_at ASC, id ASC`.
4. Аллокации строки суммарно равны `sale_items.quantity_base_units`.
5. Продажа отклоняется целиком при нехватке хотя бы одной позиции.
6. Цена и количество списания повторно вычисляются backend после блокировок.
7. Сумма строк равна totals продажи согласно утверждённой discount policy.
8. Для рецептурной позиции обязательно `prescription_confirmed = true` и audit event.

### 7.4 Возвраты

1. `sale_return_items.sale_item_id` принадлежит продаже из `sale_returns.sale_id`.
2. Каждая `sale_return_item_allocation` ссылается на исходную аллокацию той же строки продажи.
3. Сумма возвратных аллокаций равна `sale_return_items.returned_quantity_base_units`.
4. По каждой исходной аллокации нельзя вернуть больше проданного количества с учётом предыдущих завершённых и не сторнированных возвратов.
5. По строке продажи нельзя вернуть больше проданного количества.
6. Только `RESTOCK` увеличивает продаваемый остаток.
7. `WRITE_OFF`, `QUARANTINE` и `NO_PHYSICAL_RETURN` не увеличивают доступный остаток.
8. Просроченный или непригодный товар нельзя вернуть в активный продаваемый лот.
9. `sale_returns.refund_amount_dirams` равен сумме строк возврата.
10. Статус продажи меняется на `PARTIALLY_REFUNDED` или `REFUNDED` в той же транзакции.
11. Возврат, возвратные аллокации, финансовый эффект, складская операция, движения, idempotency и audit фиксируются атомарно.
12. Production-проведение возврата остаётся запрещённым до утверждения юридической политики, несмотря на готовность модели данных.

### 7.5 Идемпотентность

1. Новый scope-key вставляется до бизнес-эффекта внутри той же транзакции.
2. Тот же key и тот же `request_hash` возвращает сохранённый результат без повторного эффекта.
3. Тот же key и другой `request_hash` отклоняется как conflict.
4. Параллельные запросы одного scope сериализуются уникальным индексом и блокировкой строки.
5. `response_body` хранит только безопасный HTTP result без токенов и секретов.
6. Retention не может быть меньше срока, установленного API contract; для юридически значимых документов связь с resource может храниться дольше.

### 7.6 Аудит

1. Audit append-only: прикладные UPDATE и DELETE запрещены.
2. Критическая операция не commit-ится без обязательного audit event.
3. Audit metadata не содержит password hash, access/refresh token, полное тело чувствительного запроса, SQL или stack trace.
4. `actor_type = SYSTEM` используется только для фоновых процессов с явно определённым action.
5. Неуспешная попытка входа может аудироваться без раскрытия существования пользователя и без хранения введённого пароля.

## 8. Порядок блокировок

### 8.1 Продажа

1. `pharmacies`;
2. `pharmacy_products` по отсортированному `id`;
3. подходящие `stock_lots` в FEFO-порядке с `FOR UPDATE`;
4. повторная проверка цены, статуса и остатка;
5. вставка документов, аллокаций и движений;
6. обновление lots и `inventory_changed_at`;
7. idempotency и audit;
8. commit.

### 8.2 Возврат

1. `sales` по `sale_id`;
2. `sale_items` по `id`;
3. `sale_item_allocations` по `id`;
4. `pharmacy_products` по `id`, если есть складской эффект;
5. целевые `stock_lots` по `id`;
6. повторная проверка ранее возвращённого количества;
7. вставка возврата, аллокаций и движений;
8. обновление lots и статуса продажи;
9. idempotency и audit;
10. commit.

Все сценарии, блокирующие одинаковые сущности, должны использовать совместимый глобальный порядок.

## 9. Индексы и access patterns

Обязательные access patterns:

- lookup пользователя по `lower(login)`;
- активные sessions пользователя и token family;
- активное назначение пользователя и назначения аптеки;
- поиск каталога по названию и МНН;
- barcode lookup;
- ассортимент по аптеке и presentation;
- FEFO-выборка активных лотов;
- история движений по лоту;
- продажи и возвраты по аптеке и времени;
- конкурентный расчёт возвратных количеств по `sale_item_allocation_id`;
- idempotency lookup по полному scope;
- audit pagination по времени с `id` tie-breaker.

Перед production обязательны `EXPLAIN (ANALYZE, BUFFERS)` для публичного поиска, FEFO, истории движений, возвратных eligibility queries и audit filters.

## 10. Неизменяемость и database privileges

Production application role не должна иметь:

- `DELETE` на проведённых документах, movements и audit;
- прямой `UPDATE` исторических строк документов;
- права изменять `inventory_movements` после вставки;
- права обходить migrations.

Рекомендуется разделить роли:

- migration owner;
- runtime application role;
- read-only operational/audit role;
- backup role.

Append-only правила дополнительно защищаются privileges и, при необходимости, trigger-guard после измерения эксплуатационной стоимости.

## 11. Retention и персональные данные

1. Архивирование пользователя не удаляет ссылки из документов и audit.
2. Session rows могут очищаться после окончания security retention, но token hashes никогда не архивируются в открытом виде.
3. Idempotency response payload очищается по retention policy, если связь с resource достаточно сохранить отдельно.
4. Audit retention определяется security/legal design и не равна retention технических логов.
5. Import files и row payloads удаляются или обезличиваются после установленного срока.
6. Резервные копии наследуют требования retention и защиты персональных данных.

## 12. Migration strategy

Рекомендуемая последовательность:

1. extensions и identity tables;
2. pharmacies и assignments;
3. catalog;
4. imports/staging;
5. assortment;
6. inventory operations, receipts и lots;
7. sales и FEFO allocations;
8. returns и return allocations;
9. write-offs и adjustments;
10. idempotency;
11. audit;
12. alerts;
13. privileges, constraints hardening и data backfill.

Каждая migration обязана иметь:

- forward SQL;
- безопасную rollback policy либо явную irreversible-маркировку;
- проверку существующих данных перед `NOT NULL`/`CHECK`;
- индексы, создаваемые с учётом блокировок;
- migration integration test на чистой и обновляемой БД.

## 13. Обязательные тесты

1. Все CHECK, UNIQUE и FK constraints.
2. Login uniqueness без учёта регистра.
3. Единственная активная роль и единственное активное pharmacy assignment.
4. Session rotation, revoke-all и token-family replay.
5. Horizontal privilege escalation между аптеками.
6. Idempotent replay с тем же payload и conflict с другим payload.
7. Параллельные запросы с одинаковым idempotency key.
8. Конкурентные продажи одного лота без отрицательного остатка.
9. Многострочная продажа с различным порядком request items без deadlock.
10. FEFO через несколько лотов.
11. Конкурентные возвраты одной sale allocation.
12. Инварианты `sale_return_item_allocations`.
13. Reconciliation: `stock_lots.quantity_base_units = SUM(inventory_movements.delta_base_units)`.
14. Неизменяемость movements, документов и audit.
15. Атомарный rollback при ошибке обязательного audit event или idempotency record.
16. Планы запросов для критических access patterns.

## 14. Открытые решения

До production необходимо отдельно утвердить:

1. password hashing algorithm и параметры;
2. сроки access/refresh token и session retention;
3. окончательную discount/refund rounding policy;
4. юридические правила возврата лекарств;
5. хранение import files: PostgreSQL, filesystem или object storage;
6. необходимость отдельной audit DB role или replica;
7. конкретные RPO/RTO и backup retention;
8. применение PostgreSQL Row-Level Security: по умолчанию authorization остаётся в application layer, RLS вводится только отдельным ADR.

## 15. Definition of Done для схемы

Database feature считается завершённой только если:

1. SRS, API design, domain model и DDL согласованы;
2. migration применима на чистой и обновляемой БД;
3. repository не обходит module ownership;
4. критические инварианты покрыты integration/concurrency tests;
5. idempotency и audit включены в транзакцию, где это обязательно;
6. необходимые индексы подтверждены реальными планами запросов;
7. sensitive data не попадает в audit и idempotency response;
8. документация обновлена в том же change set.
