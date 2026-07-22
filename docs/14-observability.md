# PharmacyCRM — Observability

> Retention observability for schema `23` reports one bounded cycle budget and a real cycle deadline; an internal deadline is recorded as limited work, not as a repository failure.

**Статус документа:** Draft  
**Версия:** 1.1  
**Дата:** 2026-07-22  
**Связанные документы:** `02-srs.md`, `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `05-api-design.md`, `06-database-design.md`, `07-domain-model.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`, `12-deployment.md`, `13-testing-strategy.md`

## 1. Назначение и нормативная роль

Документ определяет обязательную observability-модель PharmacyCRM: technical logs, transactional audit, metrics, distributed tracing, frontend telemetry, SLI/SLO, dashboards, alerts, retention, redaction, operational evidence и правила диагностики инцидентов.

Observability является частью correctness и operability, а не поздним инфраструктурным дополнением. Критическая операция не считается завершённой, пока для неё не определены:

- стабильные log и audit event names;
- обязательные correlation fields;
- metric contract и cardinality budget;
- trace boundaries и sampling policy;
- failure и integrity signals;
- SLI impact;
- actionable alerts и runbooks, если требуется реакция;
- dashboard или иной approved diagnostic view;
- retention и access classification;
- автоматические observability tests.

Изменение log/audit schema, metric name/labels, trace propagation, sampling, SLI/SLO, alert routing, retention, redaction или incident evidence синхронизирует этот документ в том же change set.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует ADR или зарегистрированного operational risk.
- **Может** — допустимый вариант.

## 3. Цели observability

Observability PharmacyCRM должна позволять достоверно ответить:

1. доступна ли система пользователям и какие journeys нарушены;
2. какая версия frontend, API, worker, schema и worker protocol обслуживает traffic;
3. где конкретный request/job проводит время;
4. завершилась, откатилась, повторилась или зависла ли business operation;
5. соблюдаются ли transaction, idempotency, audit и inventory invariants;
6. есть ли lock contention, deadlock, pool exhaustion, slow query или retry storm;
7. обрабатываются ли outbox, imports, alerts и projections;
8. насколько свежа public availability projection;
9. возникли ли authorization, session или security anomalies;
10. свежи ли backups и доказан ли restore;
11. что изменилось после deployment;
12. достаточно ли evidence для безопасного incident response.

Observability не должна:

- становиться источником business truth;
- заменять transactional audit;
- раскрывать secrets, credentials, medical/search context или unrestricted payloads;
- создавать unbounded cardinality;
- выполнять synchronous remote export внутри transaction;
- менять transaction boundary, lock order или retry semantics;
- скрывать собственную деградацию.

## 4. Модель сигналов и источники истины

| Сигнал | Назначение | Источник истины | Допустимая потеря |
|---|---|---|---|
| Technical logs | диагностика runtime и ошибок | process/runtime | возможна по retention/degradation policy |
| Transactional audit | неизменяемая история значимых действий | PostgreSQL transaction | недопустима для обязательной audited operation |
| Metrics | агрегаты, SLI, capacity и alerts | instrumented components/exporters | ограниченная, но обязательно обнаружимая |
| Traces | причинная цепочка и latency breakdown | propagated trace context | допустима согласно sampling policy |
| Reconciliation evidence | доказательство data integrity | authoritative DB data | недопустима для завершённого reconciliation run |
| Deployment/recovery evidence | доказательство release/restore | deployment и recovery process | недопустима для approved release/restore |

Ни один отдельный сигнал не является достаточным доказательством критического business effect. Успешный commit подтверждается business state и audit; logs, metrics и traces используются для корреляции и диагностики.

## 5. Общие observability invariants

1. Все server timestamps формируются в UTC.
2. Logs и audit используют RFC 3339 с миллисекундной точностью; metrics используют seconds согласно backend convention.
3. Каждый HTTP request получает валидный `request_id`.
4. Каждый traceable request/job получает `trace_id` и `span_id`.
5. `request_id` возвращается клиенту и присутствует в связанных logs и audit.
6. Критическая mutation имеет стабильное `operation` и event namespace.
7. Business effect и обязательный audit commit-ятся атомарно.
8. Technical log не является доказательством commit.
9. Metric labels не содержат unbounded IDs, raw paths, queries, messages или user input.
10. Raw token, password, cookie, authorization header, DSN, private key и secret value не попадают ни в один сигнал.
11. Telemetry pipeline failure сам наблюдаем.
12. Release, schema и worker protocol versions видимы в operational signals.
13. Signal schema имеет versioning и backward-compatibility policy.
14. Security/audit events не sampling-ятся и не silently drop-ятся.
15. Telemetry exporter не вызывается синхронно внутри UoW transaction.
16. Observability platform не используется для штатного изменения business data.

## 6. Correlation model

### 6.1 `request_id`

`request_id` идентифицирует один входящий HTTP request.

Правила:

- входной `X-Request-ID` принимается только после проверки allowlisted format и maximum length;
- невалидное значение заменяется server-generated ID;
- значение не является authentication proof;
- оно присутствует в response header и `meta.request_id` API envelope;
- transaction retries сохраняют исходный `request_id`;
- новый клиентский retry создаёт новый `request_id`, но может использовать тот же `Idempotency-Key`.

### 6.2 `trace_id` и `span_id`

Trace context принимается только по утверждённому стандарту. Невалидные или неподдерживаемые headers игнорируются и заменяются безопасным server context.

Baggage запрещено использовать для secrets, actor IDs, pharmacy IDs, medical/search strings и произвольных пользовательских данных.

### 6.3 Business correlation

В restricted logs/audit/traces могут использоваться:

- `actor_id`;
- opaque `session_id`;
- `pharmacy_id`;
- document/operation ID;
- safe idempotency record ID или hash scope;
- `outbox_event_id`;
- `import_job_id`;
- `deployment_id`;
- `reconciliation_run_id`.

Эти значения запрещены как metric labels.

### 6.4 Async correlation

Outbox record может содержать безопасный producer trace context. Worker создаёт новый trace и связывает его с producer через span link. Время ожидания в очереди измеряется отдельно и не изображается как один долгоживущий active span.

## 7. Signal schema governance

Каждый machine-consumed signal contract имеет:

- owner;
- namespace;
- semantic description;
- schema version;
- required и optional fields;
- data classification;
- retention class;
- compatibility policy;
- tests;
- deprecation window.

Breaking rename/removal поля, event или metric не выполняется без migration window для dashboards, alerts, queries и runbooks.

Human-readable `message` не является API. Automation не должна зависеть от текста сообщения.

## 8. Structured logging через Zap

Backend использует `zap.Logger`. Logger создаётся в composition root и передаётся как dependency. Global mutable logger запрещён, кроме ограниченного bootstrap fallback до построения dependency graph.

Production format — structured JSON. Local console encoder допустим, но semantic fields сохраняются.

Логи выводятся одновременно:

- в terminal (`stdout`/`stderr`);
- в file sink через approved mounted path/volume, если это является требованием окружения.

### 8.1 Базовая log schema

| Поле | Семантика |
|---|---|
| `timestamp` | UTC RFC 3339 с миллисекундами |
| `level` | `debug`, `info`, `warn`, `error`, `fatal` |
| `service` | `api`, `worker`, `migrate` или approved process |
| `environment` | `local`, `ci`, `staging`, `production` |
| `version` | release version |
| `commit_sha` | immutable source revision |
| `schema_version` | active database schema version |
| `worker_protocol_version` | если применимо |
| `instance_id` | replica/process identity |
| `event` | stable machine-readable name |
| `event_schema_version` | версия event schema |
| `message` | bounded safe description |
| `request_id` | HTTP correlation, если применимо |
| `trace_id` | trace correlation, если применимо |
| `span_id` | active span, если применимо |
| `operation` | stable use case/job name |
| `outcome` | bounded enum |
| `duration_ms` | duration в миллисекундах |
| `error_code` | stable category |

Неприменимые поля отсутствуют. Фиктивные `unknown`, `n/a` и пустые строки не используются вместо корректной optional semantics.

### 8.2 HTTP access log

На request создаётся одна итоговая access-log запись со следующими применимыми полями:

- `http.method`;
- normalized `http.route`;
- sanitized `http.request_path` без query string;
- `http.status_code`;
- `http.request_size_bytes`;
- `http.response_size_bytes`;
- `duration_ms`;
- safe network identifier согласно privacy policy;
- bounded/sanitized user-agent или его classification;
- `request_id`, `trace_id`;
- authenticated actor/session/pharmacy IDs только в restricted sink;
- `error_code`;
- `outcome`.

Требование проекта о поле `response` реализуется как structured summary: status, size, error code и outcome. Полное response body запрещено.

Query string, request body и response body по умолчанию не логируются. Allowlisted metadata требует security review и redaction test.

### 8.3 Event naming

Используется dot-separated lower-case namespace:

```text
http.request.completed
auth.login.denied
auth.refresh.reuse_detected
pharmacy.assignment.ended
inventory.receipt.posted
sales.sale.completed
inventory.adjustment.posted
idempotency.conflict
transaction.retry
outbox.processing.failed
migration.completed
backup.failed
reconciliation.divergence_detected
telemetry.export.dropped
```

Event name стабилен. Изменение semantics требует новой schema version или нового event name.

### 8.4 Duplicate suppression и sampling

- transactional audit не sampling-ится;
- security events не sampling-ятся;
- error/fatal events не sampling-ятся без отдельной approved aggregation strategy;
- debug events могут sampling-иться;
- high-volume success access logs могут sampling-иться только после подтверждения, что SLI, security и investigation не зависят от полного потока;
- suppression обязана иметь counters для suppressed/dropped events.

## 9. Log levels

### `debug`

Детальная диагностика. Production disabled по умолчанию. Временное включение требует owner, scope, TTL и change/incident record.

### `info`

Нормальные lifecycle и successful operational events: startup/shutdown, request completion, worker completion, migration, release marker, backup completion.

### `warn`

Контролируемая деградация: retryable transaction, rate limit, idempotency conflict, stale lease rejection, projection lag warning, deprecated configuration.

Ожидаемые validation/domain denials не должны массово логироваться как `error`.

### `error`

Unexpected failure или failure, влияющий на SLI/correctness: exhausted retry, audit failure, poison event, migration/backup failure, telemetry drop, reconciliation divergence.

### `fatal`

Используется только перед controlled process termination, когда safe startup/runtime продолжение невозможно. Library code не вызывает `Fatal` или `Panic`.

## 10. Error logging

Errors классифицируются через `errors.Is()` и `errors.As()`. String/substr matching для определения категории запрещён.

Error log содержит:

- stable `error_code`;
- bounded `error_kind`;
- operation/route;
- correlation IDs;
- transaction attempt, если применимо;
- safe internal context;
- stack trace только для unexpected internal/panic errors.

Во внешний response не попадают stack, SQL, constraint names, file paths, internal topology или driver message.

## 11. Redaction и data classification

### 11.1 Всегда запрещённые значения

- passwords и password hashes;
- raw access/refresh/reset/MFA tokens;
- cookies и Authorization;
- JWT signing/encryption keys;
- DSN и database credentials;
- secret manager values;
- backup encryption secrets;
- unrestricted uploaded file contents;
- raw recovery codes;
- private key material.

### 11.2 Restricted values

Только при доказанной необходимости и с restricted access/retention:

- actor/session identifiers;
- phone/email;
- IP/network identifiers;
- user agent;
- search text;
- medicine/product name в пользовательском поисковом контексте;
- import filename;
- reason/comment fields.

Пользовательские строки:

- ограничиваются по длине;
- очищаются от control characters и log forging sequences;
- передаются как structured values;
- не используются в metric labels/event names;
- не участвуют в alert deduplication keys.

### 11.3 Redaction boundary

Redaction выполняется как можно ближе к producer. Central collector redaction является дополнительной защитой, а не единственным контролем.

CI проверяет отсутствие secrets и restricted payloads в logs, traces, labels, responses, frontend telemetry и crash reports.

## 12. Terminal, file sink и rotation

### 12.1 Startup

Если file logging обязательен, process fail-fast при невозможности:

- открыть approved path;
- подтвердить ownership/permissions;
- настроить rotation;
- выполнить startup probe write.

### 12.2 Runtime sink failure

File sink failure:

- не откатывает корректную business transaction;
- переключается на available fallback sink;
- увеличивает self-monitoring metric;
- создаёт actionable alert;
- не отключает transactional audit.

### 12.3 Rotation

Определяются:

- max file size;
- max age;
- max retained files;
- compression;
- disk thresholds;
- ownership/permissions;
- reopen behavior после rotation.

Application не удерживает удалённый rotated descriptor бесконтрольно. Rotation и disk-full behavior тестируются.

## 13. Transactional audit

Audit — отдельная append-only business/security history.

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
- bounded metadata;
- audit schema version.

Для mandatory audited mutation:

1. business writes выполняются внутри UoW;
2. audit insert выполняется в той же PostgreSQL transaction;
3. audit failure вызывает rollback;
4. technical signal создаётся best-effort;
5. audit failure metric и SEV alert обязательны.

Technical logs, metrics и traces не заменяют audit и не используются для восстановления stock truth.

## 14. Metrics architecture

Metrics должны быть агрегируемыми, стабильными и bounded по cardinality. Предпочтительна OpenMetrics/Prometheus-compatible model; конкретный backend утверждается operational decision.

### 14.1 Naming и units

Имена используют prefix `pharmacycrm_`.

```text
pharmacycrm_http_requests_total
pharmacycrm_http_request_duration_seconds
pharmacycrm_db_transaction_retries_total
pharmacycrm_outbox_oldest_unprocessed_age_seconds
```

- counters заканчиваются `_total`;
- seconds/bytes отражаются в имени;
- gauges описывают текущее состояние;
- duration/size используют histograms;
- summaries не используются без отдельного обоснования из-за aggregation limitations.

### 14.2 Label allowlist

Допустимы bounded labels:

- `service`;
- `environment`;
- normalized route class;
- method;
- bounded status/error class;
- stable operation allowlist;
- outcome enum;
- worker/job type;
- migration/deployment result;
- bounded retry reason.

Запрещены:

- user/session/pharmacy/product/lot/document IDs;
- request/trace IDs;
- raw URL/path/query;
- message/error text;
- filenames;
- idempotency key;
- IP/user agent;
- arbitrary user input.

### 14.3 Cardinality budget

Для каждой новой metric определяется:

- ожидаемое число series;
- maximum label combinations;
- owner;
- retention;
- query use cases;
- dashboard/alert consumers.

CI проверяет label allowlist и cardinality regression на representative inputs. Total production series budget и per-service budget утверждаются до pilot.

### 14.4 Histograms

Buckets выбираются по user-facing SLO и measured distribution, а не по случайным defaults. Изменение buckets считается contract change для recording rules и dashboards.

### 14.5 Exemplars

Где backend поддерживает exemplars, histogram observation может содержать trace exemplar без создания trace ID label.

## 15. Platform и runtime metrics

### 15.1 HTTP RED

- request rate;
- duration histogram;
- status/error rate;
- in-flight requests;
- request/response sizes;
- timeout/cancellation;
- rate limiting;
- panic recovery.

### 15.2 Runtime USE

- CPU;
- heap/RSS;
- goroutines;
- GC duration;
- file descriptors;
- restarts;
- uptime;
- log sink/rotation failures;
- telemetry queue/drop/export errors.

### 15.3 PostgreSQL и pool

- total/acquired/idle connections;
- acquisition wait count/duration;
- connection errors;
- transaction duration;
- commit/rollback;
- retry by bounded reason;
- deadlock/serialization failures;
- lock wait duration;
- statement/transaction timeout;
- long transactions;
- slow normalized query class;
- DB/WAL/disk capacity из infrastructure exporter.

Raw SQL parameters не записываются. Raw SQL text допускается только в restricted diagnostics с approved normalization/redaction.

## 16. Reliability metrics

Минимально:

- UoW transactions по outcome;
- UoW duration;
- transaction retries и exhausted retries;
- idempotency claims, replays, conflicts и in-progress waits;
- mandatory audit writes/failures;
- reconciliation runs/divergences;
- cancellation/timeout rollbacks;
- lock wait и deadlock indicators.

Metrics не являются доказательством единичной операции; они агрегируют тенденции.

## 17. Worker, outbox и import metrics

### 17.1 Outbox/workers

- pending backlog;
- oldest unprocessed age;
- claim rate;
- queue wait duration;
- processing duration;
- success/failure/retry;
- dead-letter count;
- lease expiry/recovery;
- exhausted leases terminalized per poll и cumulative count с reason `LEASE_EXPIRED_AFTER_MAX_ATTEMPTS`;
- terminalization batch saturation (`rows_affected == configured_limit`) как сигнал накопленного exhausted backlog;
- stale fencing rejection;
- heartbeat/readiness;
- worker protocol mismatch;
- worker mode (`delivery` или `maintenance_only`) и unexpected early process exit;
- drain timeout и cancellation-grace exhaustion;
- projection lag.
- retention batches/deleted rows по bounded `status` (`PROCESSED`, `DEAD_LETTER`);
- retention cycle failures.

При пустом E2 protocol registry worker публикует mode `maintenance_only`: claim неизвестных protocols отсутствует, но terminalization и retention heartbeat остаются наблюдаемыми. Repository получает `RowsAffected()` для exhausted-lease terminalization и проверяет, что result не превышает bounded limit. На E2 существующий observer interface не расширяется ради одного счётчика: значение доступно internal test seam и может быть выведено structured worker log/metric через существующую observability abstraction без изменения repository contract. Один poll не должен создавать unbounded WAL/lock spike; saturation и outbox oldest age рассматриваются совместно.

### 17.2 Imports

- jobs по bounded state;
- upload size/row-count histograms;
- parse/validation/moderation duration;
- validation findings по bounded category;
- moderation queue age;
- poison/failed jobs;
- quarantine storage usage;
- retention cleanup failure.

Filename, product name и row contents не являются labels.

## 18. Business health и integrity signals

Business metrics служат anomaly detection, но не заменяют authoritative tables или reconciliation.

Минимально:

- receipts/sales/returns/adjustments posted/failed;
- rejected sales по bounded domain code;
- stock-out events;
- low-stock/near-expiry/expired alerts;
- public availability freshness;
- reconciliation divergence;
- unusual adjustment/write-off volume;
- duplicate/replay prevention events;
- negative-stock invariant signal.

P0 integrity signals имеют ожидаемое значение `0` и не покрываются обычным error budget.

Pharmacy-level segmentation запрещена по умолчанию. Исключение требует bounded tenant count, privacy review и series budget.

## 19. Security signals

Минимально:

- login success/failure;
- blocked/archived login attempts;
- refresh denial/reuse;
- session family revocation;
- `401`/`403`/`429` anomaly;
- cross-pharmacy denial;
- assignment/role denial;
- sensitive ADMIN operation;
- confidential/mass export;
- idempotency conflict anomaly;
- audit failure;
- scanner findings;
- unusual correction/write-off volume;
- backup/restore authorization failure.

Concrete actor/target записывается в restricted log/audit. Metrics используют только bounded category.

## 20. Distributed tracing

Tracing должна быть OpenTelemetry-compatible или следовать эквивалентному approved standard.

### 20.1 Required spans

Применимые spans:

- HTTP ingress;
- authentication/authorization policy;
- application use case;
- UoW transaction attempt;
- repository normalized query class;
- idempotency claim/replay;
- transactional audit insert;
- outbox append;
- worker claim/process;
- import parse/validate/moderate;
- approved external dependency call;
- migration/backup/restore.

Чистые быстрые domain methods обычно не создают spans.

### 20.2 Span attributes

Допустимы bounded attributes:

- normalized route;
- method/status class;
- service/environment/version;
- operation;
- outcome/error code;
- retry attempt;
- database system и normalized operation class;
- worker/job type;
- schema/worker protocol version.

Запрещены secrets, bodies, SQL parameters, raw user input и unbounded identifiers как indexed attributes.

### 20.3 Transaction retries

Каждая transaction attempt представлена child span или span event с attempt number и bounded retry reason. Parent use-case span показывает конечный outcome.

### 20.4 Sampling

Sampling policy определяет:

- baseline head sampling;
- обязательное сохранение error и high-latency traces через approved tail sampling, если platform поддерживает;
- security/privacy restrictions;
- maximum queue/memory;
- drop behavior;
- sampling metadata.

Sampling не влияет на audit и metrics correctness.

## 21. Frontend observability

Frontend telemetry содержит:

- release/version;
- route/page class без sensitive parameters;
- API error code и request ID;
- unhandled error classification;
- Core Web Vitals или approved UX metrics;
- session-expiry/logout/stale-response protection outcomes;
- failed asset/API compatibility indicators.

Запрещено отправлять access/refresh tokens, cookies, form contents, search text, medicine names или unrestricted component state.

Source maps не публикуются открыто. Они хранятся в restricted build/telemetry system и привязаны к immutable release.

Frontend telemetry failure не блокирует UX, но должна быть измерима при следующем доступном export path.

## 22. Telemetry pipeline architecture

Предпочтительная модель:

```text
API / Worker / Frontend
        |
        | bounded async export
        v
