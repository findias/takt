-- Безопасный повтор для внешних вызовов.
--
-- Внутри доски повтор уже безопасен: операция несёт operationId, и второй
-- раз она не выполняется. Но операции — не весь API, а интеграция обязана
-- уметь повторять запрос, не спрашивая разрешения: ответ теряется в сети
-- регулярно, и «попробую ещё раз» не должно означать «заведу вторую доску».
--
-- Поэтому изменяющий вызов принимает заголовок Idempotency-Key. Первый
-- запрос с таким ключом выполняется и его ответ запоминается; повтор
-- получает тот же ответ, не трогая данные.
--
-- Запоминается не только тело, но и метод с путём. Ключ, предъявленный
-- к другому вызову, — это ошибка клиента, а не просьба вернуть прошлое:
-- молча отдать ответ от другого запроса значило бы соврать.
--
-- Записи живут недолго. Смысл ключа — пережить обрыв связи и повтор
-- через минуту, а не хранить историю: недельной давности ключ повторять
-- уже некому.

create table api_idempotency (
    org_id     uuid not null references orgs (id) on delete cascade,
    key        text not null,
    method     text not null,
    path       text not null,
    status     int  not null,
    -- Тело хранится текстом, а не jsonb: повтор обязан вернуть ровно то,
    -- что вернул первый вызов, а jsonb переписывает пробелы и порядок
    -- ключей. Заглядывать внутрь этого тела мы всё равно не собираемся —
    -- для базы это непрозрачный ответ, а не данные.
    body       text,
    created_at timestamptz not null default now(),

    primary key (org_id, key)
);

create index api_idempotency_created_idx on api_idempotency (created_at);

alter table api_idempotency enable row level security;
alter table api_idempotency force  row level security;

create policy tenant_isolation on api_idempotency as restrictive
    using (org_id = (select app_current_org()))
    with check (org_id = coalesce((select app_current_org()), org_id));

-- Свой ключ читает и пишет тот, кто может писать в организации. Чужие
-- ключи не видны — как и всё остальное за границей арендатора.
create policy writable on api_idempotency for all
    using ((select app_can_write())) with check ((select app_can_write()));
