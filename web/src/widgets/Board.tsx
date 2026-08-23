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
import { flowIssues, withoutParts } from '../entities/board/model.ts'
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
import { CardPanel } from '../features/board/CardPanel.tsx'
import { Appearance } from '../shared/ui/Appearance.tsx'
import { BoardSkeleton, EmptyState, ErrorState, Skeleton } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { ConfirmDialog } from '../shared/ui/Dialog.tsx'
import { FilterBar } from '../features/board/FilterBar.tsx'
import { EMPTY, filtersToQuery, isEmpty, matches, parseFilters } from '../features/board/filters.ts'
import type { Filters } from '../features/board/filters.ts'
import { withViewTransition } from '../shared/lib/withViewTransition.ts'
import { boardPath, navigate, setQuery, useQuery } from '../shared/router/index.ts'
import { Views } from '../features/board/Views.tsx'
import { SORT_NAMES, parseSort, sortToQuery } from '../features/board/tableSort.ts'
import { Workload } from '../features/board/Workload.tsx'
import { BulkBar } from '../features/board/BulkBar.tsx'
import type { Sort } from '../features/board/tableSort.ts'
import { Palette, paletteHint, usePaletteHotkey } from '../features/board/Palette.tsx'
import type { Command } from '../features/board/Palette.tsx'
import { useCollapsedColumns } from '../features/board/useCollapsed.ts'
import { nextCard } from '../features/board/navigation.ts'
import { childrenOf, dependenciesOf, parentsOf, rangeWords } from '../entities/card/model.ts'
import { NARROW, useMedia } from '../shared/lib/useMedia.ts'
import {
  GROUPING_NAMES,
  groupingToQuery,
  groupsOf,
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
import { AccessPanel, visibilityLabel } from '../features/access/AccessPanel.tsx'
import { ColumnView } from '../features/board/ColumnView.tsx'
import { useBoard } from '../features/board/useBoard.ts'
import { ScreenError } from '../shared/ui/Field'

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
const Flow = lazy(() => import('../features/flow/Flow.tsx').then((m) => ({ default: m.Flow })))
const Changes = lazy(() =>
  import('../features/board/Changes.tsx').then((m) => ({ default: m.Changes })),
)
const CardArchive = lazy(() =>
  import('../features/board/CardArchive.tsx').then((m) => ({ default: m.CardArchive })),
)
const IterationReport = lazy(() =>
  import('../features/board/IterationReport.tsx').then((m) => ({ default: m.IterationReport })),
)

export function Board({
  boardId,
  cardId,
  onCard,
  unit,
  meId,
  isOwner,
  canEdit,
  onBack,
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
  onBack: () => void
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
  const view = query.get('view') === 'table' ? 'table' : query.get('view') === 'changes' ? 'changes' : 'board'
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

  const { base, order: fullOrder, moveCard } = board

  // Заголовок вкладки: сначала то, что вкладку отличает. Открытая
  // карточка важнее доски — на неё и смотрят, когда её держат открытой.
  const открытая = openCard ? base?.cards[openCard] : undefined
  useDocumentTitle(открытая ? `${открытая.number} ${открытая.title}` : (base?.info.name ?? null))

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
        const ok = matches(card, filters, context)
        if (!ok) hidden += 1
        return ok
      })
    }
    return { ...withoutParts(base, next), hidden }
  }, [base, fullOrder, filters])

  // Группировка — тоже состояние адреса: сгруппированный вид посылают
  // ссылкой наравне с отфильтрованным.
  const grouping = useMemo(() => parseGrouping(query), [query])
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
    const спрятанные = new Set(Object.values(partIds).flat())
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
        title: 'Показать поток',
        hint: 'метрики доски',
        icon: <FlowIcon />,
        run: () => {
          setOpenCard(null)
          setShowFlow(true)
        },
      },
      {
        id: 'archive',
        title: 'Показать архив карточек',
        hint: 'убранные с доски',
        icon: <ArchiveIcon />,
        run: () => {
          setOpenCard(null)
          setShowFlow(false)
          setShowArchive(true)
        },
      },
      {
        id: 'access',
        title: 'Кому видна доска',
        icon: <PeopleIcon />,
        run: () => setShowAccess(true),
      },
      ...(Object.keys(GROUPING_NAMES) as Grouping[])
        .filter((g) => g !== grouping)
        .map((g) => ({
          id: `group-${g}`,
          title: GROUPING_NAMES[g],
          hint: 'группировка',
          icon: <TagIcon />,
          run: () => setGrouping(g),
        })),
      ...(isEmpty(filters)
        ? []
        : [
            {
              id: 'clear-filters',
              title: 'Показать все карточки',
              hint: 'сбросить фильтры',
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
        .sort((a, b) => a.name.localeCompare(b.name, 'ru')),
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
          `Карточка «${card.title}» перенесена из «${base.columns[card.columnId].name}» ` +
            `в «${base.columns[next].name}», последняя из ${(order[next]?.length ?? 0) + 1}`,
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
        `Карточка «${card.title}» перенесена на позицию ${to} из ${list.length} ` +
          `в колонке «${base.columns[card.columnId].name}»`,
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
        `Карточка «${card.title}» перенесена из «${base.columns[card.columnId].name}» ` +
          `в «${base.columns[columnId].name}»`,
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
    (cardId: string, reason: string, blockingCard?: string) =>
      void block(cardId, reason, blockingCard),
    [block],
  )
  const unblockCard = useCallback((cardId: string) => void unblock(cardId), [unblock])
  const markDone = useCallback(
    (cardId: string, done: boolean) => void markDoneAction(cardId, done),
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
    () => (base ? base.iterations.filter((i) => !i.closedAt) : []),
    [base],
  )

  // Кто кого держит — один обход связей на доску, как подзадачи.
  const dependencies = useMemo(
    () => (base ? dependenciesOf(base) : { holds: {}, waitsFor: {} }),
    [base],
  )

  /** cardId → название итерации: карточке нужно слово, а не ссылка.
   *  Считается один раз на доску — как и всё, что уходит в карточку. */
  const cardIterationNames = useMemo(() => {
    const names: Record<string, string> = {}
    if (!base) return names
    const byId = new Map(base.iterations.map((i) => [i.id, i.name]))
    for (const [cardId, iterationId] of Object.entries(base.cardIterations)) {
      const name = byId.get(iterationId)
      if (name) names[cardId] = name
    }
    return names
  }, [base])

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
  if (board.archived) {
    return (
      <div className="board-screen">
        <EmptyState
          title="Доска в архиве"
          action={
            <Button
              kind="primary"
              onClick={async () => {
                await api.restoreBoard(boardId)
                await board.reload()
              }}
            >
              Вернуть из архива
            </Button>
          }
        >
          Карточки и журнал целы — доска просто убрана с глаз. Вернуть её
          можно прямо отсюда.
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
          what="загрузить доску"
          error={
            пропала
              ? `${board.loadError}. Возможно, её убрали или доступ к ней закрыли.`
              : board.loadError
          }
          onRetry={пропала ? undefined : () => void board.reload()}
        />
        {пропала && (
          <button className="btn" onClick={onBack}>
            Все доски
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
        cards={base.cards}
        unit={unit}
        sleDays={base.info.sleDays}
        people={base.people}
        onAssign={assignCard}
        justMoved={justMoved}
        labels={base.labels}
        cardLabels={base.cardLabels}
        cardAssignees={base.cardAssignees}
        parents={parents}
        iterations={cardIterationNames}
        holds={dependencies.holds}
        waitsFor={dependencies.waitsFor}
        children={children}
        onLabel={toggleLabel}
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
        onCreateCard={(title) => void board.createCard(columnId, title)}
        onRenameColumn={(name) => void board.renameColumn(columnId, name)}
        onSetLimit={(limit) => void board.setColumnLimit(columnId, limit)}
        onUpdateColumn={(patch) => void board.updateColumn(columnId, patch)}
        onRenameCard={renameCard}
        onArchiveCard={archiveCard}
        onDeleteCard={isOwner ? askDelete : undefined}
      />
    ))

  return (
    // `tabIndex` — чтобы экрану можно было отдать фокус, когда
    // возвращать его больше некуда: диалог, открытый из спрятавшегося
    // меню карточки, иначе оставлял фокус на `body`.
    <div
      className={chosen.length > 0 ? 'board-screen board-screen--picking' : 'board-screen'}
      tabIndex={-1}
    >
      <header className="board-header">
        <button className="btn btn--quiet" onClick={onBack}>
          <ChevronLeftIcon />
          Все доски
        </button>
        <h1>{base.info.name}</h1>
        {/* Ключ стоит у названия, потому что больше ему стоять негде:
            в номерах карточек он виден только там, где карточки уже есть,
            а знать его нужно раньше — по нему доску называют в разговоре
            и ищут карточку по номеру. */}
        <span className="board-key" title="Ключ доски — начало номеров её карточек">
          {base.info.key}
        </span>
        <span className="version" title="Версия доски растёт с каждой операцией">
          v{base.info.version}
        </span>
        {board.pending > 0 && (
          <span className="pending" title="Изменения ещё не подтверждены сервером">
            сохраняем… {board.pending}
          </span>
        )}
        {/* Тема и плотность живут и здесь. Плотность нужна ровно там, где
            много карточек, то есть на доске, — а переключатель до сих пор
            стоял только в списке досок, где он бесполезен. */}
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
          <Appearance />
        </div>
      </header>

      <div className="board-toolbar filters-line">
        <FilterBar
          filters={filters}
          people={peopleList}
          labels={base.labels}
          iterations={openIterations}
          hidden={hidden}
          onChange={setFilters}
        />
        {/* Загрузка считается по показанному: рядом стоит «скрыто N»
            от фильтра, и числа по всей доске спорили бы с экраном. */}
        <Workload base={base} order={order} unit={unit} />
        <Views
          boardId={boardId}
          query={query.toString()}
          onOpen={(saved) => navigate(`${boardPath(boardId)}${saved ? `?${saved}` : ''}`)}
        />
        <select
          className="grouping"
          value={grouping}
          aria-label="Группировка"
          onChange={(e) => setGrouping(e.target.value as Grouping)}
        >
          {(Object.keys(GROUPING_NAMES) as Grouping[]).map((g) => (
            <option key={g} value={g}>
              {GROUPING_NAMES[g]}
            </option>
          ))}
        </select>
      </div>

      {/* Одна полоса, а не три: подсказка о потоке, итерации и переход
          к потоку — это всё «про доску целиком», и разносить их
          по отдельным строкам значит съедать высоту у самих колонок. */}
      <div className="board-toolbar">
        <div className="row row--between">
          <div className="row">
            <FlowHint columns={columnList} />
            <Iterations
              boardId={boardId}
              canEdit={canEdit}
              iterations={base.iterations}
              onChanged={board.reload}
              onReport={setReportOf}
            />
          </div>
          {/* Всё, что переключает взгляд на доску, — одной группой:
              когда строка не помещается, она переносится целиком,
              а не рассыпается на «Поток» слева и «Архив» справа. */}
          <div className="row board-tools">
            <button className="btn btn--quiet" onClick={() => setPalette(true)}>
              <SearchIcon />
              Найти
              <span className="muted small">{paletteHint()}</span>
            </button>
            {/* Переключатель видов: одна доска, разные раскладки. Сегмент,
                а не выпадающий список, — вариантов три, и выбранный должен
                быть виден без нажатия. */}
            <div className="segment" role="group" aria-label="Вид доски">
              {[
                { key: 'board', name: 'Доска' },
                { key: 'table', name: 'Таблица' },
                { key: 'changes', name: 'Изменения' },
              ].map((item) => (
                <button
                  key={item.key}
                  className={item.key === view ? 'segment-item segment-item--on' : 'segment-item'}
                  aria-pressed={item.key === view}
                  onClick={() => {
                    const next = new URLSearchParams(query)
                    if (item.key === 'board') next.delete('view')
                    else next.set('view', item.key)
                    // Смена раскладки показывается движением: это те же
                    // карточки, а не другой экран. Довод и замер —
                    // в `withViewTransition`.
                    withViewTransition(() => setQuery(next))
                  }}
                >
                  {item.name}
                </button>
              ))}
            </div>
            {asTable && (
              <select
                value={sort}
                aria-label="Сортировка"
                onChange={(e) => setQuery(sortToQuery(e.target.value as Sort, query))}
              >
                {(Object.keys(SORT_NAMES) as Sort[]).map((key) => (
                  <option key={key} value={key}>
                    {SORT_NAMES[key]}
                  </option>
                ))}
              </select>
            )}
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
              Поток
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
              Архив
            </button>
          </div>
        </div>
      </div>

      {Object.keys(base.cards).length === 0 && (
        <div className="note empty-board board-toolbar" role="note">
          <p className="small">
            <strong>На доске ещё нет карточек.</strong> Это не поломка — просто здесь пока ничего не
            заводили.
          </p>
          <p className="muted small">
            Заведите первую в колонке «{base.columns[base.columnIds[0]]?.name ?? 'первой'}». Дальше
            её можно перетащить мышью, перенести кнопкой на самой карточке или клавишами: Ctrl со
            стрелками.
          </p>
        </div>
      )}

      {narrow && (
        <div className="column-switch board-toolbar" role="tablist" aria-label="Колонки">
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
      {view === 'changes' ? (
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
        <>
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
              <h2 className="swimlane-title">{group.title}</h2>
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
        </>
      )}

      {showFlow && (
        <Suspense fallback={<Skeleton lines={4} />}>
        <Flow
          boardId={boardId}
          sleDays={base.info.sleDays}
          sleProbability={base.info.sleProbability}
          onClose={() => setShowFlow(false)}
          onPromise={board.reload}
        />
        </Suspense>
      )}

      {/* Полоса действий над выделенными. Пусто выделено — полосы нет:
          она обещала бы действие, которому не над чем работать. */}
      {chosen.length > 0 && (
        <BulkBar
          count={chosen.length}
          columns={columnList}
          labels={base.labels}
          people={base.people}
          onMove={(columnId) => {
            const ids = chosen
            clearPicked()
            void board.moveMany(ids, columnId)
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
      )}

      <Palette open={palette} commands={commands} onClose={() => setPalette(false)} />

      {showArchive && (
        <Suspense fallback={<Skeleton lines={3} />}>
        <CardArchive
          boardId={boardId}
          canDelete={isOwner}
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
        />
        </Suspense>
      )}

      {/* Вопрос задаётся один раз и называет карточку: подтверждение
          «вы уверены?» без имени того, что исчезнет, отвечают не читая. */}
      <ConfirmDialog
        open={pendingDelete !== null}
        title="Удалить навсегда?"
        confirmLabel="Удалить навсегда"
        danger
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => {
          const card = pendingDelete
          setPendingDelete(null)
          if (card) void board.deleteCard(card.id).then(() => setArchiveKey((k) => k + 1))
        }}
      >
        <p>
          «{pendingDelete?.title ?? 'Карточка'}» исчезнет вместе с историей её работы, связями
          и обсуждением. Вернуть будет нечем — в отличие от архива.
        </p>
        <p className="muted small">
          В журнале действий останется запись о том, кто её удалил и что в ней было.
        </p>
      </ConfirmDialog>

      {showAccess && (
        <AccessPanel
          boardId={boardId}
          canEdit={canEdit}
          onClose={() => setShowAccess(false)}
          onChanged={loadAccess}
        />
      )}

      {openCard && base.cards[openCard] && (
        <CardPanel
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
          subtaskBoards={subtaskBoards}
          onSubtask={(parentCardId, title, toBoard) =>
            void board.createSubtask(parentCardId, title, undefined, toBoard)
          }
          onLink={(from, to, kind) => void board.linkCards(from, to, kind)}
          onUnlink={(from, to, kind) => void board.unlinkCards(from, to, kind)}
          onBlock={blockCard}
          onUnblock={unblockCard}
          onMarkDone={markDone}
          onField={(id, fieldId, value) => void board.setCardField(id, fieldId, value)}
          onIteration={(id, iterationId) => {
            const current = base.cardIterations[id]
            // Перенос — это выход из одного и вход в другой, и оба факта
            // остаются в истории: карточка не может идти в двух сразу.
            if (current) void board.removeFromIteration(id, current)
            if (iterationId) void board.addToIteration(id, iterationId)
          }}
        />
      )}

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
        + Колонка
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
        aria-label="Название колонки"
        placeholder="Название колонки"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && setAdding(false)}
      />
      <button type="submit" aria-label="Завести колонку">
        Завести
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

/**
 * Итерации доски.
 *
 * Закрытие необратимо, поэтому спрашивается подтверждением: это
 * утверждение «вот что было сделано», а не отметка о прочтении.
 */
function Iterations({
  boardId,
  canEdit,
  iterations,
  onChanged,
  onReport,
}: {
  boardId: string
  /** Наблюдателю итерации видны, но заводить и закрывать их он не может:
   *  показанная кнопка означала бы обещание, которое сервер отвергнет. */
  canEdit: boolean
  iterations: Iteration[]
  onChanged: () => void
  /** Открыть отчёт по итерации. */
  onReport: (iteration: Iteration) => void
}) {
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Какую итерацию спрашивают закрыть. Нативный confirm тут был
  // единственным на всё приложение: он выглядит чужим и, в отличие
  // от своего диалога, останавливает страницу целиком.
  const [toClose, setToClose] = useState<Iteration | null>(null)
  const open = iterations.filter((i) => i.closedAt === null)
  // Закрытые не пропадают с экрана. Итерация закрывается ради ответа
  // «что было в спринте на момент закрытия» — а до сих пор в этот момент
  // она и исчезала, унося ответ вместе с собой.
  const closed = iterations.filter((i) => i.closedAt !== null)

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(onChanged).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <div className="iterations">
      <ScreenError>{error}</ScreenError>

      <ConfirmDialog
        open={toClose !== null}
        title="Закрыть итерацию?"
        // Не просто «Закрыть»: рядом на экране живёт «Закрыть» панели,
        // и одно и то же слово означало бы то «уйти отсюда», то
        // «заморозить состав навсегда».
        confirmLabel="Закрыть итерацию"
        danger
        onCancel={() => setToClose(null)}
        onConfirm={() => {
          const it = toClose
          setToClose(null)
          if (it) act(api.closeIteration(boardId, it.id))
        }}
      >
        <p>
          Состав «{toClose?.name}» замрёт: закрытая итерация больше не принимает и не отпускает
          карточки. Открыть обратно нечем.
        </p>
      </ConfirmDialog>
      <div className="row row--tight">
        {/* «Итераций нет» — только когда их нет вовсе. Рядом со списком
            закрытых эта надпись противоречила бы сама себе. */}
        {iterations.length === 0 && !adding && <span className="muted small">Итераций нет</span>}
        {open.map((i) => (
          <span key={i.id} className="mark" title={i.goal}>
            <button className="link" onClick={() => onReport(i)}>
              {i.name} · {rangeWords(i.startsOn, i.endsOn)} · {i.cardCount}
            </button>
            {/* Имя называет итерацию: кнопок «закрыть» в строке столько
                же, сколько итераций, и с диктора они звучали одинаково. */}
            {canEdit && (
              <button
                className="link"
                aria-label={`Закрыть итерацию «${i.name}»`}
                onClick={() => setToClose(i)}
              >
                закрыть
              </button>
            )}
          </span>
        ))}
        {closed.length > 0 && (
          <>
            <span className="muted small">Закрытые:</span>
            {closed.map((i) => (
              <button key={i.id} className="link" title={i.goal} onClick={() => onReport(i)}>
                {i.name}
              </button>
            ))}
          </>
        )}
        {canEdit && !adding && (
          <button className="link" onClick={() => setAdding(true)}>
            + итерация
          </button>
        )}
      </div>

      {adding && (
        <form
          className="row row--tight"
          onSubmit={(e) => {
            e.preventDefault()
            const form = e.currentTarget
            const data = new FormData(form)
            const name = String(data.get('name') ?? '').trim()
            if (!name) return
            act(
              api.createIteration(boardId, {
                name,
                goal: String(data.get('goal') ?? ''),
                startsOn: String(data.get('startsOn') ?? ''),
                endsOn: String(data.get('endsOn') ?? ''),
              }),
            )
            setAdding(false)
          }}
        >
          <input
            name="name"
            autoFocus
            aria-label="Название итерации"
            placeholder="Название"
            required
          />
          <input name="startsOn" type="date" required aria-label="Начало" />
          <input name="endsOn" type="date" required aria-label="Конец" />
          <input name="goal" aria-label="Цель итерации" placeholder="Цель" />
          <button type="submit" aria-label="Завести итерацию">
            Завести
          </button>
          <Button kind="quiet" type="button" onClick={() => setAdding(false)}>
            Отмена
          </Button>
        </form>
      )}
    </div>
  )
}
