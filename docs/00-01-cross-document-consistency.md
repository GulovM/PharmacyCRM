# PharmacyCRM — Cross-Document Consistency Amendment

**Статус документа:** Incorporated  
**Версия:** 1.1  
**Дата:** 2026-07-20  
**Связанные документы:** `00-documentation-index.md`, `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`, `13-testing-strategy.md`, `14-observability.md`

## 0. Статус инкорпорации
Все нормативные расхождения из редакции 1.0 перенесены в исходные документы `04–14` и проверены на единство ownership, transaction protocol, API paths, persisted states, enum, event naming, deployment и operational controls.
Документ имеет статус `Incorporated` и сохраняется только как история cross-document review. Он больше не является отдельным активным источником правил: приоритет имеют актуальные исходные документы и принятые ADR/policies.
Gate E0 закрыт решениями, зафиксированными в Security Design, Database Design, Development Roadmap, Deployment, Testing Strategy и Observability.

## 1. Назначение и нормативный приоритет

Документ фиксирует результаты полного cross-document review основного комплекта PharmacyCRM `00–14` и устраняет обнаруженные расхождения между детальными проектными документами.

Поправка не изменяет Product Vision, SRS и принятые ADR. Она:

- выбирает единую трактовку там, где два или несколько детальных документов описывают один механизм по-разному;
- уточняет, как следует читать двусмысленные формулировки Product Vision и SRS без изменения их продуктового смысла;
- определяет обязательные изменения, которые должны быть инкорпорированы в исходные документы;
- предотвращает реализацию нескольких несовместимых вариантов одного механизма.

При противоречии применяется порядок:

1. применимое законодательство и обязательные регуляторные требования;
2. `01-product-vision.md`;
3. `02-srs.md`;
4. принятые ADR;
5. настоящий amendment для явно перечисленных в нём конфликтов;
6. остальные детальные документы `03–14`;
7. реализация и тесты.

Если принятый ADR противоречит этой поправке, решение не изменяется молча: создаётся новый ADR со статусом `Supersedes`, после чего amendment и затронутые документы обновляются в одном change set.

До инкорпорации каждого пункта в исходные документы настоящий amendment является нормативным источником истины по соответствующему расхождению.

## 2. Результат review

Проверены следующие области:

- продуктовый scope и роли;
- системная граница и внешние зависимости;
- backend modules и ownership таблиц;
- агрегаты и transaction boundaries;
- API paths, enum и error contracts;
- identity, sessions, roles и pharmacy assignments;
- Unit of Work, idempotency, lock order и retry;
- inventory, FEFO, receipts, sales, returns, adjustments и reversals;
- transactional audit и outbox;
- deployment, migrations и recovery;
- testing strategy;
- logs, metrics, tracing, SLI/SLO и alerting;
- физические границы `backend/` и `frontend/`.

Подтверждены как согласованные:

- три роли `CLIENT`, `PHARMACIST`, `ADMIN`;
- состояния пользователя и аптеки `ACTIVE`, `BLOCKED`, `ARCHIVED`;
- публичный поиск без обязательной регистрации;
- sibling roots `backend/` и `frontend/`;
- Go backend, PostgreSQL и Gin только как HTTP delivery;
- `gin.Context` и `pgx.Tx` не выходят в Domain/Application API;
- деньги хранятся целыми дирами в `bigint`/`int64`;
- количества хранятся целыми базовыми единицами;
- frontend не является источником цены, total, stock, role или FEFO allocation;
- inventory movements, audit и проведённые документы append-only;
- возвраты не допускаются к production до утверждения юридической policy;
- public API не раскрывает точные остатки, lot IDs, закупочные цены и audit;
- Redis, broker и search engine не являются обязательными источниками истины MVP.

## 3. Нормативная модель backend modules и ownership

Единый нормативный набор backend modules:

```text
identity
pharmacy
catalog
assortment
inventory
sales
returns
reliability
audit
alerts
search
replenishment
```

