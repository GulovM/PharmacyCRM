# PharmacyCRM — API Design

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-17

## 1. Назначение

Этот документ является единым человекочитаемым каталогом HTTP API PharmacyCRM. Он определяет общие правила API, форматы запросов и ответов, модель ошибок, авторизацию, пагинацию, идемпотентность и полный перечень планируемых endpoint-ов.

Документ предназначен для backend-, frontend- и QA-разработки. После реализации endpoint-а его фактический контракт — URL, method, headers, request DTO, response DTO, status codes и ошибки — должен быть описан здесь в том же change set.

При противоречии применяется следующий приоритет:

1. применимое законодательство и обязательные регуляторные требования;
2. `docs/01-product-vision.md`;
3. `docs/02-srs.md`;
4. принятые ADR;
5. этот документ;
6. реализация и тесты.

## 2. Границы API

API разделяется на три контура:

1. **Public API** — поиск лекарств и получение публичной информации об аптеках без авторизации.
2. **Protected API** — функции авторизованного пользователя, аптекаря и администратора.
3. **Operational API** — health, readiness и другие технические endpoint-ы, не относящиеся к бизнес-контракту frontend.

MVP использует REST-подобный JSON API поверх HTTPS. Gin применяется только в HTTP delivery-слое. `gin.Context` не передаётся в application, domain и repository.

## 3. Base URL и версионирование

Базовый путь бизнес-API:

```text
/api/v1
```

Примеры:

```text
GET  /api/v1/public/products/search
POST /api/v1/pharmacies/{pharmacy_id}/sales
GET  /api/v1/admin/audit-events
```

Технические endpoint-ы не входят в `/api/v1`:

```text
GET /healthz
GET /readyz
```

Версия меняется при несовместимом изменении контракта. Добавление необязательного поля ответа, нового endpoint-а или нового необязательного query-параметра обычно считается обратно совместимым. Удаление поля, изменение его типа, изменение обязательности request-поля, изменение значения enum или семантики существующего status code требует новой версии либо формального deprecation-периода.

## 4. Content negotiation и кодировка

Для JSON-запросов:

```http
Content-Type: application/json
Accept: application/json
```

Кодировка — UTF-8.

Файловые загрузки используют `multipart/form-data`. Выгрузки могут возвращать JSON, CSV или XLSX в зависимости от endpoint-а и заголовка `Accept`.

Неизвестный или неподдерживаемый media type возвращает `415 UNSUPPORTED_MEDIA_TYPE`. Неподдерживаемый формат ответа возвращает `406 NOT_ACCEPTABLE`.

## 5. Идентификаторы и базовые типы

### 5.1 Идентификаторы

Внешние идентификаторы сущностей передаются как строки. Конкретный внутренний тип БД не является частью публичного контракта.

```json
{
  "id": "01JZX3E2J9Q4JY3N8VY6F7XH2A"
}
```

Frontend не должен выполнять арифметику над ID или предполагать, что ID последовательны.

### 5.2 Даты и время

Дата без времени:

```text
YYYY-MM-DD
```

Дата и время:

```text
RFC 3339 / ISO 8601 с часовым поясом
```

Пример:

```text
2026-07-17T14:30:00+05:00
```

Серверное время является источником истины для `created_at`, `posted_at`, `completed_at`, аудита и иных юридически или операционно значимых временных меток.

### 5.3 Деньги

Все денежные значения передаются целыми числами в дирамах — минимальной денежной единице сомони.

```json
{
  "amount_dirams": 12550,
  "currency": "TJS"
}
```

Денежные значения с плавающей точкой запрещены.

### 5.4 Количества

Складской остаток хранится и передаётся в целых базовых единицах отпуска.

Поля количества должны явно указывать единицу в имени:

- `quantity_base_units`;
- `quantity_packages`;
- `display_quantity` вместе с `sale_unit`;
- `base_units_per_package`.

### 5.5 Nullable и отсутствующие поля

Отсутствующее поле и `null` имеют разную семантику:

- отсутствующее поле в `PATCH` означает «не изменять»;
- `null` означает явную очистку значения, если это разрешено контрактом;
- обязательное non-nullable поле не может отсутствовать и не может быть `null`.

## 6. Общие HTTP-заголовки

### 6.1 Request headers

