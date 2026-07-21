from pathlib import Path
import re

DOCS=Path('docs')
DATE='2026-07-21'
versions={
'00-documentation-index.md':'2.3','00-01-cross-document-consistency.md':'1.2',
'04-architecture.md':'2.0','04-01-backend-architecture.md':'2.0','05-api-design.md':'2.0','06-database-design.md':'2.1',
'07-domain-model.md':'2.0','08-project-structure.md':'2.0','09-security-design.md':'2.0','10-sequence-diagrams.md':'2.0',
'11-development-roadmap.md':'1.0','12-deployment.md':'1.1','13-testing-strategy.md':'1.1','14-observability.md':'1.1'}

def load(n): return (DOCS/n).read_text(encoding='utf-8')
def save(n,t): (DOCS/n).write_text(t.rstrip()+'\n',encoding='utf-8')
def section(t,start,nxt,repl):
    p=re.compile(rf'^{re.escape(start)}\n.*?(?=^{re.escape(nxt)}\n)',re.M|re.S)
    if not p.search(t): raise RuntimeError(f'missing {start}')
    return p.sub(repl.rstrip()+'\n\n',t,1)

def meta(n,status=None):
    t=load(n)
    if status:
        t=re.sub(r'(\*\*Статус(?: документа)?:\*\*\s*)[^\n]+',lambda m:m.group(1)+status+'  ',t,count=1)
    v=versions.get(n)
    if v:
        t=re.sub(r'(\*\*Версия(?: документа)?:\*\*\s*)[^\n]+',lambda m:m.group(1)+v+'  ',t,count=1)
    t=re.sub(r'(\*\*Дата:\*\*\s*)[^\n]+',lambda m:m.group(1)+DATE+'  ',t,count=1)
    save(n,t)

for n in versions:
    t=load(n)
    t=re.sub(r'\n?<!-- consistency-incorporated:start -->.*?<!-- consistency-incorporated:end -->\n?','\n',t,flags=re.S)
    t=re.sub(r'\n?<!-- gate-e0-approved:start -->.*?<!-- gate-e0-approved:end -->\n?','\n',t,flags=re.S)
    t=re.sub(r'(?i)\bauth[_ -]?version\b','authorization state',t)
    t=t.replace('version-counter авторизации','authorization state')
    t=re.sub(r'\n{3,}','\n\n',t)
    save(n,t)

meta('00-documentation-index.md','Active')
meta('00-01-cross-document-consistency.md','Incorporated')
t=load('00-01-cross-document-consistency.md')
t=t.replace('возвраты не допускаются к production до утверждения юридической policy;','на момент review возвратный production flow ожидал legal policy; после Gate E0 действует консервативный baseline: customer-returned medicines не возвращаются в sellable stock;')
save('00-01-cross-document-consistency.md',t)