Отдельные backend modules `import`, `receipt` и `adjustments` не создаются. Это предметные части уже определённых модулей.

### 3.1 Таблицы по владельцам

| Module owner | Таблицы / данные |
|---|---|
| `identity` | `users`, `roles`, `user_roles`, `user_sessions` |
| `pharmacy` | `pharmacies`, `pharmacy_assignments` |
| `catalog` | `products`, `product_presentations`, `product_barcodes`, `product_requests`, `import_jobs`, `import_rows` |
| `assortment` | `pharmacy_products` |
| `inventory` | `inventory_operations`, `inventory_movements`, `stock_lots`, `receipts`, `receipt_items`, `write_offs`, `write_off_items`, `inventory_adjustments`, `inventory_adjustment_items` |
| `sales` | `sales`, `sale_items`, `sale_item_allocations` |
| `returns` | `sale_returns`, `sale_return_items`, `sale_return_item_allocations` |
| `reliability` | `idempotency_records`, transactional outbox storage |
| `audit` | `audit_events` |
| `alerts` | `alerts` |
| `search` | public search projections/read models |
| `replenishment` | computed recommendation read models |

`pharmacy_assignments` принадлежат `pharmacy`, поскольку назначение является связью пользователя с конкретной аптекой и агрегатом `PharmacyAssignment`. Модуль `identity` предоставляет актуальное состояние пользователя и роли, но не владеет assignment history.

Catalog import и initial-stock import используют общий технический job protocol, но ownership зависит от команды:

- catalog staging rows и catalog publication принадлежат `catalog`;
- подтверждение initial stock координирует inventory use case и создаёт `Receipt`/`InventoryOperation` через `inventory`;
- raw/quarantine storage является infrastructure concern, а не отдельным bounded context.

## 4. Канонический transaction protocol критической mutation

Для authenticated critical mutation применяется следующий порядок:

1. HTTP delivery проверяет credential format, DTO, headers и limits без бизнес-изменений.
2. Application до транзакции выполняет только детерминированную validation, canonicalization и подготовку stable command values.
3. Unit of Work начинает PostgreSQL transaction.
4. Первым сериализующим lock берётся idempotency record по `actor + operation + effective scope + key`.
5. Внутри transaction повторно проверяются actor, active session, active role, pharmacy assignment, pharmacy state и иные stale-sensitive права.
6. При replay текущая authorization и result visibility перепроверяются до возврата сохранённого результата.
7. Business roots блокируются в глобальном каноническом порядке.
8. После получения locks повторно вычисляются eligibility, quantities, prices, return limits и другие mutable conditions.
9. Сохраняются business document, allocations, lot balances и append-only movements.
10. Сохраняется обязательный transactional audit.
11. Сохраняются transactional outbox rows для надёжных post-commit reactions.
12. Idempotency record переводится в `COMPLETED` с безопасным replayable result/reference.
13. Выполняется commit.
14. Успешный HTTP response возвращается только после успешного commit.

Ошибка до commit приводит к rollback всей transaction function. Retryable PostgreSQL error повторяет всю transaction function, начиная с idempotency claim и текущей revalidation.

Исключения:

- login не имеет authenticated mutation idempotency scope и использует собственную session-creation sequence;
- unauthenticated denied security events могут сохраняться отдельной короткой транзакцией;
- read-only query не обязан создавать idempotency record.

## 5. Глобальный порядок блокировок

Для use cases, затрагивающих одинаковые сущности, действует единый порядок:

1. idempotency scope/key;
2. current actor/user/session/role, если операция security-sensitive;
3. target user, если он является изменяемым ресурсом;
4. pharmacy;
5. root business document, например sale, receipt или return;
6. `pharmacy_products` по `id` по возрастанию;
7. sale items и source allocations по `id` по возрастанию;
8. stock lots в порядке `expiration_date`, затем `received_at`, затем `id`;
9. вставка новых documents, allocations, movements, audit, outbox и completed idempotency result;
10. commit.

