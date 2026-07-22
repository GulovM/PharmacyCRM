# PharmacyCRM — Подробное техническое задание на реализацию

**Статус документа:** Draft  
**Версия:** 1.0  
**Дата:** 2026-07-21  
**Связанные документы:** `00-documentation-index.md`, `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`, `13-testing-strategy.md`, `14-observability.md`

## 1. Назначение и нормативная роль

Документ переводит утверждённые продуктовые, архитектурные, API-, data-, security-, testing-, deployment- и observability-требования PharmacyCRM в исполнимое техническое задание. По нему команда должна последовательно реализовывать продукт короткими вертикальными срезами, не восстанавливая требования из разрозненных исходников и не выбирая альтернативную архитектуру по ходу разработки.

Документ определяет:

- порядок реализации этапов `E1–E12`;
- состав функций каждого этапа;
- акторов, права и pharmacy scope;
- минимальные request/response contracts;
- валидацию, бизнес-ограничения и состояния;
- транзакционные границы, идемпотентность, блокировки, audit и outbox;
- стабильные категории ошибок;
- требования безопасности и приватности;
- frontend-поведение;
- обязательные тесты и критерии приёмки;
- блокирующие решения, которые нельзя заменять скрытыми допущениями.

Документ не отменяет исходные нормативные документы. При противоречии применяется порядок из `02-srs.md`: закон и обязательные регуляторные требования → Product Vision → SRS → принятые ADR → детальные проектные документы → реализация и тесты. Обнаруженное расхождение исправляется в том же change set.

## 2. Как работать по этому ТЗ

### 2.1 Единица реализации

Единицей реализации является **vertical slice**, включающий по применимости:

1. уточнённый API contract и OpenAPI;
2. migration и constraints;
3. domain model и typed errors;
4. application use case;
5. use-case-specific Unit of Work;
6. PostgreSQL adapters;
7. authorization и pharmacy scope;
8. idempotency, transactional audit и outbox;
9. HTTP delivery;
10. frontend workflow;
11. logs, metrics и traces;
12. unit, integration, concurrency, contract, security и browser tests;
13. обновление документации.

Таблица без работающего use case, handler без application policy, mock-only frontend, mutation без обязательного audit/idempotency или endpoint без contract tests не считаются завершённой функцией.

### 2.2 Идентификаторы требований

Требования имеют стабильные идентификаторы:

```text
E<этап>-<область>-<номер>
```

Примеры: `E1-FND-001`, `E3-AUTH-004`, `E7-SALE-003`. Test case, pull request и release evidence должны ссылаться на соответствующие ID.

### 2.3 Статусы функции

Допустимые статусы реализации:

- `PLANNED` — есть требования, реализация не начата;
- `IN_PROGRESS` — change set открыт, contract ещё не признан стабильным;
- `IMPLEMENTED` — код, migration, tests, observability и документация согласованы;
- `PRODUCTION_DISABLED` — реализация может существовать, но production policy запрещает использование;
- `DEPRECATED` — действует период совместимости;
- `REMOVED` — функция удалена после завершённого deprecation process.

## 3. Граница MVP

### 3.1 Входит в MVP

- одна аптека как одна физическая точка продажи и хранения;
- роли `CLIENT`, `PHARMACIST`, `ADMIN`;
- публичный поиск лекарств без регистрации;
- пользователи, роли, sessions и назначения аптекарей;
- управление аптеками и публичным профилем;
- глобальный каталог, presentations, barcodes, requests и staging import;
- ассортимент аптеки, цены и правила внутренней единицы;
- поступления, initial stock, лоты и immutable inventory movements;
- продажа упаковкой и разрешённой внутренней единицей;
- server-side FEFO;
- sale void/reversal, write-off и inventory adjustment;
- customer return model, но production mutation выключена до юридического допуска;
- public availability projection;
- low-stock/expiry alerts и рекомендации ручного пополнения;
- transactional audit, idempotency и outbox;
- frontend для ADMIN, PHARMACIST и public/client journeys;
- Docker-based local/CI/staging/production delivery, backup и restore.

### 3.2 Не входит в MVP

- аптечные сети, несколько складов и перемещения между точками;
- бухгалтерский и налоговый учёт полного цикла;
- фискальные устройства и эквайринг;
- государственная маркировка, реестры и электронные рецепты;
- медицинская диагностика и хранение медицинских карт;
- онлайн-бронирование, онлайн-оплата и доставка;
- автоматические заказы поставщикам;
- работа critical stock/sale commands без backend и PostgreSQL;
- mobile applications;
- микросервисная архитектура.

## 4. Роли, scope и классификация данных

### 4.1 Публичный пользователь

Имеет доступ только к опубликованному каталогу, активным аптекам, публичной цене, агрегированному availability status и freshness timestamp. Не получает точный остаток, lot IDs, закупочную цену, документы, audit, персональные или session data.

### 4.2 `CLIENT`

Получает публичные возможности и будущие персональные функции. Роль не даёт доступа к internal pharmacy data.

### 4.3 `PHARMACIST`

Работает только в аптеке, на которую существует active `PharmacyAssignment`. Переданный `pharmacy_id`, frontend route и JWT claims не являются достаточным подтверждением scope. Backend повторно проверяет current user, session, role, assignment и pharmacy state.

### 4.4 `ADMIN`

Управляет пользователями, ролями, pharmacies, assignments, каталогом, moderation, audit и расследованиями. `ADMIN` не обходит domain invariants, immutable history, idempotency, audit и transaction protocol.

### 4.5 Классы данных

- `PUBLIC`: опубликованный каталог, публичная pharmacy profile, публичная цена и availability.
- `INTERNAL`: точные остатки, lots, documents, alerts и operational state.
- `CONFIDENTIAL`: закупочные цены, персональные данные, audit, IP, supplier data.
- `SECRET`: passwords, raw tokens, cookies, signing keys, DSN и backup secrets.

## 5. Общий HTTP API contract

### 5.1 Base paths и форматы

- business API: `/api/v1`;
- liveness: `GET /healthz`;
- readiness: `GET /readyz`;
- JSON: UTF-8, `application/json`;
- uploads: `multipart/form-data`;
- JSON request fields и query parameters: `snake_case`;
- paths: `kebab-case`, plural nouns;
- IDs во внешнем API: strings;
- datetime: RFC 3339 с timezone;
- date: `YYYY-MM-DD`;
- money: integer dirams, currency `TJS`;
- stock quantities: integer base units;
- `sale_unit`: `PACKAGE` или `INNER_UNIT`.

### 5.2 Strict request decoding

Command DTO обязаны:

- отклонять неизвестные поля;
- отклонять trailing data и несколько JSON values;
- ограничивать body, string, array и collection size;
- различать omitted, `null` и empty string;
- не принимать client-calculated totals, authoritative prices, lot allocation, role, ownership или security state;
- возвращать deterministic validation details.

Общие transport errors:

- malformed JSON/type/path/query/header → `400 INVALID_ARGUMENT`;
- unknown field → `400 UNKNOWN_FIELD`;
- empty mutable `PATCH` → `400 EMPTY_PATCH`;
- invalid cursor → `400 INVALID_CURSOR`;
- unsupported `Accept` → `406 NOT_ACCEPTABLE`;
- oversized body/file → `413 PAYLOAD_TOO_LARGE`;
- unsupported content type → `415 UNSUPPORTED_MEDIA_TYPE`.

### 5.3 Success envelope

```json
{
  "success": true,
  "data": {},
  "meta": {
    "request_id": "01JZX3G15D8RT7R1N3QFJ8Q5PX"
  }
}
```

Правила:

