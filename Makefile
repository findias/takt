.DEFAULT_GOAL := help
SHELL := /bin/bash

DEV_DB_URL ?= postgres://takt:takt@localhost:55432/takt?sslmode=disable
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

# Роль takt намеренно создаётся без прав суперпользователя: для
# суперпользователя политики Row-Level Security не действуют, и локальная
# разработка перестала бы отличаться от продакшена ровно в том месте,
# где ошибка стоит дороже всего.
.PHONY: db
db: ## Поднять локальную базу в docker (порт 55432)
	@docker start takt-dev-db 2>/dev/null || \
	 docker run -d --name takt-dev-db \
	   -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres -e APP_DB_PASSWORD=takt \
	   -v "$(CURDIR)/deploy/postgres-init:/docker-entrypoint-initdb.d:ro" \
	   -p 55432:5432 postgres:16-alpine
	@until docker exec takt-dev-db pg_isready -U takt -d takt >/dev/null 2>&1; do sleep 0.5; done
	@echo "база готова: $(DEV_DB_URL)"

.PHONY: db-stop
db-stop: ## Остановить и удалить локальную базу вместе с данными
	-docker rm -f takt-dev-db

.PHONY: migrate
migrate: db ## Применить миграции к локальной базе
	DATABASE_URL="$(DEV_DB_URL)" go run ./cmd/takt migrate

# Смотреть на пустой интерфейс бесполезно: почти всякая ошибка вёрстки
# видна только на настоящей длине текста и настоящем числе меток.
# Заводит организацию, людей, три доски всех видов, карточки со всеми
# свойствами, итерации, архив и историю на три недели назад.
.PHONY: demo
demo: migrate ## Наполнить базу данными для работы над видом
	DATABASE_URL="$(DEV_DB_URL)" go run ./cmd/takt demo

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
	go run ./cmd/takt serve

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
# Данные, а не только схема: часть сценариев меряет вёрстку
# на демонстрационной доске — заголовки там настоящей длины, а
# на выдуманных коротких мера ничего не показывает. Прежде цель
# доводила базу до миграций и останавливалась, и на своей машине
# это сходило с рук: данные лежали от `make stand`. На чистой машине
# доски «Поставки» не было, и шесть сценариев отваливались по тайм-ауту
# клика — то есть выглядели поломкой вёрстки, которой не было.
# `make demo` идемпотентен: на наполненной базе он ничего не делает.
e2e: demo ## Сквозные сценарии в настоящем браузере
	cd web && npm run test:e2e

# --- документация ---

# HTML собирается из тех же `.md`, что читают в репозитории: второй
# набор текстов, набранный руками, разошёлся бы с первым в тот же день
# и разошёлся бы молча. Страницы самодостаточны — ни одной внешней
# ссылки: их читают в закрытом контуре и из папки на диске.
.PHONY: docs
docs: ## Собрать документацию в HTML (docs/html)
	go run ./cmd/docs

# PDF печатается из того же файла, что читают в браузере, — `all.html`.
# Браузер берётся системный, тот же, что у сквозных проверок: второй
# инструмент ради одной кнопки «печать» стоил бы установки при каждом
# `npm ci`. Здесь же проверяется обещание памятки быть одной страницей.
.PHONY: docs-pdf
docs-pdf: docs ## Напечатать документацию в PDF (docs/html/takt.pdf)
	cd web && node print-docs.mjs

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
	# Инструменты ставятся каждый раз, а не «если их нет». Оба берутся
	# @latest, и правила у них пополняются: у себя оставался тот, что
	# поставлен когда-то, в CI ставился свежий, и вчерашний зелёный
	# прогон дома означал красный там — при том же коде. На тёплом
	# кеше это секунда, а сети проверка требует и без того.
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	$(SECURITY_TOOLS)/govulncheck ./...
	# cmd/docs исключён, и это не поблажка. Все находки gosec в нём —
	# одного рода: файл читается и пишется по пути из переменной,
	# а права у HTML 0644, а не 0600. Переменная там — элемент явного
	# списка страниц в том же файле, а «документацию нельзя читать
	# никому, кроме владельца» — требование не про документацию.
	# Существеннее другое: этот инструмент не едет заказчику. Проверка
	# `internal/security` следит, чтобы так и осталось: если cmd/docs
	# когда-нибудь окажется вкомпилирован в cmd/takt, она упадёт,
	# и исключение придётся снимать.
	$(SECURITY_TOOLS)/gosec -quiet -exclude-generated -exclude-dir=cmd/docs ./...
	cd web && npm audit --audit-level=high

