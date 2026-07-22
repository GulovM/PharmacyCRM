# PharmacyCRM — Deployment

**Статус документа:** Draft  
**Версия:** 1.1  
**Дата:** 2026-07-21  
**Связанные документы:** `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `06-database-design.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`

## 1. Назначение и нормативная роль

Документ определяет целевую модель сборки, конфигурации, развёртывания, миграций, эксплуатации, резервного копирования и восстановления PharmacyCRM.

Deployment Design является нормативным для local, CI, staging и production окружений, Docker images, Compose artifacts, PostgreSQL migrations, runtime configuration, secrets, backup/restore, release, rollback и forward-fix процедур.

Изменение topology, ports, volumes, runtime dependencies, migration order, compatibility window, readiness semantics, secret delivery, backup policy, rollout strategy или release procedure должно обновлять этот документ в том же change set.

Документ не определяет бизнес-инварианты и HTTP-контракты. Они остаются в SRS, API Design, Database Design, Domain Model, Security Design и Sequence Diagrams.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует ADR или зарегистрированного operational risk.
- **Может** — допустимый вариант.

Operational exception должен иметь владельца, обоснование, оценку риска, компенсирующие меры и дату пересмотра. Бессрочные исключения запрещены.

## 3. Deployment invariants

Ни один deployment не должен нарушать следующие инварианты:

1. staging и production используют тот же immutable image digest;
2. production secrets отсутствуют в Git, image layers, frontend bundle и logs;
3. API не применяет production migrations автоматически при startup;
4. PostgreSQL не публикуется в public network;
5. application version запускается только с совместимой schema version;
6. worker version запускается только с совместимым job/outbox protocol;
7. application rollback не предполагает автоматический destructive schema rollback;
8. business traffic не направляется на instance до успешной readiness;
9. deployment не считается успешным без post-deploy verification;
10. backup не считается пригодным без успешного restore drill;
11. timeout, cancellation или restart не должны оставлять частично проведённую business operation;
12. singleton operation не может одновременно исполняться несколькими владельцами без fencing;
13. production correction не выполняется прямым SQL как штатный workflow;
14. любой production artifact связан с commit, build metadata, image digest и release record.

## 4. Границы приложений и deploy artifacts

Репозиторий содержит два независимых application roots:

```text
PharmacyCRM/
├── backend/
├── frontend/
├── deploy/
├── docker-compose.yml
└── Makefile
```

Backend и frontend:

- собираются отдельными pipelines;
- имеют отдельные Dockerfile;
- не используют общий dependency workspace;
- имеют раздельные runtime/build-time configuration boundaries;
- интегрируются только через HTTP API;
- не разделяют runtime secrets.

Целевая структура deployment artifacts:

```text
deploy/
├── compose/
│   ├── compose.local.yml
│   ├── compose.ci.yml
│   ├── compose.staging.yml
│   └── compose.production.yml
├── env/
│   ├── local.example.env
│   ├── ci.example.env
│   ├── staging.example.env
│   └── production.example.env
├── scripts/
│   ├── preflight.sh
│   ├── deploy.sh
│   ├── migrate.sh
│   ├── backup.sh
│   ├── restore-verify.sh
│   ├── reconcile.sh
│   └── smoke-test.sh
└── runbooks/
    ├── release.md
    ├── rollback.md
    ├── forward-fix.md
    ├── database-recovery.md
    ├── worker-recovery.md
    ├── initial-data-cutover.md
    └── secret-rotation.md
```

Example env-файлы содержат только имена параметров и безопасные placeholders. Реальные secrets запрещено коммитить.

## 5. Окружения

| Окружение | Назначение | Данные | Доступность |
|---|---|---|---|
| local | разработка | synthetic/local | localhost |
| CI | автоматические тесты | ephemeral | отсутствует |
| staging | pre-production verification | synthetic или обезличенные | ограниченная |
| production | рабочая система | production | frontend/API через ingress |

Правила:

1. staging архитектурно близок production;
2. environment-specific поведение задаётся configuration, а не ветками кода;
3. production data не копируются в local/CI;
4. production backup в staging допускается только после обезличивания и отдельного approval;
5. development defaults не активируются автоматически в production;
6. startup валидирует environment mode и критические параметры;
7. CI использует disposable database и не зависит от developer state;
8. staging проверяет тот же migration и rollout flow, что production.

## 6. Целевая runtime topology

```text
Internet
   |
