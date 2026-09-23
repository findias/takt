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
//	takt-fetch jira boards --url …                — то же для Jira
//	takt-fetch jira fetch --url … --board ID --out F
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

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer/pack"
	"github.com/findias/takt/internal/importer/yougile"
	"github.com/findias/takt/internal/version"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr); err != nil {
		// Отказы YouGile и пакета — из общего каталога сервера, свои
		// выгрузчик уже сказал на нужном языке.
		fmt.Fprintln(os.Stderr, "takt-fetch:", i18n.Say(langOf(os.Getenv), err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, env func(string) string, in io.Reader, out, errOut io.Writer) error {
	lang := langOf(env)
	tx := textsFor(lang)
	// Справка — в стандартный вывод и без отказа: её просят, а не
	// получают за ошибку.
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, tx.usage)
		return nil
	}
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(out, version.Строка())
		return nil
	}
	switch args[0] {
	case "yougile":
	case "jira":
		return runJira(ctx, tx, lang, args[1:], env, out, errOut)
	case "kaiten":
		return runKaiten(ctx, tx, lang, args[1:], env, out, errOut)
	default:
		fmt.Fprint(errOut, tx.usage)
		return errors.New(tx.unknownSource)
	}
	if len(args) < 2 || isHelp(args[1]) {
		fmt.Fprint(out, tx.usageYougile)
		return nil
	}
	if args[1] != "boards" && args[1] != "fetch" {
		fmt.Fprint(errOut, tx.usageYougile)
		return errors.New(tx.unknownCommand("yougile", args[1]))
	}
	fs := flag.NewFlagSet("takt-fetch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(out, tx.usage) }
	base := fs.String("url", "https://ru.yougile.com", "")
	company := fs.String("company", "", "")
	var boards multi
	fs.Var(&boards, "board", "")
	everything := fs.Bool("all", false, "")
	file := fs.String("out", "", "")
	noChats := fs.Bool("no-chats", false, "")
	noHistory := fs.Bool("no-history", false, "")
	collectedBy := fs.String("collected-by", "", "")
	if err := fs.Parse(args[2:]); err != nil {
		// --help у команды — та же справка, и тоже не отказ.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	key, err := yougileKey(ctx, tx, *base, *company, env, in, errOut)
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
		return errors.New(tx.noOut)
	}
	if *everything {
		boards = nil
		for _, b := range all {
			boards = append(boards, b.ID)
		}
	}
	if len(boards) == 0 {
		return errors.New(tx.noBoard)
	}
	var collected []pack.Board
	var cards int
	for i, id := range boards {
		fmt.Fprintln(errOut, tx.boardOf(i+1, len(boards)))
		b, err := client.Board(ctx, id, yougile.FetchOptions{
			Chats:    !*noChats,
			History:  !*noHistory,
			Progress: func(s string) { fmt.Fprintln(errOut, "  "+tx.progress(s)) },
		})
		if err != nil {
			// Отказ YouGile — из каталога сервера, на языке выгрузчика.
			return fmt.Errorf("%s: %s", tx.board(id), i18n.Say(lang, err.Error()))
		}
		if len(b.Cards) > pack.MaxCards {
			return errors.New(tx.tooBig(b.Title, len(b.Cards), pack.MaxCards))
		}
		cards += len(b.Cards)
		collected = append(collected, b)
	}

	return save(tx, out, *file, pack.Source{System: yougile.Source, URL: *base}, *collectedBy, collected, cards)
}

// save пишет собранные доски пакетом — одинаково для всех источников.
func save(tx texts, out io.Writer, file string, src pack.Source, collectedBy string, boards []pack.Board, cards int) error {
	m := pack.Manifest{
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		CreatedBy:   "takt-fetch " + version.Строка(),
		CollectedBy: collectedBy,
		Source:      src,
	}
	if err := writeAtomically(file, func(w io.Writer) error { return pack.Write(w, m, boards) }); err != nil {
		return err
	}
	fmt.Fprintln(out, tx.written(file, len(boards), cards))
	fmt.Fprintln(out, tx.carry)
	return nil
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help" || arg == "-help"
}

// yougileKey — ключ API: из окружения, либо по почте и паролю.
func yougileKey(ctx context.Context, tx texts, base, company string, env func(string) string, in io.Reader, errOut io.Writer) (string, error) {
	if key := strings.TrimSpace(env("YOUGILE_KEY")); key != "" {
		return key, nil
	}
	login := strings.TrimSpace(env("YOUGILE_LOGIN"))
	if login == "" {
		return "", errors.New(tx.needLogin)
	}
	password := env("YOUGILE_PASSWORD")
	if password == "" {
		// Пароль видно при наборе: без сторонней библиотеки скрыть ввод
		// нечем. Кому это важно — задайте YOUGILE_PASSWORD или ключ.
		fmt.Fprint(errOut, tx.passwordPrompt(login))
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New(tx.noPassword)
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
		return "", errors.New(tx.noCompany(company, login))
	case len(chosen) > 1:
		names := make([]string, len(chosen))
		for i, c := range chosen {
			names[i] = c.Name
		}
		return "", errors.New(tx.manyCompanies(login, names))
	}
	key, created, err := yougile.Key(ctx, base, login, password, chosen[0].ID)
	if err != nil {
		return "", err
	}
	if created {
		fmt.Fprintln(errOut, tx.keyCreated)
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
