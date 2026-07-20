# PharmacyCRM — Testing Strategy

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-20  
**Связанные документы:** `02-srs.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`

## 1. Назначение и нормативная роль

Документ определяет обязательную стратегию тестирования PharmacyCRM: уровни тестов, границы ответственности, тестовые окружения, правила работы с PostgreSQL, проверки контрактов, безопасности, конкурентности, миграций, frontend workflows, deployment и recovery.

Testing Strategy является нормативной для backend, frontend, QA, security review, CI и release gates. Изменение transaction boundary, lock order, API contract, schema, authorization, idempotency, audit, worker protocol, deployment topology или recovery procedure должно синхронизировать соответствующие тесты и этот документ в том же change set.

Тесты не заменяют корректный дизайн. Однако feature не считается завершённой, пока её критические инварианты не подтверждены воспроизводимыми автоматическими проверками и, где требуется, rehearsal/runbook evidence.

## 2. Цели тестирования

Тестирование должно доказывать, что система:

1. сохраняет доменные инварианты при обычных и ошибочных сценариях;
2. не допускает обход authentication, authorization и pharmacy scope;
3. не создаёт повторный необратимый эффект при retry/replay;
4. корректно работает при конкурентных транзакциях;
5. commit-ит или rollback-ит критические изменения атомарно;
6. сохраняет immutable inventory и audit history;
7. совместима с заявленной schema и worker protocol version;
8. корректно переживает crash, restart, network failure и partial failure;
9. не раскрывает secrets, credentials и внутренние данные;
10. предоставляет frontend стабильный HTTP-контракт;
11. может быть развернута, восстановлена и проверена воспроизводимо;
12. остаётся наблюдаемой при отказах.

## 3. Основные принципы

### 3.1 Risk-based testing

Глубина тестирования определяется не размером функции, а риском её ошибки.

Наиболее строгая проверка обязательна для:

- identity и sessions;
- authorization и assignments;
- Unit of Work;
- idempotency;
- transactional audit;
- immutable inventory movements;
- FEFO и stock allocation;
- returns/write-offs/adjustments;
- migrations;
- outbox/workers;
- backup/restore;
- production configuration и secrets.

### 3.2 Проверка поведения, а не реализации

Тест должен проверять внешний контракт или бизнес-инвариант. Он не должен быть хрупко привязан к внутренним private-функциям, SQL formatting или случайному порядку вызовов, если этот порядок не является нормативным.

Исключение: lock order, transaction sequence, audit-before-commit и другие архитектурно значимые протоколы могут проверяться явно.

### 3.3 Реальный PostgreSQL для persistence semantics

SQLite, in-memory map и mock SQL не являются доказательством корректности PostgreSQL-specific поведения.

Реальный PostgreSQL обязателен для проверки:

- constraints;
- transaction isolation;
- row locks;
- `FOR UPDATE` / `SKIP LOCKED`;
- deadlocks и serialization failures;
- advisory locks;
- migration behavior;
- query plans;
- roles/permissions;
- immutable table restrictions;
- backup/restore.

### 3.4 Детерминированность

Тесты не должны зависеть от случайных `sleep`, локальной timezone, порядка выполнения пакетов или реального wall clock.

Используются:

- fake/controlled clock;
- deterministic ID generator, где это полезно;
- explicit synchronization barriers для concurrency tests;
- fixed seeds для property/fuzz tests, если failure нужно воспроизвести;
- bounded polling с диагностикой вместо произвольного sleep.

### 3.5 Fail-closed test environment

Тестовое окружение должно завершаться ошибкой при:

- подключении к production-like host без явного разрешения;
- использовании production database name/credentials;
- отсутствии isolation marker;
- попытке destructive cleanup вне тестовой БД;
- обнаружении real secrets или production PII в fixtures.

## 4. Пирамида тестирования

Используются следующие уровни:

| Уровень | Основная цель | Скорость | Реальные зависимости |
|---|---|---:|---|
| Domain unit | инварианты value objects и aggregates | высокая | нет |
| Application unit/component | orchestration и policies | высокая | узкие fakes/ports |
| Repository integration | SQL, mappings, constraints, locks | средняя | PostgreSQL |
| Module integration | use case + UoW + repositories | средняя | PostgreSQL |
| HTTP contract | routing, DTO, envelope, status codes | средняя | app + test server |
| Concurrency | race/lock/retry correctness | ниже | PostgreSQL |
| Security/adversarial | abuse и boundary failures | средняя/ниже | app + PostgreSQL/browser |
| Frontend component | UI state и user interactions | высокая | mocked contract boundary |
| Browser E2E | полный user workflow | ниже | frontend + backend + PostgreSQL |
| Migration/deployment/recovery | эксплуатационная корректность | ниже | production-like environment |
| Performance | latency/capacity без нарушения correctness | ниже | controlled environment |

E2E не заменяет unit/integration tests. Unit tests не заменяют PostgreSQL concurrency и migration tests.

## 5. Backend domain tests

Domain tests проверяют:

- конструкторы создают только валидные сущности;
- недопустимые state transitions отклоняются;
- Money и quantity operations проверяют overflow;
- package/inner-unit conversion корректна;
- snapshots не изменяются задним числом;
- posted document не редактируется как draft;
- reversal создаёт компенсацию, а не переписывает историю;
- cumulative return не превышает исходную allocation;
- invalid reason/status/enum отклоняются;
- ARCHIVED является терминальным состоянием, где это определено;
- domain errors корректно распознаются через `errors.Is()`/`errors.As()`.

Domain tests не используют PostgreSQL, Gin, `pgx.Tx`, HTTP DTO или filesystem.

## 6. Application tests

Application tests проверяют orchestration:

- authorization вызывается до business mutation;
- stale-sensitive state повторно проверяется внутри transaction function;
- UoW охватывает все обязательные writes;
- audit записывается до commit;
- idempotency claim/complete/replay protocol соблюдается;
- post-commit side effects не выполняются до commit;
- non-retryable errors не повторяются;
- retryable transaction повторяется целиком;
- cancellation/timeout приводят к rollback;
- mapping domain/repository errors не зависит от строкового сравнения;
- frontend-supplied price/lot/total не становится authoritative.

Fakes должны моделировать только contract порта. Нельзя писать fake, который автоматически скрывает важные ошибки порядка вызовов или транзакций.

## 7. Repository integration tests

Каждый PostgreSQL adapter проверяется на реальной schema.

Обязательны:

- create/read/update только разрешённых mutable entities;
- nullable и enum mapping;
- foreign key behavior;
- unique/check constraints;
- not-found semantics;
- optimistic version checks;
- deterministic ordering;
- pagination/cursor correctness;
- transaction visibility;
- row lock behavior;
- immutable history restrictions;
- runtime role permissions;
- query cancellation и timeout handling;
- PostgreSQL errors преобразуются в стабильные repository/domain errors.

Тест не должен напрямую утверждать driver error как внешний application error.

## 8. Module integration tests

Module integration test запускает реальный use case с настоящими UoW и repositories.

Для каждой критической команды проверяются минимум:

1. success path;
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
13. network-style client retry после успешного commit;
14. отсутствие partial writes после rollback.

После теста проверяется не только response, но и состояние всех затронутых таблиц: business records, movements, audit, idempotency и outbox.

## 9. HTTP contract tests

HTTP contract tests должны подтверждать `05-api-design.md`.

Проверяются:

- method и URL;
- authentication requirement;
- authorization result;
- request DTO;
- unknown fields;
- malformed JSON;
- multiple JSON objects;
- unsupported content type/accept;
- body/header/query/path limits;
- success envelope;
- error envelope;
- stable `error.code`;
- `meta.request_id`;
- pagination;
- `Idempotency-Key` semantics;
- cache headers;
- `401` vs `403` vs concealment `404` policy;
- no internal SQL/stack/constraint leakage;
- `Cache-Control: no-store` для auth/admin/sensitive responses;
- CORS/preflight behavior;
- security headers, где они принадлежат приложению.

Backend contract и frontend generated/typed client должны проверяться на расхождение автоматически.

## 10. Authentication и session tests

Обязательные сценарии:

- неизвестный login и неверный password имеют одинаковый внешний ответ;
- password hash verification использует dummy path для отсутствующего user;
- blocked/archived user не создаёт session;
- login success невозможен без сохранённых session и audit;
- access token проверяет signature, algorithm, issuer, audience, expiry, nbf и claims format;
- неизвестный `kid` отклоняется;
- refresh token хранится только как hash;
- refresh rotation одноразова;
- два параллельных refresh одного generation не успешны одновременно;
- reuse отзывает всю token family;
- expired/revoked session отклоняется;
- logout current/all sessions работает атомарно;
- password reset/change отзывает требуемые sessions;
- JWT key rotation принимает допустимый overlap и отклоняет retired key после policy window;
- ADMIN MFA и recovery codes проверяются согласно ADR;
- refresh/logout CSRF protection работает;
- cookie flags соответствуют environment policy.

## 11. Authorization tests

Authorization matrix должна быть машинно проверяемой.

Для каждого protected use case проверяются:

- разрешённая роль и scope;
- каждая запрещённая роль;
- ресурс другой аптеки;
- отсутствующий assignment;
- revoked assignment;
- inactive pharmacy;
- blocked/archived actor;
- self-target restrictions;
- last-admin protection;
- replay после отзыва доступа;
- role/assignment revoked между предварительной проверкой и mutation;
- forged resource ID;
- mass assignment попытка через неизвестные/запрещённые поля.

Frontend route guards не считаются authorization test.

## 12. Idempotency tests

Для каждой критической mutation обязательны:

- новый key выполняет effect один раз;
- тот же key + тот же semantic payload возвращает исходный safe result;
- тот же key + другой payload возвращает conflict;
- JSON key order и transport-only поля не меняют fingerprint;
- effective pharmacy/resource scope входит в fingerprint;
- actor/operation/scope разделяют namespace;
- concurrent requests с одним key не выполняют effect дважды;
- rollback не оставляет ложный completed result;
- disconnect после commit безопасно восстанавливается replay-ом;
- replay повторно проверяет текущую authorization;
- retention/expiry idempotency record соответствует policy;
- sensitive result snapshot не раскрывается другому actor.

## 13. Concurrency tests

Concurrency tests используют explicit barriers/channels и реальный PostgreSQL.

Минимальный набор:

- две продажи конкурируют за один lot;
- продажа и write-off конкурируют за один lot;
- два возврата конкурируют за одну sale allocation;
- два refresh используют один token generation;
- два assignment create конкурируют за unique active assignment;
- два worker-а claim-ят одну outbox job;
- два migration processes конкурируют за migration lock;
- stale worker с истёкшим lease не может завершить job после нового owner;
- simultaneous price updates не создают lost update;
- block/revoke конкурирует с protected mutation;
- deadlock/serialization retry не создаёт duplicate effect;
- deterministic lock order предотвращает известные циклы.

Тест обязан иметь bounded timeout и диагностировать зависшие goroutines, locks и SQL queries.

## 14. Inventory, sales и returns tests

### 14.1 Receipts

- initial stock создаётся только через movement;
- receipt, lines, lots, movements, audit и idempotency атомарны;
- posted receipt immutable;
- reversal создаёт compensating movements;
- malformed quantity/expiration/batch отклоняется;
- network retry не удваивает stock.

### 14.2 Sales

- FEFO выбирает earliest eligible expiration;
- expired/quarantined/depleted lot исключается;
- quantity пересчитывается в base units;
- server рассчитывает total;
- price snapshot сохраняется;
- stock не становится отрицательным;
- allocations и movements immutable;
- concurrent sale не oversell-ит;
- overflow money/quantity отклоняется;
- outbox event создаётся в той же transaction.

### 14.3 Returns

- return привязан к исходной sale allocation;
- cumulative quantity не превышает sold quantity;
- disposition применяется согласно legal policy;
- RETURN_TO_STOCK создаёт допустимый lot/movement;
- non-restocking return не увеличивает stock;
- concurrent returns не превышают allocation;
- return не изменяет original sale history.

### 14.4 Adjustments/write-offs

- generic stock patch отсутствует;
- reason обязателен и валидируется;
- elevated permission применяется;
- document + movement + audit атомарны;
- negative resulting balance запрещён;
- reversal не удаляет историю.

