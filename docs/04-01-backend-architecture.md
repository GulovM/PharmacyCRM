# PharmacyCRM — Backend Architecture

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-16

## 1. Назначение

Документ фиксирует структуру Go-бэкенда PharmacyCRM, границы модулей, правила сборки зависимостей и способ интеграции Gin, application use cases, Unit of Work и PostgreSQL-репозиториев.

## 2. Архитектурный стиль

Backend реализуется как модульный монолит.

Каждый бизнес-модуль владеет своими доменными моделями, use case-ами, HTTP delivery-слоем и реализациями репозиториев. Межмодульная оркестрация допускается только через явно опубликованные application-интерфейсы, команды, запросы или transaction-scoped контракты.

Прямой доступ одного модуля к приватным репозиториям другого модуля запрещён.

## 3. Предлагаемая структура

```text
backend/
├── cmd/
│   ├── api/
│   │   └── main.go
│   └── worker/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── application.go
│   │   ├── config.go
│   │   ├── modules.go
│   │   └── routes.go
│   ├── platform/
│   │   ├── config/
│   │   ├── database/
│   │   │   ├── transactor.go
│   │   │   └── postgres/
│   │   ├── httpserver/
│   │   ├── logging/
│   │   ├── observability/
│   │   └── validation/
│   ├── shared/
│   │   ├── kernel/
│   │   ├── errors/
│   │   └── authcontext/
│   └── modules/
│       ├── auth/
│       ├── catalog/
│       ├── pharmacy/
│       ├── inventory/
│       ├── sales/
│       ├── discovery/
│       ├── recommendation/
│       └── audit/
├── migrations/
├── test/
├── go.mod
└── Makefile
```

## 4. Структура отдельного модуля

```text
internal/modules/sales/
├── domain/
│   ├── entities.go
│   ├── value_objects.go
│   ├── errors.go
│   └── services.go
├── application/
│   ├── commands/
│   ├── queries/
│   ├── ports.go
│   └── service.go
├── infrastructure/
│   └── postgres/
│       ├── repository.go
│       └── queries.go
└── delivery/
    └── http/
        ├── handler.go
        ├── dto.go
        ├── mapper.go
        └── routes.go
```

Не каждый модуль обязан содержать все каталоги. Структура добавляется по необходимости, но направление зависимостей сохраняется.

## 5. Направление зависимостей

Допустимое направление:

```text
Gin handler
    ↓
application command/query
    ↓
domain rules and ports
    ↓
infrastructure implementation
```

Правила:

1. `domain` не импортирует Gin, pgx, PostgreSQL DTO или HTTP DTO.
2. `application` не импортирует Gin и pgx.
3. `infrastructure/postgres` может импортировать pgx и application/domain-контракты своего модуля.
4. `delivery/http` может импортировать Gin и application-контракты.
5. `app` является composition root и имеет право знать о конкретных реализациях.
6. `platform` не должен владеть бизнес-интерфейсами модулей.

## 6. Composition Root

Все concrete dependencies собираются только в `internal/app`.

`cmd/api/main.go` выполняет минимальный bootstrap:

```text
load config
→ initialize logger
→ open pgx pool
→ initialize repositories and transactor
→ construct module services and use cases
→ construct Gin handlers
→ register routes and middleware
→ start http.Server
→ graceful shutdown
```

Бизнес-модули не используют глобальные service locator-ы и не читают зависимости из package-level variables.

## 7. Dependency Injection

Для первой версии используется ручной constructor-based DI.

Причины:

- граф зависимостей остаётся явным;
- отсутствует runtime reflection;
- ошибки сборки обнаруживаются компилятором;
- приложение пока недостаточно велико для обязательного DI-фреймворка;
- тесты могут напрямую подставлять fake- и mock-реализации.

Wire, Dig или другой контейнер не вводятся до подтверждённой необходимости.

## 8. Unit of Work и репозитории

ADR-0013 требует явный Unit of Work.

При этом интерфейсы бизнес-репозиториев должны оставаться в модулях, которые ими владеют. Платформенный пакет транзакций не должен объявлять пустые `SalesTxRepository`, `InventoryTxRepository` и другие бизнес-типы.

Предпочтительный контракт application-слоя продажи:

```go
type SaleUnitOfWork interface {
    PharmacyProducts() PharmacyProductTxRepository
    Inventory() InventoryTxRepository
    Sales() SalesTxRepository
}

type SaleTransactor interface {
    WithinTransaction(
        ctx context.Context,
        fn func(ctx context.Context, uow SaleUnitOfWork) error,
    ) error
}
```

Конкретная PostgreSQL-реализация находится в composition/infrastructure-слое и связывает эти интерфейсы с одним `pgx.Tx`.

Так platform/database предоставляет низкоуровневые примитивы транзакций, но не зависит от модулей продаж, склада или аптек.

## 9. Реализация PostgreSQL Unit of Work

Репозитории создаются один раз при создании transaction-scoped Unit of Work, а не при каждом вызове accessor-метода.

