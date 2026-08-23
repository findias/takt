.DEFAULT_GOAL := help
SHELL := /bin/bash

DEV_DB_URL ?= postgres://board:board@localhost:55432/board?sslmode=disable
# Адрес для того, кто умеет заводить базы. Нужен одной проверке — той,
# что применяет цепочку миграций с нуля в своей, только что заведённой
# базе. Роль приложения этого не умеет намеренно (nocreatedb), и права
# ей не выдаются: заводит базу другая роль, а миграции в ней идут уже
# под ролью приложения — иначе проверялась бы не та цепочка.
DEV_ADMIN_DB_URL ?= postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable
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

# Смотреть на пустой интерфейс бесполезно: почти всякая ошибка вёрстки
# видна только на настоящей длине текста и настоящем числе меток.
# Заводит организацию, людей, три доски всех видов, карточки со всеми
# свойствами, итерации, архив и историю на три недели назад.
.PHONY: demo
demo: migrate ## Наполнить базу данными для работы над видом
	DATABASE_URL="$(DEV_DB_URL)" go run ./cmd/board demo

# Стенд с нуля одной командой: снести базу, поднять, мигрировать,
# наполнить, собрать фронтенд. Дальше — make run.
.PHONY: stand
stand: ## Собрать стенд с нуля вместе с данными (сносит базу)
	$(MAKE) db-stop
	$(MAKE) demo
	$(MAKE) web
	@echo
	@echo "стенд готов. дальше: make run → http://localhost:$(DEV_PORT)"
	@echo "вход: anna@example.test / parol12345"

# Сборка клиента перед запуском — не забота о чистоте, а починка
# ловушки: сервер отдаёт то, что лежит в web/dist, и после правки
# фронтенда стенд молча показывает вчерашний клиент. Новый сервер
# со старым клиентом даёт не ошибку, а пустой экран доски. Сборка
# идёт около двух секунд, а без неё за день теряются десятки минут
# на «правка не работает».
.PHONY: run
run: migrate web-dist ## Запустить сервер разработки (API + свежая статика)
	DATABASE_URL="$(DEV_DB_URL)" LISTEN_ADDR=":$(DEV_PORT)" \
	BASE_URL="http://localhost:$(DEV_PORT)" WEB_DIR=./web/dist \
	SIGNUP=open \
	go run ./cmd/board serve

.PHONY: web
web: ## Собрать фронтенд (вместе с установкой зависимостей)
	cd web && npm install && npm run build

# Отдельно от `web`: установка зависимостей нужна раз, а сборка —
# перед каждым запуском, и тянуть npm install в каждый `make run`
# значит платить за него секундами на ровном месте.
.PHONY: web-dist
web-dist: ## Пересобрать клиент (без установки зависимостей)
	cd web && npm run build

.PHONY: web-dev
web-dev: ## Vite с горячей перезагрузкой (запросы к API проксируются на $(DEV_PORT))
	cd web && npm run dev

# --- проверки ---

.PHONY: test
test: ## Быстрые тесты без базы
	go test ./...

.PHONY: test-integration
test-integration: db migrate ## Все тесты, включая работающие с настоящей базой
	TEST_DATABASE_URL="$(DEV_DB_URL)" TEST_ADMIN_DATABASE_URL="$(DEV_ADMIN_DB_URL)" \
	  go test ./... -count=1

.PHONY: test-web
test-web: ## Тесты клиентской модели доски
	cd web && npm test

# Сквозные сценарии отделены от check намеренно. Им нужен установленный
# браузер и поднятый сервер с базой, то есть машина, где всё это есть;
# check же обязан проходить везде. Ломать общую проверку из-за
# отсутствующего Chrome — верный способ приучить не запускать её вовсе.
# Снимки всех экранов демонстрационного стенда. Не проверка: файлы
# складываются в web/screenshots и разбираются глазами.
.PHONY: screens
screens: demo ## Снять все экраны стенда в web/screenshots
	cd web && npm run screens
	@echo "снимки: web/screenshots"

.PHONY: e2e
e2e: db migrate ## Сквозные сценарии в настоящем браузере
	cd web && npm run test:e2e

# --- документация ---

# HTML собирается из тех же `.md`, что читают в репозитории: второй
# набор текстов, набранный руками, разошёлся бы с первым в тот же день
# и разошёлся бы молча. Страницы самодостаточны — ни одной внешней
# ссылки: их читают в закрытом контуре и из папки на диске.
.PHONY: docs
docs: ## Собрать документацию в HTML (docs/html)
	go run ./cmd/docs

# --- безопасность ---

# Отдельной целью, а не частью `make check`, по той же причине, что
# сквозные и нагрузочные: этим проверкам нужна сеть. govulncheck
# и npm audit сверяются с базами уязвимостей, а `make check` обязан
# проходить и в закрытом контуре, где сети нет вовсе.
#
# Три инструмента отвечают на три разных вопроса, и подменять один
# другим нельзя:
#   govulncheck — есть ли в зависимостях и стандартной библиотеке
#                 известные дыры, и вызываем ли мы их на самом деле;
#   gosec       — обычные ошибки в коде на Go;
#   npm audit   — то же про зависимости клиента.
# Свои проверки — про этот продукт, и живут они в `make check`
# (internal/security), потому что сети им не нужно.
SECURITY_TOOLS = $(shell go env GOPATH)/bin

