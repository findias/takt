package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/findias/takt/internal/auth"
	"github.com/findias/takt/internal/board"
	"github.com/findias/takt/internal/importer/pack"
	"github.com/findias/takt/internal/store"
)

// runImport — `takt import пакет.takt`: перенос пакета с командной
// строки сервера (docs/import-package.md).
//
// Нужен для пакетов больше 50 МБ, которые через экран не пройдут, и для
// тех, кто переносит много досок подряд. Переносит от имени человека
// (`--as`), с его правами: доску, в которую он не пишет, команда
// не тронет, как не тронул бы экран. По умолчанию — предпросмотр,
// тот же, что на экране; пишет только с `--apply`: необратимое
// спрашивает, и здесь вопрос — отдельный флаг.
func runImport(ctx context.Context, db *store.Store, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(out)
	as := fs.String("as", "", "почта человека, от чьего имени переносить (обязательно)")
	org := fs.String("org", "", "слаг организации, если человек состоит в нескольких")
	boardN := fs.Int("board", 1, "какая доска пакета, с единицы")
	into := fs.String("into", "", "идентификатор существующей доски, куда переносить")
	name := fs.String("new-board", "", "название новой доски; пусто — как в пакете")
	apply := fs.Bool("apply", false, "перенести; без флага — только предпросмотр")
	fs.Usage = func() {
		fmt.Fprintln(out, "takt import ПАКЕТ.takt --as ПОЧТА [--org СЛАГ] [--board N] [--into ДОСКА | --new-board НАЗВАНИЕ] [--apply]")
		fs.PrintDefaults()
	}
	// Файл — первым: так команду набирают, и флаги после имени файла
	// flag иначе не разобрал бы.
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fs.Usage()
		return errors.New("не назван файл пакета")
	}
	file := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *as == "" {
		fs.Usage()
		return errors.New("не назван человек: --as почта")
	}

	// #nosec G304 G703 -- путь к пакету называет администратор сервера
	// в собственной командной строке: прочитать указанный им файл — ровно
	// то, для чего команда и есть.
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("пакет не читается: %w", err)
	}
	pkg, err := pack.Read(raw)
	if err != nil {
		return err
	}
	plan, err := pkg.Plan(*boardN)
	if err != nil {
		return err
	}

	var userID string
	if err := db.Pool.QueryRow(ctx, `select id from users where lower(email) = lower($1)`, *as).Scan(&userID); err != nil {
		return fmt.Errorf("человека с почтой %s нет в установке", *as)
	}
	memberships, err := auth.Memberships(ctx, db.Pool, userID)
	if err != nil {
		return err
	}
	var member *auth.Membership
	for i, m := range memberships {
		if *org == "" && len(memberships) == 1 || m.OrgSlug == *org {
			member = &memberships[i]
		}
	}
	switch {
	case member == nil && *org == "" && len(memberships) > 1:
		slugs := make([]string, len(memberships))
		for i, m := range memberships {
			slugs[i] = m.OrgSlug
		}
		return fmt.Errorf("%s состоит в нескольких организациях — назовите одну: --org %s", *as, strings.Join(slugs, " | "))
	case member == nil:
		return fmt.Errorf("%s не состоит в организации %q", *as, *org)
	case member.Role == auth.RoleViewer:
		return fmt.Errorf("%s — наблюдатель в «%s» и переносить не может", *as, member.OrgName)
	}

	b := pkg.Boards[*boardN-1]
	target := board.ImportTarget{BoardID: *into, NewBoardName: *name}
	if target.BoardID == "" && target.NewBoardName == "" {
		target.NewBoardName = b.Title
	}
	rep, err := board.New(db).Import(ctx, member.OrgID, userID, target, plan, *apply)
	if err != nil {
		return err
	}
	printImport(out, pkg, *boardN, rep)
	return nil
}

func printImport(out io.Writer, pkg *pack.Package, n int, rep board.ImportReport) {
	m := pkg.Manifest
	fmt.Fprintf(out, "Пакет: %s", pack.Systems[m.Source.System])
	if m.Source.Account != "" {
		fmt.Fprintf(out, " (%s)", m.Source.Account)
	}
	fmt.Fprintf(out, ", собран %s %s\n", m.CreatedBy, m.CreatedAt.Format("2006-01-02"))
	fmt.Fprintf(out, "Доска %d из %d: «%s» → «%s»", n, len(pkg.Boards), pkg.Boards[n-1].Title, rep.BoardName)
	if rep.NewBoard && !rep.Applied {
		fmt.Fprint(out, " (будет заведена)")
	}
	fmt.Fprintln(out)
	verb := "Переедут"
	if rep.Applied {
		verb = "Перенесено"
	}
	fmt.Fprintf(out, "%s: карточек %d из %d, подзадач %d, связей %d, реплик %d\n",
		verb, rep.Created, rep.Rows, rep.Parts, rep.Links, rep.Comments)
	if len(rep.Skipped) > 0 {
		fmt.Fprintf(out, "Уже перенесены раньше и пропущены: %d\n", len(rep.Skipped))
	}
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintln(out, title+":")
		for _, it := range items {
			fmt.Fprintln(out, "  - "+it)
		}
	}
	var cols, people, problems []string
	for _, c := range rep.NewColumns {
		cols = append(cols, c.Name+" · "+c.Kind)
	}
	for _, p := range rep.MissingPeople {
		who := p.Email
		if who == "" {
			who = p.Name + " (без почты)"
		}
		people = append(people, fmt.Sprintf("%s — карточек %d", who, p.Cards))
	}
	for _, p := range rep.Problems {
		problems = append(problems, fmt.Sprintf("карточка %d: %s", p.Row, p.Message))
	}
	list("Новые колонки", cols)
	list("Новые метки", rep.NewLabels)
	list("Почты не найдены — без этих исполнителей", people)
	list("Не переносится", rep.Lost)
	list("Претензии", problems)
	if !rep.Applied {
		fmt.Fprintln(out, "Это предпросмотр: ничего не записано. Перенести — тот же вызов с --apply.")
	}
}