## 15. Worker и outbox tests

Проверяются:

- outbox write атомарен с business transaction;
- worker claim использует lease/lock protocol;
- at-least-once delivery не создаёт duplicate projection/effect;
- crash до side effect допускает retry;
- crash после side effect до mark-processed не создаёт duplicate effect;
- lease expiry позволяет recovery;
- stale fencing token отклоняется;
- bounded retry и backoff работают;
- poison event переходит в dead-letter/failed state;
- manual replay audit-ится;
- worker shutdown не claim-ит новую работу;
- protocol version mismatch блокирует readiness/processing;
- projection lag и backlog измеримы.

## 16. Migration tests

Каждая migration проверяется:

1. на пустой database;
2. в последовательности со всеми предыдущими migrations;
3. на production-like volume/data shape, если есть риск lock/rewrite;
4. на совместимость с old и new application version в expand/migrate/contract window;
5. на повторный безопасный запуск, где это применимо;
6. verification queries;
7. rollback или forward-fix rehearsal;
8. runtime role permissions после применения;
9. schema version/readiness compatibility;
10. отсутствие silent data truncation/overflow.

Для destructive/large migrations дополнительно измеряются:

- lock duration;
- WAL growth;
- disk growth;
- backfill rate;
- cancellation behavior;
- abort threshold;
- recovery procedure.

`down` migration не считается обязательной, если она небезопасна; в этом случае тестируется forward-fix plan.

## 17. Frontend tests

### 17.1 Unit/component

Проверяются:

- rendering states;
- form validation;
- stable error-code mapping;
- loading/empty/error/success states;
- duplicate submit prevention;
- accessibility basics;
- keyboard navigation;
- confirmation опасных действий;
- state cleanup при logout/session expiry;
- stale response не восстанавливает очищенные sensitive данные;
- prices/lots/totals не становятся frontend authority.

### 17.2 Contract integration

Frontend API layer проверяет:

- generated/typed DTO compatibility;
- envelopes;
- auth refresh behavior;
- `Idempotency-Key` для critical commands;
- pagination/cursors;
- cancellation через `AbortController` или generation guard;
- unknown response fields не ломают клиента;
- incompatible breaking contract обнаруживается в CI.

### 17.3 Browser E2E

Минимальные role-based workflows:

- ADMIN login, user/pharmacy/assignment management;
- PHARMACIST login и pharmacy scope;
- catalog moderation;
- assortment/pricing;
- receipt → lot → inventory history;
- sale → FEFO decrement;
- permitted return/write-off/adjustment;
- logout/session expiry;
- public search list/map/freshness;
- forbidden cross-pharmacy access;
- late response после logout;
- duplicate click/retry.

E2E использует synthetic data и не зависит от внешней map service availability без controllable stub/fallback.

## 18. Security и adversarial tests

Обязательны проверки:

- malformed/oversized requests;
- unknown JSON fields;
- path/query/header injection;
- SQL injection payloads;
- XSS payloads в catalog/import/user-controlled fields;
- CSV formula injection;
- path traversal filename;
- MIME/magic-byte mismatch;
- decompression/row-count limits;
- CORS origin reflection;
- CSRF для cookie-auth endpoints;
- spoofed forwarded headers вне trusted proxy;
- auth header/cookie/log redaction;
- rate limit bypass attempts;
- user enumeration;
- token algorithm confusion;
- session fixation/reuse;
- IDOR/cross-pharmacy access;
- public endpoint data leakage;
- admin self-escalation/last-admin removal;
- secret scanning и dependency vulnerability checks.

Security findings Critical/High блокируют release без утверждённого исключения; P0 исключений не допускает.

## 19. Property-based и fuzz testing

Fuzz/property tests применяются для:

- JSON decoding и strict DTO validation;
- IDs, barcodes, dosage/product names;
- Money/quantity arithmetic;
- package/base-unit conversion;
- cursor parsing;
- semantic fingerprint canonicalization;
- import parsers;
- HTTP error mapper;
- date/time boundaries;
- token parsing.

Любой найденный crash, panic, invariant violation или resource exhaustion превращается в regression test.

