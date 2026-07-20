from pathlib import Path
import re

DOCS = Path("docs")


def load(name: str) -> str:
    return (DOCS / name).read_text(encoding="utf-8")


def save(name: str, text: str) -> None:
    (DOCS / name).write_text(text.rstrip() + "\n", encoding="utf-8")


def replace_section(text: str, start: str, nxt: str, replacement: str) -> str:
    pattern = re.compile(
        rf"^{re.escape(start)}\n.*?(?=^{re.escape(nxt)}\n)",
        re.MULTILINE | re.DOTALL,
    )
    if not pattern.search(text):
        raise RuntimeError(f"section not found: {start}")
    return pattern.sub(replacement.rstrip() + "\n\n", text, count=1)


# -----------------------------------------------------------------------------
# 00-01: historical evidence only; it no longer participates in precedence.
# -----------------------------------------------------------------------------
name = "00-01-cross-document-consistency.md"
text = load(name)
text = replace_section(
    text,
    "## 1. Назначение и нормативный приоритет",
    "## 2. Результат review",
    """## 1. Историческое назначение

Документ сохраняет контекст cross-document review, выполненного до инкорпорации: обнаруженные расхождения, выбранные варианты и матрицу переноса в исходные документы.

После получения статуса `Incorporated` он:

- не участвует в нормативном порядке приоритетов;
- не заменяет исходные документы `04–14` и принятые ADR;
- не используется реализацией как самостоятельный источник правил;
- обновляется только для сохранения достоверной истории review.

Актуальные правила находятся в `00-documentation-index.md`, Product Vision, SRS, принятых ADR и соответствующих исходных документах.""",
)
text = replace_section(
    text,
    "### 7.3 version-counter авторизации",
    "## 8. API paths как источник истины",
    """### 7.3 Актуальность authorization state

Отдельное скрытое поле или счётчик инвалидирования доступа не используется. Актуальность полномочий определяется текущими записями:

- `users.status`, `users.version`, `password_changed_at`;
- active role assignment;
- active, non-revoked и non-expired session;
- active pharmacy assignment;
- pharmacy state.

Новый механизм инвалидирования требует ADR, изменения схемы, migrations и синхронизации session/API semantics.""",
)
text = replace_section(
    text,
    "## 10. Return terminology и protocol",
    "## 11. Import states",
    """## 10. Return terminology и protocol

Domain/API enum `ReturnAction` содержит:

- `RESTOCK`;
- `WRITE_OFF`;
- `QUARANTINE`;
- `NO_PHYSICAL_RETURN`.

Складские типы операций возврата относятся к `InventoryOperation.type`, а не к `ReturnAction`.

Для customer-returned medicines Gate E0 запрещает перевод в sellable stock: production flow использует `QUARANTINE`, `WRITE_OFF` или `NO_PHYSICAL_RETURN`. `RESTOCK` допустим только для отдельного юридически и операционно утверждённого non-customer flow.""",
)
text = re.sub(
    r"^## 20\. Правило сопровождения\n.*\Z",
    """## 20. Историческое сопровождение

Новый конфликт устраняется непосредственно во всех затронутых исходных документах в одном change set и при необходимости оформляется ADR. Этот исторический amendment не расширяется новыми активными правилами.

Если нужен новый amendment, создаётся отдельный пронумерованный документ с собственным статусом и матрицей инкорпорации.""",
    text,
    flags=re.MULTILINE | re.DOTALL,
)
save(name, text)


# -----------------------------------------------------------------------------
# 04: the original module table must equal the incorporated ownership model.
# -----------------------------------------------------------------------------
name = "04-architecture.md"
text = load(name)
text = replace_section(
    text,
    "## 4. Логические модули backend",
    "## 5. Владение данными и межмодульные границы",
    """## 4. Логические модули backend

Нормативная декомпозиция backend:

| Модуль | Ответственность |
|---|---|
| `identity` | пользователи, credentials, роли и sessions |
| `pharmacy` | аптеки, публичные данные и история `pharmacy_assignments` |
| `catalog` | глобальный каталог, presentations, barcodes, requests, import staging и moderation |
| `assortment` | ассортимент аптеки, локальные цены и правила отпуска |
| `inventory` | receipts, initial stock, lots, movements, write-offs и adjustments |
| `sales` | продажи, snapshots, totals и FEFO allocations |
| `returns` | возвраты по исходным sale allocations и refund state |
| `reliability` | idempotency, transactional outbox, retry и lease protocol |
| `audit` | неизменяемые audit events |
| `alerts` | low-stock, expiry и reconciliation alerts |
| `search` | rebuildable public availability projections |
| `replenishment` | вычисляемые рекомендации ручного пополнения |

Отдельные modules `import`, `receipt` и `adjustments` не создаются: соответствующие use cases принадлежат `catalog` или `inventory`.""",
)
save(name, text)


