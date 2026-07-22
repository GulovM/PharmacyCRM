# PharmacyCRM — Development Roadmap

> E2-FIX-022 through E2-FIX-031 complete schema `23` reliability hardening: upgrade compatibility, least privilege, verified session constraints, bounded retention and architecture gates.

**Статус документа:** Draft  
**Версия:** 1.0  
**Дата:** 2026-07-21  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`

## 1. Назначение и нормативная роль

Документ определяет исполнимый порядок реализации PharmacyCRM: критический путь, параллельные workstreams, зависимости, обязательные архитектурные и security gates, критерии входа и выхода, release blockers и условия допуска MVP к pilot и production.

Roadmap не является календарным обещанием. Сроки, команда и фактическая скорость могут меняться, но изменение порядка, позволяющее реализовать зависимую функцию до доказанной готовности её correctness- и security-примитивов, требует явного пересмотра риска.

Roadmap не заменяет:

- Product Vision и SRS — продуктовый scope и внешние требования;
- API Design — HTTP-контракты;
- Database Design — схему, constraints и migration strategy;
- Domain Model — агрегаты, инварианты и transaction boundaries;
- Project Structure — package ownership и физические границы;
- Security Design — security controls;
- Sequence Diagrams — порядок проверок, блокировок, commit, rollback и post-commit flow.

Изменение scope, этапа, gate, зависимости или release blocker обновляет этот документ в том же change set.

## 2. Нормативные понятия

- **Stage / этап** — логическая группа capability, объединённая общим риском и gate.
- **Entry criteria** — условия, без которых этап нельзя считать начатым.
- **Exit gate** — проверяемые доказательства завершения этапа.
- **Critical path** — последовательность capability, без которой невозможен безопасный pilot.
- **Workstream** — непрерывное направление, выполняемое параллельно этапам.
- **Vertical slice** — завершённый пользовательский или операционный сценарий от migration и domain до API, frontend, audit, tests и документации.
- **Release blocker** — дефект или незакрытое условие, запрещающее выпуск.
- **Evidence** — воспроизводимый тест, отчёт CI, migration rehearsal, restore drill, benchmark, security review или иной проверяемый артефакт.

Gate не закрывается устным утверждением или наличием кода без evidence.

## 3. Принципы реализации

### 3.1 Vertical slice вместо горизонтальных слоёв

Функция реализуется как единый change set или короткая серия совместимых change sets:

1. нормативный contract;
2. migration и constraints;
3. domain/application rules;
4. repository и Unit of Work;
5. authorization и audit;
6. HTTP endpoint;
7. frontend workflow, если применимо;
8. unit, integration, concurrency, contract, security и browser tests;
9. logs, metrics и traces;
10. документация.

Не считаются завершёнными:

- таблица без рабочего use case;
- handler без application policy и transaction boundary;
- mock-only frontend;
- happy path без negative/race tests;
- mutation без idempotency и audit, когда они обязательны;
- endpoint, отсутствующий в `05-api-design.md`;
- feature, которую невозможно безопасно наблюдать и диагностировать.

### 3.2 Risk-first

Сначала доказываются механизмы, ошибка в которых может повредить весь продукт:

1. воспроизводимый runtime и CI;
2. migrations и constraints;
3. Unit of Work, retry и lock order;
4. identity, sessions и authorization;
5. idempotency, transactional audit и outbox;
6. immutable inventory truth;
7. только затем — продажи, возвраты и корректировки.

### 3.3 Security, testing и observability не являются финальными этапами

Security review, тестирование, logging, metrics, tracing, deployment compatibility и documentation выполняются в каждом vertical slice. Поздний hardening только проверяет систему целиком и не компенсирует небезопасный дизайн ранних этапов.

### 3.4 Без скрытого временного дизайна

Временное решение допустимо только если оно:

- явно помечено;
- имеет владельца и срок пересмотра;
- не нарушает domain, security и data-integrity invariants;
- не создаёт альтернативный несовместимый implementation path;
- имеет automated guard от случайного production use, если не production-ready;
- зарегистрировано как risk или security exception.

### 3.5 Small-batch delivery

Change set должен быть достаточно мал, чтобы reviewer мог проверить:

- transaction boundary;
- authorization;
- lock order;
- idempotency;
- audit;
- API contract;
- migration safety;
- tests.

Большие изменения, скрывающие несколько независимых рисков, должны разбиваться.

## 4. Модель выполнения roadmap

Roadmap состоит из:

- критического пути `E0 → E1 → E2 → E3 → E4 → E5 → E6 → E7 → E8 → E9 → E10 → E11 → E12`;
- параллельных workstreams `W1–W7`, выполняемых с первого этапа;
- release gates `RG1–RG4`;
- pilot gates `PG1–PG3`.

Этап может начаться частично, если его независимый scope не опирается на незакрытую зависимость. Его exit gate закрывается только полностью.

Например, frontend shell может развиваться на E1, но frontend продажи не могут считаться завершёнными до стабильного API, transaction semantics и backend contract E7.

## 5. E0 — решения и baseline governance — CLOSED
### Результат
Gate E0 закрыт 2026-07-20. Параллельные несовместимые implementation paths запрещены.
### Decision register

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

Дополнительно утверждено:
- production topology baseline: reverse proxy → backend API + outbox worker → PostgreSQL; projection/search остаются rebuildable;
- risk acceptance owner — project owner/architecture owner; security/legal exceptions требуют явной записи и срока;
- release artifacts — immutable OCI images by digest; migration — separate one-shot job; startup auto-migration запрещена.
### Exit evidence
- каждое решение имеет нормативный раздел в документах 04–14;
- правила cross-document amendment полностью перенесены в основные документы, а промежуточный файл удалён;
- module ownership, API paths, states, enum, events и transaction protocol едины;
- Gate E1–E3 могут начинаться без выбора альтернативной архитектуры;
- implementation evidence остаётся exit condition соответствующих этапов, но не открытым design decision.
### Запрещённый переход
Нельзя отклоняться от утверждённого baseline без ADR/policy update и синхронизации всех затронутых документов в одном change set.

## 6. E1 — engineering foundation

### Цель

Создать воспроизводимую основу независимых приложений `backend/` и `frontend/`.

### Backend scope

- Go module и package layout согласно `08-project-structure.md`;
- composition root;
- `gin.New()` и явная middleware composition;
- явный `http.Server` с timeouts и graceful shutdown;
- `envconfig` и startup validation;
- Zap logging в terminal и file;
- request ID, recovery, access log и tracing middleware;
- централизованный HTTP response/error mapper;
- PostgreSQL pool configuration;
- health/readiness;
- migration command;
- clock, ID, crypto и transaction ports;
- test database lifecycle.

### Frontend scope

- независимый `frontend/`;
- утверждённый package manager и lockfile;
- TypeScript strict mode;
- routing, error boundary и application shell;
- типизированный/generated API client strategy;
- единая обработка error envelope;
- memory-only auth state contract;
- browser test harness;
- baseline accessibility checks.

### CI scope

- formatting и lint;
- `go test ./...` и `go vet ./...`;
- frontend typecheck/lint/tests;
- migration smoke test;
- secret scanning;
- dependency vulnerability scanning;
- architecture import checks;
- Markdown link и Mermaid validation;
- reproducible container build без production secrets.

### Exit gate E1

- clean checkout запускается документированной командой;
- неправильная конфигурация вызывает fail-fast startup;
- health и readiness имеют различную семантику;
- graceful shutdown ограничен timeout и протестирован;
- CI проходит воспроизводимо;
- backend и frontend не имеют source-level cross-root imports;
- artifact не содержит секретов и локальных credentials.

## 7. E2 — database kernel и reliability primitives

### Цель

Доказать транзакционные примитивы до появления критических бизнес-операций.

### Entry criteria

- E1 закрыт;
- lock order, retry и outbox решения утверждены;
- Database Design синхронизирован с Domain Model.

### Scope

- базовые migrations identity, pharmacy, catalog, assortment, inventory, sales, returns, idempotency, audit и outbox;
- ID policy;
- foreign keys, unique и check constraints;
- runtime/migration DB roles;
- append-only protection audit и movements;
- Unit of Work без утечки `pgx.Tx` в application/domain;
- retry classifier и bounded retry;
- idempotency claim/complete/replay/conflict protocol;
- transactional audit writer;
- outbox writer, lease, retry и dead-letter policy;
- real `cmd/worker` bootstrap, fatal polling classification и graceful drain;
- deterministic `inventory_movements.lot_sequence` под lot lock;
- explicit runtime privilege matrix и composite audit session ownership;
- separate bounded outbox retention (`PROCESSED` 30 дней, `DEAD_LETTER` 180 дней);
- deterministic lock helpers;
- migration, failure-injection и concurrency harness.

### Обязательные evidence

- migration from zero;
- upgrade E1 schema `1` → current E2 schema `21` с immutable checksum history;
- rollback transaction function;
- panic внутри UoW;
- commit failure;
- serialization/deadlock retry;
- same-key same-payload replay;
- same-key different-payload conflict;
- audit insert failure → rollback;
- two-worker lease race;
- duplicate outbox delivery без duplicate business effect;
- runtime DB role не может изменять immutable rows штатным путём;
- mandatory PostgreSQL suites реально запускаются в CI без optional skip.

### Exit gate E2

- UoW гарантирует commit/rollback semantics;
- retry повторяет всю transaction function;
- idempotency выдерживает disconnect-after-commit;
- обязательный audit fail-closed;
- lock order опубликован и доказан race tests;
- outbox допускает at-least-once delivery без повторного эффекта;
- worker bootstrap, graceful drain, movement sequence, privilege matrix и outbox retention доказаны integration tests;
- migration verification queries определены.

### Запрещённый переход

Нельзя проводить receipts, sales, returns, write-offs или adjustments до закрытия E2.

## 8. E3 — identity, authentication и authorization

### Цель

Создать доверенный actor context и управляемое прекращение доступа.

### Scope

- создание пользователя администратором;
- user states `ACTIVE`, `BLOCKED`, `ARCHIVED`;
- role assignments;
- pharmacy assignments;
- password hashing и transparent rehash;
- login и generic failure response;
- signed access tokens;
- server-side refresh sessions;
- refresh rotation и reuse detection;
- logout current/all sessions;
- password change/reset;
- block/archive с отзывом sessions;
- assignment revoke;
- RBAC + resource scope policy layer;
- stale-sensitive revalidation внутри transaction;
- MFA/recovery для remote `ADMIN`;
- audit и security events.

### Frontend slice

- login;
- refresh cookie flow;
- access token только в памяти;
- logout и purge sensitive state;
- actor generation для защиты от stale responses;
- session expiry handling;
- route guards только как UX.

### Exit gate E3

- валидный JWT blocked user не даёт доступ;
- role/assignment revoke прекращает новые scoped mutations согласно SLA;
- два refresh одного generation не завершаются успешно;
- reuse отзывает family;
- unknown user и wrong password не различимы внешне;
- mass assignment роли невозможен;
- self-lockout и privilege escalation контролируются policy;
- security-critical flow покрыт integration, race и browser tests.

## 9. E4 — pharmacy и global catalog

### Цель

Создать управляемые аптеки и модерируемый глобальный каталог.

### Scope

Pharmacy:

- create/update разрешённых полей;
- block/archive;
- geo, address, contacts, working hours;
- state check во всех scoped operations;
- отсутствие physical delete для значимой истории.

Catalog:

- `Product` и `ProductPresentation`;
- barcode и нормализованные значения;
- moderation lifecycle;
- staging import job;
- quarantine storage;
- streaming parser с limits;
- validation findings;
- approve/reject/merge;
- safe export и formula-injection protection.

### Exit gate E4

- `PHARMACIST` не редактирует global catalog напрямую;
- import не публикует данные без moderation;
- malformed row не создаёт скрытый partial publish;
- duplicate rules защищены application и constraints;
- staging/internal metadata не попадает в public API;
- import worker restart безопасен.

## 10. E5 — assortment и pricing

### Цель

Создать pharmacy-local продаваемую позицию и authoritative pricing rules.

### Scope

- `PharmacyProduct`;
- enable/disable assortment;
- package и optional inner-unit price;
- base-units-per-package policy;
- minimum stock threshold;
- inner-unit sale permission;
- optimistic concurrency;
- price/rule history и audit.

### Exit gate E5

- frontend price не является authoritative total;
- изменение цены не переписывает historical snapshots;
- чужая pharmacy product недоступна;
- concurrent update не создаёт silent lost update;
- disabled product нельзя использовать в новой sale;
- audit содержит actor, old/new safe values и reason, где требуется.

## 11. E6 — receipts и inventory truth

### Цель

Создать первый полный inventory vertical slice и доказать stock truth.

### Entry criteria

- E2, E3, E4 и E5 закрыты;
- receipt sequence согласована с `10-sequence-diagrams.md`.

### Scope

- draft/posted receipt lifecycle;
- receipt lines и snapshots;
- `StockLot`;
- batch и expiration;
- initial quantity только через movement;
- immutable `InventoryMovement`;
- lot balance update;
- idempotent posting;
- reversal/compensation вместо изменения posted document;
- inventory history read model;
- pharmacist frontend workflow.

### Exit gate E6

- quantity не становится отрицательной;
- posted receipt нельзя update/delete;
- receipt, lots, movements, audit, outbox и idempotency result атомарны;
- network retry возвращает исходный result;
- reversal расширяет историю, а не переписывает её;
- concurrency tests подтверждают lock order;
- reconciliation query восстанавливает balance из movements;
- frontend завершает полный receipt workflow без ручного SQL.

## 12. E7 — sales и FEFO

### Цель

Реализовать главный revenue и stock-decrement сценарий без нарушения остатков.

### Scope

- create/post sale;
- package и inner-unit quantities;
- server-side totals;
- current assortment rules;
- FEFO allocation;
- исключение expired/quarantined/depleted lots;
- price и packaging snapshots;
- immutable allocations;
- decrement и SALE movements;
- idempotency, audit, outbox;
- receipt/print-friendly read model;
- pharmacist frontend sale workflow.

### Exit gate E7

- frontend не выбирает authoritative lot allocation;
- две продажи не списывают один stock дважды;
- eligibility и quantity перечитываются после lock;
- money/quantity overflow безопасно отклоняется;
- sale graph, balances, movements, audit, outbox и idempotency атомарны;
- public projection обновляется надёжным post-commit protocol;
- disconnect-after-commit не создаёт вторую sale;
- end-to-end sale проходит от UI до immutable history.

### Release gate RG1 — internal inventory alpha

После E7 разрешён ограниченный internal alpha только если:

- receipts и sales reconciled;
- нет P0;
- backup/restore development rehearsal успешен;
- audit и logs позволяют расследовать операцию;
- данные не считаются production legal record.

## 13. E8 — returns, write-offs, adjustments и reversals

### Entry criteria

- юридическая returns policy утверждена;
- E7 закрыт;
- elevated permissions определены.

### Returns scope

- return по original sale item/allocation;
- cumulative returned quantity;
- disposition;
- `RETURN_TO_STOCK` только по утверждённой policy;
- non-restocking return;
- compensating movements;
- idempotency, audit и concurrency control.

### Write-off/adjustment scope

- отдельные domain commands и documents;
- обязательный reason и allowlisted reason codes;
- elevated permission для risky adjustments;
- запрет generic `PATCH stock`;
- reversal/compensation;
- anomaly metrics.

### Exit gate E8

- return не изменяет original sale;
- cumulative returns не превышают allocation;
- concurrent returns не превышают sold quantity;
- adjustment всегда создаёт document, movement и audit;
- reason validation нельзя обойти;
- ADMIN не получает hidden bypass immutable history;
- reconciliation остаётся точной после compensation flows.

## 14. E9 — public search, alerts и replenishment

### Public search scope

- public projection;
- published catalog и active pharmacies;
- aggregated availability без internal quantity;
- freshness timestamp;
- safe geo sorting;
- pagination, rate limiting, caching, ETag;
- graceful degradation map provider.

### Alerts scope

- low stock;
- near expiry;
- expired lot;
- deduplication;
- acknowledge/resolve lifecycle;
- idempotent worker.

### Replenishment scope

- explainable recommendation;
- no automatic supplier order;
- read-model inputs;
- отсутствие mutation stock truth.

### Exit gate E9

- public projection не является command source;
- purchase prices и exact stock не раскрываются;
- projection lag измеряется;
- duplicate event не создаёт duplicate alert;
- projection/alert failure не откатывает committed sale;
- stale availability маркируется или исключается по policy;
- public API выдерживает abuse limits.

## 15. E10 — complete operational frontend

Frontend развивается с E1 и каждым vertical slice. E10 закрывает целостность пользовательских journeys, а не начинает frontend-разработку.

### Scope

- ADMIN: users, pharmacies, assignments, catalog moderation, sessions, audit;
- PHARMACIST: assortment, receipt, lots, sale, return, write-off, adjustment, alerts;
- PUBLIC/CLIENT: search, filters, map/list, freshness;
- loading/error/empty/offline-degraded states;
- keyboard navigation и accessibility;
- confirmation risky operations;
- duplicate-submit prevention;
- session expiry и authorization change handling;
- purge sensitive state;
- role и ownership browser E2E.

### Exit gate E10

- API client не расходится с API Design;
- critical commands используют `Idempotency-Key`;
- stale response не восстанавливает очищенное состояние;
- UI не показывает success до server confirmation;
- stable `error.code` локализуется frontend;
- every critical workflow имеет browser E2E;
- frontend build не содержит secrets;
- direct URL navigation не обходит backend authorization.

### Release gate RG2 — pilot candidate

После E10 можно собирать pilot candidate при отсутствии P0 и наличии pilot deployment, testing и observability baseline.

## 16. E11 — system hardening

E11 не добавляет обязательный MVP business scope. Он проверяет систему как единое целое.

### Security hardening

- threat-model review;
- MFA enforcement remote ADMIN;
- CORS, CSRF, CSP, HSTS, trusted proxies;
- rate limits и abuse controls;
- recovery/session adversarial tests;
- secret и key rotation drill;
- dependency/SBOM review;
- PostgreSQL privilege review;
- audit completeness review;
- backup access review.

### Reliability hardening

- concurrency suite under load;
- worker crash/restart;
- outbox backlog recovery;
- DB connection exhaustion;
- graceful shutdown during request/job;
- migration recovery rehearsal;
- retry-storm prevention;
- clock skew и expiry boundaries;
- disk-full/read-only dependency scenarios, где воспроизводимо.

### Performance baseline

Измеряются:

- password hashing capacity;
- sale transaction p50/p95/p99;
- FEFO query plan;
- public search p50/p95/p99;
- import throughput/memory;
- outbox/projection lag;
- pool saturation;
- table/index growth;
- frontend bundle и core workflow timings.

### Exit gate E11

- нет Critical/High security findings без approved exception;
- load не нарушает correctness;
- retry не создаёт duplicate effect;
- slow queries имеют решение или accepted limit;
- RPO/RTO подтверждены restore drill;
- runbooks воспроизводимы другим инженером;
- SLO baseline зафиксирован для production.

## 17. E12 — production readiness и pilot

### Production-readiness scope

- актуальные `12-deployment.md`, `13-testing-strategy.md`, `14-observability.md`;
- environment/configuration matrix;
- TLS и secret management;
- backup/restore automation;
- monitoring, alerts и routing;
- incident contacts и escalation;
- migration deploy procedure;
- rollback/forward-fix policy;
- audit/data retention;
- privacy/data-handling review;
- inventory reconciliation procedure;
- operator training;
- release checklist;
- disaster-recovery rehearsal;
- support and ownership model.

### Pilot preparation

- ограниченная pharmacy и user set;
- verified initial catalog;
- initial stock import через auditable operation;
- reconciliation до go-live;
- roles/assignments review;
- backup до cutover;
- support, rollback и incident flow rehearsed;
- pilot success/failure criteria утверждены.

### Pilot metrics

- unexplained stock variance;
- rejected/failed sales;
- adjustment frequency;
- latency/error rate;
- user errors;
- alert usefulness;
- projection freshness;
- security anomalies;
- support load;
- restore/recovery incidents.

### Pilot gate PG1 — start

Pilot начинается только если:

- нет P0;
- P1 имеют owner и не затрагивают correctness/security/data integrity;
- restore drill успешен;
- critical alerts доставляются;
- reconciliation baseline равен ожидаемому;
- release artifact immutable и проверен.

### Pilot gate PG2 — continue

Pilot приостанавливается при:

- необъяснимом stock divergence;
- authorization bypass;
- duplicate irreversible effect;
- невозможности audit reconstruction;
- backup/restore failure;
- повторяемом critical workflow failure.

### Pilot gate PG3 — production approval

Production разрешён только если:

- все P0/P1 correctness и security defects закрыты;
- pilot завершён по утверждённым критериям;
- inventory reconciliation не имеет необъяснимых расхождений;
- restore и incident drills успешны;
- audit восстанавливает actor, action, target, result и correlation;
- Product Owner принимает scope;
- Engineering/Security принимают технический риск;
- Operations принимает эксплуатационную готовность.

### Release gate RG3 — production release

- tag/commit immutable;
- deploy используется тот же artifact, что прошёл CI;
- migration compatibility проверена;
- release notes и rollback/forward-fix plan готовы;
- backup и restore point подтверждены;
- monitoring active до открытия traffic.

### Release gate RG4 — post-release verification

После deployment проверяются:

- migration version;
- readiness;
- login/refresh;
- role/assignment enforcement;
- receipt/sale smoke paths в безопасном режиме;
- outbox processing;
- error/latency/DB metrics;
- audit creation;
- backup schedule.

## 18. Параллельные workstreams

### W1 — Documentation and architecture governance

На каждом change set:

- HTTP feature обновляет API Design;
- schema меняет Database Design и migration docs;
- aggregate/transaction меняет Domain Model и Sequence Diagrams;
- auth/security меняет Security Design;
- package ownership меняет Project Structure;
- stage/gate/scope меняет Roadmap;
- архитектурное решение оформляется ADR.

### W2 — Testing

Каждый use case получает применимые уровни:

- domain unit;
- application;
- repository integration;
- authorization negative;
- idempotency;
- concurrency;
- API contract;
- migration;
- frontend/browser;
- failure injection.

Тесты добавляются вместе с feature, а не отдельной «фазой покрытия».

### W3 — Security

Security review обязателен при изменении:

- auth/session/recovery;
- role/assignment;
- public/private boundary;
- upload/export;
- ADMIN operation;
- audit;
- crypto/secrets;
- infrastructure privilege;
- dependency execution path.

### W4 — Observability

Каждая critical operation до merge определяет:

- structured logs;
- audit event;
- metrics;
- trace boundary;
- dashboard impact;
- alert condition и owner, если failure требует реакции.

### W5 — Deployment and migrations

Каждое schema/runtime изменение оценивает:

- backward/forward compatibility;
- lock duration и table rewrite;
- data backfill;
- expand/migrate/contract sequence;
- rollback или forward-fix;
- verification query;
- backend/frontend/migration deployment order.

### W6 — Data quality and reconciliation

С E6 непрерывно поддерживаются:

- balance-from-movements reconciliation;
- orphan/duplicate detection;
- invalid state transition checks;
- projection-vs-source drift checks;
- pilot discrepancy workflow.

### W7 — Product validation and usability

Начиная с первых frontend slices:

- workflow review с реальным pharmacist/admin;
- terminology validation;
- error-message clarity;
- package/inner-unit usability;
- accessibility;
- pilot feedback без обхода quality gates.

## 19. Definition of Ready для feature

Feature допускается к реализации, когда:

1. actor и business goal определены;
2. role/resource/pharmacy scope определены;
3. API contract описан или входит в change set;
4. aggregate owner и transaction boundary известны;
5. schema/migration impact понятен;
6. idempotency scope и fingerprint определены;
7. audit semantics определены;
8. lock order и race scenarios перечислены;
9. retry/partial-failure behaviour определено;
10. frontend workflow и states понятны;
11. observability requirements определены;
12. acceptance criteria проверяемы;
13. legal/security blockers отсутствуют или формально решены;
14. зависимый stage gate закрыт.

## 20. Definition of Done для feature

Feature завершена только если:

1. соответствует SRS и API Design;
2. business invariants находятся в Domain/Application;
3. authorization revalidates актуальный actor и resource scope;
4. critical mutation имеет idempotency;
5. transaction boundary и lock order согласованы;
6. обязательный audit атомарен с effect;
7. ошибки идентифицируются через `errors.Is()`/`errors.As()` и централизованно отображаются;
8. migrations/constraints добавлены и протестированы;
9. unit/integration/concurrency/contract/security/browser tests проходят по применимости;
10. frontend не дублирует server authority;
11. logs/metrics/traces не содержат secrets;
12. runbook/alert добавлены, если failure операционно значим;
13. документация обновлена в том же change set;
14. CI проходит на clean checkout;
15. нет P0/P1 в scope;
16. rollback/retry/partial failure проверены;
17. migration и previous-version compatibility проверены;
18. feature доступна только после готовности зависимостей.

## 21. Классификация дефектов

### P0 — немедленный release blocker

- потеря или незаметная порча inventory/history;
- отрицательный остаток;
- authorization/scope bypass;
- credential/secret disclosure;
- duplicate irreversible effect;
- изменение immutable records;
- невозможность restore;
- отсутствие обязательного audit;
- remote code execution или критическая supply-chain compromise.

P0 не допускает exception и останавливает pilot/production.

### P1 — обязательный до production

- существенная race condition;
- неверный public availability с бизнес-риском;
- broken session revocation SLA;
- отсутствующий critical incident signal;
- critical workflow требует небезопасный workaround;
- недетерминированная migration/deployment;
- High security finding без compensating control.

### P2 — допустим только при owner и safe workaround

- UX defect без риска данных;
- некритичная performance degradation;
- неполная аналитика;
- cosmetic inconsistency.

## 22. Release strategy

- trunk-based или short-lived branches;
- small vertical change sets;
- immutable tag/commit;
- signed image или проверяемый digest;
- один artifact проходит CI и deployment;
- release не rebuild-ится после approval;
- feature flags не обходят authorization/domain rules;
- schema rollback не выполняется, если способен повредить данные;
- irreversible migration требует forward-fix plan;
- production migration отделена от обычного application startup;
- deploy не продолжает traffic rollout при failed verification.

## 23. Метрики прогресса

Прогресс измеряется не количеством файлов или endpoint-ов, а:

- завершёнными vertical slices;
- закрытыми stage/release gates;
- acceptance criteria coverage;
- количеством P0/P1;
- migration reliability;
- concurrency/security evidence;
- lead time Ready → Done;
- escaped defects;
- reconciliation accuracy;
- outbox/projection lag;
- pilot incident rate;
- restore success rate;
- долей critical operations с полным observability contract.

Code coverage percentage не является самостоятельным доказательством качества.

## 24. Критический путь и допустимый параллелизм

```text
E0 Decisions
    ↓