Use case, которому не нужен один из уровней, пропускает его, но не меняет взаимный порядок остальных locks.

Pre-lock read не является достаточным основанием для stock, authorization, price или return decision.

## 6. Transactional outbox

Transactional outbox является обязательным reliability primitive для post-commit событий, потеря которых может привести к недопустимому расхождению проекции, alerts, import workflow, security reaction или внешнего side effect.

Нормативные правила:

1. outbox row создаётся в той же transaction, что business effect, обязательный audit и completed idempotency result;
2. прямой network publish внутри transaction запрещён;
3. worker использует lease/claim protocol, bounded retry, backoff и dead-letter state;
4. несколько workers не обрабатывают один lease как единоличные владельцы одновременно;
5. stale worker после потери lease не завершает job за нового владельца; используется fencing или эквивалентный guarded completion;
6. delivery semantics — at-least-once, поэтому consumer обязан быть idempotent;
7. crash после side effect, но до mark-processed, не создаёт повторный необратимый эффект;
8. protocol version видима в readiness, deployment и observability;
9. outbox backlog, oldest age, retries и dead letters наблюдаемы;
10. projections имеют rebuild/reconciliation path.

Точное DDL, retention, batch size, lease duration, dead-letter workflow и protocol versioning должны быть утверждены ADR/operational policy. До добавления outbox storage нельзя заявлять готовность reliability kernel E2.

`07-domain-model.md` должен включать `OutboxEvent` в Reliability context. `06-database-design.md` должен включать outbox storage и constraints до первых зависящих от него migrations.

## 7. Identity, sessions и authorization state

### 7.1 Assignment lifecycle

`PharmacyAssignment` использует историческую модель:

```text
ACTIVE -> ENDED
ENDED -> X
```

Завершение назначения изменяет:

- `ended_at`;
- `ended_by_user_id`;
- `end_reason`.

Поле status и optimistic `version` для assignment не считаются существующими, пока они явно не добавлены в Database Design и ADR. История assignment не удаляется.

### 7.2 Session revocation

Блокировка/архивирование пользователя, смена password, отзыв роли и подтверждённая компрометация отзывают применимые refresh sessions согласно Security Design.

Critical mutations немедленно прекращают доступ после commit изменения security state, поскольку повторно читают актуальные user/session/role/assignment records внутри transaction.

### 7.3 version-counter авторизации

Термин version-counter авторизации в диаграммах не означает существующую колонку БД.

До отдельного ADR/schema change актуальность доступа определяется через:

- `users.status` и `users.version`;
- `password_changed_at`;
- active role assignment;
- active/revoked/expired session;
- active pharmacy assignment;
- pharmacy state.

Нельзя реализовать скрытое поле version-counter авторизации, denylist или session-version protocol без синхронизации Database Design, Security Design, API/session semantics и migrations.

## 8. API paths как источник истины

`05-api-design.md` является нормативным каталогом HTTP paths. Sequence Diagrams должны использовать следующие paths:

| Сценарий | Нормативный path |
|---|---|
| Block user | `POST /api/v1/admin/users/{user_id}/block` |
| Assign pharmacist | `POST /api/v1/admin/users/{user_id}/pharmacy-assignments` |
| End assignment | `DELETE /api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}` |
| Post receipt | `POST /api/v1/pharmacies/{pharmacy_id}/receipts` |
| Complete sale | `POST /api/v1/pharmacies/{pharmacy_id}/sales` |
| Complete return | `POST /api/v1/pharmacies/{pharmacy_id}/returns` |
| Post adjustment | `POST /api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` |
| Upload catalog import | `POST /api/v1/admin/catalog-imports` |

`source sale_id` для return передаётся согласно детальному endpoint contract; pseudo-path `/api/v1/pharmacies/{pharmacy_id}/returns` не является нормативным.

