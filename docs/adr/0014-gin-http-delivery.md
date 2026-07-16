# ADR-0014: Использование Gin в HTTP delivery-слое

- **Status:** Accepted
- **Date:** 2026-07-16
- **Decision owners:** PharmacyCRM team

## Context

PharmacyCRM предоставляет REST API для frontend-приложения и публичного поиска лекарств. Для HTTP-слоя требуется зрелый Go-фреймворк с маршрутизацией, middleware, JSON binding/validation, группировкой маршрутов и удобным тестированием обработчиков.

При этом выбор HTTP-фреймворка не должен проникать в бизнес-логику, доменную модель, транзакционные сценарии или PostgreSQL-репозитории.

## Decision

Для реализации REST API используется `github.com/gin-gonic/gin`.

Gin применяется исключительно в delivery-слое:

- регистрация маршрутов;
- извлечение path/query/header-параметров;
- JSON binding входных DTO;
- HTTP-аутентификация и middleware;
- преобразование application errors в HTTP status codes;
- формирование JSON-ответов;
- request ID, access logging, recovery, CORS и rate limiting.

`gin.Context` не передаётся в use case, domain или repository слои. Handler обязан извлечь стандартный `context.Context` через `c.Request.Context()` и передать его application-сервису.

Пример границы:

```go
func (h *SaleHandler) ConductSale(c *gin.Context) {
    var request ConductSaleRequest
    if err := c.ShouldBindJSON(&request); err != nil {
        writeValidationError(c, err)
        return
    }

    command, err := request.ToCommand()
    if err != nil {
        writeValidationError(c, err)
        return
    }

    result, err := h.conductSale.Execute(c.Request.Context(), command)
    if err != nil {
        h.errors.Write(c, err)
        return
    }

    c.JSON(http.StatusCreated, ConductSaleResponseFrom(result))
}
```

## Layer boundaries

### Gin-aware code

Gin разрешён только в пакетах HTTP delivery-слоя, например:

```text
internal/modules/sales/delivery/http/
internal/modules/catalog/delivery/http/
internal/platform/httpserver/
```

### Gin-independent code

Следующие слои не импортируют Gin:

```text
application/usecase
domain
repository contracts
postgres repository implementations
transaction manager
workers
```

Use case должен быть вызываем не только из Gin handler, но и из фонового воркера, CLI-команды или интеграционного теста без создания `gin.Context`.

## HTTP DTO and domain separation

HTTP request/response DTO не являются доменными сущностями.

Handler выполняет преобразование:

```text
HTTP request DTO
→ application command/query
→ use case result
→ HTTP response DTO
```

Gin binding tags, JSON field names и HTTP validation rules не должны добавляться в domain structs.

## Router construction

Для production-сервера используется `gin.New()`, а не `gin.Default()`, чтобы middleware подключались явно и были видны в bootstrap-коде.

Минимальная цепочка middleware:

1. recovery;
2. request ID;
3. structured access logging;
4. trusted proxy configuration;
5. security headers;
6. CORS;
7. authentication;
8. authorization;
9. rate limiting для публичных endpoint-ов;
10. metrics/tracing.

Порядок middleware должен быть определён централизованно в `internal/platform/httpserver`.

## Error handling

Handlers не формируют произвольные форматы ошибок. Используется единый error mapper, преобразующий application/domain errors в согласованный API envelope.

Пример категорий:

- validation error → `400 Bad Request`;
- unauthenticated → `401 Unauthorized`;
- forbidden → `403 Forbidden`;
- entity not found → `404 Not Found`;
- conflict/idempotency conflict → `409 Conflict`;
- business rule violation → `422 Unprocessable Entity`;
- concurrency retry exhausted → `409 Conflict` или `503 Service Unavailable` по контракту endpoint-а;
- unexpected error → `500 Internal Server Error` без утечки внутренних деталей.

## Validation

Gin binding используется для синтаксической проверки HTTP-запроса: обязательные поля, формат UUID, допустимая длина строки и базовые ограничения формы.

Бизнес-инварианты проверяются use case/domain-слоем. Например, Gin может проверить, что `quantity > 0`, но право продавать внутреннюю единицу, рецептурность, доступный остаток и FEFO проверяются внутри транзакционного сценария.

## Server lifecycle

Gin router запускается через стандартный `http.Server`, чтобы явно настроить:

- `ReadHeaderTimeout`;
- `ReadTimeout`;
- `WriteTimeout`;
- `IdleTimeout`;
- максимальный размер request body;
- graceful shutdown;
- завершение по отмене application context.

Прямой вызов `router.Run()` не используется в production bootstrap.

## Testing

HTTP handlers тестируются через `httptest` с реальным Gin router в test mode.

Отдельно тестируются:

- маршрутизация;
- binding и validation;
- authentication/authorization middleware;
- error mapping;
- response envelope;
- отсутствие утечки внутренних ошибок;
- propagation отмены `c.Request.Context()`.

Use case тестируется независимо от Gin.

## Alternatives considered

### `net/http` без фреймворка

Отклонено для MVP: обеспечивает максимальный контроль, но потребует больше собственного кода для маршрутизации, binding, middleware и единой обработки ошибок.

### Chi

Подходящий лёгкий router, хорошо совместимый со стандартным `net/http`. Не выбран, поскольку команда предпочитает Gin и уже имеет опыт его использования.

### Echo и Fiber

Также способны реализовать REST API, но не дают достаточного преимущества для смены выбранного и знакомого команде инструмента.

## Consequences

### Positive

- быстрый старт REST API;
- понятная маршрутизация и группировка endpoint-ов;
- зрелая middleware-модель;
- удобное JSON binding и тестирование;
- знакомый команде стек.

### Negative

- требуется дисциплина, чтобы не протащить `gin.Context` в application/domain слои;
- встроенное binding не заменяет бизнес-валидацию;
- глобальные defaults Gin могут скрывать middleware, поэтому bootstrap должен быть явным.

## Invariants

1. `gin.Context` существует только в HTTP delivery-слое.
2. Use case принимает стандартный `context.Context`.
3. Domain и repository packages не импортируют `github.com/gin-gonic/gin`.
4. Handler не содержит складской, финансовой или транзакционной бизнес-логики.
5. Handler не начинает PostgreSQL-транзакцию самостоятельно.
6. Все API errors проходят через единый mapper.
7. Production-сервер использует явно настроенный `http.Server` и graceful shutdown.
8. Все значения цены, остатка и итоговой суммы повторно определяются backend use case-ом, а не принимаются на доверии из HTTP DTO.
