# PharmacyCRM — Sequence Diagrams

**Статус документа:** Draft  
**Версия:** 0.2  
**Дата:** 2026-07-20  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `09-security-design.md`

## 1. Назначение и нормативная роль

Документ фиксирует исполнимые последовательности критических сценариев PharmacyCRM: участников, порядок проверок, границы транзакций, блокировки, повторную авторизацию, идемпотентность, audit, commit/rollback, retry и post-commit обработку.

Диаграммы не заменяют API Design, Domain Model или Database Design. Они связывают эти документы и показывают, где конкретно обеспечиваются инварианты.

Если реализация меняет security-sensitive проверку, transaction boundary, lock order, idempotency protocol, audit semantics, retry policy или момент публикации post-commit события, соответствующая диаграмма обновляется в том же change set.

## 2. Нормативные обозначения

Диаграммы записаны в Mermaid `sequenceDiagram`.

Основные участники:

- `Browser` — недоверенный клиент;
- `HTTP` — Gin delivery, middleware, DTO validation и единый responder;
- `UseCase` — application service;
- `Policy` — application authorization policy;
- `UoW` — Unit of Work и transaction retry boundary;
- `Repo` — repository ports и PostgreSQL adapters;
- `DB` — PostgreSQL;
- `Audit` — transactional audit writer;
- `Outbox` — transactional outbox;
- `Worker` — фоновый обработчик;
- `External` — внешняя система или необратимый side effect.

Обязательные правила:

1. `HTTP` не выполняет бизнес-логику и SQL.
2. `UseCase` координирует authorization, idempotency, UoW, lock order и domain operations.
3. Дорогие операции без необходимости держать locks — password hashing, парсинг файла, сетевые вызовы — не выполняются внутри длительной DB transaction.
4. Все stale-sensitive полномочия повторно проверяются внутри mutation transaction.
5. Audit, обязательный для признания операции успешной, записывается до commit в той же транзакции.
6. Надёжные post-commit действия инициируются через transactional outbox, если их потеря недопустима.
7. Ошибка до commit приводит к rollback; внешний успешный ответ возвращается только после успешного commit.
8. Retry повторяет транзакционную функцию целиком и не повторяет уже выполненный внешний side effect.
9. Repository не открывает скрытую transaction внутри транзакционного use case.
10. Ветви диаграммы, завершающиеся ошибкой, обязаны явно показывать rollback или безопасный commit security-event-only transaction.

## 3. Общий шаблон mutation

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant HTTP
    participant UC as UseCase
    participant UoW
    participant Policy
    participant Idem
    participant DomainRepo
    participant Audit
    participant Outbox
    participant DB

    Client->>HTTP: Protected mutation + Idempotency-Key
    HTTP->>HTTP: Validate auth context, DTO, size, headers
    HTTP->>UC: Execute(command, actor_context)
    UC->>UoW: Run bounded transaction
    UoW->>DB: BEGIN
    UC->>Policy: Revalidate actor/session/role/scope
    Policy->>DB: Read current security state
    UC->>Idem: Claim deterministic idempotency scope
    Idem->>DB: INSERT or lock existing record
    alt completed replay
        UC->>Policy: Revalidate result visibility
        UoW->>DB: COMMIT
        HTTP-->>Client: Original response + replay flag
    else fingerprint conflict
        UC->>Audit: Record conflict
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Client: 409 IDEMPOTENCY_CONFLICT
    else new command
        UC->>DomainRepo: Lock resources in canonical order
        DomainRepo->>DB: SELECT ... FOR UPDATE
        UC->>UC: Apply domain rules
        UC->>DomainRepo: Persist business changes
        DomainRepo->>DB: INSERT/UPDATE
        UC->>Audit: Insert required audit
        Audit->>DB: INSERT event
        UC->>Outbox: Insert post-commit events
        Outbox->>DB: INSERT outbox rows
        UC->>Idem: Complete stable response snapshot
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Client: Success
    end
