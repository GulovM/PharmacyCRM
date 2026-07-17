# PharmacyCRM — Database Design

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-16

## 1. Назначение документа

Документ описывает первую согласованную версию модели данных PharmacyCRM для PostgreSQL. Он фиксирует границы между глобальным каталогом, представлениями товара, ассортиментом аптеки, поставочными лотами, операционными документами и неизменяемым логом складских движений.

DDL ниже является проектным контрактом, а не готовой миграцией. Перед реализацией он должен быть разложен на последовательные миграции, дополнен таблицами пользователей, ролей и аудита, а также проверен интеграционными тестами.

## 2. Ключевые принципы

1. PostgreSQL является источником истины для каталога, продаж, поступлений, возвратов, лотов и остатков.
2. Текущий остаток хранится в базовых единицах отпуска.
3. Любое изменение остатка сопровождается неизменяемым складским движением.
4. Текущий остаток и движение создаются в одной транзакции.
5. Деньги хранятся целым числом дирамов в `bigint`.
6. Исторические документы содержат снимки цен и упаковочных коэффициентов.
7. Изменение каталога не должно ретроактивно менять старые партии, продажи и возвраты.
8. Физическое удаление операционных данных запрещено; жизненный цикл управляется статусами.

## 3. Концептуальная модель

```text
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
```

### 3.1 Глобальный товар и представление

`products` описывает лекарственный продукт: торговое название, МНН, форму, дозировку, производителя и рецептурность.

`product_presentations` описывает конкретную заводскую фасовку, например коробку с тремя блистерами. Разные фасовки одного лекарства являются разными представлениями.

### 3.2 Ассортимент аптеки

`pharmacy_products` связывает конкретное представление товара с аптекой и хранит локальные правила продажи, текущие цены по умолчанию, пороги пополнения и время последней успешной складской транзакции.

### 3.3 Поставочный лот

`stock_lots` представляет конкретную позицию конкретного поступления. Одинаковая заводская серия может существовать в нескольких лотах, если товар поступал разными документами, по разным ценам или в разное время.

### 3.4 Складская операция и движение

`inventory_operations` — заголовок одной бизнес-операции изменения склада.

`inventory_movements` — append-only проводки по конкретным лотам. Одна операция может создавать несколько движений.

## 4. Проектный DDL

