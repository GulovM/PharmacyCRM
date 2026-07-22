from pathlib import Path

root = Path(__file__).resolve().parents[1]


def replace_once(path: str, old: str, new: str) -> None:
    target = root / path
    text = target.read_text()
    if text.count(old) != 1:
        raise RuntimeError(f"expected one marker in {path}, found {text.count(old)}")
    target.write_text(text.replace(old, new, 1))


replace_once(
    "docs/04-01-backend-architecture.md",
    "Запрещены global service locator, package-level mutable dependency registry и чтение environment из произвольных business packages.\n\n## 8. Dependency Injection",
    """Запрещены global service locator, package-level mutable dependency registry и чтение environment из произвольных business packages.

### 7.1 Process configuration boundaries

Каждый executable загружает только принадлежащую ему operational configuration:

- `APIConfig` содержит `App`, `HTTP`, `APIPostgres`, `Auth`, `ProxyCORS`, `Logging`, `Telemetry` и `Storage`;
- API loader не читает и не валидирует `WORKER_*`; отсутствие или некорректность worker operational settings не может остановить API;
- `WorkerProcessConfig` содержит отдельный `WorkerPostgres` и полный `WorkerConfig`; `LoadWorker()` строго проверяет owner, concurrency, polling, lease, claim, drain и retention limits;
- `MigrationConfig` содержит только shared application metadata, migration DSN и logging;
- protocol compatibility остаётся shared release contract через `AppConfig.WorkerProtocol`, но полный operational `WorkerConfig` никогда не возвращается API process.

`POSTGRES_API_RUNTIME_DSN`, `POSTGRES_WORKER_RUNTIME_DSN` и `POSTGRES_MIGRATION_DSN` доставляются только соответствующему process. Переменная из чужой process ownership group не является скрытой startup dependency.

## 8. Dependency Injection""",
)
replace_once(
    "docs/04-01-backend-architecture.md",
    """- после exhaustion переводит event в `DEAD_LETTER`;
- при transient polling errors использует bounded backoff, при fatal error прекращает polling и выполняет bounded graceful drain уже запущенных handlers;""",
    """- после exhaustion переводит event в `DEAD_LETTER`;
- перед claim переводит только bounded batch просроченных `PROCESSING` leases с исчерпанными attempts: deterministic `ORDER BY lease_expires_at, id`, `FOR UPDATE SKIP LOCKED`, limit `1..100`;
- при request limit `N` одна claim transaction меняет не более `N` exhausted rows плюс `N` claimed rows, то есть максимум `2N`;
- invalid owner, limit, lease duration, timestamp или protocol set отклоняются typed application error до первого SQL statement;
- при transient polling errors использует bounded backoff, при fatal error прекращает polling и выполняет bounded graceful drain уже запущенных handlers;""",
)

replace_once(
    "docs/09-security-design.md",
    """Runtime role не создаёт schema/roles/extensions, не выполняет unrestricted DDL и не имеет `BYPASSRLS`/superuser privileges.

PostgreSQL не публикуется в интернет.""",
    """Runtime role не создаёт schema/roles/extensions, не выполняет unrestricted DDL и не имеет `BYPASSRLS`/superuser privileges.

E1 → E2 upgrade использует обязательный upgrade-only parameter `legacy_runtime_role`. Provisioning fail closed, если имя пусто, role отсутствует, совпадает с API/worker/migration login или с reserved E2 group role. Старый runtime credential выводится из эксплуатации до запуска E2 application: PostgreSQL catalogs `pg_default_acl`, `pg_roles` и `aclexplode` используются для обнаружения default table ACL независимо от их owner; direct table, column, sequence, function, schema и database privileges отзываются; memberships удаляются; login отключается и password очищается. Legacy role не удаляется автоматически, потому что ownership и audit/operational references требуют отдельной проверки.

`pharmacycrm_runtime` является только compatibility role для immutable migrations `000014–000019`. Provisioning нормализует её даже из существующей `LOGIN` role в `NOLOGIN`, passwordless, memberless role без memberships и direct/default privileges. Provisioning выполняется до migration и повторно после migration, поскольку immutable migrations временно ссылаются на compatibility role. API и worker logins никогда её не наследуют.

PostgreSQL не публикуется в интернет.""",
)