| Header | Обязательность | Назначение |
|---|---|---|
| `Authorization` | для protected API | bearer access token |
| `Content-Type` | для request body | формат тела запроса |
| `Accept` | рекомендуется | ожидаемый формат ответа |
| `Idempotency-Key` | для критических команд | защита от повторного эффекта |
| `X-Request-ID` | необязательно | клиентский correlation ID |

### 6.2 Response headers

| Header | Назначение |
|---|---|
| `Content-Type` | формат ответа |
| `X-Request-ID` | идентификатор запроса |
| `Location` | URL созданного ресурса для применимых `201` |
| `Retry-After` | рекомендуемая задержка для применимых `429` или `503` |
| `Deprecation` | признак deprecated endpoint-а |
| `Sunset` | дата отключения deprecated endpoint-а |

Если клиент не передал допустимый `X-Request-ID`, сервер генерирует его самостоятельно. Сервер не обязан принимать произвольную строку без проверки длины и формата.

## 7. Success envelope

Все JSON-ответы с телом используют единый envelope:

```json
{
  "success": true,
  "data": {
    "id": "01JZX3E2J9Q4JY3N8VY6F7XH2A"
  },
  "meta": {
    "request_id": "01JZX3G15D8RT7R1N3QFJ8Q5PX"
  }
}
```

Правила:

1. `success` всегда равен `true`.
2. `data` содержит ресурс, коллекцию или результат команды.
3. `meta.request_id` присутствует во всех JSON-ответах.
4. `meta.pagination` добавляется только для пагинированных коллекций.
5. Endpoint с `204 No Content` не возвращает envelope и body.
6. В одном ответе не могут одновременно присутствовать `data` и `error`.

Пример коллекции:

```json
{
  "success": true,
  "data": {
    "items": []
  },
  "meta": {
    "request_id": "01JZX3G15D8RT7R1N3QFJ8Q5PX",
    "pagination": {
      "next_cursor": null,
      "has_more": false,
      "limit": 50
    }
  }
}
```

## 8. Error envelope

Все JSON-ошибки формируются централизованным HTTP error responder. Сравнение ошибок выполняется только через `errors.Is()`; handlers не сопоставляют ошибки вручную по строке, `err.Error()` или PostgreSQL-коду.

Базовый формат:

```json
{
  "success": false,
  "error": {
    "code": "BUSINESS_RULE_VIOLATION",
    "message": "operation violates a business rule",
    "details": [
      {
        "field": "items[0].display_quantity",
        "code": "INSUFFICIENT_STOCK",
        "message": "requested quantity is unavailable"
      }
    ]
  },
  "meta": {
    "request_id": "01JZX3G15D8RT7R1N3QFJ8Q5PX"
  }
}
```

`details` необязательно и используется для безопасных структурированных ошибок валидации или нескольких бизнес-нарушений. Поле не содержит SQL, stack trace, имена таблиц, constraint names, токены, пароли, filesystem paths и необработанный текст внутренних ошибок.

### 8.1 Общая таблица ошибок

| HTTP | Public code | Назначение | Retry |
|---:|---|---|---|
| 400 | `INVALID_ARGUMENT` | некорректный JSON, path/query-параметр или формат поля | после исправления запроса |
| 401 | `UNAUTHENTICATED` | отсутствует, истёк или недействителен access token | после повторной аутентификации |
| 403 | `FORBIDDEN` | роль или область полномочий не разрешает операцию | нет без изменения прав |
| 404 | `NOT_FOUND` | ресурс не найден или скрыт политикой авторизации | обычно нет |
| 409 | `CONFLICT` | конфликт состояния, уникальности или idempotency key | зависит от причины |
| 413 | `PAYLOAD_TOO_LARGE` | превышен лимит body или файла | после уменьшения payload |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | неподдерживаемый Content-Type | после исправления запроса |
| 422 | `BUSINESS_RULE_VIOLATION` | валидный запрос нарушает доменное правило | после изменения команды |
| 429 | `RATE_LIMITED` | превышен rate limit | да, с учётом `Retry-After` |
| 500 | `INTERNAL_ERROR` | неизвестная внутренняя ошибка | неавтоматически клиентом без политики |
| 503 | `SERVICE_UNAVAILABLE` | обязательная зависимость временно недоступна | ограниченный retry |

