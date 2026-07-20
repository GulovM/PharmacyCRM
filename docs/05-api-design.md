# PharmacyCRM — API Design

**Статус документа:** Draft  
**Версия:** 1.2  
**Дата:** 2026-07-20  
**Связанные документы:** `01-product-vision.md`, `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `06-database-design.md`  
**Связанные ADR:** ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0016, ADR-0017

## 1. Назначение и нормативная роль

Этот документ является единым человекочитаемым каталогом HTTP API PharmacyCRM. Он определяет общие правила API, форматы запросов и ответов, модель ошибок, authentication, authorization, пагинацию, конкурентное обновление, идемпотентность, асинхронные операции и перечень планируемых endpoint-ов.

Документ предназначен для backend-, frontend-, QA- и security-разработки. Frontend не должен изучать handlers, use cases, repository или SQL для восстановления внешнего контракта.

При противоречии применяется следующий приоритет:

1. применимое законодательство и обязательные регуляторные требования;
2. `docs/01-product-vision.md`;
3. `docs/02-srs.md`;
4. принятые ADR;
5. этот документ;
6. реализация и тесты.

Код не может молча расходиться с этим документом. Изменение URL, method, request/response DTO, status code, error code, authorization или side effects должно обновлять `05-api-design.md` в том же change set.

## 2. Границы API

API разделяется на три контура:

1. **Public API** — публичный поиск лекарств и получение публичных данных активных аптек без авторизации.
2. **Protected API** — функции `CLIENT`, `PHARMACIST`, `ADMIN` и возможных будущих служебных ролей.
3. **Operational API** — health, readiness, metrics и иные технические endpoint-ы, не являющиеся frontend-контрактом.

MVP использует REST-подобный JSON API поверх HTTPS. Gin применяется только в HTTP delivery-слое. `gin.Context` не передаётся в application, domain и repository.

API не предоставляет общий CRUD для проведённых документов, складских движений и аудита. Для них используются предметные команды и read-only представления.

## 3. Base URL, naming и версионирование

Базовый путь бизнес-API:

```text
/api/v1
```

Технические endpoint-ы не входят в `/api/v1`:

```text
GET /healthz
GET /readyz
```

Правила naming:

- paths используют kebab-case и существительные во множественном числе;
- path parameters используют snake_case: `{pharmacy_id}`;
- JSON-поля и query parameters используют snake_case;
- команды, которые нельзя корректно выразить CRUD-семантикой, оформляются action endpoint-ами: `/archive`, `/confirm`, `/reverse`;
- action endpoint не должен дублировать обычный `PATCH`, если операция является простым изменением разрешённых полей.

Несовместимыми считаются удаление или переименование поля, изменение его типа или nullable-семантики, добавление обязательного request-поля, удаление enum-значения, изменение authorization, side effects или семантики status code. Такое изменение требует новой версии либо формального deprecation-периода.

Добавление необязательного response-поля обычно обратно совместимо, однако клиент обязан игнорировать неизвестные поля.

## 4. Content negotiation, кодировка и локализация

Для JSON:

```http
Content-Type: application/json
Accept: application/json
```

Кодировка — UTF-8. JSON должен быть синтаксически строгим: trailing comma и комментарии запрещены. Неизвестные request-поля по умолчанию отклоняются кодом `UNKNOWN_FIELD`, если endpoint явно не разрешает forward-compatible payload.

Файловые загрузки используют `multipart/form-data`. Выгрузки могут возвращать JSON, CSV или XLSX согласно endpoint-контракту и `Accept`.

- неподдерживаемый `Accept` → `406 NOT_ACCEPTABLE`;
- превышение лимита body/file → `413 PAYLOAD_TOO_LARGE`;
- неподдерживаемый `Content-Type` → `415 UNSUPPORTED_MEDIA_TYPE`.

Стабильным контрактом является `error.code`, а не локализованный `message`. На первом этапе server messages могут быть английскими. Frontend должен локализовать известные коды самостоятельно. Позднее `Accept-Language` может использоваться только как обратно совместимое расширение.

## 5. Идентификаторы и базовые типы

### 5.1 Идентификаторы

Внешние ID передаются как строки. Клиент не должен предполагать внутренний тип БД, последовательность или возможность арифметики над ID.

```json
{"id":"01JZX3E2J9Q4JY3N8VY6F7XH2A"}
```

Невалидный формат ID возвращает `400 INVALID_ARGUMENT`; корректно сформированный, но отсутствующий или скрытый ресурс — `404 NOT_FOUND`.

### 5.2 Даты и время

- дата: `YYYY-MM-DD`;
- datetime: RFC 3339 с timezone;
- duration, если потребуется: целое число секунд с суффиксом `_seconds`.

Серверное время является источником истины для `created_at`, `posted_at`, `completed_at`, аудита и иных значимых временных меток. Клиентское время не подменяет время проведения операции.

### 5.3 Деньги

Все денежные значения передаются целыми числами в дирамах:

```json
{"amount_dirams":12550,"currency":"TJS"}
```

`float` для денег запрещён. В MVP допустима только `TJS`, если endpoint не говорит иное. Backend рассчитывает итоговые цены, скидки и refund; frontend не является источником итоговой суммы.

### 5.4 Количества и единицы

Остатки передаются в целых базовых единицах. Имя поля обязано отражать единицу:

- `quantity_base_units`;
- `quantity_packages`;
- `base_units_per_package`;
- `display_quantity` вместе с `sale_unit`.

Поддерживаемые `sale_unit` MVP: `PACKAGE`, `INNER_UNIT`. Неизвестное значение enum отклоняется.

### 5.5 Nullable, omitted и empty

- отсутствующее поле в `PATCH` означает «не изменять»;
- `null` означает явную очистку только когда это разрешено;
- пустая строка не эквивалентна `null`;
- обязательное non-nullable поле не может отсутствовать или быть `null`;
- response не должен произвольно менять поле между omitted и `null` без изменения контракта.

## 6. Общие HTTP-заголовки

### 6.1 Request

| Header | Обязательность | Назначение |
|---|---|---|
| `Authorization` | protected API | bearer access token |
| `Content-Type` | request body | формат тела |
| `Accept` | рекомендуется | формат ответа |
| `Idempotency-Key` | критические команды | защита от повторного эффекта |
| `X-Request-ID` | необязательно | клиентский correlation ID |
| `If-Match` | endpoint-specific | optimistic concurrency по ETag/version |
| `Accept-Language` | необязательно | предпочтительный язык сообщений, если поддерживается |

### 6.2 Response

| Header | Назначение |
|---|---|
| `Content-Type` | формат ответа |
| `X-Request-ID` | идентификатор запроса |
| `Location` | canonical URL созданного ресурса |
| `ETag` | версия изменяемого ресурса, если контракт поддерживает concurrency control |
| `Retry-After` | задержка для применимых `429`/`503` |
| `Deprecation` | deprecated contract |
| `Sunset` | дата отключения deprecated contract |
| `Cache-Control` | политика кэширования |

`X-Request-ID` валидируется по длине и формату. Недопустимое значение заменяется серверным. Публичные read endpoint-ы могут применять `ETag` и cache headers; ответы с токенами, сессиями, внутренними остатками и чувствительными данными используют `Cache-Control: no-store`.

## 7. Success envelope

Все JSON-ответы с телом используют envelope:

```json
{
  "success": true,
  "data": {"id":"01JZX3E2J9Q4JY3N8VY6F7XH2A"},
  "meta": {"request_id":"01JZX3G15D8RT7R1N3QFJ8Q5PX"}
}
```

Правила:

1. `success` всегда `true`.
2. `data` содержит ресурс, коллекцию или результат команды.
3. `meta.request_id` присутствует во всех JSON-ответах.
4. `meta.pagination` присутствует только у пагинированной коллекции.
5. `meta.idempotency_replayed=true` присутствует при replay, если endpoint это поддерживает.
6. `204 No Content` не возвращает body.
7. `data` и `error` не присутствуют одновременно.
8. Пустая коллекция возвращает `200` и `items: []`, а не `404`.

Пример пагинации:

```json
{
  "success": true,
  "data": {"items":[]},
  "meta": {
    "request_id":"01JZX3G15D8RT7R1N3QFJ8Q5PX",
    "pagination":{"next_cursor":null,"has_more":false,"limit":50}
  }
}
```

## 8. Error envelope и централизованный mapping

Handlers передают ошибку единому responder. Категория определяется только через `errors.Is()`. Запрещены сравнение по `err.Error()`, substring matching и прямое сопоставление PostgreSQL-кодов в delivery/application.

```json
{
  "success": false,
  "error": {
    "code":"BUSINESS_RULE_VIOLATION",
    "message":"operation violates a business rule",
    "details":[
      {"field":"items[0].display_quantity","code":"INSUFFICIENT_STOCK","message":"requested quantity is unavailable"}
    ]
  },
  "meta":{"request_id":"01JZX3G15D8RT7R1N3QFJ8Q5PX"}
}
```

`details` используется для безопасных структурированных ошибок. Нельзя возвращать SQL, stack trace, table/constraint names, токены, пароли, filesystem paths, driver errors и panic values.

### 8.1 Общие категории

| HTTP | Public code | Семантика |
|---:|---|---|
| 400 | `INVALID_ARGUMENT` | JSON, path/query/header или field format invalid |
| 401 | `UNAUTHENTICATED` | отсутствует или недействителен credential |
| 403 | `FORBIDDEN` | роль или scope не разрешает операцию |
| 404 | `NOT_FOUND` | ресурс отсутствует или намеренно скрыт |
| 406 | `NOT_ACCEPTABLE` | неподдерживаемый response format |
| 409 | `CONFLICT` | state, uniqueness, version или idempotency conflict |
| 412 | `PRECONDITION_FAILED` | `If-Match` не соответствует текущей версии |
| 413 | `PAYLOAD_TOO_LARGE` | body/file exceeds limit |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | неподдерживаемый content type |
| 422 | `BUSINESS_RULE_VIOLATION` | синтаксически валидная команда нарушает доменное правило |
| 429 | `RATE_LIMITED` | превышен rate limit |
| 500 | `INTERNAL_ERROR` | неизвестная внутренняя ошибка |
| 503 | `SERVICE_UNAVAILABLE` | обязательная зависимость недоступна |

Transport validation возвращает `400`; доменные ограничения — `422`; конфликт текущего состояния или уникальности — `409`.

### 8.2 Validation detail

```json
{"field":"phone","code":"INVALID_FORMAT","message":"phone has invalid format"}
```

`field` использует путь request DTO. Для cross-field ошибки поле может отсутствовать. Порядок `details` должен быть детерминированным.

## 9. Authentication и sessions

Protected API использует bearer access token:

```http
Authorization: Bearer <access-token>
```

Окончательная политика access/refresh token, TTL, rotation, storage и forced logout фиксируется в `09-security-design.md`. До её принятия auth contract остаётся `Planned`.

Базовые правила:

1. credentials передаются только по HTTPS;
2. пароли и токены не логируются и не попадают в error details;
3. access token идентифицирует principal, но не является единственным источником authorization;
4. backend повторно проверяет актуальный статус пользователя и необходимые назначения;
5. публичный поиск не требует токена;
6. самостоятельная регистрация `ADMIN` и `PHARMACIST` отсутствует;
7. login не раскрывает, существует ли пользователь;
8. refresh token rotation обязана обнаруживать replay согласно security design;
9. auth/session responses используют `Cache-Control: no-store`.

Способ передачи refresh token — защищённая cookie либо response body — должен быть выбран только в `09-security-design.md`; API design не фиксирует небезопасную схему заранее.

## 10. Authorization

Роли MVP: `CLIENT`, `PHARMACIST`, `ADMIN`.

Проверка состоит из:

1. **RBAC** — разрешена ли операция роли;
2. **scope authorization** — имеет ли principal доступ к аптеке/объекту;
3. **current-state authorization** — активны ли пользователь, назначение, аптека и ресурс на момент операции.

`pharmacy_id` в URL не предоставляет доступ. Для `PHARMACIST` backend проверяет активное назначение именно этой аптеке. `ADMIN` не получает автоматическое право обходить доменные инварианты и immutable history.

Для предотвращения object enumeration защищённый endpoint может возвращать `404` вместо `403`; выбор фиксируется детальным контрактом и тестами.

## 11. Пагинация, фильтрация и сортировка

Для потенциально больших или изменяемых коллекций используется cursor pagination:

```text
?limit=50&cursor=<opaque>
```

| Параметр | Default | Ограничение |
|---|---:|---:|
| `limit` | 50 | 1–100 |
| `cursor` | absent | opaque server-issued value |

Курсор связывается с endpoint, actor/scope, filter set, sort и schema version. Изменение этих параметров делает cursor недействительным и возвращает `400 INVALID_CURSOR`.

Каждая выборка имеет стабильный total order с уникальным tie-breaker:

```text
created_at DESC, id DESC
expiration_date ASC, received_at ASC, id ASC
name ASC, id ASC
```

Offset pagination допускается только для небольшого стабильного справочника и должна быть явно обоснована.

Фильтры передаются отдельными query parameters. `sort` принимает только allowlist значений, а не SQL columns. `q` нормализуется по trim; минимальная длина и поведение пустой строки фиксируются endpoint-ом.

По умолчанию API не возвращает `total_count`, поскольку его вычисление может быть дорогим и семантически нестабильным. Если count нужен, он должен быть явно описан.

## 12. Idempotency

`Idempotency-Key` обязателен для команд, создающих складской/финансовый эффект или проводящих документ:

- receipt posting/reversal;
- initial stock confirmation;
- sale/void;
- return/reversal;
- write-off;
- inventory adjustment;
- staging publish;
- другие явно отмеченные команды.

Формат: непустая ASCII-строка длиной 1–128; рекомендуемый UUID v4.

Scope pharmacy-команды:

```text
actor_id + pharmacy_id + operation_id + idempotency_key
```

`operation_id` — стабильное логическое имя команды, а не raw URL. Scope не должен позволять случайно переиспользовать ключ для разных операций.

Backend хранит canonical semantic hash. В него входят все поля, влияющие на смысл операции, включая path scope и нормализованный payload. Не входят `X-Request-ID`, порядок JSON keys и другие transport-only данные.

Поведение:

1. новый ключ — команда выполняется;
2. тот же key + semantic hash — возвращается исходный HTTP status и representation без повторного эффекта;
3. тот же key + другой hash — `409 IDEMPOTENCY_KEY_REUSED`;
4. параллельные одинаковые запросы сериализуются;
5. запись idempotency и бизнес-эффект фиксируются атомарно;
6. failed validation до начала команды не резервирует ключ, если детальный контракт не определяет иное;
7. неопределённый сетевой исход проверяется повтором того же запроса.

Replay должен возвращать тот же успешный status (`201` остаётся `201`), а не менять его на `200`; отличие указывается через `meta.idempotency_replayed` и/или response header. Это исключает зависимость клиента от истории доставки.

Минимальный retention — 24 часа. Для юридически значимых документов связь может храниться столько же, сколько документ.

## 13. Concurrency, preconditions и retries

Идемпотентность защищает от повторной доставки, но не от lost update. Для изменяемых справочников endpoint может возвращать `ETag` и требовать:

```http
If-Match: "<version>"
```

Несовпадение версии → `412 PRECONDITION_FAILED`. Если endpoint не использует `ETag`, он обязан явно определить иной механизм concurrency control либо last-write-wins.

Backend может ограниченно повторять PostgreSQL `40P01`/`40001` только для безопасных transactional use cases согласно ADR. Клиент не должен автоматически retry `4xx`, кроме явно retryable `409`, `429` и auth refresh flow. `503` retry допускается с bounded backoff и `Retry-After`.

## 14. HTTP status codes

| Status | Использование |
|---:|---|
| 200 | чтение или синхронная команда без создания нового ресурса |
| 201 | новый ресурс или проведённый документ; replay сохраняет `201` |
| 202 | принята асинхронная job |
| 204 | успешная команда без body |
| 400 | malformed/invalid transport input |
| 401 | authentication failed |
| 403 | authenticated but forbidden |
| 404 | absent or concealed resource |
| 406 | unacceptable response media type |
| 409 | state/uniqueness/idempotency conflict |
| 412 | failed conditional update |
| 413 | body/file too large |
| 415 | unsupported media type |
| 422 | domain rule violation |
| 429 | rate limited |
| 500 | unexpected internal error |
| 503 | mandatory dependency unavailable |

`POST` не означает автоматически `201`: action endpoint возвращает статус по фактической семантике. `DELETE` может возвращать `204`; завершение assignment/session не означает физическое удаление исторических данных.

## 15. PATCH, архивирование и исторические данные

`PATCH` используется только для разрешённых изменяемых полей. Request должен содержать хотя бы одно изменяемое поле; пустой `PATCH` возвращает `400 EMPTY_PATCH`.

Проведённые receipts, sales, returns, write-offs, adjustments, inventory movements и audit events:

- не редактируются через CRUD;
- не удаляются физически;
- исправляются отдельной компенсирующей операцией;
- сохраняют snapshots;
- не меняются каскадно при редактировании справочников.

Архивирование не равняется удалению и должно быть идемпотентным по состоянию: повторное архивирование либо возвращает текущее состояние, либо стабильный conflict — выбор фиксируется endpoint-ом.

## 16. Async jobs и file contracts

Upload использует `multipart/form-data` с полем `file`. Ограничиваются MIME, extension, size, row count и parser. Client filename не используется как filesystem path.

Длительная обработка возвращает `202`:

```json
{
  "success":true,
  "data":{"job_id":"...","status":"QUEUED","status_url":"/api/v1/..."},
  "meta":{"request_id":"..."}
}
```

Базовые job status: `QUEUED`, `RUNNING`, `SUCCEEDED`, `FAILED`, `CANCELLED` — конкретный endpoint может использовать подмножество. Job response содержит timestamps, progress/counters, safe error summary и links на report. Нельзя использовать бесконечный HTTP request вместо status polling.

Повторная загрузка файла сама по себе не обязана быть idempotent; команда publish/confirm обязана. Политика частичной или атомарной публикации фиксируется до реализации.

Downloads обязаны задавать безопасный `Content-Disposition`; пользовательские значения не вставляются в filename без sanitization.

## 17. Cache, rate limits и CORS

Публичные GET могут кэшироваться только если freshness semantics не вводят пользователя в заблуждение. Availability response обязан содержать `inventory_changed_at`/`as_of` и короткую cache policy. Protected responses по умолчанию `private, no-store`, если endpoint явно не разрешает иное.

Rate limiting минимум применяется к login, refresh, public search, uploads и массовым admin-командам. `429` возвращает `Retry-After`, когда возможно. Лимиты конфигурируются server-side.

Production CORS использует allowlist origins, methods и headers. `*` с credentials запрещён.

## 18. Batch operations

Batch endpoint не вводится автоматически ради уменьшения количества запросов. Если batch требуется:

- должен быть определён max item count;
- порядок результатов соответствует request либо содержит client correlation key;
- политика atomic/all-or-nothing или partial success фиксируется явно;
- partial success не кодируется неструктурированным message;
- batch не обходит per-item authorization, validation, audit и idempotency.

В MVP критические складские документы остаются атомарными бизнес-командами, а не generic bulk CRUD.

## 19. Каталог планируемых endpoint-ов

Все endpoint-ы ниже имеют статус `Planned`, пока реализация и обязательные тесты не сверены с контрактом. Столбец `Idem` показывает обязательность `Idempotency-Key`.

### 19.1 Operational

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/healthz` | operational | no | liveness без проверки зависимостей |
| GET | `/readyz` | operational | no | readiness с проверкой PostgreSQL |

