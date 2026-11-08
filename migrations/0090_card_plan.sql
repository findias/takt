-- План — календарные дни, отдельно от обязательства и фактов потока.
-- NULL сохраняет прежнее поведение карточек и старого приложения.
alter table cards
    add column planned_start_on date,
    add column planned_end_on date,
    add constraint cards_plan_order check (
        planned_start_on is null or planned_end_on is null
        or planned_start_on <= planned_end_on);
