-- Итерация закрывается сама, когда кончился её последний день
-- (ROADMAP 34.10, решение владельца 25.09.2026).
--
-- Закрывали руками, и закрывали когда придётся: отчёт «что успели»
-- считался на момент закрытия, а момент зависел от того, когда
-- кто-нибудь вспомнил. Спринт, закрытый во вторник следующей недели,
-- засчитывал себе сделанное в понедельник — уже чужое.
--
-- Закрывает проход в самом сервере, как снятие блокировок по сроку
-- (0053), и тем же приёмом: у задачи своё имя в области транзакции,
-- и политики ниже открывают ей ровно то, что она делает. Моментом
-- закрытия ставится полночь после последнего дня, а не время прохода:
-- иначе состав зависел бы от того, когда крутился фон. «Полночь» — по
-- часам базы: своего часового пояса у организации нет, и даты карточек
-- считаются так же.
--
-- Итерацию, заведённую уже после своего конца, проход не трогает: её
-- заводят задним числом, чтобы записать прошлое, и карточки кладут после
-- заведения. Закрытая полуночью, которая была до неё, она показала бы
-- пустой отчёт. Такую закрывают руками, когда состав внесён.
--
-- Старая версия приложения на этой схеме работает как прежде: имени
-- задачи она не ставит, и новые политики её не касаются; ослабленное
-- ограничение (ниже) без арендатора не открывает ей ничего, потому что
-- разрешающие политики без арендатора пусты.

-- Проход ищет незакрытые с прошедшим концом каждую минуту — по индексу,
-- а не перебором всех итераций всех организаций.
create index iterations_closing on iterations (ends_on) where closed_at is null;

-- Шаг первый — без арендатора: какие организации пора обойти. Строгое
-- ограничение ослабляется так же, как у блокировок в 0053: без
-- арендатора оно пропускает строку, а решают разрешающие политики,
-- из которых без арендатора сработает одна — узкая, ниже.
alter policy tenant_isolation on iterations
    using (org_id = coalesce((select app_current_org()), org_id));

create policy autoclose_finds on iterations for select
    using ((select app_no_actor())
           and (select app_task()) = 'close_iterations'
           and closed_at is null
           and ends_on < current_date
           and created_at < (ends_on + 1)::timestamptz);

-- Шаг второй — в каждой организации, с арендатором: закрыть итерации
-- с прошедшим концом и поднять версию их досок, чтобы открытые экраны
-- узнали сами.
create policy autoclose_sees on iterations for select
    using ((select app_task()) = 'close_iterations' and (select app_current_org()) is not null);
create policy autoclose_closes on iterations for update
    using ((select app_task()) = 'close_iterations'
           and (select app_current_org()) is not null
           and closed_at is null
           and ends_on < current_date
           and created_at < (ends_on + 1)::timestamptz)
    -- Закрывается полуночью после последнего дня — иначе это не
    -- закрытие по календарю, и задача его делать не вправе.
    with check ((select app_task()) = 'close_iterations'
                and closed_at = (ends_on + 1)::timestamptz);

create policy autoclose_sees on boards for select
    using ((select app_task()) = 'close_iterations' and (select app_current_org()) is not null);
create policy autoclose_bumps on boards for update
    using ((select app_task()) = 'close_iterations' and (select app_current_org()) is not null)
    with check ((select app_task()) = 'close_iterations');