### 19.2 Authentication and current user

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| POST | `/api/v1/auth/login` | public | no | вход по identifier/password |
| POST | `/api/v1/auth/refresh` | refresh session | no | rotation session |
| POST | `/api/v1/auth/logout` | authenticated/session | no | завершение текущей сессии |
| POST | `/api/v1/auth/logout-all` | authenticated | recommended | отзыв всех сессий |
| GET | `/api/v1/me` | authenticated | no | текущий principal, role и scope |
| PATCH | `/api/v1/me` | authenticated | no | собственные изменяемые profile fields |
| POST | `/api/v1/me/password` | authenticated | recommended | смена собственного пароля и session policy |
| GET | `/api/v1/me/sessions` | authenticated | no | список сессий без secrets |
| DELETE | `/api/v1/me/sessions/{session_id}` | authenticated + ownership | no | отзыв выбранной сессии |

### 19.3 Public catalog and availability

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/public/products/search` | public | no | поиск по торговому названию/МНН |
| GET | `/api/v1/public/products/{product_id}` | public | no | публичная карточка active product |
| GET | `/api/v1/public/presentations/{presentation_id}` | public | no | публичная карточка active presentation |
| GET | `/api/v1/public/presentations/{presentation_id}/availability` | public | no | active pharmacies, price, status, freshness |
| GET | `/api/v1/public/pharmacies/{pharmacy_id}` | public | no | публичная карточка active pharmacy |

Публичный API не возвращает точные остатки, закупочные цены, lot number, audit IDs или персональные данные.

### 19.4 Admin users and assignments

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/admin/users` | ADMIN | no | список пользователей |
| POST | `/api/v1/admin/users` | ADMIN | recommended | создание пользователя |
| GET | `/api/v1/admin/users/{user_id}` | ADMIN | no | карточка пользователя |
| PATCH | `/api/v1/admin/users/{user_id}` | ADMIN | no | profile/status с concurrency control |
| POST | `/api/v1/admin/users/{user_id}/block` | ADMIN | recommended | блокировка и session revocation policy |
| POST | `/api/v1/admin/users/{user_id}/unblock` | ADMIN | recommended | снятие блокировки |
| POST | `/api/v1/admin/users/{user_id}/archive` | ADMIN | recommended | архивирование |
| GET | `/api/v1/admin/users/{user_id}/pharmacy-assignments` | ADMIN | no | назначения аптекаря |
| POST | `/api/v1/admin/users/{user_id}/pharmacy-assignments` | ADMIN | recommended | создание назначения |
| DELETE | `/api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}` | ADMIN | no | завершение назначения без удаления истории |
| POST | `/api/v1/admin/users/{user_id}/password-reset` | ADMIN | recommended | безопасный reset flow, не выдача пароля |

