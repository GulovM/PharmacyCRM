# PharmacyCRM — Deployment

**Статус документа:** Draft  
**Версия:** 0.1  
**Дата:** 2026-07-20  
**Связанные документы:** `03-system-context.md`, `04-architecture.md`, `04-01-backend-architecture.md`, `06-database-design.md`, `08-project-structure.md`, `09-security-design.md`, `10-sequence-diagrams.md`, `11-development-roadmap.md`

## 1. Назначение и нормативная роль

Документ определяет целевую модель сборки, конфигурации, развёртывания, миграций, эксплуатации, резервного копирования и восстановления PharmacyCRM.

Deployment Design является нормативным для:

- локального окружения разработки;
- CI и integration environments;
- staging;
- production;
- Docker images и Compose artifacts;
- PostgreSQL migrations;
- secrets и runtime configuration;
- backup/restore;
- release, rollback и forward-fix процедур.

Изменение topology, runtime dependencies, ports, volumes, migration order, readiness semantics, secret delivery, backup policy или release procedure должно обновлять этот документ в том же change set.

Документ не определяет бизнес-инварианты и HTTP-контракты. Они остаются в соответствующих SRS, API, Database, Domain, Security и Sequence документах.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует ADR или зафиксированного operational risk.
- **Может** — допустимый вариант.

## 3. Цели deployment-модели

Deployment PharmacyCRM должен обеспечивать:

1. воспроизводимую сборку backend и frontend из immutable commit;
2. запуск одного и того же проверенного artifact в staging и production;
3. отсутствие production secrets в repository и container image;
4. контролируемое применение migrations;
5. сохранность PostgreSQL data и application logs;
6. безопасный startup и graceful shutdown;
7. корректные health/readiness сигналы;
8. возможность восстановить production data в пределах утверждённых RPO/RTO;
9. наблюдаемость версии, конфигурации и состояния deployment;
10. безопасный rollback application version без опасного отката схемы;
11. отсутствие публичного доступа к PostgreSQL и operational endpoints;
12. документированный release и incident workflow.

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
- имеют отдельные runtime/build-time configuration boundaries;
- не используют общий dependency workspace;
- интегрируются только через HTTP API;
- не разделяют runtime secrets.

Каталог `deploy/` содержит deployment-specific artifacts:

```text
deploy/
├── compose/
│   ├── compose.local.yml
│   ├── compose.staging.yml
│   └── compose.production.yml
├── env/
│   ├── local.example.env
│   ├── staging.example.env
│   └── production.example.env
├── scripts/
│   ├── deploy.sh
│   ├── migrate.sh
│   ├── backup.sh
│   ├── restore-verify.sh
│   └── smoke-test.sh
└── runbooks/
    ├── release.md
    ├── rollback.md
    ├── database-recovery.md
    └── secret-rotation.md
```

Example env-файлы содержат только имена параметров и безопасные примеры. Реальные secrets запрещено коммитить.

## 5. Окружения

Минимально различаются:

| Окружение | Назначение | Данные | Внешняя доступность |
|---|---|---|---|
| local | разработка | synthetic/local | localhost |
| CI | автоматические тесты | ephemeral | отсутствует |
| staging | pre-production verification | synthetic или обезличенные | ограниченная |
| production | рабочая система | production | публичный frontend/API через ingress |

Правила:

1. staging должен быть архитектурно близок production;
2. production data не копируются в local/CI;
3. использование production backup в staging допустимо только после обезличивания и отдельного разрешения;
4. environment-specific поведение задаётся конфигурацией, а не ветками кода;
5. небезопасные development defaults не должны автоматически переноситься в production;
6. application startup обязан валидировать environment mode и критические параметры.

## 6. Целевая runtime topology

Минимальная production topology:

```text
Internet
   |
Reverse Proxy / TLS Ingress
   |----------------------|
   |                      |
Frontend static service   Backend API
                          |-- Worker
                          |-- Migration job (one-shot)
                          |
                       PostgreSQL
                          |
                    Backup storage
```

Компоненты:

- reverse proxy / ingress завершает TLS и применяет базовые network controls;
- frontend отдаёт статические assets;
- backend API обслуживает HTTP requests;
- worker выполняет outbox, alerts, projections и import jobs;
- migration executable запускается отдельно и не является частью обычного API startup;
- PostgreSQL доступен только внутренним компонентам и контролируемым административным каналам;
- backup storage отделён от runtime host.

API и worker могут использовать один backend image с разными commands.

## 7. Local Docker Compose

Локальное окружение должно поднимать как минимум:

- PostgreSQL;
- backend API;
- backend worker;
- frontend;
- одноразовую migration command либо явную migration target.

### 7.1 Порты

Нормативные local host mappings:

| Component | Container port | Host port |
|---|---:|---:|
| PostgreSQL | 5432 | 5433 |
| Backend API | configuration-defined | 8080 по умолчанию |
| Frontend | configuration-defined | 5173 development / 8081 built preview |

PostgreSQL host port `5433` предназначен только для local development. В staging и production PostgreSQL не публикуется в публичный host interface.

Предпочтительный local binding:

```yaml
ports:
  - "127.0.0.1:5433:5432"
```

### 7.2 Volumes

Local Compose использует named volumes:

```text
pharmacycrm_postgres_data
pharmacycrm_backend_logs
pharmacycrm_import_quarantine
```

Обязательные правила:

- PostgreSQL data не хранится внутри writable layer контейнера;
- application logs сохраняются через volume или централизованный log driver;
- import quarantine не смешивается с frontend public files;
- удаление volumes выполняется только отдельной destructive command с подтверждением;
- `docker compose down` без `-v` не удаляет данные.

### 7.3 Health dependencies

`depends_on` не является доказательством готовности PostgreSQL. Backend readiness/startup использует реальную connection check и migration compatibility check.

Compose healthchecks не заменяют application `/healthz` и `/readyz`.

## 8. Docker images

### 8.1 Backend image

Backend Dockerfile должен:

- использовать multi-stage build;
- собирать binaries `api`, `worker`, `migrate` или эквивалентный набор;
- фиксировать Go toolchain/image version;
- не включать source tree и build credentials в final image;
- запускаться non-root user;
- иметь минимальный runtime base image;
- содержать CA certificates и timezone data только при необходимости;
- поддерживать graceful termination по `SIGTERM`;
- не записывать в filesystem, кроме явно разрешённых mounted paths;
- не содержать `.env` и secrets.

Build metadata должны включать:

- commit SHA;
- build timestamp;
- version/tag;
- dirty state для local build, если применимо.

Backend должен предоставлять build metadata через безопасный operational endpoint или structured startup log.

### 8.2 Frontend image

Frontend Dockerfile должен:

- выполнять dependency install по lockfile;
- собирать production bundle в отдельном build stage;
- отдавать assets через минимальный static server;
- использовать immutable hashed asset names;
- задавать корректные cache headers;
- не встраивать backend secrets;
- не включать source maps публично без осознанного решения;
- не использовать runtime environment variables как секретный канал.

Frontend build-time variables считаются публичными, так как попадают в браузерный bundle.

### 8.3 Image provenance

Production deployment использует image по immutable digest или подписанному tag.

Запрещено:

- использовать `latest`;
- пересобирать image после approval;
- использовать разные artifacts для staging verification и production без нового release decision;
- хранить registry credentials в image layer.

## 9. Runtime configuration

Backend загружает конфигурацию через `github.com/kelseyhightower/envconfig` согласно архитектурным требованиям.

Конфигурация делится на группы:

- application identity и environment;
- HTTP server;
- PostgreSQL;
- authentication/session;
- security headers и trusted proxies;
- logging;
- tracing/metrics;
- worker settings;
- import limits;
- backup/operational integration settings.

### 9.1 Startup validation

Backend обязан завершить startup с ошибкой при:

- отсутствии обязательного secret;
- placeholder/default production credential;
- невалидном DSN;
- небезопасном cookie/TLS режиме для production;
- пустом JWT issuer/audience;
- неизвестном JWT algorithm;
- некорректных TTL;
- wildcard CORS вместе с credentials;
- пустом trusted proxy allowlist при включённом доверии proxy headers;
- неподдерживаемой migration/schema version;
- невалидных log paths;
- отрицательных или нулевых critical timeouts/limits.