replace_once(
    "docs/13-testing-strategy.md",
    "> E2 schema `23` integration coverage includes E1/19 upgrades, session-security negative constraints, API/worker privilege denial and capability-based outbox replay.",
    "> E2 schema `23` integration coverage includes E1/19/21 upgrades, real E1 credential retirement, process-owned configuration, bounded exhausted-lease terminalization, session-security negative constraints, API/worker privilege denial and capability-based outbox replay.",
)
replace_once(
    "docs/13-testing-strategy.md",
    """- lease expiry recovery;
- stale fencing rejection;
- bounded retry/backoff;""",
    """- lease expiry recovery;
- exhausted lease terminalization is deterministic by `lease_expires_at, id`, bounded by request limit `1..100`, and changes at most `2N` rows together with claim;
- unexpired, retryable, `PENDING`, `PROCESSED` and already `DEAD_LETTER` rows are not terminalized;
- concurrent workers do not process the same exhausted lease twice and independently respect `SKIP LOCKED` limits;
- invalid claim owner/limit/lease/timestamp/protocol input returns a typed pre-SQL error;
- stale fencing rejection;
- bounded retry/backoff;""",
)
replace_once(
    "docs/13-testing-strategy.md",
    """8. runtime role permissions;
9. readiness compatibility;""",
    """8. runtime role permissions, including a real isolated E1 LOGIN/password/default-ACL scenario and post-migration compatibility-role reconciliation;
9. readiness compatibility;""",
)
replace_once(
    "docs/13-testing-strategy.md",
    """Mandatory PostgreSQL CI gate запускает без skip:

- `internal/platform/database -run Integration`;
- `internal/platform/migration -run Integration` (включая E1 `1` → E2 `21`);""",
    """Mandatory PostgreSQL CI gate запускает без skip при `CI_INTEGRATION_REQUIRED=true`:

- `deploy/scripts/tests/test-e1-role-upgrade.sh` как отдельный шаг `Verify E1 runtime credential retirement`;
- `internal/platform/database -run Integration`;
- `internal/platform/migration -run Integration` (пути `0 → 23`, E1 `1 → 23`, `19 → 23`, `21 → 23` и `23 → no-op`);""",
)

replace_once(
    "docs/14-observability.md",
    """- dead-letter count;
- lease expiry/recovery;
- stale fencing rejection;""",
    """- dead-letter count;
- lease expiry/recovery;
- exhausted leases terminalized per poll и cumulative count с reason `LEASE_EXPIRED_AFTER_MAX_ATTEMPTS`;
- terminalization batch saturation (`rows_affected == configured_limit`) как сигнал накопленного exhausted backlog;
- stale fencing rejection;""",
)
replace_once(
    "docs/14-observability.md",
    """- retention cycle failures.

### 17.2 Imports""",
    """- retention cycle failures.

Repository получает `RowsAffected()` для exhausted-lease terminalization и проверяет, что result не превышает bounded limit. На E2 существующий observer interface не расширяется ради одного счётчика: значение доступно internal test seam и может быть выведено structured worker log/metric через существующую observability abstraction без изменения repository contract. Один poll не должен создавать unbounded WAL/lock spike; saturation и outbox oldest age рассматриваются совместно.

### 17.2 Imports""",
)

