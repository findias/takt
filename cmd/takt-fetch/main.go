// Команда takt-fetch — выгрузчик для переезда в закрытый контур
// (docs/import-package.md).
//
// Работает там, где есть интернет: заходит в чужой трекер, собирает
// доски и пишет пакет переноса (.takt). Пакет несут через периметр,
// и takt внутри переносит его экраном или командой `takt import`.
//
// Отдельная программа, а не подкоманда takt: бинарник, который ставят
// в закрытый контур, не должен уметь ходить за чужими досками в чужие
// облака, и проверяющему проще сказать «этого кода в поставке нет».
//
//	takt-fetch yougile boards                     — какие доски есть
//	takt-fetch yougile fetch --board ID --out F   — собрать пакет
//
// Учётные данные — только из окружения или с клавиатуры, не флагами:
// флаги видны в списке процессов и остаются в истории оболочки.
// В пакет они не попадают никогда.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/findias/takt/internal/importer/pack"
	"github.com/findias/takt/internal/importer/yougile"
	"github.com/findias/takt/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "takt-fetch:", err)
		os.Exit(1)
	}
}

const usage = `takt-fetch — выгрузчик досок в пакет переноса takt

  takt-fetch yougile boards [флаги]
  takt-fetch yougile fetch --board ID [--board ID …] --out ФАЙЛ.takt [флаги]
  takt-fetch version

Вход в YouGile — одно из двух:
  YOUGILE_KEY=ключ                          ключ API компании
  YOUGILE_LOGIN=почта [YOUGILE_PASSWORD=…]  почта и пароль; пароль, если не задан,
                                            спросят с клавиатуры

Флаги:
  --url АДРЕС          адрес YouGile (по умолчанию https://ru.yougile.com)
  --company ИМЯ        компания, если их у почты несколько
  --board ID           доска; можно несколько раз; --all — все доски компании
  --out ФАЙЛ           куда записать пакет
  --no-chats           без чатов задач: быстрее, но обсуждение не переедет
  --collected-by ТЕКСТ кто собрал и зачем — попадёт в пакет как есть
`

func run(ctx context.Context, args []string, env func(string) string, in io.Reader, out, errOut io.Writer) error {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(out, version.Строка())
		return nil
	}
	if len(args) < 2 || args[0] != "yougile" || (args[1] != "boards" && args[1] != "fetch") {
		fmt.Fprint(errOut, usage)
		return errors.New("источник пока один — yougile; остальные придут следующими")
	}
	fs := flag.NewFlagSet("takt-fetch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(errOut, usage) }
	base := fs.String("url", "https://ru.yougile.com", "")
	company := fs.String("company", "", "")
	var boards multi
	fs.Var(&boards, "board", "")
	everything := fs.Bool("all", false, "")
	file := fs.String("out", "", "")
	noChats := fs.Bool("no-chats", false, "")
	collectedBy := fs.String("collected-by", "", "")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	key, err := yougileKey(ctx, *base, *company, env, in, errOut)
	if err != nil {
		return err
	}
	client := yougile.New(*base, key)
	client.Patient = true
	all, err := client.Boards(ctx)
	if err != nil {
		return err
	}

	if args[1] == "boards" {
		for _, b := range all {
			fmt.Fprintf(out, "%s\t%s · %s\n", b.ID, b.Project, b.Title)
		}
		return nil
	}

	if *file == "" {
		return errors.New("не назван файл пакета: --out склад.takt")
	}
	if *everything {
		boards = nil
		for _, b := range all {
			boards = append(boards, b.ID)
		}
	}
	if len(boards) == 0 {
		return errors.New("не названа ни одна доска: --board ID (список — takt-fetch yougile boards) или --all")
	}
	var collected []pack.Board
	var cards int
	for i, id := range boards {
		fmt.Fprintf(errOut, "доска %d из %d…\n", i+1, len(boards))
		b, err := client.Board(ctx, id, yougile.FetchOptions{
			Chats:    !*noChats,
			Progress: func(s string) { fmt.Fprintln(errOut, "  "+s) },
		})
		if err != nil {
			return fmt.Errorf("доска %s: %w", id, err)
		}
		if len(b.Cards) > pack.MaxCards {
			return fmt.Errorf("на доске «%s» %d карточек — takt переносит до %d за раз; разделите её в YouGile", b.Title, len(b.Cards), pack.MaxCards)
		}
		cards += len(b.Cards)
		collected = append(collected, b)
	}

	m := pack.Manifest{
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		CreatedBy:   "takt-fetch " + version.Строка(),
		CollectedBy: *collectedBy,
		Source:      pack.Source{System: yougile.Source, URL: *base},
	}
	if err := writeAtomically(*file, func(w io.Writer) error { return pack.Write(w, m, collected) }); err != nil {
		return err
	}
	fmt.Fprintf(out, "Пакет записан: %s — досок %d, карточек %d.\n", *file, len(collected), cards)
	fmt.Fprintln(out, "Перенесите его в закрытый контур и откройте в takt: «Перенос задач» → «Пакет переноса».")
	return nil
}

// yougileKey — ключ API: из окружения, либо по почте и паролю.
func yougileKey(ctx context.Context, base, company string, env func(string) string, in io.Reader, errOut io.Writer) (string, error) {
	if key := strings.TrimSpace(env("YOUGILE_KEY")); key != "" {
		return key, nil
	}
	login := strings.TrimSpace(env("YOUGILE_LOGIN"))
	if login == "" {
		return "", errors.New("нужен вход в YouGile: YOUGILE_KEY или YOUGILE_LOGIN (и пароль)")
	}
	password := env("YOUGILE_PASSWORD")
	if password == "" {
		// Пароль видно при наборе: без сторонней библиотеки скрыть ввод
		// нечем. Кому это важно — задайте YOUGILE_PASSWORD или ключ.
		fmt.Fprint(errOut, "Пароль YouGile для "+login+" (виден при наборе; YOUGILE_PASSWORD его заменяет): ")
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("пароль не введён")
		}
		password = strings.TrimRight(line, "\r\n")
	}
	companies, err := yougile.Companies(ctx, base, login, password)
	if err != nil {
		return "", err
	}
	var chosen []yougile.Company
	for _, c := range companies {
		if company == "" || strings.EqualFold(c.Name, company) || c.ID == company {
			chosen = append(chosen, c)
		}
	}
	switch {
	case len(chosen) == 0:
		return "", fmt.Errorf("компании %q у %s нет", company, login)
	case len(chosen) > 1:
		names := make([]string, len(chosen))
		for i, c := range chosen {
			names[i] = c.Name
		}
		return "", fmt.Errorf("у %s несколько компаний — назовите одну: --company «%s»", login, strings.Join(names, "» | «"))
	}
	key, created, err := yougile.Key(ctx, base, login, password, chosen[0].ID)
	if err != nil {
		return "", err
	}
	if created {
		fmt.Fprintln(errOut, "В YouGile заведён ключ API для выгрузки; когда закончите, его можно удалить там.")
	}
	return key, nil
}

// writeAtomically пишет во временный файл рядом и переименовывает:
// прерванная выгрузка не оставит полупакета под нужным именем, а пакет
// — не для чужих глаз, поэтому 0600.
func writeAtomically(name string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(name), ".takt-fetch-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := write(tmp); err != nil {
		_ = tmp.Close() // отказ записи важнее отказа закрытия
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), name)
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }
