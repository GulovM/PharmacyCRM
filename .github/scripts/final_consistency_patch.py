from pathlib import Path
import re
D=Path('docs')
def load(n): return (D/n).read_text(encoding='utf-8')
def save(n,t): (D/n).write_text(t.rstrip()+'\n',encoding='utf-8')
def section(t,start,nxt,repl):
 p=re.compile(rf'^{re.escape(start)}\n.*?(?=^{re.escape(nxt)}\n)',re.M|re.S)
 if not p.search(t): raise RuntimeError(start)
 return p.sub(repl.rstrip()+'\n\n',t,1)

n='04-architecture.md'; t=load(n)
t=section(t,'## 11. Продажи и возвраты','## 12. Аутентификация и авторизация',r'''## 11. Продажи и возвраты

### 11.1 Продажа

Use case проведения продажи внутри одной transaction соблюдает общий protocol из раздела 9:

1. claim/lock idempotency record;
2. revalidate current user, session, role, pharmacy assignment и pharmacy;
3. lock affected `pharmacy_products` по `id` и повторно проверить assortment, unit policy и server prices;
4. lock eligible lots по `expiration_date`, `received_at`, `id`;
5. повторно проверить sellability и quantity;
6. выполнить server-side FEFO allocation и totals;
7. создать sale, items, snapshots, allocations, operation, movements и обновить lot balances;
8. записать mandatory audit и outbox events;
9. complete idempotency result;
10. commit до возврата success.

Недостаток любой строки отклоняет всю продажу. Frontend price, total и lot selection не являются authoritative.

### 11.2 Возврат

`ReturnAction`: `RESTOCK`, `WRITE_OFF`, `QUARANTINE`, `NO_PHYSICAL_RETURN`. Sale status: `COMPLETED`, `PARTIALLY_REFUNDED`, `REFUNDED`, `REVERSED`.

Gate E0 устанавливает консервативную legal policy: customer-return mutation production-disabled по умолчанию; до передачи товара применяется sale void/reversal. После передачи legally approved exception может создать refund, но physical goods получают `QUARANTINE`, `WRITE_OFF` или `NO_PHYSICAL_RETURN`; `RESTOCK` для customer-returned medicines запрещён.

Разрешённый return use case сначала claim-ит idempotency, затем revalidate-ит authorization, после чего блокирует source sale, sale items, source allocations, `pharmacy_products` и lots в каноническом порядке. Cumulative completed non-reversed returned quantity не превышает sold allocation. Return document, sale status, refund/inventory effect, audit, outbox и idempotency result commit-ятся атомарно.''')
save(n,t)

n='06-database-design.md'; t=load(n)
t=t.replace('Возвратная mutation остаётся выключенной в production до утверждения юридической policy и refund/rounding rules.','Customer-return mutation production-disabled утверждённым Gate E0 baseline; partial refund path дополнительно остаётся disabled до утверждения allocation/rounding rules.')
t=t.replace('Return command production-disabled до legal и refund/rounding approval.','Customer-return command production-disabled утверждённым legal baseline; partial refund execution дополнительно требует утверждённых allocation/rounding rules.')
save(n,t)

n='07-domain-model.md'; t=load(n)
t=t.replace('13. Production command disabled до утверждения legal policy.','13. Customer-return command production-disabled утверждённым Gate E0 legal baseline; partial refund path требует утверждённой rounding policy.')
t=t.replace('1. Validate command and legal feature availability.','1. Validate command, approved return mode и refund/rounding feature availability.')
save(n,t)

n='08-project-structure.md'; t=load(n)
t=t.replace('Предпочтительно, чтобы component Makefile/script скрывал выбранный package manager; root Makefile не должен жёстко предполагать npm до принятия решения.','Component Makefile/script вызывает утверждённый `pnpm` через Corepack; root Makefile не допускает параллельный npm/yarn workflow.')
save(n,t)

n='09-security-design.md'; t=load(n)
t=t.replace('Предпочтительна асимметричная подпись `EdDSA` или `ES256`. Выбор алгоритма и key-rotation protocol фиксируются ADR.','Нормативный algorithm — `EdDSA`/Ed25519; key rotation выполняется каждые 90 дней с verification overlap не менее 20 минут. Альтернатива требует нового ADR и синхронного изменения API, deployment и tests.')
t=t.replace('Минимальные роли: `CLIENT`, `PHARMACIST`, `ADMIN`. Неизвестная роль не имеет полномочий.\nМинимальные роли: `CLIENT`, `PHARMACIST`, `ADMIN`. Неизвестная роль не имеет полномочий.','Минимальные роли: `CLIENT`, `PHARMACIST`, `ADMIN`. Неизвестная роль не имеет полномочий.')
t=t.replace('Для критических mutations user, role, assignment, session и target resource повторно проверяются внутри той же транзакции после необходимых locks.','Для critical mutation первым сериализующим lock является idempotency record; затем внутри transaction повторно читаются user, session, role, assignment и pharmacy; только после authorization revalidation берутся business locks в каноническом порядке.')
t=t.replace('Scope включает actor/client identity, pharmacy ID, operation name и key.','Полная idempotency identity: `actor + operation + effective_scope + key`; `effective_scope = pharmacy_id` для pharmacy command и `GLOBAL` для global/admin command.')
save(n,t)

n='13-testing-strategy.md'; t=load(n)
t=t.replace('<!-- consistency-regression:start -->\n','').replace('\n<!-- consistency-regression:end -->','')
save(n,t)

active=['04-architecture.md','04-01-backend-architecture.md','05-api-design.md','06-database-design.md','07-domain-model.md','08-project-structure.md','09-security-design.md','10-sequence-diagrams.md','11-development-roadmap.md','12-deployment.md','13-testing-strategy.md','14-observability.md']
a='\n'.join(load(x) for x in active)
for bad in [
'1. повторно проверить пользователя и его назначение аптеке;\n2. проверить или создать запись идемпотентности;',
'до утверждения юридической policy','Production command disabled до утверждения legal policy',
'до принятия решения','Предпочтительна асимметричная подпись','Scope включает actor/client identity, pharmacy ID',
'<!-- consistency-regression:', 'auth_version','auth version','version-counter авторизации']:
 if bad.lower() in a.lower(): raise RuntimeError('stale: '+bad)
for req in ['claim/lock idempotency record;\n2. revalidate current user, session, role, pharmacy assignment и pharmacy;',
'Customer-return mutation production-disabled утверждённым Gate E0 baseline',
'Нормативный algorithm — `EdDSA`/Ed25519','`effective_scope = pharmacy_id`','root Makefile не допускает параллельный npm/yarn workflow']:
 if req not in a: raise RuntimeError('missing: '+req)
print('OK')
