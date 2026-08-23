// Проверка чарта отрисовкой.
//
// `make check` чарта не касался вовсе: ни отрисовки шаблонов, ни установки
// в одноразовый кластер. Опечатка в имени поля или сбитый отступ доезжали
// до заказчика целыми — и обнаруживались у него, а не у нас. Это самый
// дешёвый шаг, который ловит большинство: отрисовка идёт секунду
// и кластера не поднимает. Установка в одноразовый кластер — следующий
// шаг, и здесь она честно не делается.
//
// Наборы значений выбраны по тому, чем установки отличаются друг
// от друга: база снаружи или в кластере, секрет свой или принесённый,
// ingress и автомасштабирование включены или нет.
package helm_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func чарт(t *testing.T) string {
	t.Helper()
	_, файл, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не найти собственный путь")
	}
	return filepath.Join(filepath.Dir(файл), "board")
}

// helm нужен для отрисовки. Пропуск объявляется вслух: молчаливо
// пропущенная проверка выглядит как пройденная.
func требуетHelm(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm не установлен — чарт не проверяется")
	}
}

func отрисовать(t *testing.T, значения ...string) (string, error) {
	t.Helper()
	args := append([]string{"template", "board", чарт(t)}, значения...)
	out, err := exec.Command("helm", args...).CombinedOutput()
	return string(out), err
}

// Наборы значений, на которых чарт обязан отрисовываться.
var наборы = []struct {
	имя      string
	значения []string
	ждём     []string
	неЖдём   []string
	// Что приносит заказчик: такие ресурсы чарт не создаёт законно,
	// и ссылки на них — не висячие. Перечисляются явно, потому что
	// «имя начинается с board» отличить их не может.
	внешние []string
}{
	{
		имя: "база снаружи, секрет принесён заказчиком",
		значения: []string{
			"--set", "baseURL=https://board.example.test",
			"--set", "database.existingSecret=board-db",
		},
		// Ни базы, ни секрета чарт не заводит: и то и другое — чужая зона.
		неЖдём:  []string{"kind: StatefulSet", "kind: Secret"},
		ждём:    []string{"kind: Deployment", "kind: Job", "kind: Service"},
		внешние: []string{"board-db"},
	},
	{
		имя: "база снаружи, секрет от чарта",
		значения: []string{
			"--set", "baseURL=https://board.example.test",
			"--set", "database.url=postgres://board:pass@db.example.test:5432/board",
		},
		ждём:   []string{"kind: Secret", "DATABASE_URL: \"postgres://board:pass@db.example.test:5432/board\""},
		неЖдём: []string{"kind: StatefulSet"},
	},
	{
		имя: "база в кластере — способ для стенда",
		значения: []string{
			"--set", "baseURL=https://board.example.test",
			"--set", "postgresql.enabled=true",
			"--set", "postgresql.password=p1",
			"--set", "postgresql.superuserPassword=p2",
		},
		ждём: []string{
			"kind: StatefulSet",
			// Строка подключения собрана чартом из адреса службы и роли:
			// второе место, где их пишут руками, разошлось бы с первым.
			`DATABASE_URL: "postgres://board:p1@board-postgres:5432/board?sslmode=disable"`,
			// Роль приложения заводится без прав суперпользователя:
			// для суперпользователя RLS не действует вовсе.
			"nosuperuser",
			// Миграция ждёт базу: хуки идут до основных ресурсов, то есть
			// до того, как база вообще появится.
			"name: wait-for-db",
		},
	},
	{
		имя: "ingress, TLS и автомасштабирование",
		значения: []string{
			"--set", "baseURL=https://board.example.test",
			"--set", "database.existingSecret=board-db",
			"--set", "ingress.enabled=true", "--set", "ingress.host=board.example.test",
			"--set", "ingress.tls.enabled=true", "--set", "ingress.tls.secretName=board-tls",
			"--set", "autoscaling.enabled=true",
		},
		ждём:    []string{"kind: Ingress", "kind: HorizontalPodAutoscaler"},
		внешние: []string{"board-db"},
		// При автомасштабировании число реплик задаёт HPA: указанное
		// в Deployment сбрасывало бы его при каждой выкладке.
		неЖдём: []string{"replicas: 2"},
	},
	{
		имя: "одна реплика, без миграций и без PDB",
		значения: []string{
			"--set", "baseURL=https://board.example.test",
			"--set", "database.existingSecret=board-db",
			"--set", "replicaCount=1",
			"--set", "migrations.enabled=false",
			"--set", "podDisruptionBudget.enabled=false",
		},
		ждём:    []string{"replicas: 1"},
		неЖдём:  []string{"kind: Job", "kind: PodDisruptionBudget"},
		внешние: []string{"board-db"},
	},
}