Ошибки startup не должны выводить secret values.

### 9.2 Configuration ownership

Каждый parameter должен иметь:

- имя;
- тип;
- default только когда он безопасен;
- обязательность по environment;
- описание;
- владельца;
- validation rule.

Production configuration matrix хранится как документация без secret values.

## 10. Secrets

Secrets включают:

- PostgreSQL credentials;
- JWT signing keys;
- refresh/reset token peppers, если используются;
- encryption keys;
- external provider credentials;
- backup encryption credentials;
- registry/deployment credentials.

Правила:

1. secrets не хранятся в Git;
2. secrets не встраиваются в images;
3. secrets доставляются через secret manager, protected environment или mounted secret files;
4. `.env` допустим только для local development и должен быть gitignored;
5. production secrets имеют владельца и rotation procedure;
6. secret rotation не требует пересборки source artifact;
7. startup/logging запрещено выводить secret values;
8. доступ к secrets соответствует least privilege;
9. backup encryption keys не хранятся только рядом с backup;
10. компрометация secret запускает incident runbook и отзыв зависимых credentials.

JWT key rotation должна поддерживать overlap старого verification key и нового signing key в пределах максимального TTL выданных tokens.

## 11. PostgreSQL deployment

### 11.1 Network

Production PostgreSQL:

- не публикуется в интернет;
- принимает соединения только от API, worker, migration job, backup process и контролируемого admin channel;
- использует TLS, если соединение пересекает host/network boundary;
- не использует superuser credentials в runtime приложении.

### 11.2 Роли

Минимально создаются:

- owner/bootstrap role;
- migration role;
- runtime role;
- backup role;
- optional read-only diagnostics role.

Runtime role не должен иметь права:

- создавать или удалять schema;
- управлять roles;
- выполнять unrestricted DDL;
- изменять immutable audit/movement history вне разрешённых commands;
- bypass-ить row/security constraints через superuser privileges.

### 11.3 Pool и timeouts

Backend pool configuration задаёт:

- максимальное число соединений;
- минимальное число idle connections при необходимости;
- connection lifetime и idle timeout;
- acquisition timeout;
- statement/transaction timeout policy;
- health check interval.

Сумма pool limits всех API/worker replicas должна оставлять запас для migrations, backup и emergency access.

## 12. Database migrations

### 12.1 Общая модель

Migrations выполняются отдельной one-shot command до переключения traffic на несовместимую application version.

API startup не должен незаметно применять production migrations.

Migration process:

1. проверяет target environment;
2. получает migration lock;
3. фиксирует текущую schema version;
4. выполняет preflight checks;
5. применяет migrations по порядку;
6. проверяет итоговую version;
7. записывает deployment evidence;
8. освобождает lock.

Одновременно migrations запускает только один процесс.

### 12.2 Compatibility strategy

Предпочтительна expand/migrate/contract стратегия:

1. **Expand** — добавить обратно совместимую schema;
2. **Migrate** — развернуть код, выполнить backfill/dual-read при необходимости;
3. **Contract** — удалить старые поля только после подтверждения отсутствия старых consumers.

Запрещено в одном опасном шаге:

- удалять используемый столбец до deployment совместимого кода;
- переименовывать столбец без compatibility window;
- делать длительный table rewrite без оценки lock impact;
- добавлять `NOT NULL` к большой таблице без безопасного плана;
- выполнять неограниченный backfill в одной транзакции;
- предполагать, что rollback application автоматически откатит schema.

### 12.3 Migration review

Каждая production migration документирует:

- ожидаемый lock level;
- ожидаемую длительность;
- table rewrite risk;
- compatibility window;
- backfill strategy;
- verification query;
- rollback или forward-fix plan;
- backup requirement;
- monitoring во время выполнения.

### 12.4 Failed migration

При failure:

- traffic не переключается на несовместимую version;
- migration lock освобождается только после безопасного завершения process;
- состояние schema проверяется вручную/автоматизированно;
- частично применённая non-transactional migration требует runbook;
- повтор разрешён только после проверки idempotency migration steps;
- destructive manual SQL без change/incident record запрещён.