- `meta.request_id` обязателен в каждом JSON response;
- replay добавляет `meta.idempotency_replayed=true`;
- collection возвращает `items: []`, а не `404`;
- `204` не содержит body;
- `data` и `error` не присутствуют одновременно.

### 5.4 Error envelope

```json
{
  "success": false,
  "error": {
    "code": "INSUFFICIENT_STOCK",
    "message": "requested quantity is unavailable",
    "details": [
      {
        "field": "items[0].display_quantity",
        "code": "INSUFFICIENT_STOCK",
        "message": "requested quantity is unavailable"
      }
    ]
  },
  "meta": {
    "request_id": "01JZX3G15D8RT7R1N3QFJ8Q5PX"
  }
}
```

Handlers передают errors единому responder. Категория определяется через `errors.Is()`/`errors.As()`. Сравнение `err.Error()`, substring matching и ручное копирование mapping в handlers запрещены.

### 5.5 Общая HTTP error matrix

| HTTP | Public code | Назначение |
|---:|---|---|
| 400 | `INVALID_ARGUMENT` | malformed или structurally invalid request |
| 401 | `UNAUTHENTICATED` | credential отсутствует, invalid, expired или revoked |
| 403 | `FORBIDDEN` | роль или scope не разрешает операцию |
| 404 | `NOT_FOUND` | ресурс отсутствует или скрыт от actor |
| 406 | `NOT_ACCEPTABLE` | response format не поддерживается |
| 409 | `CONFLICT` | state, uniqueness или idempotency conflict |
| 412 | `PRECONDITION_FAILED` | stale `If-Match`/version |
| 413 | `PAYLOAD_TOO_LARGE` | превышен body/file limit |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | content type не поддерживается |
| 422 | `BUSINESS_RULE_VIOLATION` | syntactically valid command нарушает domain rule |
| 429 | `RATE_LIMITED` | превышен rate limit |
| 500 | `INTERNAL_ERROR` | unexpected internal failure |
| 503 | `SERVICE_UNAVAILABLE` | обязательная dependency недоступна |

SQL, stack trace, constraint/table names, filesystem paths, panic value, passwords, tokens, cookies и driver errors наружу не возвращаются.

### 5.6 Pagination

Для больших коллекций используется cursor pagination:

```text
?limit=50&cursor=<opaque>
```

- default `limit=50`;
- range `1–100`;
- cursor привязан к endpoint, actor/scope, filter set, sort и schema version;
- stable order всегда содержит unique tie-breaker `id`;
- `total_count` возвращается только при явно описанной необходимости.

### 5.7 Optimistic concurrency

Mutable reference resources (`User`, `Pharmacy`, `Product`, `ProductPresentation`, `PharmacyProduct`, import row) возвращают `ETag`, построенный из `version`.

Для `PATCH` и state-changing command над versioned resource:

- `If-Match` обязателен, если endpoint помечен как concurrency-controlled;
- отсутствие header → `428 PRECONDITION_REQUIRED` после добавления к API Design/OpenAPI;
- mismatch → `412 PRECONDITION_FAILED`;
- successful update увеличивает version и возвращает новый `ETag`.

До реализации первого такого endpoint код `PRECONDITION_REQUIRED` должен быть добавлен в `05-api-design.md` и OpenAPI в том же change set.

### 5.8 Idempotency

Critical commands требуют `Idempotency-Key`: ASCII, 1–128 символов, рекомендуемый UUID v4.

Identity:

```text
actor + operation + effective_scope + idempotency_key
```

- pharmacy command: `effective_scope=pharmacy_id`;
- global/admin command: `effective_scope=GLOBAL`;
- fingerprint включает path/resource IDs, scope, resource version и canonical semantic payload;
- request ID, JSON key order и transport-only metadata не входят;
- same identity + same fingerprint → original committed result;
- same identity + different fingerprint → `409 IDEMPOTENCY_KEY_REUSED`;
- replay выполняет current authorization и result visibility revalidation;
- record хранится минимум 30 дней.

### 5.9 Cache policy

- auth, sessions, confidential/internal responses: `Cache-Control: no-store`;
- public catalog may use ETag;
- public availability обязательно возвращает `as_of`/`inventory_changed_at`;
- initial public availability cache target: browser/shared max-age не более 30 секунд;
- projection старше 2 минут помечается stale или исключается согласно endpoint policy;
- фактические TTL и freshness SLO подтверждаются нагрузочными измерениями до pilot.

## 6. Общий security contract

### 6.1 Passwords

- Argon2id PHC string: `m=65536 KiB`, `t=3`, `p=2`, salt 16 bytes, hash 32 bytes;
- successful verify более слабого hash выполняет transparent rehash;
- `CLIENT`/`PHARMACIST`: минимум 12 Unicode code points;
- `ADMIN`: минимум 14;
- максимум 128;
- trimming/truncation password запрещены;
- known compromised и очевидно слабые passwords отклоняются;
- password, hash и recovery material не логируются.

### 6.2 Access token

- JWT `EdDSA`/Ed25519;
- TTL 10 минут;
- claims: `iss`, `aud`, `sub`, `sid`, `iat`, `nbf`, `exp`, `jti`;
- JWT header содержит `kid`;
- allowlist algorithm, issuer, audience и clock-skew validation обязательны;
- unknown `kid`, `none`, unexpected algorithm и invalid claims → `401 UNAUTHENTICATED`;
- private key не хранится в Git/image/frontend;
- rotation каждые 90 дней, verification overlap минимум 20 минут.

### 6.3 Refresh session

- opaque CSPRNG token 32 bytes;
- DB хранит только hash;
- cookie: `__Secure-pharmacy_refresh`, host-only, `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`;
- absolute TTL 30 дней;
- idle TTL 7 дней;
- rotation при каждом refresh;
- reuse старой generation отзывает token family;
- raw refresh token не логируется и не возвращается в JSON.

### 6.4 Session invalidation

Применимые sessions отзываются при:

- block/archive user;
- password change/reset/recovery;
- role revoke/change;
- pharmacy assignment end/change;
- confirmed compromise;
- logout current/all.

Critical mutation после commit security-state change немедленно отказывает благодаря DB revalidation. Некритический read с существующим access token прекращается не позже TTL access token.

### 6.5 ADMIN MFA

Production `ADMIN` обязан пройти TOTP MFA. Recovery codes генерируются CSPRNG, отображаются один раз, хранятся как hash и одноразовы. Recovery/reset создаёт high-severity audit. До реализации MFA endpoints их точные paths/DTO добавляются в API Design и OpenAPI.

### 6.6 CORS, CSRF и trusted proxies

- exact-origin allowlist;
- wildcard origin с credentials запрещён;
- forwarded headers доверяются только configured proxy CIDR;
- cookie-auth state-changing endpoints требуют exact allowed `Origin` и `X-CSRF-Protection: 1`;
- absent, `null`, opaque или mismatched Origin отклоняется;
- HTTPS обязателен в production;
- auth/confidential responses используют `no-store`.

### 6.7 Rate limits

Обязательны для login, refresh, public search, uploads и risky admin commands. Limit key не должен опираться только на spoofable header. `429` по возможности содержит `Retry-After`.

## 7. Общий transaction и reliability contract

### 7.1 Critical mutation protocol

Authenticated critical mutation выполняется в порядке:

1. transport validation без business writes;
2. deterministic canonicalization и stable IDs/values;
3. `BEGIN` через Unit of Work;
4. claim/lock idempotency record;
5. lock/read current user, session, role, assignment и pharmacy;
6. replay visibility revalidation;
7. business locks в canonical order;
8. повторный расчёт eligibility, price, quantity, FEFO и limits;
9. business document, snapshots, allocations, balances и movements;
10. mandatory transactional audit;
11. outbox rows;
12. completed idempotency result;
13. commit;
14. success HTTP response.

