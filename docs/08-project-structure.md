# PharmacyCRM — Project Structure

**Статус документа:** Draft  
**Версия:** 1.2  
**Дата:** 2026-07-17  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`  
**Связанные ADR:** ADR-0011, ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Документ определяет физическую структуру репозитория PharmacyCRM и правила размещения backend-, frontend-, deployment-, test- и documentation-кода.

Главное структурное правило проекта:

- `backend/` и `frontend/` являются двумя независимыми приложениями верхнего уровня;
- оба каталога находятся непосредственно в корне репозитория;
- `frontend/` не является частью `backend/` и никогда не размещается внутри него;
- `backend/` не содержит frontend source code, frontend build artifacts, `package.json`, Vite config или frontend dependencies;
- `frontend/` не содержит Go backend packages, migrations или backend runtime configuration.

Документ отвечает на вопросы:

- где должен находиться новый код;
- как разделяются backend и frontend;
- какой backend-модуль владеет конкретным package и таблицами;
- как разделяются Domain, Application, Infrastructure и Delivery;
- где размещаются межмодульные Unit of Work;
- где находятся HTTP DTO, PostgreSQL adapters и query projections;
- как организуются frontend features и shared UI;
- где размещаются unit, integration, concurrency и end-to-end tests;
- какие структуры и зависимости запрещены.

Project Structure не заменяет Domain Model. Package boundary обязан отражать bounded context, aggregate ownership и transaction boundaries из `07-domain-model.md`.

При расхождении с ранним примером структуры в `04-01-backend-architecture.md` настоящий документ является более детальной целевой конкретизацией.

## 2. Модель репозитория

Репозиторий содержит два самостоятельных приложения:

1. `backend/` — Go backend со своим `go.mod`, migrations, Dockerfile, tests и executable entrypoints;
2. `frontend/` — React/TypeScript frontend со своим `package.json`, TypeScript/Vite configuration, Dockerfile и frontend tests.

Они имеют:

- независимые dependency graphs;
- независимые build commands;
- независимые Docker build contexts;
- отдельные CI jobs;
- отдельные lint/test pipelines;
- отдельные runtime configuration boundaries.

Их интеграционный контракт — HTTP API, описанный в `05-api-design.md`. Frontend не импортирует backend source code, Go-модели или PostgreSQL-модели. Backend не импортирует TypeScript-типы.

Термин `monorepo` в документации не используется как архитектурное описание, чтобы не создавать ложного впечатления общего application workspace или вложенности одного приложения в другое. Используется формулировка: **единый репозиторий с двумя независимыми корневыми приложениями**.

## 3. Нормативные корневые каталоги

```text
PharmacyCRM/
├── backend/
├── frontend/
├── deploy/
├── docs/
├── .github/
├── .env.example
├── .gitignore
├── docker-compose.yml
├── Makefile
└── README.md
```

Нормативные имена:

- `backend/` — backend application root;
- `frontend/` — frontend application root;
- `deploy/` — deployment и operational scripts;
- `docs/` — документация и ADR;
- `.github/` — CI workflows и repository automation.

Запрещённые альтернативы для frontend application root:

- `backend/frontend/`;
- `backend/web/`;
- `backend/client/`;
- `web/`;
- `client/`;
- `ui/`.

Изменение имени `backend/` или `frontend/` требует синхронного обновления:

- root Makefile;
- component Makefile/scripts;
- Dockerfile и Compose build contexts;
- CI paths и working directories;
- deployment scripts;
- README;
- документации;
- generated-client paths.

## 4. Полная целевая структура

```text
PharmacyCRM/
├── .github/
│   └── workflows/
│       ├── backend.yml
│       ├── frontend.yml
│       ├── migrations.yml
│       ├── integration.yml
│       └── docs.yml
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

## 5. Backend application root

`backend/` является полностью самостоятельным Go application root.

Он содержит:

- собственный `go.mod` и `go.sum`;
- backend executables;
- backend internal packages;
- PostgreSQL migrations;
- backend tests;
- backend Dockerfile;
- backend-specific Makefile targets.

В `backend/` запрещено размещать:

- `package.json`;
- `node_modules`;
- React components;
- Vite configuration;
- frontend public assets;
- frontend build output.

### 5.1 `backend/cmd`

```text
backend/cmd/
├── api/main.go
├── worker/main.go
└── migrate/main.go
```

Каждый каталог содержит отдельный executable и минимальный bootstrap.

`main.go` может:

- принять process-level параметры;
- вызвать constructor из `internal/bootstrap`;
- установить signal handling;
- запустить приложение;
- выполнить graceful shutdown;
- вернуть корректный exit code.

`main.go` не содержит:

- бизнес-логику;
- SQL;
- ручную регистрацию десятков dependencies;
- HTTP handlers;
- repository implementations;
- package-level mutable globals.

### 5.2 `backend/internal/bootstrap`

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

`bootstrap` является composition root backend-приложения.

Он отвечает за:

- загрузку и валидацию backend config;
- создание Zap logger;
- создание pgx pool;
- создание transaction runner;
- создание infrastructure adapters;
- создание module services и orchestrators;
- создание HTTP handlers и middleware;
- регистрацию routes;
- запуск workers;
- graceful shutdown.

Ни один бизнес-модуль не импортирует `bootstrap`.

### 5.3 `backend/internal/platform`

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

`platform` содержит только технические primitives без бизнес-семантики.

Допустимо:

- connection pool;
- transaction runner;
- context-aware retry;
- HTTP server;
- logger;
- clock;
- ID generator;
- password hashing adapter;
- file storage adapter;
- metrics/tracing primitives.

Запрещено объявлять здесь `SaleRepository`, `UserService`, `InventoryPolicy` и другие бизнес-интерфейсы.

### 5.4 `backend/internal/shared`

```text
backend/internal/shared/
├── kernel/
├── apperror/
├── authcontext/
├── httpx/
└── testutil/
```

Правила:

1. shared type имеет одинаковую семантику минимум в двух bounded contexts;
2. бизнес-правило отдельного модуля не переносится в shared;
3. `shared` не импортирует `modules`;
4. `shared/kernel` не зависит от Gin и pgx;
5. общий каталог `utils` запрещён.

### 5.5 `backend/internal/orchestration`

```text
backend/internal/orchestration/
├── sale/
├── returns/
├── receipt/
├── initialstock/
├── reversal/
└── catalogpublish/
```

Orchestration package создаётся только для use case, который атомарно координирует несколько module owners.

Он:

- не является новым bounded context;
- не владеет таблицами;
- не содержит SQL;
- не обходит module ports;
- определяет узкую transaction boundary;
- координирует один Unit of Work.

## 6. Backend business modules

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

Модульные имена синхронизированы с Architecture, Database Design и Domain Model.

Запрещены глобальные технические каталоги:

```text
backend/internal/handlers/
backend/internal/services/
backend/internal/repositories/
backend/internal/models/
backend/internal/utils/
```

## 7. Шаблон backend-модуля

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
```

Не каждый модуль обязан содержать все подкаталоги. Пустые слои заранее не создаются.

### 7.1 Domain

Содержит:

- aggregate roots;
- entities;
- value objects;
- state machines;
- domain services;
- policies;
- domain errors;
- domain events;
- чистые unit tests.

Не содержит:

- Gin;
- pgx;
- SQL;
- HTTP DTO;
- JSON transport models;
- logger;
- environment config.

### 7.2 Application

Содержит:

- commands;
- queries;
- use cases;
- input/output models;
- ports;
- authorization checks уровня сценария;
- transaction orchestration внутри одного module boundary;
- post-commit intents.

Application не импортирует Gin и pgx.

### 7.3 Infrastructure

Содержит:

- PostgreSQL repositories;
- SQL queries;
- scanners/mappers;
- external adapters;
- projection writers/readers;
- module-specific infrastructure configuration.

Concrete adapters реализуют ports, объявленные consumer/application package.

### 7.4 Delivery

Содержит:

- HTTP routes;
- handlers;
- request DTO;
- response DTO;
- transport validation;
- mapping application result в HTTP response.

Handler не выполняет:

- SQL;
- FEFO;
- расчёт доверенной цены;
- открытие транзакции;
- обновление остатка;
- ручную классификацию domain errors через строковые сравнения.

## 8. Unit of Work placement

Низкоуровневый transaction runner находится в:

```text
backend/internal/platform/database/
```

Business transaction contracts находятся:

- в application package модуля, если use case затрагивает один module owner;
- в `backend/internal/orchestration/<usecase>/`, если use case координирует несколько module owners.

Запрещены:

- передача `pgx.Tx` в Domain/Application API;
- один глобальный UoW со всеми repositories;
- service locator `Repository(name string)`;
- скрытые transactions внутри repository многомодульного сценария;
- создание repository при каждом accessor call.

## 9. PostgreSQL migrations

Все backend migrations находятся только в:

```text
backend/migrations/
```

Frontend не содержит migrations.

Пример:

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

- timestamp prefix задаёт порядок;
- имя отражает module owner;
- migration имеет одну связную ответственность;
- destructive changes используют expand/migrate/contract;
- production reference data создаются migration;
- demo/test fixtures не входят в production migration;
- DDL синхронизируется с `06-database-design.md`.

## 10. Backend tests

```text
backend/test/
├── integration/
├── concurrency/
├── contract/
├── e2e/
├── fixtures/
└── testkit/
```

Domain/application unit tests размещаются рядом с кодом.

Integration/concurrency tests используют реальный PostgreSQL.

`backend/test` не содержит frontend component tests.

## 11. Frontend application root

`frontend/` является полностью самостоятельным React/TypeScript application root и находится рядом с `backend/` в корне репозитория.

Правильное расположение:

```text
PharmacyCRM/
├── backend/
└── frontend/
```

Неправильное расположение:

```text
PharmacyCRM/
└── backend/
    └── frontend/