replace_once(
    "docs/11-development-roadmap.md",
    "- upgrade E1 schema `1` → current E2 schema `21` с immutable checksum history;",
    "- upgrades `0 → 23`, E1 `1 → 23`, `19 → 23`, `21 → 23` и `23 → no-op` с immutable checksum history;",
)
replace_once(
    "docs/11-development-roadmap.md",
    """- two-worker lease race;
- duplicate outbox delivery без duplicate business effect;""",
    """- two-worker lease race;
- bounded exhausted-lease terminalization with deterministic order and at most `2N` changed rows per claim transaction;
- E1 runtime credential retirement with password/default/direct privileges removed and idempotent pre/post-migration provisioning;
- API startup independent from all `WORKER_*` operational settings;
- duplicate outbox delivery без duplicate business effect;""",
)

replace_once(
    "docs/12-deployment.md",
    """Configuration groups:

- application/environment;
- HTTP server;
- PostgreSQL;
- authentication/session;
- security headers/trusted proxies;
- logging;
- metrics/tracing;
- worker/job protocol;
- import limits/storage;
- backup/operations.""",
    """Configuration groups разделяются по process ownership:

- shared application metadata, schema compatibility и worker protocol compatibility;
- API-only HTTP, authentication/session, security headers/trusted proxies и `POSTGRES_API_RUNTIME_DSN`;
- worker-only owner, concurrency, polling, lease, claim, drain, retention и `POSTGRES_WORKER_RUNTIME_DSN`;
- migration-only `POSTGRES_MIGRATION_DSN`;
- shared logging, metrics/tracing и storage settings только там, где process действительно их использует;
- backup/operations settings для соответствующих one-shot jobs.

API process не читает и не требует `WORKER_OWNER`, `WORKER_CONCURRENCY`, `WORKER_POLL_INTERVAL`, `WORKER_LEASE_DURATION`, `WORKER_MAX_CLAIM`, `WORKER_DRAIN_TIMEOUT` или `WORKER_RETENTION_*`. Invalid worker operational environment не может сломать корректно настроенный API. Worker process по-прежнему валидирует полный worker config fail closed.""",
)
replace_once(
    "docs/12-deployment.md",
    """API и worker roles могут различаться. Runtime roles не имеют права создавать schema, управлять roles, выполнять unrestricted DDL или изменять immutable history вне разрешённого protocol.

### 11.3 Pool budget""",
    """API и worker roles обязаны различаться и используют отдельные `POSTGRES_API_RUNTIME_DSN` и `POSTGRES_WORKER_RUNTIME_DSN`; migration job использует только `POSTGRES_MIGRATION_DSN`. Runtime roles не имеют права создавать schema, управлять roles, выполнять unrestricted DDL или изменять immutable history вне разрешённого protocol.

Для E1 upgrade deployment передаёт upgrade-only `legacy_runtime_role`. Provisioning обнаруживает и отзывает legacy default ACL независимо от owner, direct privileges и memberships, затем устанавливает `NOLOGIN` и `PASSWORD NULL`. Legacy role не удаляется автоматически. `pharmacycrm_runtime` остаётся только `NOLOGIN` compatibility role без password, members, memberships и direct/default privileges; API и worker logins её не наследуют. Из-за ссылок immutable migrations `000014–000019` provisioning выполняется до и после migration chain.

### 11.3 Pool budget""",
)
replace_once(
    "docs/12-deployment.md",
    """Текущая E2 application compatibility закреплена на schema version `21`. CI проверяет migration from zero и изолированный upgrade от E1 version `1`; API/worker не применяют migrations при startup. API, worker и migrator используют разные `POSTGRES_API_RUNTIME_DSN`, `POSTGRES_WORKER_RUNTIME_DSN` и `POSTGRES_MIGRATION_DSN`.

Migration process:""",
    """Supported schema version: `23`. Поддерживаемые и обязательные CI paths: `0 → 23`, `1 → 23`, `19 → 23`, `21 → 23` и `23 → no-op`. API/worker не применяют migrations при startup. API, worker и migrator используют разные `POSTGRES_API_RUNTIME_DSN`, `POSTGRES_WORKER_RUNTIME_DSN` и `POSTGRES_MIGRATION_DSN`.

Migration process:""",
)
replace_once(
    "docs/12-deployment.md",
    """8. фиксирует resulting schema version и evidence;
9. освобождает lock.

Одновременно migration запускает только один владелец.

### 13.2 Expand → migrate → contract""",
    """8. фиксирует resulting schema version и evidence;
9. освобождает lock.

Одновременно migration запускает только один владелец.

### 13.2 E1 → E2 credential-safe upgrade order

1. остановить все E1 API и worker processes; legacy credential не используется после этого шага;
2. создать backup или подтверждённый restore point;
3. выполнить provisioning в upgrade mode с непустым `legacy_runtime_role` и проверить fail-closed validation;
4. доказать, что legacy login отключён, password очищен, memberships/direct/default privileges отсутствуют;
5. применить immutable migrations до schema version `23` через `POSTGRES_MIGRATION_DSN`;
6. повторно выполнить тот же idempotent provisioning в upgrade mode, чтобы удалить compatibility grants, временно созданные migrations `000014–000019`;
7. проверить API и worker privilege matrix, а также inert `pharmacycrm_runtime NOLOGIN`;
8. запустить новый API без `WORKER_*` operational settings и дождаться readiness;
9. запустить новый worker, проверить protocol compatibility, heartbeat, bounded exhausted-lease terminalization и outbox processing;
10. выполнить post-deploy smoke и только после этого переключить traffic.

Legacy role сохраняется для audit/ownership review и не удаляется автоматически. Если role или одна из memberships владеет объектами, которые нельзя безопасно интерпретировать, provisioning прекращается до изменения ownership.

### 13.3 Expand → migrate → contract""",
)
replace_once("docs/12-deployment.md", "### 13.3 Migration review card", "### 13.4 Migration review card")
replace_once("docs/12-deployment.md", "### 13.4 Failed migration", "### 13.5 Failed migration")

