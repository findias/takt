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
	"github.com/findias/takt/internal/importer/jira"
	"github.com/findias/takt/internal/importer/pack"
)

// runJira — takt-fetch jira boards|fetch.
//
// Вход — из окружения, как у YouGile: JIRA_EMAIL и JIRA_TOKEN — облако,
// один JIRA_TOKEN — своя установка (личный токен). Пароля нет вовсе:
// облако паролем в API не пускает, а своя установка пускает, но пароль
// человека в утилите переезда — ровно то, от чего уходим ключами.
func runJira(ctx context.Context, tx texts, lang i18n.Lang, args []string, env func(string) string, out, errOut io.Writer) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, tx.usageJira)
		return nil
	}
	if args[0] != "boards" && args[0] != "fetch" {
		fmt.Fprint(errOut, tx.usageJira)
		return errors.New(tx.unknownCommand("jira", args[0]))
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
		return errors.New(tx.jiraNoURL)
	}
	email := strings.TrimSpace(env("JIRA_EMAIL"))
	token := strings.TrimSpace(env("JIRA_TOKEN"))
	if token == "" {
		return errors.New(tx.jiraNeedLogin)
	}
	client := jira.New(*base, email, token)

	if args[0] == "boards" {
		all, err := client.Boards(ctx)
		if err != nil {
			return err
		}
		for _, b := range all {
			fmt.Fprintf(out, "%s\t%s · %s (%s)\n", b.ID, b.Project, b.Name, b.Type)
		}
		return nil
	}

	if *file == "" {
		return errors.New(tx.noOut)
	}
	if len(boards) == 0 {
		return errors.New(tx.jiraNoBoard)
	}
	var collected []pack.Board
	var cards int
	for i, id := range boards {
		fmt.Fprintln(errOut, tx.boardOf(i+1, len(boards)))
		b, err := client.Board(ctx, id, jira.FetchOptions{
			Comments: !*noComments,
			Progress: func(s string) { fmt.Fprintln(errOut, "  "+tx.progress(s)) },
		})
		if err != nil {
			return fmt.Errorf("%s: %s", tx.board(id), i18n.Say(lang, err.Error()))
		}
		cards += len(b.Cards)
		collected = append(collected, b)
	}
	return save(tx, out, *file, pack.Source{System: jira.Source, URL: *base}, *collectedBy, collected, cards)
}
