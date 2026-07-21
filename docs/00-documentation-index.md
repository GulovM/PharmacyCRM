# PharmacyCRM — Documentation Index

**Статус документа:** Active  
**Версия:** 2.4  
**Дата:** 2026-07-21  

## 1. Назначение

Этот файл является навигационной картой документации PharmacyCRM. Он показывает назначение, статус и нормативную роль документов, а также определяет порядок их чтения и обновления.

Документ не заменяет SRS, архитектуру или ADR. Его задача — не допустить появления забытых, ненумерованных, противоречащих друг другу или неясно применимых файлов.

## 2. Порядок приоритетов

При противоречии применяется порядок, установленный в `02-srs.md`:

1. применимое законодательство и обязательные регуляторные требования;
2. `01-product-vision.md`;
3. `02-srs.md`;
4. принятые ADR;
5. детальные проектные документы;
6. реализация и тесты.

Обнаруженное противоречие не разрешается молча. Документы должны быть синхронизированы в том же change set либо расхождение должно быть явно зафиксировано как открытый вопрос.

## 3. Основные документы

| № | Документ | Назначение | Текущий статус |
|---:|---|---|---|
| 00 | `00-documentation-index.md` | карта документации и правила её сопровождения | Active |
| 01 | `01-product-vision.md` | продуктовое видение, пользователи и границы MVP | Draft |
| 02 | `02-srs.md` | нормативные системные требования и критерии приёмки | Draft |
| 03 | `03-system-context.md` | системная граница, акторы, внешние зависимости и trust boundaries | Draft |
| 04 | `04-architecture.md` | общая целевая архитектура и обязательные архитектурные правила | Draft |
| 04-01 | `04-01-backend-architecture.md` | конкретизация Go backend, composition root, orchestration и Unit of Work | Draft |
| 05 | `05-api-design.md` | единый человекочитаемый каталог HTTP API-контрактов | Draft |
| 06 | `06-database-design.md` | целевая PostgreSQL-модель, DDL, инварианты, индексы и migration strategy | Draft |
| 07 | `07-domain-model.md` | bounded contexts, агрегаты, сущности, value objects, состояния, события и транзакционные границы | Draft |
| 08 | `08-project-structure.md` | независимые корневые приложения `backend/` и `frontend/`, package layout, build/test/config boundaries | Draft |
| 09 | `09-security-design.md` | модель угроз, authentication, authorization, sessions, secrets, audit и secure development controls | Draft |
| 10 | `10-sequence-diagrams.md` | последовательности критических сценариев, transaction boundaries, locks, idempotency, audit и failure paths | Draft |
| 11 | `11-development-roadmap.md` | risk-first этапы реализации, зависимости, quality gates и Definition of Done | Draft |
| 12 | `12-deployment.md` | окружения, Docker, volumes, migrations, release, backup, recovery и production operations | Draft |
| 13 | `13-testing-strategy.md` | уровни тестирования, PostgreSQL integration, concurrency, security, contract, migration, recovery и release gates | Draft |
| 14 | `14-observability.md` | Zap logging, audit correlation, metrics, tracing, SLI/SLO, alerts, dashboards и incident evidence | Draft |

## 4. Статус основного комплекта

Основной проектный комплект документов `00–14` создан. Новые верхнеуровневые документы добавляются только при появлении отдельной нормативной области, которую нельзя корректно включить в существующие документы.

Дальнейшая работа с документацией состоит из:

- предотвращения новых противоречий в одном change set;
- принятия обязательных ADR;
- перевода зрелых документов из `Draft` в утверждённый статус;
- синхронизации документов с реализацией в каждом change set;
- создания узких runbooks и operational policies по мере необходимости.

Новый файл не создаётся только ради переноса раздела из существующего документа. Временный amendment после полной инкорпорации удаляется; в репозитории остаётся только итоговая версия основного документа.

## 5. ADR

Архитектурные решения находятся в `docs/adr` и имеют независимую последовательность `0001`, `0002`, ... .

ADR после принятия не переписывается так, будто старого решения не существовало. Изменение решения оформляется новым ADR со статусом `Supersedes`, а старый получает ссылку на заменяющий документ.

Актуальные решения включают:

- базовые единицы отпуска;
- неизменяемые складские движения;
- детерминированные блокировки;
- продажи и возвраты;
- Unit of Work;
- Gin в delivery-слое;
- Zap, конфигурацию и локальные Docker volumes;
- централизованный HTTP error mapper;
- композицию middleware;
- единый API contract catalog.