n='04-architecture.md'; t=load(n)
t=section(t,'## 9. Идемпотентность','## 10. Модель складских изменений',r'''## 9. Идемпотентность и канонический transaction protocol

Полная identity критической команды:

```text
actor + operation + effective_scope + idempotency_key
```

Для pharmacy command `effective_scope = pharmacy_id`; для global/admin command — `GLOBAL`. Semantic fingerprint включает path/resource IDs, effective scope, применимую resource version и смысловой payload; `request_id`, порядок JSON keys и transport-only metadata исключаются.

Authenticated critical mutation выполняется только в следующем порядке:

1. delivery проверяет credential format, DTO, headers и transport limits;
2. до транзакции выполняются только детерминированные validation/canonicalization;
3. Unit of Work начинает PostgreSQL transaction;
4. первым сериализующим lock берётся idempotency record;
5. внутри transaction повторно читаются current user, session, role, pharmacy assignment и pharmacy state;
6. replay возвращается только после текущей authorization и result-visibility revalidation;
7. business roots блокируются в каноническом порядке;
8. после locks повторно вычисляются eligibility, prices, quantities, FEFO и return limits;
9. атомарно сохраняются business document, snapshots, allocations, lot balances и append-only movements;
10. сохраняются mandatory transactional audit и необходимые `outbox_events`;
11. idempotency record переводится в `COMPLETED` с replayable result/reference;
12. commit предшествует successful HTTP response.

Канонический lock order:

1. idempotency scope/key;
2. current actor/user/session/role для security-sensitive mutation;
3. target user, если он изменяется;
4. pharmacy;
5. root business document (`sale`, `receipt`, `sale_return`, adjustment/reversal root);
6. `pharmacy_products` по `id`;
7. sale items/source allocations по `id`;
8. stock lots по `expiration_date`, затем `received_at`, затем `id`;
9. inserts documents, allocations, movements, audit, outbox и completed idempotency result;
10. commit.

Use case пропускает ненужный уровень, но не меняет взаимный порядок остальных locks. Retryable PostgreSQL error повторяет всю transaction function с idempotency claim; внешний network side effect внутри callback запрещён.''')
t=section(t,'## 15. Внутренние события и post-commit действия','## 16. Фоновые процессы',r'''## 15. Committed events и transactional outbox

Domain event именуется в прошедшем времени и отражает уже committed business fact. Канонический event catalog:

| Context | Events |
|---|---|
| Identity | `UserCreated`, `UserBlocked`, `UserUnblocked`, `UserArchived`, `UserPasswordChanged`, `UserRoleAssigned`, `UserRoleRevoked`, `SessionCreated`, `SessionRotated`, `SessionRevoked` |
| Pharmacy | `PharmacistAssigned`, `PharmacistAssignmentEnded` |
| Catalog | `ProductCreated`, `ProductArchived`, `PresentationCreated`, `BarcodeAssigned`, `CatalogImportCompleted` |
| Assortment | `PharmacyProductActivated`, `PharmacyProductPriceChanged` |
| Inventory | `ReceiptPosted`, `InitialStockConfirmed`, `WriteOffCompleted`, `InventoryAdjusted`, `InventoryOperationReversed` |
| Sales | `SaleCompleted`, `SalePartiallyRefunded`, `SaleRefunded`, `SaleReversed` |
| Returns | `SaleReturnCompleted`, `SaleReturnReversed` |

Technical names используют lower-case dot namespace, например `pharmacy.assignment.ended`, `inventory.receipt.posted`, `sales.sale.completed`, `returns.sale_return.completed`, но не описывают иной факт.

Любая durable post-commit reaction, потеря которой нарушит correctness, freshness проекции, alert/import/security workflow или внешний side effect, создаёт `outbox_events` row в той же transaction, что business effect, audit и completed idempotency result. Delivery — at-least-once; consumer обязан быть idempotent; worker использует lease, guarded completion/fencing, bounded retry и dead-letter. In-process goroutine/channel или прямой publish внутри transaction не являются альтернативой outbox.''')
t=section(t,'## 28. Открытые архитектурные вопросы','## 29. Критерии соответствия реализации',r'''## 28. Remaining implementation decisions

Gate E0 закрыт. До production остаётся выбрать конкретные продукты и численные параметры, не меняющие утверждённые contracts:

1. допустимую задержку public availability projection;
2. способ запуска worker process в deployment topology;
3. search/projection rebuild implementation;
4. exact SLI/SLO и capacity limits;
5. secret manager, observability stack и operator runbooks;
6. elevated approval model для adjustment/reversal.

Эти вопросы не открывают повторно module ownership, transaction order, lock order, outbox, retry, auth/session transport, retention, RPO/RTO, return baseline или frontend tooling.''')
save(n,t); meta(n)