# --- состав поставки и машинные отчёты ---
#
# Проверка ИБ у заказчика просит не пересказ, а файлы: из чего собрано
# и что показали сканеры. Пересказ ей не годится — его не загрузить
# в свою систему и не сверить через год. Поэтому состав отдаётся
# в CycloneDX, а отчёты — в SARIF и OpenVEX: три формата, которые
# читает чужой инструмент, а не только человек.
#
# Сети эти цели требуют так же, как `security`, — оттого и стоят рядом,
# а не в `check`.
SBOM_DIR   ?= dist/sbom
REPORT_DIR ?= dist/security

.PHONY: sbom
sbom: ## Состав поставки в CycloneDX (dist/sbom)
	@mkdir -p $(SBOM_DIR)
	go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest
	# -std: версия Go — такая же часть поставки, как и модули, и дыры
	# стандартной библиотеки сверяют именно по ней.
	$(SECURITY_TOOLS)/cyclonedx-gomod app -json -licenses -std \
	  -main cmd/takt -output $(SBOM_DIR)/takt-server.cdx.json .
	# Версия клиента ставится на время сборки состава и снимается сразу:
	# в package.json её нет намеренно — версия в этом проекте вшивается
	# из git, а не читается из файла, — а инструменту она нужна, иначе
	# он не соберёт purl и остановится.
	#
	# Свой `npm sbom` тут не годится: с `--omit dev` он выбрасывает react
	# и react-dom — они достижимы ещё и через сборочные зависимости,
	# и он считает их сборочными. Состав без react описывал бы не тот
	# клиент, который едет заказчику.
	cd web && trap 'npm pkg delete version >/dev/null 2>&1' EXIT && \
	  npm pkg set version="$(patsubst v%,%,$(VERSION))" >/dev/null && \
	  npx --yes @cyclonedx/cyclonedx-npm@latest --omit dev \
	    --output-format JSON --output-file "../$(SBOM_DIR)/takt-web.cdx.json"
	@echo "состав поставки: $(SBOM_DIR)"

.PHONY: security-report
security-report: ## Отчёты сканеров машинным форматом (dist/security)
	@mkdir -p $(REPORT_DIR)
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/securego/gosec/v2/cmd/gosec@latest
	# Отчёт пишется и тогда, когда находки есть, — он затем и нужен.
	# Но код возврата сохраняется до конца: цель, отдающая ноль при
	# находках, врёт тому, кто её запустил в своём конвейере.
	#
	# Имена переменных латиницей: bash кириллических не берёт вовсе.
	#
	# У gosec здесь нет `-quiet`, который стоит в `security`: с ним он
	# не пишет и файла тоже, а отчёт пустым файлом — худший вид отчёта.
	# Болтовню он уводит в свой лог, рядом с отчётом.
	#
	# OpenVEX рядом с SARIF не для полноты: это машинный ответ на вопрос
	# «уязвимость в списке есть, а вы её вызываете?». Иначе на него
	# отвечают перепиской, и каждый заказчик заново.
	@status=0; \
	  $(SECURITY_TOOLS)/govulncheck -format sarif ./... \
	    > "$(REPORT_DIR)/govulncheck.sarif" || status=1; \
	  $(SECURITY_TOOLS)/govulncheck -format openvex ./... \
	    > "$(REPORT_DIR)/govulncheck.openvex.json" || status=1; \
	  $(SECURITY_TOOLS)/gosec -exclude-generated -exclude-dir=cmd/docs \
	    -log "$(REPORT_DIR)/gosec.log" -fmt sarif -out "$(REPORT_DIR)/gosec.sarif" \
	    ./... || status=1; \
	  (cd web && npm audit --audit-level=high --json) \
	    > "$(REPORT_DIR)/npm-audit.json" || status=1; \
	  echo "отчёты сканеров: $(REPORT_DIR)"; \
	  exit $$status

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
VERSION_LDFLAGS = -X github.com/findias/takt/internal/version.Value=$(VERSION)

