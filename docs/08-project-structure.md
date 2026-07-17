# PharmacyCRM — Project Structure

**Статус документа:** Draft  
**Версия:** 1.1  
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

## 2. Нормативные имена корневых каталогов

В существующем monorepo используются следующие корневые каталоги:

- `backend/` — Go backend, migrations и backend tests;
- `frontend/` — React/TypeScript frontend;
- `deploy/` — локальная и production-oriented инфраструктура;
- `docs/` — проектная документация и ADR.

Имя `frontend/` является фактическим и нормативным. Каталоги `web/`, `client/`, `ui/` или `app/` как альтернативный корень frontend не создаются.

Изменение имени `backend/` или `frontend/` требует синхронного обновления:

- root и component Makefile;
- Dockerfile и Compose build contexts;
- CI workflow paths и working directories;
- TypeScript, Vite и test configuration;
- deployment scripts;
- README и проектной документации.

## 3. Основные принципы

1. Репозиторий является monorepo с отдельными `backend/` и `frontend/`.
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
15. Вся реализация Go-приложения, кроме executables, находится под `backend/internal`.

## 4. Целевая структура репозитория

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
├── frontend/
│   ├── src/
│   │   ├── app/
│   │   ├── pages/
│   │   ├── features/
│   │   ├── entities/
│   │   ├── shared/
│   │   └── test/
│   ├── public/
│   ├── package.json
│   ├── package-lock.json
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

## 5. Backend: верхний уровень

### 5.1 `backend/cmd`

Каждый каталог под `cmd` содержит отдельный executable и только минимальный bootstrap.

```text
backend/cmd/
├── api/main.go
├── worker/main.go
└── migrate/main.go
```

`main.go` может:

- принять process-level параметры запуска;
- вызвать constructor из `internal/bootstrap`;
- установить signal handling;
- запустить и корректно остановить приложение;
- вернуть process exit code.

`main.go` не должен:

- содержать бизнес-логику;
- вручную собирать большой граф репозиториев;
- регистрировать маршруты по одному;
- содержать SQL;
- читать бизнес-конфигурацию из environment в разных местах;
- использовать mutable package-level globals.

### 5.2 `backend/internal/bootstrap`

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

Он отвечает за:

- загрузку и валидацию конфигурации;
- создание Zap logger;
- открытие pgx pool;
- создание transactor и PostgreSQL adapters;
- создание application services и orchestrators;
- создание middleware и HTTP handlers;
- регистрацию routes;
- запуск workers;
- graceful shutdown в обратном порядке зависимостей.

`bootstrap` может импортировать concrete infrastructure packages всех модулей. Бизнес-модули не импортируют `bootstrap`.

### 5.3 `backend/internal/platform`

`platform` содержит технические механизмы без бизнес-семантики.

```text
backend/internal/platform/
├── config/
├── database/
│   ├── pool.go
│   ├── transaction.go
│   ├── retry.go
│   ├── errors.go
│   └── health.go
├── httpserver/
├── logging/
├── observability/
├── clock/
├── ids/
├── crypto/
├── files/
└── validation/
```

`platform` не объявляет бизнес-типы вроде `SaleRepository`, `UserService` или `InventoryPolicy`. Он предоставляет transaction runner, clock, ID generator, crypto adapters, storage, logger, tracing и HTTP server primitives.

### 5.4 `backend/internal/shared`

`shared` содержит только малый стабильный shared kernel.

```text
backend/internal/shared/
├── kernel/
│   ├── money.go
│   ├── quantity.go
│   ├── pagination.go
│   └── time.go
├── apperror/
├── authcontext/
├── httpx/
└── testutil/
```

Правила:

1. Тип помещается в `shared` только при одинаковой семантике минимум в двух bounded contexts.
2. Бизнес-правило конкретного модуля не переносится в shared ради устранения импорта.
3. `shared` не зависит от `modules`.
4. `shared/kernel` не импортирует Gin, pgx и infrastructure.
5. Каталог `utils` запрещён.

### 5.5 `backend/internal/orchestration`

Здесь находятся межмодульные application use cases и transaction-scoped Unit of Work contracts.