n='04-01-backend-architecture.md'; t=load(n)
t=t.replace('Reliability() IdempotencyTxPort','Reliability() ReliabilityTxPort')
t=section(t,'## 11. Transaction retry','## 12. Gin HTTP delivery',r'''## 11. Critical transaction protocol и retry

Внутри `WithinTransaction` application coordinator соблюдает единый порядок:

1. claim/lock idempotency identity `actor + operation + effective_scope + key`;
2. revalidate current user, session, role, pharmacy assignment и pharmacy;
3. при replay — повторно проверить visibility сохранённого result;
4. взять business locks в каноническом порядке из `04-architecture.md`/`06-database-design.md`;
5. после locks повторно вычислить mutable conditions;
6. записать business effect, mandatory audit, outbox rows и completed idempotency result;
7. commit до возврата success.

Retry допускается только для PostgreSQL SQLSTATE `40001` и `40P01`. Максимум — 3 попытки на request path; повторяется вся transaction function. Backoff — full-jitter exponential, base 25 ms, cap 250 ms, с учётом `context.Context`.

IDs, idempotency key и stable command values создаются до callback. Каждая попытка повторяет authorization и business revalidation. Callback не выполняет HTTP calls, email, broker publish, filesystem side effects или другие необратимые действия. Domain conflict, constraint violation из invalid command и insufficient stock автоматически не повторяются.''')
t=section(t,'## 16. Владение данными','## 17. Migrations',r'''## 16. Владение данными

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
| `search` | rebuildable public projections |
| `replenishment` | computed recommendation read models |

`pharmacy_assignments` принадлежат `pharmacy`, а не `identity`. Catalog import принадлежит `catalog`; receipt, initial-stock confirmation, write-off и adjustment — `inventory`. Отдельные modules `import`, `receipt`, `adjustments` не создаются.

Cross-module transaction не меняет ownership. SQL write остаётся в infrastructure package владельца таблицы. `ReliabilityTxPort` предоставляет idempotency и outbox writer; Audit port остаётся отдельным fail-closed boundary.''')
t=section(t,'## 18. Workers','## 19. Тестирование',r'''## 18. Workers

`backend/cmd/worker` использует те же application contracts, что API, и не вызывает HTTP endpoint собственного backend. Outbox worker:

- claim-ит до 100 due rows через `FOR UPDATE SKIP LOCKED`;
- устанавливает lease 30 секунд, `lease_token`, `lease_generation` и owner;
- выполняет side effect вне claim transaction;
- завершает row только guarded update по token/generation;
- использует at-least-once delivery и idempotent consumers;
- повторяет максимум 8 раз с full-jitter exponential backoff 2 s → cap 15 min;
- после exhaustion переводит event в `DEAD_LETTER`;
- экспортирует backlog, oldest age, retries, stale completion и dead-letter metrics.

Worker не обновляет чужие business tables напрямую и не выполняет best-effort substitute для обязательного outbox.''')
save(n,t); meta(n)

n='05-api-design.md'; t=load(n)
t=section(t,'## 9. Authentication и sessions','## 10. Authorization',r'''## 9. Authentication и sessions

Protected API использует bearer access token:

```http
Authorization: Bearer <access-token>
```

Access token — JWT `EdDSA`/Ed25519 с TTL 10 минут и claims `iss`, `aud`, `sub`, `sid`, `iat`, `nbf`, `exp`, `jti`, `kid`. Browser хранит access token только в памяти.

Refresh token — opaque CSPRNG secret 32 bytes. В базе хранится только hash; browser transport — host-only cookie `__Secure-pharmacy_refresh` с `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`. Absolute TTL — 30 дней, idle TTL — 7 дней; rotation выполняется при каждом refresh. Reuse старого token отзывает всю family.

Block/archive, password change/recovery, role revoke/change, pharmacy assignment end/change и confirmed compromise отзывают применимые sessions. Logout отзывает current session; logout-all — все sessions.

Auth/session responses используют `Cache-Control: no-store`. Credentials передаются только по HTTPS, не логируются и не попадают в error details. Access token не заменяет transaction-time revalidation current user/session/role/assignment/pharmacy. Public search не требует token. Самостоятельная регистрация `ADMIN`/`PHARMACIST` отсутствует.''')
t=section(t,'## 12. Idempotency','## 13. Concurrency, preconditions и retries',r'''## 12. Idempotency

`Idempotency-Key` обязателен для критических commands, отмеченных `required` в каталоге endpoint-ов. Формат — непустая ASCII-строка 1–128 символов; рекомендуемый UUID v4.

Полная identity:

```text
actor + operation + effective_scope + idempotency_key
```

`effective_scope = pharmacy_id` для pharmacy command и `GLOBAL` для global/admin command. `operation` — стабильное логическое имя, а не raw URL. Fingerprint включает path/resource IDs, effective scope, применимую resource version и нормализованный semantic payload; `X-Request-ID`, JSON key order и transport-only metadata исключаются.

В transaction сначала claim/lock idempotency record, затем current authorization/visibility revalidation, затем business locks. Same identity + same fingerprint возвращает исходный committed status/representation с `meta.idempotency_replayed=true`; same identity + другой fingerprint — `409 IDEMPOTENCY_KEY_REUSED`. Replay после block/revoke/assignment end не раскрывает сохранённый result.

Business effect, audit, outbox и completed idempotency result commit-ятся атомарно. Неопределённый network outcome проверяется повтором того же запроса. Idempotency records хранятся минимум 30 дней; business uniqueness юридически значимого документа не зависит от очистки technical record.''')
t=section(t,'## 16. Async jobs и file contracts','## 17. Cache, rate limits и CORS',r'''## 16. Import jobs и file contracts

Upload использует `multipart/form-data` с полем `file`. Ограничиваются MIME, extension, size, row count и parser complexity. Client filename не используется как filesystem path; source file проходит quarantine/scanning.

Persisted `ImportJob` states:

```text
UPLOADED → VALIDATING → READY/HAS_ERRORS/FAILED
READY/HAS_ERRORS → VALIDATING
READY → CONFIRMING → COMPLETED/FAILED
```

Допустимый enum: `UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`. Transport не вводит альтернативный persisted state machine. Status response содержит timestamps, counters/progress, safe error summary и report links.

Upload создаёт job; publish/confirm являются отдельными commands с authorization, idempotency, audit и outbox. Долгий parsing не удерживает business transaction. Downloads задают безопасный `Content-Disposition`; пользовательские значения sanitise-ятся.''')
t=t.replace('Проведение return остаётся `Planned` до юридического утверждения policy.','Customer-return command production-disabled по умолчанию. До передачи товара покупателю используется sale void/reversal. После передачи legally approved exception может вернуть деньги, но физический товар получает `QUARANTINE`, `WRITE_OFF` или `NO_PHYSICAL_RETURN`; `RESTOCK` для customer-returned medicines запрещён.')
save(n,t); meta(n)