## 6. Правила нумерации

1. Все Markdown-документы верхнего уровня `docs` имеют числовой префикс.
2. Основные документы используют двухзначный номер: `01`, `02`, ... .
3. Детализирующие активные документы используют номер родителя: `04-01`, ... .
4. `00` зарезервирован для индекса и документационного governance.
5. ADR сохраняют собственную четырёхзначную нумерацию внутри `docs/adr`.
6. Переименование или удаление файла обязательно сопровождается обновлением всех ссылок.
7. Runbooks и operational policies размещаются в соответствующей deployment/operations структуре и не обязаны становиться новыми основными документами.

## 7. Definition of Done для документации

Изменение считается документированным только если:

1. затронутые SRS, Architecture, API, Domain Model, Project Structure, Security Design, Sequence Diagrams, Development Roadmap, Deployment, Testing Strategy, Observability и Database Design не противоречат друг другу;
2. все ссылки используют актуальные пути;
3. новый верхнеуровневый файл имеет номер;
4. статус и версия документа обновлены;
5. открытые вопросы явно перечислены;
6. временный amendment полностью перенесён в исходные документы и удалён;
7. HTTP feature обновляет `05-api-design.md` в том же change set;
8. изменение схемы синхронизирует Database Design и migrations;
9. изменение aggregate boundary или критического доменного инварианта синхронизирует `07-domain-model.md` и при необходимости оформляется ADR;
10. изменение backend module/package ownership синхронизирует `04-01-backend-architecture.md` и `08-project-structure.md`;
11. изменение корневого layout сохраняет независимые sibling roots `backend/` и `frontend/` и синхронизирует CI, Docker, Makefile, configuration и deployment paths;
12. изменение API generation flow синхронизирует backend contract и frontend generated-client rules;
13. изменение authentication, authorization, sessions, secrets, audit или public/private data boundary синхронизирует `09-security-design.md` и при необходимости оформляется ADR;
14. изменение transaction boundary, lock order, idempotency protocol, transactional audit или post-commit flow синхронизирует `10-sequence-diagrams.md`;
15. изменение MVP scope, зависимостей этапов, release gate, Definition of Ready/Done или приоритета P0/P1 синхронизирует `11-development-roadmap.md`;
16. изменение topology, ports, volumes, runtime configuration, migration order, readiness, release, rollback, backup или restore синхронизирует `12-deployment.md`;
17. изменение acceptance criteria, authorization matrix, persistence behavior, concurrency protocol, contract, migration, security, frontend workflow, performance baseline или recovery procedure синхронизирует `13-testing-strategy.md` и соответствующие automated suites;
18. изменение log/audit schema, metrics, labels, traces, SLI/SLO, alerts, dashboards, retention, redaction или incident evidence синхронизирует `14-observability.md`;
19. архитектурно значимое решение оформлено ADR;
20. примеры кода и диаграммы не противоречат принятым ADR и текущему стеку.

## 8. Текущий статус синхронизации

Cross-document consistency review полностью перенесён в основные документы. Gate E0 закрыт: security, reliability, delivery, retention, recovery, legal-return и frontend/tooling baselines утверждены в исходных документах.

До начала зависимого этапа остаются только implementation evidence соответствующего gate: migrations, tests, restore drills, security review и CI artifacts. Они не являются альтернативными архитектурными решениями.

## 9. Правило сопровождения

После каждого изменения документации автор обязан проверить:

- индекс;
- ссылки на переименованные и удалённые файлы;
- статусы документов;
- ссылки на ADR;
- согласованность aggregate и transaction boundaries;
- согласованность lock order, idempotency, audit и post-commit flow;
- согласованность этапов, gates, Definition of Ready/Done и release blockers;
- согласованность deployment topology, ports, volumes, configuration, migrations, release и recovery;
- согласованность test levels, authorization matrix, concurrency, migration, contract, security и release evidence;
- согласованность logs, audit, metrics, traces, SLI/SLO, alerts, dashboards и retention;
- согласованность backend module ownership и package layout;
- согласованность authentication, authorization, sessions, secrets и audit controls;
- независимость `backend/` и `frontend/`;
- build/test/config boundaries двух приложений;
- отсутствие ненумерованных Markdown-файлов верхнего уровня;
- отсутствие незадокументированных API endpoint-ов;
- отсутствие временных amendments после полной инкорпорации.

Если верхнеуровневый документ больше не является источником актуальных правил и его содержимое полностью перенесено, файл удаляется. История изменения сохраняется в Git, а не отдельной устаревшей копией документа.