Endpoint может определять более точный стабильный код, например `INSUFFICIENT_STOCK`, `PHARMACY_INACTIVE`, `IDEMPOTENCY_KEY_REUSED`, `PRESCRIPTION_CONFIRMATION_REQUIRED`, но он должен отображаться в одну из общих категорий.

### 8.2 Validation details

```json
{
  "field": "phone",
  "code": "INVALID_FORMAT",
  "message": "phone has invalid format"
}
```

`field` использует путь request DTO. Для ошибок, не связанных с одним полем, `field` отсутствует.

## 9. Authentication

Protected API использует bearer access token:

```http
Authorization: Bearer <access-token>
```

Окончательная схема access/refresh token, сроки жизни, хранение refresh session, rotation и forced logout фиксируются в `09-security-design.md`.

Базовые правила:

1. Пароль передаётся только по HTTPS.
2. Пароль и токены никогда не возвращаются в error details и не логируются.
3. Access token идентифицирует пользователя, но не является единственным источником авторизации.
4. Backend повторно проверяет актуальный статус пользователя, блокировку, роль и применимые назначения.
5. Самостоятельная регистрация `ADMIN` и `PHARMACIST` отсутствует.
6. Публичный поиск не требует токена.

Планируемые auth endpoint-ы используют refresh-session модель, а не бессрочный access token.

## 10. Authorization

Роли MVP:

- `CLIENT`;
- `PHARMACIST`;
- `ADMIN`.

Авторизация состоит из двух проверок:

1. **RBAC** — разрешена ли операция роли пользователя.
2. **Scope authorization** — имеет ли пользователь доступ к конкретной аптеке или объекту.

Для `PHARMACIST` наличие `pharmacy_id` в URL не даёт права доступа. Backend обязан проверить активное назначение пользователя этой аптеке и активность самой аптеки.

Чтобы снизить риск раскрытия существования чужого объекта, endpoint может возвращать `404 NOT_FOUND` вместо `403 FORBIDDEN`, если это определено его контрактом.

## 11. Пагинация

### 11.1 Базовая модель

Для изменяемых и потенциально больших коллекций используется cursor pagination (пагинация по непрозрачному курсору):

```text
?limit=50&cursor=eyJpZCI6Ii4uLiJ9
```

Параметры:

| Параметр | Default | Ограничение |
|---|---:|---:|
| `limit` | 50 | 1–100 |
| `cursor` | отсутствует | непрозрачная строка, выданная сервером |

Ответ:

```json
{
  "pagination": {
    "next_cursor": "eyJpZCI6Ii4uLiJ9",
    "has_more": true,
    "limit": 50
  }
}
```

Клиент не должен декодировать или конструировать cursor самостоятельно.

### 11.2 Детерминированный порядок

Каждая пагинированная выборка имеет полный стабильный порядок с уникальным tie-breaker, обычно `id`.

Примеры:

```text
created_at DESC, id DESC
expiration_date ASC, received_at ASC, id ASC
name ASC, id ASC
```

Изменение filters, sort или scope делает ранее выданный cursor недействительным.

### 11.3 Offset pagination

Offset pagination допускается только для небольших административных справочников, если её преимущества доказаны конкретным контрактом. По умолчанию новые endpoint-ы используют cursor pagination.

## 12. Фильтрация, сортировка и поиск

Фильтры передаются отдельными query-параметрами:

```text
?status=ACTIVE&expiration_before=2026-08-17
```

Сортировка:

```text
?sort=price_asc
```

Допустимые значения `sort` перечисляются в контракте endpoint-а. Произвольные имена SQL-колонок от клиента не принимаются.

Строковый поиск использует параметр `q`. Пробелы по краям нормализуются. Пустой `q` либо отклоняется, либо трактуется как отсутствие фильтра — правило определяется endpoint-ом.

## 13. Idempotency

### 13.1 Обязательные endpoint-ы

`Idempotency-Key` обязателен для команд, создающих складской или финансовый эффект либо проводящих бизнес-документ:

- проведение поступления;
- подтверждение импорта начальных остатков;
- проведение продажи;
- проведение возврата;
- проведение списания;
- проведение инвентаризационной корректировки;
- публикация staging-строк;
- другие операции, явно отмеченные в контракте.

### 13.2 Формат и scope

```http
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
```

Ключ — непустая строка длиной до 128 символов. Рекомендуемый формат — UUID v4.

Scope ключа для pharmacy-команд:

```text
actor + pharmacy_id + endpoint operation + idempotency key
```

