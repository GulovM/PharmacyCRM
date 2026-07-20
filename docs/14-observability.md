# PharmacyCRM — Observability

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-20  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`, `13-testing-strategy.md`

## 1. Назначение и нормативная роль

Документ определяет observability-модель PharmacyCRM: технические логи, audit-события, metrics, distributed tracing, SLI/SLO, dashboards, alerts, retention, redaction, operational evidence и правила расследования инцидентов.

Observability является частью корректности системы, а не поздним эксплуатационным дополнением. Новая критическая операция не считается завершённой, пока для неё не определены:

- структурированные log events;
- обязательный transactional audit;
- metrics и допустимая cardinality;
- trace boundaries;
- failure signals;
- alert и runbook, если отказ требует реакции;
- dashboard или существующее представление, позволяющее диагностировать состояние;
- тесты observability-контракта.

Изменение log schema, audit semantics, metric names/labels, trace propagation, SLI/SLO, alert routing, retention, redaction или incident evidence должно обновлять этот документ в том же change set.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует ADR или зарегистрированного operational risk.
- **Может** — допустимый вариант.

## 3. Цели observability

Observability PharmacyCRM должна позволять достоверно ответить:

1. доступна ли система пользователям;
2. какая версия API, worker, frontend и schema сейчас активна;
3. выполняется ли конкретный request и где он задерживается;
4. какая business operation завершилась, откатилась или повторяется;
5. соблюдаются ли transaction, idempotency, audit и inventory invariants;
6. есть ли contention, deadlock, pool exhaustion или slow query;
7. обрабатываются ли outbox, import, alert и projection jobs;
8. насколько свежа публичная availability projection;
9. не возникли ли authorization, session или security anomalies;
10. свежи ли backups и доказано ли восстановление;
11. что изменилось после deployment;
12. какие доказательства необходимо сохранить для incident review.

Observability не должна:

- раскрывать secrets, credentials или unrestricted payloads;
- становиться вторым источником бизнес-истины;
- заменять transactional audit;
- создавать неограниченную metric cardinality;
- блокировать business traffic из-за необязательного telemetry exporter;
- скрывать потерю telemetry без отдельного сигнала.

## 4. Модель сигналов

Используются четыре взаимосвязанных класса сигналов:

| Сигнал | Назначение | Источник истины |
|---|---|---|
| Technical logs | диагностика событий и ошибок | runtime processes |
| Transactional audit | неизменяемая история значимых действий | PostgreSQL transaction |
| Metrics | агрегированное состояние, SLI и alerts | instrumented components/exporters |
| Traces | причинная цепочка request/job и latency breakdown | propagated trace context |

Дополнительно используются:

- deployment events;
- backup/restore evidence;
- reconciliation reports;
- incident records;
- CI/release evidence.

Ни один отдельный сигнал не считается достаточным для расследования критического инцидента. Корреляция выполняется через `request_id`, `trace_id`, operation identifiers, target identifiers, release version и server timestamps.

## 5. Общие observability invariants

1. Время формируется на сервере в UTC с миллисекундной точностью.
2. Каждый HTTP request получает валидный `request_id`.
3. Каждый traceable request/job получает `trace_id` и `span_id`.
4. `request_id` возвращается клиенту и присутствует в связанных logs и audit.
5. Критическая mutation имеет стабильное `operation`/event name.
6. Успешный business effect и обязательный audit commit-ятся атомарно.
7. Technical log не является доказательством commit.
8. Metric label не содержит unbounded ID, URL, query, message или user input.
9. Raw token, password, cookie, authorization header, DSN и encryption key не попадают ни в один сигнал.
10. Отказ telemetry pipeline сам наблюдаем и alert-ится.
11. Telemetry не изменяет transaction boundary и lock order.
12. Release version и schema compatibility видимы во всех operational representations.

## 6. Correlation model

### 6.1 `request_id`

`request_id` идентифицирует одну входящую HTTP-операцию.

Правила:

- входной `X-Request-ID` принимается только после проверки формата и длины;
- невалидное значение заменяется server-generated ID;
- доверять идентификатору как доказательству личности запрещено;
- значение присутствует в response header и `meta.request_id` JSON envelope;
- один request использует одно значение во всех слоях;
- internal retry transaction не создаёт новый `request_id`.

### 6.2 `trace_id` и `span_id`

`trace_id` связывает distributed operation. `span_id` идентифицирует конкретный участок выполнения.

Внешний trace context принимается только по утверждённому стандарту и валидируется. Неутверждённые tracing headers игнорируются.

### 6.3 Business identifiers

Для расследования могут записываться:

- `user_id`;
- `session_id` как opaque internal ID;
- `pharmacy_id`;
- `operation_id` / document ID;
- `idempotency_scope_hash` или безопасный идентификатор записи;
- `outbox_event_id`;
- `import_job_id`;
- `deployment_id`.

Запрещено использовать эти значения как metric labels. Они допустимы в logs, audit и traces с учётом access control и retention.

## 7. Structured logging через Zap

Backend использует `zap.Logger`. Production logging должен быть structured JSON. Human-readable console encoder допустим для local development, но набор смысловых полей остаётся тем же.

Логи выводятся одновременно:

- в terminal (`stdout`/`stderr`);
- в файл через утверждённый mounted path/volume, если это является обязательным требованием окружения.

Logger создаётся в composition root и передаётся как dependency. Глобальный mutable logger запрещён, кроме контролируемого bootstrap fallback до построения dependency graph.

### 7.1 Обязательные базовые поля

Каждая запись содержит применимые поля:

| Поле | Семантика |
|---|---|
| `timestamp` | UTC RFC 3339 с миллисекундами |
| `level` | `debug`, `info`, `warn`, `error`, `fatal` |
| `service` | `api`, `worker`, `migrate` или другой утверждённый process |
| `environment` | `local`, `ci`, `staging`, `production` |
| `version` | release/version |
| `commit_sha` | immutable source revision |
| `instance_id` | replica/process identity |
| `event` | стабильное machine-readable event name |
| `message` | краткое безопасное описание |
| `request_id` | HTTP correlation ID, если применимо |
| `trace_id` | trace correlation ID, если применимо |
| `span_id` | active span ID, если применимо |
| `operation` | стабильное имя use case/job |
| `outcome` | `success`, `denied`, `failed`, `cancelled`, `retried` |
| `duration_ms` | длительность операции в миллисекундах |
| `error_code` | стабильный internal/public category без текста driver error |

Поля со значением, не применимым к событию, могут отсутствовать. Нельзя заполнять их фиктивными строками вроде `unknown`, если отсутствие имеет ясную семантику.

### 7.2 HTTP access log schema

Одна итоговая access-log запись создаётся на request и содержит:

- `http.method`;
- `http.route` — шаблон route, например `/api/v1/pharmacies/{pharmacy_id}/sales`;
- `http.request_path` — нормализованный path без query string, если это безопасно;
- `http.status_code`;
- `http.request_size_bytes`;
- `http.response_size_bytes`;
- `duration_ms`;
- `client.network_id` согласно privacy policy;
- bounded/sanitized `user_agent` либо его классификацию;
- `request_id`, `trace_id`;
- authenticated `actor_id`, `session_id`, `pharmacy_id`, когда они известны;
- `error_code`;
- `outcome`.

Требование проекта о поле `response` реализуется как безопасный результат ответа: status, size, error code и outcome. Полное response body логировать запрещено.

Query string, request body и response body по умолчанию не логируются. Точечное логирование allowlisted metadata требует security review.

### 7.3 Event naming

`event` использует стабильный lower snake case либо dot-separated convention, выбранный один раз для проекта. Примеры:

```text
http.request.completed
auth.login.denied
auth.refresh.reuse_detected
inventory.receipt.posted
sales.sale.posted
inventory.adjustment.posted
idempotency.conflict
transaction.retry
outbox.processing.failed
migration.completed
backup.failed
reconciliation.divergence_detected
```

Message может изменяться для читаемости; automation и alerts не должны зависеть от `message`.

## 8. Log levels

### `debug`

Используется для локальной диагностики и временной детализации. Production debug logging выключено по умолчанию и не должно активироваться без ограниченного срока и change/incident record.

### `info`

Нормальные lifecycle и завершённые значимые технические операции:

- process started/stopped;
- request completed;
- worker job completed;
- migration applied;
- release marker;
- backup completed.

### `warn`

Неуспешное, но контролируемое состояние:

- retryable transaction error;
- rate limit;
- idempotency conflict;
- stale lease rejection;
- projection lag выше warning threshold;
- deprecated configuration;
- partial dependency degradation без потери correctness.

### `error`

Операция завершилась ошибкой и требует расследования либо влияет на SLI:

- unhandled internal error;
- audit insert failure;
- exhausted retries;
- worker poison event;
- migration failure;
- backup failure;
- observability export/drop failure;
- reconciliation divergence.

### `fatal`

Используется только перед немедленным завершением process из-за невозможности безопасного startup/runtime продолжения. Библиотечный код не вызывает `Fatal`/`Panic`.

## 9. Error logging

Errors сравниваются через `errors.Is()` и извлекаются через `errors.As()`. Logging не использует substring matching для определения категории.

Ошибка записывается с:

- стабильным `error_code`;
- безопасным `error_kind`;
- operation/route;
- correlation IDs;
- retry attempt, если применимо;
- bounded internal message;
- stack trace только для unexpected internal/panic errors.

Запрещено записывать:

- raw PostgreSQL query parameters с чувствительными данными;
- constraint/table names во внешний response;
- password/token/cookie/Authorization;
- full request/response body;
- filesystem secret path вместе с содержимым;
- unrestricted panic value.

Stack trace хранится только во внутреннем technical log с ограниченным доступом.

## 10. Redaction и data classification

### 10.1 Всегда запрещённые значения

- passwords и password hashes;
- raw access/refresh/reset/MFA tokens;
- cookies;
- Authorization header;
- JWT signing/encryption keys;
- database DSN/credentials;
- secret manager values;
- backup encryption secrets;
- unrestricted uploaded file contents;
- payment/financial secret data, если появится.

### 10.2 Ограниченные значения

Следующие данные допускаются только при необходимости и с retention/access policy:

- user/session IDs;
- phone/email;
- IP/network identifiers;
- user agent;
- search text;
- medicine/product names, если они могут раскрыть чувствительный пользовательский контекст;
- import filenames;
- reason/comment fields.

Пользовательские строки:

- ограничиваются по длине;
- очищаются от control characters и log forging sequences;
- записываются как structured values, не как format string;
- не используются в metric labels.

### 10.3 Redaction tests

CI содержит regression tests, подтверждающие отсутствие секретов в:

- logs;
- traces;
- metrics labels;
- error responses;
- frontend telemetry;
- crash reports.

## 11. Terminal и file logging

### 11.1 Startup

Если file logging обязателен конфигурацией окружения, process обязан fail-fast при невозможности:

- создать/открыть log path;
- подтвердить допустимые permissions;
- настроить rotation;
- записать startup probe event.

### 11.2 Runtime failure

Ошибка file sink после startup:

- не откатывает уже корректную business transaction;
- записывается в доступный fallback sink, обычно `stderr`;
- увеличивает telemetry self-monitoring metric;
- создаёт alert;
- не отключает transactional audit.

### 11.3 Rotation

Для file logs задаются:

- maximum file size;
- maximum age;
- maximum retained files;
- compression policy;
- disk free-space thresholds;
- ownership и permissions.

Application не должна бесконтрольно удерживать удалённый rotated file descriptor. Rotation behavior тестируется.

## 12. Transactional audit и отличие от logs

Audit является отдельной append-only бизнес/security историей.

Audit event содержит минимум:

- event ID;
- server timestamp;
- actor type/ID;
- session ID, если применимо;
- action;
- target type/ID;
- pharmacy ID;
- result;
- reason code;
- request ID;
- trace ID;
- normalized network identifier;
- bounded metadata.

Для обязательной audited mutation:

1. business changes выполняются внутри UoW;
2. audit event вставляется в той же PostgreSQL transaction;
3. audit failure вызывает rollback;
4. technical log об audit failure создаётся best-effort;
5. metric и alert фиксируют отказ.

Technical logs:

- могут быть потеряны или ротированы согласно operational policy;
- не являются append-only business history;
- не заменяют audit;
- не должны использоваться для восстановления stock truth.

## 13. Metrics architecture

Metrics должны быть агрегируемыми, стабильными и ограниченными по cardinality.

Предпочтительно использовать совместимую с OpenMetrics/Prometheus модель, однако конкретный backend утверждается отдельным operational decision.

### 13.1 Naming

Имена metrics используют единый prefix, например:

```text
pharmacycrm_http_requests_total
pharmacycrm_http_request_duration_seconds
pharmacycrm_db_transaction_retries_total
pharmacycrm_outbox_oldest_unprocessed_age_seconds
```

Units отражаются в имени: `_seconds`, `_bytes`, `_total`.

Counters не уменьшаются. Gauges используются только для текущего состояния. Duration/size используют histograms с утверждёнными buckets.

### 13.2 Cardinality policy

Разрешённые labels:

- `service`;
- `environment`;
- normalized `route`;
- `method`;
- coarse `status_class` или bounded `status_code`;
- stable `error_code`;
- `operation` из allowlist;
- `outcome` из bounded enum;
- `worker_type`;
- `job_type`;
- `migration_result`;
- `deployment_result`.

Запрещённые labels:

- user/session/pharmacy/product/lot/sale IDs;
- request/trace IDs;
- raw URL/path/query;
- error message;
- filename;
- idempotency key;
- IP/user agent;
- arbitrary user input.

Новая label dimension требует review ожидаемой cardinality.

## 14. Golden signals и platform metrics

### 14.1 HTTP RED metrics

Обязательны:

- request rate;
- request duration histogram;
- response status/error rate;
- in-flight requests;
- request/response size;
- timeout/cancellation count;
- rate-limit count;
- panic recovery count.

### 14.2 Process/runtime USE metrics

- CPU;
- memory/heap;
- goroutines;
- GC duration;
- file descriptors;
- restarts;
- uptime;
- open log file/rotation errors;
- telemetry exporter queue/drop/error counts.

### 14.3 PostgreSQL/pool metrics

- pool total/acquired/idle connections;
- acquisition wait count/duration;
- connection errors;
- transaction duration;
- commit/rollback counts;
- retry count по bounded reason;
- deadlock/serialization count;
- lock wait duration;
- statement/transaction timeout count;
- slow query count по normalized query class;
- database size/WAL/disk capacity из инфраструктурного exporter.

Raw SQL и bound parameters не должны попадать в metric labels.

## 15. Reliability и transaction metrics

Обязательны:

- `uow_transaction_total{outcome}`;
- `uow_transaction_duration_seconds{operation,outcome}`;
- `transaction_retry_total{operation,reason}`;
- `transaction_retry_exhausted_total{operation}`;
- `idempotency_claim_total{operation,outcome}`;
- `idempotency_replay_total{operation}`;
- `idempotency_conflict_total{operation}`;
- `audit_write_total{action,outcome}` с bounded action class;
- `audit_write_failure_total{action_class}`;
- `reconciliation_run_total{scope,outcome}`;
- `reconciliation_divergence_total{scope}`.

Metrics не считаются доказательством единичной операции; для этого используются audit/log/trace identifiers.

## 16. Worker, outbox и import metrics

### 16.1 Outbox/workers

- pending backlog;
- oldest unprocessed event age;
- claim rate;
- processing duration;
- success/failure/retry counts;
- dead-letter count;
- lease expiry/recovery count;
- stale fencing token rejection;
- worker heartbeat/readiness;
- protocol version mismatch;
- projection update lag.

### 16.2 Imports

- jobs по состоянию;
- upload size и row count в bounded histograms;
- parsing duration;
- validation findings count по bounded category;
- moderation queue age;
- failed/poison job count;
- quarantine storage usage;
- cleanup/retention failures.

Filename, product name и row content не являются metric labels.

## 17. Business health и integrity metrics

Business metrics служат для обнаружения аномалий, но не заменяют доменные таблицы и reconciliation.

Минимальный набор:

- receipts posted/failed;
- sales posted/failed;
- returns posted/failed;
- write-offs/adjustments count по bounded reason class;
- stock-out count;
- low-stock/near-expiry/expired alerts;
- public availability projection freshness;
- rejected sale count по bounded domain error;
- inventory reconciliation divergence;
- unusual adjustment volume;
- duplicate/replay prevention events.

Денежные суммы, количества и pharmacy segmentation публикуются только при утверждённой privacy/cardinality policy. Metric по отдельной аптеке допустима лишь при малом bounded числе и formal review; по умолчанию pharmacy ID label запрещён.

## 18. Security metrics и events

Обязательные security signals:

- login success/failure rate;
- failed-login spike;
- blocked/archived login attempts;
- refresh success/denial;
- refresh-token reuse;
- session family revocation;
- `401`/`403`/`429` rate;
- cross-pharmacy access denial;
- assignment/role denial;
- ADMIN sensitive operation count;
- mass/confidential export;
- idempotency conflict spike;
- audit failure;
- secret/dependency/container scanner findings;
- unusual correction/write-off volume;
- backup/restore authorization failures.

Security event с конкретным actor/target записывается в restricted log/audit. Metric содержит только bounded category.

## 19. Distributed tracing

Tracing implementation должна быть совместима с OpenTelemetry semantic model либо эквивалентным утверждённым стандартом.

### 19.1 Обязательные spans

Применимые spans создаются для:

- HTTP ingress;
- authentication/authorization policy;
- application use case;
- UoW transaction attempt;
- repository query class;
- idempotency claim/replay;
- transactional audit insert;
- outbox append;
- worker claim/process;
- import parse/validate/moderate;
- external map/object-storage request;
- migration/backup/restore operation.

Domain methods обычно не создают spans, если они являются чистыми быстрыми функциями.

### 19.2 Span attributes

Допустимы:

- normalized route;
- method/status;
- service/environment/version;
- operation;
- outcome/error code;
- retry attempt;
- database system и normalized operation class;
- worker/job type;
- schema/worker protocol version.

Запрещены:

- secrets/tokens/cookies;
- request/response bodies;
- raw SQL parameters;
- arbitrary product/user input;
- unbounded IDs как индексируемые attributes без утверждённой policy.

### 19.3 Transaction retries

Каждая transaction attempt является отдельным child span либо span event с attempt number и bounded retry reason. Итоговый use-case span отражает общий outcome.

### 19.4 Async propagation

Outbox record может сохранять безопасный producer trace context.

Worker:

- создаёт новый processing trace;
- связывает его с producer через span link;
- не изображает долгую очередь как непрерывный активный span;
- записывает queue age отдельно.

### 19.5 Sampling

До production утверждается sampling policy:

- низкая/адаптивная доля успешных частых requests;
- повышенная выборка ошибок и slow operations;
- обязательное сохранение критических security/integrity incidents через audit/logs независимо от trace sampling;
- защита от sampling amplification при атаке.

Trace sampling не влияет на audit completeness.

## 20. Frontend observability

Frontend telemetry должна включать:

- release/version;
- page/route class;
- uncaught error/error boundary events;
- failed API calls по stable error code/status class;
- session refresh/logout failures;
- stale-response suppression events при необходимости;
- Web Vitals/performance baseline;
- asset load failures;
- map provider degradation;
- browser E2E synthetic monitoring, если введено.

Frontend получает `request_id` из response и отображает/передаёт его в support context без раскрытия internal details.

Запрещено отправлять:

- tokens/cookies;
- form contents;
- unrestricted search text;
- credentials;
- sensitive pharmacy/user data;
- raw stack/source maps в публично доступный backend.

Source maps, если используются, хранятся в ограниченном observability service либо не публикуются.

## 21. Health и telemetry self-monitoring

Observability pipeline обязан наблюдать сам себя.

Минимальные signals:

- log sink write failures;
- dropped log entries;
- file rotation errors;
- exporter queue depth;
- exporter retry/drop count;
- metrics scrape failures;
- trace export failures;
- telemetry backend ingestion lag;
- dashboard query errors;
- alert evaluation/delivery failures;
- clock drift;
- disk usage log/telemetry volumes.

Нельзя считать отсутствие logs доказательством отсутствия ошибок.

`/healthz` не зависит от удалённого telemetry backend. `/readyz` также не становится false только из-за необязательного metrics/tracing exporter, если business correctness не нарушена. Потеря обязательного audit остаётся fail-closed на уровне операции.

## 22. SLI

До pilot должны быть утверждены SLI минимум для:

### 22.1 API availability

Доля eligible requests, завершившихся допустимым server outcome. Client-caused `4xx` не считаются server failure, кроме специально классифицированных случаев неправильной server behavior.

### 22.2 API latency

Latency distribution для ключевых route classes:

- authentication;
- protected reads;
- receipt posting;
- sale posting;
- public search.

### 22.3 Correctness/integrity

- доля critical mutations без unexplained reconciliation divergence;
- duplicate irreversible effect count;
- negative stock count;
- mandatory audit failure count;
- idempotency false-complete/duplicate count.

Для P0 integrity событий допустимое значение — ноль.

### 22.4 Worker/projection freshness

- outbox oldest age;
- projection lag;
- import/moderation queue age;
- alert generation delay.

### 22.5 Recovery

- backup success/freshness;
- restore drill completion time;
- measured RPO/RTO;
- reconciliation success после restore.

## 23. SLO и error budgets

Конкретные numerical SLO утверждаются до pilot после baseline measurements и фиксируются отдельной operational policy/ADR.

Каждый SLO должен определять:

- SLI query/formula;
- scope и user journey;
- measurement window;
- exclusions;
- target;
- owner;
- error budget;
- action при burn;
- dashboard и alert references.

Запрещено улучшать availability SLI путём скрытия ошибок, исключения неудобных routes или переклассификации server failures в client errors.

Integrity/security P0 events не покрываются обычным error budget. Одно подтверждённое событие блокирует release/pilot и запускает incident process.

## 24. Alerting principles

Alerts должны быть:

- actionable;
- symptom-oriented;
- привязаны к owner и runbook;
- дедуплицированы;
- устойчивы к кратким безопасным колебаниям;
- проверены synthetic/fault-injection test;
- защищены от unbounded cardinality.

Alert без owner, severity, routing, runbook и clear condition не допускается к production.

Alerts не должны создаваться на каждую единичную ожидаемую `4xx` ошибку. Для security-sensitive единичных событий допускается event alert.

## 25. Severity model

### SEV-1 — критический инцидент

Примеры:

- authorization bypass;
- отрицательный stock или подтверждённая corruption;
- duplicate irreversible effect;
- невозможность обязательного audit;
- active secret compromise;
- production data loss;
- restore невозможен при аварии.

Требуется немедленная эскалация и containment.

### SEV-2 — существенная деградация

- высокий server error rate;
- sale/receipt workflow недоступен;
- длительный outbox/projection backlog;
- DB pool exhaustion;
- backup freshness нарушена;
- worker protocol mismatch после release.

### SEV-3 — ограниченная деградация

- рост latency без нарушения correctness;
- отдельный worker/import class деградирован;
- telemetry частично потеряна при сохранённом audit;
- disk/capacity warning.

### SEV-4 — информационный сигнал

- approaching capacity threshold;
- deprecated configuration;
- scheduled maintenance evidence;
- non-urgent anomaly для анализа.

Точная severity/routing policy утверждается до production.

## 26. Обязательные alerts

Минимальный production набор:

1. API availability/error-budget burn;
2. critical route latency burn;
3. panic/unhandled error spike;
4. PostgreSQL unavailable;
5. pool acquisition saturation;
6. deadlock/serialization retry exhaustion;
7. long lock waits/long transactions;
8. mandatory audit write failure;
9. negative stock/reconciliation divergence;
10. duplicate effect/idempotency integrity signal;
11. outbox oldest age/backlog;
12. worker heartbeat/protocol mismatch;
13. poison/dead-letter growth;
14. public projection stale;
15. refresh-token reuse/security anomaly;
16. failed-login/401/403/429 anomaly;
17. unusual ADMIN/export/adjustment activity;
18. migration failure/ambiguous schema version;
19. deployment failed/mixed incompatible versions;
20. backup failure/staleness;
21. restore drill failure;
22. disk/WAL/log volume capacity;
23. clock drift;
24. log/metric/trace pipeline failure;
25. alert delivery failure.

## 27. Alert routing и lifecycle

Каждый alert определяет:

- unique name;
- severity;
- service/domain owner;
- primary/secondary contact;
- channel/on-call route;
- runbook URL/path;
- trigger и clear condition;
- deduplication key;
- suppression/maintenance policy;
- expected response time;
- evidence links.

Alert acknowledgement не закрывает root cause. Alert закрывается после нормализации signal и фиксации incident/task, если требуется follow-up.

Silence допускается только:

- на ограниченное время;
- с owner и причиной;
- с scope, не скрывающим другие incidents;
- с change/incident reference.

## 28. Dashboards

### 28.1 System overview

- availability/latency/error rate;
- active release/schema versions;
- API/worker replica health;
- deployment markers;
- open SEV alerts;
- DB/pool status;
- outbox lag;
- backup freshness;
- reconciliation state.

### 28.2 API dashboard

- RED metrics по normalized route class;
- top stable error codes;
- auth/session outcomes;
- rate limiting;
- slow requests;
- panic count.

### 28.3 PostgreSQL dashboard

- connections/pool budget;
- lock waits;
- long transactions;
- deadlocks/serialization failures;
- statement timeouts;
- slow query classes;
- DB/WAL/disk growth;
- replication/backup status, если применимо.

### 28.4 Worker/outbox dashboard

- backlog/oldest age;
- throughput;
- retry/dead-letter;
- lease recovery/fencing rejection;
- processing latency;
- projection freshness;
- worker protocol/version distribution.

### 28.5 Inventory integrity dashboard

- receipts/sales/returns/adjustments;
- rejected operations;
- negative stock invariant signal;
- reconciliation runs/divergences;
- unusual write-off/adjustment volume;
- expired/near-expiry alerts;
- projection divergence.

### 28.6 Security dashboard

- login failures;
- refresh reuse;
- session revocations;
- authorization denials;
- ADMIN sensitive operations;
- mass exports;
- audit failures;
- scanner findings.

### 28.7 Deployment/recovery dashboard

- deployed versions/digests;
- migration state;
- readiness/restarts;
- deployment duration/result;
- smoke result;
- backup freshness;
- restore drill/RPO/RTO evidence.

Dashboard не должен требовать знания raw internal IDs для первичной диагностики.

## 29. Deployment markers

Каждый deployment публикует marker/event с:

- deployment ID;
- environment;
- commit/tag;
- API/worker/frontend image digest;
- schema version;
- worker protocol version;
- actor/automation identity;
- started/completed/failed timestamp;
- migration result;
- smoke result.

Markers отображаются на latency/error/backlog dashboards, чтобы изменения после release были видимы.

## 30. Retention и storage

До production утверждаются отдельные retention policies для:

- technical logs;
- security logs;
- audit events;
- metrics;
- traces;
- frontend telemetry;
- deployment evidence;
- incident evidence;
- backup/restore evidence.

Retention учитывает:

- законодательные требования;
- расследование инцидентов;
- стоимость хранения;
- privacy/data minimization;
- immutable/legal hold;
- deletion/anonymization procedure.

Короткий retention traces не может уменьшать обязательный audit retention. Incident evidence может получить отдельный legal hold.

## 31. Access control

Доступ разделяется минимум на:

- обычные application logs;
- restricted security logs;
- transactional audit;
- metrics/dashboards;
- traces;
- incident evidence;
- observability administration.

Правила:

- least privilege;
- персональные identities;
- MFA для административного доступа, где поддерживается;
- access logging;
- запрет shared root accounts;
- ограничение export/download;
- отдельное approval для bulk sensitive evidence.

Observability platform credentials не выдаются runtime application шире, чем необходимо для write-only telemetry export.

## 32. Incident evidence

Для SEV-1/SEV-2 сохраняются:

- incident timeline в UTC;
- deployment markers;
- relevant logs;
- traces;
- metric snapshots/query definitions;
- audit event references;
- database lock/activity evidence;
- reconciliation reports;
- configuration/version metadata без secret values;
- containment/recovery actions;
- owner decisions;
- post-incident tasks.

Evidence должна быть привязана к времени и immutable release IDs. Скриншот без query, time range и version context недостаточен.

Сбор evidence не должен изменять production business data.

## 33. Runbooks

Минимально создаются runbooks для:

- API error/latency spike;
- PostgreSQL unavailable/pool exhaustion;
- lock contention/deadlock storm;
- audit pipeline failure;
- inventory reconciliation divergence;
- outbox backlog/poison event;
- worker split-brain/protocol mismatch;
- public projection staleness;
- failed deployment/migration;
- backup failure/restore;
- secret/session compromise;
- telemetry pipeline outage;
- disk/WAL/log volume saturation;
- clock drift.

Runbook содержит:

1. impact и severity;
2. immediate checks;
3. safe containment;
4. запрещённые действия;
5. diagnostic queries/dashboards;
6. rollback/forward-fix criteria;
7. evidence requirements;
8. escalation contacts;
9. recovery verification;
10. post-incident follow-up.

## 34. Observability failure semantics

### 34.1 Technical log failure

Не откатывает business transaction, но создаёт fallback signal и alert.

### 34.2 Metrics/tracing exporter failure

Не блокирует business operation. Export использует bounded queues, timeouts и backpressure policy. При заполнении очереди telemetry может быть отброшена с обязательным drop metric/log.

### 34.3 Transactional audit failure

Для операции с обязательным audit вызывает rollback.

### 34.4 Alert delivery failure

Фиксируется отдельным secondary route/health signal. Нельзя полагаться на один канал доставки критических alerts без утверждённого риска.

### 34.5 Observability backend outage

System продолжает безопасную работу в пределах local buffering/capacity. Если потеря видимости делает эксплуатацию опасной, incident commander может ограничить critical commands или остановить rollout согласно runbook.

## 35. Performance и overhead budget

Instrumentation обязана иметь измеренный overhead.

Контролируются:

- logging allocation/serialization cost;
- sync file write behavior;
- metric collection cardinality/memory;
- tracing sampling/export queue;
- frontend telemetry bundle/network cost;
- observability network egress;
- disk usage.

Запрещено выполнять synchronous remote telemetry call внутри business transaction.

Structured access logging не должно удерживать request после формирования response дольше утверждённого budget. File logging policy должна учитывать durability и throughput без создания глобальной serialization bottleneck.

## 36. Observability testing

Обязательные tests:

- log schema contract;
- timestamp precision и UTC;
- request/trace correlation;
- stable event/error codes;
- secret/header/body redaction;
- control-character/log-forging sanitization;
- metric name/label allowlist;
- cardinality regression;
- histogram unit/buckets review;
- trace propagation и async span links;
- retry attempt visibility;
- audit failure → rollback + signal;
- log sink/exporter outage;
- dropped telemetry signal;
- file rotation/disk threshold;
- alert rule evaluation на synthetic data;
- alert routing smoke;
- dashboard query smoke;
- deployment marker visibility;
- backup/restore/reconciliation signals;
- frontend stale-state/error telemetry redaction.

Fault injection из `13-testing-strategy.md` должна подтверждать не только корректность системы, но и наличие достаточного diagnostic evidence.

## 37. CI и release gates

### Pull request

Проверяются:

- observability contract изменений;
- log/metric/trace schema tests;
- redaction tests;
- metric cardinality policy;
- dashboard/alert definitions syntax;
- runbook link existence для нового actionable alert.

### Main/nightly

Дополнительно:

- telemetry exporter failure tests;
- alert synthetic evaluation;
- dashboard query tests;
- log rotation;
- trace sampling/propagation;
- observability overhead baseline.

### Release candidate

Обязательны:

- production dashboards доступны;
- alert routing проверен;
- deployment markers работают;
- SLI queries подтверждены;
- backup/recovery signals видимы;
- telemetry retention/access настроены;
- critical runbooks исполнимы;
- нет открытых P0/P1 observability gaps.

## 38. Definition of Ready для observability feature

Изменение готово к реализации, когда:

1. известны operation/user journey и criticality;
2. определены success/failure outcomes;
3. известны audit requirements;
4. определены required logs и redaction;
5. определены metrics и bounded labels;
6. определены trace boundaries;
7. известны SLI/alert impact;
8. назначены owner и runbook при actionable failure;
9. retention/access impact оценён;
10. observability tests сформулированы.

## 39. Definition of Done для observability feature

Observability считается завершённой только если:

1. stable event/operation names определены;
2. logs содержат обязательные correlation fields;
3. secrets и unrestricted bodies не записываются;
4. mandatory audit атомарен с business effect;
5. metrics имеют units и bounded cardinality;
6. traces отражают transaction retry и async boundary;
7. telemetry failure semantics определены;
8. SLI/dashboard обновлены;
9. actionable alert имеет severity, owner, route и runbook;
10. tests проверяют schema, redaction и failure signals;
11. deployment/release markers позволяют коррелировать regressions;
12. retention/access policy соблюдается;
13. documentation обновлена в том же change set;
14. нет открытого P0/P1 diagnostic gap.

## 40. Запрещённые практики

Запрещено:

- использовать `fmt.Println` как штатный production logging;
- логировать secrets, cookies, Authorization или raw tokens;
- логировать полные request/response bodies по умолчанию;
- использовать user input как format string;
- создавать metric labels из IDs, URLs, queries или error messages;
- строить alert на нестабильном human-readable message;
- считать отсутствие logs отсутствием ошибок;
- заменять audit техническим log;
- выполнять remote telemetry export синхронно внутри transaction;
- игнорировать telemetry drop/export failure;
- создавать alert без owner/runbook;
- хранить source maps публично без осознанного решения;
- изменять SLI query, чтобы скрыть release regression;
- считать dashboard достаточным без проверенного alerting;
- использовать observability platform для штатного изменения business data.

## 41. Открытые решения

До production необходимо утвердить:

1. metrics/logs/traces backend и hosting model;
2. OpenTelemetry SDK/export protocol и collector topology;
3. production log file rotation implementation;
4. exact retention по каждому сигналу;
5. SLI formulas и numerical SLO targets;
6. alert manager, routing и on-call model;
7. severity/escalation policy;
8. trace sampling policy;
9. metric histogram buckets;
10. frontend telemetry/error reporting platform;
11. source map storage/access;
12. restricted security log storage;
13. dashboards-as-code и alert-rules-as-code tooling;
14. observability cost/capacity budget;
15. ownership observability platform и incident evidence retention.

Открытые tooling-решения не отменяют обязательные schema, redaction, correlation, audit и diagnostic requirements этого документа.