# -----------------------------------------------------------------------------
# 05: canonical UUID example and only genuinely open post-E0 questions.
# -----------------------------------------------------------------------------
name = "05-api-design.md"
text = load(name)
text = text.replace(
    '{"id":"01JZX3E2J9Q4JY3N8VY6F7XH2A"}',
    '{"id":"a9aa71f1-6c24-4c6d-b2db-388ecbbbd2ea"}',
)
text = re.sub(
    r"^## 24\. Открытые решения\n.*?(?=^<!-- consistency-incorporated:start -->)",
    """## 24. Remaining non-E0 decisions

Gate E0 transport, session invalidation, legal-return baseline, retention, package manager и API generation strategy утверждены. Остаётся детализировать:

1. atomic или partial catalog staging publication;
2. elevated approval model для void/reverse/adjustment;
3. ETag/resource-version transport policy;
4. MIME, size, row и complexity limits импортов;
5. public availability cache TTL и freshness budget.

Эти вопросы не разрешают альтернативные auth transport, idempotency identity, URL paths, persisted states или generated-client flow.

""",
    text,
    flags=re.MULTILINE | re.DOTALL,
)
save(name, text)


# -----------------------------------------------------------------------------
# 07: Reliability owns both durable idempotency and durable event delivery.
# -----------------------------------------------------------------------------
name = "07-domain-model.md"
text = load(name)
text = text.replace(
    "| Reliability | идемпотентность команд | `IdempotencyRecord` |",
    "| Reliability | идемпотентность и durable post-commit delivery | `IdempotencyRecord`, `OutboxEvent` |",
)
text = text.replace(
    "`SaleReturnID`, `IdempotencyRecordID`, `AuditEventID`",
    "`SaleReturnID`, `IdempotencyRecordID`, `OutboxEventID`, `AuditEventID`",
)
save(name, text)


# -----------------------------------------------------------------------------
# 09: Gate E0 decisions are approved, not alternative choices awaiting ADR.
# -----------------------------------------------------------------------------
name = "09-security-design.md"
text = load(name)
text = replace_section(
    text,
    "## 27. Обязательные ADR до production",
    "## 28. Открытые вопросы",
    """## 27. Утверждённые security decisions и дальнейшая формализация

Gate E0 утвердил password hashing, access/refresh transport, session invalidation, ADMIN TOTP MFA/recovery, trusted proxy, CORS/CSRF и minimum retention. Альтернативный механизм требует нового ADR и синхронного изменения документов.

До production необходимо выбрать operational implementation для:

1. secret manager и rotation/recovery procedures;
2. backup encryption и restore authorization;
3. security monitoring, incident severity и notification workflow;
4. TLS/network identities между backend и PostgreSQL.

Эти пункты не открывают повторно закрытые Gate E0 semantics.""",
)
text = replace_section(
    text,
    "## 28. Открытые вопросы",
    "## 29. Правило сопровождения",
    """## 28. Открытые вопросы

До production необходимо детализировать:

- jurisdiction-specific extension retention и legal hold поверх minimum baseline;
- точный secret manager;
- TLS backend → PostgreSQL;
- WAF/rate-limit implementation;
- проверку compromised passwords;
- incident notification requirements;
- confidential export policy;
- emergency production access.

Открытые infrastructure details не отменяют утверждённые Argon2id, token/cookie, session revoke, TOTP MFA, proxy/CORS/CSRF и deny-by-default controls.""",
)
save(name, text)


# -----------------------------------------------------------------------------
# 10: replace stale paths, ordering, states, FEFO and event semantics.
# -----------------------------------------------------------------------------
name = "10-sequence-diagrams.md"
text = load(name)
for old, new in {
    "SELECT user id, status, hash, auth version": "SELECT user id, status, password_hash, password_changed_at, version",
    "SELECT status/version-counter авторизации FOR UPDATE": "SELECT status, password_changed_at, version FOR UPDATE",
    "SELECT current status/auth version": "SELECT current user, session and role state",
    "UPDATE status/version-counter авторизации": "UPDATE status, version",
    "Status, auth version, session revocation and audit are atomic.": "User status, session revocation and audit are atomic.",
}.items():
    text = text.replace(old, new)

