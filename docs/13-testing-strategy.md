# PharmacyCRM — Testing Strategy

> E2 schema `23` integration coverage includes E1/19 upgrades, session-security negative constraints, API/worker privilege denial and capability-based outbox replay.

**Статус документа:** Draft  
**Версия:** 1.1  
**Дата:** 2026-07-21  
**Связанные документы:** `02-srs.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`

## 1. Назначение и нормативная роль

Документ определяет обязательную стратегию тестирования PharmacyCRM: test levels, test oracles, environments, PostgreSQL semantics, API compatibility, security, concurrency, migrations, workers, frontend workflows, deployment, backup и recovery.

Testing Strategy нормативна для backend, frontend, QA, security review, CI и release gates. Изменение transaction boundary, lock order, API contract, schema, authorization, idempotency, audit, worker protocol, deployment topology или recovery procedure обязано синхронизировать тесты и этот документ в том же change set.

Feature не считается завершённой, пока её критические инварианты не подтверждены воспроизводимым evidence: автоматическим тестом, migration rehearsal, restore drill, security review или иным заранее определённым проверяемым артефактом.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует зафиксированного риска.
- **Может** — допустимый вариант.

## 3. Цели тестирования

Тестирование должно доказывать, что система:

1. сохраняет доменные инварианты на success, failure, retry и race paths;
2. не допускает обход authentication, authorization и pharmacy scope;
3. не создаёт повторный необратимый эффект;
4. корректно работает при конкурентных транзакциях;
5. атомарно commit-ит или rollback-ит критические изменения;
6. сохраняет immutable inventory и audit history;
7. совместима с заявленными API, schema и worker protocol versions;
8. корректно переживает crash, restart, timeout, disconnect и partial failure;
9. не раскрывает secrets, credentials и внутренние данные;
10. предоставляет frontend стабильный контракт;
11. воспроизводимо deploy-ится и восстанавливается;
12. остаётся наблюдаемой при отказах.

## 4. Ключевые testing invariants

Следующие утверждения должны иметь автоматические test oracles:

1. `stock_lot.quantity_base_units >= 0` после любой завершённой операции.
2. Сумма подтверждённых inventory movements согласуется с materialized lot balance согласно утверждённой reconciliation formula.
3. Одна idempotency identity создаёт не более одного business effect.
4. Completed idempotency result никогда не существует без соответствующего committed effect.
5. Обязательный audit event существует для каждой успешной critical mutation.
6. Rollback не оставляет business rows, movements, audit, outbox или completed idempotency record.
7. Sale allocations не превышают доступное количество lot.
8. Cumulative returns не превышают исходные sale allocations.
9. Revoked session, role или assignment не разрешает новую protected mutation.
10. Outbox consumer может обрабатывать событие повторно без повторного business effect.
11. Immutable records нельзя штатно UPDATE/DELETE runtime role-ю.
12. Restore сохраняет inventory reconciliation и audit traceability.

Нельзя ограничиваться проверкой HTTP status. Критический integration test обязан проверять итоговое состояние всех затронутых таблиц и reconciliation oracle.

## 5. Основные принципы

### 5.1 Risk-based testing

Глубина тестирования определяется риском, а не размером diff.

Максимальная строгость обязательна для:

- identity и sessions;
- authorization и assignments;
- Unit of Work;
- idempotency;
- transactional audit;
- inventory movements;
- FEFO allocation;
- returns, write-offs и adjustments;
- migrations;
- outbox/workers;
- backup/restore;
- production configuration и secrets.

### 5.2 Поведение, а не private implementation

Тест проверяет внешний contract, state transition или invariant. Он не должен зависеть от private-функций, SQL formatting и случайного порядка вызовов.

Явно проверяются только нормативные протоколы: lock order, audit-before-commit, idempotency claim order, outbox atomicity и transaction retry boundary.

### 5.3 Реальный PostgreSQL

SQLite, in-memory map и SQL mock не доказывают PostgreSQL-specific semantics.

Реальный PostgreSQL обязателен для:

- constraints;
- isolation и visibility;
- row/advisory locks;
- `FOR UPDATE` и `SKIP LOCKED`;
- deadlocks и serialization failures;
- migrations;
- query plans;
- roles/permissions;
- immutable restrictions;
- backup/restore.

Тестовая major version PostgreSQL должна совпадать с production baseline.

