import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { monitorForElements } from '@atlaskit/pragmatic-drag-and-drop/element/adapter'
import { autoScrollForElements } from '@atlaskit/pragmatic-drag-and-drop-auto-scroll/element'
import { extractClosestEdge } from '@atlaskit/pragmatic-drag-and-drop-hitbox/closest-edge'
import { flowIssues } from '../entities/board/model.ts'
import { api } from '../shared/api/index.ts'
import type {
  BoardAccess as Access,
  BoardInfo,
  Column,
  EstimateUnit,
  Iteration,
} from '../shared/api/index.ts'
import { CardPanel } from '../features/board/CardPanel.tsx'
import { Flow } from '../features/flow/Flow.tsx'
import { Appearance } from '../shared/ui/Appearance.tsx'
import { BoardSkeleton, EmptyState, ErrorState } from '../shared/ui/states.tsx'
import { Button } from '../shared/ui/Button.tsx'
import { FilterBar } from '../features/board/FilterBar.tsx'
import { EMPTY, filtersToQuery, isEmpty, matches, parseFilters } from '../features/board/filters.ts'
import type { Filters } from '../features/board/filters.ts'
import { boardPath, navigate, setQuery, useQuery } from '../shared/router/index.ts'
import { Views } from '../features/board/Views.tsx'
import { Palette, paletteHint, usePaletteHotkey } from '../features/board/Palette.tsx'
import type { Command } from '../features/board/Palette.tsx'
import { useCollapsedColumns } from '../features/board/useCollapsed.ts'
import { nextCard } from '../features/board/navigation.ts'
import { childrenOf, parentsOf } from '../entities/card/model.ts'
import { NARROW, useMedia } from '../shared/lib/useMedia.ts'
import {
  GROUPING_NAMES,
  groupingToQuery,
  groupsOf,
  parseGrouping,
} from '../features/board/grouping.ts'
import type { Grouping } from '../features/board/grouping.ts'
import { useToast } from '../shared/ui/Toast.tsx'
import {
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

export function Board({
  boardId,
  cardId,
  onCard,
  unit,
  meId,
  onBack,
}: {
  boardId: string
  /** Какая карточка открыта — приходит из адреса, а не хранится здесь:
   *  ссылку на карточку должно быть можно прислать. */
  cardId: string | null
  onCard: (cardId: string | null) => void
  unit: EstimateUnit
  meId: string
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
  const setFilters = useCallback(
    (next: Filters) => setQuery(filtersToQuery(next, query), { replace: true }),
    [query],
  )

  const { base, order: fullOrder, moveCard } = board

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
  const { order, hidden } = useMemo(() => {
    if (!base || isEmpty(filters)) return { order: fullOrder, hidden: 0 }
    const context = {
      labelsOf: (cardId: string) => base.cardLabels[cardId] ?? [],
      assigneesOf: (cardId: string) => base.cardAssignees[cardId] ?? [],
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
    return { order: next, hidden }
  }, [base, fullOrder, filters])

  // Группировка — тоже состояние адреса: сгруппированный вид посылают
  // ссылкой наравне с отфильтрованным.
  const grouping = useMemo(() => parseGrouping(query), [query])
  const setGrouping = useCallback(
    (next: Grouping) => setQuery(groupingToQuery(next, query), { replace: true }),
    [query],
  )
  const groups = useMemo(
    () => (base ? groupsOf(base, order, grouping) : []),
    [base, order, grouping],
  )

  // Что можно найти и что можно сделать — в одном списке: человек,
  // набрав «мет», одинаково может иметь в виду карточку со словом
  // «метка» и команду «сгруппировать по меткам».
  const commands = useMemo((): Command[] => {
    if (!base) return []
    const cards: Command[] = Object.values(base.cards).map((card) => ({
      id: `card-${card.id}`,
      title: card.title,
      hint: base.columns[card.columnId]?.name,
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
  const { assignCard: assign, toggleLabel: label, renameCard: rename, archiveCard: archive } = board
  const assignCard = useCallback(
    (cardId: string, userId: string, on: boolean) => void assign(cardId, userId, on),
    [assign],
  )
  const toggleLabel = useCallback(
    (cardId: string, labelId: string, on: boolean) => void label(cardId, labelId, on),
    [label],
  )
  const renameCard = useCallback(
    (cardId: string, title: string) => void rename(cardId, title),
    [rename],
  )
  const archiveCard = useCallback((cardId: string) => void archive(cardId), [archive])
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
    return (
      <div className="board-screen">
        <ErrorState
          what="загрузить доску"
          error={board.loadError}
          onRetry={() => void board.reload()}
        />
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

  const renderColumns = (groupOrder: Record<string, string[]>) =>
    shownColumns.map((columnId) => (
      <ColumnView
        key={columnId}
        name={base.columns[columnId].name}
        columnId={columnId}
        column={base.columns[columnId]}
        cardIds={groupOrder[columnId] ?? []}
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
        children={children}
        onLabel={toggleLabel}
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
      />
    ))

  return (
    <div className="board-screen">
      <header className="board-header">
        <button className="btn btn--quiet" onClick={onBack}>
          <ChevronLeftIcon />
          Все доски
        </button>
        <h1>{base.info.name}</h1>
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
          hidden={hidden}
          onChange={setFilters}
        />
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
            <Iterations boardId={boardId} iterations={base.iterations} onChanged={board.reload} />
          </div>
          <button className="btn btn--quiet" onClick={() => setPalette(true)}>
            <SearchIcon />
            Найти
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
            Поток
          </button>
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
                <span className="muted small">{(order[columnId] ?? []).length}</span>
              </button>
            )
          })}
        </div>
      )}

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
            {renderColumns(group.order)}
            {grouping === 'none' && (
              <NewColumn onCreate={(name) => void board.createColumn(name)} />
            )}
          </div>
        </div>
      ))}

      {showFlow && (
        <Flow
          boardId={boardId}
          sleDays={base.info.sleDays}
          sleProbability={base.info.sleProbability}
          onClose={() => setShowFlow(false)}
          onPromise={board.reload}
        />
      )}

      <Palette open={palette} commands={commands} onClose={() => setPalette(false)} />

      {showAccess && (
        <AccessPanel
          boardId={boardId}
          canEdit
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
          canEdit
          onClose={() => setOpenCard(null)}
          onDescribe={(id, text) => void board.describeCard(id, text)}
          onEstimate={(id, value) => void board.estimateCard(id, value)}
          onOpenCard={showCard}
          onAssign={assignCard}
          subtaskBoards={subtaskBoards}
          onSubtask={(parentCardId, title, toBoard) =>
            void board.createSubtask(parentCardId, title, undefined, toBoard)
          }
          onLink={(from, to, kind) => void board.linkCards(from, to, kind)}
          onUnlink={(from, to, kind) => void board.unlinkCards(from, to, kind)}
          onBlock={(id, reason) => void board.blockCard(id, reason)}
          onUnblock={(id) => void board.unblockCard(id)}
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
        placeholder="Название колонки"
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => e.key === 'Escape' && setAdding(false)}
      />
      <button type="submit">Создать</button>
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
  iterations,
  onChanged,
}: {
  boardId: string
  iterations: Iteration[]
  onChanged: () => void
}) {
  const [adding, setAdding] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const open = iterations.filter((i) => i.closedAt === null)

  const act = (p: Promise<unknown>) => {
    setError(null)
    p.then(onChanged).catch((e) => setError(e instanceof Error ? e.message : 'Не получилось'))
  }

  return (
    <div className="iterations">
      {error && <p className="error">{error}</p>}
      <div className="row row--tight">
        {open.length === 0 && !adding && <span className="muted small">Итераций нет</span>}
        {open.map((i) => (
          <span key={i.id} className="mark" title={i.goal}>
            {i.name} · {i.startsOn}—{i.endsOn} · {i.cardCount}
            <button
              className="link"
              onClick={() => {
                if (window.confirm(`Закрыть «${i.name}»? Состав замрёт, вернуть нельзя.`)) {
                  act(api.closeIteration(boardId, i.id))
                }
              }}
            >
              закрыть
            </button>
          </span>
        ))}
        {!adding && (
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
          <input name="name" autoFocus placeholder="Название" required />
          <input name="startsOn" type="date" required aria-label="Начало" />
          <input name="endsOn" type="date" required aria-label="Конец" />
          <input name="goal" placeholder="Цель" />
          <button type="submit">Создать</button>
          <button type="button" className="link" onClick={() => setAdding(false)}>
            Отмена
          </button>
        </form>
      )}
    </div>
  )
}