Generic pseudo-path `/api/v1/{documents}/{id}/reverse` используется только как абстракция диаграммы и не является HTTP contract. Реализация использует resource-specific endpoint из API Design, например receipt reverse, return reverse или sale void.

Path parameters в документах именуются `{pharmacy_id}`, `{user_id}`, `{assignment_id}` и т. п., а не сокращённым `{id}`, когда речь идёт о нормативном контракте.

## 9. Sale и FEFO protocol

Для sale canonical sequence:

1. idempotency claim;
2. current authorization revalidation;
3. lock affected `PharmacyProduct` rows по `id`;
4. перепроверка active assortment, inner-unit policy и current server prices;
5. lock eligible lots в порядке `expiration_date`, `received_at`, `id`;
6. повторная sellability и quantity validation;
7. server-side FEFO allocation;
8. server-side totals и snapshots;
9. sale graph, balances, movements, audit, outbox и idempotency result;
10. commit.

`expiration_date, lot_id` без `received_at` не является полным нормативным FEFO order.

## 10. Return terminology и protocol

Domain/API enum `ReturnAction`:

- `RESTOCK`;
- `WRITE_OFF`;
- `QUARANTINE`;
- `NO_PHYSICAL_RETURN`.

`RETURN_TO_STOCK`, `RETURN_WRITE_OFF` и `RETURN_QUARANTINE` являются типами `InventoryOperation`, а не значениями `ReturnAction`.

Sequence labels `RETURN_TO_STOCK allowed` и `DO_NOT_RETURN_TO_STOCK` должны читаться соответственно как ветви `RESTOCK` и non-restocking action (`WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`).

Для `RESTOCK`/`QUARANTINE` target lot определяется явной suitability policy. Только `RESTOCK` увеличивает sellable stock. `WRITE_OFF`, `QUARANTINE` и `NO_PHYSICAL_RETURN` не увеличивают доступный остаток.

Return production path остаётся disabled до юридического утверждения policy и refund/rounding rules.

## 11. Import states

Domain-specific `ImportJob` использует состояния:

```text
UPLOADED
VALIDATING
READY
HAS_ERRORS
CONFIRMING
COMPLETED
FAILED
```

`legacy review-ready transport label`, `QUEUED`, `RUNNING` и `SUCCEEDED` не являются состояниями persisted `ImportJob`, если отдельное изменение Domain/Database Design не вводит их явно.

Generic async job statuses из API Design могут использоваться только для другого технического job contract или как отображаемая transport-категория, но не переопределяют persisted catalog/initial-stock import state machine.

Worker после validation переводит job в `READY`, `HAS_ERRORS` или `FAILED`.

## 12. Event naming и ownership

Domain events именуются в прошедшем времени и отражают уже committed business fact.

Канонические события:

| Context | Event |
|---|---|
| Identity | `UserCreated`, `UserBlocked`, `UserUnblocked`, `UserArchived`, `UserPasswordChanged`, `UserRoleAssigned`, `UserRoleRevoked`, `SessionCreated`, `SessionRotated`, `SessionRevoked` |
| Pharmacy | `PharmacistAssigned`, `PharmacistAssignmentEnded` |
| Catalog | `ProductCreated`, `ProductArchived`, `PresentationCreated`, `BarcodeAssigned`, `CatalogImportCompleted` |
| Assortment | `PharmacyProductActivated`, `PharmacyProductPriceChanged` |
| Inventory | `ReceiptPosted`, `InitialStockConfirmed`, `WriteOffCompleted`, `InventoryAdjusted`, `InventoryOperationReversed` |
| Sales | `SaleCompleted`, `SalePartiallyRefunded`, `SaleRefunded`, `SaleReversed` |
| Returns | `SaleReturnCompleted`, `SaleReturnReversed` |

