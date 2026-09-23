package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer/monday"
	"github.com/findias/takt/internal/importer/pack"
)

// runMonday — takt-fetch monday boards|fetch.
//
// Вход — личным токеном из окружения, MONDAY_TOKEN. Адрес API у monday
// один, своей установки нет; --url оставлен для прокси и проверок.
func runMonday(ctx context.Context, tx texts, lang i18n.Lang, args []string, env func(string) string, out, errOut io.Writer) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, tx.usageMonday)
		return nil
	}
	if args[0] != "boards" && args[0] != "fetch" {
		fmt.Fprint(errOut, tx.usageMonday)
		return errors.New(tx.unknownCommand("monday", args[0]))
	}
	fs := flag.NewFlagSet("takt-fetch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(out, tx.usage) }
	base := fs.String("url", monday.DefaultURL, "")
	var boards multi
	fs.Var(&boards, "board", "")
	file := fs.String("out", "", "")
	noComments := fs.Bool("no-comments", false, "")
	column := fs.String("column", "", "")
	collectedBy := fs.String("collected-by", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	token := strings.TrimSpace(env("MONDAY_TOKEN"))
	if token == "" {
		return errors.New(tx.mondayNeedLogin)
	}
	client := monday.New(*base, token)

	if args[0] == "boards" {
		all, err := client.Boards(ctx)
		if err != nil {
			return errors.New(i18n.Say(lang, err.Error()))
		}
		for _, b := range all {
			fmt.Fprintf(out, "%s\t%s · %s\n", b.ID, b.Workspace, b.Name)
		}
		return nil
	}

	if *file == "" {
		return errors.New(tx.noOut)
	}
	if len(boards) == 0 {
		return errors.New(tx.mondayNoBoard)
	}
	var collected []pack.Board
	var cards int
	for i, id := range boards {
		fmt.Fprintln(errOut, tx.boardOf(i+1, len(boards)))
		b, err := client.Board(ctx, id, monday.FetchOptions{
			Column:   strings.TrimSpace(*column),
			Comments: !*noComments,
			Progress: func(s string) { fmt.Fprintln(errOut, "  "+tx.progress(s)) },
			Chosen: func(byGroup bool, status string) {
				fmt.Fprintln(errOut, "  "+tx.mondayColumns(byGroup, status))
			},
		})
		if err != nil {
			return fmt.Errorf("%s: %s", tx.board(id), i18n.Say(lang, err.Error()))
		}
		cards += len(b.Cards)
		collected = append(collected, b)
	}
	return save(tx, out, *file, pack.Source{System: monday.Source, URL: *base}, *collectedBy, collected, cards)
}
