# PharmacyCRM — Sequence Diagrams

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-20  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `09-security-design.md`

## 1. Назначение и нормативная роль

Документ фиксирует последовательности критических сценариев PharmacyCRM: участников, порядок вызовов, границы транзакций, блокировки, повторные проверки полномочий, идемпотентность, audit, commit/rollback и post-commit действия.

Диаграммы не заменяют API-контракты, Domain Model или Database Design. Они связывают эти документы в исполнимые сценарии и показывают, где именно должны соблюдаться инварианты.

Если реализация меняет порядок security-sensitive проверок, transaction boundary, lock order, idempotency protocol, audit semantics или момент публикации post-commit события, соответствующая диаграмма обновляется в том же change set.

## 2. Нотация и правила чтения

Диаграммы записаны в Mermaid `sequenceDiagram`.

Участники:

- `Browser` — недоверенный клиент;
- `HTTP` — Gin delivery, middleware и единый responder;
- `UseCase` — application service;
- `Policy` — authorization policy;
- `UoW` — Unit of Work;
- `Repo` — repository ports и PostgreSQL adapters;
- `DB` — PostgreSQL;
- `Audit` — transactional audit writer;
- `Outbox/Events` — post-commit публикация внутренних событий;
- `Worker` — фоновый процесс.

Нормативные правила:

1. `HTTP` не выполняет бизнес-логику и SQL.
2. `UseCase` координирует authorization, idempotency, UoW, lock order и domain operations.
3. Все stale-sensitive полномочия повторно проверяются в транзакции до изменения бизнес-данных.
4. Audit, необходимый для признания операции успешной, записывается до commit в той же транзакции.
5. Post-commit действия не выполняются до успешного commit.
6. Повтор транзакции после serialization failure или deadlock не должен создавать повторный эффект.
7. Ошибка на любом обязательном шаге приводит к rollback.

## 3. Вход пользователя и создание сессии

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant LoginUC as LoginUseCase
    participant UoW
    participant UserRepo
    participant SessionRepo
    participant Audit
    participant DB

    Browser->>HTTP: POST /api/v1/auth/login
    HTTP->>HTTP: Validate JSON, request size, rate limit
    HTTP->>LoginUC: Execute(normalized_login, password, request_context)
    LoginUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    LoginUC->>UserRepo: GetByNormalizedLoginForAuth()
    UserRepo->>DB: SELECT user + password hash
    DB-->>UserRepo: user state
    LoginUC->>LoginUC: Verify password hash in constant-time-compatible flow
    alt user missing or password invalid
        LoginUC->>Audit: Record login denied without user enumeration
        Audit->>DB: INSERT security event
        UoW->>DB: COMMIT
        LoginUC-->>HTTP: ErrInvalidCredentials
        HTTP-->>Browser: 401 UNAUTHENTICATED
    else user inactive
        LoginUC->>Audit: Record login denied: blocked/archived
        Audit->>DB: INSERT security event
        UoW->>DB: COMMIT
        LoginUC-->>HTTP: ErrInvalidCredentials
        HTTP-->>Browser: 401 UNAUTHENTICATED
    else valid active user
        LoginUC->>SessionRepo: Create session + refresh token hash
        SessionRepo->>DB: INSERT session
        LoginUC->>Audit: Record login success
        Audit->>DB: INSERT audit event
        UoW->>DB: COMMIT
        LoginUC-->>HTTP: access token + raw refresh token
        HTTP-->>Browser: 200 + access token; HttpOnly refresh cookie
    end