## 13. Release pipeline

Production pipeline разделяется на stages:

1. source verification;
2. backend tests/build;
3. frontend tests/build;
4. migration verification;
5. integration/concurrency/security tests;
6. image scanning и SBOM;
7. image signing/publishing;
8. staging deployment;
9. staging smoke/contract checks;
10. approval;
11. production preflight;
12. migration job;
13. application rollout;
14. post-deploy verification;
15. release evidence сохранение.

Production deployment использует тот же image digest, который прошёл staging verification.

## 14. Deployment procedure

Нормативная последовательность:

### 14.1 Preflight

- release commit/tag immutable;
- CI green;
- image digests известны;
- migration plan проверен;
- backup freshness соответствует policy;
- rollback/forward-fix owner доступен;
- secrets/config validated;
- capacity достаточна;
- active incident отсутствует либо deployment одобрен incident commander;
- maintenance window объявлен, если требуется.

### 14.2 Apply schema

- запустить migration job;
- дождаться успешного завершения;
- проверить schema version и verification queries;
- не продолжать rollout при неоднозначном результате.

### 14.3 Rollout

- развернуть backend API;
- дождаться readiness;
- развернуть worker с совместимой version;
- развернуть frontend;
- переключать traffic только на ready instances;
- старые instances завершать graceful shutdown.

При нескольких replicas rollout должен предотвращать одновременную работу несовместимых worker versions над одним протоколом.

### 14.4 Post-deploy verification

Минимально проверяются:

- `/healthz` и `/readyz`;
- version/build metadata;
- login/refresh/logout smoke flow;
- authorized pharmacy-scoped read;
- один безопасный idempotency smoke scenario;
- DB connectivity и pool saturation;
- worker heartbeat/outbox lag;
- error rate и latency;
- migration version;
- audit/security event creation;
- frontend asset/API compatibility.

Успешный HTTP startup без post-deploy verification не считается успешным deployment.

## 15. Health, readiness и startup

### 15.1 `/healthz`

Показывает, что process запущен и event loop/server способен ответить.

Не должен:

- выполнять тяжёлые DB queries;
- раскрывать configuration/secrets;
- считаться доказательством готовности принимать business traffic.

### 15.2 `/readyz`

Readiness возвращает success только если:

- startup validation завершена;
- PostgreSQL доступен;
- schema version совместима;
- обязательные dependencies готовы;
- process не находится в shutdown/drain state.

Worker readiness дополнительно учитывает возможность получить lease и совместимость worker protocol.

### 15.3 Liveness failure

Liveness не должен перезапускать process из-за кратковременной недоступности PostgreSQL. Dependency outage отражается readiness, metrics и alerts.

## 16. Graceful shutdown

При `SIGTERM` API должен:

1. перестать принимать новый traffic через readiness=false;
2. начать drain active requests;
3. позволить текущим транзакциям завершиться в ограниченный timeout;
4. отменить оставшиеся operations через context cancellation;
5. закрыть HTTP server;
6. закрыть DB pool;
7. flush-ить logger/telemetry в пределах timeout;
8. завершиться с корректным exit code.

Worker должен:

- перестать claim-ить новые jobs;
- завершить или безопасно освободить current lease;
- не помечать event processed до успешного side effect;
- позволять повторную обработку после crash.

Shutdown timeout должен быть меньше orchestration termination grace period.

## 17. Scaling и singleton operations

API может масштабироваться горизонтально при stateless access-token handling и server-side session storage в PostgreSQL.

Worker допускает несколько replicas только если jobs используют:

- lease/claim protocol;
- `FOR UPDATE SKIP LOCKED` или утверждённый эквивалент;
- processing timeout;
- idempotent consumer;
- bounded retry и dead-letter handling.

Singleton по смыслу operations — migrations, некоторые reconciliation jobs и destructive maintenance — используют distributed/advisory lock.

In-memory mutex не является межрепликовочной блокировкой.

## 18. Логи и persistent storage

Backend использует Zap structured logging в terminal и file согласно требованиям проекта.

### 18.1 Local

Local file logs монтируются в named volume или host directory, явно предназначенный для logs.