```text
backend/internal/orchestration/
├── sale/
├── returns/
├── receipt/
├── initialstock/
├── reversal/
└── catalogpublish/
```

Orchestrator создаётся только для сценария, который атомарно координирует несколько module owners. Он не является новым bounded context и не владеет таблицами.

## 6. Backend: модули

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

## 7. Шаблон бизнес-модуля

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
│   └── dto/
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

Неиспользуемые каталоги заранее не создаются.

### 7.1 Domain

Содержит aggregate roots, entities, value objects, state machines, domain services, policies, errors и events.

Не содержит HTTP DTO, JSON/DB tags, pgx, logger, environment config и use case orchestration.

### 7.2 Application

Содержит commands, queries, use cases, application models, ports, authorization checks уровня сценария и post-commit intents.

Application не импортирует Gin и pgx и не возвращает HTTP status codes.

### 7.3 Infrastructure

Содержит PostgreSQL adapters, scan/mapping code, external clients и projection adapters.

SQL остаётся внутри infrastructure package модуля-владельца.

### 7.4 Delivery

HTTP handler:

- декодирует transport input;
- выполняет transport validation;
- строит command/query;
- передаёт `context.Context`;
- вызывает application use case;
- отображает result/error в API contract.

Handler не выполняет FEFO, pricing, authorization recheck, SQL или transaction management.

## 8. Правила зависимостей

Допустимое направление:

```text
Delivery / Infrastructure -> Application -> Domain
Bootstrap -> concrete implementations
Orchestration -> published module application/transaction ports
```

Запрещено:

- Domain импортирует Application, Infrastructure или Delivery;
- Application импортирует Gin или pgx;
- один module импортирует concrete repository другого module;
- shared импортирует modules;
- Delivery импортирует postgres implementation;
- platform владеет бизнес-интерфейсами;
- frontend импортирует файлы backend или полагается на SQL schema как API contract.

## 9. Unit of Work

Unit of Work определяется конкретным use case, а не всей системой.

```text
backend/internal/orchestration/sale/
├── service.go
├── command.go
├── ports.go
└── uow.go
```

Правила:

1. Transaction callback получает только типизированный use-case UoW.
2. `pgx.Tx` не выходит в Application и Domain.
3. Все transaction-scoped repositories используют один physical transaction.
4. Accessor возвращает уже созданный repository instance.
5. Retry повторяет всю callback только для утверждённых SQLSTATE.
6. Внешний I/O внутри retryable callback запрещён.
7. UUID, idempotency key и стабильный input создаются до callback.

Запрещены глобальный UoW со всеми repositories и service locator `Repository(name)`.

## 10. HTTP routing и DTO

Root router собирается в bootstrap, но route ownership остаётся у модуля.

```go
api := router.Group("/api/v1")
identityHTTP.RegisterRoutes(api, deps.Identity)
catalogHTTP.RegisterRoutes(api, deps.Catalog)
```

В модуле допустимы:

```text
routes.go
public_routes.go
protected_routes.go
admin_routes.go
```

Request/response DTO находятся в `delivery/http`. Database models и domain entities не используются как transport contracts.

## 11. PostgreSQL и migrations

Persistence implementation находится рядом с module owner. Cross-module read projection может выполнять разрешённые joins, но не получает write ownership.

```text
backend/migrations/
├── *_identity_core.sql
├── *_pharmacy_core.sql
├── *_catalog_core.sql
├── *_assortment_core.sql
├── *_inventory_core.sql
├── *_sales_core.sql
├── *_returns_core.sql
├── *_reliability_audit.sql
└── *_cross_module_constraints.sql
```

Migration должна иметь связную ответственность, module-oriented имя и синхронизироваться с `06-database-design.md`.

Production reference data создаётся migration. Development/demo fixtures в production migration не помещаются.

## 12. Workers

Worker использует те же application contracts, что HTTP API.

Job handler обязан:

- принимать `context.Context`;
- быть идемпотентным;
- использовать bounded batch;
- применять locking policy через repository;
- корректно фиксировать system actor/audit;
- поддерживать graceful shutdown;
- не удерживать транзакцию во время внешнего I/O.

Worker не вызывает HTTP endpoint собственного приложения и не обходит use case прямым SQL update.

