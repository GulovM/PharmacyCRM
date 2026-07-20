# PharmacyCRM — Documentation Index

**Статус документа:** Active  
**Версия:** 1.7  
**Дата:** 2026-07-20

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
| 06-01 | `06-01-database-design-return-allocations.md` | историческое дополнение по возвратным аллокациям, включённое в основной документ | Incorporated |
| 07 | `07-domain-model.md` | bounded contexts, агрегаты, сущности, value objects, состояния, события и транзакционные границы | Draft |
| 08 | `08-project-structure.md` | независимые корневые приложения `backend/` и `frontend/`, package layout, build/test/config boundaries | Draft |
| 09 | `09-security-design.md` | модель угроз, authentication, authorization, sessions, secrets, audit и secure development controls | Draft |
| 10 | `10-sequence-diagrams.md` | последовательности критических сценариев, transaction boundaries, locks, idempotency, audit и failure paths | Draft |

## 4. Планируемые документы

Следующая рекомендуемая последовательность:

| № | Планируемый документ | Назначение |
|---:|---|---|
| 11 | `11-development-roadmap.md` | этапы реализации, зависимости и Definition of Done |
| 12 | `12-deployment.md` | Docker, volumes, PostgreSQL `5433`, окружения, backup и recovery |
| 13 | `13-testing-strategy.md` | уровни тестирования и обязательные concurrency/contract/security tests |
| 14 | `14-observability.md` | Zap, log schema, ротация, tracing, metrics и alerting |

Номер зарезервирован даже до создания файла, чтобы новые документы не нарушали принятую последовательность.

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
3. Детализирующие документы используют номер родителя: `04-01`, `06-01`, ... .
4. `00` зарезервирован для индекса и документационного governance.
5. ADR сохраняют собственную четырёхзначную нумерацию внутри `docs/adr`.
6. Переименование файла обязательно сопровождается обновлением всех ссылок.

## 7. Definition of Done для документации

Изменение считается документированным только если:

1. затронутые SRS, Architecture, API, Domain Model, Project Structure, Security Design, Sequence Diagrams и Database Design не противоречат друг другу;
2. все ссылки используют актуальные пути;
3. новый верхнеуровневый файл имеет номер;
4. статус и версия документа обновлены;
5. открытые вопросы явно перечислены;
6. нормативные дополнения имеют понятный приоритет относительно основного документа;
7. HTTP feature обновляет `05-api-design.md` в том же change set;
8. изменение схемы синхронизирует Database Design и migrations;
9. изменение aggregate boundary или критического доменного инварианта синхронизирует `07-domain-model.md` и при необходимости оформляется ADR;
10. изменение backend module/package ownership синхронизирует `04-01-backend-architecture.md` и `08-project-structure.md`;
11. изменение корневого layout сохраняет независимые sibling roots `backend/` и `frontend/` и синхронизирует CI, Docker, Makefile, configuration и deployment paths;
12. изменение API generation flow синхронизирует backend contract и frontend generated-client rules;
13. изменение authentication, authorization, sessions, secrets, audit или public/private data boundary синхронизирует `09-security-design.md` и при необходимости оформляется ADR;
14. изменение transaction boundary, lock order, idempotency protocol, transactional audit или post-commit flow синхронизирует `10-sequence-diagrams.md`;
15. архитектурно значимое решение оформлено ADR;
16. примеры кода и диаграммы не противоречат принятым ADR и текущему стеку.

## 8. Известные задачи синхронизации

Перед началом массовой реализации необходимо:

1. определить нормативные правила возврата лекарств после юридической проверки;
2. создать deployment, testing и observability документы до production-ready реализации;
3. принять security ADR, перечисленные в `09-security-design.md`, до завершения соответствующих механизмов;
4. разрешить открытые вопросы lock order, transactional outbox и retry policy из `10-sequence-diagrams.md`;
5. при создании первых migrations сверить их с `06-database-design.md` версии 1.1 и добавить migration/concurrency tests;
6. внедрить автоматические architecture checks для package/import boundaries и запрета cross-root source imports;
7. утвердить frontend package manager, API client generation flow и ownership browser E2E tests.

Задачи создания `05-api-design.md`, интеграции возвратного amendment, добавления identity/assignments/sessions/idempotency/audit в Database Design, создания `07-domain-model.md`, фиксации независимых application roots, синхронизации backend architecture с Project Structure, создания Security Design и фиксации критических sequence diagrams закрыты.

## 9. Правило сопровождения

После каждого изменения документации автор обязан проверить:

- индекс;
- ссылки на переименованные файлы;
- статусы документов;
- ссылки на ADR;
- согласованность aggregate и transaction boundaries;
- согласованность lock order, idempotency, audit и post-commit flow;
- согласованность backend module ownership и package layout;
- согласованность authentication, authorization, sessions, secrets и audit controls;
- независимость `backend/` и `frontend/`;
- build/test/config boundaries двух приложений;
- отсутствие ненумерованных Markdown-файлов верхнего уровня;
- отсутствие незадокументированных API endpoint-ов.

Если документ устарел, он не должен оставаться без метки. Используются статусы `Draft`, `Active`, `Accepted`, `Accepted amendment`, `Incorporated`, `Deprecated` и `Superseded`.