n='06-database-design.md'; t=load(n)
t=re.sub(r'\n## Утверждённые Gate E0 database/reliability policies\n\n\| Решение \| Утверждённый baseline \|.*?(?=\n### Outbox worker protocol)', '\n', t, flags=re.S)
t=t.replace('`legacy review-ready transport label`, `QUEUED`, `RUNNING`, `SUCCEEDED` не являются persisted `ImportJob` states.','Любые transport-only job labels не являются persisted `ImportJob` states и не расширяют этот enum.')
save(n,t); meta(n)

n='07-domain-model.md'; t=load(n)
t=section(t,'## 13. Reliability context','## 14. Audit context',r'''## 13. Reliability context

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
7. event payload не является копией HTTP DTO и не содержит secrets.''')
t=section(t,'### 18.5 Eventual consistency и доставка событий','## 19. Доменные события',r'''### 18.5 Eventual consistency и доставка событий

После commit могут обновляться public search, alerts, replenishment, analytics и notifications. Если потеря reaction нарушает correctness, freshness contract, security workflow или внешний side effect, owning transaction обязана записать `OutboxEvent`.

In-process callback допустим только для явно best-effort telemetry, потеря которой не меняет business/operational guarantees. Projections имеют rebuild/reconciliation path; stock, documents, movements, audit и idempotency остаются synchronous authoritative effects.''')
t=section(t,'## 19. Доменные события','## 20. Repository boundaries',r'''## 19. Доменные события

Канонический catalog committed facts:

- Identity: `UserCreated`, `UserBlocked`, `UserUnblocked`, `UserArchived`, `UserPasswordChanged`, `UserRoleAssigned`, `UserRoleRevoked`, `SessionCreated`, `SessionRotated`, `SessionRevoked`;
- Pharmacy: `PharmacistAssigned`, `PharmacistAssignmentEnded`;
- Catalog: `ProductCreated`, `ProductArchived`, `PresentationCreated`, `BarcodeAssigned`, `CatalogImportCompleted`;
- Assortment: `PharmacyProductActivated`, `PharmacyProductPriceChanged`;
- Inventory: `ReceiptPosted`, `InitialStockConfirmed`, `WriteOffCompleted`, `InventoryAdjusted`, `InventoryOperationReversed`;
- Sales: `SaleCompleted`, `SalePartiallyRefunded`, `SaleRefunded`, `SaleReversed`;
- Returns: `SaleReturnCompleted`, `SaleReturnReversed`.

Событие содержит stable name/version, `occurred_at`, aggregate type/ID и минимальный safe payload. Нерегистрируемые posting aliases и generic reversal event names запрещены. Technical dot name может отличаться синтаксисом, но не семантикой.''')
t=section(t,'## 24. Открытые решения','## 25. Definition of Done для domain feature',r'''## 24. Remaining non-E0 domain decisions

1. atomic/partial catalog publish;
2. line discount allocation и partial refund rounding;
3. elevated approval model для reversal/adjustment;
4. alert reopen policy;
5. correction policy catalog snapshots до первой операции;
6. calendar-date interpretation expiration и separate return-lot suitability;
7. permission model сверх трёх ролей MVP при реальной необходимости.

Gate E0 legal baseline закрыт: customer-returned medicines не возвращаются в sellable stock. Open details не разрешают альтернативные event names, states, enums, ownership, outbox или transaction protocol.''')
save(n,t); meta(n)

