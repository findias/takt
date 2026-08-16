package board

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Ключ доски — префикс номеров её карточек: ПРО в ПРО-142.
//
// Границы выбраны по тому, как номер живёт снаружи: его называют вслух
// и набирают руками. Шесть знаков — предел, после которого префикс
// перестаёт быть короче названия и смысл в нём пропадает; два — предел,
// ниже которого ключи разных досок различаются одной буквой.
const (
	keyMinLen = 2
	keyMaxLen = 6
)

// Сколько ключей перебрать, прежде чем сдаться. Упереться в предел можно
// только имея сотню досок, чьи названия начинаются одинаково; это уже
// не столкновение ключей, а вопрос к названиям.
const keyAttempts = 100

// insertBoard заводит строку доски, подобрав ключ.
//
// Занятость ключа не спрашивается заранее, а узнаётся от уникального
// индекса. Предварительный вопрос «свободен ли» пришлось бы задавать
// через политики видимости, а они показывают не все доски организации:
// закрытая доска чужой команды невидима, её ключ выглядел бы свободным,
// и человек получал бы вместо ответа пятисотую ошибку от индекса.
// Индекс знает про все доски — значит спрашивать надо его.
//
// Заданный человеком ключ не подгоняется: занятый ключ это ошибка,
// а не повод молча выдать соседний. Выведенный из названия, наоборот,
// подгоняется — человек его не выбирал и о столкновении не знает.
func insertBoard(
	ctx context.Context, tx pgx.Tx, orgID, projectID, name, want string,
) (Info, error) {
	explicit := strings.TrimSpace(want) != ""
	if explicit {
		want = strings.ToUpper(strings.TrimSpace(want))
		if !validKey(want) {
			return Info{}, ErrBadKey
		}
	}
	base := deriveKey(name)

	for attempt := 1; attempt <= keyAttempts; attempt++ {
		key := want
		if !explicit {
			key = base
			// Номер к основе приписывается со второй попытки: у первой
			// доски с таким началом названия ключ остаётся чистым.
			if attempt > 1 {
				suffix := strconv.Itoa(attempt)
				// Основа урезается, чтобы номер уместился: ПРОЕК2,
				// а не ПРОЕКТ2 длиной в семь.
				key = trimRunes(base, keyMaxLen-len(suffix)) + suffix
			}
		}

		// Вложенная транзакция — точка сохранения: столкновение ключей
		// отменяет только вставку доски, а не всё, что было сделано
		// до неё. Без неё первая же занятая основа рушила бы транзакцию
		// целиком, вместе с уже заведённым проектом.
		nested, err := tx.Begin(ctx)
		if err != nil {
			return Info{}, err
		}
		b, err := scanBoard(nested.QueryRow(ctx, `
			insert into boards (org_id, project_id, name, key) values ($1, $2, $3, $4)
			returning `+boardFields, orgID, projectID, name, key))
		if err == nil {
			return b, nested.Commit(ctx)
		}
		_ = nested.Rollback(ctx)

		if !isKeyCollision(err) {
			return Info{}, err
		}
		if explicit {
			return Info{}, ErrKeyTaken
		}
	}
	return Info{}, ErrKeyTaken
}

func isKeyCollision(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" && pgErr.ConstraintName == "boards_key_key"
}

// deriveKey выводит основу ключа из названия доски: первые четыре буквы
// и цифры без пробелов и знаков, в верхнем регистре.
//
// Соблазн был складывать аббревиатуру по первым буквам слов —
// «Мобильное приложение» дало бы МП. Отказались: то же правило пришлось
// бы повторить в миграции, которая проставляет ключи уже заведённым
// доскам, а правило, записанное дважды, разъезжается. Простое правило
// одинаково выражается и там, и здесь, а ключ, который человеку
// не нравится, он задаёт руками при создании.
const keyDeriveLen = 4

func deriveKey(name string) string {
	var compact []rune
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			compact = append(compact, unicode.ToUpper(r))
		}
		if len(compact) == keyDeriveLen {
			break
		}
	}
	if len(compact) < keyMinLen {
		// Название без букв и цифр или в один знак: своей основы у такого
		// ключа нет, и подпирать её нечем.
		return "ДОСКА"
	}
	return string(compact)
}

// validKey проверяет заданный человеком ключ. Начинается с буквы —
// иначе номер вида 12-7 читается как диапазон, а не как имя задачи.
func validKey(key string) bool {
	runes := []rune(key)
	if len(runes) < keyMinLen || len(runes) > keyMaxLen {
		return false
	}
	if !unicode.IsLetter(runes[0]) {
		return false
	}
	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func trimRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