Любая ошибка до commit приводит к rollback всех business, audit, outbox и completed idempotency writes.

### 7.2 Canonical lock order

1. idempotency scope/key;
2. current actor, session, role и pharmacy assignment;
3. pharmacy;
4. root business document (для возврата — source `sale`);
5. source `sale_items` по `id`;
6. source `sale_item_allocations` по `id`;
7. `pharmacy_products` по `id`;
8. stock lots по `expiration_date`, `received_at`, `id`;
9. append-only inserts;
10. commit.

Use case может пропустить ненужный уровень, но не меняет взаимный порядок остальных.

### 7.3 Retry

- retry только SQLSTATE `40001` и `40P01`;
- максимум 3 попытки на request path;
- повторяется вся transaction callback;
- full-jitter exponential backoff: base 25 ms, cap 250 ms;
- context cancellation прекращает retry;
- domain conflict, insufficient stock и invalid command не retry-ятся;
- network/filesystem/broker side effect внутри callback запрещён.

### 7.4 Transactional audit

Mandatory audit входит в business transaction. Audit failure → rollback. Audit append-only и содержит actor/system, session, pharmacy, action, target, result, reason, request/trace IDs и bounded metadata без secrets.

### 7.5 Transactional outbox

- business fact и outbox row commit-ятся вместе;
- delivery at-least-once;
- claim batch 100;
- `FOR UPDATE SKIP LOCKED`;
- lease 30 секунд;
- guarded completion по `id + lease_token`/generation;
- max 8 attempts;
- full-jitter backoff от 2 секунд, cap 15 минут;
- exhausted → `DEAD_LETTER`;
- consumer idempotent;
- processed retention 30 дней, dead letter 180 дней;
- retention запускается отдельной periodic task, удаляет bounded terminal batches по `processed_at`/`dead_lettered_at` и не получает table-level `DELETE` для runtime role;
- projection имеет rebuild/reconciliation path.

## 8. Общие observability и testing requirements

### 8.1 Logging

Backend использует `zap.Logger`, созданный в composition root. Logs идут в terminal и configured file sink. Production format — JSON.

Минимальные поля: timestamp UTC с миллисекундами, level, service, environment, version, commit SHA, schema/worker protocol version, event, event schema version, request/trace/span IDs, operation, outcome, duration, error code. Access log содержит normalized route, method, status, request/response size и безопасный response summary, но не body.

### 8.2 Metrics

Prefix `pharmacycrm_`. Запрещены unbounded labels: IDs, raw URL/query, filename, user input, idempotency key, IP, user-agent. Обязательны HTTP RED, runtime USE, DB pool/transactions/locks, idempotency, audit, outbox, import, inventory integrity, security, backup и projection freshness metrics.

### 8.3 Tracing

OpenTelemetry-compatible traces включают HTTP ingress, auth policy, use case, UoW attempt, normalized DB operation, idempotency, audit, outbox, worker/import/migration/backup spans. Bodies, SQL parameters, secrets и user text не записываются.

### 8.4 Testing baseline

- Domain/Application unit tests;
- real PostgreSQL repository/module integration;
- HTTP contract tests;
- deterministic concurrency tests с independent connections;
- security/adversarial tests;
- frontend component/contract/browser E2E;
- migration/deployment/restore tests;
- fuzz/property tests для parsers, quantities, money, cursors, fingerprints и token parsing;
- fault injection для commit/audit/outbox/worker/storage failures.

Критический тест проверяет не только HTTP status, но и все затронутые rows, balances, movements, audit, outbox и idempotency state.

# 9. E1 — Engineering Foundation

## 9.1 Цель и gate

Создать воспроизводимую основу независимых `backend/` и `frontend/`. Business endpoints, кроме operational health/readiness, на этом этапе не реализуются.

### E1-FND-001 — Repository roots

**Статус:** `IMPLEMENTED`
**Evidence:** `make architecture-check` проверяет обязательные roots, запрещённые global paths и source imports между `backend/` и `frontend/`.

Создать sibling roots:

```text
backend/
frontend/
deploy/
docs/
```

Backend содержит `cmd/api`, `cmd/worker`, `cmd/migrate`, `internal/bootstrap`, `internal/platform`, `internal/shared`, `internal/orchestration`, `internal/modules`, `migrations`, `test`. Frontend содержит `app`, `pages`, `widgets`, `features`, `entities`, `shared`, `test`, `e2e`.

Запрещены source imports между roots, frontend внутри backend, global backend `handlers/services/repositories/models/utils`, giant `frontend/src/api.ts` и empty layer directories без use case.

**Acceptance:** architecture check автоматически отклоняет forbidden imports/paths.

### E1-FND-002 — Backend configuration

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/config`

Использовать `github.com/kelseyhightower/envconfig`. Configuration groups: app, HTTP, PostgreSQL, auth, proxy/CORS, logging, tracing/metrics, worker, import/storage.

Startup fail-fast при missing secret, invalid DSN, unsafe production cookie/TLS, wildcard CORS with credentials, invalid TTL/timeouts/pool, incompatible schema/protocol, invalid log path или production debug mode.

**Errors:** startup process завершает работу non-zero; secret values в message отсутствуют.

### E1-FND-003 — Logger

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/logging`

Создать Zap logger с terminal и rotating file sink. File path configurable, local default mounted volume. Startup probe проверяет path/permissions. Runtime file-sink failure использует fallback terminal, metric и alert, но не отключает transactional audit.

### E1-FND-004 — HTTP server

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/httpserver`

Использовать `gin.New()` и explicit `http.Server` с `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`, header/body limits и graceful shutdown.

Middleware order:

1. request ID;
2. panic recovery;
3. access logging;
4. tracing/metrics;
5. body-size limit;
6. CORS/security headers;
7. authentication parsing;
8. route policy/rate limit.

`gin.Context` не покидает delivery.

### E1-FND-005 — Central HTTP responder

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/shared/httpx`

Создать единый response/error mapper. Он формирует envelopes, request ID, safe message/details и mapping typed errors через `errors.Is()`/`errors.As()`.

**Acceptance:** fuzz/contract tests не обнаруживают panic, multiple responses, SQL/stack/secret leakage.

### E1-FND-006 — PostgreSQL pool

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/database`

Настроить `pgxpool` с max/min connections, acquire timeout, connection lifetime, idle timeout, health interval и cancellation propagation. Runtime и migration credentials разделены.

### E1-FND-007 — Operational endpoints

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/httpserver`

#### `GET /healthz`

- не требует auth;
- проверяет только живость process;
- не выполняет тяжёлый dependency check;
- `200` при живом process;
- safe response без topology/config.

#### `GET /readyz`

- `200`, если startup complete, process not draining, PostgreSQL reachable, schema compatible, worker protocol compatible и critical initialization complete;
- `503 SERVICE_UNAVAILABLE` иначе;
- не раскрывает DSN, hostnames или stack.

### E1-FND-008 — Migration executable

**Статус:** `IMPLEMENTED`
**Evidence:** `cd backend && go test ./internal/platform/migration`

`cmd/migrate` выполняет migrations как one-shot command. API/worker не мигрируют schema при startup. Добавить advisory lock, schema version, verification query и machine-readable exit status.

### E1-FND-009 — Frontend shell

**Статус:** `IMPLEMENTED`
**Evidence:** `cd frontend && pnpm typecheck && pnpm build`