### 5.4 Детерминированность

Тесты не должны зависеть от arbitrary `sleep`, wall clock, local timezone, package order или внешней сети.

Используются:

- controlled clock;
- deterministic IDs, где требуется;
- explicit barriers/channels;
- fixed random seeds;
- bounded polling с diagnostics;
- explicit timeouts;
- local controllable stubs.

### 5.5 Fail-closed environment

Test runner обязан завершиться ошибкой при:

- production-like host/database name без explicit allow flag;
- production credentials или PII;
- отсутствии test isolation marker;
- destructive cleanup вне test database;
- неконтролируемой внешней интеграции;
- отсутствующем timeout для integration/concurrency test.

## 6. Test suite classes

| Suite | Цель | Целевое время | Gate |
|---|---|---:|---|
| Fast | domain, application, frontend unit | минуты | каждый PR |
| Integration | PostgreSQL repositories, modules, HTTP contracts | минуты | каждый PR |
| Critical concurrency | изменённые race/lock scenarios | минуты | каждый PR |
| Full concurrency/security | полный набор adversarial scenarios | дольше | main/nightly |
| Browser E2E | role-based workflows | дольше | main/release |
| Migration/recovery | schema, deploy, restore | дольше | release candidate |
| Performance/capacity | baseline и regression | контролируемо | scheduled/release |

Один и тот же critical regression не должен существовать только в nightly suite. Минимальный репродуктор обязан выполняться в PR gate.

## 7. Пирамида тестирования

Уровни:

1. Domain unit.
2. Application component.
3. Repository integration.
4. Module integration.
5. HTTP contract.
6. Concurrency.
7. Security/adversarial.
8. Frontend component/contract.
9. Browser E2E.
10. Migration/deployment/recovery.
11. Performance/capacity.

E2E не заменяет lower-level tests. Unit tests не заменяют PostgreSQL tests.

## 8. Domain tests

Проверяются:

- валидность constructors;
- state transitions;
- Money/quantity overflow;
- package/base-unit conversion;
- immutable snapshots;
- posted document immutability;
- reversal как компенсация;
- cumulative return bounds;
- enum/reason validation;
- terminal states;
- `errors.Is()`/`errors.As()` semantics.

Domain tests не используют PostgreSQL, Gin, `pgx.Tx`, HTTP DTO или filesystem.

## 9. Application tests

Проверяются:

- authorization до mutation;
- stale-sensitive revalidation внутри transaction function;
- корректная UoW boundary;
- audit до commit;
- idempotency claim/complete/replay;
- отсутствие side effect до commit;
- bounded full-transaction retry;
- rollback при timeout/cancellation;
- error classification без string matching;
- server authority для price, total, lot и scope;
- отсутствие success result при failed commit.

Fake обязан поддерживать controlled error injection и не скрывать нарушение sequence.

## 10. Repository integration tests

Каждый PostgreSQL adapter проверяется на real schema и production-like role permissions.

Обязательны:

- mapping nullable/enum/value types;
- foreign keys, unique и check constraints;
- not-found semantics;
- optimistic version;
- deterministic ordering;
- cursor pagination;
- transaction visibility;
- lock behavior;
- query cancellation/timeouts;
- immutable restrictions;
- stable error translation;
- absence of unauthorized generic update/delete methods.

Driver errors не выходят как application/public errors.

## 11. Module integration tests

Для каждой critical command минимум проверяются:

1. success;
2. validation failure;
3. unauthenticated actor;
4. wrong role;
5. wrong pharmacy scope;
6. blocked/archived actor;
7. revoked assignment/session;
8. idempotent replay;
9. same key with different fingerprint;
10. audit failure;
11. repository failure до commit;
12. commit/retry failure;
13. disconnect-after-commit replay;
14. absence of partial writes;
15. reconciliation oracle.

После выполнения проверяются business rows, movements, audit, idempotency, outbox и materialized balances.

## 12. HTTP contract tests

Проверяются:

- method/URL;
- authentication/authorization;
- strict DTO;
- unknown fields;
- malformed/multiple JSON objects;
- content negotiation;
- body/header/query/path limits;
- success/error envelopes;
- stable `error.code`;
- request ID;
- pagination;
- idempotency headers;
- cache/security headers;
- `401/403/404` concealment policy;
- отсутствие SQL/stack/constraint leakage;
- CORS и preflight;
- compatibility с frontend client.

