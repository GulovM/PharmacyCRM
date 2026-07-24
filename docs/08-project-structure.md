# PharmacyCRM — Project Structure

> E2 schema `24`: runtime PostgreSQL composition is split into API and worker credentials; the legacy `pharmacycrm_runtime` role exists only for immutable migration compatibility.

**Статус документа:** Draft  
**Версия:** 2.0  
**Дата:** 2026-07-21  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`  
**Связанные ADR:** ADR-0011, ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0017

## 1. Назначение

Документ определяет физическую структуру репозитория PharmacyCRM и правила размещения backend, frontend, deployment, test и documentation artifacts.

Главное правило:

```text
PharmacyCRM/
├── backend/
└── frontend/
```

`backend/` и `frontend/` являются двумя независимыми приложениями верхнего уровня. Они расположены рядом в корне репозитория; ни одно приложение не находится внутри другого.

## 2. Границы двух приложений

### 2.1 Backend

`backend/` является самостоятельным Go application root со своими:

- `go.mod` и `go.sum`;
- executables;
- internal packages;
- PostgreSQL migrations;
- tests;
- Dockerfile;
- build/lint/test commands;
- runtime configuration.

### 2.2 Frontend

`frontend/` является самостоятельным React/TypeScript application root со своими:

- `package.json`;
- lockfile выбранного package manager;
- TypeScript/Vite configuration;
- source code;
- tests;
- Dockerfile;
- build/lint/test commands;
- public runtime/build-time configuration.

### 2.3 Интеграционная граница

Единственный обязательный application contract между frontend и backend — HTTP API из `05-api-design.md`.

Запрещено:

- импортировать Go source или database models во frontend;
- импортировать TypeScript source в backend;
- размещать frontend под `backend/`;
- размещать backend под `frontend/`;
- использовать прямой доступ frontend к PostgreSQL;
- разделять runtime secrets между приложениями.

## 3. Нормативная структура корня

```text
PharmacyCRM/
├── .github/
│   └── workflows/
├── backend/
├── frontend/
├── deploy/
├── docs/
│   └── adr/
├── .gitignore
├── docker-compose.yml
├── Makefile
└── README.md
```

Корневые файлы являются repository-level coordination artifacts. Они не превращают два приложения в общий dependency workspace.

`backend/.env.example` и `frontend/.env.example` являются предпочтительными местами для component-specific configuration examples. Корневой `.env.example`, если используется, содержит только Compose/repository-level параметры и не дублирует все настройки приложений.

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
│   │   ├── api/main.go
│   │   ├── worker/main.go
│   │   └── migrate/main.go
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
│   ├── .env.example
│   ├── go.mod
│   ├── go.sum
│   ├── Makefile
│   └── Dockerfile
├── frontend/
│   ├── src/
│   │   ├── app/
│   │   ├── pages/
│   │   ├── widgets/
│   │   ├── features/
│   │   ├── entities/
│   │   ├── shared/
│   │   └── test/
│   ├── e2e/
│   ├── public/
│   ├── .env.example
│   ├── package.json
│   ├── <package-manager-lockfile>
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── Dockerfile
├── deploy/
│   ├── compose/
│   └── scripts/
├── docs/
│   └── adr/
├── .gitignore
├── docker-compose.yml
├── Makefile
└── README.md
```

Lockfile не фиксируется как `package-lock.json`, пока отдельно не выбран package manager. После выбора в репозитории хранится ровно один соответствующий lockfile.

## 5. Backend root

Backend structure детально определена `04-01-backend-architecture.md`.

Основные каталоги:

```text
backend/
├── cmd/
├── internal/
│   ├── bootstrap/
│   ├── platform/
│   ├── shared/
│   ├── orchestration/
│   └── modules/
├── migrations/
└── test/
```

В `backend/` запрещены:

- `package.json`;
- `node_modules`;
- React/TypeScript source;
- Vite config;
- frontend public assets;
- frontend build output.

## 6. Backend modules

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

Ownership фиксирован: `pharmacy` владеет `pharmacy_assignments`; `reliability` — `idempotency_records` и `outbox_events`; catalog import находится в `catalog`; receipts, initial stock, write-offs и adjustments — в `inventory`. Отдельные module roots `import/`, `receipt/`, `adjustments/` запрещены.

Handwritten production source ограничен 400 физическими строками. Один файл несёт одну связную ответственность; при 300–350 строках требуется оценить разделение. God-files и новые generic filenames (`utils.go`, `helpers.go`, `common.go`, `misc.go`, `manager.go`, `service_all.go`, `repository_all.go`) запрещены. Canonical locking разделён между repository, inventory/sale-return orchestration, SQL queries и validation; reconciliation testkit разделяет types, orchestration, individual SQL oracles, scanner и validation.

