package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/findias/takt/internal/i18n"
	"github.com/findias/takt/internal/importer/kaiten"
	"github.com/findias/takt/internal/importer/pack"
)

// runKaiten — takt-fetch kaiten boards|fetch.
//
// Вход — токеном из окружения, KAITEN_TOKEN, как у Jira: флаг виден
// в списке процессов и остаётся в истории оболочки. Адрес обязателен —
// у каждой компании свой (`компания.kaiten.ru` или адрес коробки).
func runKaiten(ctx context.Context, tx texts, lang i18n.Lang, args []string, env func(string) string, out, errOut io.Writer) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, tx.usageKaiten)
		return nil
	}
	if args[0] != "boards" && args[0] != "fetch" {
		fmt.Fprint(errOut, tx.usageKaiten)
		return errors.New(tx.unknownCommand("kaiten", args[0]))
	}
	fs := flag.NewFlagSet("takt-fetch", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() { fmt.Fprint(out, tx.usage) }
	base := fs.String("url", "", "")
	var boards multi
	fs.Var(&boards, "board", "")
	file := fs.String("out", "", "")
	noComments := fs.Bool("no-comments", false, "")
	collectedBy := fs.String("collected-by", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if u, err := url.Parse(*base); *base == "" || err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return errors.New(tx.kaitenNoURL)
	}
	token := strings.TrimSpace(env("KAITEN_TOKEN"))
	if token == "" {
		return errors.New(tx.kaitenNeedLogin)
	}
	client := kaiten.New(*base, token)

	if args[0] == "boards" {
		all, err := client.Boards(ctx)
		if err != nil {
			return errors.New(i18n.Say(lang, err.Error()))
		}
		for _, b := range all {
			fmt.Fprintf(out, "%s\t%s · %s\n", b.ID, b.Space, b.Title)
		}
		return nil
	}

	if *file == "" {
		return errors.New(tx.noOut)
	}
	if len(boards) == 0 {
		return errors.New(tx.kaitenNoBoard)
	}
	var collected []pack.Board
	var cards int
	for i, id := range boards {
		fmt.Fprintln(errOut, tx.boardOf(i+1, len(boards)))
		b, err := client.Board(ctx, id, kaiten.FetchOptions{
			Comments: !*noComments,
			Progress: func(s string) { fmt.Fprintln(errOut, "  "+tx.progress(s)) },
			Lane:     tx.lane,
		})
		if err != nil {
			return fmt.Errorf("%s: %s", tx.board(id), i18n.Say(lang, err.Error()))
		}
		cards += len(b.Cards)
		collected = append(collected, b)
	}
	return save(tx, out, *file, pack.Source{System: kaiten.Source, URL: *base}, *collectedBy, collected, cards)
}
