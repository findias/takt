-- Метка человека из источника переноса (ROADMAP 23.6).
--
-- Исполнитель чужой доски, которого не нашли среди участников, не должен
-- пропадать с карточек: на все его карточки вешается метка с его именем.
-- Метка техническая — её рисуют контуром и не предлагают в выборе метки,
-- — поэтому у метки появляется вид. А чтобы снять её, когда человека
-- позже сопоставят, метка помнит, кого она заменяет: почту или, если
-- почты источник не дал, имя.
--
-- Умолчание 'regular': старая версия приложения заводит метки как прежде
-- и о новых колонках не знает.
alter table labels
    add column kind text not null default 'regular'
        constraint labels_kind_valid check (kind in ('regular', 'person')),
    add column source_person text,
    add constraint labels_person_has_source
        check ((kind = 'person') = (source_person is not null));

-- Метка человека и обычная метка с тем же названием друг другу
-- не мешают: «Иван Петров» из YouGile и метка «Иван Петров», заведённая
-- руками, — разные вещи, и перенос не должен отказывать из-за второй.
drop index labels_name_idx;
create unique index labels_name_idx
    on labels (org_id, coalesce(board_id, team_id, org_id), kind, lower(name))
    where archived_at is null;

-- Одна метка на человека на доске.
create unique index labels_person_idx
    on labels (board_id, source_person)
    where kind = 'person' and archived_at is null;