Для admin-команд scope определяется конкретным endpoint-ом.

### 13.3 Semantic hash

Сервер сохраняет hash канонического смыслового payload. В hash не включаются transport-поля, которые не меняют смысл операции, например порядок JSON-ключей и `X-Request-ID`.

Поведение:

1. новый ключ — операция выполняется;
2. тот же ключ и тот же semantic payload — возвращается исходный результат без повторного эффекта;
3. тот же ключ и другой semantic payload — `409 IDEMPOTENCY_KEY_REUSED`;
4. параллельные запросы с одним ключом сериализуются;
5. неизвестный итог после сетевого разрыва безопасно проверяется повтором с тем же ключом.

Минимальный срок хранения результата критической операции — 24 часа. Для юридически значимых документов запись связи с idempotency key может храниться дольше согласно retention policy.

## 14. HTTP status codes

| Status | Использование |
|---:|---|
| 200 | успешное чтение, команда с синхронным результатом или idempotent replay |
| 201 | создан новый ресурс или проведён новый документ |
| 202 | принята асинхронная задача импорта/обработки |
| 204 | успешная команда без тела ответа |
| 400 | transport/format validation error |
| 401 | authentication required/failed |
| 403 | authenticated but forbidden |
| 404 | resource not found or intentionally concealed |
| 409 | state, uniqueness or idempotency conflict |
| 413 | body/file too large |
| 415 | unsupported media type |
| 422 | domain/business rule violation |
| 429 | rate limit exceeded |
| 500 | unexpected internal error |
| 503 | mandatory dependency unavailable |

Создание проведённого документа возвращает `201`, даже если его побочные эффекты применены в той же транзакции. Повтор по idempotency key может возвращать `200` с тем же представлением и признаком replay в `meta`.

## 15. PATCH, архивирование и исторические данные

`PATCH` используется только для разрешённых изменяемых полей текущего состояния.

Проведённые поступления, продажи, возвраты, списания, корректировки, складские движения и аудит:

- не редактируются через общий CRUD endpoint;
- не удаляются физически;
- исправляются отдельной компенсирующей или сторнирующей операцией;
- сохраняют исторические snapshots.

Для справочников применяется архивирование или изменение статуса вместо физического удаления, если ресурс связан с операционной историей.

## 16. Upload/download contracts

Файловая загрузка выполняется через `multipart/form-data` с полем `file`.

Общие ограничения:

1. разрешены только явно перечисленные MIME-типы и расширения;
2. размер файла и количество строк ограничены конфигурацией;
3. файл не исполняется и не публикуется как активный контент;
4. импорт сначала попадает в staging или validation job;
5. длительная обработка возвращает `202 Accepted` и job ID;
6. статус и отчёт запрашиваются отдельными endpoint-ами;
7. ошибки отдельных строк доступны в структурированном отчёте.

## 17. Rate limiting

Минимально rate limiting применяется к:

- login;
- refresh;
- публичному поиску;
- файловым upload endpoint-ам;
- административным массовым операциям.

При превышении лимита возвращается `429 RATE_LIMITED` и, где возможно, `Retry-After`.

Конкретные лимиты определяются security и deployment design, а не жёстко фиксируются в frontend.

## 18. Каталог планируемых endpoint-ов

Все endpoint-ы ниже имеют статус `Planned`, пока код и обязательные тесты не реализованы и не сверены с этим документом.

### 18.1 Operational

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/healthz` | public operational | liveness без проверки готовности зависимостей |
| GET | `/readyz` | public/internal operational | readiness с обязательной проверкой PostgreSQL |

### 18.2 Authentication and current user

| Method | Path | Access | Назначение |
|---|---|---|---|
| POST | `/api/v1/auth/login` | public | вход по идентификатору и паролю |
| POST | `/api/v1/auth/refresh` | refresh session | ротация access/refresh session |
| POST | `/api/v1/auth/logout` | authenticated | завершение текущей сессии |
| POST | `/api/v1/auth/logout-all` | authenticated | завершение всех сессий пользователя |
| GET | `/api/v1/me` | authenticated | текущий пользователь, роль и доступный scope |
| PATCH | `/api/v1/me` | authenticated | изменение разрешённых собственных профильных полей |
| POST | `/api/v1/me/password` | authenticated | смена собственного пароля |
| GET | `/api/v1/me/sessions` | authenticated | список активных сессий без секретов |
| DELETE | `/api/v1/me/sessions/{session_id}` | authenticated | отзыв выбранной сессии |

### 18.3 Public catalog and pharmacy search

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/public/products/search` | public | поиск по торговому названию и МНН |
| GET | `/api/v1/public/products/{product_id}` | public | публичная карточка продукта |
| GET | `/api/v1/public/presentations/{presentation_id}` | public | публичная карточка фасовки |
| GET | `/api/v1/public/presentations/{presentation_id}/availability` | public | аптеки, цена и статус наличия |
| GET | `/api/v1/public/pharmacies/{pharmacy_id}` | public | публичные данные активной аптеки |

