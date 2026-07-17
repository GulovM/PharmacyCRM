# ADR-0016: Централизованное сопоставление ошибок и композиция HTTP middleware

- **Status:** Accepted
- **Date:** 2026-07-17
- **Decision owners:** PharmacyCRM team

## Context

HTTP handlers PharmacyCRM должны преобразовывать application/domain errors в стабильные HTTP-ответы. Если каждый handler выполняет это вручную, быстро появляются расхождения:

- одинаковая ошибка получает разные status code;
- различаются response envelope и уровень логирования;
- внутренние детали случайно попадают клиенту;
- разработчики сравнивают ошибки по тексту или через `==`;
- recovery и обычные ошибки формируют несовместимые ответы;
- порядок middleware становится неявным и различается между группами маршрутов.

Проект уже использует Gin как HTTP delivery framework и Zap как технический logger. Требуется единый механизм классификации ошибок, формирования ответа, логирования и подключения middleware.

## Decision

### 1. Сравнение ошибок

Для проверки принадлежности ошибки к стабильной категории используется только `errors.Is()`.

Запрещается определять категорию ошибки через:

- сравнение `err == sentinel`, если ошибка могла быть обёрнута;
- сравнение `err.Error()`;
- поиск подстроки в сообщении;
- switch по тексту ошибки;
- проверку строк драйвера PostgreSQL вне инфраструктурного mapper.

Ошибки могут оборачиваться через `%w`, сохраняя цепочку причин.

Пример:

```go
if err != nil {
    return fmt.Errorf("conduct sale: %w", err)
}
```

После такого оборачивания delivery продолжает корректно определять категорию через `errors.Is()`.

### 2. Стабильные категории ошибок

Во внутренних слоях определяются sentinel errors или ошибки, поддерживающие `Is`-семантику. Минимальные категории:

```go
var (
    ErrInvalidArgument  = errors.New("invalid argument")
    ErrUnauthenticated  = errors.New("unauthenticated")
    ErrForbidden        = errors.New("forbidden")
    ErrNotFound         = errors.New("not found")
    ErrConflict         = errors.New("conflict")
    ErrBusinessRule     = errors.New("business rule violation")
    ErrUnavailable      = errors.New("service unavailable")
    ErrInternal         = errors.New("internal error")
)
```

Модули могут иметь более точные ошибки, если они корректно сопоставляются с одной из стабильных категорий через wrapping или реализацию `Is(error) bool`.

Текст ошибки предназначен для разработчика и логов, но не является публичным API-контрактом.

### 3. Централизованный HTTP error mapper

В delivery-слое создаётся единый компонент, концептуально `HTTPErrorResponder` или `ErrorMapper`. Он отвечает за:

- классификацию ошибки через `errors.Is()`;
- выбор HTTP status code;
- выбор стабильного публичного error code;
- выбор безопасного сообщения клиенту;
- выбор уровня Zap-логирования;
- формирование единого response envelope;
- добавление `request_id` и, при наличии, `trace_id`;
- недопущение утечки внутренних деталей.

Handlers не должны содержать повторяющиеся switch-блоки по ошибкам.

Концептуальный контракт:

```go
type HTTPErrorResponder interface {
    WriteError(c *gin.Context, err error, operation string)
    WritePanic(c *gin.Context, recovered any, operation string)
}
```

Допустима реализация структурой с logger и factory безопасных ответов:

```go
type ErrorResponder struct {
    log *zap.Logger
}
```

Logger не должен хранить `gin.Context` или `http.ResponseWriter` внутри долгоживущего объекта. Контекст конкретного запроса передаётся в метод явно. Это предотвращает случайное переиспользование request-scoped состояния и упрощает тестирование.

### 4. Нормативное сопоставление ошибок

Минимальная таблица:

| Error category | HTTP status | Log level | Public code |
|---|---:|---|---|
| `ErrInvalidArgument` | 400 | Warn | `INVALID_ARGUMENT` |
| `ErrUnauthenticated` | 401 | Info или Warn | `UNAUTHENTICATED` |
| `ErrForbidden` | 403 | Warn | `FORBIDDEN` |
| `ErrNotFound` | 404 | Debug или Info | `NOT_FOUND` |
| `ErrConflict` | 409 | Warn | `CONFLICT` |
| `ErrBusinessRule` | 422 | Info или Warn | `BUSINESS_RULE_VIOLATION` |
| `ErrUnavailable` | 503 | Error или Warn | `SERVICE_UNAVAILABLE` |
| неизвестная ошибка | 500 | Error | `INTERNAL_ERROR` |

Конкретное публичное сообщение может уточняться endpoint-ом, но не должно раскрывать:

- SQL и параметры запросов;
- имена таблиц и constraints;
- stack trace;
- filesystem paths;
- секреты и токены;
- внутренние адреса сервисов;
- необработанный текст panic;
- текст ошибки внешнего драйвера.

### 5. Единый error envelope

API design должен определить окончательную форму. Базовая архитектурная форма:

```json
{
  "success": false,
  "error": {
    "code": "NOT_FOUND",
    "message": "resource not found",
    "request_id": "..."
  }
}
```

Поле с `err.Error()` не возвращается клиенту автоматически. Полная ошибка пишется только в защищённый технический лог с request/trace context.

В development допустим дополнительный диагностический detail только через явно включённую конфигурацию и только после sanitization. Production не возвращает внутренние ошибки.

### 6. Успешные ответы

Для JSON и `204 No Content` используется единый response writer/helper, чтобы handlers не повторяли заголовки и сериализацию.

Концептуально:

```go
type Responder interface {
    JSON(c *gin.Context, status int, body any)
    NoContent(c *gin.Context)
    Error(c *gin.Context, err error, operation string)
}
```

При ошибке сериализации ответа:

- ошибка логируется через Zap;
- нельзя пытаться повторно записать другой HTTP status, если headers уже отправлены;
- метрика response encoding failure увеличивается;
- соединение завершается в соответствии с возможностями `net/http`/Gin.

### 7. Panic recovery

Recovery middleware использует тот же responder для формирования безопасного `500`.

Recovery обязан:

- перехватить panic;
- создать внутреннюю error-обёртку;
- залогировать panic, stack trace, request ID и trace ID;
- не возвращать клиенту значение panic;
- не продолжать цепочку handler после panic;
- вернуть единый `INTERNAL_ERROR` envelope, если ответ ещё не был отправлен.

Panic не используется как обычный механизм управления бизнес-ошибками.

### 8. Композиция middleware

Middleware подключаются централизованно как упорядоченный срез. Для Gin используется его нативный тип:

```go
func MiddlewareChain(deps Dependencies) []gin.HandlerFunc {
    return []gin.HandlerFunc{
        RequestID(deps.IDGenerator),
        Recovery(deps.Logger, deps.Responder),
        Tracing(deps.Tracer),
        AccessLog(deps.Logger),
        TrustedProxy(deps.ProxyConfig),
        SecurityHeaders(),
        CORS(deps.CORSConfig),
        RequestBodyLimit(deps.Limits),
        RateLimit(deps.RateLimiter),
    }
}
```

Подключение выполняется одной операцией:

```go
router := gin.New()
router.Use(MiddlewareChain(deps)...)
```

Middleware доступа, зависящие от группы маршрутов, подключаются дополнительными срезами:

```go
protected := router.Group("/api/v1")
protected.Use(Authentication(deps.Auth))

pharmacy := protected.Group("/pharmacies/:pharmacyID")
pharmacy.Use(PharmacyAccess(deps.Authorization))
```

Порядок элементов в срезе является нормативным и тестируется.

### 9. Совместимость с `net/http`

Общий алгоритм цепочки, приведённый ниже, корректен для middleware типа `func(http.Handler) http.Handler`:

```go
type Middleware func(http.Handler) http.Handler

func ChainMiddleware(h http.Handler, middleware ...Middleware) http.Handler {
    for i := len(middleware) - 1; i >= 0; i-- {
        h = middleware[i](h)
    }
    return h
}
```

Однако основной HTTP delivery PharmacyCRM использует Gin, поэтому нельзя смешивать `gin.HandlerFunc` и `net/http` middleware без явного адаптера. Нативная Gin-композиция через `router.Use(slice...)` является базовым решением.