### 19.5 Pharmacies

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/admin/pharmacies` | ADMIN | no | список, включая inactive |
| POST | `/api/v1/admin/pharmacies` | ADMIN | recommended | создание аптеки |
| GET | `/api/v1/admin/pharmacies/{pharmacy_id}` | ADMIN | no | административная карточка |
| PATCH | `/api/v1/admin/pharmacies/{pharmacy_id}` | ADMIN | no | разрешённые поля/status |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/activate` | ADMIN | recommended | активация |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/block` | ADMIN | recommended | блокировка новых операций |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/archive` | ADMIN | recommended | архивирование |
| GET | `/api/v1/pharmacies/{pharmacy_id}` | PHARMACIST/ADMIN + scope | no | внутренняя карточка |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/public-profile` | PHARMACIST/ADMIN + scope | no | публичные поля |

### 19.6 Global catalog and product requests

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/catalog/products` | PHARMACIST/ADMIN | no | внутренний поиск каталога |
| GET | `/api/v1/catalog/products/{product_id}` | PHARMACIST/ADMIN | no | полная карточка product |
| POST | `/api/v1/admin/catalog/products` | ADMIN | recommended | создание product |
| PATCH | `/api/v1/admin/catalog/products/{product_id}` | ADMIN | no | изменение будущего состояния |
| POST | `/api/v1/admin/catalog/products/{product_id}/archive` | ADMIN | recommended | архивирование |
| POST | `/api/v1/admin/catalog/products/{product_id}/presentations` | ADMIN | recommended | создание presentation |
| GET | `/api/v1/catalog/presentations/{presentation_id}` | PHARMACIST/ADMIN | no | полная карточка presentation |
| PATCH | `/api/v1/admin/catalog/presentations/{presentation_id}` | ADMIN | no | изменения только для будущих операций |
| POST | `/api/v1/admin/catalog/presentations/{presentation_id}/archive` | ADMIN | recommended | архивирование |
| POST | `/api/v1/admin/catalog/presentations/{presentation_id}/barcodes` | ADMIN | recommended | добавление barcode |
| PATCH | `/api/v1/admin/catalog/barcodes/{barcode_id}` | ADMIN | no | primary/status |
| DELETE | `/api/v1/admin/catalog/barcodes/{barcode_id}` | ADMIN | no | удаление только ошибочного неиспользуемого barcode |
| POST | `/api/v1/catalog/product-requests` | PHARMACIST + scope | recommended | запрос отсутствующей позиции |
| GET | `/api/v1/admin/catalog/product-requests` | ADMIN | no | очередь запросов |
| PATCH | `/api/v1/admin/catalog/product-requests/{request_id}` | ADMIN | no | решение по запросу |