Публичный API не возвращает точные остатки, закупочные цены, номера партий, внутренний аудит или закрытые пользовательские данные.

### 18.4 Admin users and assignments

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/admin/users` | ADMIN | список пользователей |
| POST | `/api/v1/admin/users` | ADMIN | создание пользователя |
| GET | `/api/v1/admin/users/{user_id}` | ADMIN | карточка пользователя |
| PATCH | `/api/v1/admin/users/{user_id}` | ADMIN | изменение разрешённых профильных полей и статуса |
| POST | `/api/v1/admin/users/{user_id}/block` | ADMIN | блокировка пользователя |
| POST | `/api/v1/admin/users/{user_id}/unblock` | ADMIN | снятие блокировки |
| POST | `/api/v1/admin/users/{user_id}/archive` | ADMIN | архивирование пользователя |
| GET | `/api/v1/admin/users/{user_id}/pharmacy-assignments` | ADMIN | назначения аптекаря |
| POST | `/api/v1/admin/users/{user_id}/pharmacy-assignments` | ADMIN | назначение аптекаря аптеке |
| DELETE | `/api/v1/admin/users/{user_id}/pharmacy-assignments/{assignment_id}` | ADMIN | завершение назначения |
| POST | `/api/v1/admin/users/{user_id}/password-reset` | ADMIN | административный запуск безопасного сброса пароля |

### 18.5 Pharmacies

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/admin/pharmacies` | ADMIN | список аптек, включая неактивные |
| POST | `/api/v1/admin/pharmacies` | ADMIN | создание аптеки |
| GET | `/api/v1/admin/pharmacies/{pharmacy_id}` | ADMIN | административная карточка аптеки |
| PATCH | `/api/v1/admin/pharmacies/{pharmacy_id}` | ADMIN | изменение административных и публичных полей |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/activate` | ADMIN | активация аптеки |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/block` | ADMIN | блокировка новых операций |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/archive` | ADMIN | архивирование аптеки |
| GET | `/api/v1/pharmacies/{pharmacy_id}` | PHARMACIST, ADMIN + scope | внутренняя карточка аптеки |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/public-profile` | PHARMACIST, ADMIN + scope | изменение публичных данных аптеки |

