# PharmacyCRM — Development Roadmap

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-20  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`

## 1. Назначение и нормативная роль

Документ определяет порядок реализации PharmacyCRM: этапы, зависимости, обязательные архитектурные и security gates, критерии завершения, правила допуска функций к следующему этапу и условия готовности MVP к production.

Roadmap не является календарным обещанием и не задаёт произвольные сроки. Он фиксирует порядок снижения рисков: сначала создаются инфраструктурные и архитектурные механизмы, от которых зависит корректность последующих функций, затем реализуются бизнес-сценарии, после чего выполняются hardening, эксплуатационная подготовка и production readiness.

Изменение продуктового scope синхронизируется с Product Vision и SRS. Изменение API, схемы данных, aggregate boundary, security control, transaction protocol или package ownership обновляет соответствующие нормативные документы в том же change set.

## 2. Принципы планирования

### 2.1 Вертикальные срезы

Функции реализуются вертикальными срезами: migration, domain, application, repository, HTTP contract, authorization, audit, tests, frontend и документация должны сходиться в одном завершённом change set.

Запрещено считать функцию готовой только потому, что:

- создана таблица без use case;
- создан handler без транзакционных инвариантов;
- frontend отображает mock-данные;
- happy path работает без authorization, idempotency, audit и негативных тестов;
- код написан, но `05-api-design.md` не обновлён.

### 2.2 Risk-first порядок

Сначала реализуются механизмы, ошибка в которых способна скомпрометировать весь продукт:

1. конфигурация, startup validation и безопасный runtime;
2. migrations и базовые ограничения БД;
3. Unit of Work и transaction retry;
4. identity, sessions и authorization;
5. idempotency и transactional audit;
6. immutable inventory movements и lock order;
7. только после этого — поступления, продажи, возвраты и корректировки.

### 2.3 Без скрытого временного дизайна

Временное упрощение допустимо только если:

- оно явно отмечено;
- не нарушает бизнес-инварианты;
- имеет владельца и задачу удаления;
- не создаёт второй несовместимый путь реализации;
- не ослабляет security control без зарегистрированного security exception.

### 2.4 Один источник истины

- API-контракт — `05-api-design.md`;
- схема и ограничения — `06-database-design.md` и migrations;
- агрегаты и транзакции — `07-domain-model.md`;
- package ownership — `08-project-structure.md`;
- security controls — `09-security-design.md`;
- порядок критических вызовов и locks — `10-sequence-diagrams.md`.

Roadmap не дублирует детали этих документов, а определяет, когда они должны быть реализованы и проверены.

## 3. Модель этапов и gates

Каждый этап имеет:

- входные условия;
- обязательный scope;
- выходные артефакты;
- quality gate;
- запрещённые переходы.

Переход к следующему этапу разрешён только после закрытия обязательных критериев текущего этапа. Незакрытые элементы могут переноситься только как явно зарегистрированный риск, если они не затрагивают correctness, security или data integrity.

## 4. Этап 0 — закрытие архитектурных решений

### 4.1 Цель

Устранить открытые решения, которые способны вызвать несовместимые реализации в нескольких модулях.

### 4.2 Обязательные решения

До массовой реализации должны быть приняты ADR или нормативные уточнения для:

1. алгоритма password hashing и параметров rehash;
2. access/refresh token model, JWT key rotation и session invalidation;
3. MFA для `ADMIN` и recovery flow;
4. окончательного lock order;
5. transaction retry policy;
6. transactional outbox и delivery semantics;
7. политики возврата лекарств, включая `RETURN_TO_STOCK`;
8. retention audit, security logs, sessions, imports и backup;
9. frontend package manager и API client generation;
10. deployment topology и trusted proxy model.

### 4.3 Gate E0

Этап завершён, когда:

- у каждого открытого решения есть принятый ADR или явная deferred-позиция;
- deferred-решение не блокирует корректную реализацию ближайшего этапа;
- документы 01–10 не содержат взаимоисключающих правил;
- список рисков имеет владельцев.

## 5. Этап 1 — engineering foundation

### 5.1 Цель

Создать воспроизводимую, безопасную и тестируемую основу двух независимых приложений `backend/` и `frontend/`.

### 5.2 Backend foundation

Обязательный scope:

- Go module и package layout согласно `08-project-structure.md`;
- composition root;
- `gin.New()` без default middleware;
- явный `http.Server` с timeouts;
- конфигурация через `envconfig`;
- startup validation конфигурации;
- Zap logger в terminal и file;
- request ID, recovery, access logging и tracing middleware;
- централизованный HTTP response/error mapper;
- graceful shutdown;
- health и readiness endpoints;
- PostgreSQL pool configuration;
- migration runner или отдельная migration command;
- test fixtures и test database lifecycle;
- clock, ID generator и crypto ports.

### 5.3 Frontend foundation

Обязательный scope:

- независимое приложение в `frontend/`;
- утверждённый package manager и lockfile;
- TypeScript strict mode;
- routing, error boundary и application shell;
- generated или строго типизированный API client;
- единая обработка error envelope;
- безопасная in-memory auth state;
- базовые accessibility и browser test fixtures.

### 5.4 Repository и CI foundation

- форматирование и lint;
- `go test ./...`;
- `go vet ./...`;
- frontend typecheck, lint и tests;
- migration up/down verification;
- secret scanning;
- dependency vulnerability scanning;
- architecture checks для запрещённых imports;
- проверка Markdown links и Mermaid syntax;
- build контейнеров без production secrets.

### 5.5 Gate E1

- новый разработчик запускает окружение документированной командой;
- приложение падает при небезопасной или неполной конфигурации;
- health/readiness различают process alive и dependency ready;
- graceful shutdown не обрывает активную транзакцию без контролируемого timeout;
- CI воспроизводимо проходит на чистом checkout;
- секреты отсутствуют в репозитории и build artifacts.

## 6. Этап 2 — database kernel и reliability primitives

### 6.1 Цель

Реализовать механизмы, на которых будут строиться все критические бизнес-транзакции.

### 6.2 Обязательный scope

- базовые identity, pharmacy, catalog, assortment, inventory, sales, returns, idempotency, audit и outbox migrations;
- ULID/UUID policy согласно Database Design;
- foreign keys, unique constraints и check constraints;
- immutable movement/audit permissions;
- repository base abstractions без generic CRUD над агрегатами;
- Unit of Work на `pgx` без утечки `pgx.Tx` в application;
- transaction retry classifier;
- idempotency claim/complete/replay protocol;
- transactional audit writer;
- outbox writer и worker lease protocol;
- deterministic lock helpers;
- migration/concurrency test harness.

### 6.3 Обязательные tests

- migration с нуля;
- последовательное применение всех migrations;
- проверка constraints;
- rollback transaction function;
- panic внутри UoW;
- serialization/deadlock retry;
- idempotency same payload replay;
- idempotency different payload conflict;
- audit failure вызывает rollback;
- два worker-а не обрабатывают одну lease одновременно;
- повтор outbox delivery не создаёт второй эффект.

### 6.4 Gate E2

Нельзя начинать проведение продаж, пока не доказано, что:

- UoW корректно commit/rollback-ит;
- idempotency защищает от повторного эффекта;
- audit является fail-closed для обязательных операций;
- immutable records нельзя штатно изменить или удалить;
- retry повторяет всю transaction function;
- lock order опубликован и проверяется concurrency tests.

## 7. Этап 3 — identity, authentication и authorization

### 7.1 Цель

Создать доверенный actor context и немедленно управляемый доступ к защищённым операциям.

### 7.2 Scope

- создание пользователя `ADMIN`-ом;
- роли и role assignments;
- user states `ACTIVE`, `BLOCKED`, `ARCHIVED`;
- password hashing и transparent rehash;
- login;
- access token validation;
- server-side refresh sessions;
- refresh rotation и reuse detection;
- logout current/all sessions;
- блокировка пользователя с отзывом sessions;
- password change/reset с отзывом sessions;
- pharmacy assignments;
- отзыв assignment;
- policy layer для RBAC + resource scope;
- stale-sensitive revalidation внутри transactions;
- MFA для `ADMIN` согласно принятому ADR;
- audit и security events.

### 7.3 Frontend

- login screen;
- memory-only access token;
- refresh cookie flow;
- logout и очистка sensitive state;
- route guards только как UX, не как security boundary;
- защита от stale responses после logout или смены actor generation;
- session expiry handling.

### 7.4 Gate E3

- cryptographically valid token заблокированного пользователя не даёт доступ;
- отзыв assignment останавливает новые pharmacy-scoped mutations;
- два параллельных refresh одного generation не завершаются успешно;
- reuse отзывает session family;
- role/profile mass assignment невозможен;
- неизвестный пользователь и неверный пароль не различаются внешне;
- security-critical flows покрыты integration и browser tests.

## 8. Этап 4 — pharmacy и global catalog

### 8.1 Pharmacy management

- создание, изменение разрешённых публичных полей, блокировка и архивирование аптеки;
- проверка state аптеки во всех scoped operations;
- адрес, координаты, телефон и график работы;
- запрет физического удаления операционно значимой аптеки.

### 8.2 Global catalog

- `Product` и `ProductPresentation`;
- штрихкоды и нормализованные значения;
- создание и модерация глобальных карточек;
- staging import job;
- quarantine storage;
- streaming parser с лимитами;
- validation findings;
- approve/reject/merge moderation flow;
- защита от formula injection в exports.

### 8.3 Gate E4

- `PHARMACIST` не меняет global catalog напрямую;
- import не публикует данные без moderation;
- malformed row не приводит к скрытой частичной публикации;
- дубликаты и уникальность защищены application + constraints;
- public API не раскрывает staging и internal metadata.

## 9. Этап 5 — assortment и pricing

### 9.1 Scope

- `PharmacyProduct`;
- включение/выключение позиции в ассортименте;
- package и optional inner-unit price;
- base units per package snapshot policy;
- минимальный остаток;
- правила продажи внутренними единицами;
- optimistic concurrency для изменяемых настроек;
- audit изменения цен и правил.

### 9.2 Gate E5

- цена не принимается как итоговая сумма продажи от frontend;
- изменение текущей цены не переписывает исторические документы;
- неизвестная или чужая pharmacy product скрывается как `NOT_FOUND`/`FORBIDDEN` согласно API policy;
- concurrent update не приводит к silent lost update;
- изменение цены покрыто audit и authorization tests.

## 10. Этап 6 — inventory intake и stock truth

### 10.1 Scope

- draft/posted receipt lifecycle;
- receipt lines и snapshots;
- создание `StockLot`;
- batch number и expiration date;
- initial stock только через movement;
- immutable `InventoryMovement`;
- stock balance update;
- idempotent posting;
- reversal вместо изменения posted receipt;
- inventory history read model.

### 10.2 Gate E6

- lot quantity не становится отрицательной;
- posted receipt нельзя редактировать или удалить;
- receipt, lots, movements, audit и idempotency result атомарны;
- повтор после network disconnect возвращает исходный result;
- reversal создаёт компенсирующую историю;
- concurrency tests подтверждают lock order.

## 11. Этап 7 — sales и FEFO

### 11.1 Scope

- создание и проведение продажи;
- package/inner-unit quantities;
- server-side totals;
- current assortment rules;
- FEFO allocation;
- exclusion expired/quarantined/depleted lots;
- sale snapshots;
- immutable sale allocations;
- inventory decrement и movements;
- idempotency, audit и outbox;
- receipt/print-friendly read model без фискализации.

### 11.2 Gate E7

- frontend не выбирает authoritative lot allocation;
- две продажи не могут списать один остаток дважды;
- после lock выполняется повторная проверка eligibility и quantity;
- money и quantity overflow обрабатываются;
- sale graph, lot balances, movements, audit и idempotency commit атомарно;
- public availability обновляется через надёжный post-commit protocol.

## 12. Этап 8 — returns, write-offs и adjustments

### 12.1 Returns

Реализация начинается только после утверждения юридической policy.

Scope:

- возврат по исходной sale item/allocation;
- контроль cumulative returned quantity;
- disposition;
- `RETURN_TO_STOCK` только при разрешённой policy;
- non-restocking return;
- компенсирующие movements;
- idempotency и audit.

### 12.2 Write-offs и adjustments

- отдельные предметные документы;
- обязательный reason;
- allowlisted reason codes;
- повышенное разрешение для рискованных корректировок;
- запрет generic `PATCH stock`;
- reversal/compensation flow;
- anomaly metrics.

### 12.3 Gate E8

- return не переписывает sale;
- cumulative returns не превышают исходное количество;
- две конкурирующие returns не превышают allocation;
- adjustment всегда оставляет документ, movement и audit;
- reason нельзя обойти пустым или произвольным техническим значением;
- административная роль не получает скрытый обход immutable history.

## 13. Этап 9 — public search, alerts и replenishment

### 13.1 Public search

- public projection;
- published catalog only;
- active pharmacies only;
- availability без точного внутреннего остатка;
- freshness timestamp;
- safe geo sorting;
- pagination, rate limiting, caching и ETag;
- graceful degradation внешнего map provider.

### 13.2 Alerts

- low stock;
- near expiry;
- expired lot;
- deduplication;
- lifecycle acknowledgement/resolution;
- идемпотентный worker.

### 13.3 Replenishment

- вычисляемая рекомендация;
- отсутствие автоматического заказа поставщику;
- объяснимые входные данные;
- read model без изменения stock truth.

### 13.4 Gate E9

- public projection не является command source;
- internal quantities и purchase prices не раскрываются;
- projection lag измеряется;
- повтор worker event не создаёт дубликаты;
- alert/replenishment failure не откатывает уже committed sale;
- stale availability явно маркируется или исключается согласно policy.

## 14. Этап 10 — frontend operational workflows

Frontend развивается параллельно backend vertical slices, но этот этап закрывает целостный пользовательский workflow.

Обязательные сценарии:

- admin: users, pharmacies, assignments, catalog moderation и audit;
- pharmacist: assortment, receipt, lots, sale, return, write-off, adjustment, alerts;
- public: search, filters, map/list и freshness;
- robust loading/error/empty states;
- keyboard navigation и базовая accessibility;
- confirmation для необратимых действий;
- prevention accidental duplicate submit;
- session expiry и authorization changes;
- client-side sensitive state purge;
- browser E2E по ролям и ownership boundaries.

Gate E10:

- frontend не содержит ручных расхождений с API contract;
- все critical commands используют `Idempotency-Key`;
- поздний response не восстанавливает очищенные credentials/state;
- UI не показывает действие как успешно завершённое до подтверждённого server response;
- ошибки локализуются по стабильному `error.code`.

## 15. Этап 11 — security, reliability и performance hardening

### 15.1 Security hardening

- threat model review;
- MFA enforcement для remote ADMIN;
- CORS, CSRF, CSP, HSTS и trusted proxies;
- rate limits и abuse controls;
- session/recovery adversarial tests;
- secret/key rotation drill;
- dependency/SBOM review;
- privilege review PostgreSQL roles;
- backup access review;
- audit completeness review.

### 15.2 Reliability hardening

- concurrency suite под нагрузкой;
- worker crash/restart tests;
- outbox backlog recovery;
- database connection exhaustion;
- graceful shutdown during requests/jobs;
- migration rollback/recovery rehearsal;
- retry storm prevention;
- clock skew and expiration boundaries.

### 15.3 Performance baseline

Измеряются, но не оптимизируются вслепую:

- login/password hashing capacity;
- sale transaction latency;
- FEFO query plan;
- public search p50/p95/p99;
- import throughput и memory usage;
- outbox lag;
- connection pool saturation;
- critical table/index growth.

### 15.4 Gate E11

- отсутствуют открытые Critical/High security findings без утверждённого exception;
- нагрузка не нарушает correctness;
- retry не создаёт duplicate effect;
- slow query plans исследованы;
- RPO/RTO проверены restore drill;
- security и reliability runbooks исполнимы.

## 16. Этап 12 — production readiness и pilot

### 16.1 Production readiness

Обязательны:

- `12-deployment.md`, `13-testing-strategy.md`, `14-observability.md` в актуальном состоянии;
- production configuration matrix;
- TLS и secret management;
- backup/restore;
- monitoring и alert routing;
- incident response contacts;
- migration deployment procedure;
- rollback/forward-fix policy;
- audit retention;
- privacy/data handling review;
- юридическое подтверждение returns scope;
- inventory reconciliation procedure;
- operator training;
- release checklist.

### 16.2 Pilot

Pilot проводится на ограниченной аптеке и ограниченном числе сотрудников.

Перед pilot:

- загружен и проверен начальный каталог;
- initial stock импортирован и reconciled;
- роли и assignments проверены;
- backup выполнен;
- поддержка знает rollback и incident flow.

Во время pilot измеряются:

- расхождения остатков;
- rejected/failed sales;
- latency;
- frequency adjustments;
- user errors;
- alerts usefulness;
- projection freshness;
- security anomalies.

### 16.3 Gate E12

MVP допускается к production только если:

- все P0/P1 correctness и security defects закрыты;
- inventory reconciliation не выявляет необъяснимых расхождений;
- restore drill успешен;
- critical alerts доставляются;
- rollback/forward-fix procedure проверена;
- audit позволяет восстановить actor, action, target и result критической операции;
- product owner принимает MVP scope;
- ответственный за эксплуатацию принимает operational readiness.

## 17. Cross-cutting workstreams

Следующие направления выполняются на каждом этапе, а не откладываются на конец.

### 17.1 Documentation

Каждый HTTP feature обновляет `05-api-design.md` с URL, authorization, request, response, errors и side effects.

Изменение схемы, модели, структуры, security или последовательности синхронизирует соответствующий документ.

### 17.2 Testing

Для каждого use case обязательны:

- domain unit tests;
- application tests;
- repository integration tests;
- authorization negative tests;
- idempotency tests для critical commands;
- concurrency tests при shared mutable state;
- API contract tests;
- frontend/browser tests для пользовательского workflow.

### 17.3 Observability

Новая critical operation до merge определяет:

- structured log events;
- audit event;
- metrics;
- trace boundaries;
- alert condition, если failure требует реакции.

### 17.4 Security

Security review обязателен при изменении:

- auth/session;
- role/assignment;
- public/private boundary;
- upload/export;
- admin operations;
- audit;
- secret/crypto;
- infrastructure privileges.

### 17.5 Data migration

Каждое изменение schema оценивает:

- backward compatibility;
- lock duration;
- table rewrite;
- rollback или forward-fix;
- data backfill;
- verification query;
- deployment order backend/frontend/migration.

## 18. Приоритет дефектов

### P0 — release blocker

- потеря или незаметная порча inventory/financial history;
- отрицательный остаток;
- обход authorization или чужой pharmacy scope;
- утечка credentials/secrets;
- повторный необратимый эффект;
- возможность изменения immutable records;
- невозможность restore;
- audit отсутствует для критической успешной операции.

### P1 — обязательный до production

- существенная race condition без подтверждённой потери данных;
- неверный public availability;
- недостаточный incident signal;
- broken logout/session revocation SLA;
- критический workflow недоступен без workaround;
- недетерминированная migration/deployment процедура.

### P2 — допустим в pilot при владельце

- UX defect с безопасным workaround;
- некритичная performance degradation;
- неполная аналитика;
- cosmetic inconsistency.

P0 и P1 нельзя переносить в production backlog без formal risk acceptance. P0 не допускает исключений.

## 19. Definition of Ready для feature

Feature готова к реализации, когда:

1. известны actor и business goal;
2. определены роли и resource scope;
3. API contract описан или подготовлен change;
4. aggregate owner и transaction boundary определены;
5. schema impact понятен;
6. idempotency requirement определён;
7. audit requirement определён;
8. failure, retry и race scenarios перечислены;
9. frontend contract понятен;
10. acceptance criteria проверяемы;
11. открытые юридические/security вопросы не блокируют реализацию.

## 20. Definition of Done для feature

Feature завершена только если:

1. внешнее поведение соответствует SRS и API Design;
2. business invariants находятся в Domain/Application, а не только в handler;
3. authorization проверяет актуальный actor и resource scope;
4. critical mutation имеет idempotency;
5. transaction boundary и lock order соответствуют Sequence Diagrams;
6. обязательный audit атомарен с business effect;
7. errors сравниваются через `errors.Is()`/`errors.As()` и централизованно отображаются;
8. migration и constraints добавлены и протестированы;
9. unit, integration, concurrency, contract и security tests проходят;
10. frontend не дублирует server authority;
11. structured logs и metrics не содержат secrets;
12. документация обновлена в том же change set;
13. CI проходит на clean checkout;
14. нет открытых P0/P1 defects в scope feature;
15. rollback, retry и partial failure behaviour определены.

## 21. Release strategy

Рекомендуется trunk-based или short-lived branch development с небольшими вертикальными change sets.

Правила release:

- production release создаётся из immutable commit/tag;
- container image подписывается или имеет проверяемый digest;
- migration и application compatibility проверены заранее;
- release не строится заново после approval;
- deployment использует тот же artifact, который прошёл CI;
- feature flags не обходят authorization и domain invariants;
- rollback не откатывает schema небезопасным способом;
- при необратимой migration используется forward-fix plan.

## 22. Метрики прогресса

Прогресс не измеряется числом созданных файлов или endpoint-ов. Используются:

- завершённые vertical slices;
- покрытые acceptance criteria;
- количество закрытых gates;
- число открытых P0/P1;
- migration reliability;
- concurrency/security coverage;
- lead time от Ready до Done;
- escaped defects;
- inventory reconciliation accuracy;
- outbox/projection lag;
- production/pilot incident rate.

Coverage percentage не используется как самостоятельное доказательство качества.

## 23. Зависимости этапов

```text
E0 Decisions
    ↓
E1 Engineering Foundation
    ↓
E2 Database & Reliability Kernel
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
E10 Complete Frontend Workflows
    ↓
E11 Hardening
    ↓
E12 Production Readiness & Pilot
```

Параллельная работа допустима внутри этапа и над frontend vertical slice, если зависимые contracts уже стабильны. Нельзя параллельно реализовывать несовместимые версии UoW, auth, idempotency, audit или lock order.

## 24. Открытые вопросы

1. Юридическая политика возврата лекарств.
2. Итоговый набор security ADR из `09-security-design.md`.
3. Transactional outbox и retry ADR.
4. Deployment topology и окружения.
5. RPO/RTO и backup retention.
6. Frontend package manager и API generation tool.
7. Допустимый SLA public projection.
8. Нужен ли dual approval для особо рискованных ADMIN operations.
9. Pilot pharmacy и критерии выхода из pilot.
10. Ownership production operations и incident commander role.