Reverse Proxy / TLS Ingress
   |----------------------|
   |                      |
Frontend static service   Backend API replicas
                           |
                           |---- Worker replicas
                           |---- Migration job (one-shot)
                           |---- Reconciliation job (controlled)
                           |
                        PostgreSQL
                           |
                    Backup/WAL storage
                           |
                  Restore verification env
```

Компоненты:

- ingress завершает TLS, ограничивает request size/rate и нормализует proxy headers;
- frontend отдаёт immutable static assets;
- API обслуживает HTTP traffic;
- worker обрабатывает outbox, imports, alerts и projections;
- migration executable запускается отдельно;
- reconciliation job запускается по schedule/runbook и не изменяет business truth без отдельной команды;
- PostgreSQL доступен только внутренним компонентам и контролируемому administrative channel;
- backup storage отделён от primary runtime credentials и host.

API, worker и migrate могут использовать один backend image с разными commands, но имеют отдельные service identities и permissions.

## 7. Local Docker Compose

Local Compose поднимает минимум:

- PostgreSQL;
- backend API;
- backend worker;
- frontend;
- migration command;
- при необходимости локальные telemetry components.

### 7.1 Порты

| Component | Container port | Host port |
|---|---:|---:|
| PostgreSQL | 5432 | 5433 |
| Backend API | configurable | 8080 default |
| Frontend dev | configurable | 5173 default |
| Frontend preview | configurable | 8081 default |

PostgreSQL local binding:

```yaml
ports:
  - "127.0.0.1:5433:5432"
