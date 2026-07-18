# PharmacyCRM — Security Design

**Статус документа:** Draft  
**Версия:** 0.2  
**Дата:** 2026-07-18

## 1. Назначение документа

Документ определяет обязательную модель безопасности PharmacyCRM: защищаемые активы, границы доверия, модель угроз, аутентификацию, управление сессиями, авторизацию, защиту HTTP API и браузерного клиента, криптографию, управление секретами, аудит, журналирование, защиту данных, безопасную разработку и реагирование на инциденты.

Security Design является нормативным для backend, frontend, инфраструктуры, тестов и эксплуатационных процедур. Функция не считается завершённой, пока её security controls не реализованы, не задокументированы и не покрыты позитивными, негативными и конкурентными тестами.

Документ не заменяет:

- `01-product-vision.md` — продуктовые цели и границы MVP;
- `02-srs.md` — обязательное внешнее поведение системы;
- `03-system-context.md` — системную границу, акторов и trust boundaries;
- `04-architecture.md` и `04-01-backend-architecture.md` — архитектурные слои, модули и dependency rules;
- `05-api-design.md` — HTTP-контракты;
- `06-database-design.md` — таблицы, ограничения и модель хранения;
- `07-domain-model.md` — агрегаты, инварианты и транзакционные границы;
- `08-project-structure.md` — физическую структуру backend и frontend;
- ADR — принятые архитектурно значимые решения.

При противоречии применяется порядок приоритетов, установленный в SRS. Security Design не должен незаметно расширять полномочия ролей, менять бизнес-инварианты или создавать новый публичный контракт.

## 2. Нормативные слова

- **Должен / обязан / запрещено** — обязательное требование.
- **Следует** — рекомендуемое требование; отклонение требует ADR или явно зафиксированного security exception.
- **Может** — допустимый вариант, не являющийся обязательным.

Security exception должен иметь владельца, обоснование, оценку риска, компенсирующие меры и дату пересмотра. Бессрочные исключения запрещены.

## 3. Цели безопасности

PharmacyCRM должна обеспечивать:

1. конфиденциальность учётных данных, токенов, персональных и внутренних операционных данных;
2. целостность складских, торговых, ценовых и административных операций;
3. невозможность обхода ограничений роли и назначения аптекаря конкретной аптеке;
4. невозможность незаметного изменения проведённых документов, складских движений и аудита;
5. контролируемое прекращение доступа заблокированного, архивированного или скомпрометированного пользователя;
6. устойчивость критических команд к повторной отправке, гонкам и подмене payload;
7. трассируемость действий человека, администратора, фонового процесса и будущей интеграции;
8. минимизацию последствий компрометации токена, браузера, аккаунта, контейнера или инфраструктурного компонента;
9. безопасное хранение, использование и ротацию секретов;
10. обнаружение злоупотреблений и достаточные данные для расследования;
11. отказ в доступе при неопределённости;
12. автоматическую проверяемость security controls.

## 4. Принципы безопасности

### 4.1 Недоверенный клиент

Браузер, frontend-код, HTTP-запрос, route parameters, headers, cookies, импортируемые файлы и любые клиентские идентификаторы являются недоверенными.

Backend самостоятельно определяет и проверяет:

- идентичность и состояние пользователя;
- состояние сессии;
- актуальную роль;
- активное назначение аптекаря аптеке;
- право на действие над конкретным ресурсом;
- pharmacy scope ресурса;
- итоговую цену и денежную сумму;
- доступный остаток;
- допустимые лоты и FEFO-распределение;
- серверное время;
- допустимый переход состояния;
- соответствие idempotency key смысловому payload.

Скрытие кнопки, страницы или маршрута во frontend не является авторизацией.

### 4.2 Deny by default

Неизвестная роль, неизвестное состояние, отсутствующее назначение, неполный security context, ошибка чтения пользователя или сессии, ошибка policy check и неоднозначный результат приводят к отказу.

Authentication, authorization, audit и secret-loading не должны использовать fail-open поведение.

### 4.3 Least privilege

Пользователь, процесс, контейнер, токен, сервисный аккаунт и роль БД получают только минимальные полномочия, необходимые для конкретной функции.