Collector / Log Agent
        |
        +--> Logs backend
        +--> Metrics backend
        +--> Trace backend
        +--> Alert evaluation
```

Правила:

- application имеет bounded queues;
- remote exporter использует timeout и backoff;
- exporter retry не создаёт retry storm;
- backpressure не блокирует business transaction;
- dropped telemetry учитывается;
- collector configuration versioned и tested;
- collector outage не отключает transactional audit;
- local buffering имеет explicit size/age limits.

## 23. Self-observability

Observability pipeline публикует:

- accepted/exported/dropped signal counts;
- queue utilization;
- export latency/errors;
- collector health/restarts;
- ingestion lag;
- storage/index capacity;
- alert evaluation errors;
- alert delivery failures;
- dashboard/recording rule query failures;
- file sink/rotation errors.

Отсутствие self-monitoring делает observability platform непригодной для production.

## 24. SLI definitions

До pilot утверждаются точные queries и eligibility rules.

### 24.1 API availability

```text
eligible successful server outcomes / all eligible requests
```

Client-caused expected `4xx` исключаются только по заранее утверждённой классификации. Server misclassification как `4xx` не должна улучшать SLI.

### 24.2 API latency

Для key journeys измеряется доля eligible successful requests в пределах threshold и полное p50/p95/p99 distribution:

- login/refresh;
- protected reads;
- receipt posting;
- sale posting;
- public search.

### 24.3 Integrity

- negative stock events = 0;
- duplicate irreversible effects = 0;
- mandatory audit failures = 0;
- unexplained reconciliation divergence = 0;
- false completed idempotency results = 0.

Integrity violations являются incident signals, а не обычной availability статистикой.

### 24.4 Worker/projection freshness

- outbox oldest age;
- queue wait;
- public projection lag;
- import/moderation queue age;
- alert generation delay.

### 24.5 Recovery

- backup freshness/success;
- restore drill duration;
- measured RPO/RTO;
- reconciliation success после restore.

## 25. SLO и error budgets

Numerical SLO утверждаются до pilot после baseline measurements.

Каждый SLO определяет:

- exact SLI query;
- eligible scope;
- measurement window;
- exclusions;
- target;
- owner;
- error budget;
- multi-window burn alerts;
- action при burn;
- dashboard и runbook.

Availability/latency alerts должны использовать multi-window multi-burn-rate подход или эквивалент, уменьшающий шум и одновременно обнаруживающий быстрые и медленные regressions.

P0 integrity/security events не имеют error budget. Одно подтверждённое событие запускает incident и блокирует release/pilot.

## 26. Alerting principles

Alert должен быть:

- actionable;
- symptom-oriented;
- связан с user/business impact;
- привязан к owner и runbook;
- deduplicated;
- устойчив к коротким безопасным колебаниям;
- проверен synthetic/fault-injection test;
- bounded по cardinality;
- иметь trigger и clear condition.

Alert без owner, severity, route, runbook и evidence links не допускается в production.

Каждая ожидаемая единичная `4xx` не создаёт alert. Security-sensitive single event может создавать event alert.

## 27. Severity model

### SEV-1

- authorization bypass;
- negative stock/corruption;
- duplicate irreversible effect;
- mandatory audit unavailable;
- active secret compromise;
- production data loss;
- restore невозможен в аварии.

### SEV-2

- key receipt/sale workflow unavailable;
- sustained high 5xx/latency burn;
- prolonged outbox/projection backlog;
- DB pool exhaustion;
- backup freshness breach;
- incompatible worker protocol after release.

### SEV-3

- limited degradation;
- latency increase без correctness impact;
- partial telemetry loss при сохранённом audit;
- capacity warning;
- isolated import/worker class failure.

### SEV-4

Informational/anomaly signal без немедленного user impact.

Exact response-time и escalation policy утверждаются до production.

## 28. Минимальный production alert set

1. API availability burn;
2. critical journey latency burn;
3. panic/unhandled error spike;
4. PostgreSQL unavailable;
5. pool acquisition saturation;
6. retry exhaustion/deadlock storm;
7. long lock waits/transactions;
8. mandatory audit failure;
9. negative stock/reconciliation divergence;
10. duplicate-effect/idempotency integrity signal;
11. outbox backlog/oldest age;
12. worker heartbeat/protocol mismatch;
13. poison/dead-letter growth;
14. public projection stale;
15. refresh reuse/security anomaly;
16. failed-login/authorization/rate-limit anomaly;
17. unusual ADMIN/export/adjustment activity;
18. migration failure/schema ambiguity;
19. deployment failure/incompatible mixed versions;
20. backup failure/staleness;
21. restore drill failure;
22. disk/WAL/log capacity;
23. clock drift;
24. telemetry pipeline drop/outage;
25. alert delivery failure.

## 29. Alert lifecycle

Каждый alert определяет:

- unique stable name;
- severity;
- owner и backup owner;
- routing/on-call channel;
- runbook;
- trigger и clear condition;
- deduplication key;
- inhibition/suppression rules;
- maintenance policy;
- expected response time;
- evidence links.

Silence допускается только с TTL, owner, reason, scope и change/incident reference. Silence не должна скрывать unrelated incidents.

Acknowledgement не закрывает root cause. Закрытие происходит после нормализации signal и регистрации follow-up, если он необходим.

## 30. Dashboards

Минимально:

### 30.1 System overview

Availability, latency, errors, active versions, deployment markers, replicas, DB/pool, outbox lag, backup freshness и reconciliation state.

### 30.2 API

RED metrics, stable error codes, auth/session outcomes, rate limit, slow requests, panics.

### 30.3 PostgreSQL

Connection budget, locks, long transactions, deadlocks, timeouts, query classes, WAL/disk growth, backup/replication state.

### 30.4 Workers/outbox

Backlog, oldest age, throughput, retries, dead letters, leases, fencing, latency, projection freshness, protocol distribution.

### 30.5 Inventory integrity

Receipts/sales/returns/adjustments, rejection reasons, reconciliation, negative-stock signal, unusual corrections, expiry signals.

### 30.6 Security

Login failures, refresh reuse, revocations, authorization denials, sensitive ADMIN operations, exports, audit failures, scanner findings.

### 30.7 Deployment/recovery

Versions/digests, migration state, readiness/restarts, smoke result, backup freshness, restore/RPO/RTO evidence.

Dashboards не требуют raw IDs для первичной диагностики. Drill-down к restricted identifiers выполняется через controlled logs/traces/audit access.

## 31. Deployment markers

Каждый deployment публикует marker:

- deployment ID;
- environment;
- commit/tag;
- frontend/API/worker image digests;
- schema version;
- worker protocol version;
- actor/automation identity;
- started/completed/failed timestamps;
- migration result;
- smoke result.

Markers отображаются на ключевых dashboards и trace/log searches.

## 32. Observability-as-code

В repository или approved configuration repository versioned-ятся:

- metric recording rules;
- alert rules;
- dashboards;
- collector configuration;
- log parsing/schema rules;
- redaction rules;
- SLI queries;
- runbook references.

CI проверяет syntax, schema, duplicate names, missing owners/runbooks, forbidden labels и unsafe queries.

Manual production-only alert/dashboard change без последующей фиксации в version control запрещён, кроме incident emergency procedure.

## 33. Retention и storage

Утверждённый minimum baseline: application logs — 30 days hot + 180 days archive; traces — 7 days; audit/inventory/sales history — минимум 5 лет или дольше по legal hold. Storage implementation дополнительно классифицирует:

- technical logs;
- restricted security logs;
- audit;
- metrics;
- traces;
- frontend telemetry;
- deployment evidence;
- incident evidence;
- backup/restore evidence.

Retention учитывает legal requirements, investigation window, privacy minimization, storage cost, legal hold и deletion/anonymization process.

Короткий trace retention не уменьшает audit retention. Incident evidence может иметь отдельный legal hold.

## 34. Access control

Доступ разделяется минимум на:

- ordinary technical logs;
- restricted security logs;
- transactional audit;
- metrics/dashboards;
- traces;
- incident evidence;
- observability administration.

Обязательны least privilege, personal identities, MFA для admin access, access logging, export restrictions и отсутствие shared root accounts.

Runtime application получает только write-only/minimal telemetry credentials. Она не получает administrative query/delete permissions observability backend.

## 35. Incident evidence

Для SEV-1/SEV-2 сохраняются:

- UTC timeline;
- release/deployment markers;
- relevant logs/traces;
- metric snapshots и exact query definitions;
- audit references;
- DB lock/activity evidence;
- reconciliation reports;
- safe configuration/version metadata;
- containment/recovery actions;
- decisions/owners;
- follow-up tasks.

Скриншот без query, time range, timezone и release context недостаточен.

Evidence collection не изменяет production business data и имеет controlled access/export audit.

## 36. Runbooks

Минимальные runbooks:

- API error/latency burn;
- PostgreSQL unavailable/pool exhaustion;
- lock contention/deadlock storm;
- audit failure;
- inventory divergence;
- outbox backlog/poison event;
- worker split-brain/protocol mismatch;
- stale public projection;
- failed deployment/migration;
- backup failure/restore;
- secret/session compromise;
- telemetry pipeline outage;
- disk/WAL/log saturation;
- clock drift.

Runbook содержит impact/severity, immediate checks, safe containment, forbidden actions, diagnostic queries, rollback/forward-fix criteria, evidence, escalation, recovery verification и follow-up.

## 37. Failure semantics

### 37.1 Technical logging failure

Не откатывает business transaction. Используется fallback sink, self-monitoring и alert.

### 37.2 Metrics/tracing failure

Не блокирует business operation. Используются bounded queues, timeouts, backoff и mandatory drop counters.

### 37.3 Transactional audit failure

Для mandatory audited operation вызывает rollback.

### 37.4 Alert delivery failure

Создаёт secondary health signal и alternate route. Один delivery channel не считается достаточным для SEV-1 без approved risk.

### 37.5 Observability backend outage

Система продолжает безопасную работу в пределах buffering/capacity. Если потеря visibility делает critical operations небезопасными, incident commander ограничивает rollout или critical commands согласно runbook.

### 37.6 Telemetry overload

При overload сначала уменьшается optional/debug telemetry. Audit, security и integrity signals имеют приоритет и отдельный capacity path.

## 38. Performance и cost budget

Измеряются:

- logging allocation/serialization;
- sync file write cost;
- metrics series/memory;
- tracing queue/sampling/export;
- frontend bundle/network overhead;
- telemetry egress;
- disk/index growth;
- observability platform cost.

Запрещён synchronous remote export внутри transaction.

До pilot задаются maximum acceptable overhead и capacity budget. Превышение budget требует optimization или sampling/retention change без ослабления audit/security/integrity coverage.

## 39. Observability testing

Обязательны:

- log/event schema contract;
- event schema version compatibility;
- timestamp UTC/precision;
- request/trace correlation;
- stable event/error names;
- secret/body/header redaction;
- log-forging sanitization;
- metric name/label allowlist;
- cardinality budget regression;
- histogram units/buckets;
- exemplar/trace correlation, если используется;
- trace propagation и async links;
- retry attempt visibility;
- sampling behavior;
- audit failure → rollback + signal;
- log sink/exporter/collector outage;
- telemetry drop/backpressure;
- file rotation/disk-full;
- alert synthetic evaluation;
- multi-window burn-rule tests;
- alert routing/inhibition smoke;
- dashboard/recording rule query smoke;
- deployment marker visibility;
- backup/restore/reconciliation signals;
- frontend telemetry redaction;
- telemetry overhead baseline.

Fault injection из `13-testing-strategy.md` должна подтверждать наличие достаточного diagnostic evidence.

## 40. CI и release gates

### Pull request

- observability contract review;
- log/metric/trace schema tests;
- redaction tests;
- cardinality calculation;
- dashboard/alert/collector syntax;
- runbook existence для actionable alert;
- compatibility/deprecation review.

### Main/nightly

- exporter/collector failure tests;
- alert synthetic evaluation;
- dashboard query tests;
- rotation/disk behavior;
- trace sampling/propagation;
- self-observability checks;
- overhead baseline.

### Release candidate

- production dashboards доступны;
- alert routing и alternate critical route проверены;
- deployment markers работают;
- SLI queries подтверждены;
- backup/recovery signals видимы;
- retention/access настроены;
- critical runbooks исполнимы;
- telemetry capacity достаточна;
- нет открытых P0/P1 diagnostic gaps.

## 41. Definition of Ready

Observability change готов к реализации, когда:

1. известны operation/journey и criticality;
2. определены outcomes и failure modes;
3. определён audit requirement;
4. определены logs/events и schema version;
5. определены metrics, labels и cardinality budget;
6. определены spans и sampling;
7. известен SLI/alert impact;
8. назначены owner/runbook;
9. определены data classification, retention и access;
10. сформулированы observability tests.

## 42. Definition of Done

Observability завершена, если:

1. stable event/operation names определены;
2. schema version и compatibility policy определены;
3. correlation fields присутствуют;
4. secrets и unrestricted payloads отсутствуют;
5. mandatory audit атомарен с business effect;
6. metrics имеют units, owner и bounded cardinality;
7. traces отражают retries и async boundaries;
8. sampling и drop semantics определены;
9. telemetry failure self-observable;
10. SLI/dashboard обновлены;
11. actionable alert имеет severity, route, owner и runbook;
12. observability-as-code прошла CI;
13. retention/access policy соблюдается;
14. deployment markers позволяют связать regression с release;
15. fault tests оставляют достаточное evidence;
16. нет P0/P1 diagnostic gap.

## 43. Запрещённые практики

Запрещено:

- использовать `fmt.Println` как production logging;
- логировать secrets, cookies, Authorization, raw tokens или bodies;
- использовать user input как format string;
- создавать metric labels из IDs, URLs, queries или messages;
- строить alert на human-readable message;
- считать отсутствие logs доказательством отсутствия ошибок;
- заменять audit technical log;
- выполнять synchronous remote telemetry export внутри transaction;
- игнорировать drops/export failures;
- создавать alert без owner/runbook;
- открыто публиковать source maps;
- менять SLI query для сокрытия regression;
- использовать dashboard вместо alerting;
- создавать manual production-only rules без version control;
- использовать observability platform для business mutation;
- sampling-ить mandatory audit/security events;
- включать debug logging в production без TTL и owner.

## 44. Открытые решения

До production необходимо утвердить:

1. logs/metrics/traces backend и hosting model;
2. OpenTelemetry SDK/export protocol и collector topology;
3. production file rotation implementation;
4. storage tiering, deletion jobs и capacity verification для утверждённой retention policy;
5. exact SLI formulas и numerical SLO;
6. alert manager, routing и on-call model;
7. severity/escalation policy;
8. trace sampling/tail-sampling policy;
9. histogram buckets;
10. frontend telemetry platform;
11. source map storage/access;
12. restricted security log storage;
13. dashboards/alerts/collector configuration as code tooling;
14. total series, storage и cost budgets;
15. observability platform ownership;
16. incident evidence retention/legal hold;
17. event/metric deprecation window;
18. privacy policy для network identifiers и search-related telemetry.

Открытые tooling-решения не отменяют обязательные требования schema, redaction, correlation, audit, self-monitoring и diagnostic evidence.