text = replace_section(
    text,
    "## 3. Общий шаблон mutation",
    "## 4. Вход пользователя и создание сессии",
    """## 3. Общий шаблон mutation

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant HTTP
    participant UC as UseCase
    participant UoW
    participant Idem
    participant Policy
    participant Repo
    participant Audit
    participant Outbox
    participant DB

    Client->>HTTP: Protected mutation + Idempotency-Key
    HTTP->>HTTP: Validate credential format, DTO, limits and headers
    HTTP->>UC: Execute(command, actor_context)
    UC->>UoW: Run bounded transaction
    UoW->>DB: BEGIN
    UC->>Idem: Claim actor + operation + effective scope + key
    Idem->>DB: INSERT or lock idempotency record
    UC->>Policy: Revalidate user, session, role, assignment and pharmacy
    Policy->>DB: Lock/read current authorization state
    alt completed replay and currently visible
        UoW->>DB: COMMIT
        HTTP-->>Client: Original committed response + replay flag
    else fingerprint conflict or access no longer valid
        UoW->>DB: ROLLBACK or commit denied audit-only transaction
        HTTP-->>Client: Stable 4xx error
    else new command
        UC->>Repo: Lock roots in canonical order
        Repo->>DB: pharmacy → root → pharmacy_products → source allocations → FEFO lots
        UC->>UC: Recompute mutable eligibility, quantities, prices and limits
        UC->>Repo: Persist document, allocations, balances and movements
        Repo->>DB: INSERT/UPDATE
        UC->>Audit: Insert required transactional audit
        Audit->>DB: INSERT
        UC->>Outbox: Insert durable committed-fact events
        Outbox->>DB: INSERT
        UC->>Idem: Complete replayable result
        Idem->>DB: UPDATE COMPLETED
        UoW->>DB: COMMIT
        HTTP-->>Client: Success
    end
```

Retryable `40001`/`40P01` повторяет всю transaction function с idempotency claim. Response возвращается только после commit.""",
)

text = replace_section(
    text,
    "## 7. Блокировка пользователя и отзыв всех сессий",
    "## 8. Назначение аптекаря аптеке",
    """## 7. Блокировка пользователя и отзыв всех сессий

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant BlockUC
    participant UoW
    participant Idem
    participant Policy
    participant UserRepo
    participant SessionRepo
    participant Audit
    participant Outbox
    participant DB

    Admin->>HTTP: POST /api/v1/admin/users/{user_id}/block + Idempotency-Key
    HTTP->>BlockUC: Execute(actor, target, reason)
    BlockUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    BlockUC->>Idem: Claim GLOBAL command identity
    Idem->>DB: INSERT or lock
    BlockUC->>Policy: Revalidate ADMIN session and recent authentication
    Policy->>DB: Lock/read current actor, session and role
    BlockUC->>UserRepo: Lock target user
    UserRepo->>DB: SELECT FOR UPDATE
    BlockUC->>BlockUC: Recheck self-lockout and last-admin policy
    BlockUC->>UserRepo: Set BLOCKED and increment user version
    UserRepo->>DB: UPDATE
    BlockUC->>SessionRepo: Revoke all applicable target sessions
    SessionRepo->>DB: UPDATE
    BlockUC->>Audit: Record UserBlocked
    Audit->>DB: INSERT
    BlockUC->>Outbox: Insert UserBlocked and SessionRevoked
    Outbox->>DB: INSERT
    BlockUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Admin: 200
```

User status, session revocations, audit, outbox и idempotency result атомарны.""",
)

text = replace_section(
    text,
    "## 8. Назначение аптекаря аптеке",
    "## 9. Отзыв назначения аптекаря",
    """## 8. Назначение аптекаря аптеке

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant AssignUC
    participant UoW
    participant Idem
    participant Policy
    participant UserRepo
    participant PharmacyRepo
    participant AssignmentRepo
    participant Audit
    participant Outbox
    participant DB

    Admin->>HTTP: POST /api/v1/admin/users/{user_id}/pharmacy-assignments + Idempotency-Key
    HTTP->>AssignUC: Execute(actor, user_id, pharmacy_id)
    AssignUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    AssignUC->>Idem: Claim GLOBAL command identity
    Idem->>DB: INSERT or lock
    AssignUC->>Policy: Revalidate ADMIN session and role
    Policy->>DB: Lock/read current admin state
    AssignUC->>UserRepo: Lock target user
    UserRepo->>DB: SELECT FOR UPDATE
    AssignUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    AssignUC->>AssignmentRepo: Lock/check active assignment key
    AssignmentRepo->>DB: SELECT FOR UPDATE or guarded INSERT
    AssignUC->>AssignUC: Recheck PHARMACIST role, states and uniqueness
    AssignUC->>AssignmentRepo: Insert active assignment
    AssignmentRepo->>DB: INSERT
    AssignUC->>Audit: Record PharmacistAssigned
    Audit->>DB: INSERT
    AssignUC->>Outbox: Insert PharmacistAssigned
    Outbox->>DB: INSERT
    AssignUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Admin: 201
```

`pharmacy_assignments` принадлежат Pharmacy module; unique constraint защищает от concurrent duplicate assignment.""",
)