```

Port `5433` используется только для local development. Staging/production PostgreSQL не публикуется на public host interface.

### 7.2 Volumes

Минимальные named volumes:

```text
pharmacycrm_postgres_data
pharmacycrm_backend_logs
pharmacycrm_import_quarantine
```

Правила:

- PostgreSQL data не хранится в writable container layer;
- import quarantine отделён от frontend public files;
- удаление volumes выполняется отдельной destructive command с подтверждением;
- `docker compose down` без `-v` не удаляет data;
- local cleanup command выводит точный список удаляемых volumes;
- log volume имеет rotation/size policy.

### 7.3 Startup dependencies

`depends_on` не является readiness guarantee. API и worker выполняют реальные dependency checks и schema compatibility validation.

Compose healthchecks не заменяют application `/healthz` и `/readyz`.

## 8. Docker images и supply chain

### 8.1 Backend image

Backend Dockerfile обязан:

- использовать multi-stage build;
- собирать `api`, `worker`, `migrate` или эквивалент;
- фиксировать Go toolchain/base image version;
- выполнять dependency verification;
- не включать source, compiler и build credentials в final image;
- запускаться non-root user;
- использовать минимальный runtime image;
- поддерживать `SIGTERM` и graceful shutdown;
- писать только в allowlisted mounted paths;
- не содержать `.env`, secrets и private keys.

Build metadata:

- commit SHA;
- version/tag;
- build timestamp;
- image digest;
- dirty state для local build;
- schema compatibility range;
- worker protocol version.

### 8.2 Frontend image

Frontend build обязан:

- устанавливать dependencies строго по lockfile;
- использовать production build stage;
- отдавать hashed immutable assets;
- задавать cache headers;
- не содержать backend secrets;
- не считать runtime/build variables секретными;
- не публиковать source maps без отдельного решения;
- иметь fallback routing только для frontend routes, не для API paths.

### 8.3 Provenance

Production использует image по digest или подписанному immutable tag.

Запрещено:

- `latest`;
- rebuild после approval;
- разные artifacts для staging и production;
- mutable tag без проверки digest;
- registry credentials в image layer;
- release artifact без SBOM и vulnerability scan evidence.

## 9. Runtime configuration

Backend использует `github.com/kelseyhightower/envconfig`.

Configuration groups:

- application/environment;
- HTTP server;
- PostgreSQL;
- authentication/session;
- security headers/trusted proxies;
- logging;
- metrics/tracing;
- worker/job protocol;
- import limits/storage;
- backup/operations.

### 9.1 Startup validation

Startup завершается ошибкой при:

- отсутствии обязательного secret;
- placeholder/default production credential;
- invalid DSN;
- unsafe cookie/TLS mode;
- invalid JWT issuer/audience/algorithm;
- invalid TTL;
- wildcard CORS с credentials;
- пустом trusted proxy allowlist при доверии forwarded headers;
- несовместимой schema version;
- несовместимом worker/outbox protocol;
- invalid log path;
- invalid timeouts, limits или pool settings;
- connection budget, превышающем утверждённый DB capacity;
- включённом debug/pprof без network restriction.

Startup errors не раскрывают secret values.

### 9.2 Configuration ownership

Каждый parameter имеет:

- имя;
- тип;
- safe default либо обязательность;
- environment scope;
- validation rule;
- owner;
- restart/reload semantics;
- security classification.

Production configuration matrix хранится без secret values и versioned вместе с release process.

## 10. Secrets и key rotation

Secrets включают DB credentials, JWT signing keys, peppers, encryption keys, provider credentials, backup encryption keys и deployment credentials.

Правила:

1. secrets отсутствуют в Git, images, frontend bundle и logs;
2. delivery выполняется через secret manager, protected environment или mounted secret files;
3. `.env` разрешён только local и gitignored;
4. каждый production secret имеет owner, rotation period и emergency revoke procedure;
5. rotation не требует source rebuild;
6. service получает только необходимые secrets;
7. backup encryption keys не хранятся только рядом с backup;
8. secret compromise запускает incident runbook;
9. retired secret удаляется только после подтверждения отсутствия consumers;
10. secret access auditируется платформой, где это возможно.

JWT rotation поддерживает overlap старого verification key и нового signing key не менее максимального TTL уже выданных tokens плюс допустимый clock skew.

Key identifier `kid` уникален. Удаление старого verification key допускается только после окончания overlap и проверки telemetry.

## 11. PostgreSQL deployment

### 11.1 Network и TLS

Production PostgreSQL:

- не публикуется в internet;
- принимает соединения только от allowlisted service identities;
- использует TLS при пересечении host/network boundary;
- не использует superuser credentials в runtime;
- ограничивает administrative access через VPN/bastion/private channel;
- имеет connection logging и alerting на unusual sources.

### 11.2 Роли

Минимальные roles:

- owner/bootstrap;
- migration;
- runtime API;
- runtime worker;
- backup;
- optional read-only diagnostics.

API и worker roles могут различаться. Runtime roles не имеют права создавать schema, управлять roles, выполнять unrestricted DDL или изменять immutable history вне разрешённого protocol.

### 11.3 Pool budget

Для каждой replica задаются max/min connections, acquire timeout, lifetime, idle timeout и health interval.

Обязательный connection budget:

```text
API replicas × API max pool
+ worker replicas × worker max pool
+ migration reserve
+ backup reserve
+ operational reserve
<= PostgreSQL max_connections - safety margin
```

Изменение replica count или pool size требует пересчёта budget.

### 11.4 Database timeouts

Production задаёт:

- connection timeout;
- statement timeout;
- lock timeout для migrations/operations;
- idle-in-transaction timeout;
- transaction timeout policy;
- application cancellation propagation.

Timeout не должен приводить к частично committed business operation.

## 12. Application–schema–worker compatibility

Каждый backend release объявляет:

- минимальную schema version;
- максимальную подтверждённую schema version либо compatibility policy;
- worker protocol version;
- outbox event versions, которые умеет читать;
- API contract version.

Readiness должна быть false, если schema несовместима.

Во время rolling deployment новая и старая application versions могут одновременно работать только если:

- schema обратно совместима для обеих;
- outbox/job payload читается обеими worker versions либо workers обновляются контролируемо;
- feature не создаёт payload, который старый consumer не понимает;
- session/token validation совместима;
- frontend API contract совместим.

Несовместимый worker rollout выполняется через drain старых workers, protocol gate или blue-green switch. Одновременная обработка несовместимыми consumers запрещена.

## 13. Database migrations

### 13.1 Общая модель

Migrations выполняются отдельной one-shot command до переключения traffic на несовместимую application version.

Текущая E2 application compatibility закреплена на schema version `21`. CI проверяет migration from zero и изолированный upgrade от E1 version `1`; API/worker не применяют migrations при startup. API, worker и migrator используют разные `POSTGRES_API_RUNTIME_DSN`, `POSTGRES_WORKER_RUNTIME_DSN` и `POSTGRES_MIGRATION_DSN`.

Migration process:

1. проверяет environment и release identity;
2. получает migration advisory/distributed lock;
3. фиксирует current schema version;
4. выполняет preflight checks;
5. проверяет backup/restore point requirement;
6. применяет migrations по порядку;
7. выполняет verification queries, проверяющие critical tables, constraints, indexes, triggers/functions и least-privilege grants, а не только наличие relation;
8. фиксирует resulting schema version и evidence;
9. освобождает lock.

Одновременно migration запускает только один владелец.

### 13.2 Expand → migrate → contract

1. **Expand** — добавить совместимую schema.
2. **Migrate** — развернуть compatible code и выполнить bounded backfill.
3. **Verify** — проверить completeness и consumers.
4. **Contract** — удалить legacy schema отдельным release.

Запрещено:

- удалять используемый column до compatibility window;
- unsafe rename без dual-read/write strategy;
- длительный table rewrite без lock assessment;
- добавлять `NOT NULL` к большой таблице одним опасным шагом;
- unbounded backfill в одной transaction;
- mixing schema migration и business data correction без отдельного plan;
- считать application rollback эквивалентом schema rollback.

### 13.3 Migration review card

Каждая migration документирует:

- affected tables;
- expected lock level/duration;
- rewrite risk;
- disk/WAL growth;
- compatibility window;
- backfill batch/rate;
- verification query;
- rollback или forward-fix;
- backup requirement;
- monitoring/abort thresholds;
- tested dataset size.

### 13.4 Failed migration

При failure traffic не переключается на несовместимую version. Schema state проверяется до retry.

Partially applied non-transactional migration требует dedicated runbook. Повтор разрешён только после доказанной idempotency steps.

## 14. Singleton operations и fencing

Migrations, destructive maintenance, initial data cutover и некоторые reconciliation jobs являются singleton operations.

Они обязаны использовать:

- advisory/distributed lock;
- уникальный execution ID;
- lease timeout;
- fencing token или эквивалентную защиту от stale owner;
- heartbeat;
- persisted execution status;
- bounded retry.

Lock без fencing недостаточен, если старый process после pause способен продолжить работу после появления нового owner.

## 15. Release pipeline

Production pipeline:

1. source verification;
2. backend tests/build;
3. frontend tests/build;
4. migration verification;
5. integration/concurrency/security tests;
6. SBOM, vulnerability и secret scan;
7. image signing/publishing;
8. staging deployment теми же digests;
9. staging migration/restore/smoke checks;
10. approval;
11. production preflight;
12. migration job;
13. controlled application/worker rollout;
14. frontend rollout;
15. post-deploy verification;
16. observation window;
17. release evidence сохранение.

Deployment tooling не должно пересобирать artifact.

## 16. Deployment procedure

### 16.1 Preflight

Проверяются:

- immutable commit/tag и image digests;
- CI/security scan status;
- compatibility matrix;
- migration plan;
- backup freshness и restore evidence;
- rollback/forward-fix owner;
- secrets/config validation;
- DB connection/capacity budget;
- disk/WAL capacity;
- alert routing;
- отсутствие blocking incident;
- maintenance window, если требуется.

### 16.2 Apply schema

- запустить migration job;
- проверить execution ID и lock ownership;
- дождаться success;
- проверить schema version и verification queries;
- не продолжать при ambiguous result.

### 16.3 Rollout order

Нормативный порядок определяется compatibility plan. Базовый вариант:

1. deploy compatible API replicas;
2. дождаться readiness и version distribution;
3. drain/upgrade workers согласно protocol plan;
4. deploy frontend;
5. переключить traffic только на ready instances;
6. graceful terminate старые instances.

Для несовместимого worker protocol применяется отдельный switch plan, а не обычный rolling update.

### 16.4 Post-deploy verification

Минимально:

- `/healthz` и `/readyz`;
- build metadata/image digest;
- schema/worker protocol version;
- login/refresh/logout smoke;
- authorized pharmacy-scoped read;
- safe idempotency replay;
- DB pool saturation;
- worker heartbeat, leases и outbox lag;
- error rate и latency;
- audit/security event creation;
- frontend/API compatibility;
- отсутствие mixed incompatible versions;
- inventory invariants/reconciliation sample.

### 16.5 Observation window

После rollout release остаётся под усиленным наблюдением до закрытия defined window.

Отслеживаются 5xx, latency, DB locks, pool saturation, worker retries, outbox lag, audit failures, authorization denials anomalies и inventory inconsistencies.

## 17. Health, readiness и startup

### `/healthz`

Показывает, что process жив и способен ответить. Не выполняет тяжёлые dependency checks и не раскрывает config.

### `/readyz`

Success только если:

- startup validation завершена;
- process не draining;
- PostgreSQL доступен;
- schema compatible;
- required dependencies готовы;
- worker protocol compatible;
- critical internal initialization завершена.

Dependency outage переводит readiness в false, но не обязан переводить liveness в false.

Operational endpoint body не раскрывает DSN, host topology, secrets или internal stack traces.

## 18. Graceful shutdown и drain

API при `SIGTERM`:

1. выставляет readiness=false;
2. прекращает новый traffic;
3. drain-ит active requests;
4. позволяет transactions завершиться в bounded timeout;
5. отменяет remaining operations;
6. закрывает HTTP server и DB pool;
7. flush-ит telemetry/logs;
8. завершает process.

Worker:

- прекращает claim новых jobs;
- завершает current job либо safely releases lease;
- не marks processed до подтверждённого side effect;
- сохраняет retryability после crash;
- не продолжает работу после потери fencing ownership.

Shutdown timeout меньше orchestration termination grace period.

## 19. Scaling и workers

API масштабируется горизонтально при server-side sessions и отсутствии in-memory business truth.

Worker replicas разрешены только при:

- lease/claim protocol;
- `FOR UPDATE SKIP LOCKED` или утверждённом эквиваленте;
- processing timeout;
- idempotent consumer;
- bounded retry;
- dead-letter state;
- observable backlog;
- protocol compatibility.

Autoscaling worker по backlog не должен создавать DB connection storm. Scaling limits связаны с DB connection budget.

## 20. Logs и persistent storage

Backend пишет Zap structured logs в terminal и file согласно требованиям проекта.

Local file logs используют named volume/host path. В staging/production предпочтительна централизованная log platform.

Если file logging остаётся:

- persistent mount;
- max size/age/count;
- disk monitoring;
- safe behavior при full disk;
- no secrets/full bodies;
- log rotation не теряет ownership/permissions.

File log failure не должен отключать transactional audit. Application logs не заменяют audit events.

## 21. Import quarantine storage

Импортируемые файлы:

- хранятся вне frontend public root;
- получают server-generated ID;
- не исполняются;
- имеют restrictive permissions;
- ограничены size/retention;
- имеют checksum и audit metadata;
- удаляются controlled cleanup job;
- не переиспользуют original filename как path.

Worker читает input streaming как data. При object storage credentials имеют scoped permissions и rotation policy.

## 22. Backup policy

Backup покрывает:

- PostgreSQL data;
- schema/migration metadata;
- required import objects;
- release/image digest metadata;
- configuration metadata без secret values.

Утверждённый baseline: RPO ≤ 15 минут, RTO ≤ 4 часа, daily base backup, continuous WAL archive, off-site encrypted copies и restore drill минимум ежеквартально. Конкретный backup product обязан доказать этот baseline.

Backup обязан:

- создаваться автоматически;
- шифроваться;
- иметь checksum;
- храниться вне primary host;
- мониторить freshness/failure;
- быть защищён от удаления runtime credentials;
- поддерживать point-in-time recovery, если это требуется RPO;
- иметь immutable/retention-locked copy для защиты от ransomware или ошибочного удаления.

Простое копирование активного PostgreSQL data directory запрещено.

## 23. Restore и disaster recovery

Restore считается доказанным только после восстановления в изолированное окружение.

Restore drill:

1. выбирает backup/PITR target;
2. восстанавливает DB и required objects;
3. применяет required recovery steps;
4. проверяет schema version;
5. запускает application compatibility check;
6. выполняет integrity queries;
7. выполняет inventory reconciliation;
8. проверяет audit readability;
9. фиксирует actual RPO/RTO;
10. удаляет isolated environment безопасно.

Недостаточно проверить только успешный exit code restore command.

### 23.1 Recovery verification

Минимально проверяются:

- row counts и ключевые constraints;
- отсутствие отрицательных остатков;
- согласованность lot balances и immutable movements;
- idempotency/audit tables;
- session invalidation decision после disaster;
- outbox backlog/duplicate safety;
- application startup/readiness;
- выборочный end-to-end read flow.

### 23.2 Disaster modes

Runbooks должны покрывать:

- потерю API/worker host;
- PostgreSQL corruption/unavailability;
- accidental data deletion;
- compromised credentials;
- backup storage outage;
- failed migration;
- outbox backlog;
- quarantine storage loss;
- region/site outage, если topology это предполагает.

## 24. Initial data cutover

Первичное открытие pharmacy для работы требует отдельного controlled process:

1. freeze source data;
2. import catalog/assortment/initial lots через documented commands;
3. validate rejected rows;
4. reconcile quantities и monetary snapshots;
5. obtain responsible sign-off;
6. create baseline audit/cutover record;
7. enable business commands;
8. monitor first operational window.

Запрещено включать продажи до завершения reconciliation и sign-off.

Повтор initial import с тем же dataset должен быть idempotent либо выполняться в новом isolated pharmacy environment после clean reset.

## 25. Rollback и forward-fix

### 25.1 Application rollback

Разрешён только если previous image совместим с active schema и worker protocol.

Rollback использует previously verified digest. Rebuild старой version запрещён.

### 25.2 Schema rollback

Destructive down migration по умолчанию запрещён. Предпочтителен forward-fix.

Down допускается только при доказанной безопасности, отсутствии потери новых данных, оценённом lock impact, свежем backup и протестированном runbook.

### 25.3 Rollback triggers

Rollout немедленно останавливается при:

- migration ambiguity;
- authorization bypass;
- duplicate irreversible effect;
- отрицательном stock;
- audit failure;
- incompatible worker behavior;
- резком росте error/latency;
- frontend/backend incompatibility;
- secret leakage;
- unexplained inventory divergence.

Если rollback небезопасен, dangerous commands отключаются operationally, traffic ограничивается и запускается forward-fix/incident procedure.

## 26. Deployment security hardening

Production containers используют:

- non-root;
- read-only root filesystem;
- dropped capabilities;
- `no-new-privileges`;
- explicit writable mounts;
- CPU/memory/pid limits;
- seccomp/AppArmor defaults;
- pinned digests;
- network restrictions;
- separate identities;
- vulnerability scanning;
- SBOM;
- signed artifacts.

Ingress обязан:

- завершать TLS;
- redirect HTTP→HTTPS;
- задавать security headers;
- ограничивать request/body size;
- применять timeouts/rate limits;
- overwrite untrusted forwarded headers;
- доверять client IP только trusted proxy chain;
- ограничивать operational endpoints;
- не проксировать arbitrary paths к internal services.

## 27. Time synchronization

Hosts и PostgreSQL используют синхронизированное время. Clock drift мониторится.

UTC используется для persisted datetime. Local timezone применяется только в presentation/business-date rules, определённых доменом.

## 28. Capacity и resource limits

До pilot определяются:

- API/worker replica limits;
- DB max connections и pool budget;
- request/upload limits;
- transaction/statement/lock timeouts;
- worker batch/lease/retry limits;
- outbox backlog thresholds;
- disk free-space thresholds;
- log retention;
- backup duration/storage;
- WAL growth thresholds;
- import memory limits.

Capacity plan должен учитывать degraded mode и restore/backfill operations, а не только normal traffic.

## 29. Deployment observability

Каждый deployment фиксирует:

- release started/completed/failed;
- commit/tag/image digest;
- schema version before/after;
- worker protocol version;
- migration execution ID;
- version distribution;
- readiness failures;
- restart count;
- rollout duration;
- rollback/forward-fix status;
- smoke result;
- observation window result.

Оператор должен определить:

- какая version обслуживает traffic;
- какие worker versions active;
- какая schema version active;
- какие migrations применены;
- есть ли incompatible mixed versions;
- прошёл ли smoke/reconciliation;
- кто и когда выполнил release.

## 30. Operational access

Production shell/DB access:

- персональный;
- least privilege;
- MFA, где доступно;
- журналируется;
- ограничен по времени;
- связан с change/incident record;
- не использует shared root credentials.

Routine business correction через SQL запрещена.

Emergency SQL требует peer review, restore point, scoped script, dry-run/verification, execution log и post-incident documentation.

## 31. Release checklist

Release допускается, если:

1. CI/security scans green;
2. artifact immutable и signed;
3. digests зафиксированы;
4. compatibility matrix подтверждена;
5. migration preflight выполнен;
6. backup freshness и restore evidence допустимы;
7. rollback/forward-fix owner назначен;
8. secrets/config validation пройдена;
9. DB capacity/disk/WAL достаточны;
10. monitoring/alert routing работают;
11. staging rollout теми же digests успешен;
12. post-deploy checks и observation window определены;
13. release notes содержат API/schema/operational changes;
14. active P0/P1 operational blockers отсутствуют.

## 32. Definition of Done для deployment change

Deployment change завершён только если:

1. topology/config impact описан;
2. local/CI/staging/production semantics согласованы;
3. secrets не попали в repository/image/logs;
4. images immutable и reproducible;
5. application-schema-worker compatibility определена;
6. migration order, lock impact и verification описаны;
7. startup/readiness/shutdown протестированы;
8. singleton operations имеют lock и fencing;
9. volumes/retention определены;
10. backup/restore и reconciliation impact проверены;
11. rollback/forward-fix описан;
12. security hardening сохранён;
13. observability/alerting обновлены;
14. runbooks обновлены;
15. staging verification пройдена;
16. нет P0/P1 operational blocker.

## 33. Обязательные deployment tests

Минимально автоматизируются или регулярно rehearsed:

- clean local startup;
- production-like startup validation failure cases;
- schema compatibility rejection;
- migration lock contention;
- failed migration recovery;
- rolling API deployment;
- worker drain и lease recovery;
- stale worker fencing;
- graceful shutdown during transaction/job;
- secret rotation overlap;
- backup freshness alert;
- isolated restore drill;
- inventory reconciliation after restore;
- rollback to previous digest;
- network denial к PostgreSQL с public interface;
- disk pressure/log rotation behavior;
- post-deploy smoke failure stops release.

## 34. Remaining deployment implementation decisions

Gate E0 topology class, trusted-proxy model, release/migration protocol, outbox fencing, RPO/RTO и retention baseline утверждены. До production требуется выбрать конкретную реализацию:

1. hosting/orchestration platform и ingress product;
2. secret manager;
3. registry, signing и provenance tooling;
4. observability platform;
5. rollout strategy и maintenance-window policy;
6. scaling/connection budget;
7. import object storage;
8. release, DBA и incident ownership;
9. staging anonymization;
10. certificate rotation и network segmentation;
11. compatibility declaration format;
12. initial data cutover owner/sign-off form.

Выбранный продукт не может ослабить утверждённые protocol, recovery и trust guarantees.
