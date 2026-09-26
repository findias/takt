import {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { monitorForElements } from '@atlaskit/pragmatic-drag-and-drop/element/adapter'
import { autoScrollForElements } from '@atlaskit/pragmatic-drag-and-drop-auto-scroll/element'
import { extractClosestEdge } from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import { agingLabel, flowIssues, withoutParts } from '../entities/board/model.ts'
import { AttentionRail } from '../features/board/AttentionRail.tsx'
import type { AttentionItem } from '../features/board/AttentionRail.tsx'
import { api } from '../shared/api/index.ts'
import { useDocumentTitle } from '../shared/lib/useDocumentTitle.ts'
import type {
  BoardAccess as Access,
  BoardInfo,
  Column,
  EstimateUnit,
  Iteration,
  Priority,
} from '../shared/api/index.ts'
import { BoardSkeleton, EmptyState, ErrorState, Skeleton } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'
import { CardSearch, FilterBar } from '../features/board/FilterBar.tsx'
import { EMPTY, NO_ITERATION, filtersToQuery, isEmpty, matches, parseFilters } from '../features/board/filters.ts'
import type { Filters } from '../features/board/filters.ts'
import { withViewTransition } from '../shared/lib/withViewTransition.ts'
import { boardPath, navigate, setQuery, useQuery } from '../shared/router/index.ts'
import { Views } from '../features/board/Views.tsx'
import { SORT_NAMES, parseSort, sortToQuery } from '../features/board/tableSort.ts'
import { Workload } from '../features/board/Workload.tsx'
import type { Sort } from '../features/board/tableSort.ts'
import { Palette, paletteHint, usePaletteHotkey } from '../features/board/Palette.tsx'
import type { Command } from '../features/board/Palette.tsx'
import { useCollapsedColumns } from '../features/board/useCollapsed.ts'
import { useColumnWidths } from '../features/board/columnWidth.ts'
import { nextCard } from '../features/board/navigation.ts'
import { cardDetails, childrenOf, dateWords, dependenciesOf, parentsOf } from '../entities/card/model.ts'
import { NARROW, useMedia } from '../shared/lib/useMedia.ts'
import {
  GROUPING_NAMES,
  groupingToQuery,
  groupsOf,
  byTree,
  parseGrouping,
} from '../features/board/grouping.ts'
import type { Group, Grouping } from '../features/board/grouping.ts'
import { useToast } from '../shared/ui/Toast.tsx'
import {
  ArchiveIcon,
  ChevronLeftIcon,
  CloseIcon,
  FlowIcon,
  OpenIcon,
  PeopleIcon,
  SearchIcon,
  TagIcon,
} from '../shared/ui/icons.tsx'
import { visibilityLabel } from '../features/access/visibility.ts'
import { ColumnView } from '../features/board/ColumnView.tsx'
import { useBoard } from '../features/board/useBoard.ts'
import { SandboxNote } from '../features/demo/SandboxNote.tsx'
import { locale, t, withSections } from '../shared/i18n/index.ts'
import { useHelpTopic } from '../shared/lib/help.ts'
import { HelpButton } from '../shared/ui/HelpButton.tsx'
import { Hint } from '../shared/ui/Hint.tsx'
import { Bell } from '../features/notifications/Bell.tsx'

// Вторичные экраны доски едут отдельными кусками — по тому же доводу,
// по которому вынесены экраны организации: доска открывается всегда,
// а таблица, поток, отчёт по итерации, архив и лента изменений —
// по требованию. Класть их во входной кусок значит заставлять всех
// платить временем открытия за то, чем пользуются иногда; порог
// размера сборки на этом и упёрся.
/**
 * Кусок вида запрашивается сразу, а не после того, как приехали данные.
 *
 * Замер 23.08.2026 по адресу `?view=table`: приложение на экране к 73 мс,
 * данные доски к 145, а таблица — только к 516. Между ними человек
 * триста миллисекунд смотрел в пустой экран, потом на сто миллисекунд
 * мелькал скелетон — ровно то мигание, ради которого у него и стоит
 * задержка в двести. Причина: `lazy` начинает загрузку в тот момент,
 * когда доходит до отрисовки, то есть после данных, — две ожидания
 * выстраивались в очередь вместо того, чтобы идти рядом.
 */
const loadTableView = () => import('../features/board/TableView.tsx')
const TableView = lazy(() => loadTableView().then((m) => ({ default: m.TableView })))
// Панель карточки — своим куском (этап 33.3): первая загрузка доски
// упёрлась в порог размера (web/e2e/perf.spec.ts), а панель нужна только
// открытой карточке. Загрузка начинается, как только доска нарисована,
// поэтому к первому нажатию на карточку кусок обычно уже на месте.
const loadCardPanel = () => import('../features/board/CardPanel.tsx')
const CardPanel = lazy(() => loadCardPanel().then((m) => ({ default: m.CardPanel })))
const Flow = lazy(
  withSections(
    () => import('../features/flow/Flow.tsx').then((m) => ({ default: m.Flow })),
    'flow',
    'flowReport',
  ),
)
// Дерево работы — вид для тех, кому нужна иерархия (этап 32.6); с доской
// он не грузится, и подписи едут вместе с ним.
const TreeView = lazy(
  withSections(() => import('../features/board/TreeView.tsx').then((m) => ({ default: m.TreeView })), 'tree'),
)
// Полоса действий над выделенными нужна, только когда что-то выделено.
const BulkBar = lazy(() => import('../features/board/BulkBar.tsx').then((m) => ({ default: m.BulkBar })))
const Iterations = lazy(() =>
  import('../features/board/Iterations.tsx').then((m) => ({ default: m.Iterations })),
)
const Changes = lazy(() =>
  import('../features/board/Changes.tsx').then((m) => ({ default: m.Changes })),
)
const CardArchive = lazy(() =>
  import('../features/board/CardArchive.tsx').then((m) => ({ default: m.CardArchive })),
)
// Панель доступа открывают редко и нажатием — грузить её с доской
// значит платить за неё при каждом открытии доски (П7).
const AccessPanel = lazy(() =>
  import('../features/access/AccessPanel.tsx').then((m) => ({ default: m.AccessPanel })),
)
const IterationReport = lazy(
  withSections(
    () =>
      import('../features/board/IterationReport.tsx').then((m) => ({ default: m.IterationReport })),
    'flowReport',
  ),
)

export function Board({
  boardId,
  cardId,
  onCard,
  unit,
  meId,
  isOwner,
  canEdit,
  sandboxExpiresAt,
  account,
  onBack,
  panelOnly = false,
  onChanged,
}: {
  boardId: string
  /** Какая карточка открыта — приходит из адреса, а не хранится здесь:
   *  ссылку на карточку должно быть можно прислать. */
  cardId: string | null
  /** Может ли смотрящий менять доску. Наблюдателю действий
   *  не показываем вовсе: сервер их всё равно отвергает, а кнопка,
   *  ведущая к отказу, — обещание, которого интерфейс не держит.
   *  Проход по интерфейсу упёрся ровно в это: форма открывалась,
   *  текст печатался, и только Enter приносил «у вас доступ только
   *  на чтение». */
  canEdit: boolean
  onCard: (cardId: string | null) => void
  unit: EstimateUnit
  meId: string
  /** Удалять насовсем может только владелец организации: действие
   *  необратимо, и одного «администратора» для него мало. */
  isOwner: boolean
  /** Песочница публичного демо: когда исчезнет. */
  sandboxExpiresAt?: string
  /** Имя смотрящего с личными настройками. Приходит готовым от
   *  приложения: выйти из сессии умеет только оно, а доске знать
   *  об этом незачем. */
  account?: React.ReactNode
  onBack: () => void
  /** Только панель карточки, без самой доски: так задачу открывают
   *  с экрана «Задачи», не уходя с него (владелец 25.09.2026). Вся
   *  логика доски — правки, вопросы, отмена — та же самая, потому что
   *  это та же доска; не рисуется лишь поле. */
  panelOnly?: boolean
  /** Доска подтвердила правку (выросла её версия): экран, который
   *  показывает эту карточку по-своему, перечитывает себя. */
  onChanged?: () => void
}) {
  const notify = useToast()
  const board = useBoard(boardId, notify)
  const [announcement, setAnnouncement] = useState('')

  // Объявление ставится с задержкой около секунды: смена фокуса, которая
  // неизбежно следует за перемещением, иначе перебивает его, и скринридер
  // читает пустоту. Так это решено в live-region у Atlassian, и по той же
  // причине здесь role="status", а не alert — alert читается ненадёжно.
  const announce = useCallback((text: string) => {
    setAnnouncement('')
    window.setTimeout(() => setAnnouncement(text), 1000)
  }, [])

  // Узел карточки перемонтируется в новой колонке, и фокус улетает
  // в body. Возвращаем его руками — иначе человек, работающий
  // с клавиатуры, теряет место после каждого переноса.
  // Горизонтальная автопрокрутка живёт на контейнере колонок: там,
  // где есть горизонтальная прокрутка, там и подвозить.
  const columnsRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const element = columnsRef.current
    if (!element) return
    return autoScrollForElements({ element })
  }, [])

  const flash = useCallback((cardId: string) => {
    setJustMoved(cardId)
    window.setTimeout(() => setJustMoved((current) => (current === cardId ? null : current)), 600)
  }, [])

  const refocus = useCallback((cardId: string) => {
    window.setTimeout(() => {
      document.querySelector<HTMLElement>(`[data-card="${cardId}"]`)?.focus()
    }, 50)
  }, [])
  const openCard = cardId
  const setOpenCard = onCard
  const [showFlow, setShowFlow] = useState(false)
  const [showAccess, setShowAccess] = useState(false)
  // Архив карточек. До него убранную карточку можно было вернуть только
  // из всплывающего уведомления — исчезло оно, и карточка недостижима.
  const [showArchive, setShowArchive] = useState(false)
  const { collapsed, toggle: toggleColumn } = useCollapsedColumns(boardId)
  const { widths: columnWidths, setWidth: resizeColumn } = useColumnWidths(boardId)
  const [palette, setPalette] = useState(false)
  // На узком экране колонки не помещаются рядом, и горизонтальная
  // прокрутка доски превращает работу в поиск: показываем одну колонку
  // и переключатель. Это разный состав разметки, а не разное
  // оформление, — CSS такого не умеет.
  const narrow = useMedia(NARROW)
  const [visibleColumn, setVisibleColumn] = useState<string | null>(null)
  // Карточка, которую только что перенесли: она вспыхивает, чтобы глаз
  // нашёл её на новом месте. Живёт полсекунды — это подсказка, а не
  // состояние доски.
  const [justMoved, setJustMoved] = useState<string | null>(null)
  usePaletteHotkey(useCallback(() => setPalette(true), []))
  // Видимость доски показывается в шапке: «доску видят не те» — это то,
  // что замечают, глядя на доску, а не на её строку в списке.
  const [access, setAccess] = useState<Access | null>(null)

  const loadAccess = useCallback(() => {
    api
      .boardAccess(boardId)
      .then(setAccess)
      // Молчаливый отказ намеренный: не сумели прочитать — подпись просто
      // не появится, а работать доске это не мешает.
      .catch(() => setAccess(null))
  }, [boardId])

  useEffect(loadAccess, [loadAccess])

  // Фильтры живут в адресе: отфильтрованный вид можно прислать ссылкой,
  // и он переживает перезагрузку.
  const query = useQuery()
  const filters = useMemo(() => parseFilters(query), [query])
  // Вид и сортировка живут в адресе рядом с фильтрами: отсортированный
  // список присылают ссылкой так же, как отфильтрованную доску.
  const asked = query.get('view')
  const view = asked === 'table' || asked === 'changes' || asked === 'tree' ? asked : 'board'
  const asTable = view === 'table'
  // Кусок таблицы едет рядом с данными доски, а не за ними: см. довод
  // у `loadTableView`. Эффект, а не вызов в теле, — загрузка не должна
  // случаться при отрисовке, у которой могут быть свои причины
  // повториться.
  useEffect(() => {
    if (asTable) void loadTableView()
  }, [asTable])
  const sort = useMemo(() => parseSort(query), [query])
  const setFilters = useCallback(
    (next: Filters) => setQuery(filtersToQuery(next, query), { replace: true }),
    [query],
  )
  // Отбор по эпику — с метки на карточке (этап 33.3). Через ссылку
  // на отбор, а не замыканием на него: иначе обработчик менялся бы
  // с каждым отбором и перерисовывал бы все карточки доски.
  const filtersNow = useRef(filters)
  filtersNow.current = filters
  const setFiltersNow = useRef(setFilters)
  setFiltersNow.current = setFilters
  const filterByEpic = useCallback((epicId: string) => {
    const f = filtersNow.current
    setFiltersNow.current({ ...f, epic: f.epic === epicId ? null : epicId })
  }, [])

  const { base, order: fullOrder, moveCard: moveCardNow } = board

  // Карточка с незакрытыми подзадачами уходит в «Готово» только спросив
  // (решение владельца 25.09.2026, этап 34, пункт 11). Правило «обратимое
  // не спрашивает» здесь нарушено сознательно: перенос обратим, но эпик,
  // закрытый при открытых задачах, говорит неправду о состоянии работы,
  // а замечают это поздно. Спрашивает, а не запрещает: хвост бывает
  // брошен намеренно. Доска читается через ref, чтобы обёртки не меняли
  // тождество с каждым снимком и не перерисовывали колонки.
  const baseForClose = useRef(base)
  baseForClose.current = base
  const [closing, setClosing] = useState<Closing | null>(null)
  const openPartsOf = (cardId: string): ClosingItem | null => {
    const current = baseForClose.current
    const d = current ? cardDetails(current, cardId) : null
    if (!d) return null
    const open = d.subtasks.filter((s) => !s.done)
    return open.length > 0
      ? { title: d.card.title, open: open.map((s) => s.title), total: d.subtasks.length }
      : null
  }
  const closesIt = (cardId: string, columnId: string) => {
    const current = baseForClose.current
    const card = current?.cards[cardId]
    if (!current || !card) return false
    return (
      !!current.columns[columnId]?.isFinishedPoint &&
      !current.columns[card.columnId]?.isFinishedPoint
    )
  }
  const moveCard = useCallback(
    (cardId: string, columnId: string, placement: Parameters<typeof moveCardNow>[2]) => {
      const parts = closesIt(cardId, columnId) ? openPartsOf(cardId) : null
      if (!parts) return moveCardNow(cardId, columnId, placement)
      setClosing({
        items: [parts],
        confirm: t.screen.closingMoveAnyway,
        go: () => moveCardNow(cardId, columnId, placement),
      })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- доска читается через ref
    [moveCardNow],
  )
  // Кусок панели карточки — сразу за доской, а не по нажатию: см. довод
  // у loadCardPanel.
  // Повторный вызов с каждым новым снимком ничего не стоит: сборщик
  // отдаёт уже загруженный модуль.
  useEffect(() => {
    if (base) void loadCardPanel()
  }, [base])
  // Есть ли вообще блокировки со сроком — от этого зависит, стоит ли
  // в строке отборов «Блокировка истекает».
  const hasBlockDeadlines = useMemo(
    () => Object.values(base?.cards ?? {}).some((c) => c.blocked?.until),
    [base?.cards],
  )

  // Заголовок вкладки: сначала то, что вкладку отличает. Открытая
  // карточка важнее доски — на неё и смотрят, когда её держат открытой.
  const открытая = openCard ? base?.cards[openCard] : undefined
  useDocumentTitle(открытая ? `${открытая.number} ${открытая.title}` : (base?.info.name ?? null))

  // Версия доски выросла — правка принята сервером. Сообщаем об этом
  // тому, кто показывает карточку у себя (экран «Задачи»): перечитывать
  // по закрытию панели рано — правка могла ещё не доехать.
  const version = base?.info.version
  const seenVersion = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (version === undefined) return
    if (seenVersion.current !== undefined && version !== seenVersion.current) onChanged?.()
    seenVersion.current = version
  }, [version, onChanged])

  /**
   * Зеркало доски для обработчиков.
   *
   * Обработчики карточки уходят в мемоизированный компонент, и любая
   * их пересборка перерисовывает все карточки разом. Замыкать в них
   * `base` нельзя: он меняется на каждую правку — а значит, правка
   * одной карточки стоила бы отрисовки всей доски. Замер на пятистах
   * карточках показывал 120 мс на переименование одной; читая
   * состояние из зеркала, обработчики остаются теми же объектами.
   *
   * Запись в эффекте, а не во время отрисовки: отрисовка может быть
   * отброшена, а обработчик вызывается уже после того, как результат
   * показан.
   */
  const stateRef = useRef<{ base: typeof base; order: Record<string, string[]> }>({
    base: null,
    order: {},
  })

  // Фильтр применяется к показу, а не к данным: перетаскивание,
  // счётчики лимита и догон патчами продолжают работать с полной
  // доской, иначе включённый фильтр начал бы менять её поведение.
  // «Работаем итерациями» выключено — итерации пропадают с экрана
  // целиком (этап 32.4): из отбора, группировки, карточек и таблицы.
  // Данные не трогаются, включение вернёт всё как было.
  const iterationsOn = base?.info.iterationsEnabled !== false
  // Удалить насовсем может тот, кто доской распоряжается: владелец
  // организации или владелец её подразделения. Отвечает сервер;
  // старый сервер поля не знает — тогда, как прежде, по роли.
  const canPurge = base?.info.canPurge ?? isOwner
  const { order, partIds, hidden } = useMemo(() => {
    if (!base)
      return {
        order: fullOrder,
        partIds: {} as Record<string, string[]>,
        hidden: 0,
      }
    if (isEmpty(filters)) return { ...withoutParts(base, fullOrder), hidden: 0 }
    // Карточки, у которых стоит часть. Считается один раз на проход
    // отбора: связей у доски единицы на карточку, а спрашивать по одной
    // значило бы обходить их заново для каждой.
    const stuck = new Set<string>()
    for (const link of base.links) {
      if (link.kind !== 'subtask') continue
      const own = base.cards[link.toCard]
      const foreign = base.linked[link.toCard]
      if (own ? Boolean(own.blocked) : Boolean(foreign?.blocked)) stuck.add(link.fromCard)
    }
    // И застрявшее глубже прямых частей: его знает только сервер.
    for (const card of Object.values(base.cards)) if (card.subtree?.stuck) stuck.add(card.id)
    const context = {
      labelsOf: (cardId: string) => base.cardLabels[cardId] ?? [],
      iterationOf: (cardId: string) => base.cardIterations[cardId],
      assigneesOf: (cardId: string) => base.cardAssignees[cardId] ?? [],
      partsBlocked: (cardId: string) => stuck.has(cardId),
      sleDays: base.info.sleDays,
    }
    const next: Record<string, string[]> = {}
    let hidden = 0
    for (const [columnId, ids] of Object.entries(fullOrder)) {
      next[columnId] = ids.filter((id) => {
        const card = base.cards[id]
        if (!card) return true
        // Отбор по итерации при выключенных итерациях не действует: иначе
        // ссылка, присланная до выключения, прятала бы карточки без
        // единого видимого повода.
        const ok = matches(card, iterationsOn ? filters : { ...filters, iteration: null }, context)
        if (!ok) hidden += 1
        return ok
      })
    }
    return { ...withoutParts(base, next), hidden }
  }, [base, fullOrder, filters, iterationsOn])

  // Группировка — тоже состояние адреса: сгруппированный вид посылают
  // ссылкой наравне с отфильтрованным.
  // Группировка по итерации при выключенных итерациях — как её отсутствие:
  // ссылка, присланная до выключения, открывает доску, а не пустые дорожки.
  const grouping = useMemo(() => {
    const asked = parseGrouping(query)
    return asked === 'iteration' && base?.info.iterationsEnabled === false ? 'none' : asked
  }, [query, base?.info.iterationsEnabled])
  const setGrouping = useCallback(
    (next: Grouping) => setQuery(groupingToQuery(next, query), { replace: true }),
    [query],
  )
  /**
   * Дорожки — и спрятанные части по дорожкам.
   *
   * Часть, показанная внутри родителя, из колонки убрана, но из счёта
   * не выкинута: сервер считает её в лимите так же. При дорожках счёт
   * был общий на доску — «Очередь 3» и «здесь только части задач, всего
   * 3» стояло в каждой дорожке, включая те, где не было ни одной части.
   *
   * Раскладываются части вместе с карточками, одним проходом: дорожка,
   * в которой нет ничего, кроме спрятанной части, обязана существовать —
   * иначе работа пропадает с доски совсем.
   */
  const { groups, partsIn } = useMemo(() => {
    if (!base) return { groups: [] as Group[], partsIn: {} as Record<string, Record<string, number>> }
    const вместе: Record<string, string[]> = {}
    for (const [columnId, ids] of Object.entries(order)) вместе[columnId] = [...ids]
    for (const [columnId, ids] of Object.entries(partIds)) {
      вместе[columnId] = [...(вместе[columnId] ?? []), ...ids]
    }
    // По дереву части не прячутся: дорожка родителя и есть место,
    // где они видны. Спрятанные внутри карточки, они оставили бы
    // дорожку пустой.
    const спрятанные = new Set(byTree(grouping) ? [] : Object.values(partIds).flat())
    const partsIn: Record<string, Record<string, number>> = {}
    const groups = groupsOf(base, вместе, grouping).map((group) => {
      const видимые: Record<string, string[]> = {}
      const здесь: Record<string, number> = {}
      let count = 0
      for (const [columnId, ids] of Object.entries(group.order)) {
        видимые[columnId] = ids.filter((id) => !спрятанные.has(id))
        здесь[columnId] = ids.length - видимые[columnId].length
        count += видимые[columnId].length
      }
      partsIn[group.id] = здесь
      return { ...group, order: видимые, count }
    })
    return { groups, partsIn }
  }, [base, order, partIds, grouping])

  /**
   * Сколько карточек скрыл отбор — по колонке каждой дорожки.
   *
   * Пустая колонка обязана отличать «здесь ничего нет» от «здесь
   * ничего не подошло»: «Пусто. Перетащите карточку сюда» при десяти
   * скрытых отправляет человека искать поломку, которой нет.
   * Считается по дорожкам, а не по доске: в дорожке счёт свой,
   * и общий соврал бы ровно так же.
   */
  const hiddenIn = useMemo(() => {
    if (!base || isEmpty(filters)) return {}
    const shown = new Map(groups.map((group) => [group.id, group.order]))
    const out: Record<string, Record<string, number>> = {}
    for (const group of groupsOf(base, fullOrder, grouping)) {
      const here: Record<string, number> = {}
      for (const [columnId, ids] of Object.entries(group.order)) {
        here[columnId] = ids.length - (shown.get(group.id)?.[columnId]?.length ?? 0)
      }
      out[group.id] = here
    }
    return out
  }, [base, fullOrder, grouping, groups, filters])

  // Что можно найти и что можно сделать — в одном списке: человек,
  // набрав «мет», одинаково может иметь в виду карточку со словом
  // «метка» и команду «сгруппировать по меткам».
  const commands = useMemo((): Command[] => {
    if (!base) return []
    const cards: Command[] = Object.values(base.cards).map((card) => ({
      id: `card-${card.id}`,
      title: card.title,
      // Номер в приписке, а не только в поиске: его называют вслух
      // и им же ищут — «посмотри ПОСТ-4», — и увидеть, что нашлось
      // именно оно, человек должен глазами.
      hint: `${card.number} · ${base.columns[card.columnId]?.name ?? ''}`,
      search: card.description,
      icon: <OpenIcon />,
      run: () => {
        setShowFlow(false)
        setOpenCard(card.id)
      },
    }))

    const actions: Command[] = [
      {
        id: 'flow',
        title: t.screen.showFlow,
        hint: t.screen.flowHint,
        icon: <FlowIcon />,
        run: () => {
          setOpenCard(null)
          setShowFlow(true)
        },
      },
      {
        id: 'archive',
        title: t.screen.showArchive,
        hint: t.screen.archiveHint,
        icon: <ArchiveIcon />,
        run: () => {
          setOpenCard(null)
          setShowFlow(false)
          setShowArchive(true)
        },
      },
      {
        id: 'access',
        title: t.screen.whoSees,
        icon: <PeopleIcon />,
        run: () => setShowAccess(true),
      },
      ...(Object.keys(GROUPING_NAMES) as Grouping[])
        .filter((g) => g !== grouping && (base.info.iterationsEnabled !== false || g !== 'iteration'))
        .map((g) => ({
          id: `group-${g}`,
          title: GROUPING_NAMES[g],
          hint: t.screen.groupingHint,
          icon: <TagIcon />,
          run: () => setGrouping(g),
        })),
      ...(isEmpty(filters)
        ? []
        : [
            {
              id: 'clear-filters',
              title: t.screen.showAllCards,
              hint: t.screen.resetHint,
              icon: <CloseIcon />,
              run: () => setFilters(EMPTY),
            },
          ]),
    ]

    return [...cards, ...actions]
  }, [base, grouping, filters, setGrouping, setFilters])

  // Список людей для фильтра: в снимке они словарём, а выпадающему
  // списку нужен порядок.
  const peopleList = useMemo(
    () =>
      Object.entries(base?.people ?? {})
        .map(([userId, name]) => ({ userId, name }))
        .sort((a, b) => a.name.localeCompare(b.name, locale())),
    [base?.people],
  )

  // useLayoutEffect, а не useEffect: обычные эффекты откладываются
  // планировщиком, и между отрисовкой карточек и обновлением зеркала
  // помещается нажатие клавиши. Так и было поймано: перенос
  // с клавиатуры сразу после загрузки доски не делал ничего.
  useLayoutEffect(() => {
    stateRef.current = { base, order }
  })

  // Стрелки водят выделение по доске, как по сетке: Tab идёт по всем
  // кнопкам подряд, и до третьей карточки во второй колонке им нужно
  // два десятка нажатий.
  const navigateCards = useCallback(
    (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => {
      const { base, order } = stateRef.current
      if (!base) return
      const next = nextCard(base.columnIds, order, cardId, direction)
      if (next && next !== cardId) refocus(next)
    },
    [refocus],
  )

  // Один монитор на всю доску: он знает и источник, и цель, и порядок
  // колонки — вычислять намерение по частям в отдельных обработчиках
  // значит собирать его из неполных данных.
  useEffect(() => {
    return monitorForElements({
      canMonitor: ({ source }) => source.data.kind === 'card',
      onDrop({ source, location }) {
        const target = location.current.dropTargets[0]
        if (!target) return
        const cardId = source.data.cardId as string
        // Вспышка на новом месте: карточка уехала, и глаз должен успеть
        // её там найти. Это единственная анимация в интерфейсе, и она
        // объясняет перемещение, а не украшает его.
        flash(cardId)

        if (target.data.kind === 'column') {
          moveCard(cardId, target.data.columnId as string, { place: 'end' })
          return
        }

        const overCardId = target.data.cardId as string
        const columnId = target.data.columnId as string
        if (overCardId === cardId) return

        // Порядок без перетаскиваемой карточки: иначе соседом окажется
        // она сама, и намерение получится бессмысленным.
        const list = (order[columnId] ?? []).filter((id) => id !== cardId)
        const at = list.indexOf(overCardId)
        const edge = extractClosestEdge(target.data)

        if (edge === 'bottom') {
          moveCard(cardId, columnId, { place: 'after', afterCardId: overCardId })
        } else if (at <= 0) {
          moveCard(cardId, columnId, { place: 'start' })
        } else {
          moveCard(cardId, columnId, { place: 'after', afterCardId: list[at - 1] })
        }
      },
    })
  }, [moveCard, order, flash])

  // Перетаскивание — не единственный способ переместить карточку.
  // Тот же moveCard вызывается с клавиатуры, поэтому доска остаётся
  // управляемой без мыши.
  const moveByKeyboard = useCallback(
    (cardId: string, direction: 'left' | 'right' | 'up' | 'down') => {
      const { base, order } = stateRef.current
      if (!base) return
      const card = base.cards[cardId]
      if (!card) return
      const columnIndex = base.columnIds.indexOf(card.columnId)

      if (direction === 'left' || direction === 'right') {
        const next = base.columnIds[columnIndex + (direction === 'left' ? -1 : 1)]
        if (!next) return
        moveCard(cardId, next, { place: 'end' })
        flash(cardId)
        announce(
          t.screen.movedAcross(
            card.title,
            base.columns[card.columnId].name,
            base.columns[next].name,
            (order[next]?.length ?? 0) + 1,
          ),
        )
        refocus(cardId)
        return
      }

      const list = order[card.columnId] ?? []
      const at = list.indexOf(cardId)
      if (direction === 'up') {
        if (at <= 0) return
        if (at === 1) moveCard(cardId, card.columnId, { place: 'start' })
        else moveCard(cardId, card.columnId, { place: 'after', afterCardId: list[at - 2] })
      } else {
        if (at < 0 || at >= list.length - 1) return
        moveCard(cardId, card.columnId, { place: 'after', afterCardId: list[at + 1] })
      }
      const to = direction === 'up' ? at : at + 2
      flash(cardId)
      announce(
        t.screen.movedWithin(card.title, to, list.length, base.columns[card.columnId].name),
      )
      refocus(cardId)
    },
    [moveCard, announce, refocus, flash],
  )

  /**
   * Перенос указателем без перетаскивания.
   *
   * Это не удобство, а требование: WCAG 2.5.7 прямо говорит, что
   * клавиатурного эквивалента недостаточно — нужен путь, выполнимый
   * одним кликом. Перетаскивание таким путём не является, а меню
   * на карточке — является.
   */
  const moveToColumn = useCallback(
    (cardId: string, columnId: string) => {
      const { base } = stateRef.current
      if (!base) return
      const card = base.cards[cardId]
      if (!card || card.columnId === columnId) return
      moveCard(cardId, columnId, { place: 'end' })
      announce(
        t.screen.movedTo(card.title, base.columns[card.columnId].name, base.columns[columnId].name),
      )
      refocus(cardId)
    },
    [moveCard, announce, refocus],
  )

  // Обработчики карточек собраны один раз: они уходят в мемоизированную
  // карточку, и новая функция на каждую отрисовку доски обесценивает
  // мемоизацию целиком. Идентификатор карточки приходит аргументом —
  // замыкать его значит делать функцию своей у каждой карточки.
  // Зависимости — сами действия, а не объект доски: он собирается
  // заново на каждую отрисовку, и обработчики вместе с ним.
  const {
    assignCard: assign,
    toggleLabel: label,
    renameCard: rename,
    archiveCard: archive,
    estimateCard: estimate,
    blockCard: block,
    prioritiseCard: prioritise,
    commitCard: commit,
    unblockCard: unblock,
    setBlockUntil: blockUntil,
    setBlockReason: blockReason,
    setCardDone: markDoneAction,
    createSubtask: subtaskAction,
  } = board
  const assignCard = useCallback(
    (cardId: string, userId: string, on: boolean) => void assign(cardId, userId, on),
    [assign],
  )
  const toggleLabel = useCallback(
    (cardId: string, labelId: string, on: boolean) => void label(cardId, labelId, on),
    [label],
  )
  const estimateCard = useCallback(
    (cardId: string, value: number | null) => void estimate(cardId, value),
    [estimate],
  )
  const prioritiseCard = useCallback(
    (cardId: string, priority: Priority) => void prioritise(cardId, priority),
    [prioritise],
  )
  const commitCard = useCallback(
    (cardId: string, dueOn: string | null) => void commit(cardId, dueOn),
    [commit],
  )
  const blockCard = useCallback(
    (cardId: string, reason: string, blockingCard?: string, until?: string) =>
      void block(cardId, reason, blockingCard, until),
    [block],
  )
  const setBlockUntil = useCallback(
    (cardId: string, until: string | null) => void blockUntil(cardId, until),
    [blockUntil],
  )
  const setBlockReason = useCallback(
    (cardId: string, reason: string) => void blockReason(cardId, reason),
    [blockReason],
  )
  const unblockCard = useCallback((cardId: string) => void unblock(cardId), [unblock])
  const markDone = useCallback(
    (cardId: string, done: boolean) => {
      const parts = done ? openPartsOf(cardId) : null
      if (!parts) return void markDoneAction(cardId, done)
      setClosing({
        items: [parts],
        confirm: t.screen.closingDoneAnyway,
        go: () => void markDoneAction(cardId, done),
      })
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps -- доска читается через ref
    [markDoneAction],
  )
  const addSubtask = useCallback(
    (parentCardId: string, title: string) => void subtaskAction(parentCardId, title),
    [subtaskAction],
  )
  const renameCard = useCallback(
    (cardId: string, title: string) => void rename(cardId, title),
    [rename],
  )
  const archiveCard = useCallback((cardId: string) => void archive(cardId), [archive])

  /**
   * Что выделено для действия над многими сразу.
   *
   * Местное состояние экрана: выделение не присылают ссылкой и не ждут
   * увидеть завтра — в отличие от фильтров, которые живут в адресе,
   * и от режима панели, который живёт в браузере.
   *
   * Набор, а не список: выделение проверяется на каждой карточке
   * при каждой отрисовке доски, и поиск по списку из ста выделенных
   * делал бы это за сто действий вместо одного.
   */
  const [picked, setPicked] = useState<Set<string>>(() => new Set())
  // С какой карточки начали: от неё считается диапазон при shift-щелчке.
  // Ref, а не состояние: от него ничего не перерисовывается.
  const pickedFrom = useRef<string | null>(null)
  const pickCard = useCallback((cardId: string, on: boolean, extend = false) => {
    setPicked((current) => {
      const next = new Set(current)
      // Shift-щелчок берёт всё между прошлым флажком и этим — в порядке
      // колонки, то есть в том, который человек видит. Без него разбор
      // бэклога остаётся щелчками по одной: двадцать карточек — двадцать
      // попаданий в квадрат тринадцати пикселей.
      const from = pickedFrom.current
      const { order } = stateRef.current
      if (extend && from && from !== cardId) {
        const column = Object.values(order).find((ids) => ids.includes(from) && ids.includes(cardId))
        if (column) {
          const a = column.indexOf(from)
          const b = column.indexOf(cardId)
          for (const id of column.slice(Math.min(a, b), Math.max(a, b) + 1)) {
            if (on) next.add(id)
            else next.delete(id)
          }
          pickedFrom.current = cardId
          return next
        }
      }
      if (on) next.add(cardId)
      else next.delete(cardId)
      pickedFrom.current = cardId
      return next
    })
  }, [])
  const clearPicked = useCallback(() => {
    setPicked(new Set())
    pickedFrom.current = null
  }, [])
  /**
   * Кого касается массовое действие.
   *
   * Пересечение выделенного с показанным: карточка, спрятанная
   * фильтром, остаётся выделенной, но под действие не попадает —
   * «в архив» не должно уносить то, чего на экране нет. Тем же счётом
   * живёт строка загрузки: числа не спорят с тем, что человек видит.
   */
  const chosen = useMemo(() => {
    if (picked.size === 0) return []
    const shown = new Set(Object.values(order).flat())
    return [...picked].filter((id) => shown.has(id))
  }, [picked, order])
  // Другая доска — другое выделение: идентификаторы чужие, и полоса
  // действий над ними обещала бы то, чего сделать нельзя.
  useEffect(() => clearPicked(), [boardId, clearPicked])

  // Какую карточку спрашивают удалить. Диалог один на доску, а не один
  // на карточку: пятьсот скрытых диалогов — это пятьсот узлов разметки
  // ради вопроса, который задают раз в месяц.
  // По какой итерации открыт отчёт. Закрытая итерация — это утверждение
  // «вот что было сделано», и посмотреть его должно быть можно.
  const [reportOf, setReportOf] = useState<Iteration | null>(null)
  // Справка с экрана: о чём спрашивают, когда нажимают F1 здесь. Верхнее
  // из открытого поверх доски — карточка, отчёт, «Поток», архив, — иначе
  // раскладка: у таблицы свой раздел.
  useHelpTopic(
    openCard
      ? 'card'
      : reportOf
        ? 'iterations'
        : showFlow
          ? 'flow'
          : showArchive
            ? 'archive'
            : view === 'table'
              ? 'table'
              : view === 'tree'
                ? 'tree'
                : 'board',
  )
  // Название хранится вместе с идентификатором, а не берётся из доски:
  // карточку спрашивают удалить и из архива, а там её на доске уже нет.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; title: string } | null>(null)
  const askDelete = useCallback(
    (cardId: string, title: string) => setPendingDelete({ id: cardId, title }),
    [],
  )
  // Меняется после удаления: архив перечитывает себя, потому что своего
  // состояния доски у него нет.
  const [archiveKey, setArchiveKey] = useState(0)
  const showCard = useCallback((cardId: string) => {
    setShowFlow(false)
    setOpenCard(cardId)
  }, [])

  // Список колонок для меню «перенести»: тот же массив, пока колонки
  // не менялись. Зависимость — сами колонки, а не доска: у доски
  // меняется хотя бы номер версии, то есть каждый раз.
  const columnIds = base?.columnIds
  const columnsById = base?.columns
  // Кто чья подзадача — один раз на доску: строка «часть такой-то
  // задачи» на карточке нужна всем карточкам сразу.
  const parents = useMemo(() => (base ? parentsOf(base) : {}), [base])
  // Подзадачи каждого родителя — один обход связей на доску, а не
  // по обходу на карточку: см. childrenOf.
  const children = useMemo(() => (base ? childrenOf(base) : {}), [base])

  // Отбирают по идущим итерациям: закрытую смотрят отчётом, а не доской.
  const openIterations = useMemo(
    () => (base && base.info.iterationsEnabled !== false ? base.iterations.filter((i) => !i.closedAt) : []),
    [base],
  )

  // Кто кого держит — один обход связей на доску, как подзадачи.
  const dependencies = useMemo(
    () => (base ? dependenciesOf(base) : { holds: {}, waitsFor: {} }),
    [base],
  )

  /** cardId → название итерации: карточке нужно слово, а не ссылка.
   *  Считается один раз на доску — как и всё, что уходит в карточку. */
  //  Название — вместе со сроком итерации: «Неделя 40 · до 4 окт.».
  //  Срок итерации и есть срок её карточек (решение владельца 25.09.2026):
  //  он показывается, но в «Обязательство» не пишется — то остаётся для
  //  обещаний наружу. Итерация кончилась, а карточка не сделана —
  //  `late`: пометка горит, карточку пора переносить в следующую.
  //  Два простых словаря, а не объект на карточку: объект пересоздавался
  //  бы с каждым снимком и перерисовывал все карточки доски.
  // Подсветка чужих правок (владелец 25.09.2026, «как в Планке»):
  // карточки, которые другие меняли после прошлого захода смотрящего
  // на эту доску. Когда он заходил, знает только его браузер — сервер
  // этого не хранит, и хранить незачем: это удобство смотрящего,
  // а не данные доски. Первый заход — окно в сутки. Открытая карточка
  // гаснет: её уже посмотрели.
  const [seenSince] = useState(() => lastVisit(boardId))
  const [opened, setOpened] = useState<ReadonlySet<string>>(() => new Set())
  useEffect(() => {
    if (cardId) setOpened((prev) => (prev.has(cardId) ? prev : new Set(prev).add(cardId)))
  }, [cardId])
  const changedCards = useMemo(() => {
    const out: Record<string, string> = {}
    for (const [id, change] of Object.entries(base?.recentChanges ?? {})) {
      if (change.at <= seenSince || change.actorId === meId || opened.has(id)) continue
      const when = new Date(change.at).toLocaleString(locale(), {
        day: 'numeric',
        month: 'short',
        hour: '2-digit',
        minute: '2-digit',
      })
      out[id] = change.actorId
        ? t.cardView.changedBy(base?.people[change.actorId] ?? '—', when)
        : t.cardView.changedByServer(when)
    }
    return out
  }, [base?.recentChanges, base?.people, seenSince, meId, opened])

  const { cardIterationNames, cardIterationLate, cardIterationEnd } = useMemo(() => {
    const names: Record<string, string> = {}
    const late: Record<string, boolean> = {}
    const ends: Record<string, string> = {}
    if (!base || base.info.iterationsEnabled === false) {
      return { cardIterationNames: names, cardIterationLate: late, cardIterationEnd: ends }
    }
    const now = new Date()
    const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    const byId = new Map(base.iterations.map((i) => [i.id, i]))
    for (const [cardId, iterationId] of Object.entries(base.cardIterations)) {
      const it = byId.get(iterationId)
      if (!it) continue
      names[cardId] = t.cardView.iterationUntil(it.name, dateWords(it.endsOn))
      ends[cardId] = dateWords(it.endsOn)
      const card = base.cards[cardId]
      if (card && it.endsOn < today && card.outcome === null && card.doneAt === null) late[cardId] = true
    }
    return { cardIterationNames: names, cardIterationLate: late, cardIterationEnd: ends }
  }, [base])

  // «Требует внимания» (шаг 5 нового дизайна): что стоит и почему.
  // Порядок — от того, что работу остановило, к тому, что её тормозит:
  // блокировка, дольше обещанного, переполненная колонка, кончившаяся
  // итерация. У карточки одна причина — старшая, как одна тревога
  // на самой карточке.
  const attention = useMemo<AttentionItem[]>(() => {
    if (!base) return []
    const blocked: AttentionItem[] = []
    const aging: AttentionItem[] = []
    const late: AttentionItem[] = []
    for (const card of Object.values(base.cards)) {
      if (card.doneAt || card.finishedAt || card.outcome) continue
      const common = { cardId: card.id, number: card.number, title: card.title }
      if (card.blocked) {
        blocked.push({ kind: 'blocked', ...common, note: card.blocked.reason })
        continue
      }
      const long = agingLabel(card, base.info.sleDays)
      if (long) {
        aging.push({ kind: 'aging', ...common, note: long })
        continue
      }
      if (cardIterationLate[card.id]) {
        const it = base.iterations.find((i) => i.id === base.cardIterations[card.id])
        late.push({ kind: 'late', ...common, note: t.attention.lateText(it?.name ?? '') })
      }
    }
    const limits: AttentionItem[] = []
    for (const id of base.columnIds) {
      const column = base.columns[id]
      const count = base.order[id]?.length ?? 0
      if (column.wipLimit !== null && count > column.wipLimit) {
        limits.push({ kind: 'limit', columnId: id, note: t.attention.limitText(column.name, count, column.wipLimit) })
      }
    }
    return [...blocked, ...aging, ...limits, ...late]
  }, [base, cardIterationLate])
  const showColumn = useCallback((columnId: string) => {
    document
      .querySelector(`[data-column-id="${columnId}"]`)
      ?.scrollIntoView({ block: 'nearest', inline: 'center' })
  }, [])

  const columnList = useMemo(
    () => (columnIds && columnsById ? columnIds.map((id) => columnsById[id]) : []),
    [columnIds, columnsById],
  )

  // Куда можно поставить работу соседям. Спрашивается один раз на доску,
  // а не на каждое открытие карточки: список короткий, меняется редко,
  // а панель открывают постоянно.
  //
  // Отказ проглатывается намеренно: без списка выбор доски просто
  // не появится, и подзадача заведётся здесь — то есть ровно так, как
  // работало до появления выбора.
  const [subtaskBoards, setSubtaskBoards] = useState<BoardInfo[]>([])
  useEffect(() => {
    let alive = true
    api.listBoards().then(
      ({ boards }) =>
        alive && setSubtaskBoards(boards.filter((b) => b.writable && b.id !== boardId)),
      () => {},
    )
    return () => {
      alive = false
    }
  }, [boardId])

  // Доска в архиве — не поломка, а положение дел: сказать об этом надо
  // словами и дать то единственное, что здесь делают. Прежде такая
  // ссылка отвечала «доска не найдена», и человек шёл искать поломку
  // там, где её нет.
  if (panelOnly && (board.archived || board.loadError || !base)) return null
  if (board.archived) {
    return (
      <div className="board-screen">
        <EmptyState
          title={t.screen.archivedTitle}
          action={
            <Button
              kind="primary"
              onClick={async () => {
                await api.restoreBoard(boardId)
                await board.reload()
              }}
            >
              {t.screen.restoreFromArchive}
            </Button>
          }
        >
          {t.screen.archivedBody}
        </EmptyState>
      </div>
    )
  }

  if (board.loadError) {
    // Доски нет или она чужая — повторять нечего: чужая неотличима
    // от несуществующей нарочно, и человеку нужен не «Повторить»,
    // а дорога назад. «Повторить» здесь отправляло бы искать поломку,
    // которой нет.
    const пропала = board.loadStatus === 404 || board.loadStatus === 403
    return (
      <div className="board-screen">
        <ErrorState
          what={t.screen.loadWhat}
          error={
            пропала
              ? t.screen.gone(board.loadError)
              : board.loadError
          }
          onRetry={пропала ? undefined : () => void board.reload()}
        />
        {пропала && (
          <button className="btn" onClick={onBack}>
            {t.screen.allBoards}
          </button>
        )}
      </div>
    )
  }
  // Заглушка в форме доски, а не слово «загружаем»: человек успевает
  // привыкнуть к раскладке до того, как она наполнится.
  if (!base) {
    return (
      <div className="board-screen">
        <BoardSkeleton />
      </div>
    )
  }

  // Колонки рисуются для каждой дорожки: сами колонки одни и те же,
  // разнится только набор карточек в них.
  // На узком экране рисуется одна колонка — выбранная или первая:
  // остальные доступны переключателем над доской.
  const shownColumns = narrow
    ? base.columnIds.filter((id) => id === (visibleColumn ?? base.columnIds[0]))
    : base.columnIds

  // Доска отобрана по открытой итерации — новые карточки заводятся в неё.
  const intoIteration =
    filters.iteration && filters.iteration !== NO_ITERATION && openIterations.some((i) => i.id === filters.iteration)
      ? filters.iteration
      : undefined

  const renderColumns = (
    groupOrder: Record<string, string[]>,
    hidden?: Record<string, number>,
    parts?: Record<string, number>,
  ) =>
    shownColumns.map((columnId) => (
      <ColumnView
        key={columnId}
        canEdit={canEdit}
        grouped={grouping !== 'none'}
        name={base.columns[columnId].name}
        columnId={columnId}
        column={base.columns[columnId]}
        cardIds={groupOrder[columnId] ?? []}
        partsInside={parts?.[columnId] ?? 0}
        hiddenByFilter={hidden?.[columnId] ?? 0}
        collapsed={collapsed.has(columnId)}
        onToggleCollapsed={() => toggleColumn(columnId)}
        width={columnWidths[columnId] ?? null}
        onResize={resizeColumn}
        cards={base.cards}
        unit={unit}
        sleDays={base.info.sleDays}
        people={base.people}
        onAssign={assignCard}
        justMoved={justMoved}
        labels={base.labels}
        boardId={boardId}
        cardLabels={base.cardLabels}
        cardAssignees={base.cardAssignees}
        parents={parents}
        iterations={cardIterationNames}
        iterationLate={cardIterationLate}
        iterationEnd={cardIterationEnd}
        changed={changedCards}
        holds={dependencies.holds}
        waitsFor={dependencies.waitsFor}
        children={children}
        onLabel={toggleLabel}
        onEpic={filterByEpic}
        selected={picked}
        onSelect={pickCard}
        onPrioritise={prioritiseCard}
        onBlock={blockCard}
        onUnblock={unblockCard}
        onMarkDone={markDone}
        onSubtask={addSubtask}
        columns={columnList}
        onMoveToColumn={moveToColumn}
        onOpenCard={showCard}
        onMoveByKeyboard={moveByKeyboard}
        onNavigate={navigateCards}
        onCreateCard={(title) => void board.createCard(columnId, title, intoIteration)}
        onRenameColumn={(name) => void board.renameColumn(columnId, name)}
        onSetLimit={(limit) => void board.setColumnLimit(columnId, limit)}
        onUpdateColumn={(patch) => void board.updateColumn(columnId, patch)}
        onMoveLeft={
          canEdit && base.columnIds.indexOf(columnId) > 0
            ? () => {
                const i = base.columnIds.indexOf(columnId)
                void board.moveColumn(columnId, i >= 2 ? base.columnIds[i - 2] : null)
              }
            : undefined
        }
        onMoveRight={
          canEdit && base.columnIds.indexOf(columnId) < base.columnIds.length - 1
            ? () => void board.moveColumn(columnId, base.columnIds[base.columnIds.indexOf(columnId) + 1])
            : undefined
        }
        onSortByIteration={
          iterationsOn && base.iterations.length > 0 ? () => void board.sortColumn(columnId) : undefined
        }
        onRenameCard={renameCard}
        onArchiveCard={archiveCard}
        onDeleteCard={canPurge ? askDelete : undefined}
      />
    ))

  const cardPanel = openCard && base.cards[openCard] && (
    <Suspense fallback={null}>
      <CardPanel
        onRename={renameCard}
        base={base}
        boardId={boardId}
        cardId={openCard}
        unit={unit}
        meId={meId}
        canEdit={canEdit}
        onClose={() => setOpenCard(null)}
        onDescribe={(id, text) => void board.describeCard(id, text)}
        onEstimate={estimateCard}
        onOpenCard={showCard}
        onAssign={assignCard}
        onLabel={toggleLabel}
        onPrioritise={prioritiseCard}
        onDue={commitCard}
        // С доски команды портфель не предлагается: подзадача там
        // была бы эпиком под задачей, а эпик всегда родитель
        // (сервер такое откажет — здесь просто не предлагаем).
        subtaskBoards={subtaskBoards.filter(
          (b) => base.info.level === 'portfolio' || b.level !== 'portfolio',
        )}
        onSubtask={(parentCardId, title, toBoard) =>
          void board.createSubtask(parentCardId, title, undefined, toBoard)
        }
        onLink={(from, to, kind) => void board.linkCards(from, to, kind)}
        onUnlink={(from, to, kind) => void board.unlinkCards(from, to, kind)}
        onBlock={blockCard}
        onSetBlockUntil={setBlockUntil}
        onSetBlockReason={setBlockReason}
        onUnblock={unblockCard}
        onMarkDone={markDone}
        onField={(id, fieldId, value) => void board.setCardField(id, fieldId, value)}
        onAddRef={(id, kind, ref) => void board.addCardRef(id, kind, ref)}
        onRemoveRef={(id, refId) => void board.removeCardRef(id, refId)}
        onIteration={(id, iterationId) => {
          const current = base.cardIterations[id]
          // Перенос — это выход из одного и вход в другой, и оба факта
          // остаются в истории: карточка не может идти в двух сразу.
          // Из закрытой не выходят: перенос в открытую сам закроет
          // участие в ней, не трогая её отчёт.
          const closed = base.iterations.find((i) => i.id === current)?.closedAt
          if (current && !closed) void board.removeFromIteration(id, current)
          if (iterationId) void board.addToIteration(id, iterationId)
        }}
      />
    </Suspense>
  )

  if (panelOnly)
    return (
      <div className="board-panel-only">
        <ClosingDialog closing={closing} onDone={() => setClosing(null)} />
        {cardPanel}
      </div>
    )

  return (
    // `tabIndex` — чтобы экрану можно было отдать фокус, когда
    // возвращать его больше некуда: диалог, открытый из спрятавшегося
    // меню карточки, иначе оставлял фокус на `body`.
    <div
      className={chosen.length > 0 ? 'board-screen board-screen--picking' : 'board-screen'}
      tabIndex={-1}
    >
      <ClosingDialog closing={closing} onDone={() => setClosing(null)} />
      <header className="board-header">
        <button className="btn btn--quiet" onClick={onBack}>
          <ChevronLeftIcon />
          {t.screen.allBoards}
        </button>
        <h1>{base.info.name}</h1>
        {/* Ключ стоит у названия, потому что больше ему стоять негде:
            в номерах карточек он виден только там, где карточки уже есть,
            а знать его нужно раньше — по нему доску называют в разговоре
            и ищут карточку по номеру. */}
        <span className="board-key" title={t.screen.keyTitle}>
          {base.info.key}
        </span>
        <span className="version" title={t.screen.versionTitle}>
          v{base.info.version}
        </span>
        <SandboxNote expiresAt={sandboxExpiresAt} />
        {board.pending > 0 && (
          <span className="pending" title={t.screen.pendingTitle}>
            {t.screen.saving(board.pending)}
          </span>
        )}
        {/* Личные настройки — и здесь: плотность нужна ровно там, где
            много карточек, то есть на доске, а уходить за ней в список
            досок незачем. */}
        <div className="board-header-tail">
          <button
            className="btn btn--quiet"
            aria-expanded={showAccess}
            onClick={() => {
              setOpenCard(null)
              setShowFlow(false)
              setShowAccess((v) => !v)
            }}
          >
            <PeopleIcon />
            {visibilityLabel(access)}
          </button>
          <Bell />
          <HelpButton />
          {account}
        </div>
      </header>

      {/* Две строки инструментов (решение владельца 25.09.2026: отбор
          на виду, но компактно). Первая — что показано: вид списком
          и весь отбор, основная панель (решение 22.09.2026). Вторая —
          как показано и куда уйти: поиск, группировка, сохранённые виды,
          справа палитра, «Поток», «Архив». Прежде это было три строки,
          а с открытой карточкой — пять, и «+ Колонка» уходила под панель. */}
      <div className="board-toolbar board-tools-line">
        {/* Вид — выпадающим списком слева (решение владельца 22.09.2026):
            основную строку занимает отбор, а вид меняют реже, чем
            отбирают. */}
        <select
          className="view-select"
          value={view}
          aria-label={t.screen.viewGroup}
          onChange={(e) => {
            const next = new URLSearchParams(query)
            if (e.target.value === 'board') next.delete('view')
            else next.set('view', e.target.value)
            // Смена раскладки показывается движением: это те же
            // карточки, а не другой экран. Довод и замер —
            // в `withViewTransition`.
            withViewTransition(() => setQuery(next))
          }}
        >
          <option value="board">{t.screen.viewBoard}</option>
          <option value="table">{t.screen.viewTable}</option>
          <option value="tree">{t.screen.viewTree}</option>
          <option value="changes">{t.screen.viewChanges}</option>
        </select>
        {asTable && (
          <select
            value={sort}
            aria-label={t.screen.sort}
            onChange={(e) => setQuery(sortToQuery(e.target.value as Sort, query))}
          >
            {(Object.keys(SORT_NAMES) as Sort[]).map((key) => (
              <option key={key} value={key}>
                {SORT_NAMES[key]}
              </option>
            ))}
          </select>
        )}
        <FilterBar
          filters={filters}
          people={peopleList}
          labels={base.labels}
          iterations={openIterations}
          hidden={hidden}
          hasBlockDeadlines={hasBlockDeadlines}
          epicTitle={
            filters.epic
              ? (Object.values(base.cards).find((c) => c.epic?.id === filters.epic)?.epic?.title ?? null)
              : null
          }
          onChange={setFilters}
        />
      </div>
      <div className="board-toolbar board-tools-line board-tools-line--second">
        <CardSearch filters={filters} onChange={setFilters} />
        <select
          className="grouping"
          value={grouping}
          aria-label={t.screen.grouping}
          onChange={(e) => setGrouping(e.target.value as Grouping)}
        >
          {(Object.keys(GROUPING_NAMES) as Grouping[])
            .filter((g) => iterationsOn || g !== 'iteration')
            .map((g) => (
              <option key={g} value={g}>
                {GROUPING_NAMES[g]}
              </option>
            ))}
        </select>
        {/* «?» — когда доска уже разложена дорожками: в строке инструментов
            он виден всегда и становился бы шумом, а вопрос «почему
            карточку не перетащить между дорожками» появляется, только
            когда они есть. */}
        {grouping !== 'none' && <Hint topic="grouping" />}
        <Views
          boardId={boardId}
          query={query.toString()}
          onOpen={(saved) => navigate(`${boardPath(boardId)}${saved ? `?${saved}` : ''}`)}
        />
        <div className="row board-tools">
          <button className="btn btn--quiet palette-open" onClick={() => setPalette(true)}>
            <SearchIcon />
            {t.screen.find}
            <span className="muted small">{paletteHint()}</span>
          </button>
          <button
            className="btn btn--quiet"
            aria-expanded={showFlow}
            onClick={() => {
              // Две панели разом перекрывают друг друга, а в модальном
              // режиме ещё и спорят за фокус. Открываем по одной.
              setOpenCard(null)
              setShowFlow((v) => !v)
            }}
          >
            <FlowIcon />
            <span className="tool-label">{t.screen.flow}</span>
          </button>
          <button
            className="btn btn--quiet"
            aria-expanded={showArchive}
            onClick={() => {
              setOpenCard(null)
              setShowFlow(false)
              setShowArchive((v) => !v)
            }}
          >
            <ArchiveIcon />
            <span className="tool-label">{t.screen.archive}</span>
          </button>
        </div>
      </div>

      {/* Про доску целиком — тихой полосой: кто сколько несёт, итерации,
          подсказка о разметке потока. Это читают, а не нажимают, и стоять
          в одном ряду с инструментами оно не должно. Загрузка считается
          по показанному: рядом «скрыто N» от отбора, и числа по всей
          доске спорили бы с экраном. */}
      <div className="board-toolbar board-context">
        <Workload
          base={base}
          order={order}
          unit={unit}
          picked={filters.assignee}
          onPick={(assignee) => setFilters({ ...filters, assignee })}
        />
        <FlowHint columns={columnList} />
        {iterationsOn ? (
          <Suspense fallback={null}>
            <Iterations
              boardId={boardId}
              canEdit={canEdit}
              iterations={base.iterations}
              onChanged={board.reload}
              onReport={setReportOf}
            />
          </Suspense>
        ) : (
          canEdit && (
            // Включение — там же, где итерации жили: искать его
            // в настройках, которых не видно, никто не станет.
            <button
              className="btn btn--quiet"
              onClick={() => void api.setIterations(boardId, true).then(board.reload)}
            >
              {t.screen.iterationsOn}
            </button>
          )
        )}
      </div>

      {Object.keys(base.cards).length === 0 && (
        <div className="note empty-board board-toolbar" role="note">
          <p className="small">
            <strong>{t.screen.emptyBoard}</strong>
            {t.screen.emptyBoardNotBroken}
          </p>
          <p className="muted small">
            {t.screen.emptyBoardHow(base.columns[base.columnIds[0]]?.name ?? t.screen.firstColumn)}
          </p>
        </div>
      )}

      {narrow && (
        <div className="column-switch board-toolbar" role="tablist" aria-label={t.screen.columns}>
          {base.columnIds.map((columnId) => {
            const current = columnId === (visibleColumn ?? base.columnIds[0])
            return (
              <button
                key={columnId}
                role="tab"
                aria-selected={current}
                className={current ? 'column-tab column-tab--current' : 'column-tab'}
                onClick={() => setVisibleColumn(columnId)}
              >
                {base.columns[columnId].name}
                {/* Число то же, что в шапке колонки: части, спрятанные
                    внутрь родителей, считаются и там и здесь. Иначе
                    на одном экране стояли рядом «Очередь 2»
                    в переключателе и «Очередь 5» в самой колонке. */}
                <span className="muted small">
                  {(order[columnId] ?? []).length + (partIds[columnId] ?? []).length}
                </span>
              </button>
            )
          })}
        </div>
      )}

      {/* Таблица — второй вид на те же данные, а не второй экран:
          фильтр, группировка и права остаются теми же, меняется только
          раскладка. Колонки при этом не рисуются вовсе — прятать их
          стилями значило бы держать в разметке пятьсот невидимых
          карточек. */}
      {view === 'tree' ? (
        <Suspense fallback={<Skeleton lines={6} />}>
          <TreeView base={base} unit={unit} onOpenCard={showCard} />
        </Suspense>
      ) : view === 'changes' ? (
        // Заглушка в форме списка, а не слово «загружаем»: кусок
        // приезжает за десятки миллисекунд, и мигать словом дольше,
        // чем показывать раскладку.
        <Suspense fallback={<Skeleton lines={4} />}>
          <Changes boardId={boardId} fields={base.fields} onOpenCard={showCard} />
        </Suspense>
      ) : asTable ? (
        <Suspense fallback={<Skeleton lines={6} />}>
        <TableView
          base={base}
          order={order}
          columns={columnList}
          unit={unit}
          sort={sort}
          onSort={(next) => setQuery(sortToQuery(next, query))}
          people={base.people}
          labels={base.labels}
          onOpenCard={showCard}
          onMoveToColumn={moveToColumn}
          onAssign={assignCard}
        />
        </Suspense>
      ) : (
        <div className="board-field">
        <div className="board-lanes">
      {/* Доска прокручивается вбок сама, когда карточку подносят к краю:
          иначе перетащить в дальнюю колонку можно только в два приёма —
          бросить, прокрутить, взять снова. */}
      {groups.map((group) => (
        <div
          className={grouping === 'none' ? 'swimlane swimlane--single' : 'swimlane'}
          key={group.id}
        >
          {grouping !== 'none' && (
            <div className="swimlane-head board-toolbar">
              <h2 className="swimlane-title">
                {/* Дорожка родителя с этой доски открывает его карточку:
                    дорожку по эпику смотрят, чтобы дойти до эпика. */}
                {group.cardId ? (
                  <button className="link" onClick={() => showCard(group.cardId!)}>
                    {group.title}
                  </button>
                ) : (
                  group.title
                )}
              </h2>
              {group.note && <span className="muted small">{group.note}</span>}
              <span className="muted small">{group.count}</span>
            </div>
          )}
          <div className="columns" ref={columnsRef}>
            {renderColumns(group.order, hiddenIn[group.id], partsIn[group.id])}
            {grouping === 'none' && canEdit && (
              <NewColumn onCreate={(name) => void board.createColumn(name)} />
            )}
          </div>
        </div>
      ))}
        </div>
        {/* На узком экране панели нет: там одна колонка с переключателем,
            и треть экрана под список — это полдоски. */}
        {!narrow && (
          <AttentionRail
            items={attention}
            onOpenCard={showCard}
            onShowColumn={showColumn}
            onFlow={() => setShowFlow(true)}
          />
        )}
        </div>
      )}

      {showFlow && (
        <Suspense fallback={<Skeleton lines={4} />}>
        <Flow
          boardId={boardId}
          sleDays={base.info.sleDays}
          sleProbability={base.info.sleProbability}
          iterationsEnabled={iterationsOn}
          level={base.info.level === 'portfolio' ? 'portfolio' : 'team'}
          onClose={() => setShowFlow(false)}
          onPromise={board.reload}
        />
        </Suspense>
      )}

      {/* Полоса действий над выделенными. Пусто выделено — полосы нет:
          она обещала бы действие, которому не над чем работать. */}
      {chosen.length > 0 && (
        <Suspense fallback={null}>
          <BulkBar
            count={chosen.length}
            columns={columnList}
            boardId={boardId}
            labels={base.labels}
            people={base.people}
            onMove={(columnId) => {
              const ids = chosen
              clearPicked()
              const items = ids
                .filter((id) => closesIt(id, columnId))
                .map(openPartsOf)
                .filter((x): x is ClosingItem => x !== null)
              if (items.length === 0) void board.moveMany(ids, columnId)
              else
                setClosing({
                  items,
                  confirm: t.screen.closingMoveAnyway,
                  go: () => void board.moveMany(ids, columnId),
                })
            }}
            onPrioritise={(priority) => {
              const ids = chosen
              clearPicked()
              void board.prioritiseMany(ids, priority)
            }}
            onLabel={(labelId) => {
              const ids = chosen
              clearPicked()
              void board.labelMany(ids, labelId)
            }}
            onAssign={(userId) => {
              const ids = chosen
              clearPicked()
              void board.assignMany(ids, userId)
            }}
            onArchive={() => {
              const ids = chosen
              clearPicked()
              void board.archiveMany(ids)
            }}
            onClear={clearPicked}
          />
        </Suspense>
      )}

      <Palette open={palette} commands={commands} onClose={() => setPalette(false)} />

      {showArchive && (
        <Suspense fallback={<Skeleton lines={3} />}>
        <CardArchive
          boardId={boardId}
          canDelete={canPurge}
          reloadKey={archiveKey}
          onRestored={board.reload}
          onDelete={askDelete}
          onClose={() => setShowArchive(false)}
        />
        </Suspense>
      )}

      {reportOf && (
        <Suspense fallback={<Skeleton lines={4} />}>
        <IterationReport
          boardId={boardId}
          iteration={reportOf}
          unit={unit}
          onOpenCard={(cardId) => {
            setReportOf(null)
            showCard(cardId)
          }}
          onClose={() => setReportOf(null)}
          carry={
            canEdit && reportOf.closedAt && openIterations.length > 0
              ? {
                  targets: openIterations,
                  // В отчёте карточки на момент закрытия; переносить есть
                  // смысл только те, что всё ещё числятся за этой итерацией.
                  stillIn: (cardId) => base.cardIterations[cardId] === reportOf.id,
                  onCarry: board.carryOver,
                }
              : undefined
          }
        />
        </Suspense>
      )}

      {/* Вопрос задаётся один раз и называет карточку: подтверждение
          «вы уверены?» без имени того, что исчезнет, отвечают не читая. */}
      <ConfirmDialog
        open={pendingDelete !== null}
        title={t.screen.deleteTitle}
        confirmLabel={t.screen.deleteForever}
        danger
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          const card = pendingDelete
          setPendingDelete(null)
          if (card) void board.deleteCard(card.id).then(() => setArchiveKey((k) => k + 1))
        }}
      >
        <p>{t.screen.deleteBody(pendingDelete?.title ?? t.screen.aCard)}</p>
        <p className="muted small">{t.screen.deleteAudit}</p>
      </ConfirmDialog>

      {showAccess && (
        <Suspense fallback={null}>
          <AccessPanel
            boardId={boardId}
            canEdit={canEdit}
            onClose={() => setShowAccess(false)}
            onChanged={loadAccess}
          />
        </Suspense>
      )}

      {cardPanel}

      <div className="sr-only" role="status" aria-live="polite" aria-atomic="true">
        {announcement}
      </div>
    </div>
  )
}