```

Инварианты:

- неизвестный пользователь и неверный пароль имеют одинаковый внешний ответ;
- raw refresh token не сохраняется;
- blocked/archived user не получает session;
- login success не возвращается, если session или audit не сохранены.

## 4. Refresh token rotation и reuse detection

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant RefreshUC
    participant UoW
    participant SessionRepo
    participant UserRepo
    participant Audit
    participant DB

    Browser->>HTTP: POST /api/v1/auth/refresh + cookie
    HTTP->>RefreshUC: Execute(raw_refresh_token, request_context)
    RefreshUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    RefreshUC->>SessionRepo: LockSessionByTokenSelector()
    SessionRepo->>DB: SELECT ... FOR UPDATE
    DB-->>SessionRepo: session family + generation
    RefreshUC->>RefreshUC: Verify token hash, expiry, generation
    RefreshUC->>UserRepo: GetCurrentUserState()
    UserRepo->>DB: SELECT status, role version
    alt token is current and user active
        RefreshUC->>SessionRepo: Mark generation used and insert next generation
        SessionRepo->>DB: UPDATE + INSERT
        RefreshUC->>Audit: Record refresh success
        Audit->>DB: INSERT audit event
        UoW->>DB: COMMIT
        RefreshUC-->>HTTP: new access + refresh token
        HTTP-->>Browser: 200; replace cookie
    else rotated token reused
        RefreshUC->>SessionRepo: Revoke entire token family
        SessionRepo->>DB: UPDATE sessions SET revoked_at
        RefreshUC->>Audit: Record refresh token reuse
        Audit->>DB: INSERT high-severity event
        UoW->>DB: COMMIT
        HTTP-->>Browser: 401; clear cookie
    else expired/revoked/inactive
        RefreshUC->>Audit: Record denied refresh
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Browser: 401; clear cookie
    end
```

Два параллельных refresh request одного поколения не могут оба завершиться успешно.

## 5. Блокировка пользователя и отзыв сессий

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant BlockUC
    participant UoW
    participant AdminPolicy
    participant UserRepo
    participant SessionRepo
    participant Audit
    participant DB

    Admin->>HTTP: POST /users/{id}/block
    HTTP->>BlockUC: Execute(actor, target, reason)
    BlockUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    BlockUC->>AdminPolicy: Revalidate admin session and role
    AdminPolicy->>DB: SELECT actor/session FOR SHARE
    BlockUC->>UserRepo: Lock target user
    UserRepo->>DB: SELECT ... FOR UPDATE
    BlockUC->>BlockUC: Reject self-lockout if policy forbids it
    BlockUC->>UserRepo: Block(reason)
    UserRepo->>DB: UPDATE user status/version
    BlockUC->>SessionRepo: Revoke all active target sessions
    SessionRepo->>DB: UPDATE sessions
    BlockUC->>Audit: Record user blocked + sessions revoked
    Audit->>DB: INSERT audit event
    UoW->>DB: COMMIT
    BlockUC-->>HTTP: result
    HTTP-->>Admin: 200
```

Изменение статуса и отзыв сессий атомарны. После commit новый защищённый запрос пользователя должен быть отклонён в пределах Security SLA.

## 6. Назначение аптекаря аптеке

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant AssignUC
    participant UoW
    participant Policy
    participant UserRepo
    participant PharmacyRepo
    participant AssignmentRepo
    participant Audit
    participant DB

    Admin->>HTTP: POST /pharmacies/{pharmacy_id}/assignments
    HTTP->>AssignUC: Execute(actor, user_id, pharmacy_id)
    AssignUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    AssignUC->>Policy: Revalidate ADMIN
    Policy->>DB: SELECT current role/session
    AssignUC->>UserRepo: Lock pharmacist user
    UserRepo->>DB: SELECT ... FOR UPDATE
    AssignUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT ... FOR UPDATE
    AssignUC->>AssignmentRepo: Check active duplicate
    AssignmentRepo->>DB: SELECT active assignment
    alt invalid user, role, pharmacy or duplicate
        UoW->>DB: ROLLBACK
        HTTP-->>Admin: 409/422
    else valid
        AssignUC->>AssignmentRepo: Create active assignment
        AssignmentRepo->>DB: INSERT assignment
        AssignUC->>Audit: Record assignment created
        Audit->>DB: INSERT audit event
        UoW->>DB: COMMIT
        HTTP-->>Admin: 201
    end
```

Отзыв назначения использует тот же lock order. Новые pharmacy-scoped mutations после commit запрещены; уже открытая mutation обязана повторно проверить assignment внутри своей транзакции.

