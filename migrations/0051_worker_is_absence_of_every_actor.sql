-- Работник — это отсутствие всякого действующего лица, а не только
-- отсутствие арендатора.
--
-- Политики фоновых задач — вебхуки, уборка, отключение подписки —
-- узнавали работника по одному признаку: `app_current_org() is null`.
-- Признак неверный. Арендатора нет ещё у двух областей, и обе
-- предъявляются снаружи:
--
--   * приём приглашения — организация неизвестна, у человека может
--     не быть учётной записи, право даёт секрет из ссылки;
--   * обмен по ключу — организация неизвестна до того, как ключ найден.
--
-- Политики строк складываются по ИЛИ. Поэтому обладателю ссылки-
-- приглашения — то есть любому, кого хоть раз позвали хоть куда, —
-- политики работника открывали таблицы целиком, поверх всех организаций.
-- Замер 26.08.2026 на стенде: по одному токену приглашения видно
-- 145 строк `webhooks` и 151 строку `webhook_deliveries`. В `webhooks`
-- есть колонка `secret`, и лежит она открытым текстом: этим секретом
-- подписываются доставки, то есть чужую доставку можно было подделать.
--
-- Миграция 0043 этот случай назвала вслух — «областью без арендатора
-- ходят и обмен по ключу, и приём приглашения, и работник вебхуков, —
-- им приглашения открывать незачем», — и всё-таки оставила проверку
-- на одном арендаторе. Довод в комментарии условия не заменяет.
--
-- Со старой версией приложения совместимо: работник ходит
-- `store.Scope{}`, где пусты все четыре настройки, и под новое условие
-- попадает так же, как под старое. Ужимается ровно то, что и должно, —
-- области, предъявившие секрет.

-- Одно условие в одном месте: повторённое пять раз, оно и разойдётся
-- пять раз. Функция stable и не читает таблиц, поэтому планировщик
-- разворачивает её так же, как разворачивал app_current_org().
create function app_no_actor() returns boolean
language sql stable
as $$
    select nullif(current_setting('app.current_org', true), '') is null
       and nullif(current_setting('app.invite_token', true), '') is null
       and nullif(current_setting('app.api_token', true), '') is null
$$;

-- Условия остальных частей политик повторены дословно: меняется
-- только признак работника. Политика `retry` на доставках в список
-- не входит — она про владельца (`app_is_owner()`), а не про работника.

-- Вебхуки.
alter policy worker_reads_hooks on webhooks
    using ((select app_no_actor()));
alter policy worker_switches_off on webhooks
    using ((select app_no_actor()))
    with check ((select app_no_actor()) and disabled_at is not null);

alter policy worker_reads on webhook_deliveries
    using ((select app_no_actor()));
alter policy worker on webhook_deliveries
    using ((select app_no_actor()))
    with check ((select app_no_actor()));
alter policy cleanup on webhook_deliveries
    using ((select app_no_actor()));

-- Уборка журнала действий: срок у каждой организации свой.
alter policy cleanup on audit_events
    using ((select app_no_actor()));
alter policy cleanup_reads on audit_events
    using (
        (select app_no_actor())
        and at < now() - make_interval(days => (
            select o.audit_retention_days from orgs o where o.id = audit_events.org_id))
    );

-- Ключи повторной отправки живут сутки.
alter policy cleanup on api_idempotency
    using ((select app_no_actor()));
alter policy cleanup_reads on api_idempotency
    using (
        (select app_no_actor())
        and created_at < now() - interval '24 hours'
    );

-- Приглашения: с них разбор и начался.
alter policy cleanup on invites
    using (
        (select app_no_actor())
        and coalesce(accepted_at, revoked_at, expires_at) < now() - interval '30 days'
    );
alter policy cleanup_reads on invites
    using (
        (select app_no_actor())
        and coalesce(accepted_at, revoked_at, expires_at) < now() - interval '30 days'
    );