```

Этот шаблон является базовым. Отклонение должно быть объяснено рядом с конкретной диаграммой.

## 4. Вход пользователя и создание сессии

Password verification выполняется до mutation transaction. Для отсутствующего пользователя используется заранее подготовленный dummy hash, чтобы внешний timing не раскрывал существование login.

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant LoginUC
    participant UserRead
    participant Hasher
    participant UoW
    participant UserRepo
    participant SessionRepo
    participant Audit
    participant DB

    Browser->>HTTP: POST /api/v1/auth/login
    HTTP->>HTTP: Validate JSON, size and rate limits
    HTTP->>LoginUC: Authenticate(normalized_login, password, request_context)
    LoginUC->>UserRead: Load authentication snapshot
    UserRead->>DB: SELECT user id, status, hash, auth version
    DB-->>UserRead: User snapshot or not found
    LoginUC->>Hasher: Verify against real or dummy hash
    Hasher-->>LoginUC: valid / invalid
    alt unknown, invalid or inactive
        LoginUC->>UoW: Record denied security event
        UoW->>DB: BEGIN
        LoginUC->>Audit: Insert non-enumerating denied event
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Browser: 401 UNAUTHENTICATED
    else credentials valid
        LoginUC->>UoW: Create session transaction
        UoW->>DB: BEGIN
        LoginUC->>UserRepo: Revalidate and lock auth-relevant user state
        UserRepo->>DB: SELECT status/auth_version FOR UPDATE
        alt user changed or inactive
            LoginUC->>Audit: Record denied due to stale snapshot
            Audit->>DB: INSERT event
            UoW->>DB: COMMIT
            HTTP-->>Browser: 401 UNAUTHENTICATED
        else still valid
            LoginUC->>SessionRepo: Insert session + refresh token hash
            SessionRepo->>DB: INSERT session
            LoginUC->>UserRepo: Record successful login / optional rehash metadata
            UserRepo->>DB: UPDATE auth metadata
            LoginUC->>Audit: Record login success
            Audit->>DB: INSERT event
            UoW->>DB: COMMIT
            LoginUC-->>HTTP: access token + raw refresh token
            HTTP-->>Browser: 200; HttpOnly refresh cookie
        end
    end
```

Инварианты:

- unknown user и invalid password имеют одинаковый внешний ответ;
- raw password и raw refresh token не сохраняются и не логируются;
- session создаётся только после повторной проверки current user state;
- hash upgrade не должен приводить к созданию session при failed audit или failed commit.

## 5. Refresh token rotation и reuse detection

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
    RefreshUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    RefreshUC->>SessionRepo: Lock token family by opaque selector
    SessionRepo->>DB: SELECT family/generation FOR UPDATE
    RefreshUC->>RefreshUC: Verify hash, generation and expiry
    RefreshUC->>UserRepo: Load current user and role state
    UserRepo->>DB: SELECT current status/auth version
    alt current generation and active user
        RefreshUC->>SessionRepo: Consume current generation and insert next
        SessionRepo->>DB: UPDATE + INSERT
        RefreshUC->>Audit: Record successful rotation
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        RefreshUC-->>HTTP: New access + refresh token
        HTTP-->>Browser: 200; atomically replace cookie
    else previous generation reused
        RefreshUC->>SessionRepo: Revoke complete family
        SessionRepo->>DB: UPDATE family revoked_at
        RefreshUC->>Audit: Record high-severity reuse
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Browser: 401; clear cookie
    else expired, revoked, invalid or inactive
        RefreshUC->>Audit: Record denied refresh without secret material
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Browser: 401; clear cookie
    end
```

Два конкурентных refresh request одной generation не могут оба завершиться успешно. Commit должен произойти до выдачи нового cookie.

## 6. Logout текущей сессии

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant LogoutUC
    participant UoW
    participant SessionRepo
    participant Audit
    participant DB

    Browser->>HTTP: POST /api/v1/auth/logout
    HTTP->>LogoutUC: Execute(session selector, actor context)
    LogoutUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    LogoutUC->>SessionRepo: Lock matching session
    SessionRepo->>DB: SELECT FOR UPDATE
    alt active session
        LogoutUC->>SessionRepo: Revoke session
        SessionRepo->>DB: UPDATE revoked_at/reason
        LogoutUC->>Audit: Record logout
        Audit->>DB: INSERT event
    else missing or already revoked
        LogoutUC->>Audit: Optionally record idempotent logout
        Audit->>DB: INSERT event if policy requires
    end
    UoW->>DB: COMMIT
    HTTP-->>Browser: 204; expire cookie
```