function NewColumn({ onCreate }: { onCreate: (name: string) => void }) {
  const [adding, setAdding] = useState(false)
  const [value, setValue] = useState('')
  if (!adding)
    return (
      <button className="column column--ghost" onClick={() => setAdding(true)}>
        {t.screen.addColumn}
      </button>
    )
  return (
    <form
      className="column column--ghost"
      onSubmit={(e) => {
        e.preventDefault()
        if (value.trim()) {
          onCreate(value.trim())
          setValue('')
          setAdding(false)
        }
      }}
    >
      <input
        autoFocus
        value={value}
        aria-label={t.screen.columnName}
        placeholder={t.screen.columnName}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && setAdding(false)}
      />
      <button type="submit" aria-label={t.screen.createColumn}>
        {t.screen.create}
      </button>
    </form>
  )
}

/**
 * Чего не хватает доске для метрик потока. Показывается один раз сверху,
 * а не на каждой колонке: это свойство доски целиком.
 */
function FlowHint({ columns }: { columns: Column[] }) {
  const issues = flowIssues(columns)
  if (issues.length === 0) return null
  return (
    <div className="note" role="note">
      {issues.map((text) => (
        <p key={text} className="small">
          {text}
        </p>
      ))}
    </div>
  )
}

/** Карточка, которую закрывают при незакрытых подзадачах. */
type ClosingItem = { title: string; open: string[]; total: number }
/** Что спросить и что сделать, если согласились. */
type Closing = { items: ClosingItem[]; confirm: string; go: () => void }

