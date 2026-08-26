# Один образ на все профили развёртывания. Compose и Helm получают его без
# пересборки — разница только в переменных окружения. Второй Dockerfile
# завёл бы расхождение между «на сервере» и «в кубере» в тот же день.
#
# Основание итогового слоя приходит аргументом. Причина та же: заказчику
# бывает нужен не alpine, а glibc-система или предписанный ему
# дистрибутив, и второй Dockerfile под каждый разошёлся бы с первым
# так же быстро.
# Сборочные слои от основания не зависят вовсе: бинарник статический
# (CGO_ENABLED=0), клиент — просто файлы.
ARG RUNTIME_BASE=alpine:3.21

# Клиент собирается на машине сборщика, а не под целевую архитектуру:
# на выходе файлы, и они одинаковы везде. Без --platform buildx поднял бы
# node под эмуляцией ради того же результата.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci || npm install
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
# Версия приезжает снаружи и вшивается линковщиком. Сборка без неё
# работает, но отвечает «версия не задана» — и это честнее, чем
# показывать выдуманное число.
ARG VERSION=""
# Кросс-компиляция вместо эмуляции: Go умеет собирать под чужую
# архитектуру сам, и это минуты против десятков минут под QEMU.
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X github.com/findias/takt/internal/version.Value=${VERSION}" \
    -o /out/takt ./cmd/takt

FROM ${RUNTIME_BASE}
# Оба шага разветвлены по наличию команды, а не по имени основания:
# имя приходит аргументом и заранее не известно.
RUN set -eu; \
    # Сертификаты и часовые пояса: в alpine их нет, в debian-подобных
    # они уже стоят. Ставить поверх — лишний слой и лишний поход в сеть.
    if command -v apk >/dev/null 2>&1; then \
      apk add --no-cache ca-certificates tzdata; \
    fi; \
    # Пользователь: busybox adduser и shadow useradd — разные команды
    # с несовместимыми ключами, и есть они в разных системах.
    #
    # Группа заводится явным номером, а не выдаётся системой: чарт
    # задаёт runAsGroup: 10001 и fsGroup: 10001, и образ, у которого
    # группа своя, разошёлся бы с ним молча. На debian-подобных
    # `useradd --system` без --gid выдал бы 999.
    if command -v useradd >/dev/null 2>&1; then \
      groupadd --system --gid 10001 takt; \
      useradd --system --uid 10001 --gid 10001 \
        --no-create-home --shell /usr/sbin/nologin takt; \
    else \
      addgroup -g 10001 takt; \
      adduser -D -u 10001 -G takt takt; \
    fi
WORKDIR /app
COPY --from=build /out/takt /app/takt
COPY --from=web /src/web/dist /app/web/dist
ENV WEB_DIR=/app/web/dist
USER takt
EXPOSE 8080
ENTRYPOINT ["/app/takt"]
CMD ["serve"]