text = replace_section(
    text,
    "## 9. Отзыв назначения аптекаря",
    "## 10. Проведение поступления",
    """## 9. Завершение назначения аптекаря

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant EndUC
    participant UoW
    participant Idem
    participant Policy
    participant UserRepo
    participant PharmacyRepo
    participant AssignmentRepo
    participant SessionRepo
    participant Audit
    participant Outbox
    participant DB

    Admin->>HTTP: DELETE /api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id} + Idempotency-Key
    HTTP->>EndUC: Execute(actor, user_id, assignment_id, reason)
    EndUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    EndUC->>Idem: Claim GLOBAL command identity
    Idem->>DB: INSERT or lock
    EndUC->>Policy: Revalidate ADMIN session and role
    Policy->>DB: Lock/read current admin state
    EndUC->>UserRepo: Lock target user
    UserRepo->>DB: SELECT FOR UPDATE
    EndUC->>PharmacyRepo: Lock assignment pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    EndUC->>AssignmentRepo: Lock active assignment
    AssignmentRepo->>DB: SELECT FOR UPDATE
    EndUC->>AssignmentRepo: Set ended_at, ended_by_user_id and end_reason
    AssignmentRepo->>DB: UPDATE
    EndUC->>SessionRepo: Revoke sessions whose scope depended on assignment
    SessionRepo->>DB: UPDATE
    EndUC->>Audit: Record PharmacistAssignmentEnded
    Audit->>DB: INSERT
    EndUC->>Outbox: Insert PharmacistAssignmentEnded and applicable SessionRevoked
    Outbox->>DB: INSERT
    EndUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Admin: 204
```

Assignment history не удаляется и не использует отдельный status/version field.""",
)

text = replace_section(
    text,
    "## 10. Проведение поступления",
    "## 11. Проведение продажи с FEFO",
    """## 10. Проведение поступления

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReceiptUC
    participant UoW
    participant Idem
    participant Policy
    participant PharmacyRepo
    participant ReceiptRepo
    participant AssortmentRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{pharmacy_id}/receipts + Idempotency-Key
    HTTP->>ReceiptUC: Execute(command, actor)
    ReceiptUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReceiptUC->>Idem: Claim pharmacy-scoped identity
    Idem->>DB: INSERT or lock
    ReceiptUC->>Policy: Revalidate user, session, role, assignment and pharmacy
    Policy->>DB: Lock/read current authorization state
    ReceiptUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    ReceiptUC->>ReceiptRepo: Lock/check document-number scope
    ReceiptRepo->>DB: SELECT FOR UPDATE
    ReceiptUC->>AssortmentRepo: Lock affected pharmacy_products by id
    AssortmentRepo->>DB: SELECT FOR UPDATE ORDER BY id
    ReceiptUC->>ReceiptUC: Revalidate items and derive lots
    ReceiptUC->>ReceiptRepo: Insert posted receipt and items
    ReceiptRepo->>DB: INSERT
    ReceiptUC->>InventoryRepo: Insert lots, RECEIPT movements and balances
    InventoryRepo->>DB: INSERT/UPDATE
    ReceiptUC->>Audit: Record ReceiptPosted
    Audit->>DB: INSERT
    ReceiptUC->>Outbox: Insert ReceiptPosted
    Outbox->>DB: INSERT
    ReceiptUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Pharmacist: 201
```

Receipt, lots, movements, audit, outbox и idempotency result commit-ятся атомарно.""",
)