### 19.7 Catalog staging import

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| POST | `/api/v1/admin/catalog-imports` | ADMIN | no | upload и создание job |
| GET | `/api/v1/admin/catalog-imports` | ADMIN | no | список jobs |
| GET | `/api/v1/admin/catalog-imports/{import_id}` | ADMIN | no | status/counters |
| GET | `/api/v1/admin/catalog-imports/{import_id}/rows` | ADMIN | no | staging rows |
| PATCH | `/api/v1/admin/catalog-imports/{import_id}/rows/{row_id}` | ADMIN | no | normalized data/decision |
| POST | `/api/v1/admin/catalog-imports/{import_id}/validate` | ADMIN | recommended | повторная validation job |
| POST | `/api/v1/admin/catalog-imports/{import_id}/publish` | ADMIN | required | публикация подтверждённых строк |
| GET | `/api/v1/admin/catalog-imports/{import_id}/report` | ADMIN | no | error report |

Политика atomic/partial publish должна быть выбрана до реализации.

### 19.8 Pharmacy assortment

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/assortment` | PHARMACIST/ADMIN + scope | no | локальный ассортимент |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment` | PHARMACIST/ADMIN + scope | recommended | подключение presentation |
| GET | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}` | PHARMACIST/ADMIN + scope | no | карточка local product |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}` | PHARMACIST/ADMIN + scope | no | цены, sale rules, stock levels |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}/archive` | PHARMACIST/ADMIN + scope | recommended | исключение из новых операций |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}/activate` | PHARMACIST/ADMIN + scope | recommended | повторная активация |