## 7. Проведение поступления

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReceiptUC
    participant UoW
    participant Policy
    participant IdemRepo
    participant ReceiptRepo
    participant InventoryRepo
    participant Audit
    participant DB
    participant Events

    Pharmacist->>HTTP: POST /pharmacies/{id}/receipts + Idempotency-Key
    HTTP->>ReceiptUC: Execute(command, actor)
    ReceiptUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    ReceiptUC->>Policy: Revalidate user, session, assignment, pharmacy
    Policy->>DB: SELECT current state
    ReceiptUC->>IdemRepo: Claim(scope, operation, key, fingerprint)
    IdemRepo->>DB: INSERT or SELECT FOR UPDATE
    alt completed same fingerprint
        ReceiptUC->>Policy: Recheck replay visibility
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: Original result + replay flag
    else same key different fingerprint
        ReceiptUC->>Audit: Record idempotency conflict
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 409 IDEMPOTENCY_CONFLICT
    else new command
        ReceiptUC->>ReceiptRepo: Create and post receipt snapshot
        ReceiptRepo->>DB: INSERT receipt + lines
        ReceiptUC->>InventoryRepo: Create lots and immutable movements
        InventoryRepo->>DB: INSERT lots + movements
        ReceiptUC->>Audit: Record receipt posted
        Audit->>DB: INSERT audit event
        ReceiptUC->>IdemRepo: Complete with stable response snapshot
        IdemRepo->>DB: UPDATE idempotency record
        UoW->>DB: COMMIT
        ReceiptUC->>Events: Publish ReceiptPosted after commit
        HTTP-->>Pharmacist: 201
    end
```

Поступление, лоты, движения, idempotency result и transactional audit commit-ятся как единое целое.

## 8. Проведение продажи с FEFO

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant SaleUC
    participant UoW
    participant Policy
    participant IdemRepo
    participant AssortmentRepo
    participant InventoryRepo
    participant SaleRepo
    participant Audit
    participant DB
    participant Events

    Pharmacist->>HTTP: POST /pharmacies/{id}/sales + Idempotency-Key
    HTTP->>SaleUC: Execute(items, actor)
    SaleUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    SaleUC->>Policy: Revalidate actor/session/assignment/pharmacy
    Policy->>DB: SELECT current state
    SaleUC->>IdemRepo: Claim key + semantic fingerprint
    IdemRepo->>DB: INSERT or lock existing record
    SaleUC->>AssortmentRepo: Load current sale rules and prices
    AssortmentRepo->>DB: SELECT pharmacy products
    SaleUC->>InventoryRepo: Lock eligible lots in deterministic FEFO order
    InventoryRepo->>DB: SELECT lots ORDER BY expiration,id FOR UPDATE
    SaleUC->>SaleUC: Calculate base units, FEFO allocations and total
    alt insufficient or ineligible stock
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 409 INSUFFICIENT_STOCK
    else valid
        SaleUC->>SaleRepo: Insert posted sale, lines, snapshots, allocations
        SaleRepo->>DB: INSERT sale graph
        SaleUC->>InventoryRepo: Decrease lots + append SALE movements
        InventoryRepo->>DB: UPDATE lots + INSERT movements
        SaleUC->>Audit: Record sale posted
        Audit->>DB: INSERT audit event
        SaleUC->>IdemRepo: Complete response snapshot
        IdemRepo->>DB: UPDATE idempotency record
        UoW->>DB: COMMIT
        SaleUC->>Events: Publish SalePosted after commit
        HTTP-->>Pharmacist: 201 + server-calculated totals
    end
```

Frontend quantity, price, total и выбранный lot не являются источником истины. FEFO и итоговая сумма вычисляются backend после получения блокировок.

## 9. Конкурентная продажа одного остатка