text = replace_section(
    text,
    "## 11. Проведение продажи с FEFO",
    "## 12. Конкурентная продажа одного остатка",
    """## 11. Проведение продажи с FEFO

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant SaleUC
    participant UoW
    participant Idem
    participant Policy
    participant PharmacyRepo
    participant AssortmentRepo
    participant SaleRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{pharmacy_id}/sales + Idempotency-Key
    HTTP->>SaleUC: Execute(items, actor)
    SaleUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    SaleUC->>Idem: Claim pharmacy-scoped identity
    Idem->>DB: INSERT or lock
    SaleUC->>Policy: Revalidate user, session, role, assignment and pharmacy
    Policy->>DB: Lock/read current authorization state
    SaleUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    SaleUC->>AssortmentRepo: Lock affected pharmacy_products by id
    AssortmentRepo->>DB: SELECT FOR UPDATE ORDER BY id
    SaleUC->>SaleUC: Recheck assortment, inner-unit policy and server prices
    SaleUC->>InventoryRepo: Lock eligible lots in FEFO order
    InventoryRepo->>DB: SELECT ... ORDER BY expiration_date, received_at, id FOR UPDATE
    SaleUC->>SaleUC: Recompute sellability, quantities, FEFO allocations, snapshots and total
    alt insufficient or ineligible stock
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 409 INSUFFICIENT_STOCK
    else valid
        SaleUC->>SaleRepo: Insert COMPLETED sale, items and allocations
        SaleRepo->>DB: INSERT
        SaleUC->>InventoryRepo: Update lots and append SALE movements
        InventoryRepo->>DB: UPDATE/INSERT
        SaleUC->>Audit: Record SaleCompleted
        Audit->>DB: INSERT
        SaleUC->>Outbox: Insert SaleCompleted
        Outbox->>DB: INSERT
        SaleUC->>Idem: Complete result
        Idem->>DB: UPDATE COMPLETED
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201 + server-calculated totals
    end
```

Frontend price, total, lot selection и FEFO allocation не являются authoritative.""",
)

text = replace_section(
    text,
    "## 13. Возврат по исходной продаже",
    "## 14. Списание или корректировка",
    """## 13. Возврат по исходной продаже

Customer-return endpoint остаётся production-disabled до реализации утверждённой legal/refund policy. После включения он не возвращает customer-returned medicine в sellable stock.

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant ReturnUC
    participant UoW
    participant Idem
    participant Policy
    participant PharmacyRepo
    participant SaleRepo
    participant ReturnRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{pharmacy_id}/returns + Idempotency-Key
    HTTP->>ReturnUC: Execute(sale_id, lines, return_action, reason)
    ReturnUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReturnUC->>Idem: Claim pharmacy-scoped identity
    Idem->>DB: INSERT or lock
    ReturnUC->>Policy: Revalidate user, session, role, assignment and pharmacy
    Policy->>DB: Lock/read current authorization state
    ReturnUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    ReturnUC->>SaleRepo: Lock sale root, items and source allocations by id
    SaleRepo->>DB: SELECT FOR UPDATE
    ReturnUC->>ReturnRepo: Lock prior return allocations by id
    ReturnRepo->>DB: SELECT FOR UPDATE
    ReturnUC->>ReturnUC: Recompute remaining quantity, refund and legal eligibility
    alt RESTOCK requested for customer-returned medicine
        UoW->>DB: ROLLBACK
        HTTP-->>Pharmacist: 422 RETURN_NOT_LEGALLY_ALLOWED
    else permitted QUARANTINE, WRITE_OFF or NO_PHYSICAL_RETURN
        opt physical non-sellable handling required
            ReturnUC->>InventoryRepo: Lock approved target/source lots canonically
            InventoryRepo->>DB: SELECT FOR UPDATE
        end
        ReturnUC->>ReturnRepo: Insert COMPLETED return and source allocations
        ReturnRepo->>DB: INSERT
        ReturnUC->>InventoryRepo: Append applicable non-sellable movement
        InventoryRepo->>DB: INSERT/UPDATE
        ReturnUC->>SaleRepo: Set PARTIALLY_REFUNDED or REFUNDED
        SaleRepo->>DB: UPDATE
        ReturnUC->>Audit: Record SaleReturnCompleted
        Audit->>DB: INSERT
        ReturnUC->>Outbox: Insert SaleReturnCompleted and sale refund event
        Outbox->>DB: INSERT
        ReturnUC->>Idem: Complete result
        Idem->>DB: UPDATE COMPLETED
        UoW->>DB: COMMIT
        HTTP-->>Pharmacist: 201
    end
```

Допустимые `ReturnAction`: `RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`; customer-return policy запрещает ветвь `RESTOCK`.""",
)