n='08-project-structure.md'; t=load(n)
needle='''backend/internal/modules/
├── identity/
├── pharmacy/
├── catalog/
├── assortment/
├── inventory/
├── sales/
├── returns/
├── reliability/
├── audit/
├── alerts/
├── search/
└── replenishment/
```'''
replacement=needle+'''\n\nOwnership фиксирован: `pharmacy` владеет `pharmacy_assignments`; `reliability` — `idempotency_records` и `outbox_events`; catalog import находится в `catalog`; receipts, initial stock, write-offs и adjustments — в `inventory`. Отдельные module roots `import/`, `receipt/`, `adjustments/` запрещены.'''
if needle in t: t=t.replace(needle,replacement,1)
t=t.replace('До принятия формата генерации `generated/` может отсутствовать.','Нормативный source contract — OpenAPI 3.1 `backend/api/openapi.yaml`. `pnpm` 10.x через Corepack запускает pinned `openapi-typescript` + `openapi-fetch`; output — `frontend/src/shared/api/generated/`. Generated code не редактируется вручную, а CI выполняет generation и fails on diff.')
t=section(t,'## 23. Открытые решения','## 24. Definition of Done',r'''## 23. Remaining structural implementation decisions

Gate E0 утвердил `pnpm` 10.x/Corepack, committed `pnpm-lock.yaml` и OpenAPI-generated client flow. Остаётся выбрать:

1. frontend runtime-config delivery;
2. ownership cross-application smoke/E2E suite;
3. deployment overlays и reverse-proxy implementation;
4. repository-level contract artifact publication;
5. final naming composition layers при обоснованном изменении frontend architecture.

`npm`/`yarn` lockfiles, handwritten parallel API client и source imports между `backend/` и `frontend/` запрещены.''')
save(n,t); meta(n)

n='09-security-design.md'; t=load(n)
t=section(t,'### 9.1 Хранение','### 9.2 Политика пароля',r'''### 9.1 Хранение и rehash

Новые password hashes используют Argon2id PHC string с параметрами `m=65536 KiB`, `t=3`, `p=2`, salt 16 bytes и hash 32 bytes. Verify поддерживает только явно зарегистрированные current/legacy algorithms.

После успешной проверки backend сравнивает algorithm/parameters с current policy и атомарно rehash-ит более слабый hash. Login response не раскрывает rehash. Password, raw token, recovery code и hash никогда не логируются. Изменение параметров требует benchmark, security review и policy update; оно не применяется неявно.''')
t=section(t,'### 10.3 TTL','### 10.4 Browser storage',r'''### 10.3 TTL и key rotation

Access JWT TTL — 10 минут. Обязательные claims: `iss`, `aud`, `sub`, `sid`, `iat`, `nbf`, `exp`, `jti`, `kid`. Algorithm — `EdDSA`/Ed25519; private keys находятся вне repository. Signing keys rotate каждые 90 дней с verification overlap не менее 20 минут.

Refresh session имеет absolute TTL 30 дней и idle TTL 7 дней. Production `ADMIN` дополнительно требует TOTP MFA; более короткий operational session timeout допускается как hardening, но не ослабляет revoke semantics.''')
t=section(t,'### 10.4 Browser storage','### 10.5 Rotation и reuse detection',r'''### 10.4 Browser transport и storage

Access JWT хранится только в памяти frontend и передаётся через `Authorization: Bearer`. Он не сохраняется в `localStorage`, `sessionStorage`, IndexedDB, URL или JavaScript-readable persistent storage.

Refresh token — opaque 32-byte CSPRNG secret. Database хранит только cryptographic hash. Browser получает host-only cookie `__Secure-pharmacy_refresh` с `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`. Cookie domain не задаётся.''')
t=section(t,'### 10.5 Rotation и reuse detection','### 10.6 Session record',r'''### 10.5 Rotation, reuse и invalidation

Каждый successful refresh создаёт новую session generation/token и отзывает использованную generation в одной transaction. Повторное использование старого token отзывает всю token family, создаёт security audit и усиливает rate limiting.

Block/archive user, password change/recovery, role revoke/change, pharmacy assignment end/change и confirmed compromise отзывают все применимые sessions. Logout отзывает current session; logout-all — все sessions. Critical mutation внутри transaction повторно читает current user/session/role/assignment/pharmacy, поэтому отдельного скрытого invalidation counter не существует.''')
t=section(t,'### 12.3 CORS','### 12.4 CSRF',r'''### 12.3 CORS

Production CORS использует exact-origin allowlist, explicit methods/headers и credentials только для разрешённых origins. Wildcard origin с credentials запрещён. Preflight не предоставляет authorization и не ослабляет endpoint policy.''')
t=section(t,'### 12.4 CSRF','### 12.5 Input and output',r'''### 12.4 CSRF

Cookie-authenticated refresh/logout и другие state-changing endpoints принимают browser request только при exact allowed `Origin` и custom header:

```http
X-CSRF-Protection: 1
```

Absent, opaque, `null` или mismatched Origin отклоняется. `Referer` может использоваться только как дополнительный signal. SameSite cookie не является единственной защитой.''')
save(n,t); meta(n)

