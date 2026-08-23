# Один образ на все профили развёртывания. Compose и Helm получают его без
# пересборки — разница только в переменных окружения. Второй Dockerfile
# завёл бы расхождение между «на сервере» и «в кубере» в тот же день.

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci || npm install
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
# Версия приезжает снаружи и вшивается линковщиком. Сборка без неё
# работает, но отвечает «версия не задана» — и это честнее, чем
# показывать выдуманное число.
ARG VERSION=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/konkov/agile/internal/version.Value=${VERSION}" \
    -o /out/board ./cmd/board

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 board
WORKDIR /app
COPY --from=build /out/board /app/board
COPY --from=web /src/web/dist /app/web/dist
ENV WEB_DIR=/app/web/dist
USER board
EXPOSE 8080
ENTRYPOINT ["/app/board"]
CMD ["serve"]