text = replace_section(
    text,
    "## 14. Списание или корректировка",
    "## 15. Сторнирование проведённого документа",
    """## 14. Списание или корректировка

```mermaid
sequenceDiagram
    autonumber
    actor Pharmacist
    participant HTTP
    participant AdjustUC
    participant UoW
    participant Idem
    participant Policy
    participant PharmacyRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    Pharmacist->>HTTP: POST /api/v1/pharmacies/{pharmacy_id}/inventory-adjustments + Idempotency-Key
    HTTP->>AdjustUC: Execute(command, reason, actor)
    AdjustUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    AdjustUC->>Idem: Claim pharmacy-scoped identity
    Idem->>DB: INSERT or lock
    AdjustUC->>Policy: Revalidate user, session, assignment, pharmacy and elevated permission
    Policy->>DB: Lock/read current authorization state
    AdjustUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    AdjustUC->>InventoryRepo: Lock pharmacy_products by id, then lots in canonical order
    InventoryRepo->>DB: SELECT FOR UPDATE
    AdjustUC->>AdjustUC: Recheck reason, bounds and resulting quantity
    AdjustUC->>InventoryRepo: Insert adjustment, update balances and append movement
    InventoryRepo->>DB: INSERT/UPDATE
    AdjustUC->>Audit: Record InventoryAdjusted
    Audit->>DB: INSERT
    AdjustUC->>Outbox: Insert InventoryAdjusted
    Outbox->>DB: INSERT
    AdjustUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Pharmacist: 201
```

Общий `PATCH stock_quantity` запрещён.""",
)

text = replace_section(
    text,
    "## 15. Сторнирование проведённого документа",
    "## 16. Импорт каталога через quarantine и staging",
    """## 15. Сторнирование проведённого документа

Concrete HTTP path определяется resource-specific contract в `05-api-design.md`; generic `{documents}` path не является API.

```mermaid
sequenceDiagram
    autonumber
    actor AuthorizedUser
    participant HTTP
    participant ReverseUC
    participant UoW
    participant Idem
    participant Policy
    participant PharmacyRepo
    participant DocumentRepo
    participant InventoryRepo
    participant Audit
    participant Outbox
    participant DB

    AuthorizedUser->>HTTP: Resource-specific reverse command + Idempotency-Key
    HTTP->>ReverseUC: Execute(actor, pharmacy_id, document_id, reason)
    ReverseUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ReverseUC->>Idem: Claim command identity
    Idem->>DB: INSERT or lock
    ReverseUC->>Policy: Revalidate current elevated permission and scope
    Policy->>DB: Lock/read authorization state
    ReverseUC->>PharmacyRepo: Lock pharmacy
    PharmacyRepo->>DB: SELECT FOR UPDATE
    ReverseUC->>DocumentRepo: Lock root and existing compensations
    DocumentRepo->>DB: SELECT FOR UPDATE
    ReverseUC->>InventoryRepo: Lock affected pharmacy_products and lots canonically
    InventoryRepo->>DB: SELECT FOR UPDATE
    ReverseUC->>ReverseUC: Recheck reversibility and downstream effects
    ReverseUC->>DocumentRepo: Insert compensating document
    DocumentRepo->>DB: INSERT
    ReverseUC->>InventoryRepo: Append compensating movements
    InventoryRepo->>DB: INSERT/UPDATE
    ReverseUC->>Audit: Record resource-specific reversal
    Audit->>DB: INSERT
    ReverseUC->>Outbox: Insert InventoryOperationReversed, SaleReversed or SaleReturnReversed
    Outbox->>DB: INSERT
    ReverseUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>AuthorizedUser: 201
```

Сторнирование не изменяет исходный документ и не удаляет исходные movements.""",
)