Logout является идемпотентным и не раскрывает существование другой session.

## 7. Блокировка пользователя и отзыв всех сессий

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant BlockUC
    participant UoW
    participant Policy
    participant UserRepo
    participant SessionRepo
    participant Audit
    participant Outbox
    participant DB

    Admin->>HTTP: POST /api/v1/users/{user_id}/block
    HTTP->>BlockUC: Execute(actor, target, reason)
    BlockUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    BlockUC->>Policy: Revalidate ADMIN session and recent-auth requirement
    Policy->>DB: Read current actor/session/role
    BlockUC->>UserRepo: Lock target user
    UserRepo->>DB: SELECT FOR UPDATE
    BlockUC->>BlockUC: Enforce self-lockout and last-admin policy
    BlockUC->>UserRepo: Apply Block(reason)
    UserRepo->>DB: UPDATE status/auth_version
    BlockUC->>SessionRepo: Revoke all active target sessions
    SessionRepo->>DB: UPDATE sessions
    BlockUC->>Audit: Record user blocked and session count
    Audit->>DB: INSERT event
    BlockUC->>Outbox: Insert access-revoked event
    Outbox->>DB: INSERT event
    UoW->>DB: COMMIT
    HTTP-->>Admin: 200
```

Status, auth version, session revocation and audit are atomic. Existing access tokens перестают давать право на stale-sensitive operation после server-side revalidation.

## 8. Назначение аптекаря аптеке

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
    participant Outbox
    participant DB

    Admin->>HTTP: POST /api/v1/pharmacies/{pharmacy_id}/assignments
    HTTP->>AssignUC: Execute(actor, user_id, pharmacy_id)
    AssignUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    AssignUC->>Policy: Revalidate ADMIN
    Policy->>DB: Read current actor/session/role
    AssignUC->>UserRepo: Lock user
    UserRepo->>DB: SELECT FOR UPDATE
    AssignUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    AssignUC->>AssignmentRepo: Lock assignment key / check active duplicate
    AssignmentRepo->>DB: SELECT FOR UPDATE or guarded INSERT
    alt invalid role/status/pharmacy or duplicate
        UoW->>DB: ROLLBACK
        HTTP-->>Admin: 409/422
    else valid
        AssignUC->>AssignmentRepo: Create active assignment
        AssignmentRepo->>DB: INSERT assignment
        AssignUC->>Audit: Record assignment
        Audit->>DB: INSERT event
        AssignUC->>Outbox: Insert assignment-changed event
        Outbox->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Admin: 201
    end
```

Unique constraint остаётся последней линией защиты от concurrent duplicate assignment.

## 9. Отзыв назначения аптекаря

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant RevokeUC
    participant UoW
    participant Policy
    participant AssignmentRepo
    participant SessionRepo
    participant Audit
    participant Outbox
    participant DB

    Admin->>HTTP: POST /api/v1/pharmacy-assignments/{id}/revoke
    HTTP->>RevokeUC: Execute(actor, assignment_id, reason)
    RevokeUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    RevokeUC->>Policy: Revalidate ADMIN
    Policy->>DB: Read current admin state
    RevokeUC->>AssignmentRepo: Lock active assignment
    AssignmentRepo->>DB: SELECT FOR UPDATE
    RevokeUC->>AssignmentRepo: Revoke(reason)
    AssignmentRepo->>DB: UPDATE status/version
    RevokeUC->>SessionRepo: Bump authorization version or revoke scoped sessions
    SessionRepo->>DB: UPDATE security state
    RevokeUC->>Audit: Record assignment revoked
    Audit->>DB: INSERT event
    RevokeUC->>Outbox: Insert access-scope-changed event
    Outbox->>DB: INSERT event
    UoW->>DB: COMMIT
    HTTP-->>Admin: 200