```sql
-- UUID генерируется встроенной функцией gen_random_uuid().
-- Для современных версий PostgreSQL отдельное расширение uuid-ossp не требуется.

CREATE TABLE pharmacies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(255) NOT NULL,
    address text NOT NULL,
    landmark text,
    latitude numeric(9, 6) NOT NULL CHECK (latitude BETWEEN -90 AND 90),
    longitude numeric(9, 6) NOT NULL CHECK (longitude BETWEEN -180 AND 180),
    phone varchar(50),
    working_hours varchar(255),
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'BLOCKED', 'ARCHIVED')
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);

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
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

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
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_presentation_inner_unit_name CHECK (
        base_units_per_package = 1 OR inner_unit_name IS NOT NULL
    )
);

CREATE TABLE product_barcodes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
    barcode varchar(100) NOT NULL,
    barcode_type varchar(30) NOT NULL DEFAULT 'EAN13',
    is_primary boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_product_barcodes_barcode UNIQUE (barcode)
);

CREATE UNIQUE INDEX uq_product_barcodes_primary
ON product_barcodes (product_presentation_id)
WHERE is_primary = true;

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
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_pharmacy_product_presentation UNIQUE (pharmacy_id, product_presentation_id),
    CONSTRAINT chk_inner_unit_pricing CHECK (
        is_inner_unit_sale_allowed = false
        OR default_inner_unit_price_dirams IS NOT NULL
    ),
    CONSTRAINT chk_target_not_below_min CHECK (
        target_stock_level_base_units >= min_stock_level_base_units
    )
);

CREATE TABLE inventory_operations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_type varchar(50) NOT NULL CHECK (
        operation_type IN (
            'RECEIPT',
            'SALE',
            'RETURN_TO_STOCK',
            'WRITE_OFF',
            'CORRECTION'
        )
    ),
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (
        status IN ('POSTED', 'REVERSED')
    ),
    idempotency_key varchar(255) NOT NULL,
    initiated_by_user_id uuid,
    is_system_initiated boolean NOT NULL DEFAULT false,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_inventory_operation_idempotency UNIQUE (pharmacy_id, idempotency_key),
    CONSTRAINT chk_inventory_operation_actor CHECK (
        initiated_by_user_id IS NOT NULL OR is_system_initiated = true
    )
);

CREATE TABLE receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    receipt_number varchar(100) NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'POSTED' CHECK (
        status IN ('DRAFT', 'POSTED', 'REVERSED')
    ),
    received_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    posted_at timestamptz,
    CONSTRAINT uq_receipt_number_per_pharmacy UNIQUE (pharmacy_id, receipt_number)
);

CREATE TABLE receipt_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    receipt_id uuid NOT NULL REFERENCES receipts(id) ON DELETE RESTRICT,
    product_presentation_id uuid NOT NULL REFERENCES product_presentations(id) ON DELETE RESTRICT,
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
    CONSTRAINT chk_receipt_quantity_conversion CHECK (
        quantity_base_units = quantity_packages * base_units_per_package_snapshot
    )
);

CREATE TABLE stock_lots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_product_id uuid NOT NULL REFERENCES pharmacy_products(id) ON DELETE RESTRICT,
    receipt_item_id uuid NOT NULL UNIQUE REFERENCES receipt_items(id) ON DELETE RESTRICT,
    batch_number varchar(100) NOT NULL,
    expiration_date date NOT NULL,
    quantity_base_units bigint NOT NULL DEFAULT 0 CHECK (quantity_base_units >= 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    purchase_price_package_dirams bigint NOT NULL CHECK (purchase_price_package_dirams >= 0),
    package_retail_price_dirams bigint NOT NULL CHECK (package_retail_price_dirams >= 0),
    inner_unit_retail_price_dirams bigint CHECK (
        inner_unit_retail_price_dirams IS NULL OR inner_unit_retail_price_dirams >= 0
    ),
    received_at timestamptz NOT NULL,
    status varchar(30) NOT NULL DEFAULT 'ACTIVE' CHECK (
        status IN ('ACTIVE', 'DEPLETED', 'QUARANTINED', 'ARCHIVED')
    ),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    pharmacy_id uuid NOT NULL REFERENCES pharmacies(id) ON DELETE RESTRICT,
    operation_id uuid NOT NULL UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (
        status IN ('DRAFT', 'COMPLETED', 'PARTIALLY_REFUNDED', 'REFUNDED', 'REVERSED')
    ),
    payment_method varchar(50) NOT NULL CHECK (
        payment_method IN ('CASH', 'CARD', 'MOBILE', 'MIXED')
    ),
    subtotal_amount_dirams bigint NOT NULL CHECK (subtotal_amount_dirams >= 0),
    discount_amount_dirams bigint NOT NULL DEFAULT 0 CHECK (discount_amount_dirams >= 0),
    total_amount_dirams bigint NOT NULL CHECK (total_amount_dirams >= 0),
    prescription_confirmed boolean NOT NULL DEFAULT false,
    sold_by_user_id uuid NOT NULL,
    sold_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
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
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    unit_price_dirams bigint NOT NULL CHECK (unit_price_dirams >= 0),
    discount_amount_dirams bigint NOT NULL DEFAULT 0 CHECK (discount_amount_dirams >= 0),
    line_total_dirams bigint NOT NULL CHECK (line_total_dirams >= 0),
    base_units_per_package_snapshot bigint NOT NULL CHECK (base_units_per_package_snapshot > 0),
    prescription_required_snapshot boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT chk_sale_item_quantity_conversion CHECK (
        (sale_unit = 'PACKAGE' AND quantity_base_units = display_quantity * base_units_per_package_snapshot)
        OR
        (sale_unit = 'INNER_UNIT' AND quantity_base_units = display_quantity)
    ),
    CONSTRAINT chk_sale_item_total CHECK (
        line_total_dirams = display_quantity * unit_price_dirams - discount_amount_dirams
        AND discount_amount_dirams <= display_quantity * unit_price_dirams
    )
);

CREATE TABLE sale_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    quantity_base_units bigint NOT NULL CHECK (quantity_base_units > 0),
    allocation_sequence integer NOT NULL DEFAULT 1 CHECK (allocation_sequence > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_item_allocation UNIQUE (sale_item_id, stock_lot_id, allocation_sequence)
);

CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,
    operation_id uuid UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (
        status IN ('DRAFT', 'COMPLETED', 'REVERSED')
    ),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    returned_by_user_id uuid NOT NULL,
    returned_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sale_return_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_id uuid NOT NULL REFERENCES sale_returns(id) ON DELETE RESTRICT,
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL CHECK (returned_quantity_base_units > 0),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    return_action varchar(50) NOT NULL CHECK (
        return_action IN ('RESTOCK', 'WRITE_OFF', 'QUARANTINE', 'NO_PHYSICAL_RETURN')
    ),
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_sale_return_item UNIQUE (sale_return_id, sale_item_id)
);

CREATE TABLE inventory_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation_id uuid NOT NULL REFERENCES inventory_operations(id) ON DELETE RESTRICT,
    operation_line_id uuid NOT NULL,
    stock_lot_id uuid NOT NULL REFERENCES stock_lots(id) ON DELETE RESTRICT,
    delta_base_units bigint NOT NULL CHECK (delta_base_units <> 0),
    movement_sequence integer NOT NULL DEFAULT 1 CHECK (movement_sequence > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT uq_movement_line UNIQUE (
        operation_id,
        operation_line_id,
        stock_lot_id,
        movement_sequence
    )
);
```