- React + TypeScript strict + Vite;
- `pnpm` 10.x через Corepack;
- exact `packageManager` и единственный `pnpm-lock.yaml`;
- router, error boundary, runtime config validation;
- base API client с envelopes/request ID/cancellation;
- access token only in memory;
- generated client из `backend/api/openapi.yaml` через pinned `openapi-typescript` + `openapi-fetch`;
- generated output не редактируется вручную;
- unknown response fields игнорируются;
- sensitive state очищается при logout/session loss.

### E1-FND-010 — CI baseline

**Статус:** `IMPLEMENTED`
**Evidence:** `.github/workflows/ci.yml`

PR pipeline:

- formatting/lint/static checks;
- `go test ./...`, `go vet ./...`;
- frontend typecheck/lint/tests;
- migration from zero smoke;
- secret/dependency/container scans;
- OpenAPI generation diff;
- architecture import checks;
- Markdown links и Mermaid validation;
- reproducible container build без secrets.

**Exit Gate E1:** clean checkout запускается документированной командой; invalid configuration fail-fast; health/readiness различаются; shutdown bounded и tested; CI green; roots independent; artifacts clean.

# 10. E2 — Database Kernel and Reliability Primitives

## 10.1 E2-DB-001 — Base migrations

Создать forward migrations в dependency order:

1. extensions/schema/version metadata;
2. identity;
3. pharmacy/assignments;
4. catalog/import staging;
5. assortment;
6. inventory;
7. sales;
8. returns;
9. idempotency;
10. outbox;
11. audit;
12. alerts/public projection;
13. role seeds и DB privileges.

Каждая migration имеет verification query, проверяющую её critical constraints/indexes/triggers/functions/privileges, а также lock/rewrite assessment, compatibility note и forward-fix policy. Проверки только через `to_regclass` недостаточны. Destructive down migration по умолчанию запрещена.

## 10.2 E2-REL-001 — Unit of Work

Реализовать use-case-specific transaction contracts. Application/Domain API не содержит `pgx.Tx`, `pgxpool.Pool` или SQL. Callback success → commit; error/panic → rollback; callback error важнее secondary rollback error; commit error возвращается caller.

**Tests:** success, callback error, panic, rollback error, commit failure, cancellation, no leaked transaction.

## 10.3 E2-REL-002 — Retry classifier

Реализовать bounded whole-transaction retry только для `40001`/`40P01`. Attempt number observable. Stable IDs/command values создаются до callback. External side effects отсутствуют.

## 10.4 E2-REL-003 — Idempotency service

Ports:

- `Claim(identity, fingerprint, expires_at)`;
- `Complete(result)`;
- `Replay()`;
- `MarkRetryableFailure()` только при доказанном отсутствии committed effect.

**Errors:**

- missing/invalid key → `400 IDEMPOTENCY_KEY_REQUIRED`/`INVALID_ARGUMENT`;
- same identity, different hash → `409 IDEMPOTENCY_KEY_REUSED`;
- in-progress bounded wait exhausted → `409 CONCURRENT_MODIFICATION` или `503`, согласно доказанному state;
- storage failure → rollback/`500`.

## 10.5 E2-REL-004 — Transactional audit writer

Audit writer принимает только allowlisted structured metadata. Mandatory insert failure откатывает command. Runtime DB role не имеет штатного update/delete к audit.

## 10.6 E2-REL-005 — Outbox writer and worker

Реализовать append, batch claim, processing lease, guarded success, retry/dead-letter, graceful drain и manual audited replay. Event payload не копирует HTTP DTO и не содержит secrets.

**Tests:** two-worker race, crash before side effect, crash after side effect before acknowledgement, lease expiry, stale owner, poison event, duplicate delivery, protocol mismatch readiness.

## 10.7 E2-REL-006 — Canonical lock helpers

Создать узкие repository helpers, обеспечивающие один опубликованный order. Generic arbitrary lock API запрещён.

## 10.8 E2-REL-007 — Reconciliation testkit

Создать queries/oracles:

```text
SUM(inventory_movements.delta_base_units) = stock_lots.quantity_base_units
```

с учётом initial zero balance и operation status. Для каждого lot movements сортируются только по непрерывному `lot_sequence`, назначенному под lot lock; cumulative sum на каждом sequence равен `quantity_after_base_units`. Testkit должен выявлять gap/missing sequence, invalid cumulative state, orphan allocations, duplicate effects и missing audit/outbox.

**Exit Gate E2:** UoW/retry/idempotency/audit/outbox/lock order доказаны real PostgreSQL tests; runtime role не изменяет immutable rows; critical business commands ещё не включены.

# 11. E3 — Identity, Authentication and Authorization

## 11.1 E3-AUTH-001 — Login

**Endpoint:** `POST /api/v1/auth/login`  
**Access:** public  
**Rate limit:** required  
**Request:**

```json
{
  "login": "pharmacist-001",
  "password": "correct horse battery staple",
  "mfa_code": "123456"
}
```

`mfa_code` допускается только для actor, которому MFA required. Login normalized по trim/case policy, password не trim-ится.

**Success `200`:** JSON содержит `access_token`, `token_type="Bearer"`, `expires_in_seconds=600`, safe current principal; refresh token устанавливается только cookie.

**Rules:**

- unknown login и wrong password используют dummy hash и одинаковый внешний ответ;
- password verify выполняется вне длительной mutation transaction;
- после verify transaction повторно lock/read user state;
- session, login metadata, optional rehash и audit commit-ятся атомарно;
- blocked/archived user не получает session;
- auth response `no-store`.

**Errors:**

- invalid credentials/inactive actor → `401 INVALID_CREDENTIALS` без enumeration;
- missing/invalid ADMIN MFA → `401 INVALID_CREDENTIALS` до утверждения отдельных `MFA_*` codes;
- rate limit → `429 RATE_LIMITED`;
- DB unavailable → `503 SERVICE_UNAVAILABLE`.

**Audit/logs:** `auth.login.succeeded`, `auth.login.denied`; password/token absent.

## 11.2 E3-AUTH-002 — Refresh

**Endpoint:** `POST /api/v1/auth/refresh`  
**Credential:** refresh cookie + valid Origin + `X-CSRF-Protection: 1`.

**Success `200`:** new access token; cookie atomically replaced after commit.

**Rules:** source session/family locked; hash, expiry, user/role state verified; current generation consumed and next inserted in one transaction. Concurrent refresh: only one success. Previous generation reuse revokes family.

**Errors:** invalid/expired/revoked/reused/inactive actor → `401 SESSION_REVOKED` or generic `UNAUTHENTICATED`; cookie cleared. Reuse generates high-severity audit and metric.

## 11.3 E3-AUTH-003 — Logout

- `POST /api/v1/auth/logout`: current session revoke, idempotent, `204`, cookie expired;
- `POST /api/v1/auth/logout-all`: all actor sessions revoke, `204`, recommended idempotency;
- cookie endpoint CSRF controls mandatory;
- other users’ session existence not disclosed.

## 11.4 E3-AUTH-004 — Current principal

### `GET /api/v1/me`

Returns user ID, display name, phone, current role, current pharmacy assignment/scope, session ID and safe session expiry. No hashes, token family, audit/security internals.

### `PATCH /api/v1/me`

Mutable fields only: `display_name`, `phone`. Role, status, login, assignment, MFA/security attributes rejected as `400 UNKNOWN_FIELD` or mass-assignment violation. Requires `If-Match`.

### `POST /api/v1/me/password`

Request:

```json
{
  "current_password": "old passphrase",
  "new_password": "new long passphrase"
}
```

Success updates Argon2id hash/password_changed_at, revokes all sessions including current, audit commits, response `204`, client purges state.

**Errors:** invalid current password → `401 INVALID_CREDENTIALS`; policy violation → `422 PASSWORD_POLICY_VIOLATION` after code added to API contract; stale version → `412`.

