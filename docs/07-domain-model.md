# PharmacyCRM — Domain Model

**Статус документа:** Draft  
**Версия:** 1.0  
**Дата:** 2026-07-17  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`  
**Связанные ADR:** ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет целевую доменную модель PharmacyCRM: bounded contexts, агрегаты, aggregate roots, сущности, value objects, состояния, доменные инварианты, доменные сервисы, события и транзакционные границы.

Документ отвечает на вопросы:

- какой объект является владельцем конкретного бизнес-инварианта;
- какие изменения разрешены только через aggregate root;
- какие данные являются сущностями, а какие value objects;
- какие состояния и переходы допустимы;
- какие операции обязаны выполняться в одной PostgreSQL-транзакции;
- где допустима eventual consistency;
- какие правила принадлежат Domain, Application и Infrastructure.

Domain Model не является схемой БД и не должен механически повторять таблицы. Одна таблица не обязательно равна одному агрегату, а один use case может координировать несколько агрегатов через Unit of Work.

При противоречии применяется порядок приоритетов из SRS. Изменение доменной модели, влияющее на внешнее поведение, должно синхронно обновлять SRS, API Design, Database Design и соответствующие ADR.

## 2. Термины моделирования

- **Entity** — объект с устойчивой идентичностью и жизненным циклом.
- **Value Object** — неизменяемое значение без собственной идентичности; равенство определяется содержимым.
- **Aggregate** — согласованная группа domain objects с одной внешней точкой изменения.
- **Aggregate Root** — сущность, через которую выполняются все изменения агрегата.
- **Domain Service** — чистое бизнес-правило, которое естественно не принадлежит одной сущности.
- **Application Service / Use Case** — координация авторизации, транзакции, репозиториев, агрегатов, идемпотентности и post-commit действий.
- **Domain Event** — факт, уже произошедший в домене. Событие именуется в прошедшем времени.
- **Transaction Boundary** — набор чтений и изменений, которые должны commit или rollback как единое целое.
- **Invariant** — правило, которое не может быть нарушено ни одним допустимым состоянием системы.

## 3. Bounded contexts и владение моделью

| Context / модуль | Основная ответственность | Владеет |
|---|---|---|
| Identity | пользователи, роли, credentials, sessions | `User`, `RoleAssignment`, `UserSession` |
| Pharmacy | аптеки и назначения аптекарей | `Pharmacy`, `PharmacyAssignment` |
| Catalog | глобальные карточки препаратов, фасовки, штрихкоды, requests и staging | `Product`, `CatalogImport`, `ProductRequest` |
| Assortment | локальная продаваемая позиция и правила отпуска | `PharmacyProduct` |
| Inventory | лоты, остатки, поступления, движения, списания, корректировки | `Receipt`, `InventoryLedger`, `WriteOff`, `InventoryAdjustment` |
| Sales | продажа, строки и FEFO-аллокации | `Sale` |
| Returns | возврат по исходной продаже и исходным аллокациям | `SaleReturn` |
| Reliability | идемпотентность команд | `IdempotencyRecord` |
| Audit | неизменяемые события расследования | `AuditEvent` |
| Alerts | предупреждения и их lifecycle | `Alert` |
| Search | публичные read models наличия | проекции, не командные агрегаты |
| Replenishment | рекомендации пополнения | вычисляемые read models |

Один context может хранить внешний ID другого context, но не получает право изменять внешний агрегат. Межконтекстные команды координируются Application layer через публичные порты и Unit of Work.

## 4. Общие правила агрегатов

1. Aggregate root является единственной точкой изменения внутренних сущностей.
2. Внешний код не получает mutable-ссылки на внутренние коллекции агрегата.
3. Конструктор создаёт только валидный агрегат; восстановление из repository также обязано проверять структурную целостность.
4. Методы агрегата выражают бизнес-намерение: `Block`, `AssignRole`, `Post`, `Allocate`, `CompleteReturn`, а не общие setters.
5. Domain methods не принимают `gin.Context`, DTO, `pgx.Tx`, SQL-модели или HTTP status codes.
6. Время, ID и криптографические значения передаются в Domain готовыми значениями или через узкие порты.
7. Межагрегатная уникальность, authorization scope и конкурентная проверка обеспечиваются Application + repository + database constraints.
8. Проведённый документ после commit является историческим и не реконструируется как изменяемый draft.
9. Доменные коллекции должны иметь разумный лимит размера команды; массовые импорты моделируются отдельным staging aggregate/job.
10. Domain не начинает и не commit-ит транзакции.

## 5. Общие value objects

### 5.1 Идентификаторы

Типизированные ID предотвращают смешивание сущностей:

- `UserID`;
- `RoleID`;
- `SessionID`;
- `PharmacyID`;
- `ProductID`;
- `ProductPresentationID`;
- `PharmacyProductID`;
- `StockLotID`;
- `InventoryOperationID`;
- `ReceiptID`;
- `SaleID`;
- `SaleItemID`;
- `SaleItemAllocationID`;
- `SaleReturnID`;
- `IdempotencyRecordID`;
- `AuditEventID`.

Domain не должен использовать один общий `string` или `uuid.UUID` во всех сигнатурах без типовой обёртки.

### 5.2 `Money`

```text
Money {
    amountDirams: int64
    currency: TJS
}
```

Инварианты:

- `amountDirams >= 0` для цены и абсолютной суммы;
- отрицательный денежный эффект представляется типом операции, а не отрицательной ценой;
- сложение и сравнение разрешены только для одной валюты;
- переполнение `int64` проверяется;
- округление выполняется только утверждённой policy.

### 5.3 Количества

- `BaseUnitQuantity` — целое `int64 >= 0`;
- `PositiveBaseUnitQuantity` — целое `int64 > 0`;
- `PackageQuantity` — целое `int64 > 0`;
- `BaseUnitsPerPackage` — целое `int64 > 0`;
- `SignedBaseUnitDelta` — ненулевое `int64`, допускает знак.

Преобразование упаковок в базовые единицы проверяет overflow:

```text
baseUnits = packages × baseUnitsPerPackage
```

### 5.4 `SaleUnit`

Состояния:

- `PACKAGE`;
- `INNER_UNIT`.

Для `PACKAGE` списание равно `displayQuantity × baseUnitsPerPackageSnapshot`. Для `INNER_UNIT` списание равно `displayQuantity`.

### 5.5 `PriceSnapshot`

Неизменяемый снимок цены на момент документа:

```text
PriceSnapshot {
    packagePrice: Money
    innerUnitPrice: Money?
    baseUnitsPerPackage: BaseUnitsPerPackage
}
```

Изменение текущей цены ассортимента не изменяет snapshot.

### 5.6 Каталожные значения

- `Barcode` — нормализованная непустая строка допустимого формата;
- `ProductName`;
- `INN`;
- `Dosage`;
- `DosageForm`;
- `ManufacturerName`;
- `BatchNumber`;
- `ExpirationDate`;
- `DocumentNumber`.

Нормализация регистра для поиска и уникальности не должна уничтожать исходное отображаемое значение.

### 5.7 Identity и transport-independent security values

- `Login` — нормализованный уникальный идентификатор входа;
- `PasswordHash` — opaque value, Domain не знает алгоритм;
- `RefreshTokenHash` — opaque bytes;
- `TokenFamilyID`;
- `IPAddress`;
- `UserAgent`;
- `RequestID`;
- `TraceID`.

### 5.8 `IdempotencyKey` и `RequestFingerprint`

- `IdempotencyKey` — непустая строка до 128 символов;
- `OperationName` — стабильное имя команды;
- `IdempotencyScope` — actor + operation + pharmacy/global scope;
- `RequestFingerprint` — hash канонического смыслового payload.

Transport-only поля, порядок JSON-ключей и request ID не входят в fingerprint.

### 5.9 `GeoPoint`

```text
GeoPoint {
    latitude: [-90, 90]
    longitude: [-180, 180]
}
```

Расстояние до аптеки является вычисляемым значением search context, а не состоянием `Pharmacy`.

### 5.10 `Reason`

Непустой trimmed text. Для чувствительных операций reason обязателен и сохраняется в историческом документе и/или audit metadata.

## 6. Identity context

### 6.1 Aggregate `User`

**Aggregate root:** `User`.

Внутренние сущности:

- `RoleAssignment` — историческое назначение роли;
- при реализации в одном repository session может загружаться отдельно и не обязана входить в User aggregate.

Основные свойства:

- `UserID`;
- `Login`;
- `PasswordHash`;
- display name и phone;
- `UserStatus`;
- failed login state;
- password changed time;
- version;
- role history.

`UserStatus`:

```text
ACTIVE ──block──> BLOCKED
BLOCKED ──unblock──> ACTIVE
ACTIVE/BLOCKED ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/BLOCKED
```

Правила:

1. `ARCHIVED` — терминальное состояние для новых операций.
2. Block/archive запрещают создание новых sessions.
3. Block/archive должны приводить к отзыву активных sessions в той же application-транзакции либо по более строгой security policy.
4. Login активного пользователя уникален.
5. Пользователь имеет не более одной активной role assignment в MVP.
6. Повторное назначение ранее отозванной роли создаёт новую историческую сущность.
7. Самостоятельное назначение `ADMIN` и `PHARMACIST` запрещено.
8. Изменение password hash увеличивает security version/фиксирует `passwordChangedAt` и отзывает sessions согласно policy.

Команды агрегата:

- `ChangeProfile`;
- `ChangePasswordHash`;
- `RecordFailedLogin`;
- `RecordSuccessfulLogin`;
- `Block`;
- `Unblock`;
- `Archive`;
- `AssignRole`;
- `RevokeRole`.

Domain events:

- `UserCreated`;
- `UserBlocked`;
- `UserUnblocked`;
- `UserArchived`;
- `UserPasswordChanged`;
- `UserRoleAssigned`;
- `UserRoleRevoked`.

### 6.2 Aggregate `UserSession`

**Aggregate root:** `UserSession`.

Session является отдельным агрегатом, потому что имеет собственный lifecycle, конкурентную refresh rotation и независимый retention.

`SessionState` вычисляется из timestamps:

- `ACTIVE`: не отозвана и `now < expiresAt`;
- `EXPIRED`: не отозвана и `now >= expiresAt`;
- `REVOKED`: `revokedAt != null`.

Переходы:

```text
ACTIVE ──rotate──> REVOKED(ROTATED) + new ACTIVE session
ACTIVE ──logout/block/password-change/admin-revoke──> REVOKED
ACTIVE ──time──> EXPIRED
REVOKED/EXPIRED ──X──> ACTIVE
```

Правила:

1. Raw refresh token никогда не входит в domain entity после issuance; хранится только hash.
2. Rotation создаёт новую session и отзывает предыдущую атомарно.
3. `rotatedFromSessionID` не может ссылаться на ту же session.
4. Одна исходная session может породить не более одной следующей session.
5. Повторное использование уже rotated token отзывает всю token family.
6. Session не подтверждает authorization без проверки текущего `User` и назначения.
7. `lastUsedAt` не может быть раньше `createdAt`.

Команды:

- `Rotate`;
- `Revoke`;
- `TouchLastUsed`.

## 7. Pharmacy context

### 7.1 Aggregate `Pharmacy`

**Aggregate root:** `Pharmacy`.

Свойства:

- name;
- address, landmark;
- `GeoPoint`;
- phone, working hours;
- `PharmacyStatus`;
- optimistic version.

Состояния:

```text
ACTIVE ──block──> BLOCKED
BLOCKED ──activate──> ACTIVE
ACTIVE/BLOCKED ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/BLOCKED
```

Инварианты:

1. Только `ACTIVE` принимает новые receipt, sale, return-to-stock, write-off и adjustment команды.
2. `BLOCKED` сохраняет чтение истории и расследование, но запрещает новые операционные эффекты.
3. `ARCHIVED` не участвует в публичном поиске и не принимает новые назначения.
4. Исторические документы сохраняют `PharmacyID` после блокировки/архивирования.
5. Обновление публичного профиля использует optimistic concurrency.

### 7.2 Aggregate `PharmacyAssignment`

**Aggregate root:** `PharmacyAssignment`.

Назначение моделируется отдельно, чтобы сохранять историю переводов аптекаря.

Состояния:

- `ACTIVE`: `endedAt == null`;
- `ENDED`: `endedAt != null`.

Переход:

```text
ACTIVE ──end(reason)──> ENDED
ENDED ──X──> ACTIVE
```

Повторное назначение создаёт новый aggregate.

Инварианты:

1. Назначаемый пользователь имеет активную роль `PHARMACIST`.
2. Аптека не `ARCHIVED`.
3. В MVP у аптекаря не более одного активного назначения.
4. `assignedBy` и `endedBy` являются разрешёнными администраторами.
5. Наличие `pharmacy_id` в команде не заменяет проверку assignment.

## 8. Catalog context

### 8.1 Aggregate `Product`

**Aggregate root:** `Product`.

Внутренние сущности:

- `ProductPresentation`;
- `ProductBarcode` как дочерняя сущность presentation.

Допустимая реализация может хранить presentation отдельным aggregate root для ограничения размера aggregate и независимого редактирования. Нормативное правило: изменение presentation и его barcode выполняется только через catalog context; `Product` не предоставляет mutable-коллекции наружу.

`CatalogStatus`:

```text
ACTIVE <──activate/deactivate──> INACTIVE
ACTIVE/INACTIVE ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/INACTIVE
```

Инварианты:

1. Barcode глобально уникален среди допустимых записей.
2. У presentation не более одного active primary barcode.
3. Если `baseUnitsPerPackage > 1`, inner unit name обязателен.
4. Изменение каталога действует только на будущие операции.
5. Архивирование запрещает новые подключения/операции, но сохраняет историю.
6. Product presentation не удаляется, если на неё ссылается ассортимент или история.

### 8.2 Aggregate `ProductRequest`

Состояния:

```text
OPEN ──approve──> APPROVED
OPEN ──reject──> REJECTED
OPEN ──mark-duplicate──> DUPLICATE
terminal ──X──> another state
```

Terminal state требует resolver, resolved time и, для `APPROVED`/`DUPLICATE`, ссылки на resolved presentation согласно policy.

### 8.3 Aggregate `CatalogImport`

**Aggregate root:** `ImportJob`; внутренние сущности — `ImportRow`.

Состояния job:

```text
UPLOADED -> VALIDATING -> READY -> CONFIRMING -> COMPLETED
                   \-> HAS_ERRORS -> VALIDATING