```go
type pgxSaleUnitOfWork struct {
    pharmacyProducts PharmacyProductTxRepository
    inventory        InventoryTxRepository
    sales            SalesTxRepository
}

func newPgxSaleUnitOfWork(tx pgx.Tx) *pgxSaleUnitOfWork {
    return &pgxSaleUnitOfWork{
        pharmacyProducts: pharmacyrepo.NewTxRepository(tx),
        inventory:        inventoryrepo.NewTxRepository(tx),
        sales:            salesrepo.NewTxRepository(tx),
    }
}
```

Accessor-методы возвращают уже созданные репозитории. Это исключает лишние аллокации и гарантирует стабильную идентичность зависимостей внутри callback.

## 10. Retry Policy

Повторяется вся транзакционная callback-операция только для PostgreSQL SQLSTATE:

- `40P01` — deadlock detected;
- `40001` — serialization failure.

Правила:

1. Backoff должен учитывать `context.Context`; обычный `time.Sleep` запрещён.
2. Количество попыток ограничено.
3. Используется jitter, чтобы параллельные транзакции не повторялись синхронно.
4. UUID, idempotency key и стабильные входные данные создаются до callback.
5. В callback запрещены внешние HTTP-вызовы, отправка email, публикация в брокер и другие необратимые side effects.
6. Ошибка `Commit` также анализируется как возможная retryable database error.
7. Ошибка rollback не заменяет исходную бизнес-ошибку, но должна логироваться.

Пример context-aware ожидания:

```go
func waitRetry(ctx context.Context, delay time.Duration) error {
    timer := time.NewTimer(delay)
    defer timer.Stop()

    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-timer.C:
        return nil
    }
}
```

## 11. Gin HTTP Layer

Gin используется только в `delivery/http` и `platform/httpserver`.

Handler обязан:

1. считать path/query/header/body;
2. выполнить transport validation;
3. преобразовать DTO в application command/query;
4. передать `c.Request.Context()`;
5. вызвать use case;
6. преобразовать результат или типизированную ошибку в HTTP response.

Handler не должен:

- выполнять FEFO;
- рассчитывать окончательные цены;
- открывать транзакцию;
- обращаться напрямую к pgx;
- обновлять остатки;
- содержать SQL;
- передавать `*gin.Context` в application/domain.

## 12. HTTP Server Bootstrap

Production server создаётся через `gin.New()` и `http.Server` с явными таймаутами:

```go
server := &http.Server{
    Addr:              cfg.HTTP.Address,
    Handler:           router,
    ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
    ReadTimeout:       cfg.HTTP.ReadTimeout,
    WriteTimeout:      cfg.HTTP.WriteTimeout,
    IdleTimeout:       cfg.HTTP.IdleTimeout,
}
```

Middleware подключаются явно:

- request ID;
- panic recovery;
- structured access logging;
- authentication;
- authorization;
- CORS;
- body-size limit;
- rate limiting при подтверждённой необходимости;
- metrics and tracing.

## 13. Модульное владение таблицами

Предварительное владение:

- `catalog`: `products`, `product_presentations`, `product_barcodes`, catalog staging;
- `pharmacy`: `pharmacies`, назначения аптекарей, `pharmacy_products`;
- `inventory`: `receipts`, `receipt_items`, `stock_lots`, `inventory_operations`, `inventory_movements`, write-offs and corrections;
- `sales`: `sales`, `sale_items`, `sale_item_allocations`, `sale_returns`, `sale_return_items`, `sale_return_item_allocations`;
- `auth`: users, credentials, sessions and roles;
- `audit`: security and administrative audit records.

Межмодульная транзакция продажи является application orchestration. Она может использовать transaction-scoped порты нескольких модулей, но конкретный SQL остаётся внутри инфраструктуры владельца таблицы.

## 14. Миграции

Миграции хранятся централизованно в `backend/migrations`, поскольку PostgreSQL-схема разворачивается как единое целое.

При этом имя и содержание каждой миграции должны позволять определить модуль-владелец изменяемых таблиц.

Пример:

```text
20260716120000_catalog_core.sql
20260716121000_pharmacy_core.sql
20260716122000_inventory_core.sql
20260716123000_sales_core.sql
```

Миграции не должны создавать циклически неразрешимые зависимости. При необходимости внешние ключи между модулями добавляются отдельной интеграционной миграцией после создания базовых таблиц.

## 15. Тестирование

- domain: table-driven unit tests без БД;
- application: unit tests с fake ports и fake transactor;
- repository: PostgreSQL integration tests;
- transaction scenarios: реальные конкурентные integration tests;
- HTTP handlers: `httptest` + Gin test mode;
- end-to-end: поднятое приложение и PostgreSQL в контейнерах.

Особенно обязательны тесты:

- две конкурентные продажи одного товара;
- многострочные чеки с обратным порядком товаров;
- недостаточный остаток;
- повтор idempotency key;
- deadlock retry;
- частичные конкурентные возвраты;
- rollback при ошибке вставки движения;
- отсутствие записей после неуспешного проведения.

## 16. Следующий этап

После фиксации этого документа можно создавать первые миграции в следующем порядке:

1. базовые extensions and utility functions;
2. catalog core;
3. pharmacy core;
4. inventory receipts and lots;
5. sales and allocations;
6. return allocations;
7. append-only permissions and reconciliation indexes.
