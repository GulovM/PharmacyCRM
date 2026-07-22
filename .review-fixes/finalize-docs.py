from pathlib import Path


def replace_exact(path: str, old: str, new: str) -> None:
    file_path = Path(path)
    text = file_path.read_text()
    if old not in text:
        raise RuntimeError(f"expected text not found in {path}: {old[:120]!r}")
    file_path.write_text(text.replace(old, new, 1))


for document in (
    "docs/09-security-design.md",
    "docs/12-deployment.md",
    "docs/13-testing-strategy.md",
    "docs/14-observability.md",
):
    replace_exact(document, "**Дата:** 2026-07-21", "**Дата:** 2026-07-22")

replace_exact(
    "docs/09-security-design.md",
    """E1 → E2 upgrade использует обязательный upgrade-only parameter `legacy_runtime_role`. Provisioning fail closed, если имя пусто, role отсутствует, совпадает с API/worker/migration login или с reserved E2 group role. Старый runtime credential выводится из эксплуатации до запуска E2 application: PostgreSQL catalogs `pg_default_acl`, `pg_roles` и `aclexplode` используются для обнаружения default table ACL независимо от их owner; direct table, column, sequence, function, schema и database privileges отзываются; memberships удаляются; login отключается и password очищается. Legacy role не удаляется автоматически, потому что ownership и audit/operational references требуют отдельной проверки.

`pharmacycrm_runtime` является только compatibility role для immutable migrations `000014–000019`. Provisioning нормализует её даже из существующей `LOGIN` role в `NOLOGIN`, passwordless, memberless role без memberships и direct/default privileges. Provisioning выполняется до migration и повторно после migration, поскольку immutable migrations временно ссылаются на compatibility role. API и worker logins никогда её не наследуют.""",
    """Provisioning требует явный `provisioning_mode=fresh|upgrade`. `fresh` разрешён только для пустой БД или уже согласованной schema version `23`; обнаружение более старой metadata немедленно прекращает операцию и требует upgrade procedure. E1 → E2 upgrade дополнительно требует точный `legacy_runtime_role`. Пустой/отсутствующий mode, пустое имя, отсутствующая role, collision с API/worker/migration login или reserved E2 group role завершаются fail closed.

Перед retirement cluster-wide ownership и ownership-related membership проверяются через `pg_shdepend`. Role с ownership, owning parent membership или privileges в другой database не изменяется автоматически. В target database каталоги `pg_default_acl`, ACL catalogs и `DROP OWNED` используются только после доказательства отсутствия ownership: direct table, column, sequence, function, schema/database privileges, default ACL и memberships удаляются; login отключается и password очищается. Legacy role сохраняется для audit/operational review.

Каждый provisioning pass также санитизирует существующие API и worker login roles: удаляет любые посторонние memberships и direct/default ACL, затем назначает ровно одну разрешённую `NOLOGIN` group role. `pharmacycrm_runtime` является только compatibility role для immutable migrations `000014–000019`; она нормализуется даже из существующей `LOGIN` role в `NOLOGIN`, passwordless, memberless role без memberships и direct/default privileges. Provisioning выполняется до migration и повторно после migration.

Provisioning principal является контролируемым PostgreSQL administrator: он владеет target database, может создавать/изменять roles и читать `pg_authid`. Runtime credentials никогда не обладают этими возможностями.""",
)

