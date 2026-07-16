# ADR-0013: Явные транзакционные границы через Unit of Work

- **Status:** Accepted
- **Date:** 2026-07-16
- **Decision owners:** PharmacyCRM team

## Context

Сценарии продажи, поступления, списания и возврата изменяют несколько связанных агрегатов и таблиц в одной транзакции PostgreSQL. Например, проведение продажи одновременно создаёт чек и строки, блокирует ассортимент аптеки, выполняет FEFO-аллокацию, уменьшает остатки лотов, создаёт складскую операцию и append-only движения.

Один из распространённых способов передать `pgx.Tx` репозиториям — сохранить транзакцию в `context.Context`. Такой подход сокращает количество параметров, но делает транзакцию скрытой зависимостью:

- из сигнатуры метода не видно, требует ли он активную транзакцию;
- один и тот же репозиторий может незаметно работать то через pool, то через tx;
- ошибочный вызов вне транзакции может частично провести бизнес-операцию;
- доменный код начинает зависеть от соглашения о значении внутри `context.Context`;
- сложнее тестировать границы commit/rollback и запрещать вложенные транзакции.

`context.Context` предназначен для отмены, дедлайнов, трассировки и request-scoped metadata, но не должен использоваться как контейнер обязательных инфраструктурных зависимостей.

## Decision

Транзакционные сценарии PharmacyCRM используют явный паттерн **Unit of Work**.

1. Application/use-case слой вызывает `Transactor.WithinTransaction`.
2. Callback получает transaction-scoped `UnitOfWork` с репозиториями, привязанными к одному `pgx.Tx`.
3. Методы, требующие блокировок или участия в общей атомарной операции, доступны только через транзакционные репозитории `UnitOfWork`.
4. Read-only запросы вне транзакции выполняются обычными query services/repositories через `pgxpool.Pool`.
5. `context.Context` передаётся во все методы только для отмены, дедлайнов и трассировки; `pgx.Tx` из него не извлекается.
6. Use case не импортирует `pgx`, `pgxpool` или типы конкретной СУБД.

Пример прикладного интерфейса:

```go
type Transactor interface {
    WithinTransaction(
        ctx context.Context,
        opts TxOptions,
        fn func(ctx context.Context, uow UnitOfWork) error,
    ) error
}

type UnitOfWork interface {
    PharmacyProducts() PharmacyProductTxRepository
    Inventory() InventoryTxRepository
    Sales() SalesTxRepository
    Returns() ReturnsTxRepository
}
```

`TxOptions` является собственным типом приложения и не раскрывает `pgx.TxOptions` наружу.

```go
type IsolationLevel string

const (
    ReadCommitted IsolationLevel = "READ_COMMITTED"
)

type TxOptions struct {
    Isolation IsolationLevel
    RetryMode RetryMode
}
```

Инфраструктурная реализация создаёт `pgx.Tx`, конструирует transaction-scoped репозитории на общем интерфейсе выполнения запросов и передаёт готовый `UnitOfWork` callback-функции.

## Query executor

Чтобы не дублировать SQL-репозитории для pool и tx, инфраструктура может использовать минимальный внутренний интерфейс:

