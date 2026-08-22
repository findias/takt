-- Живой узел не висит под убранным.
--
-- Правило есть, но охраняло оно одну дверь из трёх. Архивация
-- не пускает убрать узел с живыми потомками (0045). Возврат из архива
-- не пускает вернуть узел под архивированного старшего — это проверял
-- код. А заведение нового узла под архивированным старшим проходило:
-- прогон 22 августа 2026 отвечал 201, и в дереве оказывался узел,
-- у которого есть `parent_id`, а старшего в дереве нет.
--
-- Это четвёртый за день случай одной и той же формы: правило записано
-- там, где о нём думали, и не записано там, где не думали. Поэтому оно
-- переезжает сюда целиком и формулируется не по действию, а по итогу:
-- если после правки узел живой, его старший обязан быть живым тоже.
-- Такая формулировка закрывает все три двери сразу — и заведение,
-- и перенос, и возврат из архива, — а заодно ту четвёртую, которую
-- никто ещё не придумал.
--
-- Проверка кода в `team.Restore` после этого убрана: правило, лежащее
-- в двух местах, однажды оказывается выполненным в одном.

create function teams_live_parent() returns trigger
language plpgsql
as $$
declare
    parent_name text;
begin
    if new.archived_at is not null or new.parent_id is null then
        return new;
    end if;

    select name into parent_name
      from teams
     where id = new.parent_id and archived_at is not null;

    if parent_name is not null then
        raise exception
            'сначала верните из архива подразделение «%»: внутри него это и лежит',
            parent_name
            using errcode = 'check_violation';
    end if;

    return new;
end;
$$;

create trigger teams_live_parent_check
    before insert or update of parent_id, archived_at on teams
    for each row execute function teams_live_parent();