### 4.4 Defense in depth

Критический инвариант защищается несколькими уровнями:

- transport и ingress controls;
- HTTP validation;
- authentication middleware;
- application authorization policy;
- domain invariants;
- транзакционные проверки;
- PostgreSQL constraints и locks;
- audit и monitoring.

### 4.5 Complete mediation

Каждая защищённая операция заново проходит необходимые проверки. Предыдущая успешная загрузка страницы, списка или связанной команды не подтверждает право на текущую операцию.

### 4.6 Separation of duties

Инфраструктурный оператор не является бизнес-администратором. `ADMIN` приложения не получает право незаметно переписывать историю. Migration credentials не используются runtime-приложением.

### 4.7 Security by construction

Security rules выражаются в типах, policy-компонентах, use case, Unit of Work, ограничениях БД и тестах. Разрозненные role checks в handler-ах запрещены.

## 5. Защищаемые активы и классификация

### 5.1 Критические активы

- password hashes;
- access, refresh, reset и MFA recovery tokens;
- session records;
- роли и назначения;
- продажи, возвраты, поступления, списания и корректировки;
- движения и остатки по лотам;
- закупочные и продажные цены;
- idempotency records;
- audit events;
- signing/encryption keys и credentials;
- backup;
- administrative endpoints;
- CI/CD pipeline и release artifacts.

### 5.2 Классы данных

| Класс | Примеры | Базовое правило |
|---|---|---|
| `PUBLIC` | опубликованный каталог, разрешённые данные аптеки, публичная цена, агрегированный статус наличия | доступ только через публичный contract |
| `INTERNAL` | точные остатки, служебные документы, предупреждения | только аутентифицированным ролям по scope |
| `CONFIDENTIAL` | закупочные цены, персональные данные, audit, IP, supplier data | минимальный доступ, аудит чтения при необходимости |
| `SECRET` | raw tokens, passwords, signing keys, DSN, backup keys | не логировать, не возвращать, хранить только в secret storage |

Точный остаток, внутренние lot IDs, закупочная цена, документы и audit trail не должны попадать в публичный API.

## 6. Границы доверия

Основные trust boundaries:

1. интернет → reverse proxy/ingress;
2. браузер → frontend;
3. frontend → backend API;
4. delivery → application;
5. application → infrastructure adapters;
6. backend → PostgreSQL;
7. backend → файловое хранилище или внешняя интеграция;
8. CI/CD → registry и production;
9. оператор → management plane;
10. backup storage → restore process.

Для каждой новой внешней интеграции до реализации должны быть определены данные, authentication method, timeout, retry, SSRF controls, failure mode, secret owner и audit requirements.

## 7. Модель угроз

Рассматриваются:

- внешний атакующий: credential stuffing, token theft, injection, XSS, CSRF, SSRF, traversal, enumeration, scraping, DoS, malicious upload, proxy/CORS misconfiguration;
- скомпрометированный `CLIENT`: доступ к внутреннему API, IDOR, mass assignment, privilege escalation;
- скомпрометированный `PHARMACIST`: подмена `pharmacy_id`, обход FEFO, продажа запрещённого лота, подмена цены, повтор команды, фиктивная корректировка, удаление следов;
- скомпрометированный `ADMIN`: создание скрытого администратора, изменение ролей, массовый экспорт, блокировка пользователей, отключение контроля, компрометация каталога;
- инфраструктурный инсайдер: чтение secrets/backup, прямое изменение БД, подмена artifact, отключение logging;
- compromised dependency/pipeline: malicious code, secret theft, forged image, dependency lifecycle execution;
- accidental misuse: ошибочное назначение, неверный import, запуск migration не в том окружении, публикация чувствительного ответа.

Риск оценивается по влиянию и вероятности. Угрозы потери целостности продаж, остатков, ролей, сессий и аудита считаются не ниже `HIGH`.

## 8. Идентичность и жизненный цикл пользователя

Минимальные состояния пользователя: `ACTIVE`, `BLOCKED`, `ARCHIVED`.

