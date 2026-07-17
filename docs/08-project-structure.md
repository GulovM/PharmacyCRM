# PharmacyCRM — Project Structure

**Статус документа:** Draft  
**Версия:** 1.0  
**Дата:** 2026-07-17  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`  
**Связанные ADR:** ADR-0011, ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет целевую физическую структуру репозитория PharmacyCRM: каталоги backend, frontend, миграций, тестов, локальной инфраструктуры и документации, а также правила размещения кода и допустимых зависимостей между пакетами.

Документ отвечает на вопросы:

- где должен находиться новый код;
- какой модуль владеет конкретным package и таблицами;
- как разделяются Domain, Application, Infrastructure и Delivery;
- где собираются межмодульные Unit of Work;
- где находятся HTTP DTO, PostgreSQL-модели и query projections;
- как организуются frontend features и shared UI;
- где размещаются unit, integration, concurrency и end-to-end tests;
- какие каталоги и зависимости запрещены.

Project Structure не заменяет Domain Model. Package boundary обязан отражать bounded context, aggregate ownership и transaction boundaries из `07-domain-model.md`, а не форму таблиц или список endpoint-ов.

При расхождении с ранним примером структуры в `04-01-backend-architecture.md` настоящий документ является более детальной целевой конкретизацией. Архитектурные правила и направление зависимостей остаются нормативными согласно `04-architecture.md` и ADR.

## 2. Основные принципы

1. Репозиторий является monorepo с отдельными Go backend и web frontend.
2. Backend является модульным монолитом, а не набором технических глобальных слоёв.
3. Код группируется сначала по бизнес-модулю, затем по архитектурному слою.
4. Каждый бизнес-модуль владеет своей domain model, use cases, ports, PostgreSQL adapters и HTTP delivery.
5. Общие каталоги `handlers`, `services`, `repositories`, `models` и `utils` для всех модулей запрещены.
6. Domain не зависит от Application, Infrastructure, Delivery, Gin, pgx, JSON или конфигурации.
7. Application зависит от Domain и объявленных ports, но не от Gin и pgx.
8. Infrastructure реализует ports и может зависеть от pgx, файловой системы и внешних клиентов.
9. Delivery зависит от application contracts и transport helpers, но не выполняет бизнес-логику и SQL.
10. Concrete dependency graph создаётся только в composition root.
11. Межмодульная транзакция не превращает один модуль в владельца чужих таблиц.
12. Read models могут пересекать несколько модулей, но являются явно выделенными query projections без права записи.
13. Файл создаётся по одной связной ответственности; недопустимы монолитные `handler.go`, `service.go` или `repository.go`, содержащие весь модуль.
14. Package не создаётся заранее без реальной ответственности и публичного контракта.
15. Публичность Go package минимальна: `internal` используется для всей реализации приложения.

## 3. Целевая структура репозитория

```text
PharmacyCRM/
├── .github/
│   └── workflows/
├── backend/
│   ├── cmd/
│   │   ├── api/
│   │   │   └── main.go
│   │   ├── worker/
│   │   │   └── main.go
│   │   └── migrate/
│   │       └── main.go
│   ├── internal/
│   │   ├── bootstrap/
│   │   ├── platform/
│   │   ├── shared/
│   │   ├── orchestration/
│   │   └── modules/
│   ├── migrations/
│   ├── test/
│   │   ├── integration/
│   │   ├── concurrency/
│   │   ├── contract/
│   │   ├── e2e/
│   │   ├── fixtures/
│   │   └── testkit/
│   ├── go.mod
│   ├── go.sum
│   ├── Makefile
│   └── Dockerfile
├── web/
│   ├── src/
│   │   ├── app/
│   │   ├── pages/
│   │   ├── features/
│   │   ├── entities/
│   │   ├── shared/
│   │   └── test/
│   ├── public/
│   ├── package.json
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── Dockerfile
├── deploy/
│   ├── compose/
│   └── scripts/
├── docs/
│   └── adr/
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Makefile
└── README.md
```

Названия `backend` и `web` являются нормативными для monorepo. Перенос backend в корень или переименование frontend требует синхронизации Makefile, CI, Docker, документации и deployment scripts.

## 4. Backend: верхний уровень

### 4.1 `backend/cmd`

Каждый каталог под `cmd` содержит отдельный executable и только минимальный bootstrap.

```text
backend/cmd/
├── api/main.go
├── worker/main.go
└── migrate/main.go
```

`main.go` разрешено:

- загрузить process-level параметры запуска;
- вызвать constructor из `internal/bootstrap`;
- установить signal handling;
- запустить и корректно остановить приложение;
- вернуть process exit code.

`main.go` запрещено:

- создавать репозитории вручную по всему файлу;
- регистрировать десятки маршрутов;
- содержать SQL;
- реализовывать use cases;
- читать бизнес-конфигурацию напрямую из environment в разных местах;
- использовать package-level mutable globals.

### 4.2 `backend/internal/bootstrap`

`bootstrap` является composition root приложения.

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

Ответственность:

- загрузка и валидация общей конфигурации через platform config;
- создание Zap logger;
- открытие pgx pool;
- создание transactor и PostgreSQL adapters;
- создание application services и orchestrators;
- создание middleware и HTTP handlers;
- регистрация routes;
- запуск workers;
- graceful shutdown в обратном порядке зависимостей.

`bootstrap` может импортировать concrete infrastructure packages всех модулей. Ни один бизнес-модуль не импортирует `bootstrap`.

### 4.3 `backend/internal/platform`

`platform` содержит технические механизмы общего назначения без бизнес-семантики.

```text
backend/internal/platform/
├── config/
│   ├── config.go
│   ├── loader.go
│   └── validation.go
├── database/
│   ├── pool.go
│   ├── transaction.go
│   ├── retry.go
│   ├── errors.go
│   └── health.go
├── httpserver/
│   ├── server.go
│   ├── middleware.go
│   └── health.go
├── logging/
│   ├── logger.go
│   ├── fields.go
│   └── redaction.go
├── observability/
│   ├── metrics.go
│   └── tracing.go
├── clock/
├── ids/
├── crypto/
├── files/
└── validation/
```

`platform` не объявляет `SaleRepository`, `UserService`, `InventoryPolicy` и другие бизнес-интерфейсы. Он предоставляет только технические primitives: transaction runner, clock, ID generator, password hasher adapter, file storage, logger и HTTP server.

### 4.4 `backend/internal/shared`

`shared` содержит только малый стабильный shared kernel.

```text
backend/internal/shared/
├── kernel/
│   ├── money.go
│   ├── quantity.go
│   ├── pagination.go
│   └── time.go
├── apperror/
│   ├── error.go
│   ├── codes.go
│   └── classify.go
├── authcontext/
│   ├── actor.go
│   └── context.go
├── httpx/
│   ├── envelope.go
│   ├── decode.go
│   ├── pagination.go
│   ├── errors.go
│   └── headers.go
└── testutil/
```

Правила shared kernel:

1. Тип помещается в `shared` только если имеет одинаковую семантику минимум в двух bounded contexts.
2. Бизнес-правило конкретного модуля не переносится в shared ради устранения одного импорта.
3. `shared` не зависит от `modules`.
4. `shared/kernel` не импортирует Gin, pgx и infrastructure.
5. Каталог `utils` запрещён; ответственность должна иметь предметное имя.

### 4.5 `backend/internal/orchestration`

`orchestration` содержит межмодульные application use cases и transaction-scoped Unit of Work contracts.

```text
backend/internal/orchestration/
├── sale/
│   ├── command.go
│   ├── service.go
│   ├── ports.go
│   ├── uow.go
│   └── service_test.go
├── returns/
├── receipt/
├── initialstock/
├── reversal/
└── catalogpublish/
```

Orchestrator создаётся только для сценария, который атомарно координирует несколько module owners. Он не становится новым доменным модулем и не владеет таблицами.

Например, `orchestration/sale` координирует:

- identity/pharmacy scope recheck;
- assortment locking and price snapshots;
- inventory FEFO working set;
- создание Sale;
- idempotency;
- mandatory audit.

Транзакционные repository interfaces остаются определены владельцами соответствующих capabilities либо в узком orchestration contract, но concrete pgx implementation собирается в bootstrap/infrastructure и использует один `pgx.Tx`.

## 5. Backend: модули

Целевой набор модулей:

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

Названия синхронизированы с `04-architecture.md`, `06-database-design.md` и `07-domain-model.md`. Общий модуль `auth` заменён более точным `identity`; `discovery` называется `search`; `recommendation` разделяется на `alerts` и `replenishment`.

## 6. Шаблон бизнес-модуля

Полный модуль может иметь следующую структуру:

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
│   │   ├── complete_sale.go
│   │   └── reverse_sale.go
│   ├── query/
│   │   ├── get_sale.go
│   │   └── list_sales.go
│   ├── port/
│   │   ├── repository.go
│   │   └── readers.go
│   └── dto/
├── infrastructure/
│   ├── postgres/
│   │   ├── repository.go
│   │   ├── repository_insert.go
│   │   ├── repository_read.go
│   │   ├── queries.go
│   │   ├── scan.go
│   │   └── model.go
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

Не каждый модуль обязан содержать все каталоги. Неиспользуемый слой не создаётся пустым.

### 6.1 `domain`

Содержит:

- aggregate roots и entities;
- value objects, специфичные модулю;
- state machines;
- domain services и policies;
- domain errors;
- domain events;
- чистые unit tests.

Не содержит:

- repository implementation;
- HTTP DTO;
- JSON tags ради transport;
- SQL/DB tags;
- pgx types;
- logger;
- environment config;
- use case orchestration.

Файл именуется по доменному понятию, а не `entities.go` или `models.go`, если содержимое уже достаточно велико для предметного разделения.

### 6.2 `application`

Содержит:

- commands и queries;
- use case handlers/services;
- application input/output models;
- ports;
- authorization checks уровня сценария;
- transaction orchestration внутри одного модуля;
- post-commit intents.

Command и query не обязаны использовать универсальный mediator. Прямые типизированные constructor-based services предпочтительнее framework abstraction без доказанной пользы.

Application DTO не равен HTTP request и не равен PostgreSQL row model.

### 6.3 `infrastructure`

Содержит:

- PostgreSQL repository implementations;
- scan/mapping persistence rows;
- внешние adapters;
- projection writers/readers;
- реализацию module-specific ports.

SQL хранится рядом с module owner. Допускаются:

- SQL-константы в предметно названных `.go` файлах;
- `queries/*.sql` при использовании code generation;
- generated code в отдельном `generated` package.

Нельзя складывать SQL всех модулей в один глобальный database package.

### 6.4 `delivery/http`

Содержит:

- route registration;
- request/response DTO;
- transport validation;
- mapping DTO ↔ application model;
- вызов use case;
- использование централизованного error responder.

Один handler-файл обслуживает один use case или малую связную группу. Файл `handler.go` допустим только пока модуль действительно мал; после роста он разделяется по операциям.

## 7. Структуры конкретных модулей

### 7.1 Identity

```text
modules/identity/
├── domain/
│   ├── user.go
│   ├── role_assignment.go
│   ├── session.go
│   ├── credentials.go
│   ├── events.go
│   └── errors.go
├── application/
│   ├── command/
│   │   ├── login.go
│   │   ├── refresh_session.go
│   │   ├── logout.go
│   │   ├── create_user.go
│   │   ├── block_user.go
│   │   ├── change_password.go
│   │   └── assign_role.go
│   ├── query/
│   └── port/
├── infrastructure/
│   ├── postgres/
│   ├── password/
│   └── token/
└── delivery/http/
```

Raw passwords и raw tokens существуют только на transport/application boundary настолько коротко, насколько необходимо. Они не входят в persistence model и не логируются.

### 7.2 Pharmacy

Владеет `Pharmacy` и `PharmacyAssignment`. Ассортимент не помещается внутрь pharmacy package, потому что является отдельным aggregate/context.

### 7.3 Catalog

```text
modules/catalog/
├── domain/
│   ├── product.go
│   ├── presentation.go
│   ├── barcode.go
│   ├── product_request.go
│   └── import_job.go
├── application/
│   ├── command/
│   ├── query/
│   └── port/
├── infrastructure/
│   ├── postgres/
│   └── importfile/
└── delivery/http/
```

`Product` и `ProductPresentation` имеют отдельные repositories/aggregate roots согласно Domain Model. Import file parsing не находится в Domain.

### 7.4 Inventory

```text
modules/inventory/
├── domain/
│   ├── stock_lot.go
│   ├── operation.go
│   ├── movement.go
│   ├── receipt.go
│   ├── write_off.go
│   ├── adjustment.go
│   ├── working_set.go
│   └── errors.go
├── application/
│   ├── command/
│   ├── query/
│   └── port/
├── infrastructure/postgres/
└── delivery/http/
```

`working_set.go` описывает transient command model для согласованного изменения нескольких лотов; он не является repository-backed aggregate root и не получает отдельную таблицу.

### 7.5 Sales и Returns

`sales` и `returns` остаются отдельными модулями даже при физической близости. Returns не изменяет sales tables напрямую через свой repository; межмодульный use case координируется `orchestration/returns`.

### 7.6 Reliability и Audit

`reliability` владеет idempotency semantics и PostgreSQL repository. `audit` владеет append-only AuditEvent и query API расследования.

Они не превращаются в универсальные decorators, скрывающие транзакционные границы. Критический orchestrator явно включает их transaction-scoped ports.

### 7.7 Search и Replenishment

Это query-oriented модули. Они читают проекции и не получают права обновлять операционные агрегаты.

```text
modules/search/
├── application/
│   ├── query/
│   └── port/
├── infrastructure/postgres/
└── delivery/http/
```

Domain-каталог допустимо не создавать, если модуль содержит только query projections без самостоятельных бизнес-состояний.

## 8. Межмодульные зависимости

### 8.1 Допустимые зависимости

```text
Delivery ──> Application ──> Domain
Infrastructure ──> Application ports + Domain
Bootstrap ──> all concrete constructors
Orchestration ──> public application/domain contracts of participating modules
```

### 8.2 Запрещённые зависимости

- `domain` → `application`, `infrastructure`, `delivery`, `bootstrap`;
- `application` → Gin, pgx, concrete PostgreSQL repository;
- module A infrastructure → module B private PostgreSQL package;
- handler → repository;
- repository → handler/DTO;
- frontend entity → backend implementation model;
- `shared` → business module;
- cyclic imports между modules;
- import `internal/bootstrap` из любого module.

### 8.3 Публичные module contracts

Для взаимодействия между modules используется минимальный package `application/port` либо явно выделенный `contract`:

```text
modules/pharmacy/application/port/authorization_reader.go
modules/assortment/application/port/sale_reader.go
modules/inventory/application/port/transaction_repository.go
```

Нельзя экспортировать весь service/repository ради одного метода. Contract должен быть consumer-oriented и узким.

## 9. Unit of Work placement

### 9.1 Технический transactor

`internal/platform/database` владеет технической реализацией:

- begin/commit/rollback;
- transaction options;
- retry SQLSTATE `40P01`/`40001`;
- context-aware backoff;
- commit error classification.

Он не знает о Sales, Inventory или Audit.

### 9.2 Scenario UoW contract

Use-case-specific UoW interface находится рядом с orchestrator:

```text
internal/orchestration/sale/uow.go
```

Он публикует только необходимые transaction-scoped capabilities:

```go
type UnitOfWork interface {
    Identity() IdentityTxPort
    Pharmacy() PharmacyTxPort
    Assortment() AssortmentTxPort
    Inventory() InventoryTxPort
    Sales() SalesTxPort
    Idempotency() IdempotencyTxPort
    Audit() AuditTxPort
}
```

Concrete `pgxSaleUnitOfWork` размещается в infrastructure/composition package, доступном bootstrap, например:

```text
internal/bootstrap/uow/sale_postgres.go
```

или в отдельном:

```text
internal/orchestration/sale/postgres/
```

Второй вариант допустим только если package остаётся infrastructure adapter и не смешивается с application service. Выбранный вариант применяется единообразно ко всем orchestrators.

### 9.3 Запрещённые варианты

- передача `pgx.Tx` в application callback;
- repository самостоятельно начинает скрытую транзакцию внутри многомодульного use case;
- один глобальный `UnitOfWork` со всеми repositories системы;
- service locator вида `uow.Repository(name string)`;
- создание нового repository при каждом accessor call;
- внешние сетевые вызовы внутри retryable callback.

## 10. HTTP routing structure

Root router собирается в bootstrap, но route ownership остаётся у module:

```go
api := router.Group("/api/v1")
identityHTTP.RegisterRoutes(api, deps.Identity)
catalogHTTP.RegisterRoutes(api, deps.Catalog)
```

Module routes разделяются по access boundary:

```text
routes.go
public_routes.go
protected_routes.go
admin_routes.go
```

Middleware композиция выполняется явно. Handler не проверяет роль строковыми сравнениями вручную, если это уже обязанность authorization middleware/application policy.

Operational endpoints `/healthz` и `/readyz` принадлежат platform/httpserver и не входят в `/api/v1`.

## 11. HTTP DTO и error mapping

1. Request/response DTO размещаются только в `delivery/http`.
2. DTO не используется как domain entity или database model.
3. Application output может быть отдельным immutable result type.
4. Централизованный error mapper находится в `shared/httpx` либо platform HTTP adapter согласно ADR-0016.
5. Модуль возвращает typed application/domain errors, а не HTTP status.
6. Unknown JSON fields, body limits, content type и envelope обрабатываются единообразно.
7. Sensitive DTO получают явную redaction policy и не передаются logger-у целиком.

## 12. PostgreSQL и migrations

### 12.1 Repository packages

Каждый module owner хранит persistence implementation рядом с модулем. Cross-module join для command path не создаёт нового владельца данных.

Read-only reporting/search query может находиться в query module и выполнять joins через специально разрешённый reader role.

### 12.2 Migrations

Все migrations централизованы:

```text
backend/migrations/
├── 20260717090000_identity_core.sql
├── 20260717091000_pharmacy_core.sql
├── 20260717092000_catalog_core.sql
├── 20260717093000_assortment_core.sql
├── 20260717094000_inventory_core.sql
├── 20260717095000_sales_core.sql
├── 20260717100000_returns_core.sql
├── 20260717101000_reliability_audit.sql
└── 20260717102000_cross_module_constraints.sql
```

Правила:

- timestamp prefix обеспечивает порядок;
- имя указывает module owner и назначение;
- одна migration имеет связную ответственность;
- destructive migration разделяется на expand/migrate/contract;
- FK между уже созданными modules допустимо добавлять отдельной integration migration;
- migration содержит `-- +goose Up`/`Down` или формат выбранного migration tool;
- необратимый Down явно документируется, а не имитирует потерю данных;
- DDL синхронизируется с `06-database-design.md` в том же change set.

### 12.3 Seed data

Production-required role codes и иные системные reference data создаются migration, а не случайным startup seed.

Development/demo fixtures не входят в production migration и хранятся в test/deploy tooling.

## 13. Workers и background jobs

Worker executable использует те же application services и module contracts, что API.

```text
backend/internal/bootstrap/worker.go
backend/internal/modules/alerts/application/job/
backend/internal/modules/catalog/application/job/
```

Worker не вызывает HTTP endpoint собственного приложения и не обходит use cases прямым SQL update.

Job handler обязан:

- принимать `context.Context`;
- быть идемпотентным;
- использовать bounded batch;
- применять `FOR UPDATE SKIP LOCKED` только через repository policy;
- оставлять audit/system actor там, где это требуется;
- поддерживать graceful shutdown;
- не удерживать транзакцию во время внешнего I/O.

## 14. Frontend structure

Frontend использует feature-oriented структуру с односторонним направлением зависимостей.

```text
web/src/
├── app/
│   ├── App.tsx
│   ├── router.tsx
│   ├── providers/
│   ├── config/
│   └── styles/
├── pages/
│   ├── PublicSearchPage/
│   ├── LoginPage/
│   ├── PharmacyDashboardPage/
│   ├── SalesPage/
│   ├── InventoryPage/
│   └── AdminPage/
├── features/
│   ├── auth/
│   ├── public-search/
│   ├── manage-assortment/
│   ├── post-receipt/
│   ├── complete-sale/
│   ├── complete-return/
│   ├── write-off-stock/
│   ├── adjust-inventory/
│   ├── catalog-import/
│   ├── manage-users/
│   └── manage-assignments/
├── entities/
│   ├── user/
│   ├── pharmacy/
│   ├── product/
│   ├── pharmacy-product/
│   ├── stock-lot/
│   ├── sale/
│   └── alert/
├── shared/
│   ├── api/
│   ├── ui/
│   ├── lib/
│   ├── config/
│   ├── hooks/
│   └── types/
└── test/
    ├── setup.ts
    ├── mocks/
    └── fixtures/
```

### 14.1 Frontend dependency direction

```text
app -> pages -> features -> entities -> shared
```

Нижний слой не импортирует верхний. Feature не импортирует другую feature напрямую; общая capability выносится в entity/shared либо координируется page.

### 14.2 API client

`shared/api` содержит:

- base client;
- auth header/session integration;
- request ID handling;
- common envelope parsing;
- typed API error;
- abort support;
- retry policy только для безопасных сценариев.

Endpoint-specific request/response types находятся рядом с feature/entity API module, а не в одном гигантском `api.ts`.

### 14.3 Frontend security boundaries

- refresh/access token storage определяется Security Design;
- raw credentials и sensitive responses не сохраняются в global persistent store без необходимости;
- UI role checks не являются authorization control;
- frontend не рассчитывает доверенный stock total, sale total или refund;
- критическая POST-команда генерирует и повторно использует тот же idempotency key при безопасном retry;
- stale response не должен восстанавливать очищенное sensitive state.

## 15. Tests structure

### 15.1 Colocated tests

Domain и application unit tests располагаются рядом с кодом:

```text
sale.go
sale_test.go
complete_sale.go
complete_sale_test.go
```

### 15.2 Central integration suites

```text
backend/test/
├── integration/
│   ├── identity/
│   ├── catalog/
│   ├── inventory/
│   └── sales/
├── concurrency/
│   ├── sale_same_lot_test.go
│   ├── return_same_allocation_test.go
│   ├── refresh_rotation_test.go
│   └── idempotency_test.go
├── contract/
│   ├── api_envelope_test.go
│   └── error_mapping_test.go
├── e2e/
│   └── critical_flow_test.go
├── fixtures/
└── testkit/
    ├── postgres.go
    ├── migrations.go
    ├── factories.go
    └── assertions.go
```

`testkit` не импортируется production packages, кроме тестов. Test fixture builder не должен обходить constraints там, где проверяется production invariant.

### 15.3 Package conventions

- white-box tests (`package x`) допустимы для внутренних domain details;
- black-box tests (`package x_test`) предпочтительны для публичного application contract;
- integration tests маркируются build tag или отдельной командой;
- concurrency tests используют реальный PostgreSQL, а не только mocks;
- E2E поднимает migration-clean database.

## 16. Generated code

Generated code всегда отделяется и помечается стандартным заголовком `Code generated ... DO NOT EDIT.`

Допустимые места:

```text
module/infrastructure/postgres/generated/
web/src/shared/api/generated/
```

Generated DTO/client не становится Domain Model. Обёртки и mappers защищают domain/application от изменений генератора.

## 17. Configuration и secrets

- `.env.example` содержит только имена и безопасные примеры;
- реальные `.env`, ключи и token secrets не commit-ятся;
- backend config загружается один раз в bootstrap;
- модуль получает типизированный config subset через constructor;
- запрещены `os.Getenv` внутри domain/application/module handlers;
- secrets не передаются как общая config struct всем модулям;
- frontend получает только public build-time/runtime config.

## 18. Naming conventions

### Go

- package names: короткие, lowercase, без `common`, `misc`, `helpers`;
- exported name имеет предметный смысл;
- interface обычно объявляется consumer package;
- constructor: `New...` с обязательными dependencies;
- command use case: `CompleteSale`, `PostReceipt`, `BlockUser`;
- query: `GetSale`, `ListStockLots`;
- PostgreSQL implementation: `Repository`, если package уже предметно назван;
- transport files: `<operation>_handler.go`;
- tests: `<subject>_test.go`.

### TypeScript/React

- components: PascalCase;
- hooks: `use...`;
- API functions: verb + resource;
- feature directories: kebab-case;
- avoid barrel exports, если они скрывают циклические зависимости;
- generic `components`, `services`, `helpers` на корне `src` запрещены.

## 19. File-size и cohesion rules

Жёсткий лимит строк не вводится, но файл разделяется, когда:

- содержит несколько независимых use cases;
- имеет несколько несвязанных причин для изменения;
- handler смешивает admin/public/protected flows;
- repository смешивает command locking, writes и сложные read projections;
- test file перестаёт иметь понятный subject;
- review отдельного изменения требует читать большую несвязанную часть файла.

Размер файла сам по себе не является основанием дробить связную state machine на десятки микрофайлов. Критерий — cohesion и ownership.

## 20. Dependency enforcement

Архитектурные правила должны проверяться автоматически.

Минимум:

- `go test ./...`;
- `go vet ./...`;
- static analysis;
- запрещённые imports через architecture test или linter policy;
- отсутствие cyclic imports обеспечивается Go compiler;
- frontend lint import boundaries;
- migration validation;
- generated code consistency.

Рекомендуемый architecture test проверяет, что:

- domain packages не импортируют Gin/pgx/platform;
- application packages не импортируют Gin/pgx;
- modules не импортируют concrete infrastructure другого module;
- shared не импортирует modules;
- delivery не импортирует postgres packages;
- production packages не импортируют `backend/test`.

## 21. CI layout

```text
.github/workflows/
├── backend.yml
├── frontend.yml
├── migrations.yml
├── integration.yml
└── docs.yml
```

Pipeline должен разделять быстрые unit/static checks и более тяжёлые PostgreSQL/concurrency/E2E suites, но обязательные проверки branch protection не могут быть молча skipped.

Docs workflow проверяет:

- ссылки;
- нумерацию файлов;
- отсутствие незарегистрированных верхнеуровневых документов;
- согласованность generated API artifacts, если они появятся.

## 22. Root Makefile и команды

Корневой Makefile является удобным facade и делегирует backend/web/deploy.

Рекомендуемые команды:

```text
make setup
make env-up
make env-down
make migrate-up
make migrate-down
make backend-test
make backend-integration
make frontend-test
make lint
make test
make run-api
make run-worker
```

Команды должны работать на Windows и Linux либо явно использовать portable tooling/container execution. Скрипты не зависят от текущего shell без документированного требования.

## 23. Запрещённые структуры и anti-patterns

Запрещены:

```text
internal/handlers/
internal/services/
internal/repositories/
internal/models/
internal/utils/
web/src/components/   # как глобальная свалка
web/src/services/     # как единый API/service слой
```

Также запрещены:

- один `internal/api/handler.go` со всеми endpoint-ами;
- один repository с SQL всех bounded contexts;
- один application service со всеми use cases проекта;
- database row struct как domain entity и HTTP response одновременно;
- импорт private module internals через относительные обходы;
- циклическая dependency, скрытая callback/interface abuse;
- business constants в migration-only SQL без доменного эквивалента;
- test helpers в production package ради доступа к private state;
- background worker, напрямую изменяющий таблицы другого module;
- дублирование одного value object с различной семантикой под одинаковым именем;
- premature `pkg/` public library без внешнего consumer;
- пустые каталоги ради формального соответствия шаблону.

## 24. Порядок добавления новой feature

1. Определить bounded context и aggregate owner в `07-domain-model.md`.
2. Проверить внешний contract в `05-api-design.md`.
3. Проверить data ownership/constraints в `06-database-design.md`.
4. Создать или расширить domain types и unit tests.
5. Добавить application command/query и consumer-owned ports.
6. Если сценарий межмодульный — добавить узкий orchestrator и scenario UoW.
7. Реализовать infrastructure adapters владельцев данных.
8. Добавить HTTP delivery и централизованный mapping ошибок.
9. Добавить migration, integration/concurrency tests при изменении данных.
10. Добавить frontend feature/entity API при необходимости.
11. Зарегистрировать constructors/routes только в bootstrap.
12. Проверить dependency rules и документацию в том же change set.

## 25. Открытые решения

До production-ready реализации необходимо утвердить:

1. конкретный migration tool и формат generated SQL;
2. стратегию OpenAPI generation и место generated clients;
3. необходимость transactional outbox и её module ownership;
4. точный frontend state/query library;
5. способ автоматической проверки Go module boundaries;
6. разделение API и worker binaries по container images;
7. storage adapter для import files;
8. конкретную структуру deployment manifests;
9. политику feature flags для юридически неутверждённых функций;
10. единый вариант размещения concrete scenario UoW adapters.

Открытое решение не разрешается появлением случайного package. После утверждения структура и документы обновляются явно.

## 26. Definition of Done для структуры feature

Feature считается корректно встроенной в проект, если:

1. определён module owner;
2. aggregate и transaction boundary соответствуют Domain Model;
3. package находится в правильном слое;
4. направление imports не нарушено;
5. transport DTO, application model, domain model и persistence model не смешаны;
6. SQL находится у владельца данных;
7. межмодульная запись выполняется через orchestrator/UoW;
8. bootstrap является единственным composition root;
9. handler не содержит бизнес-логики и SQL;
10. repository не открывает скрытую транзакцию для многомодульной команды;
11. tests размещены на соответствующем уровне;
12. migrations и docs синхронизированы;
13. frontend feature соблюдает dependency direction;
14. sensitive data не попадает в logs, fixtures и persistent frontend state;
15. architecture checks проходят в CI.