```mermaid
sequenceDiagram
    autonumber
    participant SaleA
    participant SaleB
    participant DB

    par transaction A
        SaleA->>DB: BEGIN
        SaleA->>DB: SELECT lot FOR UPDATE
        DB-->>SaleA: lock acquired, quantity=10
    and transaction B
        SaleB->>DB: BEGIN
        SaleB->>DB: SELECT same lot FOR UPDATE
        DB-->>SaleB: waits
    end
    SaleA->>DB: UPDATE quantity=0; INSERT movement; COMMIT
    DB-->>SaleB: lock acquired, quantity=0
    SaleB->>SaleB: Recheck stock after lock
    SaleB->>DB: ROLLBACK
```

Проверка остатка до lock не является достаточной. Вторая транзакция обязана перечитать и перепроверить состояние после ожидания.

## 10. Возврат по исходной продаже

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReturnUC
    participant UoW
    participant Policy
    participant IdemRepo
    participant SaleRepo
    participant ReturnRepo
    participant InventoryRepo
    participant Audit
    participant DB

    Pharmacist->>HTTP: POST /sales/{sale_id}/returns
    HTTP->>ReturnUC: Execute(return_lines, disposition, reason)
    ReturnUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    ReturnUC->>Policy: Revalidate assignment and resource pharmacy scope
    Policy->>DB: SELECT current actor/session/assignment
    ReturnUC->>IdemRepo: Claim key
    IdemRepo->>DB: INSERT or lock
    ReturnUC->>SaleRepo: Lock sale, items and original allocations
    SaleRepo->>DB: SELECT ... FOR UPDATE
    ReturnUC->>ReturnRepo: Lock previous return allocations
    ReturnRepo->>DB: SELECT ... FOR UPDATE
    ReturnUC->>ReturnUC: Validate returnable quantity and legal policy
    alt invalid or exceeds unreturned quantity
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 409/422
    else valid and RETURN_TO_STOCK
        ReturnUC->>InventoryRepo: Lock target lots / create approved return lot
        InventoryRepo->>DB: SELECT/INSERT + FOR UPDATE
        ReturnUC->>ReturnRepo: Insert posted return and allocations
        ReturnRepo->>DB: INSERT return graph
        ReturnUC->>InventoryRepo: Increase stock + append return movements
        InventoryRepo->>DB: UPDATE/INSERT movements
        ReturnUC->>Audit: Record return posted
        Audit->>DB: INSERT audit event
        ReturnUC->>IdemRepo: Complete
        IdemRepo->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    else valid and DO_NOT_RETURN_TO_STOCK
        ReturnUC->>ReturnRepo: Insert posted non-restocking return
        ReturnRepo->>DB: INSERT return graph
        ReturnUC->>Audit: Record disposition and reason
        Audit->>DB: INSERT audit event
        ReturnUC->>IdemRepo: Complete
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    end
```

Возврат не изменяет исходную продажу и её аллокации. История расширяется новым проведённым документом и, при необходимости, компенсирующими движениями.

## 11. Списание или корректировка

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant AdjustmentUC
    participant UoW
    participant Policy
    participant InventoryRepo
    participant Audit
    participant DB

    Pharmacist->>HTTP: POST /inventory-adjustments + reason
    HTTP->>AdjustmentUC: Execute(command)
    AdjustmentUC->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    AdjustmentUC->>Policy: Revalidate actor and elevated permission if required
    Policy->>DB: SELECT current scope
    AdjustmentUC->>InventoryRepo: Lock affected lots in deterministic order
    InventoryRepo->>DB: SELECT ... FOR UPDATE
    AdjustmentUC->>AdjustmentUC: Validate reason, bounds and resulting quantity
    AdjustmentUC->>InventoryRepo: Insert posted document + immutable movements
    InventoryRepo->>DB: INSERT document/movements; UPDATE lot balances
    AdjustmentUC->>Audit: Record before/after delta and reason
    Audit->>DB: INSERT audit event
    UoW->>DB: COMMIT
    HTTP-->>Pharmacist: 201
```

Общий `PATCH stock_quantity` запрещён. Любая коррекция выражается отдельной предметной командой с reason и audit.

