package idea

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Решение и перенос меняют одну идею. Замок берётся после проверки
// права, затем состояние перечитывается: за время ожидания другой
// запрос мог решить идею или перенести её в чужую область. NO KEY
// UPDATE совместим с проверками внешних ключей у голосов и двойников.
func lockIdea(ctx context.Context, tx pgx.Tx, ideaID string) error {
	var id string
	err := tx.QueryRow(ctx, `select id from ideas where id = $1 for no key update`, ideaID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