```

Каждая уже начатая pharmacy-scoped mutation повторно проверяет assignment внутри собственной transaction. Поэтому mutation либо видит старое assignment до его revoke lock, либо ждёт и после commit получает отказ.

## 10. Проведение поступления

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReceiptUC
    participant UoW
    participant Policy
    participant Idem
    participant ReceiptRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{id}/receipts + Idempotency-Key
    HTTP->>ReceiptUC: Execute(command, actor)
    ReceiptUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReceiptUC->>Policy: Revalidate session, role, assignment, pharmacy
    Policy->>DB: Read current security state
    ReceiptUC->>Idem: Claim command key and fingerprint
    Idem->>DB: INSERT or lock record
    alt completed same fingerprint
        ReceiptUC->>Policy: Recheck replay visibility
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: Original result + replay flag
    else same key different fingerprint
        ReceiptUC->>Audit: Record idempotency conflict
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 409
    else new command
        ReceiptUC->>ReceiptRepo: Validate document number uniqueness and create posted receipt
        ReceiptRepo->>DB: INSERT receipt and lines
        ReceiptUC->>InventoryRepo: Create lots and append RECEIPT movements
        InventoryRepo->>DB: INSERT lots and movements
        ReceiptUC->>Audit: Record receipt posted
        Audit->>DB: INSERT event
        ReceiptUC->>Outbox: Insert ReceiptPosted
        Outbox->>DB: INSERT event
        ReceiptUC->>Idem: Complete stable response
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    end
```

Поступление, лоты, movements, audit, outbox и idempotency result commit-ятся атомарно.

## 11. Проведение продажи с FEFO

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant SaleUC
    participant UoW
    participant Policy
    participant Idem
    participant AssortmentRepo
    participant InventoryRepo
    participant SaleRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{id}/sales + Idempotency-Key
    HTTP->>SaleUC: Execute(items, actor)
    SaleUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    SaleUC->>Policy: Revalidate actor/session/assignment/pharmacy
    Policy->>DB: Read current state
    SaleUC->>Idem: Claim key and semantic fingerprint
    Idem->>DB: INSERT or lock record
    SaleUC->>AssortmentRepo: Load current enabled sale rules and prices
    AssortmentRepo->>DB: SELECT current pharmacy products
    SaleUC->>InventoryRepo: Lock eligible lots in canonical FEFO order
    InventoryRepo->>DB: SELECT ORDER BY expiration_date, lot_id FOR UPDATE
    SaleUC->>SaleUC: Recalculate units, allocations, price snapshots and total
    alt insufficient or ineligible stock
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 409 INSUFFICIENT_STOCK
    else valid
        SaleUC->>SaleRepo: Insert posted sale, lines and allocations
        SaleRepo->>DB: INSERT sale graph
        SaleUC->>InventoryRepo: Decrease lots and append SALE movements
        InventoryRepo->>DB: UPDATE balances + INSERT movements
        SaleUC->>Audit: Record sale posted
        Audit->>DB: INSERT event
        SaleUC->>Outbox: Insert SalePosted
        Outbox->>DB: INSERT event
        SaleUC->>Idem: Complete response snapshot
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201 + server-calculated totals
    end
```

Client price, total, lot selection и FEFO order не являются источником истины.

## 12. Конкурентная продажа одного остатка

```mermaid
sequenceDiagram
    autonumber
    participant SaleA
    participant SaleB
    participant DB

    par Transaction A
        SaleA->>DB: BEGIN
        SaleA->>DB: Lock lot in canonical order
        DB-->>SaleA: quantity=10, lock acquired
    and Transaction B
        SaleB->>DB: BEGIN
        SaleB->>DB: Lock same lot
        DB-->>SaleB: wait
    end
    SaleA->>DB: UPDATE quantity=0; INSERT movement; COMMIT
    DB-->>SaleB: lock acquired with quantity=0
    SaleB->>SaleB: Recalculate after lock
    SaleB->>DB: ROLLBACK