```

`frontend/` содержит:

- собственный `package.json`;
- lock file;
- TypeScript config;
- Vite config;
- frontend source code;
- frontend tests;
- frontend Dockerfile;
- frontend build output rules.

В `frontend/` запрещено размещать:

- Go packages;
- `go.mod`;
- PostgreSQL migrations;
- backend secrets;
- backend repository implementations;
- прямой доступ к БД.

## 12. Frontend source structure

```text
frontend/src/
├── app/
│   ├── App.tsx
│   ├── router.tsx
│   ├── providers/
│   ├── config/
│   └── styles/
├── pages/
├── features/
├── entities/
├── shared/
└── test/
```

Рекомендуемые features:

```text
frontend/src/features/
├── auth/
├── public-search/
├── manage-assortment/
├── post-receipt/
├── complete-sale/
├── complete-return/
├── write-off-stock/
├── adjust-inventory/
├── catalog-import/
├── manage-users/
└── manage-assignments/
```

Dependency direction:

```text
app -> pages -> features -> entities -> shared
```

Нижний слой не импортирует верхний.

Feature не импортирует другую feature напрямую. Общая capability переносится в entity/shared либо координируется page/application layer frontend.

## 13. Frontend API boundary

```text
frontend/src/shared/api/
```

Содержит:

- base HTTP client;
- API base URL config;
- auth/session integration;
- request ID support;
- response envelope parsing;
- typed API error;
- request cancellation;
- безопасную retry policy.

Endpoint-specific DTO и API functions размещаются рядом с соответствующей feature/entity, а не в одном гигантском `api.ts`.

Generated client, если появится, находится в:

```text
frontend/src/shared/api/generated/
```

Generated TypeScript code не импортируется backend-приложением.

## 14. Frontend tests

Frontend tests находятся только под `frontend/`:

```text
frontend/src/test/
frontend/src/**/*.test.ts
frontend/src/**/*.test.tsx
frontend/e2e/
```

Backend tests не запускают frontend test runner, кроме отдельного repository-level E2E pipeline.

## 15. Docker и build contexts

Backend image:

```text
context: ./backend
dockerfile: Dockerfile
```

Frontend image:

```text
context: ./frontend
dockerfile: Dockerfile
```

Запрещено использовать backend build context для сборки frontend или наоборот без отдельного архитектурного решения.

Compose может находиться в корне, но каждый service использует собственный application root.

Пример:

```yaml
services:
  backend:
    build:
      context: ./backend
  frontend:
    build:
      context: ./frontend
```

## 16. Root Makefile

Root Makefile является orchestration entrypoint и вызывает команды отдельных приложений.

Пример:

```make
backend-test:
	$(MAKE) -C backend test

frontend-test:
	cd frontend && npm test

backend-run:
	$(MAKE) -C backend run

frontend-run:
	cd frontend && npm run dev