### 12.1 API compatibility

Для каждого breaking-risk change CI обязан сравнить current contract с предыдущей поддерживаемой version.

Проверяются:

- удалённые/переименованные поля;
- изменение type/nullability;
- новые required fields;
- удалённые enum values;
- изменённые status/error codes;
- изменённая authorization/side-effect semantics.

Новый backend должен пройти smoke с текущим frontend artifact, а новый frontend — с поддерживаемой backend version в пределах deployment compatibility window.

## 13. Authentication и sessions

Обязательны:

- indistinguishable invalid login response;
- dummy password verification path;
- inactive user login rejection;
- session+audit atomicity;
- JWT signature/algorithm/issuer/audience/expiry/nbf/claims;
- unknown `kid` rejection;
- hashed refresh storage;
- one-time rotation;
- concurrent refresh exclusion;
- family revoke on reuse;
- logout current/all;
- password change/reset revocation;
- key rotation overlap/retirement;
- ADMIN MFA/recovery;
- CSRF protection;
- cookie flags;
- clock skew boundaries.

## 14. Authorization matrix

Для каждого protected use case машинно проверяются:

- allowed role/scope;
- каждая denied role;
- other-pharmacy resource;
- missing/revoked assignment;
- inactive pharmacy;
- blocked/archived actor;
- self-target restrictions;
- last-admin protection;
- replay after revoke;
- revoke between precheck and mutation;
- forged resource ID;
- mass assignment attempt.

Frontend guards не являются security test.

## 15. Idempotency tests

Для каждой critical mutation:

- new key → один effect;
- same key/same payload → original safe result;
- same key/different payload → conflict;
- JSON key order не меняет fingerprint;
- scope/path/version входят в fingerprint;
- actor/operation/scope разделяют namespace;
- concurrent same-key requests не дублируют effect;
- rollback не оставляет completed record;
- disconnect after commit восстанавливается replay;
- replay revalidates authorization;
- retention semantics проверены;
- result недоступен другому actor;
- idempotency storage failure вызывает rollback.

## 16. Concurrency tests

Concurrency tests используют real PostgreSQL, independent connections и explicit synchronization.

Минимальный набор:

- sale vs sale за один lot;
- sale vs write-off;
- return vs return за одну allocation;
- concurrent refresh generation;
- concurrent active assignment create;
- two workers claim one job;
- concurrent migration lock;
- stale lease/fencing owner;
- price lost update;
- block/revoke vs protected mutation;
- retry after deadlock/serialization;
- lock-order cycle prevention.

Каждый тест обязан иметь bounded timeout и diagnostics:

- goroutine stack dump;
- `pg_stat_activity` snapshot;
- blocking/blocked PID;
- SQL state;
- transaction attempt number;
- relevant request/idempotency IDs.

Тест, который только редко воспроизводит race через цикл и `sleep`, недостаточен.

## 17. Inventory, sales и returns

### 17.1 Receipts

- initial stock только через movement;
- receipt/lots/movements/audit/idempotency atomicity;
- posted immutability;
- compensating reversal;
- quantity/expiration/batch validation;
- network retry не удваивает stock;
- reconciliation после receipt/reversal.

### 17.2 Sales

- FEFO deterministic ordering;
- expired/quarantined/depleted exclusion;
- base-unit conversion;
- server total;
- price/presentation snapshots;
- no negative stock;
- immutable allocations/movements;
- no oversell;
- overflow rejection;
- transactional outbox;
- reconciliation после sale.

### 17.3 Returns

- original allocation linkage;
- cumulative bound;
- legal disposition policy;
- safe RETURN_TO_STOCK lot/movement;
- non-restocking does not increase stock;
- concurrent return bound;
- original sale immutable;
- reconciliation после return.

### 17.4 Adjustments/write-offs

- no generic stock patch;
- mandatory allowlisted reason;
- elevated permission;
- document+movement+audit atomicity;
- non-negative result;
- compensation instead of deletion;
- anomaly metric emission.

## 18. Worker и outbox

Проверяются:

- outbox atomicity;
- lease/lock protocol;
- duplicate delivery safety;
- crash before side effect;
- crash after side effect before acknowledge;
- lease expiry recovery;
- stale fencing rejection;
- bounded retry/backoff;
- poison event dead-letter;
- audited manual replay;
- graceful shutdown;
- protocol mismatch readiness failure;
- backlog/lag metrics;
- worker version compatibility during rolling deployment.