.PHONY: security
security: ## Проверки безопасности: уязвимости зависимостей и статический разбор
	@command -v $(SECURITY_TOOLS)/govulncheck >/dev/null \
	  || go install golang.org/x/vuln/cmd/govulncheck@latest
	@command -v $(SECURITY_TOOLS)/gosec >/dev/null \
	  || go install github.com/securego/gosec/v2/cmd/gosec@latest
	$(SECURITY_TOOLS)/govulncheck ./...
	$(SECURITY_TOOLS)/gosec -quiet -exclude-generated ./...
	cd web && npm audit --audit-level=high

.PHONY: load
load: db migrate ## Поведение под нагрузкой (идёт минуты)
	TEST_DATABASE_URL="$(DEV_DB_URL)" go test -tags load -count=1 -v \
	  -run 'Scales|Crowd|Neighbour|ManyOpen|RateLimit' ./internal/board/ ./internal/httpapi/

.PHONY: check
check: ## Форматирование, vet и все тесты (кроме сквозных и нагрузочных)
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...
	# Нагрузочные проверки под тегом сборки: обычный vet их не видит,
	# и без этой строки они молча перестанут собираться.
	go vet -tags load ./...
	$(MAKE) test-integration
	cd web && npx tsc -b && npm test

# Отрисовщик страницы описания едет в бинарнике сжатым. Цель нужна
# редко — только чтобы поднять версию, — но без неё никто не вспомнит,
# откуда файл взялся и почему в нём правка.
#
# Правка одна: подпись «powered by» просит логотип с чужого адреса,
# а страница обязана открываться там, где интернета нет. Логотип
# заменён на пустую точку; политика CSP запрещает то же самое ещё раз,
# на случай, если в новой версии появится второй такой адрес.
REDOC_VERSION ?= 2.5.3

.PHONY: docs-bundle
docs-bundle: ## Обновить встроенный отрисовщик страницы описания
	curl -sL --fail -o /tmp/redoc-$(REDOC_VERSION).js \
	  "https://cdn.jsdelivr.net/npm/redoc@$(REDOC_VERSION)/bundles/redoc.standalone.js"
	sed 's|https://cdn.redoc.ly/redoc/logo-mini.svg|data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==|' \
	  /tmp/redoc-$(REDOC_VERSION).js \
	  | gzip -9 > internal/httpapi/docs/redoc-$(REDOC_VERSION).js.gz
	@echo "встроен redoc $(REDOC_VERSION); если версия сменилась — поправьте go:embed в openapi.go"

# --- сборка ---

# Версия вшивается в бинарник, а не читается из файла: файл в образе
# можно подменить, а версия обязана быть тем же артефактом, что и код.
# `git describe` даёт тег, если он есть, и хеш, если тегов ещё нет, —
# ответ на «какая у меня версия» в обоих случаях.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo неизвестна)
VERSION_LDFLAGS = -X github.com/konkov/agile/internal/version.Value=$(VERSION)

.PHONY: build
build: web ## Собрать бинарник в bin/board
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $(VERSION_LDFLAGS)" -o bin/board ./cmd/board

.PHONY: image
image: ## Собрать docker-образ
	docker build --build-arg VERSION=$(VERSION) -t board:dev .

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

# --- закрытый контур ---

# Установка не должна требовать доступа в интернет. Всё, что для неё
# нужно, — образ и чарт; ни того, ни другого не собрать на месте, потому
# что сборка тянет зависимости. Поэтому собирается здесь, увозится файлом.
#
# Контрольные суммы обязательны: в закрытый контур файл едет через
# посредников, и «тот ли это образ» — вопрос, который зададут.
BUNDLE_VERSION ?= $(shell git describe --tags --always --dirty)
BUNDLE_DIR     ?= dist/bundle-$(BUNDLE_VERSION)

.PHONY: bundle
bundle: ## Собрать комплект для установки без доступа в интернет
	@mkdir -p "$(BUNDLE_DIR)"
	docker build --build-arg VERSION=$(BUNDLE_VERSION) -t board:$(BUNDLE_VERSION) .
	docker save board:$(BUNDLE_VERSION) | gzip > "$(BUNDLE_DIR)/board-image.tar.gz"
	# Версия чарта не переписывается версией сборки: helm требует semver,
	# а описание сборки им быть не обязано. Комплект с чартом связывает
	# тег образа, передаваемый при установке.
	helm package deploy/helm/board --app-version "$(BUNDLE_VERSION)" -d "$(BUNDLE_DIR)" >/dev/null
	go run ./cmd/docs
	cp README.md CHANGELOG.md "$(BUNDLE_DIR)/"
	cp -r docs/html "$(BUNDLE_DIR)/docs"
	echo "$(BUNDLE_VERSION)" > "$(BUNDLE_DIR)/VERSION"
	cd "$(BUNDLE_DIR)" && sha256sum * > SHA256SUMS
	@echo
	@echo "комплект собран: $(BUNDLE_DIR)"
	@echo "на месте:"
	@echo "  sha256sum -c SHA256SUMS"
	@echo "  docker load < board-image.tar.gz    # либо skopeo copy в своё зеркало"
	@echo "  helm install board board-*.tgz --set image.tag=$(BUNDLE_VERSION) \\"
	@echo "    --set baseURL=... --set database.existingSecret=board-db"
