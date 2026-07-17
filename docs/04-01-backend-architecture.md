# PharmacyCRM — Backend Architecture

**Статус документа:** Draft  
**Версия:** 0.2  
**Дата:** 2026-07-17  
**Связанные документы:** `02-srs.md`, `04-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`  
**Связанные ADR:** ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0017

## 1. Назначение

Документ конкретизирует архитектуру только Go backend-приложения, расположенного в корневом каталоге `backend/`.

Frontend является отдельным корневым приложением `frontend/` и не входит в структуру backend. Интеграция между приложениями выполняется через HTTP API из `05-api-design.md`.

Документ фиксирует:

- физические backend package boundaries;
- направление зависимостей;
- composition root;
- межмодульную orchestration;
- Unit of Work;
- Gin delivery;
- PostgreSQL adapters;
- workers, migrations и backend tests.

Детальная структура всего репозитория определяется `08-project-structure.md`. При расхождении примеров путей применяется более новая и детальная конкретизация из него.

## 2. Архитектурный стиль

Backend реализуется как модульный монолит на Go с одной основной PostgreSQL-базой.

Модульный монолит означает единый deployable backend process или согласованный набор backend executables из одного Go module, но не отсутствие модульных границ.

Каждый бизнес-модуль владеет:

- domain model;
- application use cases;
- application ports;
- PostgreSQL adapters;
- HTTP delivery;
- module-specific tests;
- семантикой своих таблиц.

Прямой доступ к приватным repository implementation или таблицам другого модуля запрещён.

## 3. Backend application root

```text
backend/
├── cmd/
│   ├── api/main.go
│   ├── worker/main.go
│   └── migrate/main.go
├── internal/
│   ├── bootstrap/
│   ├── platform/
│   ├── shared/
│   ├── orchestration/
│   └── modules/
├── migrations/
├── test/
├── go.mod
├── go.sum
├── Makefile
└── Dockerfile
```

Backend не содержит:

- `package.json`;
- React/TypeScript source code;
- Vite configuration;
- frontend build artifacts;
- frontend dependency installation.

## 4. Backend modules

Нормативный набор логических модулей:

```text
backend/internal/modules/
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
```

Названия синхронизированы с `04-architecture.md`, `06-database-design.md` и `07-domain-model.md`.

Старые обобщённые названия `auth`, `discovery` и `recommendation` не используются:

- `identity` владеет users, roles и sessions;
- `search` владеет публичными read models поиска;
- `alerts` и `replenishment` имеют разные ответственности.

## 5. Структура бизнес-модуля

Пример полного модуля:

```text
backend/internal/modules/sales/
├── domain/
│   ├── sale.go
│   ├── sale_item.go
│   ├── allocation.go
│   ├── status.go
│   ├── pricing.go
│   ├── events.go
│   ├── errors.go
│   └── *_test.go
├── application/
│   ├── command/
│   ├── query/
│   ├── port/
│   ├── dto/
│   └── *_test.go
├── infrastructure/
│   ├── postgres/
│   └── projection/
└── delivery/
    └── http/
        ├── routes.go
        ├── complete_sale_handler.go
        ├── get_sale_handler.go
        ├── request.go
        ├── response.go
        └── mapper.go
```

Пустые слои заранее не создаются. Имена файлов отражают предметную ответственность; монолитные `handler.go`, `service.go`, `repository.go`, `entities.go` и `models.go` для всего модуля не являются целевой структурой.

## 6. Направление зависимостей

```text
Delivery / Infrastructure -> Application -> Domain
```

Правила:

1. `domain` не импортирует Gin, pgx, SQL models, HTTP DTO, logger или config.
2. `application` не импортирует Gin и pgx.
3. `infrastructure` реализует application ports и может зависеть от pgx и внешних SDK.
4. `delivery/http` зависит от application contracts и общих transport helpers.
5. `bootstrap` имеет право знать concrete implementations всех backend modules.
6. `platform` предоставляет технические primitives, но не объявляет бизнес-репозитории.
7. Один модуль не импортирует concrete infrastructure другого модуля.
8. Межмодульная координация не выполняется из Domain.

## 7. Composition Root

Все concrete dependencies собираются в:

```text
backend/internal/bootstrap/
```

```text
backend/internal/bootstrap/
├── application.go
├── api.go
├── worker.go
├── dependencies.go
├── modules.go
├── routes.go
└── shutdown.go
```