```

Root Makefile не смешивает dependency installation двух приложений в один скрытый workspace.

## 17. CI boundaries

Backend workflow:

```text
working-directory: backend
paths:
  - backend/**
```

Frontend workflow:

```text
working-directory: frontend
paths:
  - frontend/**
```

Repository-wide workflows могут дополнительно реагировать на:

- `docs/**`;
- `deploy/**`;
- `docker-compose.yml`;
- root Makefile;
- shared API contract changes.

Backend CI не должен выполнять `npm install` внутри `backend/`. Frontend CI не должен выполнять Go migrations внутри `frontend/`.

## 18. Configuration boundaries

Backend config:

- загружается внутри backend bootstrap;
- содержит DB, JWT/session, logging, server и worker settings;
- не передаётся frontend build process;
- secrets не публикуются в frontend bundle.

Frontend config:

- содержит только public runtime/build-time values;
- не содержит DB credentials;
- не содержит JWT signing secrets;
- не содержит internal backend config.

Корневой `.env.example` может документировать compose-level variables, но component-specific examples допустимо хранить отдельно:

```text
backend/.env.example
frontend/.env.example
```

## 19. Naming и cohesion

Go:

- package names — lowercase и предметные;
- запрещены `common`, `misc`, `helpers`, `utils`;
- command use case — `CompleteSale`, `PostReceipt`, `BlockUser`;
- query — `GetSale`, `ListStockLots`;
- transport file — `<operation>_handler.go`.

TypeScript/React:

- components — PascalCase;
- hooks — `use...`;
- feature directories — kebab-case;
- generic root `components`, `services`, `helpers` запрещены;
- barrel exports не должны скрывать циклические dependencies.

Файл разделяется, если содержит несколько независимых use cases или несвязанных причин изменения. Жёсткий line limit не вводится.

## 20. Dependency enforcement

Минимальные backend checks:

- `go test ./...` из `backend/`;
- `go vet ./...` из `backend/`;
- static analysis;
- architecture import tests;
- migration validation.

Минимальные frontend checks:

- install из `frontend/`;
- TypeScript typecheck;
- lint;
- unit/component tests;
- production build;
- frontend import-boundary lint.

Architecture checks должны подтверждать:

- Domain не импортирует Gin/pgx/platform;
- Application не импортирует Gin/pgx;
- modules не импортируют concrete infrastructure другого module;
- shared не импортирует modules;
- delivery не импортирует postgres packages;
- `backend/` не зависит от files под `frontend/`;
- `frontend/` не зависит от files под `backend/`;
- integration выполняется только через HTTP contract или generated independent client artifacts.

## 21. Порядок добавления новой feature

Backend feature:

1. определить module owner;
2. проверить aggregate и transaction boundary;
3. добавить Domain/Application code в `backend/internal/modules/...`;
4. добавить orchestration только при реальной межмодульной транзакции;
5. добавить infrastructure adapters;
6. добавить HTTP delivery;
7. обновить migrations при необходимости;
8. обновить `05-api-design.md`;
9. добавить tests.

Frontend feature:

1. определить page/feature/entity ownership;
2. использовать API contract из `05-api-design.md`;
3. добавить код только под `frontend/src`;
4. не копировать backend domain entity как mutable frontend model;
5. добавить request cancellation и error handling;
6. добавить frontend tests;
7. обновить generated client только через утверждённый generation flow.

## 22. Запрещённые структуры

```text
backend/frontend/
backend/web/
backend/client/
backend/internal/handlers/
backend/internal/services/
backend/internal/repositories/
backend/internal/models/
backend/internal/utils/
frontend/backend/
frontend/src/services/
frontend/src/helpers/
frontend/src/components/
```

Также запрещены:

- frontend source code внутри backend Docker context;
- backend source code внутри frontend package;
- общий dependency workspace, скрывающий независимые application roots, без отдельного ADR;
- frontend direct database access;
- использование backend DB models как frontend DTO;
- один общий `handler.go` для всех endpoint-ов;
- один общий `service.go` для всех use cases;
- один общий `repository.go` для всех module owners;
- скрытые transaction boundaries;
- shared package как свалка бизнес-логики.

## 23. Открытые решения

До production-ready реализации отдельно утверждаются:

1. способ генерации TypeScript API client;
2. размещение repository-level E2E browser tests;
3. необходимость отдельных `backend/.env.example` и `frontend/.env.example`;
4. ownership root Docker Compose и deployment overlays;
5. package manager frontend;
6. формат frontend runtime config;
7. необходимость shared contract generation artifact без source-level coupling.

## 24. Definition of Done для structural change

Структурное изменение завершено только если:

1. сохраняются независимые корневые каталоги `backend/` и `frontend/`;
2. frontend не перемещён внутрь backend и наоборот;
3. module/package ownership соответствует Architecture и Domain Model;
4. dependency direction не нарушена;
5. build contexts остаются `./backend` и `./frontend`;
6. CI working directories и path filters обновлены;
7. root/component Makefile обновлены;
8. документация и README синхронизированы;
9. generated paths используют правильный application root;
10. backend и frontend tests запускаются независимо;
11. architecture checks запрещают cross-root source imports;
12. новые API endpoint-ы отражены в `05-api-design.md`.