## 12. Импорт каталога через staging и модерацию

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant ImportUC
    participant Storage
    participant DB
    participant Worker
    participant ModerationUC
    participant Audit

    Admin->>HTTP: Upload catalog file
    HTTP->>HTTP: Check type, size, filename, authorization
    HTTP->>Storage: Store quarantine object with generated name
    HTTP->>ImportUC: Create ImportJob(metadata, hash)
    ImportUC->>DB: BEGIN; INSERT job; INSERT audit; COMMIT
    HTTP-->>Admin: 202 + job_id
    Worker->>Storage: Read quarantined file as data only
    Worker->>Worker: Parse with row/column/size limits
    Worker->>DB: Write normalized staging rows and validation findings
    Worker->>DB: Mark job READY_FOR_REVIEW or FAILED
    Admin->>ModerationUC: Approve selected staging records
    ModerationUC->>DB: BEGIN
    ModerationUC->>DB: Revalidate ADMIN and lock staging rows
    ModerationUC->>DB: Create/update catalog through explicit domain commands
    ModerationUC->>Audit: Record moderation and publication
    Audit->>DB: INSERT audit event
    ModerationUC->>DB: COMMIT
```

Импорт не публикует данные напрямую. Ошибка одной строки не должна незаметно создавать частично опубликованный каталог.

## 13. Публичный поиск лекарства

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant SearchUC
    participant SearchRepo
    participant DB

    Browser->>HTTP: GET /api/v1/public/products/search?q=...
    HTTP->>HTTP: Validate query, limit and rate limit
    HTTP->>SearchUC: Search(normalized_filters, optional_location)
    SearchUC->>SearchRepo: Query public projection only
    SearchRepo->>DB: SELECT published catalog + active pharmacy availability
    DB-->>SearchRepo: public rows
    SearchUC->>SearchUC: Apply safe sorting and availability freshness rules
    SearchUC-->>HTTP: public DTOs only
    HTTP-->>Browser: 200 + cache policy
```

Публичная проекция не содержит закупочных цен, точных внутренних остатков, lot IDs, audit или внутренних документов.

## 14. Идемпотентный replay после успешного commit

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant HTTP
    participant UseCase
    participant UoW
    participant Policy
    participant IdemRepo
    participant DB

    Client->>HTTP: Repeat same command and key
    HTTP->>UseCase: Execute(command, actor)
    UseCase->>UoW: WithinTransaction()
    UoW->>DB: BEGIN
    UseCase->>Policy: Revalidate current access
    Policy->>DB: SELECT current user/session/assignment
    UseCase->>IdemRepo: Load completed record FOR SHARE
    IdemRepo->>DB: SELECT result + fingerprint
    alt access revoked
        UoW->>DB: ROLLBACK
        HTTP-->>Client: 401/403
    else fingerprint differs
        UoW->>DB: COMMIT optional security audit
        HTTP-->>Client: 409 IDEMPOTENCY_CONFLICT
    else same fingerprint and access valid
        UoW->>DB: COMMIT
        HTTP-->>Client: Original response + replay flag
    end
```

Существование успешного idempotency record не даёт права получить результат после блокировки пользователя или отзыва назначения.

## 15. Serialization failure, deadlock и безопасный retry

```mermaid
sequenceDiagram
    autonumber
    participant HTTP
    participant UseCase
    participant UoW
    participant DB

    HTTP->>UseCase: Execute command
    loop bounded retry policy
        UseCase->>UoW: Run transaction attempt
        UoW->>DB: BEGIN
        UseCase->>DB: Re-run reads, policy checks, locks and writes
        alt commit succeeds
            UoW->>DB: COMMIT
            UseCase-->>HTTP: success
        else serialization/deadlock error
            UoW->>DB: ROLLBACK
            UoW->>UoW: Backoff with jitter if retryable
        else non-retryable error
            UoW->>DB: ROLLBACK
            UseCase-->>HTTP: mapped error
        end
    end