### 18.6 Global catalog

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/catalog/products` | PHARMACIST, ADMIN | внутренний поиск глобального каталога |
| POST | `/api/v1/admin/catalog/products` | ADMIN | создание продукта |
| GET | `/api/v1/catalog/products/{product_id}` | PHARMACIST, ADMIN | полная карточка продукта |
| PATCH | `/api/v1/admin/catalog/products/{product_id}` | ADMIN | изменение разрешённых полей продукта |
| POST | `/api/v1/admin/catalog/products/{product_id}/archive` | ADMIN | архивирование продукта |
| POST | `/api/v1/admin/catalog/products/{product_id}/presentations` | ADMIN | создание фасовки |
| GET | `/api/v1/catalog/presentations/{presentation_id}` | PHARMACIST, ADMIN | полная карточка фасовки |
| PATCH | `/api/v1/admin/catalog/presentations/{presentation_id}` | ADMIN | изменение фасовки для будущих операций |
| POST | `/api/v1/admin/catalog/presentations/{presentation_id}/archive` | ADMIN | архивирование фасовки |
| POST | `/api/v1/admin/catalog/presentations/{presentation_id}/barcodes` | ADMIN | добавление штрихкода |
| PATCH | `/api/v1/admin/catalog/barcodes/{barcode_id}` | ADMIN | изменение primary/status штрихкода |
| DELETE | `/api/v1/admin/catalog/barcodes/{barcode_id}` | ADMIN | удаление неиспользуемого ошибочного штрихкода по политике каталога |
| POST | `/api/v1/catalog/product-requests` | PHARMACIST + scope | запрос на добавление отсутствующего товара |
| GET | `/api/v1/admin/catalog/product-requests` | ADMIN | очередь запросов аптекарей |
| PATCH | `/api/v1/admin/catalog/product-requests/{request_id}` | ADMIN | решение по запросу |

### 18.7 Catalog staging import

| Method | Path | Access | Назначение |
|---|---|---|---|
| POST | `/api/v1/admin/catalog-imports` | ADMIN | загрузка CSV/XLSX и создание import job |
| GET | `/api/v1/admin/catalog-imports` | ADMIN | список import jobs |
| GET | `/api/v1/admin/catalog-imports/{import_id}` | ADMIN | статус и итоговые счётчики |
| GET | `/api/v1/admin/catalog-imports/{import_id}/rows` | ADMIN | строки staging с фильтрами |
| PATCH | `/api/v1/admin/catalog-imports/{import_id}/rows/{row_id}` | ADMIN | исправление нормализованных данных/решения |
| POST | `/api/v1/admin/catalog-imports/{import_id}/validate` | ADMIN | повторная валидация job |
| POST | `/api/v1/admin/catalog-imports/{import_id}/publish` | ADMIN + idempotency | публикация подтверждённых строк |
| GET | `/api/v1/admin/catalog-imports/{import_id}/report` | ADMIN | загрузка отчёта ошибок |

Политика атомарной или частичной публикации должна быть окончательно определена в детальном контракте до реализации `publish`.

### 18.8 Pharmacy assortment

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/assortment` | PHARMACIST, ADMIN + scope | список локального ассортимента |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment` | PHARMACIST, ADMIN + scope | подключение глобальной фасовки к аптеке |
| GET | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}` | PHARMACIST, ADMIN + scope | карточка локального товара |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}` | PHARMACIST, ADMIN + scope | цены, правила продажи и уровни остатка |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}/archive` | PHARMACIST, ADMIN + scope | исключение из новых операций без удаления истории |
| POST | `/api/v1/pharmacies/{pharmacy_id}/assortment/{pharmacy_product_id}/activate` | PHARMACIST, ADMIN + scope | повторная активация при допустимом состоянии |

### 18.9 Receipts

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/receipts` | PHARMACIST, ADMIN + scope | список проведённых поступлений |
| POST | `/api/v1/pharmacies/{pharmacy_id}/receipts` | PHARMACIST, ADMIN + scope + idempotency | атомарное проведение поступления |
| GET | `/api/v1/pharmacies/{pharmacy_id}/receipts/{receipt_id}` | PHARMACIST, ADMIN + scope | поступление, строки и связанные лоты |
| POST | `/api/v1/pharmacies/{pharmacy_id}/receipts/{receipt_id}/reverse` | elevated permission + idempotency | отдельная компенсирующая операция |

Draft CRUD для поступлений не включён в базовый MVP: endpoint `POST /receipts` проводит документ атомарно. Если продукту потребуется отдельный жизненный цикл draft/post, он оформляется изменением SRS и API design.

### 18.10 Initial stock imports

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/initial-stock-import-template` | PHARMACIST, ADMIN | стандартный CSV/XLSX-шаблон |
| POST | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports` | PHARMACIST, ADMIN + scope | загрузка и создание validation job |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports` | PHARMACIST, ADMIN + scope | список jobs |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}` | PHARMACIST, ADMIN + scope | статус, preview и счётчики |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/rows` | PHARMACIST, ADMIN + scope | строки и ошибки сопоставления |
| PATCH | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/rows/{row_id}` | PHARMACIST, ADMIN + scope | исправление сопоставления |
| POST | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/confirm` | PHARMACIST, ADMIN + scope + idempotency | атомарное начальное оприходование |
| GET | `/api/v1/pharmacies/{pharmacy_id}/initial-stock-imports/{import_id}/report` | PHARMACIST, ADMIN + scope | отчёт ошибок |