## 19. Migration tests

Каждая migration проверяется:

1. на пустой БД;
2. во всей chain;
3. на representative data volume;
4. с old/new application compatibility;
5. на безопасный retry, где применимо;
6. verification queries и негативные tests, которые удаляют critical identity index/outbox constraint либо required runtime grant и требуют отказа migration runner;
7. rollback/forward-fix rehearsal;
8. runtime role permissions;
9. readiness compatibility;
10. отсутствие truncation/overflow;
11. duplicate/dirty input handling при backfill;
12. cancellation и restart semantics.

Для large/destructive migrations измеряются lock duration, WAL, disk, backfill rate и abort threshold.

Contract migration нельзя merge-ить, пока telemetry/evidence не подтверждает отсутствие старых consumers.

## 20. Frontend testing

### 20.1 Component

- form/state behavior;
- loading/empty/error/success;
- error-code localization;
- duplicate submit prevention;
- accessibility/keyboard;
- dangerous action confirmation;
- sensitive state cleanup;
- stale response generation guard;
- server authority preservation.

### 20.2 Contract integration

- typed DTO compatibility;
- envelopes;
- refresh behavior;
- idempotency key generation/persistence per logical submit;
- pagination;
- cancellation;
- unknown response fields;
- breaking contract detection.

### 20.3 Browser E2E

Role-based workflows:

- ADMIN users/pharmacies/assignments/catalog;
- PHARMACIST assortment/receipt/sale/return/adjustment;
- public search/map/freshness;
- forbidden cross-pharmacy access;
- session expiry/logout;
- late response after logout;
- duplicate click/retry;
- page reload during command outcome uncertainty.

External map/provider uses controllable stub or graceful fallback.

## 21. Security и adversarial tests

Проверяются:

- malformed/oversized input;
- unknown fields;
- injection payloads;
- XSS/CSV formula injection;
- path traversal;
- MIME/magic-byte mismatch;
- archive/decompression limits;
- CORS reflection;
- CSRF;
- spoofed proxy headers;
- secret/log redaction;
- rate-limit bypass;
- user enumeration;
- JWT algorithm confusion;
- session fixation/reuse;
- IDOR;
- public data leakage;
- admin escalation/last-admin;
- dependency/secret scans.

Critical/High findings блокируют release без approved exception; P0 exceptions запрещены.

## 22. Fault injection и resilience tests

Для critical paths обязателен контролируемый fault injection:

- DB connection drop до commit;
- ambiguous client disconnect после commit;
- commit error;
- audit insert failure;
- outbox insert failure;
- worker crash в каждой значимой фазе;
- storage unavailable during import;
- PostgreSQL restart;
- pool exhaustion;
- disk/volume near-full signal;
- telemetry backend unavailable.

Fault injection не должен выполняться против production. Результат обязан подтверждать rollback, retry, replay или безопасную degradation semantics.

## 23. Property-based и fuzz testing

Применяется для DTO/JSON, IDs, barcode/name normalization, Money/quantity, conversion, cursors, fingerprint canonicalization, import parsers, error mapper, dates и token parsing.

Crash, panic, invariant violation или resource exhaustion превращается в deterministic regression test. Seed и corpus сохраняются.

## 24. Performance и capacity

Измеряются login/refresh, sale latency, FEFO plans/locks, search, imports, outbox lag, pool saturation, DB CPU/IO/WAL, backup/restore и frontend performance.

Нагрузочный тест одновременно проверяет correctness oracles:

- no negative stock;
- no duplicate effect;
- complete audit;
- idempotency consistency;
- projection reconciliation;
- bounded lock wait/retry rate.

Хорошая latency при нарушении invariant означает failed test.

## 25. Recovery и deployment

До pilot/production проверяются:

- clean deployment;
- same digest staging/production path;
- incompatible schema readiness failure;
- worker protocol mismatch;
- graceful shutdown API/worker;
- migration failure stops rollout;
- compatible app rollback;
- forward-fix;
- PostgreSQL restart;
- backlog recovery;
- backup freshness/integrity;
- isolated restore;
- inventory reconciliation;
- audit traceability;
- initial catalog/stock cutover rehearsal;
- post-deploy smoke;
- mixed-version compatibility window.

