# PharmacyCRM — Domain Model

**Статус документа:** Draft  
**Версия:** 2.0  
**Дата:** 2026-07-21  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`  
**Связанные ADR:** ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет целевую доменную модель PharmacyCRM: bounded contexts, агрегаты, aggregate roots, сущности, value objects, состояния, доменные сервисы, события, межагрегатные инварианты и транзакционные границы.

Он отвечает на вопросы:

- какой объект владеет конкретным бизнес-инвариантом;
- какие изменения разрешены только через aggregate root;
- какие данные являются сущностями, а какие value objects;
- какие состояния и переходы допустимы;
- какие операции обязаны commit или rollback как единое целое;
- где допустима eventual consistency;
- какие правила принадлежат Domain, Application и Infrastructure.

Domain Model не является схемой БД и не должен механически повторять таблицы. Одна таблица не обязательно равна одному агрегату, а один use case может координировать несколько агрегатов через Unit of Work.

При противоречии применяется порядок приоритетов из SRS. Изменение модели, влияющее на внешнее поведение, синхронно обновляет SRS, API Design, Database Design и соответствующие ADR.

## 2. Термины моделирования

- **Entity** — объект с устойчивой идентичностью и жизненным циклом.
- **Value Object** — неизменяемое значение без собственной идентичности; равенство определяется содержимым.
- **Aggregate** — минимальная согласованная группа domain objects с одной внешней точкой изменения.
- **Aggregate Root** — сущность, через которую выполняются все изменения агрегата.
- **Domain Service** — чистое бизнес-правило, которое естественно не принадлежит одной сущности.
- **Application Service / Use Case** — координация авторизации, транзакции, репозиториев, агрегатов, идемпотентности и post-commit действий.
- **Domain Event** — факт, уже произошедший в домене; именуется в прошедшем времени.
- **Transaction Boundary** — набор чтений и изменений, которые должны commit или rollback как единое целое.
- **Invariant** — правило, которое не может быть нарушено ни одним допустимым состоянием системы.
- **Read Model** — проекция для чтения, не являющаяся командным агрегатом и не владеющая бизнес-инвариантами записи.

## 3. Bounded contexts и владение моделью

| Context / модуль | Ответственность | Aggregate roots / command models |
|---|---|---|
| Identity | пользователи, credentials, роли, sessions | `User`, `RoleAssignment`, `UserSession` |
| Pharmacy | аптеки и назначения аптекарей | `Pharmacy`, `PharmacyAssignment` |
| Catalog | глобальные карточки, фасовки, штрихкоды, requests и staging | `Product`, `ProductPresentation`, `ProductRequest`, `ImportJob` |
| Assortment | локальная продаваемая позиция и правила отпуска | `PharmacyProduct` |
| Inventory | поступления, лоты, движения, списания, корректировки | `Receipt`, `WriteOff`, `InventoryAdjustment`; транзакционный `InventoryWorkingSet` |
| Sales | продажа, строки, цены и FEFO-аллокации | `Sale` |
| Returns | возврат по исходной продаже и аллокациям | `SaleReturn` |
| Reliability | идемпотентность и durable post-commit delivery | `IdempotencyRecord`, `OutboxEvent` |
| Audit | неизменяемые события расследования | `AuditEvent` |
| Alerts | предупреждения и lifecycle | `Alert` |
| Search | публичная проекция наличия | read models |
| Replenishment | рекомендации пополнения | вычисляемые read models |

Один context может хранить внешний ID другого context, но не получает право изменять внешний агрегат. Межконтекстные команды координируются Application layer через публичные порты и Unit of Work.

## 4. Общие правила агрегатов

1. Aggregate root является единственной точкой изменения внутренних сущностей.
2. Внешний код не получает mutable-ссылки на внутренние коллекции.
3. Конструктор создаёт только валидный объект; repository reconstruction проверяет структурную целостность persisted state.
4. Методы выражают бизнес-намерение: `Block`, `Assign`, `Post`, `Complete`, `Reverse`, а не общие setters.
5. Domain methods не принимают `gin.Context`, DTO, `pgx.Tx`, SQL-модели или HTTP status codes.
6. Время, ID и криптографические значения передаются готовыми значениями или через узкие порты.
7. Межагрегатная уникальность, authorization scope и конкурентная проверка обеспечиваются Application + repository + database constraints.
8. Проведённый документ после commit является историческим и не восстанавливается как свободно изменяемый draft.
9. Массовые импорты моделируются staging job, а не огромным in-memory aggregate.
10. Domain не начинает и не commit-ит транзакции.
11. Repository загружает только состояние, необходимое конкретной команде; полная историческая коллекция не должна становиться обязательной частью агрегата.
12. Aggregate method либо возвращает новое валидное состояние/domain event, либо domain error; частично изменённое состояние наружу не выходит.

## 5. Общие value objects

### 5.1 Типизированные идентификаторы

Используются отдельные типы: `UserID`, `RoleAssignmentID`, `SessionID`, `PharmacyID`, `PharmacyAssignmentID`, `ProductID`, `ProductPresentationID`, `PharmacyProductID`, `StockLotID`, `InventoryOperationID`, `ReceiptID`, `SaleID`, `SaleItemID`, `SaleItemAllocationID`, `SaleReturnID`, `IdempotencyRecordID`, `OutboxEventID`, `AuditEventID`, `AlertID`.

Общий `string` или `uuid.UUID` не используется во всех domain-сигнатурах без типовой обёртки.

### 5.2 `Money`

```text
Money {
    amountDirams: int64
    currency: TJS
}
```

Правила:

- цена и абсолютная сумма неотрицательны;
- отрицательный эффект выражается типом операции, а не отрицательной ценой;
- сложение и сравнение разрешены только для одной валюты;
- overflow проверяется;
- округление выполняется утверждённой policy.

### 5.3 Количества

- `BaseUnitQuantity`: `int64 >= 0`;
- `PositiveBaseUnitQuantity`: `int64 > 0`;
- `PackageQuantity`: `int64 > 0`;
- `BaseUnitsPerPackage`: `int64 > 0`;
- `SignedBaseUnitDelta`: ненулевой `int64`.

Преобразование `packages × baseUnitsPerPackage` проверяет overflow.

### 5.4 Продажа и цены

`SaleUnit`: `PACKAGE` или `INNER_UNIT`.

Для `PACKAGE` списание равно `displayQuantity × baseUnitsPerPackageSnapshot`; для `INNER_UNIT` — `displayQuantity`.

```text
PriceSnapshot {
    packagePrice: Money
    innerUnitPrice: Money?
    baseUnitsPerPackage: BaseUnitsPerPackage
}
```

Изменение текущей цены не изменяет snapshot проведённого документа.

### 5.5 Каталожные значения

`Barcode`, `ProductName`, `INN`, `Dosage`, `DosageForm`, `ManufacturerName`, `BatchNumber`, `ExpirationDate`, `DocumentNumber` являются нормализованными value objects. Нормализация поиска и уникальности не уничтожает исходное отображаемое значение.

### 5.6 Identity и security values

- `Login`;
- `PasswordHash` — opaque value;
- `RefreshTokenHash` — opaque bytes;
- `TokenFamilyID`;
- `IPAddress`;
- `UserAgent`;
- `RequestID`;
- `TraceID`.

Domain не знает алгоритм password/token hashing.

### 5.7 Идемпотентность

- `IdempotencyKey` — непустая строка до 128 символов;
- `OperationName` — стабильное имя команды;
- `IdempotencyScope` — actor + operation + pharmacy/global scope;
- `RequestFingerprint` — hash канонического смыслового payload.

Transport-only поля, порядок JSON-ключей и request ID не входят в fingerprint. Path parameters, effective scope и версия ресурса, влияющие на смысл команды, входят.

### 5.8 `GeoPoint`, `Reason`, `Actor`

```text
GeoPoint {
    latitude: [-90, 90]
    longitude: [-180, 180]
}
```

`Reason` — непустой trimmed text. Для чувствительных операций reason сохраняется в историческом документе и/или audit metadata.

```text
Actor {
    userID?
    sessionID?
    role?
    pharmacyScope?
    actorType: USER | SYSTEM
}
```

`USER` требует `userID`; `SYSTEM` не содержит user identity. `Actor` не заменяет stale-sensitive repository revalidation.

## 6. Identity context

### 6.1 Aggregate `User`

`User` владеет credentials, profile, account status, login failure state и optimistic version. История ролей не загружается внутрь `User`: она принадлежит отдельным `RoleAssignment`, чтобы размер User aggregate не рос бесконечно.

`UserStatus`:

```text
ACTIVE ──block──> BLOCKED
BLOCKED ──unblock──> ACTIVE
ACTIVE/BLOCKED ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/BLOCKED
```

Правила:

1. `ARCHIVED` — терминальное состояние для новых операций.
2. `BLOCKED` и `ARCHIVED` запрещают новые sessions и защищённые команды.
3. Block/archive отзывают активные sessions в той же application-транзакции.
4. Block/archive не переписывают и не отзывают роль автоматически: роль — отдельная историческая бизнес-сущность; её отзыв выполняется явной командой.
5. Login уникален согласно Database Design.
6. Изменение password hash фиксирует `passwordChangedAt` и отзывает sessions согласно security policy.
7. Failed-login counters не используются как источник authorization вне User aggregate.

Команды: `ChangeProfile`, `ChangePasswordHash`, `RecordFailedLogin`, `RecordSuccessfulLogin`, `Block`, `Unblock`, `Archive`.

События: `UserCreated`, `UserBlocked`, `UserUnblocked`, `UserArchived`, `UserPasswordChanged`.

### 6.2 Aggregate `RoleAssignment`

Отдельный aggregate root сохраняет историю назначения и отзыва роли.

Состояния:

```text
ACTIVE ──revoke(reason)──> REVOKED
REVOKED ──X──> ACTIVE
```

Повторное назначение создаёт новый aggregate.

Инварианты:

1. У пользователя не более одной active role assignment в MVP.
2. Назначение `ADMIN` и `PHARMACIST` выполняется только разрешённым администратором.
3. `assignedBy`, `revokedBy`, timestamps и reason формируют неизменяемую историю.
4. Отзыв роли не удаляет пользователя и не изменяет старые документы.
5. Отзыв активной роли отзывает sessions в той же transaction boundary, если после отзыва пользователь теряет защищённый доступ.

События: `UserRoleAssigned`, `UserRoleRevoked`.

### 6.3 Aggregate `UserSession`

Session имеет собственный lifecycle, конкурентную refresh rotation и retention.

Вычисляемые состояния:

- `ACTIVE`: не отозвана и `now < expiresAt`;
- `EXPIRED`: не отозвана и `now >= expiresAt`;
- `REVOKED`: `revokedAt != null`.

```text
ACTIVE ──rotate──> REVOKED(ROTATED) + new ACTIVE session
ACTIVE ──logout/block/password-change/admin-revoke──> REVOKED
ACTIVE ──time──> EXPIRED
REVOKED/EXPIRED ──X──> ACTIVE
```

Правила:

1. Raw refresh token не входит в persisted entity; хранится hash.
2. Rotation создаёт новую session и отзывает предыдущую атомарно.
3. Session не может ссылаться сама на себя.
4. Одна source session порождает не более одной следующей session.
5. Reuse rotated token отзывает всю token family.
6. Session не подтверждает authorization без проверки текущего User, role и assignment.
7. `lastUsedAt >= createdAt`; частота touch определяется security design и не обязана порождать write на каждый запрос.

## 7. Pharmacy context

### 7.1 Aggregate `Pharmacy`

Содержит публичный профиль, `GeoPoint`, `PharmacyStatus` и optimistic version.

```text
ACTIVE ──block──> BLOCKED
BLOCKED ──activate──> ACTIVE
ACTIVE/BLOCKED ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/BLOCKED
```

Инварианты:

1. Только `ACTIVE` принимает новые receipt, sale, return-to-stock, write-off и adjustment команды.
2. `BLOCKED` сохраняет чтение истории и расследование.
3. `ARCHIVED` не участвует в публичном поиске и новых назначениях.
4. Исторические документы сохраняют `PharmacyID`.
5. Профиль изменяется с optimistic concurrency.

### 7.2 Aggregate `PharmacyAssignment`

```text
ACTIVE ──end(reason)──> ENDED
ENDED ──X──> ACTIVE
```

Повторное назначение создаёт новый aggregate.

Инварианты:

1. Пользователь имеет active role `PHARMACIST`.
2. Аптека не `ARCHIVED`.
3. В MVP у аптекаря не более одного active assignment.
4. `assignedBy` и `endedBy` являются разрешёнными администраторами.
5. `pharmacy_id` из команды не заменяет assignment validation.

## 8. Catalog context

### 8.1 Aggregate `Product`

`Product` владеет общими лекарственными атрибутами: название, МНН, форму, дозировку, производителя, страну, рецептурность, status и version.

`ProductPresentation` не является внутренней коллекцией Product aggregate. Это отдельный aggregate root, связанный через `ProductID`. Такое разделение нормативно для MVP и предотвращает неограниченный рост Product aggregate и конфликт независимых изменений фасовок.

```text
ACTIVE <──activate/deactivate──> INACTIVE
ACTIVE/INACTIVE ──archive──> ARCHIVED
ARCHIVED ──X──> ACTIVE/INACTIVE
```

Изменение Product действует только на будущие операции и не переписывает snapshots.

### 8.2 Aggregate `ProductPresentation`

Владеет package data, `BaseUnitsPerPackage`, inner-unit semantics, status, version и дочерними `ProductBarcode`.

Инварианты:

1. Barcode глобально уникален согласно Database Design.
2. У presentation не более одного active primary barcode.
3. Если `baseUnitsPerPackage > 1`, inner unit name обязателен.
4. Архивирование запрещает новые assortment links и операции, но сохраняет историю.
5. Presentation не удаляется при наличии ссылок из ассортимента или истории.
6. Barcode изменяется только через presentation root; наружу не выдаётся mutable collection.

### 8.3 Aggregate `ProductRequest`

```text
OPEN ──approve──> APPROVED
OPEN ──reject──> REJECTED
OPEN ──mark-duplicate──> DUPLICATE
terminal ──X──> another state
```

Terminal state требует resolver и resolved time. `APPROVED`/`DUPLICATE` требуют resolved presentation согласно утверждённой policy.

### 8.4 Aggregate `ImportJob`

Внутренние сущности: `ImportRow`.

```text
UPLOADED -> VALIDATING -> READY -> CONFIRMING -> COMPLETED
                   \-> HAS_ERRORS -> VALIDATING