```

Pre-lock stock check не позволяет списывать товар. Решение принимается только после получения lock и повторного чтения.

## 13. Возврат по исходной продаже

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReturnUC
    participant UoW
    participant Policy
    participant Idem
    participant SaleRepo
    participant ReturnRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/sales/{sale_id}/returns + Idempotency-Key
    HTTP->>ReturnUC: Execute(lines, disposition, reason)
    ReturnUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReturnUC->>Policy: Revalidate actor and derive pharmacy from sale
    Policy->>DB: Read current security state
    ReturnUC->>Idem: Claim command
    Idem->>DB: INSERT or lock
    ReturnUC->>SaleRepo: Lock sale, lines and original allocations
    SaleRepo->>DB: SELECT FOR UPDATE
    ReturnUC->>ReturnRepo: Lock prior return allocations
    ReturnRepo->>DB: SELECT FOR UPDATE
    ReturnUC->>ReturnUC: Validate legal policy and unreturned quantity
    alt invalid or excessive quantity
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 409/422
    else RETURN_TO_STOCK allowed
        ReturnUC->>InventoryRepo: Lock original/approved target lots
        InventoryRepo->>DB: SELECT/INSERT in canonical lot order
        ReturnUC->>ReturnRepo: Insert posted return and allocation snapshots
        ReturnRepo->>DB: INSERT return graph
        ReturnUC->>InventoryRepo: Increase stock and append RETURN movement
        InventoryRepo->>DB: UPDATE/INSERT
        ReturnUC->>Audit: Record return and disposition
        Audit->>DB: INSERT event
        ReturnUC->>Outbox: Insert ReturnPosted
        Outbox->>DB: INSERT event
        ReturnUC->>Idem: Complete response
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    else DO_NOT_RETURN_TO_STOCK
        ReturnUC->>ReturnRepo: Insert posted non-restocking return
        ReturnRepo->>DB: INSERT return graph
        ReturnUC->>Audit: Record return, disposition and reason
        Audit->>DB: INSERT event
        ReturnUC->>Outbox: Insert ReturnPosted
        Outbox->>DB: INSERT event
        ReturnUC->>Idem: Complete response
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    end
```

Исходная sale и её allocations не переписываются. Возврат создаёт новый исторический документ.

## 14. Списание или корректировка

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant AdjustUC
    participant UoW
    participant Policy
    participant Idem
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{id}/inventory-adjustments
    HTTP->>AdjustUC: Execute(command, reason, actor)
    AdjustUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    AdjustUC->>Policy: Revalidate actor, scope and elevated permission
    Policy->>DB: Read current state
    AdjustUC->>Idem: Claim command
    Idem->>DB: INSERT or lock
    AdjustUC->>InventoryRepo: Lock affected lots in canonical order
    InventoryRepo->>DB: SELECT FOR UPDATE
    AdjustUC->>AdjustUC: Validate reason, bounds and resulting quantity
    AdjustUC->>InventoryRepo: Insert posted document, update balances, append movement
    InventoryRepo->>DB: INSERT/UPDATE
    AdjustUC->>Audit: Record delta, reason and snapshots
    Audit->>DB: INSERT event
    AdjustUC->>Outbox: Insert InventoryAdjusted
    Outbox->>DB: INSERT event
    AdjustUC->>Idem: Complete response
    Idem->>DB: UPDATE record
    UoW->>DB: COMMIT
    HTTP-->>Pharmacist: 201
