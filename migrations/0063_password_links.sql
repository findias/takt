-- Ссылка «задать пароль» (ROADMAP 23.6).
--
-- Писем takt не шлёт, поэтому ни сброса пароля по почте, ни письма
-- «вас завели» нет. Вместо них владелец выпускает одноразовую ссылку
-- и передаёт её сам — так же, как приглашение. Нужна она двоим: тому,
-- кого завёл перенос (пароля у него нет вовсе), и тому, кто пароль
-- забыл.
--
-- Таблица личности, как sessions, и под RLS не попадает: ссылку
-- открывают по токену до входа, когда организация ещё неизвестна.
-- Строка всегда ограничена одним человеком, и границу держит код
-- (store/identity_boundary_test.go, store/isolation_test.go).
-- В базе лежит только хеш токена: утечка таблицы не даёт войти.
create table password_links (
    token_hash text        primary key,
    user_id    uuid        not null references users (id) on delete cascade,
    -- Где выпущена: туда пишется журнал, и её название видит человек.
    org_id     uuid        not null references orgs (id) on delete cascade,
    created_by uuid        references users (id) on delete set null,
    expires_at timestamptz not null,
    used_at    timestamptz,
    created_at timestamptz not null default now()
);
create index password_links_user_idx on password_links (user_id) where used_at is null;

-- Пароль ещё не задан: учётную запись завёл перенос, а человек ни разу
-- не входил. «Команда» показывает это рядом с именем — иначе не видно,
-- кому ещё надо передать ссылку. Умолчание false: всё заведённое раньше
-- и всё, что заводит старая версия, входит как прежде.
alter table users add column awaiting_password boolean not null default false;