## 11.5 E3-AUTH-005 — Session management

- `GET /api/v1/me/sessions`: list own sessions without token hashes/IP precision beyond approved policy;
- `DELETE /api/v1/me/sessions/{session_id}`: revoke owned session, `204`; foreign/missing → concealed `404`;
- current session revoke clears frontend state.

## 11.6 E3-ADMIN-001 — Create user

**Endpoint:** `POST /api/v1/admin/users`  
**Access:** `ADMIN` + recent authentication; production MFA.  
**Idempotency:** recommended, GLOBAL scope.

Request minimum:

```json
{
  "login": "pharmacist-001",
  "display_name": "Operator Name",
  "phone": "+992...",
  "role": "PHARMACIST",
  "initial_password": "temporary long passphrase"
}
```

For MVP the administrator supplies an initial password through a controlled channel; it is never returned, logged or audited. Before implementation, product owner must decide whether first-login forced change is required; if required, schema/API receive an explicit flag rather than hidden convention.

**Rules:** unique active login; role allowlist; password policy by role; actor cannot create hidden/unknown role; user + active role + audit/outbox atomic.

**Errors:** duplicate login → `409 CONFLICT`; invalid role/password → `422`; unauthorized/recent-auth missing → `403`; idempotency conflict → `409`.

## 11.7 E3-ADMIN-002 — User lifecycle

Endpoints: list/get/patch/block/unblock/archive/password-reset. Mutable profile fields require ETag. Block/archive and role change lock actor then target, revoke target sessions, audit/outbox and idempotency atomically.

Protection:

- self-block/archive/revoke is denied unless explicit last-admin-safe policy permits;
- system must never end with zero active production ADMIN accounts;
- archived user is terminal for new operations;
- login identifier reuse remains disabled unless a later policy says otherwise.

Errors: `ACCOUNT_BLOCKED`, `RESOURCE_ARCHIVED`, `CONCURRENT_MODIFICATION`, `PRECONDITION_FAILED`, `FORBIDDEN`, concealed `NOT_FOUND`.

## 11.8 E3-ADMIN-003 — Roles and pharmacy assignments

- one active role per user;
- one active pharmacy assignment per PHARMACIST in MVP;
- assignment create requires active PHARMACIST and non-archived pharmacy;
- ending assignment sets `ended_at`, `ended_by_user_id`, `end_reason`, never deletes history;
- assignment end/change revokes dependent sessions;
- concurrent duplicate assignment prevented by DB constraint.

**Endpoints:** existing admin assignment paths from API Design.  
**Errors:** incompatible role → `422 BUSINESS_RULE_VIOLATION`; active assignment exists → `409 CONFLICT`; inactive pharmacy → `422 PHARMACY_INACTIVE`; foreign/missing → `404`.

**Exit Gate E3:** JWT/session/role/assignment policies, refresh races, blocked actor denial, password/MFA flows and frontend auth state pass integration, race and browser tests.

# 12. E4 — Pharmacy and Global Catalog

## 12.1 E4-PHR-001 — Create and manage pharmacy

Admin create request:

```json
{
  "name": "Аптека №1",
  "address": "Душанбе, ...",
  "landmark": "Напротив ...",
  "latitude": 38.5737,
  "longitude": 68.7738,
  "phone": "+992...",
  "working_hours": "08:00-22:00"
}
```

Validation: trimmed required name/address, latitude `[-90,90]`, longitude `[-180,180]`, bounded phone/hours/landmark. Coordinates are explicit and never silently overwritten by geocoder.

States:

```text
ACTIVE <-> BLOCKED
ACTIVE/BLOCKED -> ARCHIVED
```

- `BLOCKED`: no new scoped mutations, history readable;
- `ARCHIVED`: no new assignments, no public search, terminal;
- updates use ETag;
- state transitions, sessions affected by assignment/pharmacy policy, audit/outbox atomic.

## 12.2 E4-PHR-002 — Pharmacy public profile

`PATCH /api/v1/pharmacies/{pharmacy_id}/public-profile` permits only name/address/landmark/coordinates/phone/hours according to role policy. `PHARMACIST` requires current assignment. Status and internal configuration are forbidden fields.

## 12.3 E4-CAT-001 — Product

Fields: `title`, optional `inn`, `dosage`, `form`, `manufacturer`, optional `country`, `is_prescription_required`, status/version.

States: `ACTIVE ↔ INACTIVE`, both → `ARCHIVED`. Archive affects future assortment/search only; snapshots remain unchanged.

**Errors:** invalid normalized value → `400/422`; duplicate according to moderation policy → `409`; archived target → `422 RESOURCE_ARCHIVED`; stale ETag → `412`.

## 12.4 E4-CAT-002 — Product presentation and barcode

Presentation fields: product ID, package name, optional inner-unit name, `base_units_per_package > 0`, status/version.

Rules:

- if `base_units_per_package > 1`, inner-unit name required;
- barcode active value globally unique;
- at most one active primary barcode per presentation;
- presentation/barcode used in history cannot be destructively removed;
- changes affect future operations only.

Errors: `DUPLICATE_BARCODE`, `RESOURCE_ARCHIVED`, `PRECONDITION_FAILED`, `NOT_FOUND`, `BUSINESS_RULE_VIOLATION`.

## 12.5 E4-CAT-003 — Product request

PHARMACIST creates scoped request with `raw_name` and bounded structured `raw_details`. States:

```text
OPEN -> APPROVED | REJECTED | DUPLICATE
```

Terminal decision requires admin actor, time and non-empty note. APPROVED/DUPLICATE requires resolved presentation. Terminal request cannot be reopened.

## 12.6 E4-IMP-001 — Catalog import

Upload accepts CSV or XLSX, configurable initial defaults:

- max file size 10 MiB;
- max 50,000 data rows;
- max 100 columns;
- max normalized cell 4 KiB;
- parsing timeout 5 minutes;
- macros, formulas, external links and executable content rejected/neutralized;
- MIME checked independently of extension;
- source stored in quarantine outside web root with server-generated key and SHA-256.

Persisted job states:

```text
UPLOADED -> VALIDATING -> READY/HAS_ERRORS/FAILED
READY/HAS_ERRORS -> VALIDATING
READY -> CONFIRMING -> COMPLETED/FAILED
```

Rows: `PENDING`, `VALID`, `ERROR`, `MATCHED`, `CREATE_NEW`, `REJECTED`, `PUBLISHED`.

### Publication policy for MVP

Publication is atomic per explicit batch of at most 500 approved rows. Job may contain several batches. Each batch has its own stable operation/fingerprint and idempotency key. A failed batch publishes nothing; successful previous batches remain committed and visible in report. Job becomes `COMPLETED` only when all non-rejected approved rows are published. This decision must be copied to API Design, Database Design/Domain Model if persistence changes, and OpenAPI before implementation.

Errors: invalid file → `400 INVALID_ARGUMENT`; limit → `413`; validation findings → job `HAS_ERRORS`; publish with blocking rows → `422 IMPORT_HAS_ERRORS`; repeated completed batch → replay or `409 IMPORT_ALREADY_CONFIRMED`; storage unavailable → `503`.

**Security:** streaming parse, bounded CPU/memory, path traversal protection, formula-injection-safe reports, audit for upload/validate/row decision/publish.

**Exit Gate E4:** active pharmacy/catalog lifecycle, scope denial, moderation and import restart/replay are tested; staging metadata never reaches public API.

# 13. E5 — Assortment and Pricing

## 13.1 E5-AST-001 — Attach presentation to pharmacy

`POST /api/v1/pharmacies/{pharmacy_id}/assortment`

Request:

