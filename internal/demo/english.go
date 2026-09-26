package demo

// english — песочница для англоязычного посетителя.
//
// Не дословный перевод, а та же история по-английски: склад, релиз,
// смежники, которые не отвечают. Длина фраз держится близко к русской —
// демо заводилось ради вёрстки на настоящей длине текста, и английская
// песочница, где всё вдвое короче, показывала бы другой экран.
//
// Полнота проверяется не перечнем, а результатом: в английской
// песочнице не остаётся ни одной кириллической буквы
// (TestEnglishSandboxHasNoRussian).
var english = map[string]string{
	// Перенесённое из прежней системы.
	"Сверить остатки за июль":                  "Reconcile July stock",
	"Продлить договор с перевозчиком":          "Renew the carrier contract",
	"Описать упаковку для хрупкого":            "Specify packaging for fragile goods",
	"Перенести справочник поставщиков":         "Move the supplier directory",
	"Кирилл Лебедев":                           "Kirill Lebedev",
	"ЛОГ-12":                                   "LOG-12",
	"Согласовать график поставок":              "Agree the delivery schedule",
	"Поставщик просит сдвинуть на неделю":      "The supplier asks to move it by a week",
	"Задача создана в колонке «Нужно сделать»": "Task created in the column “To do”",
	"Задача перемещена в колонку «В работе»":   "Task moved to the column “In progress”",

	// Люди, организация, служебное.
	"Анна Королёва":        "Anna Clarke",
	"Борис Дятлов":         "Boris Dale",
	"Вера Соколова":        "Vera Scott",
	"Глеб Тишин":           "Gleb Turner",
	"Дмитрий Орлов":        "Dmitry Owen",
	"Северный проект":      "Northern Project",
	"Обмен со складом":     "Warehouse sync",
	"Оповещение дежурного": "On-call alert",

	// Подразделения.
	"Разработка": "Engineering",
	"Платформа":  "Platform",
	"Портфель":   "Portfolio",
	"Ядро":       "Core",
	"Продажи":    "Sales",
	"Курсы":      "Training",

	// Доски и их ключи.
	"Поставки": "Supplies",
	"ПОСТ":     "SUP",
	"ПЛАТ":     "PLAT",
	"ПОРТ":     "PORT",
	"Найм":     "Hiring",
	"НАЙМ":     "HIRE",

	// Метки, поле, разметка колонки, сохранённый вид.
	"Срочно":          "Urgent",
	"Смежники":        "Partners",
	"Риск":            "Risk",
	"Старый формат":   "Old format",
	"Ключевой клиент": "Key account",
	"Техдолг":         "Tech debt",
	"Ждём склад":      "Waiting on warehouse",
	"Заказчик":        "Customer",
	"Северстрой":      "Northbuild",
	"ЗНО-10492":       "SR-10492",
	"ЗНО-10517":       "SR-10517",
	"ПРБ-58":          "PRB-58",
	"Есть постановка, известен исполнитель и срок": "Has a brief, a known assignee and a due date",
	"Мои срочные": "My urgent",

	// Карточки «Поставок».
	"Согласовать смету с подрядчиком":                              "Agree the estimate with the contractor",
	"Смета на второй этап. Спорные позиции — леса и вывоз грунта.": "Estimate for phase two. Disputed items: scaffolding and soil removal.",
	"Обновить регламент приёмки":                                   "Update the acceptance procedure",
	"Разобрать обращения за неделю":                                "Go through this week's requests",
	"Восемнадцать обращений, половина — про сроки поставки.":       "Eighteen requests, half of them about delivery dates.",
	"Выпустить релиз склада":                                       "Ship the warehouse release",
	"Ждём подтверждения от смежников по интеграции.":               "Waiting for the partners to confirm the integration.",
	"Перевезти стенд в новый офис":                                 "Move the test rig to the new office",
	"Закрыть акт за июль":                                          "Close the July statement",
	"Проверить остатки на складе":                                  "Check warehouse stock",

	// Подзадачи, обсуждение, блокировки.
	"Собрать сборку":                          "Put the build together",
	"Прогнать нагрузочные":                    "Run the load tests",
	"Сборка встала на шаге с миграциями.":     "The build got stuck at the migrations step.",
	"Поправил, пересобираю.":                  "Fixed it, rebuilding.",
	"Согласовать текст письма клиентам":       "Agree the wording of the customer letter",
	"Поднять квоту на хранилище":              "Raise the storage quota",
	"Переезд на новый склад":                  "Move to the new warehouse",
	"Перевезти стеллажи":                      "Move the shelving",
	"смежники не подтвердили формат выгрузки": "the partners have not confirmed the export format",
	"Смежники обещали ответить до среды. Если не ответят — режем интеграцию из этого релиза.": "The partners promised an answer by Wednesday. If they don't reply, we cut the integration from this release.",
	"Написал им ещё раз, приложил пример выгрузки.":                                           "Wrote to them again and attached a sample export.",
	"ждём подписанный акт от склада":                                                          "waiting for the signed statement from the warehouse",
	"выгрузка обращений будет после обеда":                                                    "the request export will be ready after lunch",
	"ждали ключи от серверной":                                                                "we were waiting for the server room keys",

	// Итерации и архив.
	"Закрыть июльские хвосты": "Tie up July's loose ends",
	"Неделя %d": "Week %d",
	"Довести релиз склада до стенда": "Get the warehouse release onto the test rig",
	"Старый регламент приёмки":       "Old acceptance procedure",
	"Отменённая закупка бытовки":     "Cancelled site cabin purchase",

	// Карточки «Платформы».
	"Вынести очередь в отдельный сервис":  "Move the queue into its own service",
	"Обновить базу до 16-й версии":        "Upgrade the database to version 16",
	"Черновик схемы кеша":                 "Cache layout draft",
	"Разобраться с ростом времени ответа": "Find out why response times are growing",
}