func TestChartRenders(t *testing.T) {
	требуетHelm(t)
	for _, набор := range наборы {
		t.Run(набор.имя, func(t *testing.T) {
			out, err := отрисовать(t, набор.значения...)
			if err != nil {
				t.Fatalf("чарт не отрисовался:\n%s", out)
			}
			for _, что := range набор.ждём {
				if !strings.Contains(out, что) {
					t.Errorf("в отрисованном нет %q", что)
				}
			}
			for _, что := range набор.неЖдём {
				if strings.Contains(out, что) {
					t.Errorf("в отрисованном есть лишнее: %q", что)
				}
			}
		})
	}
}

// Чарт обязан отказываться, а не молчать: установка, поехавшая без
// базы или без адреса, ломается позже и не там, где причина.
func TestChartRefusesWhatItCannotDo(t *testing.T) {
	требуетHelm(t)
	отказы := []struct {
		имя      string
		значения []string
		причина  string
	}{
		{
			имя:      "без baseURL",
			значения: []string{"--set", "database.existingSecret=board-db"},
			причина:  "baseURL",
		},
		{
			имя:      "без базы вовсе",
			значения: []string{"--set", "baseURL=https://board.example.test"},
			причина:  "database.url",
		},
		{
			имя: "база и снаружи, и в кластере",
			значения: []string{
				"--set", "baseURL=https://board.example.test",
				"--set", "database.url=postgres://x",
				"--set", "postgresql.enabled=true",
				"--set", "postgresql.password=p", "--set", "postgresql.superuserPassword=p",
			},
			причина: "оставьте что-то одно",
		},
		{
			имя: "база в кластере без паролей",
			значения: []string{
				"--set", "baseURL=https://board.example.test",
				"--set", "postgresql.enabled=true",
			},
			причина: "не придумывает пароли сам",
		},
	}
	for _, отказ := range отказы {
		t.Run(отказ.имя, func(t *testing.T) {
			out, err := отрисовать(t, отказ.значения...)
			if err == nil {
				t.Fatalf("чарт принял то, что должен был отвергнуть:\n%s", out)
			}
			if !strings.Contains(out, отказ.причина) {
				t.Errorf("отказ не называет причину %q, а говорит:\n%s", отказ.причина, out)
			}
		})
	}
}

// Отрисованное должно быть связным.
//
// Отрисовка сама по себе ловит сломанный шаблон, но не ловит того, чем
// чарты ломаются на самом деле: селектор службы, разошедшийся с метками
// подов (служба без адресов и час поисков), ссылка на секрет, которого
// никто не создаёт (под в CreateContainerConfigError), StatefulSet
// без имени службы (нет стабильных имён). Схема ресурсов этого тоже
// не видит: все три случая — правильный YAML с правильными полями.
//
// `kubectl --dry-run` здесь не помощник: строгая проверка полей требует
// схему с живого кластера, а установка обязана проверяться там, где
// кластера нет.
func TestRenderedIsConsistent(t *testing.T) {
	требуетHelm(t)
	for _, набор := range наборы {
		t.Run(набор.имя, func(t *testing.T) {
			out, err := отрисовать(t, набор.значения...)
			if err != nil {
				t.Fatalf("чарт не отрисовался:\n%s", out)
			}
			ресурсы := разобрать(t, out)
			метки := меткиПодов(ресурсы)
			секреты := именаТипа(ресурсы, "Secret")
			конфиги := именаТипа(ресурсы, "ConfigMap")
			службы := именаТипа(ресурсы, "Service")

			for _, r := range ресурсы {
				вид := строка(r, "kind")
				имя := строка(r, "metadata", "name")
				if вид == "" || имя == "" {
					t.Errorf("ресурс без вида или имени: %v", r)
					continue
				}

				// Селектор рабочей нагрузки обязан находить её же поды.
				if вид == "Deployment" || вид == "StatefulSet" {
					селектор := карта(r, "spec", "selector", "matchLabels")
					свои := карта(r, "spec", "template", "metadata", "labels")
					for k, v := range селектор {
						if свои[k] != v {
							t.Errorf("%s %s: селектор просит %s=%v, а метки пода этого не дают",
								вид, имя, k, v)
						}
					}
				}
				if вид == "StatefulSet" {
					служба := строка(r, "spec", "serviceName")
					if служба == "" {
						t.Errorf("StatefulSet %s без serviceName: стабильных имён у подов не будет", имя)
					} else if !содержит(службы, служба) {
						t.Errorf("StatefulSet %s ссылается на службу %q, которой чарт не создаёт", имя, служба)
					}
				}

				// Служба обязана находить хоть один под.
				if вид == "Service" {
					селектор := карта(r, "spec", "selector")
					if len(селектор) == 0 {
						t.Errorf("Service %s без селектора", имя)
					} else if !естьПод(метки, селектор) {
						t.Errorf("Service %s не находит ни одного пода: селектор %v", имя, селектор)
					}
				}

				// Ссылки на секреты и конфигурации — на то, что существует.
				// Принесённое заказчиком перечислено в наборе: оно
				// отсутствует законно, а всё остальное чарт обязан создать
				// сам, иначе под встанет в CreateContainerConfigError.
				for _, ссылка := range ссылкиНа(r, "secretKeyRef") {
					if !содержит(набор.внешние, ссылка) && !содержит(секреты, ссылка) {
						t.Errorf("%s %s ссылается на секрет %q, которого чарт не создаёт", вид, имя, ссылка)
					}
				}
				for _, ссылка := range ссылкиНа(r, "configMap") {
					if !содержит(набор.внешние, ссылка) && !содержит(конфиги, ссылка) {
						t.Errorf("%s %s ссылается на конфигурацию %q, которой чарт не создаёт", вид, имя, ссылка)
					}
				}
			}
		})
	}
}