UPLOADED/VALIDATING/READY/CONFIRMING -> FAILED
```

Состояния row:

- `PENDING`;
- `VALID`;
- `ERROR`;
- `MATCHED`;
- `CREATE_NEW`;
- `REJECTED`;
- `PUBLISHED`.

Инварианты:

1. Catalog import не имеет pharmacy scope; initial stock import обязан иметь pharmacy scope.
2. Row number уникален внутри job.
3. `READY` допускается только при отсутствии blocking errors.
4. Publish использует idempotency и утверждённую atomic/partial policy.
5. `COMPLETED` терминален.
6. Raw file content не становится частью Domain; Domain получает нормализованные значения и validation decisions.

## 9. Assortment context

### 9.1 Aggregate `PharmacyProduct`

**Aggregate root:** `PharmacyProduct`.

Это локальная продаваемая позиция, а не копия глобального Product.

Содержит:

- `PharmacyID`;
- `ProductPresentationID`;
- текущие package/inner-unit prices;
- inner unit sale policy;
- min и target stock levels;
- status;
- inventory freshness marker;
- version.

Состояния:

```text
ACTIVE <──activate/deactivate──> INACTIVE
ACTIVE/INACTIVE ──archive──> ARCHIVED
```

Инварианты:

1. Пара `(PharmacyID, ProductPresentationID)` уникальна.
2. Inner-unit sale требует inner-unit price и presentation с поддерживаемой внутренней единицей.
3. `targetStock >= minStock >= 0`.
4. Только `ACTIVE` позиция участвует в новой продаже и публичной availability projection.
5. Текущая цена не изменяет historical price snapshots.
6. `inventoryChangedAt` изменяется только успешной складской транзакцией, не обычным PATCH ассортимента.

## 10. Inventory context

### 10.1 Командная граница `InventoryLedger`

`InventoryLedger` является доменной командной границей для согласованного изменения нескольких `StockLot`. Он не обязан материализоваться одной таблицей или загружаться как огромный объект.

Причина: одна Sale или Return может изменить несколько лотов. Моделирование каждого `StockLot` полностью независимым aggregate root без общей транзакционной координации позволило бы частичный commit и нарушило бы атомарность документа.

Командный inventory boundary включает:

- заблокированные `PharmacyProduct`;
- выбранные `StockLot`;
- новый `InventoryOperation`;
- новые append-only `InventoryMovement`;
- проверку resulting balances.

Application загружает только затронутый working set в детерминированном порядке.

### 10.2 Entity `StockLot`

Сущность имеет идентичность и изменяемый текущий остаток.

Свойства:

- `StockLotID`;
- `PharmacyProductID`;
- origin;
- batch;
- expiration;
- current base-unit quantity;
- historical price/package snapshots;
- received time;
- `StockLotStatus`;
- version.

Состояния:

```text
ACTIVE ──quantity becomes 0──> DEPLETED
DEPLETED ──valid restock──> ACTIVE
ACTIVE/DEPLETED ──quarantine──> QUARANTINED
QUARANTINED ──approved release──> ACTIVE/DEPLETED
ACTIVE/DEPLETED/QUARANTINED ──archive──> ARCHIVED
```

Допуск к продаже определяется не только статусом:

```text
sellable = status == ACTIVE
           AND quantity > 0
           AND expirationDate >= businessDate
           AND pharmacy/product are active