## 5. Инварианты, которые не выражаются одним CHECK

Следующие правила должны выполняться сервисным слоем в общей транзакции и проверяться интеграционными тестами:

1. `inventory_operations.pharmacy_id` должен совпадать с аптекой связанного документа.
2. `stock_lots.pharmacy_product_id` должен соответствовать аптеке поступления.
3. Сумма `sale_item_allocations.quantity_base_units` должна равняться `sale_items.quantity_base_units`.
4. Нельзя аллоцировать просроченный, карантинный, архивный или исчерпанный лот.
5. FEFO выбирает лоты по `expiration_date ASC, received_at ASC, id ASC`.
6. Списание остатка и вставка движений выполняются атомарно.
7. Нельзя вернуть больше базовых единиц, чем было продано с учётом предыдущих завершённых возвратов.
8. `RESTOCK` требует положительных движений в исходные или специально созданные возвратные лоты.
9. `WRITE_OFF`, `QUARANTINE` и `NO_PHYSICAL_RETURN` не увеличивают продаваемый остаток.
10. Продажа рецептурной позиции требует подтверждения на уровне backend.
11. Итоговые суммы всегда повторно рассчитываются backend и не принимаются на доверии от frontend.
12. `inventory_changed_at` обновляется временем фиксации успешной складской транзакции.
13. Любое расхождение между `stock_lots.quantity_base_units` и суммой движений является инцидентом и не исправляется скрыто.

## 6. Индексы