### 18.11 Sales

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales` | PHARMACIST, ADMIN + scope | список продаж |
| POST | `/api/v1/pharmacies/{pharmacy_id}/sales` | PHARMACIST, ADMIN + scope + idempotency | атомарное проведение продажи с FEFO |
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}` | PHARMACIST, ADMIN + scope | чек, строки и допустимая информация об аллокациях |
| GET | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}/return-eligibility` | PHARMACIST, ADMIN + scope | уже возвращённые и максимально доступные количества |
| POST | `/api/v1/pharmacies/{pharmacy_id}/sales/{sale_id}/void` | elevated permission + idempotency | сторнирование только по явно утверждённой политике |

В одном request одинаковые `pharmacy_product_id + sale_unit` должны быть отклонены кодом `DUPLICATE_SALE_ITEM`, а не неявно объединяться. Это делает клиентскую ошибку заметной и сохраняет однозначную связь request-строк с ответом.

### 18.12 Returns

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/returns` | PHARMACIST, ADMIN + scope | список возвратов |
| POST | `/api/v1/pharmacies/{pharmacy_id}/returns` | PHARMACIST, ADMIN + scope + idempotency | атомарное проведение возврата по исходной продаже |
| GET | `/api/v1/pharmacies/{pharmacy_id}/returns/{return_id}` | PHARMACIST, ADMIN + scope | возврат, строки, refund и disposition |
| POST | `/api/v1/pharmacies/{pharmacy_id}/returns/{return_id}/reverse` | elevated permission + idempotency | компенсирующая операция при допустимой политике |

До юридического подтверждения production-политики endpoint проведения возврата остаётся `Planned` и не должен автоматически разрешать возврат любой лекарственной позиции.

### 18.13 Write-offs and inventory adjustments

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/write-offs` | PHARMACIST, ADMIN + scope | список списаний |
| POST | `/api/v1/pharmacies/{pharmacy_id}/write-offs` | PHARMACIST, ADMIN + scope + idempotency | проведение списания с причиной |
| GET | `/api/v1/pharmacies/{pharmacy_id}/write-offs/{write_off_id}` | PHARMACIST, ADMIN + scope | документ списания |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` | PHARMACIST, ADMIN + scope | список корректировок |
| POST | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments` | elevated permission + idempotency | проведение инвентаризационной корректировки |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-adjustments/{adjustment_id}` | PHARMACIST, ADMIN + scope | документ корректировки |

### 18.14 Inventory and stock lots

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory` | PHARMACIST, ADMIN + scope | агрегированный внутренний остаток по ассортименту |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots` | PHARMACIST, ADMIN + scope | лоты с фильтрами срока и статуса |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots/{lot_id}` | PHARMACIST, ADMIN + scope | лот и вычисляемое представление упаковок |
| GET | `/api/v1/pharmacies/{pharmacy_id}/stock-lots/{lot_id}/movements` | PHARMACIST, ADMIN + scope | append-only движения лота |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-operations` | PHARMACIST, ADMIN + scope | журнал складских операций |
| GET | `/api/v1/pharmacies/{pharmacy_id}/inventory-operations/{operation_id}` | PHARMACIST, ADMIN + scope | операция и связанные движения |
| POST | `/api/v1/admin/pharmacies/{pharmacy_id}/inventory-reconciliation` | ADMIN | запуск диагностической сверки |
| GET | `/api/v1/admin/pharmacies/{pharmacy_id}/inventory-reconciliation/{job_id}` | ADMIN | результат сверки без автоисправления |

Для `inventory_movements` отсутствуют POST/PATCH/DELETE endpoint-ы.