### 18.2 Staging/production

Предпочтительно отправлять stdout/stderr в централизованную log platform. Если file logging остаётся обязательным:

- путь монтируется на persistent volume;
- применяется rotation;
- есть max size/age/count;
- disk usage мониторится;
- log volume failure не должен незаметно остановить audit DB writes;
- secrets и full bodies не логируются.

Application logs не являются заменой immutable audit events.

## 19. Import storage

Импортируемые файлы хранятся в quarantine storage:

- вне frontend public root;
- под server-generated identifier;
- без исполнения;
- с ограниченными permissions;
- с size/retention limits;
- с audit metadata;
- с очисткой по retention policy.

Worker читает файл как data stream. Original filename используется только как ограниченное metadata value.

При внешнем object storage credentials имеют минимальный scope и отдельную rotation policy.

## 20. Backup

### 20.1 Объекты backup

Backup покрывает:

- PostgreSQL data;
- schema/migration metadata;
- необходимые import objects согласно retention policy;
- deployment/configuration metadata без раскрытия secrets;
- список image digests и release version.

Application logs не считаются заменой DB backup.

### 20.2 Политика

До production утверждаются:

- RPO;
- RTO;
- frequency;
- retention;
- encryption;
- off-host/off-site copy;
- access roles;
- integrity verification;
- restore drill frequency.

Минимально backup должен:

- создаваться автоматически;
- шифроваться;
- иметь checksum/integrity verification;
- храниться отдельно от primary host;
- иметь мониторинг freshness/failure;
- быть защищён от удаления runtime application credentials.

### 20.3 Backup consistency

Для PostgreSQL используется механизм, обеспечивающий transactionally consistent snapshot или WAL-based recovery. Простое копирование активного data directory запрещено.

## 21. Restore и disaster recovery

Restore считается доказанным только после восстановления в изолированное окружение и выполнения verification.

Restore drill:

1. выбирает backup согласно policy;
2. разворачивает чистый PostgreSQL;
3. восстанавливает data;
4. проверяет migration/schema version;
5. запускает integrity/reconciliation queries;
6. проверяет возможность startup совместимой application version;
7. выполняет smoke flows;
8. измеряет фактический RPO/RTO;
9. фиксирует evidence и найденные проблемы;
10. уничтожает/защищает восстановленную копию согласно data policy.

Минимальные проверки:

- users/roles/assignments существуют;
- posted documents и movements согласованы;
- lot balances reconciled с movements;
- audit events читаются;
- idempotency records не создают повторный effect;
- latest migrations отмечены корректно;
- outbox state не приводит к неконтролируемому повтору.

Backup без успешного restore drill не считается надёжным.

## 22. Rollback и forward-fix

### 22.1 Application rollback

Application rollback допустим только если предыдущая version совместима с текущей schema и worker protocol.

Перед rollback проверяются:

- migration compatibility;
- новые enum/status values;
- outbox payload compatibility;
- frontend/backend API compatibility;
- session/token compatibility;
- background jobs, начатые новой version.

### 22.2 Schema rollback

Автоматический destructive down migration в production по умолчанию запрещён.

Предпочтителен forward-fix. Down migration допускается только если:

- доказана безопасность;
- отсутствует потеря новых данных;
- lock impact оценён;
- backup актуален;
- runbook протестирован.

### 22.3 Rollback trigger

Немедленное прекращение rollout требуется при:

- migration ambiguity/failure;
- authorization bypass;
- duplicate irreversible effect;
- отрицательном stock;
- невозможности transactional audit;
- резком росте 5xx/latency;
- worker, создающем повторные effects;
- несовместимости frontend/backend;
- утечке secret или credentials.

Если rollback небезопасен, traffic ограничивается, опасные commands отключаются operational control и выполняется forward-fix/incident procedure. Feature flag не должен обходить domain/security invariants.

## 23. Security hardening deployment

Production containers должны по возможности использовать:

- non-root user;
- read-only root filesystem;
- dropped Linux capabilities;
- `no-new-privileges`;
- explicit writable mounts;
- CPU/memory/pid limits;
- seccomp/AppArmor defaults;
- pinned image digests;
- restricted network access;
- separate service identities;
- vulnerability scanning;
- SBOM;
- signed artifacts.