Глобальные технические каталоги запрещены:

```text
backend/internal/handlers/
backend/internal/services/
backend/internal/repositories/
backend/internal/models/
backend/internal/utils/
```

## 7. Backend module template

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
```

Пустые слои заранее не создаются. Файлы именуются по предметной ответственности, а не общими `handler.go`, `service.go`, `repository.go` для всего модуля.

## 8. Backend dependency rules

```text
Delivery / Infrastructure -> Application -> Domain
```

- Domain не импортирует Gin, pgx, SQL/HTTP DTO, logger и config.
- Application не импортирует Gin и pgx.
- Infrastructure реализует application ports.
- Delivery вызывает application use cases.
- Concrete graph собирается только в `backend/internal/bootstrap`.
- Межмодульная transaction orchestration находится в `backend/internal/orchestration/<usecase>`.
- SQL write остаётся у module owner.

## 9. Unit of Work

Низкоуровневый transaction runner:

```text
backend/internal/platform/database/
```

Business UoW contract:

- в application package одного модуля для single-owner use case;
- в `backend/internal/orchestration/<usecase>/` для multi-owner use case.

Запрещены `pgx.Tx` в Application/Domain API, global UoW со всеми repositories, service locator и скрытые repository transactions.

## 10. Migrations и backend tests

Все PostgreSQL migrations находятся только в:

```text
backend/migrations/
```

Backend central tests:

```text
backend/internal/testkit/
├── postgrestest/
├── reconciliation/
└── schema/
```

Domain/application unit tests и owning-package PostgreSQL integration tests размещаются рядом с кодом. Общие cross-module correctness oracles находятся в `internal/testkit`; integration/concurrency tests используют реальную PostgreSQL и обязательные CI DSN.

## 11. Frontend root

```text
frontend/
├── src/
├── e2e/
├── public/
├── .env.example
├── package.json
├── <package-manager-lockfile>
├── tsconfig.json
├── vite.config.ts
└── Dockerfile
```

В `frontend/` запрещены Go packages, migrations, backend secrets, backend repository implementations и direct database access.

## 12. Frontend source layers

```text
frontend/src/
├── app/
├── pages/
├── widgets/
├── features/
├── entities/
├── shared/
└── test/
```

Направление зависимостей:

```text
app -> pages -> widgets -> features -> entities -> shared
```

### `app`

Application bootstrap frontend: router, providers, global styles, runtime config initialization и error boundaries.

### `pages`

Route-level composition. Page не содержит повторно используемую крупную UI-композицию, если она естественно является widget.

### `widgets`

Крупные самостоятельные блоки страницы, собирающие несколько features/entities: например `SaleWorkspace`, `InventorySummary`, `PharmacyHeader`.

### `features`

Завершённые пользовательские действия: login, complete sale, post receipt, return, write-off, catalog import.

### `entities`

Frontend representation и UI/API helpers предметных сущностей. Эти модели не являются копиями backend aggregates и не содержат серверные инварианты как источник истины.

### `shared`

UI primitives, base API client, config, generic hooks и технические helpers без бизнес-семантики.

Нижний слой не импортирует верхний. Feature не импортирует другую feature напрямую.

## 13. Frontend API structure

```text
frontend/src/shared/api/
├── client/
├── errors/
├── envelope/
├── auth/
└── generated/
```

`shared/api` содержит base client и общие protocol mechanisms. Endpoint-specific API modules находятся рядом с feature/entity.

Один гигантский `frontend/src/api.ts` запрещён.

Generated client:

- создаётся из утверждённого API schema/contract;
- не импортирует backend source code;
- перегенерируется воспроизводимой командой;
- не редактируется вручную;
- оборачивается application-friendly adapters при необходимости.

Нормативный source contract — OpenAPI 3.1 `backend/api/openapi.yaml`. `pnpm` 10.x через Corepack запускает pinned `openapi-typescript` + `openapi-fetch`; output — `frontend/src/shared/api/generated/`. Generated code не редактируется вручную, а CI выполняет generation и fails on diff.

## 14. Frontend tests

```text
frontend/src/**/*.test.ts
frontend/src/**/*.test.tsx
frontend/src/test/
frontend/e2e/
```

Ownership:

- unit/component tests принадлежат frontend;
- browser E2E tests находятся в `frontend/e2e`, если управляются frontend tooling;
- repository-level cross-application smoke tests могут находиться в `deploy/tests` или отдельном root test package только после фиксации в Testing Strategy;
- один и тот же E2E suite не дублируется одновременно в `backend/test/e2e` и `frontend/e2e`.

`backend/test/e2e` проверяет backend/API flows без браузерного UI, если иное явно не определено.

## 15. Docker boundaries

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

Compose может находиться в корне, но каждый service использует собственный build context.

Frontend build context не получает backend secrets. Backend image не устанавливает frontend dependencies.

## 16. Makefile boundaries

Root Makefile только координирует приложения:

```make
backend-test:
	$(MAKE) -C backend test

