-- Автоматическое заведение людей: чем провайдер узнаёт своих.
--
-- Провайдер (Entra ID, Okta, Keycloak) держит у себя собственный
-- идентификатор сотрудника и присылает его как externalId. Если его
-- не хранить, всё держится на почте — а почта меняется, и после смены
-- провайдер решит, что это новый человек, и заведёт второго.
--
-- Идентификатор кладётся к участию, а не к человеку: один и тот же
-- сотрудник может быть заведён двумя разными провайдерами в двух
-- организациях, и их номера не обязаны совпадать.

alter table memberships
    add column external_id text;

create unique index memberships_external_id
    on memberships (org_id, external_id)
    where external_id is not null;

comment on column memberships.external_id is
    'Идентификатор сотрудника у провайдера, приславшего его через SCIM. Пусто у заведённых вручную.';

-- Команды тоже приезжают из провайдера — группами. Тот же довод.
alter table teams
    add column external_id text;

create unique index teams_external_id
    on teams (org_id, external_id)
    where external_id is not null;