```

Общий `PATCH stock_quantity` запрещён.

## 15. Сторнирование проведённого документа

```mermaid
sequenceDiagram
    autonumber
    actor AuthorizedUser
    participant HTTP
    participant ReverseUC
    participant UoW
    participant Policy
    participant Idem
    participant DocumentRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    AuthorizedUser->>HTTP: POST /api/v1/{documents}/{id}/reverse
    HTTP->>ReverseUC: Execute(actor, document_id, reason)
    ReverseUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReverseUC->>Policy: Revalidate elevated permission and scope
    Policy->>DB: Read current security state
    ReverseUC->>Idem: Claim reversal command
    Idem->>DB: INSERT or lock
    ReverseUC->>DocumentRepo: Lock original document and existing reversals
    DocumentRepo->>DB: SELECT FOR UPDATE
    ReverseUC->>InventoryRepo: Lock affected lots in canonical order
    InventoryRepo->>DB: SELECT FOR UPDATE
    ReverseUC->>ReverseUC: Validate reversibility and current downstream effects
    alt cannot reverse safely
        UoW->>DB: ROLLBACK
        HTTP-->>AuthorizedUser: 409 BUSINESS_CONFLICT
    else reversible
        ReverseUC->>DocumentRepo: Insert reversal document linked to original
        DocumentRepo->>DB: INSERT reversal
        ReverseUC->>InventoryRepo: Append compensating movements
        InventoryRepo->>DB: INSERT movements + UPDATE balances
        ReverseUC->>Audit: Record reversal and reason
        Audit->>DB: INSERT event
        ReverseUC->>Outbox: Insert DocumentReversed
        Outbox->>DB: INSERT event
        ReverseUC->>Idem: Complete response
        Idem->>DB: UPDATE record
        UoW->>DB: COMMIT
        HTTP-->>AuthorizedUser: 201
    end
```

Сторнирование не изменяет исходный документ и не удаляет исходные movements.

## 16. Импорт каталога через quarantine и staging

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant ImportUC
    participant Storage
    participant UoW
    participant Audit
    participant Outbox
    participant DB
    participant Worker
    participant ModerationUC

    Admin->>HTTP: Upload catalog file
    HTTP->>HTTP: Check authorization, type, size and request limits
    HTTP->>Storage: Stream to quarantine with generated object name
    Storage-->>HTTP: object_id + content hash
    HTTP->>ImportUC: Create ImportJob(metadata, object_id, hash)
    ImportUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ImportUC->>Audit: Record upload
    Audit->>DB: INSERT event
    ImportUC->>Outbox: Insert ImportJobCreated
    Outbox->>DB: INSERT event
    UoW->>DB: COMMIT
    HTTP-->>Admin: 202 + job_id

    Worker->>DB: Claim job with SKIP LOCKED
    Worker->>Storage: Read quarantined object as data only
    Worker->>Worker: Parse under row/column/memory/time limits
    Worker->>DB: BEGIN
    Worker->>DB: Insert normalized staging rows and findings
    Worker->>DB: Mark READY_FOR_REVIEW or FAILED
    Worker->>DB: COMMIT

    Admin->>ModerationUC: Approve selected staging records
    ModerationUC->>DB: BEGIN
    ModerationUC->>DB: Revalidate ADMIN and lock staging rows
    ModerationUC->>DB: Apply explicit catalog domain commands
    ModerationUC->>Audit: Record moderation/publication
    Audit->>DB: INSERT event
    ModerationUC->>DB: COMMIT
```

Файл не исполняется, исходное имя не используется как storage path, а staging не публикуется автоматически.

## 17. Публичный поиск лекарства

```mermaid
sequenceDiagram
    autonumber
    actor Browser
    participant HTTP
    participant SearchUC
    participant SearchRepo
    participant DB

    Browser->>HTTP: GET /api/v1/public/products/search?q=...
    HTTP->>HTTP: Validate query, cursor, limit and rate limit
    HTTP->>SearchUC: Search(normalized_filters, optional_location)
    SearchUC->>SearchRepo: Query public projection only
    SearchRepo->>DB: SELECT published catalog and active availability projection
    DB-->>SearchRepo: Public rows
    SearchUC->>SearchUC: Apply freshness and safe sorting rules
    SearchUC-->>HTTP: Public DTOs only
    HTTP-->>Browser: 200 + cache policy
```

