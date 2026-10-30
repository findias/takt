package idea

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"github.com/findias/takt/internal/board"
)

// Cards заводит карточку из идеи операцией доски: с её блокировкой,
// лимитами колонки, номером и событием «создана» — как если бы человек
// нажал «Новая карточка». Описание — второй операцией: у CREATE_CARD
// описания нет, и заводить его ради одного вызова незачем.
type Cards struct{ Boards *board.Service }

func (m Cards) CreateCard(ctx context.Context, orgID, actorID, boardID, columnID, title, description string) (string, error) {
	payload, _ := json.Marshal(map[string]any{"title": title, "columnId": columnID})
	res, err := m.Boards.Apply(ctx, orgID, actorID, boardID, board.Request{
		OperationID: uuid.NewString(), Type: "CREATE_CARD", Payload: payload})
	if err != nil {
		return "", err
	}
	if len(res.Patch.Cards) == 0 {
		return "", errors.New("операция не вернула карточку")
	}
	id := res.Patch.Cards[0].ID
	if description == "" {
		return id, nil
	}
	payload, _ = json.Marshal(map[string]any{"cardId": id, "description": description})
	_, err = m.Boards.Apply(ctx, orgID, actorID, boardID, board.Request{
		OperationID: uuid.NewString(), Type: "UPDATE_CARD", Payload: payload})
	return id, err
}