E1 Foundation
    ↓
E2 Transaction & Reliability Kernel
    ↓
E3 Identity & Authorization
    ↓
E4 Pharmacy & Catalog
    ↓
E5 Assortment & Pricing
    ↓
E6 Receipts & Inventory Truth
    ↓
E7 Sales & FEFO
    ↓
E8 Returns / Write-offs / Adjustments
    ↓
E9 Search / Alerts / Replenishment
    ↓
E10 Complete Operational Frontend
    ↓
E11 System Hardening
    ↓
E12 Pilot & Production Readiness
```

Параллельно со всеми этапами выполняются W1–W7.

Допустим параллелизм:

- frontend shell и test harness во время E1;
- UI vertical slice после стабилизации конкретного API contract;
- deployment/observability baseline начиная с E1;
- public search projection infrastructure после outbox E2;
- alerts/replenishment read models после inventory truth E6.

Запрещён параллелизм несовместимых реализаций:

- двух UoW;
- двух auth/session models;
- двух idempotency protocols;
- разных lock orders для пересекающихся resources;
- прямой event publish и outbox для одного critical event;
- ручного frontend contract и generated contract без одного источника истины.

## 25. Remaining roadmap decisions

Gate E0 закрыт. Открыты только execution-level decisions, не допускающие альтернативную архитектуру:

1. pilot pharmacy и количественные pilot exit criteria;
2. SLO для critical API/worker pipelines;
3. ownership production operations, on-call и incident commander;
4. initial-stock cutover runbook;
5. public projection freshness target;
6. elevated approval model для особо рискованных ADMIN operations;
7. concrete infrastructure products при сохранении утверждённых RPO/RTO, retention, proxy и outbox contracts.