Публичная проекция не содержит exact internal quantity, lot IDs, purchase prices, audit или internal document IDs.

## 18. Идемпотентный replay после успешного commit

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant HTTP
    participant UC
    participant UoW
    participant Policy
    participant Idem
    participant Audit
    participant DB

    Client->>HTTP: Repeat same command and key
    HTTP->>UC: Execute(command, actor)
    UC->>UoW: Run transaction
    UoW->>DB: BEGIN
    UC->>Policy: Revalidate current access and result visibility
    Policy->>DB: Read user/session/assignment/resource scope
    UC->>Idem: Lock completed record and compare fingerprint
    Idem->>DB: SELECT FOR SHARE/UPDATE
    alt access revoked
        UoW->>DB: ROLLBACK
        HTTP-->>Client: 401/403/404
    else fingerprint differs
        UC->>Audit: Record conflict
        Audit->>DB: INSERT event
        UoW->>DB: COMMIT
        HTTP-->>Client: 409 IDEMPOTENCY_CONFLICT
    else same fingerprint and access valid
        UoW->>DB: COMMIT
        HTTP-->>Client: Original response + replay flag
    end
```

Replay не обходит текущую authorization policy и не раскрывает ресурс, ставший недоступным.

## 19. Serialization failure, deadlock и безопасный retry

```mermaid
sequenceDiagram
    autonumber
    participant HTTP
    participant UC
    participant UoW
    participant DB

    HTTP->>UC: Execute command
    loop bounded retry attempts
        UC->>UoW: Execute complete transactional closure
        UoW->>DB: BEGIN
        UC->>DB: Re-run policy checks, idempotency claim, locks and writes
        alt commit succeeds
            UoW->>DB: COMMIT
            UC-->>HTTP: Success
        else serialization/deadlock
            UoW->>DB: ROLLBACK
            UoW->>UoW: Backoff with jitter
        else non-retryable error
            UoW->>DB: ROLLBACK
            UC-->>HTTP: Mapped error
        end
    end
```

Retry budget, retryable PostgreSQL codes и backoff policy должны быть централизованы. Domain objects и response state пересоздаются для каждой attempt.

## 20. Transactional audit failure

```mermaid
sequenceDiagram
    autonumber
    participant UC
    participant BusinessRepo
    participant Audit
    participant DB

    UC->>DB: BEGIN
    UC->>BusinessRepo: Apply business changes
    BusinessRepo->>DB: INSERT/UPDATE
    UC->>Audit: Insert required audit event
    Audit->>DB: INSERT event
    alt audit succeeds
        UC->>DB: COMMIT
    else audit fails
        UC->>DB: ROLLBACK
    end
```

Required audit failure является failure всей операции. Логирование ошибки audit не заменяет rollback.

## 21. Transactional outbox worker

```mermaid
sequenceDiagram
    autonumber
    participant UC
    participant DB
    participant Worker
    participant Projection
    participant External

    UC->>DB: BEGIN
    UC->>DB: Apply business changes + INSERT outbox event
    UC->>DB: COMMIT

    Worker->>DB: BEGIN
    Worker->>DB: Claim available rows FOR UPDATE SKIP LOCKED
    Worker->>DB: Mark processing lease / increment attempt
    Worker->>DB: COMMIT
    Worker->>Projection: Apply idempotent internal projection update
    opt external side effect exists
        Worker->>External: Send with provider idempotency key
    end
    alt processing succeeds
        Worker->>DB: Mark processed_at
    else retryable failure
        Worker->>DB: Store error, next_attempt_at and release lease
    else terminal failure
        Worker->>DB: Move to dead-letter state and alert
    end