.PHONY: build
build: web binary ## Собрать бинарник в bin/takt (вместе с клиентом)

# Отдельно от клиента: клиент в бинарник не вшивается — он лежит рядом
# и отдаётся по WEB_DIR, — а пересборке ради сверки байтов npm ни к чему.
# Рычаг при этом один: флаги сборки написаны здесь, и сверяющая себя
# сборка берёт их отсюда же, а не переписывает у себя.
.PHONY: binary
binary: ## Собрать только бинарник, без клиента
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $(VERSION_LDFLAGS)" -o bin/takt ./cmd/takt

# Основания итогового слоя. Их два, и это не про размер образа:
# alpine — умолчание и самый маленький; debian — glibc, куда чаще
# берут туда, где к musl относятся с подозрением.
#
# Версии закреплены, а не `latest`: основание, меняющееся само,
# превращает пересборку старого тега в другой образ.
BASE_alpine ?= alpine:3.21
BASE_debian ?= debian:12-slim

PLATFORMS_alpine ?= linux/amd64,linux/arm64
PLATFORMS_debian ?= linux/amd64,linux/arm64

BASE ?= alpine
BASES ?= alpine debian

# Основание можно задать и напрямую, минуя список имён: образ, который
# нужен одному заказчику, незачем заводить именем в общей сборке.
# Имя из BASE идёт в метку, поэтому его задают рядом — иначе чужое
# основание уедет с меткой `dev-alpine` и найти его потом будет нечем:
#   make image BASE=своё RUNTIME_BASE=свой.реестр/образ:тег
RUNTIME_BASE ?= $(BASE_$(BASE))
# У основания без объявленного списка платформ — одна, своя же машина.
PLATFORMS ?= $(or $(PLATFORMS_$(BASE)),linux/amd64)

# Куда публикуем. Без значения цель не запускается: молча уехать
# не туда хуже, чем не уехать вовсе.
IMAGE_REPO ?=

.PHONY: image
image: ## Собрать docker-образ (BASE=alpine|debian или RUNTIME_BASE=...)
	@test -n "$(RUNTIME_BASE)" \
	  || { echo "неизвестное основание «$(BASE)»: бывают $(BASES)."; \
	       echo "своё задаётся напрямую: make image RUNTIME_BASE=образ:тег"; exit 1; }
	docker build --build-arg VERSION=$(VERSION) \
	  --build-arg RUNTIME_BASE=$(RUNTIME_BASE) \
	  -t takt:dev-$(BASE) $(if $(filter alpine,$(BASE)),-t takt:dev,) .

.PHONY: images
images: ## Собрать образы на всех основаниях (локально, без публикации)
	@for b in $(BASES); do $(MAKE) --no-print-directory image BASE=$$b || exit 1; done
	@echo
	@docker images --filter=reference='takt:dev*' \
	  --format 'table {{.Repository}}:{{.Tag}}\t{{.Size}}'

.PHONY: image-push
image-push: ## Опубликовать образы в реестр (IMAGE_REPO=..., нужен docker login)
	@test -n "$(IMAGE_REPO)" || { \
	  echo "не задан IMAGE_REPO — куда публиковать."; \
	  echo "пример: make image-push IMAGE_REPO=docker.io/имя/takt"; exit 1; }
	@case "$(VERSION)" in *dirty*|неизвестна) \
	  echo "версия «$(VERSION)»: в реестр едет только собранное из чистого дерева"; \
	  exit 1;; esac
	@docker buildx inspect takt >/dev/null 2>&1 || docker buildx create --name takt
	@for b in $(BASES); do \
	  $(MAKE) --no-print-directory image-push-one BASE=$$b || exit 1; \
	done

# Отдельной целью, а не строкой в цикле: значения оснований и платформ —
# переменные make, и в цикле оболочки их не достать.
.PHONY: image-push-one
image-push-one:
	@test -n "$(RUNTIME_BASE)" \
	  || { echo "неизвестное основание «$(BASE)»: бывают $(BASES)"; exit 1; }
	docker buildx build --push --builder takt \
	  --platform $(PLATFORMS) \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg RUNTIME_BASE=$(RUNTIME_BASE) \
	  -t $(IMAGE_REPO):$(VERSION)-$(BASE) \
	  $(if $(filter alpine,$(BASE)),-t $(IMAGE_REPO):$(VERSION) -t $(IMAGE_REPO):latest,) \
	  .

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