n='10-sequence-diagrams.md'; t=load(n)
t=re.sub(r'^## 26\. Открытые вопросы\n.*\Z',r'''## 26. Remaining sequence implementation decisions

1. endpoint-specific policy сохранения deterministic stable 4xx idempotency results;
2. public projection freshness/SLA;
3. elevated dual approval для risk-heavy ADMIN operations;
4. scoped versus all-session revoke для будущей multi-assignment model;
5. catalog publish atomic/partial sequence.

Lock order, idempotency-first protocol, retry budget, outbox lease/fencing, auth/session invalidation, API paths, states, enums и legal return baseline закрыты Gate E0 и не являются открытыми вариантами.''',t,flags=re.M|re.S)
save(n,t); meta(n)

n='11-development-roadmap.md'; t=load(n)
t=re.sub(r'^## 25\. Открытые вопросы\n.*\Z',r'''## 25. Remaining roadmap decisions

Gate E0 закрыт. Открыты только execution-level decisions, не допускающие альтернативную архитектуру:

1. pilot pharmacy и количественные pilot exit criteria;
2. SLO для critical API/worker pipelines;
3. ownership production operations, on-call и incident commander;
4. initial-stock cutover runbook;
5. public projection freshness target;
6. elevated approval model для особо рискованных ADMIN operations;
7. concrete infrastructure products при сохранении утверждённых RPO/RTO, retention, proxy и outbox contracts.''',t,flags=re.M|re.S)
save(n,t); meta(n)

meta('12-deployment.md'); meta('13-testing-strategy.md')
n='14-observability.md'; t=load(n)
t=t.replace('identity.assignment.revoked','pharmacy.assignment.ended')
t=t.replace('identity.assignment.ended','pharmacy.assignment.ended')
save(n,t); meta(n)

active=['04-architecture.md','04-01-backend-architecture.md','05-api-design.md','06-database-design.md','07-domain-model.md','08-project-structure.md','09-security-design.md','10-sequence-diagrams.md','11-development-roadmap.md','12-deployment.md','13-testing-strategy.md','14-observability.md']
for n in versions:
    t=load(n)
    t=re.sub(r'\n{3,}','\n\n',t)
    save(n,t)
all_text='\n'.join(load(n) for n in active)
for bad in ['auth_version','auth version','version-counter авторизации','consistency-incorporated','gate-e0-approved','SalePosted','ReturnPosted','sales.sale.posted','identity.assignment.revoked','01JZX3E2J9Q4JY3N8VY6F7XH2A','/api/v1/{documents}/{id}/reverse']:
    if bad.lower() in all_text.lower(): raise RuntimeError('stale '+bad)
required=['`pharmacy` | `pharmacies`, `pharmacy_assignments`','CREATE TABLE outbox_events','idempotency record','current user, session, role, pharmacy assignment','stock lots по `expiration_date`, затем `received_at`, затем `id`','POST | `/api/v1/admin/users/{user_id}/pharmacy-assignments`','DELETE | `/api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}`','`UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`','`RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`','`COMPLETED`, `PARTIALLY_REFUNDED`, `REFUNDED`, `REVERSED`','`SaleCompleted`','`PharmacistAssignmentEnded`','Argon2id PHC string','`pnpm` 10.x','openapi-typescript','RPO ≤ 15 минут','RTO ≤ 4 часа','Gate E0 закрыт']
for req in required:
    if req not in all_text: raise RuntimeError('missing '+req)
am=load('00-01-cross-document-consistency.md')
if '**Статус документа:** Incorporated' not in am or 'не участвует в нормативном порядке приоритетов' not in am: raise RuntimeError('amendment not historical')
print('OK')