/** Вопрос перед закрытием карточки с незакрытыми подзадачами. */
function ClosingDialog({ closing, onDone }: { closing: Closing | null; onDone: () => void }) {
  return (
    <ConfirmDialog
      open={closing !== null}
      title={t.screen.closingOpenTitle}
      confirmLabel={closing?.confirm ?? ''}
      onCancel={onDone}
      onConfirm={() => {
        const go = closing?.go
        onDone()
        go?.()
      }}
    >
      {closing?.items.map((item) => (
        <div key={item.title}>
          <p>{t.screen.closingOpenOne(item.title, item.open.length, item.total)}</p>
          {/* Пять названий, дальше числом: список на двадцать строк
              закрывал бы кнопки диалога. */}
          <ul className="small">
            {item.open.slice(0, 5).map((title, i) => (
              <li key={i}>{title}</li>
            ))}
            {item.open.length > 5 && <li className="muted">{t.screen.closingOpenMore(item.open.length - 5)}</li>}
          </ul>
        </div>
      ))}
      <p className="muted small">{t.screen.closingOpenStays}</p>
    </ConfirmDialog>
  )
}

/**
 * Когда смотрящий в прошлый раз открывал доску, — и отметка «сейчас»
 * на следующий раз. Хранится в браузере: не вышло прочитать или
 * записать (приватное окно) — подсветка просто берёт сутки.
 */
function lastVisit(boardId: string): string {
  const key = `board-seen:${boardId}`
  const dayAgo = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString()
  let previous: string | null = null
  try {
    previous = localStorage.getItem(key)
    localStorage.setItem(key, new Date().toISOString())
  } catch {
    // без памяти браузера — сутки
  }
  return previous ?? dayAgo
}