# Комплект для установки из бинарника: то, что распаковывают в /opt/takt.
# Отдельно от образа, потому что ставят и так тоже — там, где docker
# не ставят принципиально, а systemd есть всегда.
#
# Внутрь кладётся и собранный клиент: без него приложение поднимется
# и будет отвечать API, а экран останется белым — поломка, которую
# ищут в браузере, а причина в поставке.
.PHONY: tarball
tarball: build ## Собрать архив для установки из бинарника (dist/)
	@rm -rf "$(TARBALL_DIR)" && mkdir -p "$(TARBALL_DIR)/dist"
	cp bin/takt "$(TARBALL_DIR)/"
	cp -r web/dist/. "$(TARBALL_DIR)/dist/"
	cp README.md README.ru.md CHANGELOG.md LICENSE NOTICE THIRD-PARTY.md "$(TARBALL_DIR)/"
	cd dist && tar czf "$(notdir $(TARBALL_DIR)).tar.gz" "$(notdir $(TARBALL_DIR))"
	cd dist && sha256sum "$(notdir $(TARBALL_DIR)).tar.gz" > "$(notdir $(TARBALL_DIR)).tar.gz.sha256"
	# Бинарник ещё и отдельно, рядом с архивом.
	#
	# Сверяют на месте именно его: у поставленного из архива на диске
	# лежит `/opt/takt/takt`, и вопрос проверки звучит «этот ли файл вы
	# выпускали». Ответить на него суммой архива нельзя — архив
	# распаковали и стёрли, а сумма распакованного зависит ещё и от того,
	# чем распаковывали.
	#
	# Сборка воспроизводима: CGO выключен, пути срезаны `-trimpath`,
	# версия приходит линковщиком. Тот же тег тем же тулчейном (он назван
	# в go.mod) даёт те же байты — значит проверяющий может не верить
	# нашей сумме, а получить её сам.
	#
	# Расширение `.bin` не украшение: без него имя совпало бы с каталогом
	# внутри архива, и распаковка рядом затёрла бы файл, который сверяют.
	cp bin/takt "dist/$(notdir $(TARBALL_DIR)).bin"
	cd dist && sha256sum "$(notdir $(TARBALL_DIR)).bin" > "$(notdir $(TARBALL_DIR)).bin.sha256"
	@rm -rf "$(TARBALL_DIR)"
	@echo "архив: dist/$(notdir $(TARBALL_DIR)).tar.gz"