```json
{
  "product_presentation_id": "uuid",
  "default_package_price_dirams": 12500,
  "is_inner_unit_sale_allowed": true,
  "default_inner_unit_price_dirams": 4500,
  "min_stock_level_base_units": 5,
  "target_stock_level_base_units": 20
}
```

Rules:

- pharmacy and presentation active;
- unique `(pharmacy_id, presentation_id)`;
- amounts integer `>=0`;
- if inner sale allowed, presentation has inner unit and inner price is non-null/non-negative;
- if disabled, inner price is null;
- `target >= min >= 0`;
- actor scope revalidated in transaction;
- audit/outbox/idempotency applied.

## 13.2 E5-AST-002 — Update and state

PATCH prices, inner-unit policy and thresholds with `If-Match`. States `ACTIVE ↔ INACTIVE`, both → `ARCHIVED`. Only active item participates in new sale/public availability. Existing lots/history remain readable and snapshots unchanged.

Errors: duplicate → `409`; invalid price/rule → `422`; stale version → `412`; wrong pharmacy → concealed `404`/`PHARMACY_ACCESS_DENIED`; archived → `422 RESOURCE_ARCHIVED`.

**Security:** frontend cannot submit current stock or `inventory_changed_at`; mass assignment rejected. Price changes audit old/new safe values and reason when policy requires.

**Exit Gate E5:** no silent lost update; server price authority proven; other-pharmacy access denied; archive/activate state tested.

# 14. E6 — Receipts, Initial Stock and Inventory Truth

## 14.1 E6-RCV-001 — Post receipt

**Endpoint:** `POST /api/v1/pharmacies/{pharmacy_id}/receipts`  
**Access:** PHARMACIST/ADMIN + scope  
**Idempotency:** required.

Request:

```json
{
  "receipt_number": "RCV-2026-0001",
  "supplier_name": "Supplier",
  "received_at": "2026-07-21T08:00:00+05:00",
  "items": [
    {
      "pharmacy_product_id": "uuid",
      "batch_number": "B-100",
      "expiration_date": "2027-12-31",
      "quantity_packages": 20,
      "purchase_price_package_dirams": 9000,
      "retail_price_package_dirams": 12000,
      "retail_price_inner_unit_dirams": 4200
    }
  ]
}
```

Constraints:

- 1–200 items;
- unique item identity according to presentation/batch/expiration policy;
- positive package quantity;
- prices non-negative and overflow-safe;
- assortment belongs to pharmacy and is not archived;
- `quantity_base_units = quantity_packages × current base_units_per_package`, calculated by backend;
- server snapshots packaging/prices;
- unique receipt number within pharmacy;
- `posted_at` server-generated;
- no draft CRUD in MVP.

Atomic result: `Receipt(POSTED)`, items, one source lot per item, `InventoryOperation(RECEIPT)`, positive movements, lot balances, `inventory_changed_at`, audit `ReceiptPosted`, outbox `ReceiptPosted`, completed idempotency result.

Errors:

- duplicate number → `409 DUPLICATE_DOCUMENT_NUMBER`;
- invalid/archived assortment → `422 RESOURCE_ARCHIVED`;
- invalid quantity/price/date → `400/422`;
- overflow → `422 BUSINESS_RULE_VIOLATION`;
- inactive pharmacy → `422 PHARMACY_INACTIVE`;
- required key absent/reused → idempotency errors;
- audit/outbox/commit failure → rollback and `500/503`.

## 14.2 E6-RCV-002 — Receipt reads and reversal

List/get are scoped, cursor-paginated and include snapshots/lots/movements without exposing secrets. Posted receipt cannot PATCH/DELETE.

`POST .../{receipt_id}/reverse` is `ADMIN`-only for MVP, requires recent authentication, reason and idempotency. It creates separate `REVERSAL` operation/movements and marks source `REVERSED`; no history deletion. Reversal denied if safe compensation cannot be proven due to subsequent stock consumption.

## 14.3 E6-IMP-001 — Initial stock import

Uses standard template and same safe file controls. Preview shows original package quantity, coefficient and calculated base units. Ambiguous presentation/package mapping is blocking.

`confirm` requires idempotency and atomically creates `Receipt` with operation `INITIAL_STOCK`, lots, movements, audit/outbox. Reconfirm replays original result. Initial stock never writes lot quantity without movements.

## 14.4 E6-INV-001 — Inventory reads

Endpoints provide:

- aggregated internal stock by pharmacy product;
- lots with status/expiry/batch/quantity/prices according to access policy;
- immutable lot movement ledger;
- inventory operation journal;
- freshness/reconciliation status.

No POST/PATCH/DELETE exists for movements or generic stock.

## 14.5 Legal blocker: expiration semantics

До закрытия E6 Product Owner/legal owner обязан утвердить `DEC-LEGAL-EXPIRY-001`: является ли `expiration_date` последним допустимым днём продажи либо первым недопустимым днём. Решение синхронно меняет Domain, API validation, FEFO eligibility, alerts и tests. До решения production sale запрещена.

**Exit Gate E6:** receipt/initial stock/reversal are atomic and idempotent; movement reconciliation exact; posted history immutable; pharmacist frontend completes workflow without SQL.

# 15. E7 — Sales and FEFO

## 15.1 E7-SALE-001 — Complete sale

**Endpoint:** `POST /api/v1/pharmacies/{pharmacy_id}/sales`  
**Access:** PHARMACIST/ADMIN + scope  
**Idempotency:** required.

Request:

```json
{
  "payment_method": "CASH",
  "prescription_confirmed": false,
  "items": [
    {
      "pharmacy_product_id": "uuid",
      "sale_unit": "PACKAGE",
      "display_quantity": 2
    },
    {
      "pharmacy_product_id": "uuid",
      "sale_unit": "INNER_UNIT",
      "display_quantity": 1
    }
  ]
}
```

Client не передаёт price, subtotal, total, lot ID, allocation или quantity_base_units. Sale number генерируется backend как уникальный pharmacy-scoped identifier.

Validation:

- 1–100 items;
- unique `(pharmacy_product_id, sale_unit)`;
- positive quantity;
- payment method: `CASH`, `CARD`, `MOBILE`, `MIXED`;
- active assortment and pharmacy;
- inner sale allowed and priced;
- prescription confirmation required if any product requires it;
- integer multiplication/addition overflow-safe.

Transaction:

1. idempotency lock;
2. actor/session/role/assignment/pharmacy revalidation;
3. pharmacy lock;
4. affected pharmacy products by ID;
5. eligible lots by expiration/received/id;
6. recompute sellability, prices, base units and FEFO allocations;
7. insufficient any line → rollback whole sale;
8. insert `Sale(COMPLETED)`, items/snapshots/allocations;
9. decrement lots and append `SALE` movements;
10. update inventory freshness;
11. audit/outbox/idempotency complete;
12. commit before `201`.

Success returns sale ID/number/status, server-calculated item prices/totals, display quantities, snapshots and printable representation. Internal allocation details are returned only to authorized internal clients.

Errors:

- duplicate pair → `400 DUPLICATE_SALE_ITEM`;
- inner unit disabled → `422 INNER_UNIT_SALE_DISABLED`;
- missing prescription confirmation → `422 PRESCRIPTION_CONFIRMATION_REQUIRED`;
- insufficient eligible stock → `422 INSUFFICIENT_STOCK`;
- inactive/archived assortment/pharmacy → `422`;
- overflow → `422`;
- conflicting idempotency key → `409`;
- revoked scope → `403/404`;
- DB unavailable/timeout → `503` when dependency failure is known, otherwise safe `500`.

## 15.2 E7-SALE-002 — Sale reads

List/get are scoped and cursor-paginated. Public users never receive sale data. Read model contains immutable snapshots, payment method, totals, status and allowed allocation details. Returned/refunded state must be derivable.