## 26. Test data strategy

Данные synthetic, reproducible, minimal but realistic, isolated и явно scoped к actor/pharmacy.

Builders выражают бизнес-намерение. Generic fixtures, обходящие constraints, запрещены, кроме corruption/migration tests.

Production data допускаются только в отдельно утверждённом anonymization process. Простое маскирование нескольких полей не считается достаточным.

## 27. Isolation и cleanup

Допустимы:

- ephemeral database per process;
- schema/database namespace per suite;
- rollback для single-connection tests;
- explicit reset/truncate;
- unique IDs для parallel tests.

Concurrency tests не оборачиваются одной внешней transaction. Cleanup failure падает тестом. Parallel tests обязаны иметь documented resource ownership.

## 28. Test doubles

Допустимы fake clock/IDs/crypto, controlled HTTP provider stub и in-memory publisher для narrow unit boundary.

Mock не заменяет PostgreSQL для UoW, locks, constraints, idempotency concurrency, outbox claim, migrations и roles.

Все важные ports должны поддерживать deterministic error injection.

## 29. CI gates

### 29.1 PR

- format/lint/static checks;
- backend/frontend fast tests;
- PostgreSQL integration;
- race detector для database/reliability packages с обязательными PostgreSQL DSN;
- HTTP contracts;
- relevant migrations;
- changed critical concurrency/security regressions;
- architecture/docs checks;
- secret/dependency scans.

Mandatory PostgreSQL CI gate запускает без skip:

- `internal/platform/database -run Integration`;
- `internal/platform/migration -run Integration` (включая E1 `1` → E2 `21`);
- `internal/modules/reliability/infrastructure/postgres -run Integration`;
- `internal/modules/audit/infrastructure/postgres -run Integration`;
- `internal/orchestration/outboxreplay/postgres -run Integration`;
- `internal/testkit/reconciliation -run Integration`;
- `internal/testkit/schema -run Integration`.

### 29.2 Main/nightly

- full concurrency;
- race detector;
- fuzz corpus;
- browser E2E;
- full migration chain;
- query-plan regression;
- worker crash/recovery;
- extended security;
- performance smoke;
- reconciliation suite.

### 29.3 Release candidate

- all suites;
- production-like migration rehearsal;
- deployment/rollback/forward-fix;
- restore drill within RTO;
- reconciliation;
- post-deploy smoke;
- security review;
- capacity baseline;
- no P0/P1 blockers.

## 30. Failure evidence

При падении integration/concurrency/E2E test CI сохраняет:

- commit SHA и test seed;
- relevant logs with request/trace IDs;
- DB schema version;
- PostgreSQL version;
- sanitized `pg_stat_activity`/lock snapshot;
- browser screenshot/trace, где применимо;
- migration output;
- test data identifiers;
- retry attempt information.

Evidence не должно содержать secrets или raw credentials.

## 31. Flaky tests

Flaky test — defect.

- infinite rerun запрещён;
- flaky test имеет owner/root-cause task;
- quarantine временная и visible;
- P0/P1 coverage не quarantine-ится;
- flaky rate измеряется;
- release suite не считается green из-за случайного успешного rerun.

Тест возвращается из quarantine только после доказанного deterministic fix.

## 32. Coverage и mutation testing

Coverage — diagnostic, не доказательство.

Mutation testing особенно важно для Money/quantity, authorization, FEFO, fingerprint, transitions и error mapping.

Выжившая mutation в critical invariant означает test gap и блокирует закрытие соответствующего gate.

## 33. Traceability

Для critical feature должна существовать связь:

```text
SRS requirement
  → API/domain/database rule
  → sequence/failure scenario
  → test case ID
  → CI/release evidence
```

Test case IDs или machine-readable metadata должны позволять найти проверку требования без поиска по всему repository.

Release evidence содержит commit/digest, suite versions, migration result, security result, performance baseline, restore/reconciliation result и accepted risks.

## 34. Definition of Ready

Feature готова к тестированию, когда:

1. acceptance criteria определены;
2. actor/role/scope известны;
3. API contract определён;
4. transaction/lock order определены;
5. idempotency/audit requirements определены;
6. failure/retry/race scenarios перечислены;
7. migration impact понятен;
8. test data/environment известны;
9. observability signals определены;
10. test oracle для critical invariants определён;
11. blockers закрыты или явно отмечены.

