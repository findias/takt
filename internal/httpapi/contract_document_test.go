package httpapi

import (
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
)

// Описание контракта — рукописный файл, и до сих пор в него никто
// не смотрел целиком: проверка дрейфа сверяет три перечисления, всё
// остальное держалось на внимательности при правке. Значит, битая
// ссылка $ref, схема не по спецификации или опечатка в ключе проходят
// молча и обнаруживаются у того, кто сгенерирует по описанию клиент, —
// ровно та беда, от которой файл и заводился.
//
// Разбор и проверка тянутся только в тесты: ни один файл без _test
// их не импортирует, поэтому в бинарник они не попадают.
func TestContractDocumentIsValid(t *testing.T) {
	document, err := libopenapi.NewDocument(openapiDocument)
	if err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}

	// Сборка модели разрешает ссылки: битый $ref видно здесь, а не
	// в проверке по схеме — та смотрит на форму документа, а не на то,
	// ведут ли ссылки куда-нибудь.
	if _, err := document.BuildV3Model(); err != nil {
		t.Fatalf("модель описания не собирается: %v", err)
	}

	check, errs := validator.NewValidator(document)
	if len(errs) > 0 {
		for _, err := range errs {
			t.Fatalf("проверку описания не удалось построить: %v", err)
		}
	}
	valid, problems := check.ValidateDocument()
	if !valid {
		for _, problem := range problems {
			t.Errorf("описание не по спецификации: %s — %s", problem.Message, problem.Reason)
			for _, item := range problem.SchemaValidationErrors {
				t.Errorf("  %s: %s", item.FieldPath, item.Reason)
			}
		}
	}
}

// required, называющий поле, которого в схеме нет, — обещание, которое
// не сдержит и сам сервер: генератор объявит поле обязательным и
// невыводимым. Спецификация такого не запрещает, поэтому проверка
// по схеме молчит, а рукописная правка схемы попадает в это легко:
// поле переименовали в properties и забыли в required.
func TestContractRequiredFieldsExist(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(openapiDocument, &doc); err != nil {
		t.Fatalf("описание не разбирается: %v", err)
	}
	walkSchemas(t, doc, "")
}

func walkSchemas(t *testing.T, node any, where string) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if required, ok := value["required"].([]any); ok {
			properties, _ := value["properties"].(map[string]any)
			// Схема без properties, но с required — либо ссылка на другую
			// схему рядом (allOf), либо забытая правка. Первое законно,
			// второе нет, и различить их можно только по наличию сборки:
			// проверяем поля лишь там, где properties вообще есть.
			if properties != nil {
				names := make([]string, 0, len(properties))
				for name := range properties {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, item := range required {
					name, ok := item.(string)
					if !ok {
						t.Errorf("%s: в required не строка: %v", where, item)
						continue
					}
					if !slices.Contains(names, name) {
						t.Errorf("%s: required называет %q, а в properties его нет (есть: %s)",
							where, name, strings.Join(names, ", "))
					}
				}
			}
		}
		for key, child := range value {
			walkSchemas(t, child, where+"."+key)
		}
	case []any:
		for i, child := range value {
			walkSchemas(t, child, where+"["+strconv.Itoa(i)+"]")
		}
	}
}