Reverse proxy обязан:

- завершать TLS;
- перенаправлять HTTP на HTTPS;
- задавать approved security headers;
- ограничивать request/body size;
- применять timeouts;
- удалять/перезаписывать недоверенные forwarded headers;
- передавать client IP только через trusted proxy chain;
- не публиковать operational endpoints без network restriction.

## 24. Time synchronization

Server, PostgreSQL и infrastructure hosts должны использовать синхронизированное время.

Clock drift мониторится, поскольку влияет на:

- JWT/session expiry;
- audit ordering;
- idempotency retention;
- lot expiration;
- backup/WAL recovery;
- logs и incident investigation.

Application использует UTC для persisted datetime. Локальная timezone применяется только при presentation/business-date rules, явно определённых доменом.

## 25. Capacity и resource limits

До pilot задаются baseline limits:

- API replicas;
- worker replicas;
- DB max connections;
- pool sizes;
- request body size;
- upload size/rows;
- transaction timeout;
- worker batch size;
- outbox retry limits;
- log volume retention;
- disk free-space thresholds;
- backup duration/storage.

Resource limit не должен превращать временную нагрузку в частичную business operation. Timeout/cancellation обязаны приводить к rollback либо безопасному idempotent retry.

## 26. Deployment observability

Каждый deployment создаёт события/метрики:

- release started/completed/failed;
- commit/tag/image digest;
- migration started/completed/failed;
- schema version;
- API/worker version distribution;
- readiness failures;
- restart count;
- deployment duration;
- rollback/forward-fix status;
- post-deploy smoke result.

Должна быть возможность ответить:

- какая версия сейчас обслуживает traffic;
- какая schema version активна;
- какой image digest запущен;
- когда и кем выполнен release;
- какие migrations применены;
- прошли ли smoke checks;
- есть ли смешанные несовместимые versions.

## 27. Operational access

Production shell/DB access:

- ограничен;
- использует персональные identities;
- журналируется;
- требует MFA там, где доступно;
- не использует shared root credentials;
- применяется только по runbook/change/incident record.

Routine business correction через прямой SQL запрещена. Коррекция выполняется предметными application commands с audit.

Emergency SQL требует peer review, backup/restore point, scoped script, verification и post-incident documentation.

## 28. Release checklist

Release допускается, если:

1. CI и security scans прошли;
2. release artifact immutable;
3. image digests зафиксированы;
4. schema compatibility подтверждена;
5. migration preflight выполнен;
6. backup свежий;
7. rollback/forward-fix plan определён;
8. secrets/config validation пройдена;
9. capacity и disk space достаточны;
10. monitoring и alert routing работают;
11. staging smoke tests прошли;
12. owner deployment и incident contact назначены;
13. release notes содержат API/schema/operational changes;
14. post-deploy checks подготовлены.

## 29. Definition of Done для deployment change

Deployment change завершён только если:

1. topology/configuration impact описан;
2. local, CI, staging и production semantics не противоречат друг другу;
3. secrets не попали в repository/image/logs;
4. images воспроизводимы и immutable;
5. migration order и compatibility определены;
6. startup/readiness/graceful shutdown протестированы;
7. volumes и retention определены;
8. backup/restore impact оценён;
9. rollback/forward-fix описан;
10. security hardening сохранён;
11. observability и alerting обновлены;
12. runbook и документация обновлены;
13. change прошёл staging verification;
14. нет открытого P0/P1 operational blocker.

## 30. Открытые решения

До production необходимо утвердить ADR или эксплуатационную policy для:

1. production hosting topology и orchestration platform;
2. reverse proxy/ingress implementation;
3. secret manager;
4. container registry и artifact signing;
5. RPO/RTO;
6. backup technology, schedule и retention;
7. log/metrics/tracing platform;
8. exact deployment strategy: rolling, blue-green или иной вариант;
9. maintenance window policy;
10. production scaling baseline;
11. import object storage;
12. ownership releases, DBA operations и incident commander role;
13. staging data anonymization procedure;
14. certificate issuance/rotation;
15. production network segmentation.