```go
type DBTX interface {
    Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

Этот интерфейс остаётся внутри PostgreSQL-адаптера. Application и domain слои его не импортируют.

## Transaction ownership

Только верхнеуровневый use case владеет транзакцией.

- Репозитории не вызывают `BEGIN`, `COMMIT` или `ROLLBACK`.
- Вложенный use case, участвующий в операции, получает уже существующий `UnitOfWork` или вызывается через внутренний application service с явным transaction scope.
- Внешние HTTP-вызовы, отправка email, обращения к картографическому API и другие медленные I/O запрещены внутри транзакции.
- События для внешней доставки в будущем записываются в transactional outbox в той же транзакции, но публикуются после commit отдельным воркером.

## Retry policy

Повторяется вся callback-функция транзакции, а не отдельный SQL-запрос.

Retry разрешён только для транзиентных ошибок PostgreSQL:

- `40P01` — deadlock detected;
- `40001` — serialization failure.

Политика MVP:

- не более трёх попыток;
- bounded exponential backoff с jitter;
- немедленная остановка при отменённом `context.Context`;
- отсутствие retry для validation, conflict, insufficient stock и других доменных ошибок.

Callback обязан быть повторяемым:

- не выполнять внешние необратимые действия;
- использовать устойчивый idempotency key;
- не генерировать разные бизнес-идентификаторы на каждой попытке, если это меняет семантику операции;
- не изменять состояние процесса вне PostgreSQL до успешного commit.

## Locking methods

Методы с `FOR UPDATE` существуют только в transaction-scoped интерфейсах. Например:

```go
type PharmacyProductTxRepository interface {
    LockByIDsOrdered(ctx context.Context, ids []uuid.UUID) ([]PharmacyProduct, error)
}
```

Реализация обязана:

1. удалить дубликаты ID;
2. отсортировать ID детерминированно до SQL;
3. блокировать все строки в одном порядке;
4. проверить, что количество найденных строк совпадает с количеством запрошенных;
5. не допускать silent fallback на auto-commit.

## Alternatives considered

### Транзакция в `context.Context`

Отклонено из-за скрытой зависимости, возможности случайного auto-commit и слабой выразительности контрактов репозитория.

### Передача `pgx.Tx` параметром каждого метода

Технически надёжно, но протекает инфраструктурный тип в application слой и создаёт избыточное связывание с PostgreSQL-драйвером.

### Транзакция внутри каждого репозитория

Отклонено, потому что одна бизнес-операция использует несколько репозиториев и должна иметь единую границу commit/rollback.

### Распределённые события между модулями вместо общей транзакции

Отклонено для критического складского контура MVP: продажа, текущий остаток и ledger должны быть атомарно согласованы в PostgreSQL.

## Consequences

### Положительные

- транзакционная зависимость видна в use case;
- невозможно случайно выполнить lock-метод через pool;
- все модульные репозитории используют один `pgx.Tx`;
- проще тестировать rollback, retry и порядок блокировок;
- application слой не зависит от pgx;
- переход к transactional outbox не потребует смены базовой модели транзакций.

### Отрицательные

- требуется отдельный `UnitOfWork` и набор tx-repository интерфейсов;
- часть инфраструктурного wiring становится более объёмной;
- необходимо дисциплинированно разделять query repositories и transaction-scoped command repositories.

## Invariants

1. Одна команда проведения документа имеет ровно одного владельца транзакции.
2. Все изменения продажи, лотов, аллокаций, операций и движений выполняются через один `UnitOfWork`.
3. Lock-методы не работают вне активной транзакции.
4. Репозитории не открывают самостоятельные вложенные транзакции.
5. Повтор транзакции повторяет всю бизнес-операцию.
6. В транзакции отсутствуют внешние необратимые side effects.
7. Ошибка commit считается ошибкой операции; успешный результат нельзя возвращать до подтверждённого commit.
8. Panic внутри callback приводит к rollback и затем пробрасывается после безопасного завершения rollback.
9. Ошибка rollback не скрывает исходную ошибку, но фиксируется в структурированном логе.
10. Транзакционный callback не сохраняет `UnitOfWork` или его репозитории для использования после завершения callback.

## Testing requirements

Необходимы интеграционные тесты с реальным PostgreSQL:

- commit всех связанных изменений;
- rollback при ошибке на каждом этапе проведения;
- отсутствие частичных записей;
- повтор при `40P01`/`40001`;
- отсутствие retry для доменных ошибок;
- детерминированная блокировка нескольких `pharmacy_product_id`;
- конкурентные продажи одного товара;
- конкурентные возвраты одной продажи;
- невозможность использовать transaction-scoped repository после завершения транзакции.