## 15.3 E7-SALE-003 — Void/reversal

`POST .../{sale_id}/void` is `ADMIN`-only for MVP, recent authentication, mandatory reason, required idempotency. Allowed only before custody transfer or under approved operational policy and only if full safe compensation is possible. Creates reversal operation/movements and source status `REVERSED`; no delete/update of items/allocations.

Errors: already reversed/refunded → `409 CONFLICT`; unsafe compensation → `422`; stale permission → `403`; missing reason → `400/422`.

## 15.4 Concurrency acceptance

Two concurrent sales cannot oversell. Second transaction waits, rereads after lock and receives `422 INSUFFICIENT_STOCK`. Test uses explicit barriers, real PostgreSQL and validates balances/movements/audit/idempotency/outbox.

**Exit Gate E7 / RG1:** receipt and sale reconcile, no P0, internal alpha only, backup/restore development rehearsal successful, audit/logs sufficient for investigation.

# 16. E8 — Returns, Write-offs, Adjustments and Reversals

## 16.1 E8-RET-001 — Return eligibility

`GET .../sales/{sale_id}/return-eligibility` returns remaining quantities by sale item/source allocation and current legal feature state. It does not promise that a later command succeeds without transactional revalidation.

## 16.2 E8-RET-002 — Customer return

**Status:** `PRODUCTION_DISABLED` by default.

`POST /api/v1/pharmacies/{pharmacy_id}/returns` accepts source `sale_id`, lines with `sale_item_id`, returned quantity, action and reason. Client does not submit authoritative refund amount.

Allowed physical actions after custody transfer: `QUARANTINE`, `WRITE_OFF`, `NO_PHYSICAL_RETURN`. `RESTOCK` for customer-returned medicine is rejected `422 RETURN_NOT_LEGALLY_ALLOWED`.

Partial refund execution remains disabled until `DEC-LEGAL-REFUND-001` defines allocation and rounding. Until then only full-sale reversal before custody transfer is available.

When enabled, transaction locks sale, items, source allocations and previous return usage, recomputes cumulative limits/refund, writes return/allocations, non-sellable inventory effect, sale status, audit/outbox/idempotency atomically.

Errors: feature/legal disabled → `422 RETURN_NOT_LEGALLY_ALLOWED`; cumulative excess → `422 RETURN_QUANTITY_EXCEEDED`; wrong sale/pharmacy → `404`; terminal/reversed state → `409`; unsupported action → `400`; conflict/replay errors as common contract.

## 16.3 E8-WO-001 — Write-off

`POST /api/v1/pharmacies/{pharmacy_id}/write-offs`

Request:

```json
{
  "reason_code": "EXPIRED",
  "reason": "expired stock removal",
  "items": [
    {"stock_lot_id":"uuid","quantity_base_units":5}
  ]
}
```

Reason code uses allowlist (`EXPIRED`, `DAMAGED`, `LOST`, `QUALITY_REJECTED`, `OTHER`); `OTHER` requires detailed reason. Quantity positive, lot belongs to pharmacy, result non-negative. Entire document atomic. Status `COMPLETED`, correction only through reversal.

Errors: insufficient lot → `422 INSUFFICIENT_STOCK`; invalid reason → `422`; duplicate lot line → `400`; inactive pharmacy/lot → `422`; wrong scope → `404/403`.

## 16.4 E8-ADJ-001 — Inventory adjustment

For MVP access is `ADMIN` only, production MFA + recent authentication. This closes elevated-permission baseline until a finer permission model is introduced.

Request contains reason and lines `{stock_lot_id, expected_quantity_base_units, actual_quantity_base_units}`. Backend locks lots, verifies current quantity equals expected, calculates `delta=actual-expected`, rejects zero delta, prevents negative result, writes document/movements/audit/outbox/idempotency.

Errors: current != expected → `409 CONCURRENT_MODIFICATION`; invalid/zero delta → `422`; missing reason → `422`; wrong pharmacy → `404`; replay conflict → `409`.

## 16.5 E8-REV-001 — Reversal framework

Each owner module provides explicit reversal command. One source operation reverses at most once. Reversal has reason, actor, source link and compensating movements. Generic “set status” or “PATCH stock” forbidden.

**Exit Gate E8:** original documents immutable, cumulative return bounds/concurrency proven, every write-off/adjustment/reversal leaves document+movement+audit, and customer return remains inaccessible until blockers formally closed.

# 17. E9 — Public Search, Alerts and Replenishment

## 17.1 E9-SRCH-001 — Public product search

`GET /api/v1/public/products/search`

Query:

- `q`: trimmed, 2–150 characters;
- optional form/dosage/manufacturer filters;
- `limit`/`cursor`;
- allowlisted sort.

Returns only active published product/presentation data. Empty result → `200 items:[]`. Search input is restricted data and not emitted to metrics/logs/traces. Apply rate limiting and bounded query execution.

## 17.2 E9-SRCH-002 — Public availability

`GET .../presentations/{presentation_id}/availability`

Optional `latitude`, `longitude`, `sort=distance|price`. Response per active pharmacy: pharmacy ID, name/address/landmark/phone/hours, coordinates, package/inner price, `availability_status` (`AVAILABLE`, `LOW_STOCK`, `UNAVAILABLE` according to public policy), `as_of`, route action.

Never returns exact stock, lot, batch, expiration, purchase price, audit or employee data. Projection is eventually consistent and never used as command source.

Errors: invalid coordinates/cursor → `400`; archived/missing presentation → `404`; rate limit → `429`; projection dependency unavailable → graceful empty/stale or `503` according to explicit freshness policy.

## 17.3 E9-ALR-001 — Alerts

Types: `LOW_STOCK`, `EXPIRED`, `EXPIRING_7_DAYS`, `EXPIRING_30_DAYS`, `RECONCILIATION_MISMATCH`.

States:

```text
ACTIVE -> ACKNOWLEDGED -> RESOLVED
ACTIVE -> RESOLVED
```

Active dedup key unique per pharmacy. Re-detection updates `last_confirmed_at`; after resolved a new occurrence creates a new alert. Acknowledge/resolve requires scope, actor/time and audit.

## 17.4 E9-REP-001 — Replenishment recommendations

Read-only explainable recommendation using current eligible stock, min/target levels, recent sales window and alert state. Returns suggested quantity and reasons. It never places supplier orders or changes stock.

## 17.5 E9-REC-001 — Reconciliation job

Admin diagnostic job recomputes balances from movements and checks orphan/duplicate/invalid states. It never auto-fixes. Divergence creates P0 signal, report and incident workflow; correction uses explicit adjustment/reversal after review.

**Exit Gate E9:** public data boundary tested, freshness measured, duplicate event does not duplicate alert, projection failure does not rollback sale, abuse limits active.

# 18. E10 — Complete Operational Frontend

## 18.1 ADMIN journeys

- login + TOTP;
- users, roles, status and sessions;
- pharmacies and assignments;
- catalog/product/presentation/barcodes;
- catalog import/moderation;
- audit search/details;
- controlled reversal/adjustment/reconciliation.

## 18.2 PHARMACIST journeys

- login/session expiry;
- own pharmacy profile;
- assortment/pricing;
- receipt and initial stock import;
- inventory/lots/movements;
- sale workspace with package/inner unit intent;
- write-off;
- return eligibility with disabled-state explanation;
- alerts and replenishment.

## 18.3 Public journeys

- product search/filter;
- presentation selection;
- availability list/map;
- sort by distance/price;
- freshness and stale/degraded state;
- route to external map.

## 18.4 Frontend invariants