`cmd/*/main.go` выполняет только process bootstrap:

```text
load process configuration
-> initialize logger
-> open pgx pool
-> construct technical adapters
-> construct module services
-> construct orchestrators
-> construct handlers/workers
-> start process
-> graceful shutdown
```

Запрещены global service locator, package-level mutable dependency registry и чтение environment из произвольных business packages.

## 8. Dependency Injection

Используется ручной constructor-based DI.

Причины:

- dependency graph остаётся явным;
- отсутствует runtime reflection;
- ошибки wiring обнаруживаются компилятором;
- тесты легко подставляют fake implementations;
- размер проекта пока не оправдывает DI framework.

Wire, Dig и аналоги вводятся только отдельным архитектурным решением.

## 9. Межмодульная orchestration

Use case, атомарно затрагивающий несколько module owners, размещается в:

```text
backend/internal/orchestration/<usecase>/
```

Целевые orchestrators:

```text
backend/internal/orchestration/
├── sale/
├── returns/
├── receipt/
├── initialstock/
├── reversal/
└── catalogpublish/
```

Orchestrator:

- не является bounded context;
- не владеет таблицами;
- не содержит SQL;
- не получает `pgx.Tx`;
- определяет application command и transaction-scoped contracts;
- координирует один явный Unit of Work;
- выполняет повторную authorization/scope проверку внутри транзакции;
- планирует только безопасные post-commit действия.

## 10. Unit of Work

ADR-0013 требует явный Unit of Work для критических межмодульных операций.

Низкоуровневый transaction runner находится в:

```text
backend/internal/platform/database/
```

Business UoW contract находится у consumer use case — в application package одного модуля либо в orchestration package.

Пример:

```go
type SaleUnitOfWork interface {
    Scope() ScopeTxPort
    Assortment() AssortmentTxPort
    Inventory() InventoryTxPort
    Sales() SalesTxPort
    Reliability() IdempotencyTxPort
    Audit() AuditTxPort
}

type SaleTransactor interface {
    WithinTransaction(
        ctx context.Context,
        fn func(ctx context.Context, uow SaleUnitOfWork) error,
    ) error
}
```

В application API запрещены `pgx.Tx`, `pgxpool.Pool` и SQL strings.

PostgreSQL implementation создаёт transaction-scoped adapters один раз на callback и связывает их с одним `pgx.Tx`.

Запрещены:

- один глобальный UoW со всеми repositories системы;
- `Repository(name string)`;
- скрытая transaction внутри repository многомодульного use case;
- отдельный commit каждого module effect;
- создание repository при каждом accessor call.

## 11. Transaction retry

Вся callback-операция может повторяться только для явно разрешённых retryable PostgreSQL errors, включая:

- `40P01` — deadlock detected;
- `40001` — serialization failure.

Правила:

1. число попыток ограничено;
2. backoff учитывает `context.Context`;
3. используется jitter;
4. IDs, idempotency key и stable command values создаются до callback;
5. callback не выполняет внешние HTTP calls, email, broker publish или filesystem side effects;
6. authorization и business conditions перепроверяются при каждой попытке;
7. commit error классифицируется отдельно;
8. rollback error логируется, но не скрывает исходную ошибку.

Не каждая database error является retryable. Domain conflict, constraint violation из-за invalid command и insufficient stock не повторяются автоматически.

## 12. Gin HTTP delivery

Gin используется только в:

- module `delivery/http`;
- `backend/internal/platform/httpserver`;
- backend bootstrap routing.

Handler обязан:

1. считать path/query/header/body;
2. выполнить transport validation;
3. использовать strict JSON decoding согласно API Design;
4. преобразовать DTO в application command/query;
5. передать `c.Request.Context()`;
6. вызвать use case;
7. передать ошибку централизованному mapper/responder;
8. сформировать нормативный response envelope.

Handler не выполняет FEFO, расчёт итоговой цены, SQL, transaction management, stock mutation или authorization только по JWT claims.

Ошибки сравниваются через `errors.Is()` и централизованно классифицируются согласно ADR-0016. Ручные строковые сравнения и копирование switch mapping в каждый handler запрещены.

## 13. HTTP server и middleware

Production server создаётся через `gin.New()` и явный `http.Server` с настроенными:

- `ReadHeaderTimeout`;
- `ReadTimeout`;
- `WriteTimeout`;
- `IdleTimeout`;
- graceful shutdown.

