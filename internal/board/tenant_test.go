package board

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/konkov/agile/internal/store"
)

// Изоляция арендаторов — то место, где ошибка стоит дороже всего: она не
// падает, не логируется и обнаруживается клиентом. Поэтому проверяем не
// «код фильтрует по org_id», а «база не отдаёт чужие строки, даже если
// код попросит».

func TestTenantsDoNotSeeEachOthersBoards(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	svc := New(db)

	orgA, _ := newTenant(t, db)
	orgB, _ := newTenant(t, db)

	boardA, err := svc.Create(ctx, orgA, "Доска А")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, orgB, "Доска Б"); err != nil {
		t.Fatal(err)
	}

	listA, err := svc.List(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 1 || listA[0].Name != "Доска А" {
		t.Fatalf("организация А видит %d досок: %+v", len(listA), listA)
	}

	// Подставляем чужой идентификатор доски, зная его точно, — так выглядит
	// самая частая попытка выйти за пределы своей организации.
	if _, err := svc.Snapshot(ctx, orgB, boardA.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("чужая доска открылась по прямому идентификатору: %v", err)
	}
}

func TestOperationOnForeignBoardIsRejected(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	svc := New(db)

	orgA, userA := newTenant(t, db)
	orgB, userB := newTenant(t, db)

	boardA, err := svc.Create(ctx, orgA, "Доска А")
	if err != nil {
		t.Fatal(err)
	}
	snapA, err := svc.Snapshot(ctx, orgA, boardA.ID)
	if err != nil {
		t.Fatal(err)
	}
	columnA := snapA.Columns[0].ID

	_, err = svc.Apply(ctx, orgA, userA, boardA.ID, Request{
		OperationID: uuid.NewString(),
		Type:        "CREATE_CARD",
		Payload:     []byte(`{"columnId":"` + columnA + `","title":"Секрет"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Сосед знает и доску, и колонку, но действует от своей организации.
	_, err = svc.Apply(ctx, orgB, userB, boardA.ID, Request{
		OperationID: uuid.NewString(),
		Type:        "CREATE_CARD",
		Payload:     []byte(`{"columnId":"` + columnA + `","title":"Чужая карточка"}`),
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("операция по чужой доске не отклонена: %v", err)
	}

	snapA, err = svc.Snapshot(ctx, orgA, boardA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapA.Cards) != 1 {
		t.Errorf("на доске А оказалось %d карточек вместо одной", len(snapA.Cards))
	}
}

// Ключевое свойство RLS: без выставленного арендатора не видно ничего.
// Забыть область — значит получить пустой результат, а не чужие данные.
func TestQueryWithoutTenantScopeSeesNothing(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()
	svc := New(db)

	orgA, _ := newTenant(t, db)
	if _, err := svc.Create(ctx, orgA, "Доска А"); err != nil {
		t.Fatal(err)
	}

	var visible int
	err := db.Pool.QueryRow(ctx, `select count(*) from boards`).Scan(&visible)
	if err != nil {
		t.Fatalf("запрос вне области арендатора вернул ошибку: %v", err)
	}
	if visible != 0 {
		t.Errorf("запрос без области арендатора увидел %d досок, ожидался 0", visible)
	}
}

// Даже прямой SQL с подставленным чужим org_id не проходит: политика
// проверяет не только чтение (using), но и запись (with check).
func TestInsertIntoForeignTenantIsBlocked(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()

	orgA, _ := newTenant(t, db)
	orgB, _ := newTenant(t, db)

	err := db.InTenant(ctx, orgB, func(tx pgx.Tx) error {
		var projectID string
		if err := tx.QueryRow(ctx,
			`insert into projects (org_id, name) values ($1, 'Свой') returning id`,
			orgB).Scan(&projectID); err != nil {
			return err
		}
		// Пишем строку с чужим org_id, находясь в области orgB.
		_, err := tx.Exec(ctx,
			`insert into boards (org_id, project_id, name) values ($1, $2, 'Подложенная')`,
			orgA, projectID)
		return err
	})
	if err == nil {
		t.Fatal("запись с чужим org_id прошла, политика with check не сработала")
	}

	svc := New(db)
	listA, err := svc.List(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(listA) != 0 {
		t.Errorf("в чужой организации появилось %d досок", len(listA))
	}
}

// Приглашение адресуется секретным токеном, а не организацией. Проверяем,
// что политика по токену открывает ровно одну строку и не больше.
func TestInviteTokenScopeOpensSingleRow(t *testing.T) {
	db := testStore(t)
	ctx := context.Background()

	orgA, userA := newTenant(t, db)
	orgB, userB := newTenant(t, db)

	insert := func(orgID, userID, hash string) {
		t.Helper()
		err := db.InTenant(ctx, orgID, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				insert into invites (org_id, email, role, token_hash, invited_by, expires_at)
				values ($1, $2, 'member', $3, $4, now() + interval '1 day')`,
				orgID, uuid.NewString()+"@example.test", hash, userID)
			return err
		})
		if err != nil {
			t.Fatalf("создание приглашения: %v", err)
		}
	}
	hashA, hashB := "hash-"+uuid.NewString(), "hash-"+uuid.NewString()
	insert(orgA, userA, hashA)
	insert(orgB, userB, hashB)

	var visible int
	err := db.InScope(ctx, store.Scope{InviteToken: hashA}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from invites`).Scan(&visible)
	})
	if err != nil {
		t.Fatal(err)
	}
	if visible != 1 {
		t.Errorf("по токену видно %d приглашений, ожидалось ровно одно", visible)
	}

	// Токен без совпадений не открывает ничего.
	err = db.InScope(ctx, store.Scope{InviteToken: "нет-такого"}, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `select count(*) from invites`).Scan(&visible)
	})
	if err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Errorf("несуществующий токен открыл %d приглашений", visible)
	}
}