replace_once(
    ".github/workflows/ci.yml",
    """      - name: Run backend static checks and tests
        working-directory: backend
        run: |
          go vet ./...
          go test ./...
      - name: Run frontend lint, type checks and tests
""",
    """      - name: Run backend static checks and tests
        working-directory: backend
        run: |
          set -o pipefail
          {
            go vet ./...
            go test ./... -count=1
          } 2>&1 | tee \"$RUNNER_TEMP/backend-quality.log\"
      - name: Upload backend quality failure log
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: backend-quality-failure
          path: ${{ runner.temp }}/backend-quality.log
          if-no-files-found: error
      - name: Run frontend lint, type checks and tests
""",
)
replace_once(
    ".github/workflows/ci.yml",
    """      - name: Verify E1 runtime credential retirement
        run: bash deploy/scripts/tests/test-e1-role-upgrade.sh
      - name: Provision separate runtime and migration roles
""",
    """      - name: Verify E1 runtime credential retirement
        run: |
          set -o pipefail
          bash deploy/scripts/tests/test-e1-role-upgrade.sh 2>&1 | tee \"$RUNNER_TEMP/e1-role-upgrade.log\"
      - name: Upload E1 role-upgrade failure log
        if: failure()
        uses: actions/upload-artifact@v4
        with:
          name: e1-role-upgrade-failure
          path: ${{ runner.temp }}/e1-role-upgrade.log
          if-no-files-found: error
      - name: Provision separate runtime and migration roles
""",
)

for relative in (
    "docs/04-01-backend-architecture.md",
    "docs/09-security-design.md",
    "docs/11-development-roadmap.md",
    "docs/12-deployment.md",
    "docs/13-testing-strategy.md",
    "docs/14-observability.md",
):
    target = root / relative
    text = target.read_text().replace("**Дата:** 2026-07-21", "**Дата:** 2026-07-22", 1)
    target.write_text(text)