```

Worker использует at-least-once delivery. Обработчик обязан быть идемпотентным; crash после side effect, но до `processed_at`, не должен создавать повторный необратимый эффект.

## 22. Канонический порядок блокировок

До отдельного ADR применяется единый порядок:

1. idempotency record для конкретной команды;
2. actor user и session, если им требуется lock, а не обычное consistent read;
3. role assignment и pharmacy assignment;
4. pharmacy;
5. основной command document или aggregate root;
6. связанные исходные документы по возрастанию typed ID;
7. stock lots по `(pharmacy_id, product_presentation_id, expiration_date, lot_id)`;
8. dependent rows: allocations, returns, adjustment lines;
9. audit и outbox выполняют inserts и не захватывают business locks в обратном направлении.

Правила:

- одна и та же таблица блокируется в одинаковом порядке во всех use cases;
- набор ID сначала нормализуется, удаляет дубликаты и сортируется;
- policy query не должна случайно брать locks, нарушающие порядок;
- отклонение документируется рядом с use case и покрывается deadlock test;
- lock не удерживается во время network I/O, file parsing, password hashing или ожидания пользователя.

## 23. Поведение idempotency record при ошибке

1. Validation до transaction не создаёт idempotency record.
2. Business rejection без side effect обычно rollback-ит новый claim; клиент может исправить payload и использовать новый key.
3. Fingerprint conflict сохраняет security event, но не заменяет исходный result.
4. Serialization/deadlock rollback не оставляет частично completed record.
5. После commit record содержит stable status, response snapshot и resource reference.
6. Неопределённое состояние после network disconnect разрешается повтором того же key.
7. `FAILED` record допускается только для результата, который по контракту должен детерминированно replay-иться; эта политика фиксируется endpoint-specific.

## 24. Негативные и конкурентные последовательности для тестов

Минимально проверяются:

- token криптографически валиден, но user blocked;
- session revoked между middleware и transaction policy check;
- role или assignment отозваны одновременно с mutation;
- target resource принадлежит другой pharmacy;
- два refresh request используют одну generation;
- login user state меняется после password verification, но до session insert;
- logout повторяется для already revoked session;
- concurrent duplicate assignment;
- idempotency replay после отзыва доступа;
- same key с другим semantic payload;
- disconnect после commit и безопасный replay;
- две продажи конкурируют за один lot;
- sale конкурирует с write-off того же lot;
- два returns конкурируют за одну sale allocation;
- reversal конкурирует с другим reversal;
- required audit insert завершается ошибкой;
- commit получает serialization failure;
- retry исчерпывает budget;
- worker падает после side effect, но до `processed_at`;
- worker повторно получает одно outbox event;
- import содержит oversized file, traversal filename, malformed rows, archive bomb или CSV formula;
- frontend получает late success после logout или auth generation change.

## 25. Definition of Done критического сценария

Сценарий считается согласованным, если:

1. определены actor, target resource и effective pharmacy scope;
2. показано, где выполняются authentication и authorization;
3. stale-sensitive checks находятся внутри mutation transaction;
4. transaction boundary совпадает с Domain Model и Database Design;
5. lock order соответствует каноническому порядку;
6. дорогие или внешние операции вынесены за пределы lock-holding transaction;
7. idempotency scope, fingerprint и error-state policy определены;
8. commit/rollback показаны во всех ветвях;
9. required audit входит в transaction boundary;
10. reliable post-commit event записывается через outbox;
11. внешний success возвращается только после commit;
12. replay, race, retry, disconnect и rollback paths покрыты тестами;
13. ошибки проходят через централизованный mapper;
14. диаграмма не передаёт `gin.Context`, `pgx.Tx` или SQL models через application/domain API;
15. sequence diagram, API contract, repository behavior и integration test не противоречат друг другу.

## 26. Открытые вопросы

До production необходимо формально решить и при необходимости оформить ADR:

1. физическую схему transactional outbox, lease и dead-letter policy;
2. окончательный lock-order ADR после появления migrations и реальных query plans;
3. retry budget и PostgreSQL error classification;
4. endpoint-specific policy для сохранения детерминированных failed idempotency results;
5. юридические правила возврата лекарств и допустимость `RETURN_TO_STOCK`;
6. SLA обновления public search projection;
7. необходимость dual approval для опасных ADMIN operations;
8. способ немедленного invalidation access token: auth version lookup, cache или session check;
9. политика scoped session revocation при отзыве одного pharmacy assignment.