```

Инварианты:

1. Quantity никогда не отрицательна.
2. Каждое изменение quantity имеет ровно одно соответствующее movement в той же транзакции.
3. `quantityAfter` movement совпадает с новым состоянием lot.
4. Lot origin и ссылка на receipt/return согласованы.
5. Просроченный lot не переводится в продаваемый `ACTIVE`.
6. Изменение batch, expiration и snapshots после создания запрещено, кроме отдельной корректирующей policy до начала операций; по умолчанию — запрещено.

### 10.3 Entity `InventoryOperation`

`InventoryOperation` — неизменяемый заголовок складского эффекта.

Типы:

- `RECEIPT`;
- `INITIAL_STOCK`;
- `SALE`;
- `RETURN_TO_STOCK`;
- `RETURN_WRITE_OFF`;
- `RETURN_QUARANTINE`;
- `WRITE_OFF`;
- `INVENTORY_ADJUSTMENT`;
- `REVERSAL`.

Состояния:

```text
POSTED ──reverse by separate operation──> REVERSED
```

Исходная операция не переписывается. `REVERSAL` ссылается на одну исходную operation; одна operation не может быть сторнирована дважды.

### 10.4 Entity `InventoryMovement`

Append-only проводка:

- operation;
- lot;
- signed delta;
- quantity after;
- occurred/created time.

У movement нет update/delete command. Исправление создаёт новую compensating operation и movement.

### 10.5 Aggregate `Receipt`

**Aggregate root:** `Receipt`; внутренние сущности — `ReceiptItem`.

Receipt в базовом MVP создаётся сразу проведённым.

Состояния:

```text
POSTED ──reverse──> REVERSED
```

Инварианты:

1. Document number уникален в аптеке.
2. Все items относятся к ассортименту той же аптеки.
3. Quantity packages и base units согласованы.
4. Posting атомарно создаёт receipt, items, lots, operation, movements, idempotency result и audit.
5. Posted receipt и items неизменяемы.
6. Reverse является отдельным use case с отдельной operation; он не удаляет исходный receipt.

### 10.6 Aggregate `WriteOff`

**Aggregate root:** `WriteOff`; внутренние сущности — `WriteOffItem`.

Состояния: `COMPLETED`, `REVERSED`.

Правила:

- reason обязателен;
- quantity списания положительна;
- все lots принадлежат pharmacy;
- недостаток quantity отклоняет всю команду;
- document, movements, balances, audit и idempotency атомарны.

### 10.7 Aggregate `InventoryAdjustment`

**Aggregate root:** `InventoryAdjustment`; внутренние сущности — `InventoryAdjustmentItem`.

Каждая строка фиксирует expected, actual и delta.

Инварианты:

- `delta = actual - expected`;
- actual не отрицателен;
- approval обязателен согласно elevated-permission policy;
- adjustment не является способом переписать movement history;
- положительные и отрицательные движения фиксируются явно;
- все изменения атомарны.

## 11. Sales context

### 11.1 Aggregate `Sale`

**Aggregate root:** `Sale`.

Внутренние сущности:

- `SaleItem`;
- `SaleItemAllocation`.

Состояния:

```text
COMPLETED ──partial return──> PARTIALLY_REFUNDED
COMPLETED/PARTIALLY_REFUNDED ──full return──> REFUNDED
COMPLETED ──approved reversal──> REVERSED
PARTIALLY_REFUNDED/REFUNDED ──X──> REVERSED без специальной policy
```

В базовом MVP sale создаётся сразу `COMPLETED`; draft aggregate отсутствует.

Инварианты:

1. Sale имеет минимум одну item.
2. В одной команде пара `(PharmacyProductID, SaleUnit)` уникальна.
3. Все items принадлежат pharmacy продажи.
4. Количество каждой item в base units вычисляется backend.
5. Unit price и totals вычисляются backend из заблокированного актуального состояния и snapshots.
6. Сумма allocations каждой item равна quantity item.
7. Allocation ссылается только на sellable lot того же PharmacyProduct.
8. Выбор lots выполняется строго FEFO: expiration, receivedAt, ID.
9. Нехватка одной item отклоняет всю sale.
10. Prescription-required item требует подтверждения согласно утверждённой policy и audit.
11. Completed sale, items и allocations неизменяемы.
12. Returned quantity не хранится как свободно редактируемый counter; вычисляется/проверяется по завершённым return allocations под блокировкой.

Методы/фабрики:

- `PrepareSale` — чистая валидация command lines;
- `AllocateFEFO` — domain service;
- `Complete` — создаёт неизменяемый aggregate из рассчитанных items/allocations;
- `MarkPartiallyRefunded`;
- `MarkRefunded`;
- `MarkReversed`.

Domain events:

- `SaleCompleted`;
- `SalePartiallyRefunded`;
- `SaleRefunded`;
- `SaleReversed`.

### 11.2 Domain service `FEFOAllocator`

Вход:

- требуемая quantity;
- ordered sellable lot snapshots;
- business date.

Выход:

- набор `LotAllocation` либо `InsufficientStock`.

Service является чистым: он не выполняет SQL и не блокирует строки. Repository обязан предоставить lots уже в нормативном порядке и под необходимыми locks. Application повторно проверяет результат перед persistence.

### 11.3 Domain service `SalePricingPolicy`

Рассчитывает line subtotal, discount и totals. Клиентские totals могут использоваться только для обнаружения расхождения, но не как источник истины.

Policy обязана:

- работать с `Money`;
- проверять overflow;
- фиксировать применённые snapshots;
- быть детерминированной для одного входа.

## 12. Returns context

### 12.1 Aggregate `SaleReturn`

**Aggregate root:** `SaleReturn`.

Внутренние сущности:

- `SaleReturnItem`;
- `SaleReturnItemAllocation`.

Состояния:

```text
COMPLETED ──approved reversal──> REVERSED
```

В MVP возврат создаётся сразу completed после всех проверок. Юридически запрещённый сценарий не создаёт aggregate.

`ReturnAction`:

- `RESTOCK`;
- `WRITE_OFF`;
- `QUARANTINE`;
- `NO_PHYSICAL_RETURN`.

Инварианты:

1. Return относится к одной исходной Sale.
2. Каждая return item относится к item этой Sale.
3. Каждая return allocation относится к исходной allocation той же sale item.
4. Сумма return allocations равна returned quantity item.
5. Нельзя вернуть больше исходной allocation с учётом всех completed non-reversed returns.
6. Нельзя вернуть больше sale item quantity.
7. Refund рассчитывается backend по immutable sale snapshots и refund policy.
8. Total refund равен сумме item refunds.
9. Только `RESTOCK` требует target stock lot и увеличивает sellable quantity.
10. Для `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN` sellable target lot отсутствует.
11. Просроченный/повреждённый/сомнительный товар не возвращается в active sellable lot.
12. Completed return, items и allocations неизменяемы.
13. Sale status обновляется в той же транзакции.
14. Return production command остаётся недоступной до утверждения legal policy.

### 12.2 Domain service `ReturnEligibilityPolicy`

Вход:

- immutable Sale snapshot;
- previous completed return allocations;
- requested return lines;
- legal policy;
- current business time/date;
- condition/disposition data.

Выход:

- разрешённые quantities и actions;
- рассчитанный refund;
- либо domain violation.

Policy не читает БД самостоятельно. Application загружает и блокирует исходную Sale и allocations до вызова.

### 12.3 Domain service `RefundCalculator`

Рассчитывает refund на основе исходной line price/discount allocation и уже возвращённого количества. Правило округления должно быть зафиксировано до production возвратов.

## 13. Reliability context

### 13.1 Aggregate `IdempotencyRecord`

**Aggregate root:** `IdempotencyRecord`.

Состояния:

```text
IN_PROGRESS ──complete(response)──> COMPLETED
IN_PROGRESS ──retryable failure──> FAILED_RETRYABLE
FAILED_RETRYABLE ──claim/retry──> IN_PROGRESS
COMPLETED ──replay──> COMPLETED
```

Инварианты:

1. Scope уникален независимо от наличия pharmacy ID.
2. Один key в одном scope связан с одним request fingerprint.
3. Same key + same fingerprint возвращает исходный результат.
4. Same key + different fingerprint — conflict.
5. `COMPLETED` имеет сохранённый response status, completion time и replayable result/reference.
6. Запись критической команды создаётся/захватывается и завершается атомарно с бизнес-эффектом.
7. Неизвестный результат после сетевого разрыва безопасно читается повтором.
8. Expiration не означает разрешение повторно выполнить юридически значимый документ без дополнительной business uniqueness.

Idempotency — application/reliability concern, но его состояния и инварианты являются частью модели согласованности.

## 14. Audit context

### 14.1 Entity/Aggregate `AuditEvent`

`AuditEvent` является append-only aggregate root без команд изменения.

Содержит:

- actor type;
- user/session IDs при user actor;
- pharmacy scope;
- action;
- object type/ID;
- result;
- request/trace IDs;
- IP/user agent;
- безопасную metadata;
- occurred time.

Инварианты:

1. `USER` требует actor user; session при наличии принадлежит этому user.
2. `SYSTEM` не маскируется под user actor.
3. Metadata не содержит password, raw token, secret, full sensitive payload, SQL или stack trace.
4. Событие после insert не обновляется и не удаляется обычным runtime role.
5. Для fail-closed операций отсутствие обязательного audit event откатывает всю бизнес-транзакцию.
6. Denied authentication/authorization события могут фиксироваться в отдельной короткой транзакции, поскольку основная бизнес-транзакция не начинается.

## 15. Alerts context

### 15.1 Aggregate `Alert`

Состояния:

```text
ACTIVE ──acknowledge──> ACKNOWLEDGED
ACTIVE/ACKNOWLEDGED ──resolve──> RESOLVED
RESOLVED ──X──> ACTIVE
```

Новое обнаружение после resolved создаёт новый Alert с тем же semantic deduplication key, а не переоткрывает историческую строку, если отдельная reopen policy не утверждена.

Инварианты:

- один active/non-resolved alert на deduplication scope;
- acknowledge требует actor и time;
- resolved требует resolved time;
- alert не изменяет stock автоматически;
- reconciliation mismatch никогда не исправляется alert worker-ом автоматически.

Alerts обычно eventual-consistent и создаются post-commit/background jobs, кроме случаев, где SRS явно требует синхронное предупреждение.

## 16. Read models и объекты вне агрегатов

Следующие представления не являются командными агрегатами:

- публичная availability карточка;
- агрегированный текущий остаток по PharmacyProduct;
- inventory history;
- audit search result;
- replenishment recommendation;
- expiring stock dashboard;
- return eligibility preview;
- import report;
- reconciliation report.

Read model может объединять данные нескольких contexts и быть денормализованным. Через read model запрещены изменения domain state.

Публичная availability не раскрывает точный stock quantity, purchase price, batch, internal IDs и audit data. Её freshness определяется `inventoryChangedAt`/projection timestamp.

## 17. Доменные ошибки

Domain возвращает типизированные ошибки/категории, совместимые с централизованным mapper:

- invalid argument: structurally invalid value object/command;
- unauthenticated: не domain concern большинства агрегатов;
- forbidden: application authorization concern;
- not found: repository/application concern;
- conflict: concurrent version, duplicate business identity, idempotency mismatch;
- business rule violation: valid command нарушает invariant;
- unavailable: infrastructure/application concern.

Примеры domain codes:

- `ACCOUNT_BLOCKED`;
- `PHARMACY_INACTIVE`;
- `RESOURCE_ARCHIVED`;
- `INNER_UNIT_SALE_DISABLED`;
- `DUPLICATE_SALE_ITEM`;
- `INSUFFICIENT_STOCK`;
- `LOT_EXPIRED`;
- `LOT_QUARANTINED`;
- `PRESCRIPTION_CONFIRMATION_REQUIRED`;
- `RETURN_QUANTITY_EXCEEDED`;
- `RETURN_NOT_LEGALLY_ALLOWED`;
- `IMPORT_HAS_ERRORS`;
- `CONCURRENT_MODIFICATION`.

Domain errors не содержат SQL, table/constraint names или HTTP concepts.

## 18. Транзакционные границы

### 18.1 Общие правила

1. Одна пользовательская команда имеет одну явную транзакционную границу, если создаёт согласованный бизнес-эффект.
2. Unit of Work открывается Application layer.
3. Repository не начинает скрытую nested transaction для части многомодульного use case.
4. Isolation по умолчанию — `READ COMMITTED` с явными locks.
5. Locks берутся только после валидации формата команды, но до расчёта конкурентно изменяемых totals/limits.
6. Одинаковые сущности блокируются в детерминированном порядке по ID; FEFO выбор дополнительно сортируется по expiration/received/ID.
7. После получения locks authorization и business conditions перечитываются/перепроверяются.
8. External network calls внутри transaction callback запрещены.
9. Domain events, требующие только eventual consistency, публикуются после commit.
10. Fail-closed audit и idempotency критической команды входят в основную транзакцию.

### 18.2 Матрица use cases

| Use case | В одной транзакции | Locks / concurrency | Post-commit |
|---|---|---|---|
| Login success | user login state, new session, audit success | lock user; rate-limit state отдельно по policy | token encoding/response |
| Refresh session | old session revoke, new session, family checks, audit | lock source session; при reuse — family sessions | token response |
| Block/archive user | user status, role/session revocation, audit | lock user, active sessions in ID order | security notification |
| Assign pharmacist | user/role validation, assignment creation, audit | lock user and active assignment | none |
| End assignment | assignment end, audit | lock assignment | none |
| Update mutable catalog/assortment | version check, update, audit | optimistic version or row lock | search projection invalidation |
| Publish catalog import | claim idempotency, selected rows/products/presentations/barcodes, job completion, audit | job + rows deterministic; uniqueness constraints | indexing/projection |
| Post receipt | idempotency, authorization recheck, receipt/items, lots, operation, movements, inventory timestamps, audit | pharmacy product IDs, then lots if existing | alerts/search projection |
| Confirm initial stock | idempotency, import job/rows, receipt-equivalent document, lots, movements, audit | job, rows, products/lots deterministic | report/search projection |
| Complete sale | idempotency, scope recheck, sale/items/allocations, lot quantities, operation/movements, audit | pharmacy products by ID; sellable lots FEFO with row locks | receipt rendering, alerts/search projection |
| Complete return | idempotency, sale status, return/items/return allocations, target lots, movements, refund state, audit | sale; sale items IDs; source allocations IDs; pharmacy products IDs; lots IDs | receipt/report, alerts/search projection |
| Write-off | idempotency, document/items, lot balances, operation/movements, audit | lots by ID | alerts/search projection |
| Inventory adjustment | idempotency, document/items, lot balances, operation/movements, audit | pharmacy products/lots by ID | reconciliation/alerts/search projection |
| Reverse document | idempotency, source document status, reversal operation/movements, balances, audit | source document then affected lots deterministic | projections/alerts |
| Acknowledge/resolve alert | alert state, audit | optimistic version/row lock | none |
| Public search | read-only; transaction обычно не требуется | consistent query/projection | none |

### 18.3 Продажа: нормативный порядок

1. Validate command и duplicate lines без БД.
2. Claim/find idempotency scope.
3. Lock actor/user/assignment/pharmacy state либо перечитать его внутри транзакции.
4. Lock `PharmacyProduct` по отсортированным IDs.
5. Read/lock sellable lots в FEFO порядке.
6. Recalculate eligibility, quantities, prices and totals.
7. Build `Sale` aggregate and allocations.
8. Persist sale, items, allocations.
9. Persist inventory operation and movements; update lot balances.
10. Update inventory freshness.
11. Persist mandatory audit.
12. Complete idempotency record with response.
13. Commit.

Любая ошибка до commit откатывает весь эффект.

### 18.4 Возврат: нормативный порядок

1. Validate command and legal feature availability.
2. Claim idempotency.
3. Recheck actor, assignment and pharmacy.
4. Lock source Sale.
5. Lock selected SaleItems by ID.
6. Lock source SaleItemAllocations by ID.
7. Read completed non-reversed previous returns and re-evaluate remaining quantities.
8. For physical actions lock PharmacyProducts and target/source StockLots in deterministic order.
9. Run eligibility/refund policies.
10. Persist SaleReturn, items and return allocations.
11. Persist inventory effects according to disposition.
12. Update Sale status.
13. Persist audit and idempotency result.
14. Commit.

### 18.5 Eventual consistency boundaries

После успешного commit могут асинхронно обновляться:

- public search projection;
- low-stock/expiration alerts;
- replenishment recommendations;
- analytics/reporting projections;
- notification delivery;
- non-critical metrics.

Эти операции не могут изменить результат уже проведённой sale/receipt/return и не должны автоматически исправлять stock mismatch.

## 19. Доменные события

Минимальный каталог внутренних событий:

### Identity

- `UserCreated`;
- `UserBlocked`;
- `UserArchived`;
- `PasswordChanged`;
- `RoleAssigned`;
- `RoleRevoked`;
- `SessionCreated`;
- `SessionRotated`;
- `SessionRevoked`;
- `PharmacistAssigned`;
- `PharmacistAssignmentEnded`.

### Catalog and assortment

- `ProductCreated`;
- `ProductArchived`;
- `PresentationCreated`;
- `BarcodeAssigned`;
- `CatalogImportCompleted`;
- `PharmacyProductActivated`;
- `PharmacyProductPriceChanged`.

### Inventory and trade

- `ReceiptPosted`;
- `InitialStockConfirmed`;
- `SaleCompleted`;
- `SalePartiallyRefunded`;
- `SaleRefunded`;
- `SaleReturnCompleted`;
- `WriteOffCompleted`;
- `InventoryAdjusted`;
- `InventoryOperationReversed`.

Domain events не являются заменой транзакции. Критический синхронный эффект записывается напрямую в той же транзакции. Event используется для post-commit reactions.

До появления надёжного outbox события могут оставаться in-process post-commit callbacks. При необходимости гарантированной доставки должен быть принят отдельный ADR и добавлена transactional outbox table.

## 20. Repository boundaries

Репозиторий определяется агрегатом/use case, а не таблицей:

- `UserRepository`;
- `SessionRepository`;
- `PharmacyRepository`;
- `AssignmentRepository`;
- `ProductRepository` / `PresentationRepository`;
- `CatalogImportRepository`;
- `PharmacyProductRepository`;
- `ReceiptRepository`;
- `InventoryRepository` с lock-oriented methods для working set;
- `SaleRepository`;
- `SaleReturnRepository`;
- `IdempotencyRepository`;
- `AuditRepository`;
- `AlertRepository`.

Допустимы специализированные методы:

- `GetForUpdate`;
- `ListSellableLotsForUpdateFEFO`;
- `LockByIDsSorted`;
- `LoadReturnUsageForUpdate`;
- `InsertMovements`;
- `ClaimIdempotency`.

Недопустим универсальный repository с `Save(any)` или раскрытие `pgx.Tx` в Domain/Application API.

Read repositories/query services отделяются от command repositories и могут возвращать специализированные projections без восстановления агрегата.

## 21. Authorization и domain model

RBAC и scope authorization координируются Application layer, но Domain хранит состояния, на которых основано решение:

- User status;
- active RoleAssignment;
- active PharmacyAssignment;
- Pharmacy status;
- resource status.

Критическая команда обязана повторно проверить эти состояния внутри транзакции. JWT claims используются как подсказка идентичности, а не как окончательный источник прав.

Domain aggregate не должен самостоятельно обращаться в identity repository. Application передаёт подтверждённый `Actor`/`AuthorizationContext` как value object:

```text
Actor {
    userID
    sessionID?
    role
    pharmacyScope?
    isSystem
}
```

`Actor` не заменяет повторную repository-проверку для stale-sensitive команды.

## 22. Инварианты между агрегатами

Следующие правила принадлежат use case и не могут быть обеспечены одним aggregate:

1. Role пользователя соответствует создаваемому PharmacyAssignment.
2. PharmacyProduct принадлежит Pharmacy документа.
3. StockLot принадлежит PharmacyProduct из той же Pharmacy.
4. Sale allocation относится к lot той же sale item.
5. Return item/allocation принадлежит source Sale.
6. Совокупный возврат не превышает исходную allocation.
7. InventoryOperation соответствует business document и pharmacy.
8. Idempotency result соответствует реально committed resource.
9. Audit actor/session согласованы с identity state.
10. Public availability публикуется только для active pharmacy, active assortment и sellable stock.

Эти правила проверяются в Application transaction и подкрепляются FK, unique/check constraints и integration tests.

## 23. Testing requirements для Domain Model

### 23.1 Unit tests

Обязательны table-driven tests для:

- каждого value object;
- каждого state transition;
- terminal state rejection;
- Money/quantity overflow;
- package/base-unit conversion;
- FEFO allocation;
- sale totals;
- return limits и refund rounding;
- lot status transitions;
- idempotency state machine;
- session rotation/reuse semantics.

Domain unit tests не используют PostgreSQL, Gin и реальные часы.

### 23.2 Integration tests

Обязательны tests с PostgreSQL для:

- unique active role и повторного назначения;
- active pharmacy assignment races;
- refresh rotation concurrency;
- same idempotency key concurrency;
- global idempotency scope;
- two simultaneous sales against same lots;
- sale versus write-off/adjustment;
- two simultaneous returns;
- return versus sale/reversal;
- operation/movement/balance atomicity;
- fail-closed audit rollback;
- optimistic concurrency;
- reversal uniqueness;
- alert dedup lifecycle.

Каждый test проверяет не только ответ, но и итоговые documents, balances, movements, audit and idempotency records.

## 24. Anti-patterns

Запрещены:

- anemic entities, в которых все invariants находятся в handler/service setters;
- один гигантский `Pharmacy` aggregate со всем каталогом, лотами и продажами;
- один aggregate на каждую таблицу без анализа consistency boundary;
- загрузка всей истории movements внутрь StockLot для обычной продажи;
- изменение aggregate другого context прямым repository вызовом из Domain;
- передача database models в HTTP response;
- trusted client totals/stock/role;
- скрытые repository transactions;
- domain event как замена обязательной атомарной записи;
- update/delete проведённых documents, movements и audit;
- `time.Now()` внутри правил, которые должны детерминированно тестироваться;
- generic status setter;
- generic `Save(entity interface{})`;
- использование nullable primitive вместо выраженного value object, когда отсутствие имеет бизнес-смысл.

## 25. Открытые решения

До production-ready реализации необходимо отдельно утвердить:

1. юридическую `ReturnEligibilityPolicy`;
2. правила распределения line discount и округления partial refund;
3. необходимость отдельного `Permission` model сверх ролей MVP;
4. session TTL, absolute lifetime и reuse response;
5. atomic или partial publish catalog import;
6. elevated approval policy для reversal и inventory adjustment;
7. outbox requirement для гарантированной доставки post-commit событий;
8. точную модель initial stock document: специализированный aggregate или Receipt subtype;
9. reopen policy alerts;
10. политику исправления ошибочных catalog snapshots до первой операции.

Открытый вопрос не разрешается неявно в коде. До принятия решения соответствующая production-функция должна оставаться ограниченной или disabled.

## 26. Definition of Done для domain feature

Domain feature завершена только если:

1. определён aggregate root и consistency boundary;
2. invariants выражены в Domain и БД там, где возможно;
3. state transitions не реализованы общим setter;
4. value objects валидируют значения при создании;
5. Application use case фиксирует authorization и transaction boundary;
6. lock order документирован и протестирован;
7. idempotency и audit включены для критической команды;
8. domain errors mapped через централизованный error responder;
9. unit, integration и concurrency tests покрывают happy path и нарушения;
10. SRS, API Design, Database Design и этот документ синхронизированы;
11. post-commit reactions не влияют на уже committed business result;
12. исторические данные не редактируются обходным CRUD endpoint-ом.
