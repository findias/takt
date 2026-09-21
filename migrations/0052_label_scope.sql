-- Метка принадлежит организации, подразделению или доске.
--
-- До этой миграции метка была только организационной: словарь один
-- на всех, и «ждём юристов» с доски договоров предлагался на доске
-- инцидентов. Чем больше досок, тем длиннее меню «повесить метку»
-- и тем больше в нём чужого. Подразделению нужен свой словарь, который
-- действует на всех его досках и ниже по дереву, доске — свой,
-- который не виден больше нигде.
--
-- Область задаётся двумя ссылками, а не видом и идентификатором:
-- так на каждую есть внешний ключ, и метка не может пережить доску,
-- о которой говорит. Обе пустые — метка организации, и именно такие
-- метки заводит предыдущая версия приложения, поэтому в окне выкладки
-- она продолжает работать как работала.

alter table labels
    add column team_id  uuid references teams (id),
    add column board_id uuid references boards (id) on delete cascade,
    add constraint labels_one_scope check (team_id is null or board_id is null);

-- Путь области от корня дерева: у организации пустой, у подразделения —
-- его путь, у доски — путь её подразделения и сама доска в конце.
-- Метка действует там, где её путь — начало пути доски. Тем же
-- сравнением решается, пересекаются ли две области: пересекаются,
-- если один путь — начало другого.
--
-- Путь считается, а не хранится: подразделения переносят и доски
-- передают другому подразделению, и сохранённый путь пришлось бы
-- догонять триггерами в трёх таблицах. Меток на организацию десятки,
-- считать дешевле, чем синхронизировать.
create function label_path(scope_team uuid, scope_board uuid) returns uuid[]
language sql stable parallel safe
as $$
    select case
        when scope_board is not null then
            coalesce((select t.ancestor_ids
                        from boards b join teams t on t.id = b.team_id
                       where b.id = scope_board), '{}') || scope_board
        when scope_team is not null then
            (select ancestor_ids from teams where id = scope_team)
        else '{}'::uuid[]
    end
$$;

-- Начало ли один путь другого. Пустой — начало любого: метку
-- организации видно отовсюду.
create function label_path_prefix(head uuid[], path uuid[]) returns boolean
language sql immutable parallel safe
as $$ select head = path[1:cardinality(head)] $$;

-- Одинаковые названия запрещаются в пределах одной области. Две области,
-- вложенные одна в другую, база не сторожит — это делает приложение,
-- называя в отказе, где метка уже есть: уникальный индекс так сказать
-- не умеет. Имя индекса прежнее: по нему предыдущая версия узнаёт
-- «такая метка уже есть» и отвечает внятно, а не сбоем.
--
-- Третий член — организация, когда область пустая: без него две
-- организационные «Срочно» различались бы по NULL, а NULL в уникальном
-- индексе с другим NULL не совпадает.
drop index labels_name_idx;
create unique index labels_name_idx
    on labels (org_id, coalesce(board_id, team_id, org_id), lower(name))
    where archived_at is null;

create index labels_team_idx  on labels (team_id)  where team_id is not null;
create index labels_board_idx on labels (board_id) where board_id is not null;

-- --- Доступ ---

-- Метку доски видит тот, кто видит доску. Название само бывает
-- сведением: «увольнение», повешенное на доске найма, не должно
-- читаться из словаря людьми, которым сама доска закрыта. Метки
-- организации и подразделений по-прежнему словарь для всех: состав
-- подразделений не секрет, и по нему же решается, что предлагать.
alter policy visible on labels
    using (board_id is null or board_id = any (array(select unnest(app_visible_boards()))));

-- Заводит и убирает метку тот, кто отвечает за её область: метку
-- организации — всякий, кто пишет (как и прежде); подразделения — его
-- участники, администраторы и владелец организации; доски — тот,
-- кто в неё пишет.
alter policy manage on labels
    using ((select app_can_write()) and (
            (team_id is null and board_id is null)
         or (team_id is not null and (
                (select app_is_owner())
             or team_id = any (array(select unnest(app_member_teams())))
             or team_id = any (array(select unnest(app_admin_teams())))))
         or (board_id is not null and board_id = any (array(select unnest(app_writable_boards()))))))
    with check ((select app_can_write()) and (
            (team_id is null and board_id is null)
         or (team_id is not null and (
                (select app_is_owner())
             or team_id = any (array(select unnest(app_member_teams())))
             or team_id = any (array(select unnest(app_admin_teams())))))
         or (board_id is not null and board_id = any (array(select unnest(app_writable_boards()))))));