frontend-test:
	$(MAKE) -C frontend test

backend-run:
	$(MAKE) -C backend run

frontend-run:
	$(MAKE) -C frontend run
```

Component Makefile/script вызывает утверждённый `pnpm` через Corepack; root Makefile не допускает параллельный npm/yarn workflow.

Root targets не объединяют dependency installation в общий workspace.

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

API contract changes должны запускать как backend contract checks, так и frontend generated-client/type checks.

Repository-level Compose/deployment changes запускают соответствующие integration pipelines.

## 18. Configuration and secrets

Backend config содержит DB, auth/session secrets, logging, server и worker settings и загружается только backend bootstrap.

Frontend config содержит только public values. Любая переменная, попадающая в frontend bundle, считается публичной.

Запрещено:

- использовать backend `.env` как frontend env file;
- передавать JWT signing key, DB credentials или encryption secrets в frontend build arguments;
- читать `os.Getenv` из backend Domain/Application;
- хранить реальные `.env` в Git.

## 19. Generated artifacts

Generated files имеют стандартный заголовок и воспроизводимую generation command.

Допустимые места:

```text
backend/internal/modules/<module>/infrastructure/postgres/generated/
frontend/src/shared/api/generated/
```

Build outputs (`backend/bin`, `frontend/dist`, coverage, node_modules) не commit-ятся, если отдельное решение не требует иного.

## 20. Dependency enforcement

Backend checks:

- `go test ./...` из `backend/`;
- `go vet ./...`;
- static analysis;
- architecture import tests;
- migration validation.

Frontend checks:

- immutable/frozen install выбранного package manager;
- typecheck;
- lint;
- unit/component tests;
- production build;
- import-boundary lint.

Architecture checks подтверждают:

- Domain/Application import rules;
- modules не импортируют concrete infrastructure другого module;
- `shared` не импортирует `modules`;
- Delivery не импортирует PostgreSQL packages;
- backend source не зависит от `frontend/`;
- frontend source не зависит от `backend/`;
- отсутствуют вложенные application roots `backend/frontend` и `frontend/backend`.

## 21. Порядок добавления feature

Backend feature:

1. определить module owner;
2. проверить aggregate/transaction boundary;
3. добавить Domain/Application;
4. добавить orchestration только для multi-owner transaction;
5. добавить adapters и delivery;
6. синхронизировать API, Database Design и migrations;
7. добавить tests.

Frontend feature:

1. определить page/widget/feature/entity ownership;
2. использовать contract из `05-api-design.md`;
3. разместить код только под `frontend/src`;
4. не переносить backend aggregate logic во frontend;
5. добавить cancellation/error states;
6. добавить tests;
7. обновить generated client воспроизводимой командой при его наличии.

## 22. Запрещённые структуры

```text
backend/frontend/
backend/web/
backend/client/
frontend/backend/
web/
client/
backend/internal/handlers/
backend/internal/services/
backend/internal/repositories/
backend/internal/models/
backend/internal/utils/
frontend/src/services/
frontend/src/helpers/
```

`frontend/src/components/` как общий бесконтекстный root запрещён; UI размещается в `shared/ui`, entity UI, feature UI или widgets.

Также запрещены скрытые transaction boundaries, direct DB access из frontend, использование DB models как API DTO и shared package как свалка.

## 23. Remaining structural implementation decisions

Gate E0 утвердил `pnpm` 10.x/Corepack, committed `pnpm-lock.yaml` и OpenAPI-generated client flow. Остаётся выбрать:

1. frontend runtime-config delivery;
2. ownership cross-application smoke/E2E suite;
3. deployment overlays и reverse-proxy implementation;
4. repository-level contract artifact publication;
5. final naming composition layers при обоснованном изменении frontend architecture.

`npm`/`yarn` lockfiles, handwritten parallel API client и source imports между `backend/` и `frontend/` запрещены.

## 24. Definition of Done

Structural change завершено только если:

1. `backend/` и `frontend/` остаются независимыми sibling roots;
2. ни одно приложение не вложено в другое;
3. package/module ownership соответствует Architecture и Domain Model;
4. build contexts остаются `./backend` и `./frontend`;
5. CI paths/working directories синхронизированы;
6. root/component build commands обновлены;
7. config/secrets boundaries сохранены;
8. package manager assumptions согласованы с принятым решением;
9. generated paths и generation commands актуальны;
10. API changes синхронизируют backend contract и frontend client/types;
11. tests каждого приложения запускаются независимо;
12. E2E ownership не дублируется;
13. architecture checks запрещают cross-root source imports;
14. README и документация обновлены.