## 20. Performance и capacity tests

Performance tests выполняются после фиксации correctness baseline.

Измеряются:

- password hashing throughput;
- login/refresh latency;
- sale transaction p50/p95/p99;
- FEFO query plan и lock wait;
- public search latency;
- import throughput и memory;
- outbox lag;
- pool saturation;
- DB CPU/IO/WAL;
- backup/restore duration;
- frontend bundle/load performance.

Нагрузочный тест обязан одновременно проверять correctness counters: отрицательный stock, duplicate effect, потерянный audit, failed idempotency и projection divergence.

Оптимизация, нарушающая инварианты, считается failed test независимо от latency.

## 21. Recovery и deployment tests

До pilot/production проверяются:

- clean environment deployment;
- staging deployment тем же image digest;
- readiness при несовместимой schema;
- worker protocol mismatch;
- graceful API shutdown во время request/transaction;
- worker shutdown во время lease;
- migration failure и stop-rollout;
- application rollback при совместимой schema;
- forward-fix при необратимой migration;
- PostgreSQL restart;
- connection exhaustion;
- outbox backlog recovery;
- backup creation/freshness;
- restore в изолированное окружение;
- inventory reconciliation после restore;
- audit completeness после restore;
- initial catalog/stock cutover rehearsal;
- post-deploy smoke suite.

## 22. Test data strategy

Тестовые данные должны быть:

- synthetic;
- минимальными, но доменно реалистичными;
- независимыми между tests;
- reproducible;
- без production credentials/PII;
- явно привязанными к pharmacy/actor scope.

Используются builders/factories, выражающие бизнес-намерение, а не необозримые generic fixtures.

Fixtures не должны обходить constraints прямыми небезопасными inserts, кроме специально маркированных corruption/migration tests.

Для дат истечения используется controlled clock. Для денежных значений — целые dirams. Для quantities — base units.

## 23. Isolation и cleanup

Предпочтительные стратегии:

- отдельная ephemeral database/schema на test process;
- transaction rollback для тестов, которым не нужны concurrent connections;
- explicit truncate/reset между integration cases;
- unique IDs/namespaces для parallel tests.

Transaction-wrapped test нельзя использовать для concurrency behavior, которое требует нескольких независимых connections и реального commit visibility.

Cleanup failure должна падать тестом, а не скрываться.

## 24. Моки, fakes и test doubles

Допустимы:

- fake clock;
- fake ID generator;
- fake crypto/token generator для application unit tests;
- HTTP stub внешнего map/object storage provider;
- in-memory publisher только для post-commit boundary tests.

Не допускается использовать mock вместо реального PostgreSQL для доказательства:

- UoW;
- locks;
- constraints;
- idempotency concurrency;
- outbox claim;
- migrations;
- role permissions.

Test double обязан поддерживать error injection для rollback/failure-path tests.

## 25. CI test suites

### 25.1 Pull request gate

На каждый PR выполняются:

- formatting/lint;
- backend unit/application tests;
- frontend unit/component tests;
- repository/module integration tests;
- HTTP contract tests;
- relevant migration tests;
- relevant concurrency/security regression tests;
- architecture checks;
- secret/dependency scans;
- Markdown links и Mermaid syntax.

### 25.2 Main/nightly gate

Дополнительно:

- полный concurrency suite;
- fuzz corpus;
- race detector;
- browser E2E;
- full migration chain;
- query plan regression checks;
- worker crash/recovery;
- extended security tests;
- performance smoke baseline.

### 25.3 Release candidate gate

Обязательны:

- все PR/main suites;
- production-like migration rehearsal;
- deployment/rollback/forward-fix rehearsal;
- restore drill в пределах утверждённого RTO;
- inventory reconciliation;
- post-deploy smoke;
- security review;
- performance/capacity baseline;
- отсутствие открытых P0/P1 blockers.

## 26. Flaky tests

Flaky test является defect, а не нормой.

Правила:

1. test нельзя бесконечно rerun-ить до зелёного результата;
2. известный flaky test получает owner и root-cause task;
3. quarantine допускается только временно и не для P0/P1 correctness/security coverage;
4. quarantine сохраняет visibility и не превращается в silent skip;
5. причина nondeterminism устраняется через synchronization, fake clock, isolation или environment control;
6. flaky rate измеряется.