replace_exact(
    "docs/12-deployment.md",
    """Для E1 upgrade deployment передаёт upgrade-only `legacy_runtime_role`. Provisioning обнаруживает и отзывает legacy default ACL независимо от owner, direct privileges и memberships, затем устанавливает `NOLOGIN` и `PASSWORD NULL`. Legacy role не удаляется автоматически. `pharmacycrm_runtime` остаётся только `NOLOGIN` compatibility role без password, members, memberships и direct/default privileges; API и worker logins её не наследуют. Из-за ссылок immutable migrations `000014–000019` provisioning выполняется до и после migration chain.""",
    """Каждый запуск provisioning передаёт явный `provisioning_mode=fresh|upgrade`. `fresh` разрешён только для empty database либо schema version `23`; более старая metadata требует upgrade mode и не может быть случайно обработана как fresh install. Для E1 upgrade deployment дополнительно передаёт точный `legacy_runtime_role`. Provisioning проверяет cluster-wide ownership через `pg_shdepend`, отказывается автоматически менять role с ownership/owning membership/cross-database privileges, затем в target database отзывает default/direct ACL и memberships, устанавливает `NOLOGIN` и `PASSWORD NULL`. Legacy role не удаляется автоматически.

API и worker login roles перед выдачей разрешённой group role очищаются от любых direct/default ACL и посторонних memberships. `pharmacycrm_runtime` остаётся только `NOLOGIN` compatibility role без password, members, memberships и direct/default privileges; API и worker logins её не наследуют. Из-за ссылок immutable migrations `000014–000019` provisioning выполняется до и после migration chain. Execution principal должен владеть target database, иметь role-administration capability и доступ к `pg_authid`; обычный runtime/database-owner credential недостаточен.""",
)
replace_exact(
    "docs/12-deployment.md",
    "3. выполнить provisioning в upgrade mode с непустым `legacy_runtime_role` и проверить fail-closed validation;",
    "3. выполнить provisioning с `provisioning_mode=upgrade` и непустым `legacy_runtime_role`; отдельно доказать, что отсутствующий mode и `fresh` поверх E1 schema завершаются fail closed;",
)
replace_exact(
    "docs/12-deployment.md",
    "9. запустить новый worker, проверить protocol compatibility, heartbeat, bounded exhausted-lease terminalization и outbox processing;",
    "9. запустить новый worker, проверить protocol compatibility и heartbeat; при пустом E2 registry доказать maintenance-only polling без claim неизвестных business protocols, bounded exhausted-lease terminalization и retention;",
)

replace_exact(
    "docs/13-testing-strategy.md",
    "- invalid claim owner/limit/lease/timestamp/protocol input returns a typed pre-SQL error;",
    "- invalid claim owner/limit/lease/timestamp/protocol input returns a typed pre-SQL error; empty protocols are accepted only with explicit maintenance-only semantics;",
)
replace_exact(
    "docs/13-testing-strategy.md",
    "- graceful shutdown;",
    "- production worker wiring with an empty E2 registry remains alive until cancellation, terminalizes maintenance leases, never claims unknown protocols, and keeps retention active;\n- two-phase graceful shutdown waits for cooperative handlers after cancellation and reports bounded incomplete cancellation;",
)
replace_exact(
    "docs/13-testing-strategy.md",
    """- worker version compatibility during rolling deployment.

## 19. Migration tests""",
    """- worker version compatibility during rolling deployment.

Дополнительно mandatory PostgreSQL gate проверяет explicit provisioning mode, отказ `fresh` поверх E1 schema, owning-parent fail closed, destructive-test guard, reconciliation загрязнённых API/worker logins и отсутствие direct ACL/extra memberships после повторного provisioning. Cluster-role test запускается только в disposable cluster при `ALLOW_DESTRUCTIVE_CLUSTER_ROLE_TEST=true` и отказывается работать, если reserved roles уже существуют.

## 19. Migration tests""",
)

replace_exact(
    "docs/14-observability.md",
    """- heartbeat/readiness;
- worker protocol mismatch;
- projection lag.""",
    """- heartbeat/readiness;
- worker protocol mismatch;
- worker mode (`delivery` или `maintenance_only`) и unexpected early process exit;
- drain timeout и cancellation-grace exhaustion;
- projection lag.""",
)
replace_exact(
    "docs/14-observability.md",
    """Repository получает `RowsAffected()` для exhausted-lease terminalization и проверяет, что result не превышает bounded limit. На E2 существующий observer interface не расширяется ради одного счётчика: значение доступно internal test seam и может быть выведено structured worker log/metric через существующую observability abstraction без изменения repository contract. Один poll не должен создавать unbounded WAL/lock spike; saturation и outbox oldest age рассматриваются совместно.""",
    """При пустом E2 protocol registry worker публикует mode `maintenance_only`: claim неизвестных protocols отсутствует, но terminalization и retention heartbeat остаются наблюдаемыми. Repository получает `RowsAffected()` для exhausted-lease terminalization и проверяет, что result не превышает bounded limit. На E2 существующий observer interface не расширяется ради одного счётчика: значение доступно internal test seam и может быть выведено structured worker log/metric через существующую observability abstraction без изменения repository contract. Один poll не должен создавать unbounded WAL/lock spike; saturation и outbox oldest age рассматриваются совместно.""",
)

workflow_path = Path(".github/workflows/ci.yml")
workflow = workflow_path.read_text()
start_marker = "  # BEGIN TEMP REVIEW FIX JOB\n"
end_marker = "  # END TEMP REVIEW FIX JOB\n"
start = workflow.index(start_marker)
end = workflow.index(end_marker, start) + len(end_marker)
workflow = workflow[:start] + workflow[end:]
workflow = workflow.replace("permissions:\n  contents: write\n", "permissions:\n  contents: read\n", 1)
workflow_path.write_text(workflow)