Только `ACTIVE` может создавать сессию и выполнять защищённые операции. `BLOCKED` и `ARCHIVED` должны быть отклонены при refresh и при повторной проверке критической операции.

Блокировка, архивирование, смена пароля, изменение роли и подтверждённая компрометация должны отзывать все refresh sessions пользователя в той же транзакции либо через fail-closed протокол с гарантированным security SLA.

Целевой SLA прекращения доступа:

- refresh и новые сессии — немедленно после commit;
- критические и административные mutations — немедленно за счёт DB revalidation;
- некритические read requests с уже выпущенным access token — не позднее TTL access token;
- при инциденте должна существовать возможность принудительно инвалидировать access tokens через session version или denylist.

Фоновые задачи используют `SYSTEM` actor. Они не маскируются под пользователя или `ADMIN`.

## 9. Пароли и восстановление доступа

### 9.1 Хранение

Пароли хранятся только как password hash. Обратимое шифрование запрещено.

Для новых hashes используется Argon2id. Параметры выбираются нагрузочным тестом целевой среды, хранятся вместе с hash и пересматриваются не реже одного раза в год. Допускается чтение legacy bcrypt hashes с обязательным rehash после успешного входа.

### 9.2 Политика пароля

- `CLIENT` и `PHARMACIST`: минимум 12 символов;
- `ADMIN`: минимум 14 символов;
- максимум: 128 Unicode code points; молчаливое обрезание запрещено;
- разрешены passphrases и password manager generated values;
- запрещены известные скомпрометированные и очевидно слабые пароли;
- обязательная периодическая смена без признаков риска не требуется;
- password hints и secret questions запрещены.

### 9.3 Reset и activation

Reset/activation token:

- создаётся CSPRNG;
- имеет не менее 128 бит энтропии;
- хранится только как hash;
- одноразовый;
- имеет TTL не более 30 минут;
- привязан к user ID и purpose;
- инвалидируется после использования, нового выпуска, смены пароля, блокировки и архивирования.

Ответ reset request не раскрывает существование пользователя. Успешный reset отзывает все сессии и создаёт audit event.

Recovery flow для `ADMIN` требует отдельной утверждённой процедуры и не должен обходить MFA без усиленного подтверждения и аудита.

## 10. Аутентификация и сессии

### 10.1 Модель

Web-клиент использует короткоживущий access token и server-side refresh session.

Access token содержит только:

- `sub`;
- `sid`;
- `iat`;
- `nbf`, если используется;
- `exp`;
- `iss`;
- `aud`;
- `jti`;
- `key_id` через JWT header `kid`.

Роль может быть подсказкой, но не источником окончательного решения для критических и административных операций.

### 10.2 JWT validation

Backend обязан:

- использовать allowlist алгоритмов и отклонять `none` и неожиданные algorithms;
- проверять подпись, `iss`, `aud`, `exp`, `nbf`, `iat`, `sub`, `sid` и допустимое clock skew;
- не выбирать алгоритм доверяя только token header;
- отклонять неизвестный `kid`;
- разделять ключи access token и любые другие криптографические назначения;
- не загружать signing key из repository или frontend bundle.

Предпочтительна асимметричная подпись `EdDSA` или `ES256`. Выбор алгоритма и key-rotation protocol фиксируются ADR.

### 10.3 TTL

- access token: 10 минут;
- refresh idle TTL: 7 дней;
- refresh absolute TTL для `CLIENT`/`PHARMACIST`: 30 дней;
- `ADMIN` absolute session TTL: 12 часов;
- freshness повторной аутентификации `ADMIN` для опасной операции: 15 минут.

Изменение этих значений требует security review и configuration validation при startup.

### 10.4 Browser storage

Refresh token передаётся только в cookie:

- `HttpOnly`;
- `Secure` в production;
- `SameSite=Strict` по умолчанию; `Lax` допускается только при обоснованном deployment flow;
- узкий `Path` для auth endpoints;
- без широкого `Domain`;
- с префиксом `__Host-`, когда deployment это позволяет.

Refresh token запрещено хранить в `localStorage`, `sessionStorage`, IndexedDB или JavaScript-readable state. Access token хранится только в памяти frontend.