UPLOADED/VALIDATING/READY/HAS_ERRORS/CONFIRMING -> FAILED
```

Инварианты:

1. Catalog import имеет global scope; initial stock import — pharmacy scope.
2. Row number уникален внутри job.
3. `READY` требует отсутствие blocking errors.
4. Publish использует idempotency и утверждённую atomic/partial policy.
5. `COMPLETED` и `FAILED` терминальны.
6. Raw file не становится частью Domain; Domain получает нормализованные строки и validation decisions.
7. Job counters согласованы с row states и проверяются перед terminal transition.

## 9. Assortment context

### 9.1 Aggregate `PharmacyProduct`

Локальная продаваемая позиция, а не копия Product.

Содержит pharmacy/presentation IDs, текущие цены, inner-unit policy, min/target levels, status, inventory freshness marker и version.

```text
ACTIVE <──activate/deactivate──> INACTIVE
ACTIVE/INACTIVE ──archive──> ARCHIVED
```

Инварианты:

1. `(PharmacyID, ProductPresentationID)` уникальна.
2. Inner-unit sale требует цены и presentation с внутренней единицей.
3. `targetStock >= minStock >= 0`.
4. Только `ACTIVE` участвует в новой продаже и public availability.
5. Текущая цена не изменяет historical snapshots.
6. `inventoryChangedAt` изменяется только успешной складской транзакцией.

## 10. Inventory context

### 10.1 Транзакционная модель `InventoryWorkingSet`

`InventoryWorkingSet` — не aggregate root и не persisted entity. Это ограниченный domain command model, собираемый Application layer внутри одной Unit of Work для атомарного изменения нескольких `StockLot`.

Он включает:

- заблокированные `PharmacyProduct` snapshots;
- выбранные `StockLot`;
- создаваемый `InventoryOperation`;
- создаваемые append-only `InventoryMovement`;
- проверку resulting balances.

Application загружает только затронутый working set в детерминированном порядке. Нельзя превращать его в долгоживущий глобальный aggregate всей аптеки.

### 10.2 Entity `StockLot`

`StockLot` имеет идентичность и изменяемый current quantity, но изменяется только через inventory command boundary.

```text
ACTIVE ──quantity becomes 0──> DEPLETED
DEPLETED ──valid restock──> ACTIVE
ACTIVE/DEPLETED ──quarantine──> QUARANTINED
QUARANTINED ──approved release──> ACTIVE/DEPLETED
ACTIVE/DEPLETED/QUARANTINED ──archive──> ARCHIVED
```

Sellability определяется policy:

```text
status == ACTIVE
AND quantity > 0
AND expiration policy allows sale on businessDate
AND pharmacy and assortment are active
```

Точная трактовка продажи в календарную дату срока годности должна быть подтверждена legal/product policy; Domain не зашивает её неявно.

Инварианты:

1. Quantity не отрицательна.
2. Каждое изменение quantity имеет соответствующий movement в той же transaction.
3. `quantityAfter` совпадает с новым состоянием lot.
4. Origin и ссылка на receipt/return согласованы.
5. Просроченный lot не переводится в sellable `ACTIVE`.
6. Batch, expiration и snapshots после создания неизменяемы без отдельной корректирующей policy.

### 10.3 `InventoryOperation` и `InventoryMovement`

Operation types: `RECEIPT`, `INITIAL_STOCK`, `SALE`, `RETURN_TO_STOCK`, `RETURN_WRITE_OFF`, `RETURN_QUARANTINE`, `WRITE_OFF`, `INVENTORY_ADJUSTMENT`, `REVERSAL`.

```text
POSTED ──reverse by separate operation──> REVERSED
```

Исходная operation не переписывается. Одна operation сторнируется не более одного раза. Movement append-only и не имеет update/delete command.

### 10.4 Aggregate `Receipt`

Root: `Receipt`; entities: `ReceiptItem`.

```text
POSTED ──reverse──> REVERSED
```

Инварианты:

1. Document number уникален в аптеке.
2. Items относятся к ассортименту той же аптеки.
3. Package/base-unit quantities согласованы.
4. Posting атомарно создаёт receipt, items, lots, operation, movements, idempotency result и audit.
5. Posted receipt/items неизменяемы.
6. Reverse — отдельный compensating use case.

### 10.5 Aggregate `WriteOff`

Root: `WriteOff`; entities: `WriteOffItem`; states: `COMPLETED`, `REVERSED`.

Reason обязателен; quantities положительны; lots принадлежат pharmacy; недостаток quantity отклоняет всю команду; document, movements, balances, audit и idempotency атомарны.

### 10.6 Aggregate `InventoryAdjustment`

Root: `InventoryAdjustment`; entities: `InventoryAdjustmentItem`; states: `COMPLETED`, `REVERSED`.

Каждая строка фиксирует expected, actual и delta. `delta = actual - expected`; actual не отрицателен; elevated approval применяется согласно policy; adjustment не переписывает movement history; все эффекты атомарны.

## 11. Sales context

### 11.1 Aggregate `Sale`

Entities: `SaleItem`, `SaleItemAllocation`.

```text
COMPLETED ──partial return──> PARTIALLY_REFUNDED
COMPLETED/PARTIALLY_REFUNDED ──full return──> REFUNDED
COMPLETED ──approved reversal──> REVERSED
PARTIALLY_REFUNDED/REFUNDED ──X──> REVERSED без отдельной policy
```

В MVP sale создаётся сразу `COMPLETED`; draft отсутствует.

Инварианты:

1. Минимум одна item.
2. `(PharmacyProductID, SaleUnit)` уникальна в команде.
3. Все items принадлежат pharmacy продажи.
4. Base-unit quantity, prices и totals вычисляет backend.
5. Сумма allocations равна item quantity.
6. Allocation относится к sellable lot того же PharmacyProduct.
7. FEFO: expiration, receivedAt, ID.
8. Нехватка одной item отклоняет всю sale.
9. Prescription-required item требует подтверждения и audit согласно policy.
10. Completed sale/items/allocations неизменяемы.
11. Returned quantity проверяется по completed non-reversed return allocations под блокировкой, а не доверяется editable counter.

События: `SaleCompleted`, `SalePartiallyRefunded`, `SaleRefunded`, `SaleReversed`.

### 11.2 Domain service `FEFOAllocator`

Получает required quantity, ordered sellable lot snapshots и business date. Возвращает `LotAllocation[]` либо `InsufficientStock`.

Service чистый: не выполняет SQL и не блокирует строки. Repository предоставляет rows в нормативном порядке под lock; Application проверяет результат перед persistence.

### 11.3 Domain service `SalePricingPolicy`

Рассчитывает line subtotal, discount и totals, работает с `Money`, проверяет overflow, фиксирует snapshots и детерминирован для одного входа. Client totals не являются источником истины.

## 12. Returns context

### 12.1 Aggregate `SaleReturn`

Entities: `SaleReturnItem`, `SaleReturnItemAllocation`.

```text
COMPLETED ──approved reversal──> REVERSED
```

Юридически запрещённый сценарий не создаёт aggregate.

`ReturnAction`: `RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`.

Инварианты:

1. Return относится к одной Sale.
2. Return item относится к item этой Sale.
3. Return allocation относится к source allocation той же item.
4. Сумма allocations равна returned quantity.
5. Совокупный completed non-reversed return не превышает source allocation/item.
6. Refund рассчитывается по immutable sale snapshots.
7. Total refund равен сумме item refunds.
8. `RESTOCK` требует target lot и положительное movement; target может быть исходным или отдельным return lot только согласно явной suitability policy.
9. `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN` не увеличивают sellable stock и не имеют sellable target lot.
10. Непригодный товар не возвращается в active sellable lot.
11. Completed return/items/allocations неизменяемы.
12. Sale status меняется в той же transaction.
13. Customer-return command production-disabled утверждённым Gate E0 legal baseline; partial refund path требует утверждённой rounding policy.

### 12.2 `ReturnEligibilityPolicy` и `RefundCalculator`

Policy получает immutable sale snapshot, previous completed return usage, requested lines, legal policy, business time и condition/disposition data. Она не читает БД.

Refund calculator распределяет исходные line discount/refund amounts по утверждённой rounding policy. До её утверждения partial refund production path disabled.

## 13. Reliability context

### 13.1 Aggregate `IdempotencyRecord`

```text
IN_PROGRESS → COMPLETED
IN_PROGRESS → FAILED_RETRYABLE (только при отдельном committed technical outcome)
COMPLETED → replay
```

Scope identity — `actor + operation + effective_scope + key`. Same fingerprint replays committed result после current authorization/visibility revalidation; different fingerprint — conflict. Business effect и `COMPLETED` commit-ятся атомарно.

### 13.2 Aggregate `OutboxEvent`

```text
PENDING → PROCESSING → PROCESSED
PENDING/PROCESSING → PENDING (bounded retry)
PENDING/PROCESSING → DEAD_LETTER
```

`OutboxEvent` содержит stable `event_name`, `event_version`, aggregate identity, partition key, safe payload/headers, occurrence time, attempt count, availability, lease token/generation/expiry и terminal timestamps.

Инварианты:

1. row создаётся в той же transaction, что business fact, audit и completed idempotency result;
2. delivery semantics — at-least-once, consumer idempotent;
3. только current lease owner может завершить processing;
4. stale worker не завершает row после lease loss;
5. максимум 8 attempts, затем `DEAD_LETTER`;
6. processed rows retention 30 дней, dead letters 180 дней;
7. event payload не является копией HTTP DTO и не содержит secrets.

## 14. Audit context

`AuditEvent` — append-only record/aggregate root без команд изменения.

Инварианты:

1. `USER` требует actor user; session при наличии принадлежит user.
2. `SYSTEM` не маскируется под user.
3. Metadata не содержит passwords, raw tokens, secrets, full sensitive payload, SQL или stack traces.
4. Runtime role не обновляет и не удаляет audit.
5. Для fail-closed операций отсутствие audit откатывает business transaction.
6. Denied auth события могут фиксироваться отдельной короткой transaction.

## 15. Alerts context

```text
ACTIVE ──acknowledge──> ACKNOWLEDGED
ACTIVE/ACKNOWLEDGED ──resolve──> RESOLVED
RESOLVED ──X──> ACTIVE
```

Повторное обнаружение после `RESOLVED` создаёт новый Alert. Active deduplication key уникален в pharmacy scope. Acknowledge/resolve фиксируют actor, time и audit.

## 16. Read models

Следующие объекты не являются агрегатами:

- public product search result;
- pharmacy availability;
- inventory balance view;
- movement history;
- sale/receipt printable view;
- audit search result;
- replenishment recommendation;
- alert dashboard counters.

Они могут объединять данные нескольких contexts, возвращаться query services и быть eventually consistent. Через них запрещены command mutations.

## 17. Domain errors

Domain возвращает стабильные typed errors, например:

- `InvalidStateTransition`;
- `InsufficientStock`;
- `LotNotSellable`;
- `DuplicateSaleLine`;
- `PrescriptionConfirmationRequired`;
- `ReturnQuantityExceeded`;
- `ReturnNotAllowed`;
- `AssignmentConflict`;
- `SessionAlreadyRotated`;
- `IdempotencyConflict`;
- `ConcurrentModification`.

Domain errors не содержат HTTP status. Централизованный mapper переводит их в API error codes.

## 18. Transaction boundaries

### 18.1 Общие правила

1. Transaction boundary принадлежит Application use case.
2. Межмодульные атомарные операции выполняются через Unit of Work.
3. Repository не открывает скрытую transaction внутри общей команды.
4. Default isolation — `READ COMMITTED` с явными locks.
5. Locks берутся только после deterministic validation, но до чтения mutable state, влияющего на решение.
6. Одинаковые сущности блокируются по ID; FEFO rows — expiration/received/ID.
7. После locks повторно проверяются authorization и business conditions.
8. Network calls внутри transaction callback запрещены.
9. Fail-closed audit и completed idempotency result входят в основную transaction.
10. Post-commit reactions не могут изменить результат проведённой команды.

### 18.2 Command ownership matrix

| Use case | Coordinator | Primary roots / working set | Атомарный результат |
|---|---|---|---|
| Login | Identity application service | `User`, new `UserSession` | login state, session, audit |
| Refresh | Identity application service | source/new sessions, token family | rotation/reuse handling, audit |
| Block/archive user | Identity application service | `User`, active sessions | status, session revocation, audit |
| Assign/revoke role | Identity application service | `User`, `RoleAssignment`, sessions if access lost | role history, session revocation, audit |
| Assign/end pharmacist | Pharmacy application service | `User` snapshot, role, `PharmacyAssignment` | assignment history, audit |
| Catalog update | Catalog application service | `Product` or `ProductPresentation` | versioned update, audit |
| Publish import | Catalog application service | `ImportJob`, rows, catalog roots | published rows/resources, audit, idempotency |
| Post receipt | Inventory application service | `Receipt`, `InventoryWorkingSet` | document, lots, movements, audit, idempotency |
| Complete sale | Sales coordinator | `Sale`, assortment snapshots, `InventoryWorkingSet` | sale, allocations, stock, movements, audit, idempotency |
| Complete return | Returns coordinator | source `Sale`, `SaleReturn`, `InventoryWorkingSet` | return, sale status, stock/refund effect, audit, idempotency |
| Write-off | Inventory application service | `WriteOff`, `InventoryWorkingSet` | document, stock, movements, audit, idempotency |
| Adjustment | Inventory application service | `InventoryAdjustment`, `InventoryWorkingSet` | document, stock, movements, audit, idempotency |
| Reverse document | owning module coordinator | source document, reversal working set | source status, compensating operation, audit, idempotency |
| Alert transition | Alerts application service | `Alert` | state, actor/time, audit |

### 18.3 Нормативный порядок продажи

1. Validate command and duplicate lines без БД.
2. Claim/find idempotency scope.
3. Lock/revalidate actor, role, assignment и pharmacy.
4. Lock `PharmacyProduct` по IDs.
5. Read/lock sellable lots в FEFO порядке.
6. Recalculate eligibility, quantities, prices и totals.
7. Build Sale and allocations.
8. Persist sale/items/allocations.
9. Persist operation/movements и update lot balances.
10. Update inventory freshness.
11. Persist mandatory audit.
12. Complete idempotency result.
13. Commit.

### 18.4 Нормативный порядок возврата

1. Validate command, approved return mode и refund/rounding feature availability.
2. Claim idempotency.
3. Revalidate actor, assignment и pharmacy.
4. Lock source Sale.
5. Lock selected SaleItems by ID.
6. Lock source SaleItemAllocations by ID.
7. Read completed non-reversed return usage and re-evaluate remaining quantity.
8. Для physical action lock PharmacyProducts и lots в deterministic order.
9. Run eligibility/refund policies.
10. Persist SaleReturn/items/allocations.
11. Persist inventory/refund effects.
12. Update Sale status.
13. Persist audit and complete idempotency.
14. Commit.

### 18.5 Eventual consistency и доставка событий

После commit могут обновляться public search, alerts, replenishment, analytics и notifications. Если потеря reaction нарушает correctness, freshness contract, security workflow или внешний side effect, owning transaction обязана записать `OutboxEvent`.

In-process callback допустим только для явно best-effort telemetry, потеря которой не меняет business/operational guarantees. Projections имеют rebuild/reconciliation path; stock, documents, movements, audit и idempotency остаются synchronous authoritative effects.

## 19. Доменные события

Канонический catalog committed facts:

- Identity: `UserCreated`, `UserBlocked`, `UserUnblocked`, `UserArchived`, `UserPasswordChanged`, `UserRoleAssigned`, `UserRoleRevoked`, `SessionCreated`, `SessionRotated`, `SessionRevoked`;
- Pharmacy: `PharmacistAssigned`, `PharmacistAssignmentEnded`;
- Catalog: `ProductCreated`, `ProductArchived`, `PresentationCreated`, `BarcodeAssigned`, `CatalogImportCompleted`;
- Assortment: `PharmacyProductActivated`, `PharmacyProductPriceChanged`;
- Inventory: `ReceiptPosted`, `InitialStockConfirmed`, `WriteOffCompleted`, `InventoryAdjusted`, `InventoryOperationReversed`;
- Sales: `SaleCompleted`, `SalePartiallyRefunded`, `SaleRefunded`, `SaleReversed`;
- Returns: `SaleReturnCompleted`, `SaleReturnReversed`.

Событие содержит stable name/version, `occurred_at`, aggregate type/ID и минимальный safe payload. Нерегистрируемые posting aliases и generic reversal event names запрещены. Technical dot name может отличаться синтаксисом, но не семантикой.

## 20. Repository boundaries

Command repositories определяются агрегатом/use case:

- `UserRepository`;
- `RoleAssignmentRepository`;
- `SessionRepository`;
- `PharmacyRepository`;
- `AssignmentRepository`;
- `ProductRepository`;
- `PresentationRepository`;
- `ImportJobRepository`;
- `PharmacyProductRepository`;
- `ReceiptRepository`;
- `InventoryRepository` с lock-oriented methods;
- `SaleRepository`;
- `SaleReturnRepository`;
- `IdempotencyRepository`;
- `AuditRepository`;
- `AlertRepository`.

Допустимы `GetForUpdate`, `ListSellableLotsForUpdateFEFO`, `LockByIDsSorted`, `LoadReturnUsageForUpdate`, `InsertMovements`, `ClaimIdempotency`.

Недопустимы универсальный `Save(any)` и раскрытие `pgx.Tx` в Domain/Application API. Query services отделены от command repositories.

## 21. Authorization и межагрегатные инварианты

Application повторно проверяет User status, active role, active PharmacyAssignment, Pharmacy status и resource state внутри stale-sensitive transaction. JWT claims — подсказка идентичности, а не источник актуальных прав.

Межагрегатные инварианты:

1. Role соответствует PharmacyAssignment.
2. PharmacyProduct принадлежит Pharmacy документа.
3. StockLot принадлежит PharmacyProduct той же Pharmacy.
4. Sale allocation относится к lot той же item.
5. Return item/allocation принадлежит source Sale.
6. Совокупный return не превышает source allocation.
7. InventoryOperation соответствует document и pharmacy.
8. Idempotency result соответствует committed resource.
9. Audit actor/session согласованы с identity state.
10. Public availability требует active pharmacy, active assortment и sellable stock.

Они проверяются в Application transaction и подкрепляются FK, unique/check constraints и integration tests.

## 22. Testing requirements

### 22.1 Unit tests

Обязательны table-driven tests для value objects, state transitions, terminal rejection, Money/quantity overflow, conversion, FEFO, totals, return limits/rounding, lot transitions, idempotency state machine и session rotation/reuse.

Domain tests не используют PostgreSQL, Gin и реальные часы.

### 22.2 Integration/concurrency tests

Обязательны:

- unique active role и повторное назначение;
- role revoke versus session refresh;
- active pharmacy assignment races;
- refresh rotation/reuse concurrency;
- same idempotency key concurrency и global scope;
- concurrent sales against same lots;
- sale versus write-off/adjustment;
- simultaneous returns;
- return versus sale/reversal;
- operation/movement/balance atomicity;
- fail-closed audit rollback;
- optimistic concurrency;
- reversal uniqueness;
- alert dedup lifecycle;
- projection reconciliation after simulated lost post-commit callback.

Test проверяет response и итоговые documents, balances, movements, audit и idempotency records.

## 23. Anti-patterns

Запрещены:

- anemic entities с invariants только в handlers/setters;
- гигантский Pharmacy/Product/User aggregate с неограниченной историей;
- один aggregate на каждую таблицу без анализа consistency boundary;
- самостоятельный save каждого StockLot внутри одной sale;
- загрузка всей movement/role history для обычной команды;
- изменение aggregate другого context из Domain;
- database models в HTTP response;
- trusted client totals/stock/role;
- hidden repository transactions;
- event как замена атомарной записи;
- update/delete проведённых documents, movements и audit;
- `time.Now()` внутри детерминированных правил;
- generic status setter;
- generic `Save(any)`;
- nullable primitives вместо value objects, когда отсутствие имеет бизнес-смысл.

## 24. Remaining non-E0 domain decisions

1. atomic/partial catalog publish;
2. line discount allocation и partial refund rounding;
3. elevated approval model для reversal/adjustment;
4. alert reopen policy;
5. correction policy catalog snapshots до первой операции;
6. calendar-date interpretation expiration и separate return-lot suitability;
7. permission model сверх трёх ролей MVP при реальной необходимости.

Gate E0 legal baseline закрыт: customer-returned medicines не возвращаются в sellable stock. Open details не разрешают альтернативные event names, states, enums, ownership, outbox или transaction protocol.

## 25. Definition of Done для domain feature

Feature завершена только если:

1. определены aggregate root, command owner и consistency boundary;
2. invariants выражены в Domain и БД, где возможно;
3. transitions не реализованы generic setter;
4. cross-aggregate rules размещены в Application use case;
5. transaction, locks и idempotency описаны;
6. mandatory audit определён;
7. snapshots защищают историю;
8. unit/integration/concurrency tests покрывают happy path и races;
9. API и Database Design синхронизированы;
10. eventual-consistency reaction не выдаётся за гарантированную без outbox;
11. открытые legal/security policies не реализованы скрытыми defaults.