## 27. Coverage и mutation testing

Line/branch coverage используется как диагностический показатель, а не как доказательство качества.

Обязательные бизнес-инварианты и failure paths должны иметь явные tests независимо от общего процента.

Mutation testing рекомендуется для:

- Money/quantity rules;
- authorization policies;
- FEFO eligibility;
- idempotency fingerprint;
- status transitions;
- error mapping.

Выжившая mutation в критическом инварианте означает недостаточную test suite.

## 28. Test evidence и traceability

Для каждого critical feature должна существовать связь:

```text
SRS requirement
  → API/domain/database rule
  → sequence/failure scenario
  → automated test(s)
  → CI/release gate
```

Release evidence хранит:

- commit/image digest;
- test suite versions;
- migration result;
- security scan result;
- performance baseline;
- restore/reconciliation result;
- known accepted risks;
- owner approval.

Скриншот зелёного CI без идентифицируемого commit не является достаточным evidence.

## 29. Definition of Ready для тестирования feature

Feature готова к тестированию, когда:

1. acceptance criteria определены;
2. actor, role и pharmacy scope известны;
3. API contract определён;
4. transaction boundary и lock order определены;
5. idempotency/audit requirements определены;
6. failure/retry/race scenarios перечислены;
7. schema/migration impact понятен;
8. test data и environment requirements известны;
9. observability signals определены;
10. юридические/security blockers закрыты или явно отмечены.

## 30. Definition of Done для тестирования feature

Testing для feature завершён только если:

1. domain/application behavior покрыто;
2. persistence проверена на реальном PostgreSQL;
3. HTTP contract проверен;
4. authorization matrix включает negative cases;
5. critical mutation имеет idempotency tests;
6. shared mutable state имеет concurrency tests;
7. audit failure и rollback path проверены;
8. migration/constraint changes протестированы;
9. frontend workflow и stale-state behavior проверены;
10. security/adversarial cases проверены;
11. logs/responses не раскрывают secrets/internal data;
12. CI suites стабильны;
13. regression test добавлен для каждого исправленного critical defect;
14. документация синхронизирована;
15. нет открытых P0/P1 test gaps.

## 31. Минимальная матрица по типу изменения

| Изменение | Обязательные тесты |
|---|---|
| Domain rule | domain unit + application regression |
| HTTP endpoint | contract + auth + error envelope + frontend client |
| DB schema | migration + constraint + compatibility + verification query |
| Critical command | integration + idempotency + audit rollback + concurrency |
| Auth/session | adversarial + concurrent refresh + browser state cleanup |
| Worker/outbox | lease + crash + duplicate delivery + protocol version |
| Frontend workflow | component + contract + browser E2E |
| Deployment config | startup validation + readiness + smoke |
| Backup/recovery | restore drill + reconciliation + audit verification |
| Performance change | benchmark/load + correctness assertions |

## 32. Запрещённые практики

Запрещено:

- считать happy path достаточным;
- заменять PostgreSQL SQLite/mock-ом для транзакционных гарантий;
- использовать arbitrary sleep как основной synchronization mechanism;
- отключать failing critical test без risk record;
- утверждать correctness только coverage percentage;
- тестировать authorization только через frontend;
- хранить real credentials/PII в fixtures;
- делать tests зависимыми от порядка запуска;
- использовать shared mutable global state без reset;
- игнорировать goroutine leaks и hanging transactions;
- проверять errors через string matching, когда доступен `errors.Is()`/`errors.As()`;
- выполнять destructive test cleanup без environment guard;
- запускать release при неизвестном migration/restore результате.

## 33. Открытые решения

До production необходимо утвердить:

1. backend test libraries и assertion style;
2. frontend test runner и browser E2E framework;
3. test database provisioning strategy в CI;
4. property/fuzz corpus storage;
5. mutation testing tooling и частоту;
6. performance environment и baseline SLO;
7. security scanning/DAST tooling;
8. production-like migration dataset strategy;
9. restore drill frequency;
10. ownership flaky test triage и release evidence.