### 19.9 Receipts

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/receipts` | PHARMACIST/ADMIN + scope | no | список проведённых receipts |
| POST | `/api/v1/pharmacies/{pharmacy_id}/receipts` | PHARMACIST/ADMIN + scope | required | атомарное проведение receipt |
| GET | `/api/v1/pharmacies/{pharmacy_id}/receipts/{receipt_id}` | PHARMACIST/ADMIN + scope | no | receipt, items, lots |
| POST | `/api/v1/pharmacies/{pharmacy_id}/receipts/{receipt_id}/reverse` | elevated permission | required | компенсирующая операция |

Draft CRUD для receipts не входит в MVP.

### 19.10 Initial stock imports

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/initial-stock-import-template` | PHARMACIST/ADMIN | no | стандартный template |
| POST | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports` | PHARMACIST/ADMIN + scope | no | upload/validation job |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports` | PHARMACIST/ADMIN + scope | no | список jobs |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}` | PHARMACIST/ADMIN + scope | no | status/preview/counters |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/rows` | PHARMACIST/ADMIN + scope | no | rows/errors |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/rows/{row_id}` | PHARMACIST/ADMIN + scope | no | mapping correction |
| POST | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/confirm` | PHARMACIST/ADMIN + scope | required | атомарное initial posting |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/report` | PHARMACIST/ADMIN + scope | no | error report |

### 19.11 Sales

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales` | PHARMACIST/ADMIN + scope | no | список sales |
| POST | `/api/v1/pharmacies/{pharmacy_id}/sales` | PHARMACIST/ADMIN + scope | required | атомарная sale с FEFO |
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}` | PHARMACIST/ADMIN + scope | no | receipt representation и допустимые allocations |
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}/return-eligibility` | PHARMACIST/ADMIN + scope | no | доступные quantities для return |
| POST | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}/void` | elevated permission | required | сторнирование по утверждённой политике |

Одинаковые `pharmacy_product_id + sale_unit` в одном request отклоняются `DUPLICATE_SALE_ITEM`; backend не объединяет их неявно.

### 19.12 Returns

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/returns` | PHARMACIST/ADMIN + scope | no | список returns |
| POST | `/api/v1/pharmacies/{pharmacy_id}/returns` | PHARMACIST/ADMIN + scope | required | атомарный return по исходной sale |
| GET | `/api/v1/pharmacies/{pharmacy_id}/returns/{return_id}` | PHARMACIST/ADMIN + scope | no | items/refund/disposition |
| POST | `/api/v1/pharmacies/{pharmacy_id}/returns/{return_id}/reverse` | elevated permission | required | компенсирующая операция |