### 18.15 Alerts and recommendations

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/pharmacies/{pharmacy_id}/alerts` | PHARMACIST, ADMIN + scope | активные и исторические предупреждения |
| GET | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}` | PHARMACIST, ADMIN + scope | карточка предупреждения |
| POST | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}/acknowledge` | PHARMACIST, ADMIN + scope | подтверждение просмотра |
| POST | `/api/v1/pharmacies/{pharmacy_id}/alerts/{alert_id}/resolve` | PHARMACIST, ADMIN + scope | ручное закрытие, если разрешено типом |
| GET | `/api/v1/pharmacies/{pharmacy_id}/replenishment-recommendations` | PHARMACIST, ADMIN + scope | рекомендации без автоматического заказа |

### 18.16 Audit

| Method | Path | Access | Назначение |
|---|---|---|---|
| GET | `/api/v1/admin/audit-events` | ADMIN | системный аудит с фильтрами и пагинацией |
| GET | `/api/v1/admin/audit-events/{audit_event_id}` | ADMIN | безопасные детали события |
| GET | `/api/v1/pharmacies/{pharmacy_id}/audit-events` | ADMIN или ограниченная аудиторская роль | аудит в пределах аптеки |

Для audit events отсутствуют PATCH и DELETE endpoint-ы.

## 19. Общие коды бизнес-ошибок

Начальный каталог стабильных кодов:

| Code | Category | Назначение |
|---|---|---|
| `INVALID_CREDENTIALS` | unauthenticated | неверные данные входа без уточнения существования пользователя |
| `ACCOUNT_BLOCKED` | forbidden | пользователь заблокирован |
| `SESSION_REVOKED` | unauthenticated | refresh/access session отозвана |
| `PHARMACY_ACCESS_DENIED` | forbidden/not found | нет активного назначения аптеке |
| `PHARMACY_INACTIVE` | business rule | аптека не принимает новые операции |
| `RESOURCE_ARCHIVED` | business rule | ресурс архивирован для новых операций |
| `DUPLICATE_BARCODE` | conflict | штрихкод уже принадлежит другой фасовке |
| `DUPLICATE_DOCUMENT_NUMBER` | conflict | номер документа уже используется в аптеке |
| `DUPLICATE_SALE_ITEM` | invalid argument | повтор товара и единицы в одном sale request |
| `INNER_UNIT_SALE_DISABLED` | business rule | продажа внутренней единицы запрещена |
| `PRESCRIPTION_CONFIRMATION_REQUIRED` | business rule | нет обязательного подтверждения рецепта |
| `INSUFFICIENT_STOCK` | business rule | недостаточный допустимый остаток |
| `LOT_EXPIRED` | business rule | операция пытается использовать просроченный лот |
| `LOT_QUARANTINED` | business rule | лот находится в карантине |
| `RETURN_QUANTITY_EXCEEDED` | business rule | превышен доступный остаток возврата |
| `RETURN_NOT_LEGALLY_ALLOWED` | business rule | возврат запрещён утверждённой политикой |
| `IDEMPOTENCY_KEY_REQUIRED` | invalid argument | отсутствует обязательный ключ |
| `IDEMPOTENCY_KEY_REUSED` | conflict | ключ повторно использован с другим payload |
| `IMPORT_HAS_ERRORS` | business rule | job нельзя подтвердить из-за ошибок строк |
| `IMPORT_ALREADY_CONFIRMED` | conflict | импорт уже проведён |
| `CONCURRENT_MODIFICATION` | conflict | состояние изменилось конкурентной операцией |

Каталог расширяется только стабильными machine-readable значениями. Текст `message` может локализоваться или уточняться без изменения `code`.

## 20. Contract status и сопровождение

Статусы endpoint-а:

- `Planned`;
- `In Progress`;
- `Implemented`;
- `Deprecated`;
- `Removed`.

HTTP feature считается завершённой только если:

1. endpoint соответствует SRS;
2. handler не содержит бизнес-логики;
3. request/response DTO отделены от domain entities;
4. ошибки проходят через централизованный responder;
5. authentication и scope authorization проверяются;
6. добавлены contract/integration tests;
7. endpoint полностью описан в этом документе;
8. JSON-примеры соответствуют фактическим DTO;
9. idempotency и transaction boundaries протестированы, если применимы;
10. статус изменён на `Implemented` только после полной сверки.

## 21. Следующий этап детализации

Перед реализацией каждой feature соответствующий раздел должен быть расширен до полного endpoint-контракта:

- path/query parameters;
- headers;
- request JSON и таблица полей;
- success responses;
- endpoint-specific errors;
- idempotency semantic hash;
- transaction side effects;
- audit behavior;
- корректный request/response/error example.

Первой рекомендуется детализировать вертикальный срез:

1. `POST /api/v1/auth/login`;
2. `GET /api/v1/me`;
3. `GET /api/v1/catalog/products`;
4. `POST /api/v1/pharmacies/{pharmacy_id}/assortment`;
5. `POST /api/v1/pharmacies/{pharmacy_id}/receipts`;
6. `GET /api/v1/pharmacies/{pharmacy_id}/inventory`;
7. `POST /api/v1/pharmacies/{pharmacy_id}/sales`.

Этот набор проверяет authentication, authorization, каталог, pharmacy scope, Unit of Work, idempotency, immutable movements, FEFO и единые response envelopes на одном сквозном сценарии.