```

Retry повторяет всю транзакционную функцию, а не отдельный SQL statement. Внешние side effects выполняются только после окончательного commit. Idempotency key остаётся тем же.

## 16. Transactional audit failure

```mermaid
sequenceDiagram
    autonumber
    participant UseCase
    participant BusinessRepo
    participant Audit
    participant DB

    UseCase->>DB: BEGIN
    UseCase->>BusinessRepo: Apply business changes
    BusinessRepo->>DB: INSERT/UPDATE
    UseCase->>Audit: Insert required audit event
    Audit->>DB: INSERT audit event
    alt audit succeeds
        UseCase->>DB: COMMIT
    else audit fails
        UseCase->>DB: ROLLBACK
    end
```

Операция, для которой audit обязателен, не может завершиться успешно при ошибке audit insert.

## 17. Worker и post-commit обработка

```mermaid
sequenceDiagram
    autonumber
    participant UseCase
    participant DB
    participant Outbox
    participant Worker
    participant Projection

    UseCase->>DB: BEGIN
    UseCase->>DB: Apply business changes
    UseCase->>Outbox: Insert event in same transaction
    Outbox->>DB: INSERT outbox record
    UseCase->>DB: COMMIT
    Worker->>DB: Claim outbox rows with SKIP LOCKED
    Worker->>Projection: Update search/alerts projection idempotently
    Worker->>DB: Mark event processed
```

Если применяется transactional outbox, бизнес-изменение и запись события атомарны. Worker допускает at-least-once delivery и обязан быть идемпотентным.

## 18. Общий lock order

До появления более детального ADR применяется следующий принцип:

1. actor user/session/assignment, если требуется stale-sensitive revalidation;
2. pharmacy;
3. command document или основной aggregate root;
4. связанные исходные документы;
5. stock lots в стабильном порядке `pharmacy_id`, `product_presentation_id`, `expiration_date`, `lot_id`;
6. idempotency record в порядке, не создающем цикл с выбранным use case;
7. audit/outbox inserts без обратного захвата ранее освобождённых ресурсов.

Каждый use case обязан документировать отклонение от общего порядка. Lock order должен быть одинаковым во всех сценариях, которые могут затронуть одни и те же строки.

## 19. Негативные последовательности, обязательные для тестов

Минимально проверяются:

- токен валиден криптографически, но user заблокирован;
- роль или assignment отозваны между начальной проверкой и mutation;
- resource принадлежит другой аптеке;
- refresh token одновременно используется дважды;
- idempotency replay после отзыва доступа;
- одинаковый key с другим payload;
- две продажи конкурируют за один lot;
- возврат конкурирует с другим возвратом той же sale allocation;
- audit insert завершается ошибкой;
- commit завершается serialization failure;
- frontend получает поздний успешный ответ после logout;
- worker повторно обрабатывает одно outbox event;
- импорт содержит слишком большой файл, path traversal filename, формулы CSV или malformed rows.

## 20. Definition of Done для критического сценария

Сценарий считается согласованным, если:

1. определены actor, target resource и pharmacy scope;
2. показано, где выполняются authentication и authorization;
3. stale-sensitive checks находятся внутри транзакции;
4. transaction boundary совпадает с Domain Model и Database Design;
5. lock order детерминирован;
6. idempotency scope и fingerprint определены;
7. commit/rollback явно показаны;
8. обязательный audit входит в transaction boundary;
9. post-commit side effects не выполняются до commit;
10. ошибки используют централизованный mapping;
11. replay, race и rollback paths покрыты тестами;
12. диаграмма не передаёт `gin.Context`, `pgx.Tx` или SQL-модели через application/domain API.

## 21. Открытые вопросы

До production необходимо формально решить и при необходимости оформить ADR:

1. точную реализацию transactional outbox и перечень событий;
2. окончательный порядок блокировок между idempotency record и business aggregates;
3. retry policy для PostgreSQL serialization/deadlock errors;
4. юридические правила возврата лекарств и допустимость `RETURN_TO_STOCK`;
5. SLA обновления публичной search projection;
6. необходимость dual approval для особо опасных административных операций.