Middleware подключаются явно и в проверенном порядке:

- request ID;
- panic recovery;
- structured access logging;
- tracing/metrics;
- body-size limit;
- CORS;
- authentication;
- authorization/scope policies;
- rate limiting там, где это утверждено.

Request/response logging применяет redaction и не логирует credentials, tokens и sensitive payload целиком.

## 14. Platform packages

```text
backend/internal/platform/
├── config/
├── database/
├── httpserver/
├── logging/
├── observability/
├── clock/
├── ids/
├── crypto/
├── files/
└── validation/
```

Platform не владеет бизнес-семантикой. Здесь запрещены `SaleRepository`, `ReturnPolicy`, `UserService` и прочие domain/application contracts.

## 15. Shared packages

```text
backend/internal/shared/
├── kernel/
├── apperror/
├── authcontext/
├── httpx/
└── testutil/
```

Shared package допускается только при одинаковой устойчивой семантике в нескольких модулях.

`shared/httpx` содержит единые transport helpers: envelope, strict decoder, pagination headers и centralized HTTP error responder.

`shared/apperror` содержит типизированную application error classification, но не HTTP status constants в Domain.

Общий `utils` запрещён.

## 16. Владение данными

Нормативное владение:

- `identity`: users, role assignments, sessions;
- `pharmacy`: pharmacies, pharmacist assignments;
- `catalog`: products, presentations, barcodes, product requests, catalog imports;
- `assortment`: pharmacy products, local prices и sale policies;
- `inventory`: receipts, lots, operations, movements, write-offs, adjustments;
- `sales`: sales, sale items, sale item allocations;
- `returns`: sale returns и return allocations;
- `reliability`: idempotency records;
- `audit`: audit events;
- `alerts`: alerts;
- `search`: public read projections;
- `replenishment`: recommendation projections.

Cross-module transaction не меняет ownership. SQL write остаётся в infrastructure package владельца таблицы.

## 17. Migrations

Все PostgreSQL migrations находятся в:

```text
backend/migrations/
```

Migration names отражают module owner и ответственность. Cross-module constraints могут добавляться отдельной integration migration после создания базовых таблиц.

DDL обязан синхронизироваться с `06-database-design.md` в том же change set.

Production reference data создаются migrations. Demo/test data не смешиваются с production migrations.

## 18. Workers

`backend/cmd/worker` использует те же application contracts, что API, и не вызывает HTTP endpoint собственного backend.

Job handler обязан:

- принимать `context.Context`;
- работать bounded batches;
- быть идемпотентным;
- использовать repository locking policy;
- сохранять system actor/audit там, где требуется;
- поддерживать graceful shutdown;
- не удерживать transaction во время внешнего I/O.

## 19. Тестирование

- Domain tests — рядом с domain code, без БД;
- Application tests — рядом с use case, с fake ports/transactor;
- PostgreSQL integration tests — `backend/test/integration`;
- concurrency tests — `backend/test/concurrency` с реальной PostgreSQL;
- HTTP contract tests — `backend/test/contract`;
- backend E2E — `backend/test/e2e`.

Обязательные сценарии включают конкурентные продажи, конкурирующие возвраты, refresh rotation, idempotency races, deadlock retry, fail-closed audit и rollback без частичных movements.

## 20. Architecture enforcement

CI должен проверять:

- `go test ./...`;
- `go vet ./...`;
- static analysis;
- запрет импортов Gin/pgx в Domain/Application;
- запрет импорта concrete infrastructure другого модуля;
- запрет `shared -> modules`;
- запрет `delivery -> infrastructure/postgres`;
- отсутствие frontend source/dependencies внутри `backend/`.

## 21. Definition of Done

Backend structural change завершено только если:

1. package ownership соответствует `04-architecture.md` и `07-domain-model.md`;
2. пути соответствуют `08-project-structure.md`;
3. composition выполняется только в `internal/bootstrap`;
4. межмодульная операция имеет явный orchestration/UoW contract;
5. Domain/Application не зависят от Gin/pgx;
6. SQL остаётся у module owner;
7. HTTP contract синхронизирован с `05-api-design.md`;
8. schema change синхронизирован с `06-database-design.md`;
9. добавлены unit/integration/concurrency tests по типу изменения;
10. frontend не помещён внутрь backend и backend build не устанавливает frontend dependencies.
