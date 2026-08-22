-- Ключ каталога перестаёт быть владельцем организации.
--
-- Ключу с разрешением scim:write выдавалась роль владельца — иначе
-- политика manage на teams и team_members не пускала его заводить
-- подразделения и вести их состав. Держала его в полосе /scim/v2
-- проверка в коде, а не политика: новый маршрут, обёрнутый s.authed
-- вместо s.scoped, — и ключ становится полноправным владельцем
-- арендатора. Ключ при этом лежит в чужой системе (Okta, Entra)
-- и живёт годами, а одна такая дыра уже была настоящей: владельцем
-- считался и он, отчего «последнего владельца снять нельзя» переставало
-- работать (0047).
--
-- Довод «роль нужна, чтобы заводить людей» был неверен, и это проверено
-- опытом, а не чтением: служебная личность без роли владельца видит
-- users и memberships целиком — под RLS они не попадают вовсе, — а вот
-- insert into teams получает отказ политики. Значит, роль нужна ровно
-- для двух таблиц, и вместо роли им хватит собственного права.
--
-- Право называется своим именем: не «владелец», а «каталог». Оно уже,
-- чем роль, и оно ничего не открывает за пределами двух таблиц —
-- ни досок, ни выгрузки организации, ни отзыва чужих ключей.

-- Служебная личность обязана видеть собственную строку в api_clients:
-- по ней и определяется, каталог ли это. Своей она её не видела —
-- по политике manage её показывали только владельцу, а перестав быть
-- владельцем, ключ терял из виду сам себя. Проверено опытом до правки:
-- под контекстом такой личности `select count(*) from api_clients`
-- отвечал «0».
create policy self on api_clients for select
    using (user_id = (select app_current_user()));

-- Каталог — это действующее лицо, за которым стоит живой ключ
-- с разрешением scim:write. Отозванный и просроченный ключи сюда
-- не попадают: вход их и так не пропускает, но право не должно
-- держаться на одной лишь проверке в коде — с этого и начался пункт.
create function app_is_directory() returns boolean
language sql stable
as $$
    select exists (
        select 1
          from api_clients
         where user_id = (select app_current_user())
           and org_id = (select app_current_org())
           and revoked_at is null
           and (expires_at is null or expires_at > now())
           and 'scim:write' = any (scopes)
    )
$$;

-- Две политики вместо роли. Права каталога кончаются ровно здесь:
-- подразделения и их состав.
create policy directory on teams for all
    using ((select app_is_directory()))
    with check ((select app_is_directory()));

create policy directory on team_members for all
    using ((select app_is_directory()))
    with check ((select app_is_directory()));

-- Заведённые прежде ключи каталога роль владельца уже носят, и снять её
-- некому: в интерфейсе служебной личности нет, а сама она себя понизить
-- не может. Значит, это делает миграция.
--
-- Политика на api_clients снимается на время правки: миграция идёт под
-- ролью приложения, арендатора у неё нет, и `select … from api_clients`
-- вернул бы ноль строк — правка прошла бы вхолостую и молча, ровно как
-- в 0031. memberships этого не требует: под RLS она не попадает.
alter table api_clients no force row level security;

-- Сторожится условие, а не результат: счёт «сколько строк было» идёт
-- под теми же политиками и вернёт тот же ноль, что и сама правка.
do $$
begin
    if (select relforcerowsecurity from pg_class
         where relname = 'api_clients' and relnamespace = 'public'::regnamespace) then
        raise exception 'правка пошла бы вхолостую: политики на api_clients ещё действуют';
    end if;
end $$;

update memberships m
   set role = 'viewer'
  from api_clients c
 where c.org_id = m.org_id
   and c.user_id = m.user_id
   and 'scim:write' = any (c.scopes)
   and m.role = 'owner';

alter table api_clients force row level security;
