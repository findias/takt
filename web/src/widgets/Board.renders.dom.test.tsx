// Сколько карточек перерисовывается от изменения одной.
//
// Проверка есть потому, что мемоизация ломается молча: достаточно
// собрать обработчик прямо в разметке или написать `?? []` — и доска
// снова перерисовывается целиком, а увидеть это глазами нельзя.
// Замер на пятистах карточках показывал 120 мс на переименование одной;
// цена растёт с числом карточек, то есть заметнее всего там, где доской
// действительно пользуются.
//
// Считаем не отрисовки настоящей карточки, а срабатывания такого же
// сравнения пропсов: обёртка в memo пропускает через себя ровно те
// изменения, которые пропустил бы memo внутри — то есть проверяется
// именно стабильность приходящих пропсов.

import { memo } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Card, Column, Snapshot } from "../shared/api/index.ts";

const snapshot = vi.fn<() => Promise<Snapshot>>();
const operation = vi.fn();

vi.mock("../shared/api", async (importOriginal) => {
  const real = await importOriginal<typeof import("../shared/api")>();
  return {
    ...real,
    api: {
      ...real.api,
      snapshot,
      operation,
      events: vi.fn().mockResolvedValue({ events: [], next: null }),
      metrics: vi.fn().mockResolvedValue({}),
      comments: vi.fn().mockResolvedValue({ comments: [] }),
    },
  };
});

const renders = { count: 0 };

vi.mock("../features/board/CardView.tsx", async (importOriginal) => {
  const real =
    await importOriginal<typeof import("../features/board/CardView.tsx")>();
  const Counting = memo((props: Parameters<typeof real.CardView>[0]) => {
    renders.count += 1;
    return <real.CardView {...props} />;
  });
  return { CardView: Counting };
});

const { Board } = await import("./Board");

const COL_A = "col-a";

function column(id: string, name: string, position: string): Column {
  return {
    id,
    name,
    position,
    kind: "queue",
    isStartedPoint: false,
    isFinishedPoint: false,
    policy: "",
    wipLimit: null,
    wipLimitHard: false,
  };
}

function card(id: string, position: string, title = id): Card {
  return {
    id,
    number: `ДОСК-${id}`,
    columnId: COL_A,
    position,
    title,
    description: "",
    version: 1,
    columnEnteredAt: "2026-08-15T12:00:00Z",
    startedAt: null,
    finishedAt: null,
    outcome: null,
    estimate: null,
  };
}

const CARDS = ["первая", "вторая", "третья", "четвёртая", "пятая"];

function board(): Snapshot {
  return {
    board: {
      id: "board",
      name: "Доска",
      key: "ДОСК",
      version: 1,
      sleDays: null,
      sleProbability: 85,
    },
    columns: [column(COL_A, "Очередь", "a0")],
    cards: CARDS.map((id, i) => card(id, `a${i}`)),
    links: [],
    linked: [],
    iterations: [],
    cardIterations: {},
    fields: [],
    fieldValues: {},
    people: [],
    labels: [],
    cardLabels: {},
    cardAssignees: {},
  };
}

class FakeEventSource {
  addEventListener() {}
  close() {}
}

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  snapshot.mockReset();
  operation.mockReset();
  renders.count = 0;
  vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("изменение одной карточки не перерисовывает соседние", async () => {
  const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
  snapshot.mockResolvedValue(board());
  // Ответ сервера меняет одну карточку: ровно то, что происходит при
  // переименовании, назначении исполнителя и чужой правке из потока.
  operation.mockResolvedValue({
    version: 2,
    patch: {
      cards: [
        { ...card("первая", "a0"), title: "первая — правка", version: 2 },
      ],
    },
  });

  render(
    <Board
      boardId="board"
      cardId={null}
      onCard={() => {}}
      unit="points"
      meId="я"
      isOwner
      onBack={() => {}}
    />,
  );
  await screen.findByRole("group", { name: /Карточка «первая»/ });
  await waitFor(() => expect(renders.count).toBe(CARDS.length));

  const before = renders.count;
  await user.click(
    screen.getByRole("button", { name: /Действия карточки «первая»/ }),
  );
  await user.click(screen.getByRole("menuitem", { name: "Переименовать" }));
  await user.keyboard("{End} — правка{Enter}");

  await waitFor(() => expect(operation).toHaveBeenCalledTimes(1));
  await screen.findByRole("group", { name: /Карточка «первая — правка»/ });

  // Правится одна карточка — перерисовывается одна. Правка идёт двумя
  // шагами (сразу на экране и следом ответом сервера), поэтому
  // допускается пара отрисовок, но не пять и не десять.
  const drawn = renders.count - before;
  expect(drawn).toBeLessThanOrEqual(2);
});
