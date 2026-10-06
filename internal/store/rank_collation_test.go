package store_test

import (
	"context"
	"testing"

	"github.com/findias/takt/internal/store/testdb"
)

// Ключи порядка (internal/rank) построены под побайтовое сравнение,
// и столбцы позиций обязаны сравнивать так же (0078). По локали базы
// «aZ» > «aa», а побайтово наоборот: на проде с ru_RU после переноса
// доски «последней карточкой колонки» база называла не последнюю,
// и заведение карточки падало на cards_order_key.
//
// Проверяется схема, а не поведение: наша база на alpine (musl)
// сравнивает побайтово при любой локали, и поведенческий тест здесь
// прошёл бы и без правила — как прошли все до прода.
func TestRankKeysCompareByBytes(t *testing.T) {
	ctx := context.Background()
	db := testdb.Open(t)

	rows, err := db.Pool.Query(ctx, `
		select table_name, coalesce(collation_name, '')
		  from information_schema.columns
		 where table_schema = 'public' and column_name = 'position'
		   and data_type = 'text'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var table, collation string
		if err := rows.Scan(&table, &collation); err != nil {
			t.Fatal(err)
		}
		seen++
		if collation != "C" {
			t.Errorf("%s.position сравнивается по %q, а не побайтово: "+
				"ключи rank в нём упорядочатся по локали базы, и вставка «после последнего» "+
				"совпадёт с занятым ключом. Объявите столбец `collate \"C\"`", table, collation)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// Проверка, не нашедшая ни одного столбца, ничего не проверила.
	if seen < 2 {
		t.Fatalf("столбцов позиций найдено %d, ожидалось не меньше двух (cards, board_columns)", seen)
	}
}
