-- Порог предупреждения об отпуске общий для доски. Пусто сохраняет
-- прежнее поведение: показывать все подходящие будущие отпуска.
-- Ноль предупреждает с первого дня отпуска; начавшийся отпуск виден
-- до последнего дня при любом пороге. Старый бинарник поле не читает.
alter table boards
    add column vacation_warning_days integer
        constraint boards_vacation_warning_days_range
        check (vacation_warning_days is null or vacation_warning_days between 0 and 365);