text = replace_section(
    text,
    "## 16. Импорт каталога через quarantine и staging",
    "## 17. Публичный поиск лекарства",
    """## 16. Импорт каталога через quarantine и staging

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant HTTP
    participant ImportUC
    participant Storage
    participant UoW
    participant Idem
    participant Policy
    participant ImportRepo
    participant Audit
    participant Outbox
    participant DB
    participant Worker

    Admin->>HTTP: POST /api/v1/admin/catalog-imports + Idempotency-Key
    HTTP->>HTTP: Validate credential, file type, size and limits
    HTTP->>Storage: Stream to quarantine with generated object name
    Storage-->>HTTP: object_id + content_hash
    HTTP->>ImportUC: Create job metadata
    ImportUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ImportUC->>Idem: Claim GLOBAL command identity
    Idem->>DB: INSERT or lock
    ImportUC->>Policy: Revalidate ADMIN session and role
    Policy->>DB: Lock/read current authorization state
    ImportUC->>ImportRepo: Insert ImportJob state UPLOADED
    ImportRepo->>DB: INSERT
    ImportUC->>Audit: Record catalog import upload
    Audit->>DB: INSERT
    ImportUC->>Idem: Complete 202 response
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
    HTTP-->>Admin: 202 + job_id

    Worker->>DB: Claim UPLOADED job with SKIP LOCKED
    Worker->>DB: Set VALIDATING
    Worker->>Storage: Read quarantined object as data only
    Worker->>Worker: Parse and validate under bounded limits
    Worker->>DB: BEGIN
    Worker->>DB: Insert staging rows and findings
    alt validation findings exist
        Worker->>DB: Set HAS_ERRORS
    else valid staging
        Worker->>DB: Set READY
    end
    Worker->>DB: COMMIT

    Admin->>HTTP: Confirm selected import rows + Idempotency-Key
    HTTP->>ImportUC: Confirm(job_id, selection)
    ImportUC->>UoW: Run transaction
    UoW->>DB: BEGIN
    ImportUC->>Idem: Claim GLOBAL confirmation identity
    Idem->>DB: INSERT or lock
    ImportUC->>Policy: Revalidate ADMIN session and role
    Policy->>DB: Lock/read current authorization state
    ImportUC->>ImportRepo: Lock READY job and selected rows
    ImportRepo->>DB: SELECT FOR UPDATE; set CONFIRMING
    ImportUC->>ImportRepo: Apply catalog domain commands; set COMPLETED
    ImportRepo->>DB: INSERT/UPDATE
    ImportUC->>Audit: Record CatalogImportCompleted
    Audit->>DB: INSERT
    ImportUC->>Outbox: Insert CatalogImportCompleted
    Outbox->>DB: INSERT
    ImportUC->>Idem: Complete result
    Idem->>DB: UPDATE COMPLETED
    UoW->>DB: COMMIT
```

Persisted states: `UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`. Файл не исполняется и staging не публикуется автоматически.""",
)

text = replace_section(
    text,
    "## 18. Идемпотентный replay после успешного commit",
    "## 19. Serialization failure, deadlock и безопасный retry",
    """## 18. Идемпотентный replay после успешного commit

```mermaid
sequenceDiagram
    autonumber
    actor Client
    participant HTTP
    participant UC
    participant UoW
    participant Idem
    participant Policy
    participant Audit
    participant DB

    Client->>HTTP: Repeat same command and key
    HTTP->>UC: Execute(command, actor)
    UC->>UoW: Run transaction
    UoW->>DB: BEGIN
    UC->>Idem: Lock actor + operation + effective scope + key
    Idem->>DB: SELECT FOR UPDATE
    UC->>Policy: Revalidate current access and result visibility
    Policy->>DB: Lock/read user, session, role, assignment and resource scope
    alt access revoked or result hidden
        UoW->>DB: ROLLBACK
        HTTP-->>Client: 401/403/404
    else fingerprint differs
        UC->>Audit: Record conflict
        Audit->>DB: INSERT
        UoW->>DB: COMMIT
        HTTP-->>Client: 409 IDEMPOTENCY_KEY_REUSED
    else completed fingerprint and visibility valid
        UoW->>DB: COMMIT
        HTTP-->>Client: Original committed response + replay flag
    end
```

Replay не обходит текущую authorization policy и не раскрывает ресурс, ставший недоступным.""",
)
save(name, text)


# -----------------------------------------------------------------------------
# 12: recovery and retention are baselines, not open choices.
# -----------------------------------------------------------------------------
name = "12-deployment.md"
text = load(name)
text = text.replace(
    "До production утверждаются RPO, RTO, frequency, retention, encryption, off-site copy, access roles, integrity verification и drill schedule.",
    "Утверждённый baseline: RPO ≤ 15 минут, RTO ≤ 4 часа, daily base backup, continuous WAL archive, off-site encrypted copies и restore drill минимум ежеквартально. Конкретный backup product обязан доказать этот baseline.",
)
text = re.sub(
    r"^## 34\. Открытые решения\n.*?(?=^<!-- gate-e0-approved:start -->)",
    """## 34. Remaining deployment implementation decisions

Gate E0 topology class, trusted-proxy model, release/migration protocol, outbox fencing, RPO/RTO и retention baseline утверждены. До production требуется выбрать конкретную реализацию:

1. hosting/orchestration platform и ingress product;
2. secret manager;
3. registry, signing и provenance tooling;
4. observability platform;
5. rollout strategy и maintenance-window policy;
6. scaling/connection budget;
7. import object storage;
8. release, DBA и incident ownership;
9. staging anonymization;
10. certificate rotation и network segmentation;
11. compatibility declaration format;
12. initial data cutover owner/sign-off form.

Выбранный продукт не может ослабить утверждённые protocol, recovery и trust guarantees.

""",
    text,
    flags=re.MULTILINE | re.DOTALL,
)
save(name, text)


