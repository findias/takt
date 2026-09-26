#!/usr/bin/env bash
# Обновление с прошлого выпуска — по-настоящему, а не по тексту миграций
# (PROMPT-TESTING.md, уровень 1).
#
# Что проверяется и почему в таком порядке:
#   1. база и данные прошлого выпуска — их заводит ЕГО бинарник из
#      образа выпуска, а не нынешний код: у заказчика лежит именно это;
#   2. миграции нового бинарника поверх — так идёт хук pre-upgrade;
#   3. СТАРЫЙ бинарник на новой схеме — окно между концом миграций
#      и выкаткой подов; здесь обязано работать всё старое, включая запись;
#   4. новый бинарник на той же базе: doctor без жалоб и та же дымовая
#      проверка;
#   5. ни одной потерянной строки: счёт таблиц до и после;
#   6. откат: снова старый бинарник на новой схеме — helm rollback
#      возвращает поды, а не схему.
#
# База — отдельная, в том же Postgres, что и у разработки (make db):
# создаётся ролью postgres, удаляется в конце и при любом падении.
# Сеть нужна один раз — стянуть образ прошлого выпуска; поэтому прогон
# не входит в make check, а идёт своим workflow и перед выпуском.
set -euo pipefail

PREVIOUS=${PREVIOUS:?нужен тег прошлого выпуска, например v0.2.3}
IMAGE=${UPGRADE_IMAGE:-ghcr.io/findias/takt}:${PREVIOUS}
ADMIN_DB_URL=${ADMIN_DB_URL:?нужна строка подключения роли postgres}
APP_DB_URL=${APP_DB_URL:?нужна строка подключения роли приложения}
DB_CONTAINER=${DB_CONTAINER:-takt-dev-db}
PORT=${UPGRADE_PORT:-8096}
DBNAME=takt_upgrade

# Та же строка приложения, но к отдельной базе: роль, пароль и параметры
# остаются, меняется только имя.
base="${APP_DB_URL%%\?*}"; query=""
[ "$base" != "$APP_DB_URL" ] && query="?${APP_DB_URL#*\?}"
URL="${base%/*}/${DBNAME}${query}"
OLD_NAME=takt-upgrade-old
TMP=$(mktemp -d)
NEW_PID=""

say() { printf '\n== %s\n' "$*"; }
sql() { docker exec -i "$DB_CONTAINER" psql -v ON_ERROR_STOP=1 -qAt -U postgres -d "$1"; }

cleanup() {
  [ -n "$NEW_PID" ] && kill "$NEW_PID" 2>/dev/null || true
  docker rm -f "$OLD_NAME" >/dev/null 2>&1 || true
  echo "drop database if exists \"$DBNAME\" with (force);" | sql postgres || true
  rm -rf "$TMP"
}
trap cleanup EXIT

wait_ready() {
  for _ in $(seq 1 120); do
    if curl -sf "http://127.0.0.1:$PORT/readyz" >/dev/null 2>&1; then return 0; fi
    sleep 0.5
  done
  echo "сервер на :$PORT не поднялся за минуту" >&2
  return 1
}

old() { docker run --rm --network host -e DATABASE_URL="$URL" "$IMAGE" "$@"; }

old_serve() {
  docker run -d --name "$OLD_NAME" --network host \
    -e DATABASE_URL="$URL" -e LISTEN_ADDR=":$PORT" \
    -e BASE_URL="http://127.0.0.1:$PORT" -e SIGNUP=closed "$IMAGE" serve >/dev/null
  wait_ready || { docker logs "$OLD_NAME" | tail -20; return 1; }
}

old_stop() { docker rm -f "$OLD_NAME" >/dev/null; }

# Дымовая проверка против старого сервера: при отказе — его лог, иначе
# видно только «внутренняя ошибка», а какая миграция её вызвала — нет.
old_smoke() {
  if ! go run ./cmd/upgrade-smoke -url "http://127.0.0.1:$PORT" -label "$1"; then
    echo "--- лог старого сервера ---" >&2
    docker logs --tail 30 "$OLD_NAME" >&2
    return 1
  fi
}

counts() {
  # Порядок фиксирован — строки сравниваются как текст.
  printf '%s\n' \
    "select 'orgs', count(*) from orgs union all
     select 'users', count(*) from users union all
     select 'memberships', count(*) from memberships union all
     select 'teams', count(*) from teams union all
     select 'boards', count(*) from boards union all
     select 'board_columns', count(*) from board_columns union all
     select 'cards', count(*) from cards union all
     select 'card_events', count(*) from card_events
     order by 1;" | sql "$DBNAME"
}

say "прошлый выпуск $PREVIOUS: образ $IMAGE"
docker pull -q "$IMAGE" >/dev/null
echo "образ отвечает: $(old version)"

say "1. база и данные прошлого выпуска — его бинарником"
owner=$(sed -E 's|^[a-z]+://([^:/@]+).*|\1|' <<<"$APP_DB_URL")
{ echo "drop database if exists \"$DBNAME\" with (force);"
  echo "create database \"$DBNAME\" owner \"$owner\";"; } | sql postgres
old migrate
old demo
counts > "$TMP/before.txt"
cat "$TMP/before.txt"

say "2. миграции нового бинарника поверх (хук pre-upgrade)"
go build -ldflags="${NEW_LDFLAGS:-}" -o "$TMP/takt" ./cmd/takt
DATABASE_URL="$URL" "$TMP/takt" migrate

say "3. старый бинарник на новой схеме — окно выкладки"
old_serve
old_smoke "старый на новой схеме"
old_stop

say "4. новый бинарник на той же базе"
DATABASE_URL="$URL" "$TMP/takt" doctor
DATABASE_URL="$URL" LISTEN_ADDR=":$PORT" BASE_URL="http://127.0.0.1:$PORT" SIGNUP=closed \
  WEB_DIR=web/dist "$TMP/takt" serve >"$TMP/new.log" 2>&1 &
NEW_PID=$!
wait_ready || { tail -20 "$TMP/new.log"; exit 1; }
go run ./cmd/upgrade-smoke -url "http://127.0.0.1:$PORT" -label "новый"
kill "$NEW_PID"; wait "$NEW_PID" 2>/dev/null || true; NEW_PID=""

say "6. откат: старый бинарник снова на новой схеме"
old_serve
old_smoke "после отката"
old_stop

say "5. ни одной потерянной строки"
counts > "$TMP/after.txt"
cat "$TMP/after.txt"
# Три дымовые проверки создали по карточке; всё остальное — как было.
# Событий потока стало больше — это и есть след записи, меньше быть
# не может.
fail=0
while IFS='|' read -r table before; do
  after=$(grep "^$table|" "$TMP/after.txt" | cut -d'|' -f2)
  case "$table" in
    cards)       want=$((before + 3)); [ "$after" -eq "$want" ] || { echo "cards: было $before, стало $after, ждали $want"; fail=1; } ;;
    card_events) [ "$after" -ge "$before" ] || { echo "card_events: было $before, стало $after"; fail=1; } ;;
    *)           [ "$after" -eq "$before" ] || { echo "$table: было $before, стало $after"; fail=1; } ;;
  esac
done < "$TMP/before.txt"
[ "$fail" -eq 0 ] || { echo "данные разошлись после обновления" >&2; exit 1; }

say "обновление $PREVIOUS → $("$TMP/takt" version) прошло: старый работает на новой схеме, новый — на старых данных, откат — тоже"