## 13. Frontend: корень и структура

Frontend физически находится в корневом каталоге `frontend/`.

```text
frontend/
├── src/
│   ├── app/
│   │   ├── App.tsx
│   │   ├── router.tsx
│   │   ├── providers/
│   │   ├── config/
│   │   └── styles/
│   ├── pages/
│   │   ├── PublicSearchPage/
│   │   ├── LoginPage/
│   │   ├── PharmacyDashboardPage/
│   │   ├── SalesPage/
│   │   ├── InventoryPage/
│   │   └── AdminPage/
│   ├── features/
│   │   ├── auth/
│   │   ├── public-search/
│   │   ├── manage-assortment/
│   │   ├── post-receipt/
│   │   ├── complete-sale/
│   │   ├── complete-return/
│   │   ├── write-off-stock/
│   │   ├── adjust-inventory/
│   │   ├── catalog-import/
│   │   ├── manage-users/
│   │   └── manage-assignments/
│   ├── entities/
│   │   ├── user/
│   │   ├── pharmacy/
│   │   ├── product/
│   │   ├── pharmacy-product/
│   │   ├── stock-lot/
│   │   ├── sale/
│   │   └── alert/
│   ├── shared/
│   │   ├── api/
│   │   ├── ui/
│   │   ├── lib/
│   │   ├── config/
│   │   ├── hooks/
│   │   └── types/
│   └── test/
│       ├── setup.ts
│       ├── mocks/
│       └── fixtures/
├── public/
├── package.json
├── package-lock.json
├── tsconfig.json
├── vite.config.ts
└── Dockerfile
```

### 13.1 Направление зависимостей

```text
app -> pages -> features -> entities -> shared
```

Нижний слой не импортирует верхний. Feature не импортирует другую feature напрямую; координация выполняется page/application composition либо через нижележащую capability.

### 13.2 API client

`frontend/src/shared/api` содержит base client, envelope parsing, typed API error, auth/session integration, request ID, abort support и безопасную retry policy.

Endpoint-specific types и functions находятся рядом с соответствующей feature/entity, а не в гигантском `frontend/src/api.ts`.

### 13.3 Frontend security boundaries

- UI role checks не заменяют backend authorization;
- frontend не является источником trusted stock, price, total или refund;
- sensitive state не сохраняется без необходимости;
- критический retry повторно использует тот же idempotency key;
- stale response не должен восстанавливать очищенное sensitive state;
- token storage определяется `09-security-design.md`.

## 14. Tests

Domain и application unit tests располагаются рядом с кодом.

Central backend suites:

```text
backend/test/
├── integration/
├── concurrency/
├── contract/
├── e2e/
├── fixtures/
└── testkit/
```

Frontend tests располагаются рядом с feature/component либо в `frontend/src/test` для общего setup, mocks и fixtures.

Обязательные правила:

- concurrency tests используют реальный PostgreSQL;
- E2E запускается на migration-clean database;
- fixture builder не обходит constraints проверяемого invariant;
- frontend tests не зависят от backend package internals.

## 15. Generated code

Generated code отделяется и получает стандартный заголовок `Code generated ... DO NOT EDIT.`

Допустимые места:

```text
backend/internal/modules/<module>/infrastructure/postgres/generated/
frontend/src/shared/api/generated/
```

Generated DTO/client не становится Domain Model. Mappers и wrappers защищают application/domain от generator-specific types.

## 16. Configuration и secrets

- `.env.example` содержит только имена и безопасные примеры;
- реальные `.env`, ключи и token secrets не commit-ятся;
- backend config загружается один раз в bootstrap;
- модуль получает типизированный config subset;
- `os.Getenv` внутри domain/application запрещён;
- frontend получает только public build-time/runtime config;
- frontend secrets не существует: любое значение в browser bundle считается публичным.

## 17. Naming conventions

### Go

- package names короткие и lowercase;
- `common`, `misc`, `helpers`, `utils` запрещены;
- interface обычно объявляется consumer package;
- constructor именуется `New...`;
- command use cases: `CompleteSale`, `PostReceipt`, `BlockUser`;
- query use cases: `GetSale`, `ListStockLots`;
- transport files: `<operation>_handler.go`;
- tests: `<subject>_test.go`.