`net/http` chain может применяться для внешнего wrapper вокруг Gin engine или отдельного технического endpoint только при явной необходимости.

### 10. Порядок выполнения middleware

Базовый порядок:

1. request ID;
2. recovery;
3. tracing;
4. access logging;
5. trusted proxy processing;
6. security headers;
7. CORS;
8. request body limit;
9. rate limiting;
10. authentication;
11. route-specific authorization;
12. handler.

Request ID должен быть доступен recovery, tracing и logging. Recovery должен охватывать все последующие middleware и handler. Access logger должен измерять полную длительность последующей цепочки и видеть итоговый status code.

Допускается корректировка порядка только через ADR, если изменение влияет на безопасность, наблюдаемость или семантику ответа.

### 11. Logging rules

Error responder логирует минимум:

- operation/message;
- error через `zap.Error(err)`;
- HTTP method;
- route pattern;
- status code;
- request ID;
- trace ID;
- user ID и pharmacy ID при наличии;
- duration, если её предоставляет access logger.

Одна ошибка не должна без необходимости логироваться одинаково на каждом слое. Базовое правило:

- Domain возвращает ошибку без логирования;
- Application добавляет контекст через `%w`, но обычно не логирует ожидаемую ошибку;
- Delivery логирует окончательное HTTP-сопоставление;
- Infrastructure логирует только там, где добавляет технически значимый контекст или выполняет retry, не создавая дублирования.

### 12. PostgreSQL errors

Ошибки `pgx` и PostgreSQL классифицируются внутри infrastructure. Application и delivery не сравнивают коды PostgreSQL напрямую.

Infrastructure переводит, например:

- unique violation в `ErrConflict`;
- foreign key violation в подходящую стабильную категорию;
- check violation в `ErrBusinessRule` или `ErrConflict`;
- no rows в `ErrNotFound`, когда это соответствует контракту репозитория;
- connection failure в `ErrUnavailable`;
- неизвестную ошибку сохраняет как внутреннюю обёртку.

Исходная ошибка сохраняется через `%w`, чтобы лог содержал техническую причину.

## Testing

Обязательны table-driven tests для mapper:

- каждая стабильная ошибка;
- ошибка, обёрнутая один и несколько раз;
- неизвестная ошибка;
- отсутствие утечки внутреннего текста;
- корректный log level;
- наличие request ID;
- panic response;
- уже начатый HTTP response;
- ошибка сериализации ответа.

Для middleware тестируются:

- нормативный порядок;
- request ID доступен в последующих middleware;
- recovery перехватывает panic;
- access logger видит итоговый status;
- authentication и authorization не применяются к публичным маршрутам;
- chain не теряет middleware и не вызывает handler более одного раза.

## Consequences

### Positive

- единое сопоставление ошибок во всём API;
- безопасные production-ответы;
- корректная работа с wrapped errors;
- отсутствие повторяющихся switch в handlers;
- централизованный выбор log level;
- явный и тестируемый порядок middleware;
- возможность менять API envelope без переписывания всех handlers.

### Negative

- требуется поддерживать стабильную taxonomy ошибок;
- слишком общие sentinel errors могут скрыть значимый контекст, поэтому сообщения и wrapping остаются обязательными;
- Gin middleware нельзя напрямую использовать как `net/http` middleware без адаптации;
- responder становится критическим компонентом delivery и требует хорошего тестового покрытия.

## Invariants

1. Категория ошибки определяется через `errors.Is()`.
2. Сравнение по тексту ошибки запрещено.
3. Handlers не содержат собственные повторяющиеся error-to-status switch.
4. Неизвестная ошибка возвращает безопасный `500` и логируется на уровне Error.
5. Полный `err.Error()` не возвращается клиенту автоматически.
6. Panic использует единый безопасный response envelope.
7. Middleware собираются централизованно в упорядоченный срез.
8. Для Gin применяется `router.Use(middleware...)`.
9. Изменение нормативного порядка middleware требует архитектурного обоснования.
10. Ошибки PostgreSQL переводятся в стабильные категории в infrastructure, а не в handler.