### 10.5 Rotation и reuse detection

Каждый refresh атомарно:

1. проверяет пользователя и session;
2. блокирует session/token generation;
3. сравнивает token hash constant-time способом;
4. помечает generation использованной;
5. создаёт следующую generation;
6. сохраняет новый hash;
7. обновляет idle expiry;
8. создаёт security event.

Refresh token имеет не менее 256 бит энтропии. Повторное использование старой generation отзывает всю family и все связанные access sessions согласно incident policy.

### 10.6 Session record

Session хранит: ID, user ID, token family, generation, token hash, created/last-used timestamps, idle/absolute expiry, revoked timestamp/reason, authentication method, MFA level и ограниченные IP/user-agent данные.

Raw token не хранится.

### 10.7 MFA

MFA обязательно для production `ADMIN` до допуска удалённого административного доступа. Предпочтительны WebAuthn или TOTP. SMS не может быть единственным фактором.

MFA recovery codes:

- генерируются CSPRNG;
- отображаются один раз;
- хранятся как hash;
- одноразовые;
- их использование создаёт high-severity audit event.

Изменение MFA, роли, пароля, login identifier и экспорт confidential data требуют recent authentication.

### 10.8 Login protection

Login защищается rate limit по identifier, IP prefix и глобальной нагрузке, прогрессивным cooldown, единым внешним ответом, метриками и security events.

Постоянная блокировка аккаунта только из-за числа попыток запрещена.

## 11. Авторизация

Авторизация сочетает:

- RBAC;
- resource scope;
- active pharmacist assignment;
- user/session state;
- pharmacy state;
- business state;
- contextual constraints.

Минимальные роли: `CLIENT`, `PHARMACIST`, `ADMIN`. Неизвестная роль не имеет полномочий.

Role и assignment изменяются только отдельными административными use case. Поля роли, состояния, pharmacy ownership и security attributes запрещено принимать через общий profile DTO.

Для pharmacy-scoped operation backend:

1. получает actor ID из проверенного authentication context;
2. загружает актуального пользователя;
3. загружает target resource;
4. выводит pharmacy scope из ресурса;
5. проверяет active assignment и pharmacy state;
6. проверяет бизнес-состояние;
7. выполняет изменение.

Для критических mutations user, role, assignment, session и target resource повторно проверяются внутри той же транзакции после необходимых locks.

Изменение роли или назначения, происходящее конкурентно с операцией, должно приводить либо к сериализованному безопасному результату, либо к rollback. Check-then-act вне транзакции запрещён.

Ошибки сравниваются через `errors.Is()` и `errors.As()`. Централизованный HTTP error mapper не раскрывает внутренние причины, SQL text, stack trace и существование недоступного ресурса.

## 12. HTTP и ingress security

### 12.1 Transport

Production использует HTTPS. HTTP перенаправляется на HTTPS на ingress. HSTS включается после подтверждения корректности TLS deployment.

### 12.2 Reverse proxy trust

Backend доверяет `X-Forwarded-For`, `X-Forwarded-Proto`, `Forwarded` и подобным headers только от явно настроенных trusted proxy CIDR. Внешний клиент не должен иметь возможность подменить source IP или scheme.

Приложение должно корректно обрабатывать proxy chain и ограничивать длину forwarding headers.

### 12.3 CORS

- production origins задаются allowlist;
- wildcard origin запрещён для credentialed requests;
- origin отражается только после точного сравнения;
- разрешённые methods и headers минимальны;
- preflight не выполняет бизнес-операцию;
- development origins не попадают в production configuration.

### 12.4 CSRF

Refresh, logout и любые cookie-authenticated state-changing endpoints требуют CSRF-защиты. Используется проверка `Origin`/`Referer` и CSRF token либо другой утверждённый double-submit/synchronizer protocol.

`SameSite` является дополнительной защитой, но не единственной.

### 12.5 Input and output

- request body имеет per-endpoint size limit;
- JSON decoder отклоняет неизвестные поля для command DTO, trailing data и несовместимые типы;
- строки, массивы, pagination и файлы имеют пределы;
- server-generated IDs и amounts не принимаются как authoritative input;
- responses используют фиксированные DTO, а не serialization DB/domain structs;
- stack traces и internal errors не возвращаются.

