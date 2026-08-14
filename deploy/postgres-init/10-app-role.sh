#!/bin/sh
# Создаёт роль приложения и базу под ней.
#
# Приложение НЕ должно подключаться суперпользователем: для суперпользователя
# политики Row-Level Security не действуют вообще, и вся изоляция арендаторов
# молча превращается в ничто. Ошибка не проявляется ни в логах, ни в тестах,
# которые ходят от одной организации, — только у клиента, увидевшего чужие
# данные.
#
# Роль board владеет базой, поэтому ей хватает прав на миграции, но она не
# суперпользователь, а значит FORCE ROW LEVEL SECURITY распространяется и
# на неё. Скрипт выполняется один раз при инициализации тома с данными.
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<-EOSQL
	create role board login password '${APP_DB_PASSWORD:-board}' nosuperuser nocreaterole nocreatedb;
	create database board owner board;
EOSQL

echo "роль board создана без прав суперпользователя"