## 35. Definition of Done

Testing завершён, если:

1. domain/application behavior покрыто;
2. persistence проверена на real PostgreSQL;
3. HTTP и version compatibility проверены;
4. authorization matrix включает negative/race cases;
5. idempotency проверена;
6. shared state имеет deterministic concurrency tests;
7. audit failure/rollback проверены;
8. migrations/constraints проверены;
9. frontend workflow/stale state проверены;
10. security/fault-injection cases проверены;
11. reconciliation oracle проходит;
12. logs/responses не раскрывают secrets;
13. CI стабильна;
14. critical defects имеют regression tests;
15. failure evidence доступно;
16. documentation синхронизирована;
17. нет P0/P1 test gaps.

## 36. Минимальная матрица изменений

| Изменение | Обязательные тесты |
|---|---|
| Domain rule | unit + application regression + property where useful |
| HTTP endpoint | contract + auth + envelope + client compatibility |
| DB schema | migration + constraints + compatibility + verification |
| Critical command | integration + idempotency + audit rollback + concurrency + reconciliation |
| Auth/session | adversarial + concurrent refresh + browser cleanup |
| Worker/outbox | lease + crash + duplicate + fencing + protocol version |
| Frontend workflow | component + contract + E2E + stale response |
| Deployment config | startup validation + readiness + smoke + rollback |
| Backup/recovery | restore + reconciliation + audit verification |
| Performance | load + correctness oracle + resource limits |

## 37. Запрещённые практики

Запрещено:

- считать happy path достаточным;
- заменять PostgreSQL mock/SQLite;
- использовать arbitrary sleep для synchronization;
- отключать critical test без risk record;
- доказывать correctness coverage percentage;
- тестировать authorization только UI-ем;
- хранить real credentials/PII;
- зависеть от test order;
- использовать shared mutable globals без reset;
- игнорировать goroutine/transaction leaks;
- сравнивать errors по тексту;
- destructive cleanup без guard;
- release при ambiguous migration/restore result;
- скрывать flaky test автоматическим бесконечным rerun;
- использовать success HTTP status как единственный oracle critical mutation.

## 38. Открытые решения

До production необходимо утвердить:

1. backend test/assertion libraries;
2. frontend test runner и E2E framework;
3. CI PostgreSQL provisioning;
4. fuzz corpus storage;
5. mutation tooling/frequency;
6. performance environment и SLO;
7. DAST tooling;
8. production-like migration dataset;
9. automation, dataset sizing и evidence format для утверждённого quarterly restore drill;
10. flaky triage ownership;
11. test case traceability format;
12. failure evidence retention;
13. anonymization process;
14. reconciliation oracle implementation;
15. chaos/fault injection tooling.

## Обязательная consistency/Gate E0 regression matrix
Automated suites должны доказать:
1. module/import ownership и отсутствие отдельных `import`, `receipt`, `adjustments` modules;
2. `pharmacy_assignments` изменяются только Pharmacy owner contract;
3. critical mutation выполняет idempotency claim → authorization revalidation → canonical business locks;
4. replay после block/role revoke/assignment end не раскрывает сохранённый result;
5. FEFO lock/allocation order включает `expiration_date`, `received_at`, `id`;
6. API contract использует только нормативные paths и UUID examples;
7. persisted ImportJob states и ReturnAction/Sale status enum совпадают с DB/domain/API;
8. event catalog не допускает незарегистрированные posting aliases или generic reversal event с неоднозначной семантикой;
9. audit/outbox/idempotency failure откатывает business effect;
10. two-worker claim race, stale lease completion, retry/backoff и dead-letter;
11. DB retry выполняет whole transaction максимум три попытки только для `40001`/`40P01`;
12. Argon2id verify/rehash, refresh rotation/reuse-family revoke и all-session invalidation;
13. access token отсутствует в local/session storage, refresh token недоступен JavaScript;
14. untrusted forwarded headers, disallowed CORS origin и CSRF request отклоняются;
15. pnpm lockfile/API generation reproducibility и generated-client diff gate;
16. backup restore достигает RPO/RTO target на rehearsal dataset;
17. customer-returned medicine не переходит в sellable stock;
18. schema, fixtures и migrations не содержат скрытого дополнительного поля для инвалидирования доступа.