### 12.6 Security headers

Frontend/ingress устанавливает:

- `Content-Security-Policy`;
- `X-Content-Type-Options: nosniff`;
- `Referrer-Policy`;
- `Permissions-Policy`;
- запрет framing через CSP `frame-ancestors`;
- cache policy для auth и confidential responses.

Sensitive responses используют `Cache-Control: no-store`.

## 13. Идемпотентность и replay protection

Idempotency key обязателен для поступления, продажи, возврата, списания, корректировки, создающего данные импорта и опасных административных команд.

Scope включает actor/client identity, pharmacy ID, operation name и key.

Backend строит canonical semantic payload и hash. Повтор с тем же key и payload возвращает исходный результат. Тот же key с другим payload отклоняется как conflict и создаёт security signal.

Claim key, бизнес-операция, result record и обязательный audit согласуются одной транзакцией. Успешный result нельзя фиксировать до commit бизнес-эффекта.

При replay backend повторно проверяет актуальное право actor получить сохранённый результат. Idempotency не обходит блокировку, отзыв assignment или изменение роли.

## 14. Конкурентность и целостность

Критические use case:

- получают locks в документированном фиксированном порядке;
- повторно проверяют состояние после lock;
- не выполняют check-then-act вне транзакции;
- используют constraints как последнюю линию защиты;
- ограниченно повторяют serialization/deadlock errors только на уровне всей идемпотентной операции;
- не доверяют cached authorization при commit.

Обязательны race tests для продаж одного остатка, блокировки пользователя/аптеки, отзыва assignment, двойного refresh, повторной команды, возврата и сторнирования.

## 15. PostgreSQL security

Используются отдельные роли:

- owner/bootstrap role;
- migration role;
- runtime role;
- backup role;
- read-only diagnostics role при необходимости.

Runtime role не создаёт schema/roles/extensions, не выполняет unrestricted DDL и не имеет `BYPASSRLS`/superuser privileges.

PostgreSQL не публикуется в интернет. Production connection использует TLS, если backend и БД не находятся в одном доверенном изолированном узле.

Все values параметризуются. Динамические identifiers допускаются только через allowlist.

Критические инварианты дублируются constraints. Runtime не получает `UPDATE`/`DELETE` для append-only audit и inventory movement records, если архитектура не требует строго ограниченной технической операции.

Прямое изменение production business data запрещено как штатная операция. Emergency SQL требует ticket, peer review, scoped script, restore point, исполнителя, причины, post-check и отдельного audit/change record.

## 16. Аудит и security logging

### 16.1 Transactional audit

Audit обязателен для authentication events, изменений пользователей/ролей/assignments, pharmacy administration, цен, проведения и сторнирования документов, импорта, модерации, публикации, sensitive reads/exports, session revocation, token reuse, idempotency conflicts и manual data changes.

Для успешной бизнес-операции audit event создаётся в той же транзакции до commit. Ошибка audit insert вызывает rollback.

Audit event содержит: ID, server time, actor type/ID, session ID, action, target, pharmacy ID, result, reason code, request ID, normalized network identifier, bounded user-agent, trace ID и безопасную metadata.

Passwords, raw tokens, cookies, Authorization header, DSN, keys и unrestricted body запрещены.

Audit, inventory movements и проведённые документы append-only. Исправление выполняется compensating operation.

### 16.2 Technical logs

Structured logs связывают request, actor, session, pharmacy, use case и результат. Пользовательские строки записываются как values с ограничением длины и control characters.

Logging middleware не записывает secret headers и bodies. Panic recovery возвращает безопасный response, сохраняет request ID и stack trace только во внутреннем log.

### 16.3 Security signals

Минимальные alerts:

- failed-login spike;
- refresh reuse;
- рост 401/403/429;
- assignment bypass attempts;
- idempotency conflicts;
- необычная ADMIN activity;
- mass export;
- audit failure;
- необычный объём корректировок;
- secret/dependency scanner finding;
- restore/backup failure.

## 17. Защита данных, backup и retention

