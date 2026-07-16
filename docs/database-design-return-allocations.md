# PharmacyCRM — Дополнение к Database Design: возвратные аллокации

**Статус:** Accepted amendment  
**Дата:** 2026-07-16  
**Связанный документ:** `docs/database-design.md`  
**Связанный ADR:** `docs/adr/0012-sale-returns-and-restocking.md`

## 1. Назначение

Этот документ нормативно заменяет текущий блок таблиц `sale_returns` и `sale_return_items` в `docs/database-design.md` и добавляет `sale_return_item_allocations`.

Причина изменения: лимит возврата должен контролироваться не только по строке чека, но и по конкретным исходным FEFO-аллокациям. Это позволяет доказуемо определить, из какого поставочного лота был возвращён товар и сколько единиц каждой исходной аллокации уже использовано предыдущими возвратами.

При следующей полной редакции `database-design.md` этот блок должен быть встроен в основной DDL, а данное дополнение — помечено как incorporated.

## 2. DDL

```sql
CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_id uuid NOT NULL REFERENCES sales(id) ON DELETE RESTRICT,

    -- Заполняется только когда возврат создаёт физические складские движения.
    -- Для RESTOCK это RETURN_TO_STOCK. Для WRITE_OFF/QUARANTINE может быть
    -- отдельная операция соответствующего типа после расширения inventory_operations.
    operation_id uuid UNIQUE REFERENCES inventory_operations(id) ON DELETE RESTRICT,

    status varchar(30) NOT NULL DEFAULT 'COMPLETED' CHECK (
        status IN ('DRAFT', 'COMPLETED', 'REVERSED')
    ),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    returned_by_user_id uuid NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    returned_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sale_return_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_id uuid NOT NULL REFERENCES sale_returns(id) ON DELETE RESTRICT,
    sale_item_id uuid NOT NULL REFERENCES sale_items(id) ON DELETE RESTRICT,

    returned_quantity_base_units bigint NOT NULL CHECK (
        returned_quantity_base_units > 0
    ),
    refund_amount_dirams bigint NOT NULL CHECK (refund_amount_dirams >= 0),
    return_action varchar(50) NOT NULL CHECK (
        return_action IN (
            'RESTOCK',
            'WRITE_OFF',
            'QUARANTINE',
            'NO_PHYSICAL_RETURN'
        )
    ),
    item_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_sale_return_item UNIQUE (sale_return_id, sale_item_id),
    CONSTRAINT chk_sale_return_item_reason CHECK (
        item_reason IS NULL OR btrim(item_reason) <> ''
    )
);

CREATE TABLE sale_return_item_allocations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    sale_return_item_id uuid NOT NULL
        REFERENCES sale_return_items(id) ON DELETE RESTRICT,
    sale_item_allocation_id uuid NOT NULL
        REFERENCES sale_item_allocations(id) ON DELETE RESTRICT,
    returned_quantity_base_units bigint NOT NULL CHECK (
        returned_quantity_base_units > 0
    ),
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_sale_return_item_allocation UNIQUE (
        sale_return_item_id,
        sale_item_allocation_id
    )
);

CREATE INDEX idx_sale_return_item_allocations_lookup
ON sale_return_item_allocations (sale_item_allocation_id);

CREATE INDEX idx_sale_return_items_sale_item
ON sale_return_items (sale_item_id);
```

## 3. Обязательные транзакционные инварианты

Следующие правила невозможно полностью выразить простыми `CHECK` constraints. Они обеспечиваются application layer в общей транзакции и проверяются интеграционными тестами.

1. `sale_return_items.sale_item_id` должен принадлежать продаже, указанной в `sale_returns.sale_id`.
2. Каждая `sale_return_item_allocation` должна ссылаться на аллокацию того же `sale_item_id`, что и родительская строка возврата.
3. Сумма `sale_return_item_allocations.returned_quantity_base_units` по строке возврата должна равняться `sale_return_items.returned_quantity_base_units`.
4. Суммарно по всем завершённым и не сторнированным возвратам нельзя вернуть больше, чем `sale_item_allocations.quantity_base_units` для каждой исходной аллокации.
5. Суммарно по всем завершённым и не сторнированным возвратам нельзя вернуть больше, чем `sale_items.quantity_base_units` для строки чека.
6. Проверка лимитов выполняется после блокировки исходной продажи и исходных аллокаций.
7. Только `RESTOCK` создаёт положительные движения в продаваемый остаток.
8. `WRITE_OFF`, `QUARANTINE` и `NO_PHYSICAL_RETURN` не увеличивают доступный к продаже остаток.
9. Просроченный, повреждённый или утративший допустимые условия хранения товар нельзя вернуть в активный исходный лот.
10. Если исходный лот непригоден для повторного приёма, должен быть создан отдельный возвратный/карантинный лот после явного расширения модели; скрытое изменение атрибутов исходного лота запрещено.
11. Сумма возврата рассчитывается backend на основе неизменяемых данных исходной продажи и утверждённых правил округления.
12. `sale_returns.refund_amount_dirams` должна равняться сумме `sale_return_items.refund_amount_dirams`.
13. Статус исходной продажи меняется на `PARTIALLY_REFUNDED` или `REFUNDED` в той же транзакции.
14. Возврат, его аллокации, финансовые изменения, складская операция, движения и изменение остатка фиксируются атомарно.

## 4. Порядок блокировок

Для конкурентных возвратов применяется следующий порядок:

1. блокировка `sales` по `sale_id`;
2. чтение и блокировка затрагиваемых `sale_items` в порядке `id`;
3. чтение и блокировка исходных `sale_item_allocations` в порядке `id`;
4. при `RESTOCK` блокировка затрагиваемых `pharmacy_products` в детерминированном порядке;
5. блокировка целевых `stock_lots` в порядке `id`;
6. повторная проверка уже возвращённых количеств;
7. вставка возвратных документов, аллокаций и движений;
8. обновление лотов и статуса продажи;
9. commit.

Все транзакции, которые блокируют те же сущности, обязаны придерживаться совместимого глобального порядка. Конкретный порядок должен оставаться синхронизированным с ADR-0011.

## 5. Неизменяемость

После перехода возврата в `COMPLETED` его строки и возвратные аллокации считаются историческими данными и не редактируются. Исправления оформляются отдельной компенсирующей операцией. Физическое удаление завершённых возвратов запрещено.
