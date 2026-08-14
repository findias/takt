.DEFAULT_GOAL := help
SHELL := /bin/bash

DEV_DB_URL ?= postgres://board:board@localhost:55432/board?sslmode=disable
DEV_PORT   ?= 8099

.PHONY: help
help: ## Показать список команд
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk -F':.*?## ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# --- разработка ---

# Роль board намеренно создаётся без прав суперпользователя: для
# суперпользователя политики Row-Level Security не действуют, и локальная
# разработка перестала бы отличаться от продакшена ровно в том месте,
# где ошибка стоит дороже всего.
.PHONY: db
db: ## Поднять локальную базу в docker (порт 55432)
	@docker start board-dev-db 2>/dev/null || \
	 docker run -d --name board-dev-db \
	   -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e APP_DB_PASSWORD=board \
	   -v "$(CURDIR)/deploy/postgres-init:/docker-entrypoint-initdb.d:ro" \
	   -p 55432:5432 postgres:16-alpine
	@until docker exec board-dev-db pg_isready -U board -d board >/dev/null 2>&1; do sleep 0.5; done
	@echo "база готова: $(DEV_DB_URL)"

.PHONY: db-stop
db-stop: ## Остановить и удалить локальную базу вместе с данными
	-docker rm -f board-dev-db

.PHONY: migrate
migrate: db ## Применить миграции к локальной базе
	DATABASE_URL="$(DEV_DB_URL)" go run ./cmd/board migrate

.PHONY: run
run: migrate ## Запустить сервер разработки (API + собранная статика)
	DATABASE_URL="$(DEV_DB_URL)" LISTEN_ADDR=":$(DEV_PORT)" \
	BASE_URL="http://localhost:$(DEV_PORT)" WEB_DIR=./web/dist \
	go run ./cmd/board serve

.PHONY: web
web: ## Собрать фронтенд
	cd web && npm install && npm run build

.PHONY: web-dev
web-dev: ## Vite с горячей перезагрузкой (запросы к API проксируются на $(DEV_PORT))
	cd web && npm run dev

# --- проверки ---

.PHONY: test
test: ## Быстрые тесты без базы
	go test ./...

.PHONY: test-integration
test-integration: db migrate ## Все тесты, включая работающие с настоящей базой
	TEST_DATABASE_URL="$(DEV_DB_URL)" go test ./... -count=1

.PHONY: test-web
test-web: ## Тесты клиентской модели доски
	cd web && npm test

.PHONY: check
check: ## Форматирование, vet и все тесты
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...
	$(MAKE) test-integration
	cd web && npx tsc -b && npm test

# --- сборка ---

.PHONY: build
build: web ## Собрать бинарник в bin/board
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/board ./cmd/board

.PHONY: image
image: ## Собрать docker-образ
	docker build -t board:dev .

.PHONY: up
up: ## Поднять весь стек через docker compose
	docker compose up --build -d
	@echo "открывайте http://localhost:$${PORT:-8080}"

.PHONY: down
down: ## Остановить стек (данные сохраняются в томах)
	docker compose down

.PHONY: logs
logs: ## Логи приложения
	docker compose logs -f app