Все внешние соединения используют TLS. Production disks и backup шифруются.

Backup защищается отдельными credentials, encryption key, access logging, retention и регулярными restore tests. Production backup запрещено использовать в development без обезличивания и разрешения владельца данных.

До production должны быть утверждены сроки хранения:

- sessions и token-family metadata;
- security logs;
- audit events;
- imports;
- backups;
- персональных данных;
- incident evidence.

Удаление пользователя не разрушает операционно значимую историю. Используются архивирование и допустимое обезличивание.

Restore считается security-sensitive operation: требует авторизации, peer approval, целевого окружения, проверки целостности backup, изоляции restore environment и post-restore credential/session review.

## 18. Secrets и криптографические ключи

Secrets не хранятся в Git, Docker image, frontend bundle, test fixtures или обычных logs.

Каждый secret имеет owner, purpose, environment, rotation procedure и revoke procedure.

JWT key rotation поддерживает overlap:

1. новый public key публикуется/загружается;
2. новые tokens подписываются новым `kid`;
3. старый public key остаётся только на период максимального TTL;
4. старый key удаляется после истечения всех tokens;
5. compromise rotation немедленно отзывает affected sessions.

Configuration startup validation должна отклонять default/empty secrets, слабые keys, небезопасные cookie settings и production debug mode.

## 19. Безопасный импорт и экспорт

Import:

- имеет allowlist типов и extensions;
- проверяет MIME независимо от имени;
- ограничивает размер, число строк/колонок и сложность;
- генерирует server-side filename;
- хранит вне web root;
- предотвращает formulas/macros/external links, если формат их допускает;
- проходит staging, validation и moderation;
- не выполняет код;
- создаёт audit trail;
- обрабатывается с ограничением CPU, memory и timeout.

Export:

- повторно проверяет permission и scope;
- ограничивает объём;
- экранирует CSV formula injection;
- не включает confidential columns без явного contract;
- для массового confidential export требует recent authentication, reason и audit;
- временные файлы имеют короткий TTL и не используют предсказуемые URLs.

## 20. Frontend security

Frontend:

- не хранит refresh token;
- не содержит secrets;
- не использует `dangerouslySetInnerHTML` без утверждённой sanitization boundary;
- очищает sensitive state при logout, session loss и privilege change;
- не показывает internal error details;
- использует dependency lockfile;
- не смешивает public и authenticated cache keys;
- прекращает stale requests при logout/role change;
- не восстанавливает confidential data из устаревшего response после очистки state.

## 21. Container, deployment и supply chain

Production container:

- запускается non-root;
- использует minimal pinned base image;
- имеет read-only filesystem, где возможно;
- получает writable volumes только для необходимых paths;
- не содержит compiler/debug tools без необходимости;
- имеет resource limits и health checks;
- не публикует PostgreSQL наружу;
- не включает `.env`, source secrets или test data.

CI обязательно выполняет:

- secret scanning;
- dependency vulnerability scanning;
- Go static analysis и tests;
- frontend lint/tests;
- container image scan;
- migration validation;
- SBOM generation для release;
- проверку provenance/signature release artifact, когда tooling введён.

Production deployment выполняется из immutable artifact, а не сборкой из произвольной рабочей копии сервера.

## 22. Secure development lifecycle

Security-critical changes требуют review минимум одного дополнительного разработчика. К ним относятся authentication, authorization, roles, assignments, sessions, cryptography, audit, idempotency, imports, exports, migrations с security semantics и deployment secrets.

Каждый pull request должен отвечать:

- какие trust boundaries затронуты;
- какие permissions изменились;
- какие negative tests добавлены;
- какие данные стали публичными/чувствительными;
- требуется ли migration, rotation или incident playbook update.

Запрещено отключать security test или scanner без documented exception.

## 23. Обязательные security tests

Минимальный набор:

- неверный/истёкший/неподписанный JWT;
- неверный `iss`, `aud`, `kid`, algorithm;
- blocked/archived user;
- revoked/expired session;
- refresh reuse и concurrent refresh;
- PHARMACIST чужой аптеки;
- revoked assignment во время операции;
- `CLIENT` и unknown role на internal endpoints;
- mass assignment role/security fields;
- IDOR enumeration;
- CSRF на cookie endpoints;
- CORS allowlist;
- spoofed proxy headers;
- oversized/malformed JSON;
- SQL injection payloads;
- XSS payloads в отображаемых данных;
- malicious import и CSV formula injection;
- idempotency same/different payload;
- concurrent sale of same stock;
- audit rollback on audit failure;
- secret redaction in logs/errors;
- stale frontend response after logout;
- backup restore access controls.

Security test должен проверять не только HTTP status, но и отсутствие бизнес-эффекта, audit outcome и неизменность данных.

## 24. Реагирование на инциденты

Минимальный process:

1. обнаружение и классификация;
2. сохранение evidence;
3. containment;
4. отзыв sessions/keys/credentials;
5. устранение причины;
6. восстановление и проверка целостности;
7. уведомление владельцев;
8. post-incident review;
9. обновление tests, controls и документации.

Должны существовать runbooks минимум для:

- token/signing-key compromise;
- database credential compromise;
- ADMIN account compromise;
- malicious dependency/release;
- audit pipeline failure;
- unauthorized data export;
- corrupted inventory data;
- backup leak.

## 25. Security control matrix

| Область | Prevent | Detect | Recover |
|---|---|---|---|
| Account takeover | Argon2id, MFA, rate limit, rotation | failed-login/reuse alerts | revoke sessions, reset credentials |
| Cross-pharmacy access | transactional assignment policy | denied-scope audit | revoke assignment/session |
| Duplicate business effect | idempotency + locks | conflict/race metrics | return original result, compensate |
| History tampering | append-only privileges/constraints | audit integrity checks | restore/compensating event |
| Secret compromise | secret storage, least privilege | secret scan/anomaly | rotate and revoke |
| Malicious release | protected CI, immutable artifact | image/SBOM scan | rollback trusted artifact |
| Data leakage | DTO allowlist, classification, export policy | export/audit monitoring | revoke access, incident response |

## 26. Security Definition of Done

Функция считается завершённой, когда:

1. actor, asset, trust boundary и abuse cases определены;
2. authentication и authorization rules реализованы backend;
3. resource scope выводится и проверяется сервером;
4. критическая проверка выполняется в транзакции;
5. mutation имеет idempotency и concurrency policy;
6. audit и log schema определены без secrets;
7. DTO используют allowlist полей;
8. data classification и cache policy определены;
9. positive, negative, replay и race tests добавлены;
10. API catalog обновлён;
11. migrations/constraints синхронизированы с Database Design;
12. configuration не содержит unsafe defaults;
13. security-sensitive change прошёл peer review;
14. rollback/recovery path определён.

## 27. Обязательные ADR до production

Нужно принять ADR для:

1. JWT algorithm, key storage и rotation;
2. access/refresh session model;
3. MFA для `ADMIN` и recovery procedure;
4. password hashing parameters;
5. audit immutability и retention;
6. trusted proxy, CORS и CSRF deployment model;
7. secret management solution;
8. production backup encryption и restore authorization;
9. security monitoring и incident severity model.

## 28. Открытые вопросы

До production необходимо утвердить:

- юридические сроки хранения audit и персональных данных;
- точный secret manager;
- TOTP или WebAuthn как первичный MFA method;
- инфраструктурную схему TLS backend → PostgreSQL;
- WAF/rate-limit implementation;
- механизм проверки compromised passwords;
- требования уведомления пользователей об инциденте;
- допустимость и правила confidential exports;
- процедуру emergency access к production.

Открытый вопрос не отменяет deny-by-default и запрет небезопасного production запуска.

## 29. Правило сопровождения

Изменение authentication, authorization, roles, assignments, sessions, cryptography, secrets, audit, imports/exports, public/private data boundary, idempotency или incident process требует обновления этого документа в том же change set.

При добавлении endpoint в `05-api-design.md` должны быть указаны authentication, required role, resource scope, idempotency requirement, sensitive fields, rate limit class и audit action.

Security review выполняется повторно перед production release и после каждого инцидента уровня `HIGH` или `CRITICAL`.