```sql
CREATE INDEX idx_pharmacies_geo_bbox
ON pharmacies (latitude, longitude)
WHERE status = 'ACTIVE';

CREATE INDEX idx_products_search
ON products (title, form, dosage)
WHERE status = 'ACTIVE';

CREATE INDEX idx_pharmacy_products_lookup
ON pharmacy_products (pharmacy_id, product_presentation_id)
WHERE status = 'ACTIVE';

CREATE INDEX idx_stock_lots_fefo
ON stock_lots (
    pharmacy_product_id,
    expiration_date,
    received_at,
    id
)
WHERE quantity_base_units > 0 AND status = 'ACTIVE';

CREATE INDEX idx_stock_lots_expiration
ON stock_lots (pharmacy_product_id, expiration_date)
WHERE quantity_base_units > 0;

CREATE INDEX idx_inventory_movements_lot_history
ON inventory_movements (stock_lot_id, created_at DESC);

CREATE INDEX idx_inventory_movements_operation
ON inventory_movements (operation_id);

CREATE INDEX idx_sale_items_sale
ON sale_items (sale_id);

CREATE INDEX idx_sale_item_allocations_lot
ON sale_item_allocations (stock_lot_id);

CREATE INDEX idx_sale_returns_sale
ON sale_returns (sale_id, returned_at DESC);
```

Уникальный constraint на `product_barcodes.barcode` уже создаёт индекс, поэтому отдельный дублирующий индекс не нужен.

## 7. Иммутабельность складских движений

Основная защита строится на правах PostgreSQL:

```sql
REVOKE UPDATE, DELETE ON inventory_movements FROM pharmacycrm_app;
GRANT SELECT, INSERT ON inventory_movements TO pharmacycrm_app;
```

Дополнительная защита может быть реализована триггером:

```sql
CREATE OR REPLACE FUNCTION block_inventory_movement_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'inventory_movements is append-only; UPDATE and DELETE are forbidden';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_inventory_movements_immutable
BEFORE UPDATE OR DELETE ON inventory_movements
FOR EACH ROW
EXECUTE FUNCTION block_inventory_movement_mutation();
```

Триггер является defense in depth, а не абсолютной защитой от владельца таблицы или суперпользователя.

## 8. Обновление актуальности остатка

`inventory_changed_at` должен отражать момент успешной фиксации складской транзакции, а не ретроспективное бизнес-время операции.

Предпочтительный вариант — обновлять это поле явным SQL внутри той же транзакции, в которой меняются лоты и вставляются движения:

```sql
UPDATE pharmacy_products
SET inventory_changed_at = statement_timestamp(),
    updated_at = statement_timestamp()
WHERE id = $1;
```

Явное обновление предпочтительнее скрытого триггера, поскольку делает транзакционный сценарий видимым в коде и упрощает тестирование. Триггер допустим только как отдельно принятое решение.

## 9. Проверка согласованности

Диагностическая сверка лота:

```sql
SELECT
    sl.id AS stock_lot_id,
    sl.quantity_base_units AS stored_quantity,
    COALESCE(SUM(im.delta_base_units), 0) AS ledger_quantity,
    sl.quantity_base_units - COALESCE(SUM(im.delta_base_units), 0) AS difference
FROM stock_lots sl
LEFT JOIN inventory_movements im ON im.stock_lot_id = sl.id
GROUP BY sl.id, sl.quantity_base_units
HAVING sl.quantity_base_units <> COALESCE(SUM(im.delta_base_units), 0);
```

Этот запрос используется для мониторинга и аудита. Он не должен автоматически исправлять данные.

## 10. Ограничения текущей версии

В этой версии ещё не описаны:

- пользователи, роли и назначение аптекарей;
- staging-импорт глобального каталога;
- импорт начальных остатков;
- платежи и смешанная оплата;
- отдельные документы списания и инвентаризационной корректировки;
- карантинные зоны и отдельные возвратные лоты;
- аудит административных действий;
- полнотекстовый поиск;
- PostGIS;
- партиционирование `inventory_movements`;
- государственная маркировка и `SerializedItem`.

Эти части должны добавляться последовательно без нарушения инвариантов ADR-0003 и ADR-0005.

## 11. Следующий этап

Следующий документ должен описать транзакционные сценарии проведения:

1. поступления;
2. продажи с FEFO-аллокацией;
3. частичного возврата;
4. возврата товара на склад;
5. списания;
6. инвентаризационной корректировки;
7. диагностической сверки остатков.

Особое внимание необходимо уделить порядку блокировок, идемпотентности и защите от отрицательного остатка при параллельных продажах.