- access token memory-only;
- refresh cookie unreadable to JS;
- route guards are UX only;
- server errors mapped by stable code;
- UI never shows success before server confirmation;
- one logical submit reuses one idempotency key across uncertain retries;
- duplicate click disabled;
- AbortController + actor/session generation prevent stale response from restoring purged state;
- logout clears caches, sensitive stores and pending requests;
- dangerous commands require confirmation/reason;
- accessibility and keyboard flows mandatory;
- frontend does not calculate authoritative totals/FEFO/permissions.

**Exit Gate E10 / RG2:** each critical workflow has browser E2E; direct URL cannot bypass backend; current frontend and supported backend contracts pass compatibility tests.

# 19. E11 — System Hardening

## 19.1 Security hardening

Threat-model review, ADMIN MFA enforcement, CORS/CSRF/CSP/HSTS/trusted proxy tests, rate-limit abuse tests, key/secret rotation drill, PostgreSQL privilege review, audit completeness, dependency/SBOM review. No Critical/High finding without approved time-bounded exception; P0 exception forbidden.

## 19.2 Reliability hardening

Load-aware concurrency suite, worker crash/restart, outbox backlog recovery, DB pool exhaustion, graceful shutdown during transaction/job, migration failure, retry-storm prevention, clock skew/expiration boundaries, disk/log/telemetry degradation.

## 19.3 Performance baseline

Measure password hashing capacity, sale p50/p95/p99, FEFO plan/lock duration, public search, import throughput/memory, outbox/projection lag, DB pool saturation, table/index/WAL growth, frontend bundle/core journeys.

Correctness oracles remain active under load: no negative stock, no duplicate effect, complete audit, exact reconciliation.

## 19.4 Recovery hardening

Daily base backup + continuous WAL; off-site encrypted copy; RPO `≤15m`, RTO `≤4h`; isolated restore drill at least quarterly. Restore verifies schema, constraints, reconciliation, audit, outbox duplicate safety and application readiness.

**Exit Gate E11:** measured evidence, successful restore, runbooks reproducible by another engineer, SLO baseline approved.

# 20. E12 — Production Readiness and Pilot

## 20.1 Production prerequisites

- immutable OCI artifacts by digest;
- same digest staging/production;
- separate migration one-shot;
- schema/application/worker compatibility declaration;
- TLS and secret delivery;
- monitoring, alerts and routing;
- backup/restore automation and evidence;
- release, rollback/forward-fix and incident runbooks;
- data retention/legal hold policy;
- operator training and support ownership;
- initial cutover/reconciliation/sign-off.

## 20.2 Pilot start gate

Pilot uses limited pharmacy/user set and verified catalog/initial stock. Start only when no P0, P1 are owned and non-correctness/security, restore successful, critical alerts delivered, reconciliation baseline exact, artifact immutable.

## 20.3 Pilot stop conditions

Immediate pause on unexplained stock divergence, authorization bypass, duplicate irreversible effect, missing audit reconstruction, backup/restore failure or repeated critical workflow failure.

## 20.4 Production approval

All correctness/security P0/P1 closed; pilot criteria met; reconciliation exact; restore/incident drills successful; audit reconstructs actor/action/target/result/correlation; Product, Engineering/Security and Operations accept readiness.

# 21. Feature-level Definition of Ready

Feature may enter implementation when:

1. actor and business goal defined;
2. role/resource/pharmacy scope defined;
3. API request/response/error contract exists;
4. aggregate owner and transaction boundary known;
5. schema/migration impact known;
6. idempotency identity/fingerprint defined;
7. lock order and race scenarios listed;
8. audit/outbox semantics defined;
9. retry/partial-failure behavior defined;
10. frontend states/workflow defined;
11. logs/metrics/traces/alerts defined;
12. acceptance criteria and test oracle defined;
13. legal/security blockers closed or feature explicitly disabled;
14. dependency stage gate closed.

# 22. Feature-level Definition of Done

Feature is complete only if:

1. SRS/API/OpenAPI/DB/Domain/Security docs agree;
2. business invariants are in Domain/Application, not handler/frontend;
3. current actor/scope is revalidated where required;
4. critical mutation has idempotency;
5. transaction and lock order are tested;
6. mandatory audit is atomic;
7. outbox used for durable post-commit reactions;
8. errors use typed matching and centralized responder;
9. migrations/constraints tested from zero and upgrade path;
10. unit/integration/concurrency/contract/security/browser tests pass;
11. frontend preserves server authority and clears sensitive state;
12. logs/metrics/traces contain no secrets/unbounded labels;
13. failure/retry/disconnect-after-commit paths verified;
14. API Design status set to `Implemented` only after contract review;
15. documentation updated in the same change set;
16. CI passes on clean checkout;
17. no P0/P1 in feature scope.

# 23. Blocking decisions and deadlines

| ID | Decision | Must be closed before |
|---|---|---|
| `DEC-E3-001` | forced password change after admin-created initial password | user creation endpoint becomes `Implemented` |
| `DEC-LEGAL-EXPIRY-001` | exact expiration-date sellability semantics | E6 exit / any production sale |
| `DEC-LEGAL-REFUND-001` | customer-return legality, full/partial refund allocation and rounding | enabling E8 return mutation |
| `DEC-OPS-001` | production secret manager and key rotation tooling | E12 production |
| `DEC-OPS-002` | hosting/ingress/registry/signing stack | E12 production |
| `DEC-OPS-003` | exact SLOs, alert routing and on-call ownership | E11 exit |
| `DEC-OPS-004` | connection/capacity budgets and import storage | pilot candidate |
| `DEC-OPS-005` | pilot pharmacy, success/failure thresholds and sign-off owners | E12 pilot start |

A blocker не может быть закрыт только кодом или устным решением. Требуется update соответствующих нормативных документов, tests и operational policy/ADR.

# 24. Рекомендуемая последовательность первых change sets

1. `E1-FND-001`: roots, Go module, pnpm shell, root Makefile.
2. `E1-FND-002/003`: envconfig validation и Zap terminal/file logger.
3. `E1-FND-004/005`: Gin server, middleware, central responder.
4. `E1-FND-006/007`: pgxpool, health/readiness.
5. `E1-FND-008/010`: migrate command и CI baseline.
6. `E1-FND-009`: OpenAPI skeleton/generated frontend client.
7. `E2-DB-001`: schema metadata + identity/pharmacy migrations.
8. `E2-REL-001/002`: UoW и retry.
9. `E2-REL-003/004`: idempotency + audit.
10. `E2-REL-005`: outbox writer/worker.
11. `E2-REL-006/007`: lock helpers/reconciliation testkit.
12. `E3-AUTH-001`: login vertical slice.
13. `E3-AUTH-002/003`: refresh/logout.
14. `E3-AUTH-004/005`: me/password/sessions.
15. `E3-ADMIN-001/002`: admin user lifecycle.
16. `E3-ADMIN-003`: role/assignment lifecycle.
17. `E4-PHR-001/002`: pharmacy lifecycle/profile.
18. `E4-CAT-001/002/003`: catalog and requests.
19. `E4-IMP-001`: staging import.
20. `E5-AST-001/002`: assortment/pricing.
21. `E6-RCV-001`: receipt vertical slice.
22. `E6-IMP-001/E6-INV-001`: initial stock and inventory reads.
23. `E7-SALE-001/002`: sale + FEFO.
24. `E7-SALE-003`, then E8 corrections.
25. E9 projections/alerts/replenishment, E10 frontend completion, E11 hardening, E12 pilot.

Каждый change set должен быть достаточно мал, чтобы reviewer мог проверить transaction boundary, authorization, lock order, idempotency, audit, API contract, migration safety и tests без скрытых побочных изменений.