# Имя архива берётся из того же окружения, что и сборка, а не задаётся
# рядом с ней. Рычагов было два — GOARCH для компилятора и TARBALL_ARCH
# для имени, — и разойтись им ничего не мешало: `GOARCH=arm64 make
# tarball TARBALL_ARCH=amd64` собирается молча и даёт архив, который
# распакуется у заказчика и не запустится. Теперь рычаг один: GOARCH.
TARBALL_OS   ?= $(shell go env GOOS)
TARBALL_ARCH ?= $(shell go env GOARCH)
TARBALL_DIR  ?= dist/takt-$(VERSION)-$(TARBALL_OS)-$(TARBALL_ARCH)
# Комплект собирается под названную архитектуру, а не под ту, на которой
# оказался сборщик. Выбирать приходится: `docker save` увозит один
# образ, а набор из нескольких архитектур существует только в реестре —
# файлом его не увезти. Молчаливый выбор здесь хуже всего: комплект,
# собранный на amd64 для arm64-сервера, загрузится, установится
# и упадёт на «exec format error» — сообщение, по которому причину
# ищут в чарте, а не в поставке. Поэтому архитектура стоит в имени
# каталога и печатается в конце сборки.
#
# Чужая архитектура требует qemu на сборочной машине:
# `docker run --privileged --rm tonistiigi/binfmt --install all`.
# Версия чарта — версия сборки без ведущей «v»: helm требует semver.
# Прежде она не переписывалась вовсе, и довод — «описание сборки semver
# быть не обязано» — верен ровно для описания сборки: у тега с этим
# всё в порядке, и выпуск клал в релиз чарт с прошлым номером.
# Описание, из которого semver не выходит (в дереве без тегов
# `git describe` отдаёт голый хеш), даёт 0.0.0 — как в Chart.yaml:
# это видно и версией не притворяется.
#
# Скобок в этой команде нет намеренно: `$(shell …)` make разбирает
# по скобкам, и `case … in [0-9]*)` закрыл бы вызов на первой же —
# в рецепт уехал бы кусок самой команды вместо её вывода.
CHART_VERSION ?= $(shell v='$(BUNDLE_VERSION)'; v="$${v#v}"; \
  printf '%s' "$$v" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+' \
  && printf '%s' "$$v" || printf 0.0.0)

BUNDLE_ARCH ?= $(shell go env GOARCH)
BUNDLE_DIR  ?= dist/bundle-$(BUNDLE_VERSION)-linux-$(BUNDLE_ARCH)

.PHONY: bundle
bundle: ## Собрать комплект для установки без доступа в интернет (BUNDLE_ARCH=amd64|arm64)
	@mkdir -p "$(BUNDLE_DIR)"
	docker build --platform linux/$(BUNDLE_ARCH) \
	  --build-arg VERSION=$(BUNDLE_VERSION) -t takt:$(BUNDLE_VERSION) .
	# Собранное обязано запускаться, а не только собираться: под чужой
	# архитектурой сборка идёт через qemu, и «собралось» о запуске
	# не говорит ничего. `version` отвечает и при недоступной базе.
	docker run --rm --platform linux/$(BUNDLE_ARCH) takt:$(BUNDLE_VERSION) version
	docker save takt:$(BUNDLE_VERSION) | gzip > "$(BUNDLE_DIR)/takt-image.tar.gz"
	helm package deploy/helm/takt --version "$(CHART_VERSION)" \
	  --app-version "$(BUNDLE_VERSION)" -d "$(BUNDLE_DIR)" >/dev/null
	go run ./cmd/docs
	# Лицензия и список чужого кода едут вместе с продуктом не для
	# порядка: Apache-2.0 требует передавать NOTICE с каждой копией,
	# а комплект для закрытого контура — это и есть копия.
	cp README.md README.ru.md CHANGELOG.md LICENSE NOTICE THIRD-PARTY.md "$(BUNDLE_DIR)/"
	cp -r docs/html "$(BUNDLE_DIR)/docs"
	$(MAKE) docs-pdf
	cp docs/html/takt.pdf "$(BUNDLE_DIR)/"
	echo "$(BUNDLE_VERSION)" > "$(BUNDLE_DIR)/VERSION"
	# Состав поставки едет с поставкой. У заказчика комплект попадает
	# на проверку ИБ, и первый её вопрос — «из чего это собрано»;
	# отвечать на него перепиской через месяц после установки дороже
	# для обеих сторон, чем положить файл в комплект сразу.
	$(MAKE) sbom
	@mkdir -p "$(BUNDLE_DIR)/sbom"
	cp $(SBOM_DIR)/*.json "$(BUNDLE_DIR)/sbom/"
	# По каждому файлу, а не по каждой записи в каталоге: с тех пор как
	# в комплект легли документация и состав, `sha256sum *` спотыкался
	# о каталог — «Это каталог», код 1, и сборка комплекта обрывалась
	# на последнем шаге, сделав всю тяжёлую работу. Заодно суммы теперь
	# покрывают и вложенное: страницу документации подменить так же
	# просто, как образ.
	cd "$(BUNDLE_DIR)" && find . -type f ! -name SHA256SUMS -printf '%P\n' \
	  | sort | xargs -r sha256sum > SHA256SUMS
	@echo
	@echo "комплект собран: $(BUNDLE_DIR)"
	@echo "архитектура: linux/$(BUNDLE_ARCH) — на другой образ не запустится"
	@echo "на месте:"
	@echo "  sha256sum -c SHA256SUMS"
	@echo "  docker load < takt-image.tar.gz    # либо skopeo copy в своё зеркало"
	@echo "  docker image inspect takt:$(BUNDLE_VERSION) --format '{{.Architecture}}'"
	@echo "  helm install takt takt-*.tgz --set image.tag=$(BUNDLE_VERSION) \\"
	@echo "    --set baseURL=... --set database.existingSecret=takt-db"