# -----------------------------------------------------------------------------
# 13: fix the event-catalog assertion and approved restore cadence wording.
# -----------------------------------------------------------------------------
name = "13-testing-strategy.md"
text = load(name)
text = text.replace(
    "8. event catalog не допускает `SaleCompleted`, `SaleReturnCompleted` или generic unregistered reversal event;",
    "8. event catalog не допускает незарегистрированные posting aliases или generic reversal event с неоднозначной семантикой;",
)
text = text.replace(
    "9. restore drill frequency;",
    "9. automation, dataset sizing и evidence format для утверждённого quarterly restore drill;",
)
text = text.replace(
    "18. literal/column version-counter authorization mechanism отсутствует в schema, docs fixtures и migrations.",
    "18. schema, fixtures и migrations не содержат скрытого дополнительного поля для инвалидирования доступа.",
)
save(name, text)


# -----------------------------------------------------------------------------
# 14: retention is approved; storage implementation remains configurable.
# -----------------------------------------------------------------------------
name = "14-observability.md"
text = load(name)
text = text.replace(
    "Отдельная policy утверждается для:",
    "Утверждённый minimum baseline: application logs — 30 days hot + 180 days archive; traces — 7 days; audit/inventory/sales history — минимум 5 лет или дольше по legal hold. Storage implementation дополнительно классифицирует:",
)
text = text.replace(
    "4. retention по каждому signal class;",
    "4. storage tiering, deletion jobs и capacity verification для утверждённой retention policy;",
)
save(name, text)


# -----------------------------------------------------------------------------
# Cross-document validation.
# -----------------------------------------------------------------------------
active = [
    "04-architecture.md",
    "04-01-backend-architecture.md",
    "05-api-design.md",
    "06-database-design.md",
    "07-domain-model.md",
    "08-project-structure.md",
    "09-security-design.md",
    "10-sequence-diagrams.md",
    "11-development-roadmap.md",
    "12-deployment.md",
    "13-testing-strategy.md",
    "14-observability.md",
]
all_text = "\n".join(load(item) for item in active)

for token in [
    "auth_version",
    "auth version",
    "version-counter авторизации",
    "/api/v1/users/{user_id}/block",
    "/api/v1/pharmacies/{pharmacy_id}/assignments",
    "/api/v1/pharmacy-assignments/{id}/revoke",
    "/api/v1/pharmacies/{id}/receipts",
    "/api/v1/pharmacies/{id}/sales",
    "/api/v1/pharmacies/{id}/inventory-adjustments",
    "/api/v1/{documents}/{id}/reverse",
    "ORDER BY expiration_date, lot_id",
    "legacy review-ready transport label",
    "RETURN_TO_STOCK allowed",
    "DO_NOT_RETURN_TO_STOCK",
    "01JZX3E2J9Q4JY3N8VY6F7XH2A",
]:
    if token in all_text:
        raise RuntimeError(f"forbidden stale contract remains: {token}")

for token in [
    "POST /api/v1/admin/users/{user_id}/block",
    "POST /api/v1/admin/users/{user_id}/pharmacy-assignments",
    "DELETE /api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}",
    "POST /api/v1/admin/catalog-imports",
    "ORDER BY expiration_date, received_at, id FOR UPDATE",
    "`IdempotencyRecord`, `OutboxEvent`",
    "`UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`",
    "RPO ≤ 15 минут",
    "Gate E0 закрыт",
]:
    if token not in all_text:
        raise RuntimeError(f"required final contract missing: {token}")

amendment = load("00-01-cross-document-consistency.md")
if (
    "## 1. Историческое назначение" not in amendment
    or "не участвует в нормативном порядке приоритетов" not in amendment
):
    raise RuntimeError("amendment is not historical-only")