func разобрать(t *testing.T, out string) []map[string]any {
	t.Helper()
	ресурсы := []map[string]any{}
	for _, кусок := range strings.Split(out, "\n---") {
		var r map[string]any
		if err := yaml.Unmarshal([]byte(кусок), &r); err != nil {
			t.Fatalf("отрисованное не разбирается как YAML: %v\n%s", err, кусок)
		}
		if len(r) > 0 {
			ресурсы = append(ресурсы, r)
		}
	}
	if len(ресурсы) == 0 {
		t.Fatal("чарт не отрисовал ни одного ресурса")
	}
	return ресурсы
}

func строка(r map[string]any, путь ...string) string {
	v, _ := вглубь(r, путь...).(string)
	return v
}

func карта(r map[string]any, путь ...string) map[string]any {
	v, _ := вглубь(r, путь...).(map[string]any)
	return v
}

func вглубь(r map[string]any, путь ...string) any {
	var текущий any = r
	for _, шаг := range путь {
		м, ok := текущий.(map[string]any)
		if !ok {
			return nil
		}
		текущий = м[шаг]
	}
	return текущий
}

func именаТипа(ресурсы []map[string]any, вид string) []string {
	out := []string{}
	for _, r := range ресурсы {
		if строка(r, "kind") == вид {
			out = append(out, строка(r, "metadata", "name"))
		}
	}
	return out
}

func содержит(список []string, что string) bool {
	for _, э := range список {
		if э == что {
			return true
		}
	}
	return false
}

func меткиПодов(ресурсы []map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, r := range ресурсы {
		вид := строка(r, "kind")
		if вид != "Deployment" && вид != "StatefulSet" {
			continue
		}
		out = append(out, карта(r, "spec", "template", "metadata", "labels"))
	}
	return out
}

func естьПод(метки []map[string]any, селектор map[string]any) bool {
	for _, свои := range метки {
		подходит := true
		for k, v := range селектор {
			if свои[k] != v {
				подходит = false
				break
			}
		}
		if подходит {
			return true
		}
	}
	return false
}

// ссылкиНа собирает имена, на которые ссылается ресурс: секреты
// в `secretKeyRef` и конфигурации в `configMap`. Обход общий, потому
// что ссылки лежат на разной глубине — в env контейнера, в томе пода,
// в initContainer'е, — и перечислять места по одному значит однажды
// забыть новое.
func ссылкиНа(значение any, ключ string) []string {
	out := []string{}
	switch v := значение.(type) {
	case map[string]any:
		for k, вложенное := range v {
			if k == ключ {
				if м, ok := вложенное.(map[string]any); ok {
					if имя, ok := м["name"].(string); ok {
						out = append(out, имя)
					}
				}
				continue
			}
			out = append(out, ссылкиНа(вложенное, ключ)...)
		}
	case []any:
		for _, э := range v {
			out = append(out, ссылкиНа(э, ключ)...)
		}
	}
	return out
}