`SaleCompleted`, `SaleReturnCompleted` и generic `DocumentReversed` не используются как отдельные domain event names, если не зарегистрированы в event catalog с собственной семантикой и version.

Technical log names используют dot-separated lower-case namespace, сохраняя ту же бизнес-семантику:

```text
pharmacy.assignment.ended
inventory.receipt.posted
sales.sale.completed
returns.sale_return.completed
inventory.adjustment.posted
inventory.operation.reversed
```

Technical event name и domain event name не обязаны иметь одинаковый синтаксис, но не должны описывать разные факты.

## 13. Idempotency scope

Полная idempotency identity:

```text
actor + operation + effective_scope + idempotency_key
```

Где:

- для pharmacy-scoped command `effective_scope = pharmacy_id`;
- для global/admin command `effective_scope = GLOBAL`;
- semantic fingerprint включает path/resource IDs, effective scope, resource version и смысловой payload;
- request ID, JSON key order и transport-only metadata не входят в fingerprint.

Формулировка SRS «идемпотентность в пределах аптеки» трактуется как pharmacy scope для pharmacy commands, а не как запрет global/admin scope. Иное намерение требует изменения самого SRS до реализации.

Replay всегда повторно проверяет текущую authorization и видимость сохранённого результата.

## 14. Lot creation

Фраза Product Vision «аптекарь может создавать партии» трактуется как создание `StockLot` через утверждённый предметный сценарий:

- posted receipt;
- confirmed initial-stock import;
- approved physical return, если legal/suitability policy разрешает создание return lot.

Generic standalone `POST stock-lot`, ручная установка начального quantity без movement и создание свободного lot вне business document запрещены.

## 15. Идентификаторы API

Внешний ID остаётся opaque string для клиента.

Database baseline использует UUID, поэтому normative API examples следует показывать UUID-строками, например:

```json
{"id":"a9aa71f1-6c24-4c6d-b2db-388ecbbbd2ea"}
```

ULID-подобные значения в старых примерах не означают отдельную ID policy. Переход на ULID/UUIDv7 требует ADR и синхронизации Database Design, migrations, API examples и test fixtures.

## 16. Error comparison и extraction

Категория ошибки для control flow и HTTP mapping сравнивается через `errors.Is()`.

`errors.As()` допускается только после определения категории либо для извлечения безопасных typed metadata, например validation details. Он не заменяет `errors.Is()` как механизм сравнения категории.

Запрещены:

- `err == sentinel` вне совместимого случая, где wrapping заведомо отсутствует;
- `err.Error()` comparison;
- substring matching;
- определение public category по driver message;
- копирование error switch в каждый handler.

Central responder является единственным HTTP mapping boundary.

## 17. Матрица инкорпорации
| Документ | Статус |
|---|---|
| `04-architecture.md` | Incorporated |
| `04-01-backend-architecture.md` | Incorporated |
| `05-api-design.md` | Incorporated |
| `06-database-design.md` | Incorporated |
| `07-domain-model.md` | Incorporated |
| `08-project-structure.md` | Incorporated |
| `09-security-design.md` | Incorporated |
| `10-sequence-diagrams.md` | Incorporated |
| `11-development-roadmap.md` | Incorporated |
| `12-deployment.md` | Incorporated |
| `13-testing-strategy.md` | Incorporated |
| `14-observability.md` | Incorporated |
## 18. Закрытие Gate E0