### TypeScript/React

- components: PascalCase;
- hooks: `use...`;
- API functions: verb + resource;
- feature directories: kebab-case;
- barrel exports не должны скрывать циклические зависимости;
- generic `components`, `services`, `helpers` в корне `frontend/src` запрещены.

## 18. Cohesion и размер файлов

Файл разделяется, если:

- содержит несколько независимых use cases;
- имеет несколько несвязанных причин изменения;
- handler смешивает admin/public/protected flows;
- repository смешивает locking commands, writes и сложные read projections;
- test file перестаёт иметь понятный subject;
- review требует читать большую несвязанную часть файла.

Жёсткий числовой лимит строк не заменяет анализ cohesion.

## 19. Dependency enforcement

CI обязан проверять:

- `go test ./...` и `go vet ./...` из `backend/`;
- static analysis;
- запрещённые Go imports;
- frontend lint/typecheck/tests из `frontend/`;
- frontend import boundaries;
- migration validation;
- generated code consistency;
- отсутствие ссылок на несуществующий корневой каталог `web/` в scripts/config/docs.

Architecture checks должны подтверждать, что:

- Domain не импортирует Gin, pgx и platform;
- Application не импортирует Gin и pgx;
- module не импортирует concrete infrastructure другого module;
- shared не импортирует modules;
- Delivery не импортирует postgres implementation;
- production packages не импортируют `backend/test`.

## 20. CI layout

```text
.github/workflows/
├── backend.yml
├── frontend.yml
├── migrations.yml
├── integration.yml
└── docs.yml
```

Frontend workflow использует `frontend/` как `working-directory` и path filter.

Root Makefile может делегировать команды:

```text
make backend-test
make backend-integration
make frontend-install
make frontend-lint
make frontend-test
make frontend-build
make compose-up
make compose-down
```

Команды frontend обязаны выполнять package manager внутри `frontend/`.

## 21. Запрещённые структуры

```text
internal/handlers/
internal/services/
internal/repositories/
internal/models/
internal/utils/
backend/internal/api/handler.go
frontend/src/components/
frontend/src/services/
web/
```

Также запрещены:

- один handler/repository/service для всего приложения;
- прямой SQL из HTTP handler;
- hidden transaction внутри repository многомодульного use case;
- копирование business rules в worker и frontend;
- импорт backend database model во frontend contract;
- второй frontend root параллельно `frontend/`.

## 22. Порядок добавления feature

1. Определить owner context и aggregate boundary.
2. Обновить SRS/API/Domain/Data documents при изменении контракта.
3. Добавить domain types и tests.
4. Добавить application command/query и ports.
5. Реализовать infrastructure adapters.
6. Создать orchestrator только при реальной межмодульной транзакции.
7. Добавить HTTP delivery и routes.
8. Добавить migrations и PostgreSQL tests при изменении данных.
9. Добавить frontend feature внутри `frontend/src/features` и необходимые entity/shared части.
10. Добавить integration, concurrency, contract и E2E tests.
11. Обновить CI/Makefile/Docker только при изменении build/runtime graph.

## 23. Definition of Done

Структурное изменение завершено только если:

1. код находится в модуле-владельце;
2. package dependencies направлены внутрь;
3. aggregate и repository boundaries соответствуют `07-domain-model.md`;
4. SQL остаётся у module owner;
5. межмодульная транзакция использует типизированный UoW;
6. HTTP DTO не протекают в Domain;
7. frontend код находится под `frontend/`;
8. Makefile, CI, Docker и deployment используют `frontend/` как единственный frontend root;
9. tests расположены на правильном уровне;
10. architecture checks проходят;
11. документация не содержит устаревших путей `web/`.

## 24. Открытые решения

До начала соответствующей реализации необходимо определить:

1. package manager frontend и lockfile policy;
2. способ генерации TypeScript API client;
3. необходимость transactional outbox package;
4. инструмент автоматической проверки Go dependency boundaries;
5. инструмент frontend import-boundary enforcement;
6. точную организацию production deployment manifests.

Эти решения не меняют нормативное имя корневого frontend-каталога: `frontend/`.