Проведение return остаётся `Planned` до юридического утверждения policy.

### 19.13 Write-offs and adjustments

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/write-offs` | PHARMACIST/ADMIN + scope | no | список write-offs |
| POST | `/api/v1/pharmacies/{pharmacy_id}/write-offs` | PHARMACIST/ADMIN + scope | required | проведение с причиной |
| GET | `/api/v1/pharmacies/{pharmacy_id}/write-offs/{write_off_id}` | PHARMACIST/ADMIN + scope | no | документ write-off |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` | PHARMACIST/ADMIN + scope | no | список adjustments |
| POST | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` | elevated permission | required | ожидаемое/factual/delta |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments/{adjustment_id}` | PHARMACIST/ADMIN + scope | no | adjustment document |

### 19.14 Inventory and reconciliation

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory` | PHARMACIST/ADMIN + scope | no | агрегированный внутренний stock |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots` | PHARMACIST/ADMIN + scope | no | lots with filters |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots/{lot_id}` | PHARMACIST/ADMIN + scope | no | lot details |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots/{lot_id}/movements` | PHARMACIST/ADMIN + scope | no | immutable lot ledger |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-operations` | PHARMACIST/ADMIN + scope | no | operation journal |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-operations/{operation_id}` | PHARMACIST/ADMIN + scope | no | operation and movements |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/inventory-reconciliation` | ADMIN | recommended | diagnostic reconciliation job |
| GET | `/api/v1/admin/pharmacies/{pharmacy_id}/inventory-reconciliation/{job_id}` | ADMIN | no | result without auto-fix |

Для inventory movements отсутствуют POST/PATCH/DELETE endpoint-ы.

### 19.15 Alerts and recommendations

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/alerts` | PHARMACIST/ADMIN + scope | no | active/history alerts |
| GET | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}` | PHARMACIST/ADMIN + scope | no | alert details |
| POST | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}/acknowledge` | PHARMACIST/ADMIN + scope | recommended | acknowledgement |
| POST | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}/resolve` | PHARMACIST/ADMIN + scope | recommended | manual resolve if allowed |
| GET | `/api/v1/pharmacies/{pharmacy_id}/replenishment-recommendations` | PHARMACIST/ADMIN + scope | no | recommendations without ordering |

### 19.16 Audit

| Method | Path | Access | Idem | Назначение |
|---|---|---|---|---|
| GET | `/api/v1/admin/audit-events` | ADMIN | no | system audit |
| GET | `/api/v1/admin/audit-events/{audit_event_id}` | ADMIN | no | safe event details |
| GET | `/api/v1/pharmacies/{pharmacy_id}/audit-events` | ADMIN/future auditor role | no | pharmacy-scoped audit |

Audit API не имеет POST/PATCH/DELETE.

## 20. Общие business error codes

| Code | Category | Назначение |
|---|---|---|
| `INVALID_CREDENTIALS` | unauthenticated | generic login failure |
| `ACCOUNT_BLOCKED` | forbidden | user blocked |
| `SESSION_REVOKED` | unauthenticated | session revoked |
| `PHARMACY_ACCESS_DENIED` | forbidden/not found | нет active assignment |
| `PHARMACY_INACTIVE` | business rule | pharmacy blocks operations |
| `RESOURCE_ARCHIVED` | business rule | archived for new operations |
| `DUPLICATE_BARCODE` | conflict | barcode already assigned |
| `DUPLICATE_DOCUMENT_NUMBER` | conflict | duplicate within pharmacy scope |
| `DUPLICATE_SALE_ITEM` | invalid argument | duplicate product/unit pair |
| `INNER_UNIT_SALE_DISABLED` | business rule | inner-unit sale forbidden |
| `PRESCRIPTION_CONFIRMATION_REQUIRED` | business rule | missing confirmation |
| `INSUFFICIENT_STOCK` | business rule | insufficient eligible stock |
| `LOT_EXPIRED` | business rule | expired lot |
| `LOT_QUARANTINED` | business rule | quarantined lot |
| `RETURN_QUANTITY_EXCEEDED` | business rule | return exceeds sold remainder |
| `RETURN_NOT_LEGALLY_ALLOWED` | business rule | legal policy rejects return |
| `IDEMPOTENCY_KEY_REQUIRED` | invalid argument | required key absent |
| `IDEMPOTENCY_KEY_REUSED` | conflict | same key, different semantic hash |
| `INVALID_CURSOR` | invalid argument | cursor incompatible/invalid |
| `EMPTY_PATCH` | invalid argument | no mutable fields supplied |
| `UNKNOWN_FIELD` | invalid argument | request contains unknown field |
| `PRECONDITION_FAILED` | conflict | stale ETag/version |
| `IMPORT_HAS_ERRORS` | business rule | job cannot publish/confirm |
| `IMPORT_ALREADY_CONFIRMED` | conflict | already posted |
| `CONCURRENT_MODIFICATION` | conflict | concurrent state conflict |

`message` не является стабильным идентификатором и может уточняться. Новый code добавляется только когда клиенту действительно нужно отличать сценарий программно.

## 21. Шаблон детального endpoint-контракта

Перед переводом endpoint-а в `Implemented` его раздел обязан содержать:

1. status и owner/module;
2. method/path/access;
3. path/query parameters;
4. headers;
5. request body с field table и корректным JSON example;
6. success statuses и response examples;
7. endpoint-specific error matrix;
8. idempotency scope/hash/replay policy;
9. concurrency/precondition policy;
10. transaction boundary и side effects;
11. audit event;
12. cache/rate-limit policy;
13. security/privacy notes;
14. contract/integration/concurrency test requirements.

## 22. Contract status и Definition of Done

Статусы: `Planned`, `In Progress`, `Implemented`, `Deprecated`, `Removed`.

HTTP feature завершена только если:

1. контракт соответствует SRS и ADR;
2. handler не содержит бизнес-логики;
3. DTO отделены от domain entities;
4. errors проходят через centralized responder и `errors.Is()`;
5. authentication, RBAC и scope authorization протестированы;
6. transaction, idempotency и concurrency semantics протестированы;
7. реальные status, headers и JSON совпадают с документом;
8. отсутствует утечка secrets/internal errors;
9. endpoint описан достаточно для frontend без чтения backend-кода;
10. статус `Implemented` выставлен после contract review.

## 23. Следующая детализация

Первым вертикальным срезом рекомендуется детализировать:

1. `POST /api/v1/auth/login`;
2. `GET /api/v1/me`;
3. `GET /api/v1/catalog/products`;
4. `POST /api/v1/pharmacies/{pharmacy_id}/assortment`;
5. `POST /api/v1/pharmacies/{pharmacy_id}/receipts`;
6. `GET /api/v1/pharmacies/{pharmacy_id}/inventory`;
7. `POST /api/v1/pharmacies/{pharmacy_id}/sales`.

Этот срез проверяет authentication, authorization, catalog, pharmacy scope, Unit of Work, immutable movements, idempotency, FEFO, error mapping и response envelopes в одном сквозном сценарии.

## 24. Открытые решения

До реализации соответствующих endpoint-ов должны быть закрыты:

1. refresh token transport и rotation policy в `09-security-design.md`;
2. юридическая policy возврата лекарств;
3. atomic или partial catalog staging publish;
4. точная модель elevated permissions для void/reverse/adjustment;
5. retention idempotency records и audit exports;
6. ETag/version policy для mutable resources;
7. поддерживаемые MIME/size/row limits импортов;
8. публичная cache TTL и freshness policy availability.

<!-- consistency-incorporated:start -->
## Инкорпорированные API contracts
### Authentication transport
- `POST /api/v1/auth/login` возвращает access token в JSON и устанавливает rotating refresh cookie.
- `POST /api/v1/auth/refresh`, `POST /api/v1/auth/logout`, `POST /api/v1/auth/logout-all` используют refresh/session contract из Security Design.
- Access token хранится frontend только in-memory и передаётся `Authorization: Bearer`.
- Refresh token не возвращается в JSON и не хранится в Web Storage.
- Cookie-auth endpoints требуют exact allowed `Origin` и `X-CSRF-Protection: 1`.
### Нормативные mutation paths
| Сценарий | Method/path |
|---|---|
| Block user | `POST /api/v1/admin/users/{user_id}/block` |
| Assign pharmacist | `POST /api/v1/admin/users/{user_id}/pharmacy-assignments` |
| End assignment | `DELETE /api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}` |
| Post receipt | `POST /api/v1/pharmacies/{pharmacy_id}/receipts` |
| Complete sale | `POST /api/v1/pharmacies/{pharmacy_id}/sales` |
| Complete return | `POST /api/v1/pharmacies/{pharmacy_id}/returns` |
| Post adjustment | `POST /api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` |
| Upload catalog import | `POST /api/v1/admin/catalog-imports` |
Return request передаёт `sale_id` в request body согласно endpoint schema. Generic `/api/v1/{documents}/{id}/reverse` не является contract; используются resource-specific endpoints.
### Persisted states и enum
`ImportJob`: `UPLOADED`, `VALIDATING`, `READY`, `HAS_ERRORS`, `CONFIRMING`, `COMPLETED`, `FAILED`.
`ReturnAction`: `RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`. Значения `RETURN_TO_STOCK`, `RETURN_WRITE_OFF`, `RETURN_QUARANTINE` относятся к `InventoryOperation.type`.
`Sale.status`: `COMPLETED`, `PARTIALLY_REFUNDED`, `REFUNDED`, `REVERSED`.
### Idempotency и IDs
Critical mutation требует `Idempotency-Key`. Identity ключа: `actor + operation + effective_scope + key`; pharmacy scope использует `pharmacy_id`, global/admin scope — `GLOBAL`. Replay повторно проверяет authorization и visibility. API ID — opaque UUID string; примеры используют UUID.
### Machine-readable contract
OpenAPI 3.1 хранится в `backend/api/openapi.yaml`. Frontend generated client создаётся pinned `openapi-typescript` + `openapi-fetch`; CI проверяет, что generated output соответствует contract.
<!-- consistency-incorporated:end -->