| Решение | Утверждённый baseline |
|---|---|
| Password hashing | Argon2id PHC string: `m=65536 KiB`, `t=3`, `p=2`, salt 16 bytes, hash 32 bytes. Rehash после успешной проверки, если algorithm/parameters ниже current policy. |
| Access token | JWT `EdDSA`/Ed25519, TTL 10 минут, обязательные `iss`, `aud`, `sub`, `sid`, `iat`, `nbf`, `exp`, `jti`, `kid`; private keys вне repository, rotation каждые 90 дней с overlap не менее 20 минут. |
| Refresh token | Opaque CSPRNG token 32 bytes; хранится только hash. Browser transport: host-only `__Secure-pharmacy_refresh`, `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`; absolute TTL 30 дней, idle TTL 7 дней, rotation при каждом refresh. |
| Session invalidation | Block/archive, password change/recovery, role revoke/change, assignment end/change и compromise отзывают все применимые sessions; refresh reuse отзывает token family; logout отзывает current session, logout-all — все sessions. |
| Transaction retry | Только `40001` и `40P01`; максимум 3 попытки на request path, whole-transaction retry, full-jitter exponential backoff от 25 ms с cap 250 ms. |
| Outbox delivery | At-least-once, batch 100, lease 30 s, guarded completion по `lease_token`, max 8 attempts, full-jitter backoff от 2 s с cap 15 min, затем `DEAD_LETTER`. |
| Retention | Sessions 90 дней после expiry/revoke; idempotency 30 дней; processed outbox 30 дней; dead letters 180 дней; application logs 30 дней hot + 180 дней archive; traces 7 дней; import source 30 дней after success/90 дней after failure; audit/inventory/sales history минимум 5 лет либо дольше по обязательному праву. |
| Proxy/CORS/CSRF | Trusted proxy CIDR allowlist, default none; forwarded headers only from trusted peers. CORS exact-origin allowlist, no wildcard with credentials. Cookie-auth endpoints require exact `Origin` and `X-CSRF-Protection: 1`; absent/mismatched browser origin is rejected. |
| RPO/RTO | PostgreSQL production target: RPO ≤ 15 минут, RTO ≤ 4 часа; daily base backup + continuous WAL archive; restore drill at least quarterly. Rebuildable projections do not raise authoritative-data RPO. |
| Returns | Customer-return mutation is production-disabled by default. Before custody transfer use sale reversal. After transfer, legally approved exception may refund, but physical goods use `QUARANTINE`, `WRITE_OFF` or `NO_PHYSICAL_RETURN`; `RESTOCK` is forbidden for customer-returned medicines. |
| Frontend package manager | `pnpm` 10.x via Corepack, exact `packageManager` pin and committed `pnpm-lock.yaml`; npm/yarn lockfiles are rejected by CI. |
| API client generation | OpenAPI 3.1 contract in `backend/api/openapi.yaml`; frontend uses pinned `openapi-typescript` + `openapi-fetch`, output under `frontend/src/shared/api/generated/`; generated code is not edited manually and CI fails on diff. |
| ADMIN MFA/recovery | TOTP MFA is mandatory for production `ADMIN`; recovery uses one-time recovery codes and audited operator-assisted reset. No security-question recovery. |
| Release/migrations | Immutable OCI images referenced by digest; migrations run as a separate one-shot job before rollout; no automatic migration on application startup; `expand → migrate/backfill → validate → contract`. |

## 19. Definition of Done для устранения противоречия

Противоречие считается устранённым только если:

1. выбран один нормативный вариант;
2. Product Vision/SRS не изменены скрыто;
3. при необходимости принят или superseded ADR;
4. все затронутые исходные документы обновлены;
5. schema/API/domain/event examples используют одинаковые terms;
6. sequence и lock order синхронизированы;
7. regression tests добавлены;
8. Documentation Index обновлён;
9. amendment отмечает пункт как incorporated;
10. отсутствует второй несовместимый implementation path.

## 20. Правило сопровождения

Любое изменение modules, ownership, transaction protocol, idempotency, lock order, outbox, assignment/session semantics, API path, domain enum, ImportJob state, event catalog, ID policy или error classification обязано проверяться против этого amendment до его полной инкорпорации.

Новый конфликт не разрешается устно или только в коде. Он либо устраняется во всех документах в одном change set, либо добавляется сюда с явно выбранным нормативным вариантом, владельцем и планом инкорпорации.
