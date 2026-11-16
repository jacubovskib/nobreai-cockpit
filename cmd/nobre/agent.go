package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

func (c *cli) start(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	goal := fs.String("goal", "", "objetivo do card")
	done := fs.String("done-when", "", "critério de pronto")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	title := joinText(pos)
	if title == "" {
		return usagef(`uso: nobre start "<título>" [--goal ...] [--done-when ...]`)
	}
	card, err := c.client().Start(ctx(), title, *goal, *done)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Card %s criado e ligado a esta sessão.\n", card.Key)
	return nil
}

func (c *cli) card(args []string) error {
	const u = `uso: nobre card add "<título>" [--goal ...] [--done-when ...] [--branch ...] [--where "<onde parei>"] [--column backlog|pausado]`
	if len(args) == 0 || args[0] != "add" {
		return usagef(u)
	}
	fs := flag.NewFlagSet("card add", flag.ContinueOnError)
	goal := fs.String("goal", "", "objetivo do card")
	done := fs.String("done-when", "", "critério de pronto")
	branch := fs.String("branch", "", "branch do card")
	where := fs.String("where", "", `"Onde parei" do card`)
	column := fs.String("column", "", "backlog (padrão) ou pausado")
	pos, err := c.parse(fs, args[1:])
	if err != nil {
		return err
	}
	title := joinText(pos)
	if title == "" {
		return usagef(u)
	}
	card, err := c.client().AddCard(ctx(), map[string]string{"title": title, "goal": *goal, "done_when": *done,
		"branch": *branch, "where_stopped": *where, "column": *column})
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Card %s criado no %s (sessão não ligada).\n", card.Key, card.Column)
	return nil
}

func (c *cli) resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	force := fs.Bool("force", false, "assume o card mesmo com outra sessão viva")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef("uso: nobre resume <KEY> [--force]")
	}
	md, err := c.client().Resume(ctx(), pos[0], *force)
	if err != nil {
		return err
	}
	fmt.Fprint(c.out, md)
	if !strings.HasSuffix(md, "\n") {
		fmt.Fprintln(c.out)
	}
	return nil
}

// simple envia {field: texto} para /api/me/<action>.
func (c *cli) simple(action, what string, args []string, field string) error {
	pos, err := c.parse(flag.NewFlagSet(action, flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	text := joinText(pos)
	if text == "" {
		return usagef(`uso: nobre %s "<%s>"`, action, what)
	}
	if err := c.client().Me(ctx(), action, map[string]string{field: text}); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Registrado.")
	return nil
}

func (c *cli) ask(args []string) error {
	pos, err := c.parse(flag.NewFlagSet("ask", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	text := joinText(pos)
	if text == "" {
		return usagef(`uso: nobre ask "<pergunta>"`)
	}
	q, err := c.client().Ask(ctx(), text)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Pergunta %d registrada.\n", q.ID)
	return nil
}

func (c *cli) answer(args []string) error {
	const u = `uso: nobre answer <id> "<resposta>"`
	pos, err := c.parse(flag.NewFlagSet("answer", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) < 2 {
		return usagef(u)
	}
	id, err := strconv.ParseInt(pos[0], 10, 64)
	if err != nil {
		return usagef(u)
	}
	text := joinText(pos[1:])
	if text == "" {
		return usagef(u)
	}
	if err := c.client().AnswerQuestion(ctx(), id, text); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Resposta registrada.")
	return nil
}

func (c *cli) block(args []string) error {
	const u = `uso: nobre block <KEY> [--why "<motivo>"] | nobre block --clear`
	fs := flag.NewFlagSet("block", flag.ContinueOnError)
	why := fs.String("why", "", "motivo")
	clear := fs.Bool("clear", false, "remove o bloqueio")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if *clear {
		if len(pos) != 0 {
			return usagef(u)
		}
		if _, err := c.client().Block(ctx(), "", ""); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Bloqueio removido.")
		return nil
	}
	if len(pos) != 1 {
		return usagef(u)
	}
	card, err := c.client().Block(ctx(), pos[0], *why)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Card %s bloqueado por %s.\n", card.Key, card.BlockedBy)
	return nil
}

func (c *cli) branch(args []string) error {
	const u = `uso: nobre branch <nome>`
	pos, err := c.parse(flag.NewFlagSet("branch", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return usagef(u)
	}
	if err := c.client().SetBranch(ctx(), pos[0]); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Branch do card atualizado.")
	return nil
}

func (c *cli) decide(args []string) error {
	fs := flag.NewFlagSet("decide", flag.ContinueOnError)
	why := fs.String("why", "", "porquê da decisão")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	what := joinText(pos)
	if what == "" {
		return usagef(`uso: nobre decide "<decisão>" --why "<porquê>"`)
	}
	if err := c.client().Me(ctx(), "decide", map[string]string{"what": what, "why": *why}); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Decisão registrada.")
	return nil
}

func (c *cli) file(args []string) error {
	fs := flag.NewFlagSet("file", flag.ContinueOnError)
	why := fs.String("why", "", "motivo de o arquivo ser essencial")
	lines := fs.String("lines", "", "trecho, ex.: L12-96")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *why == "" {
		return usagef(`uso: nobre file <path> --why "<motivo>" [--lines L12-96]`)
	}
	if err := c.client().Me(ctx(), "file", map[string]string{"path": pos[0], "why": *why, "lines": *lines}); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Arquivo essencial registrado.")
	return nil
}

func (c *cli) todo(args []string) error {
	const u = `uso: nobre todo add "<texto>" | nobre todo done <id>`
	if len(args) < 2 {
		return usagef(u)
	}
	switch args[0] {
	case "add":
		text := joinText(args[1:])
		if err := c.client().Me(ctx(), "todo", map[string]string{"text": text}); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Pendência adicionada.")
	case "done":
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || len(args) != 2 {
			return usagef(u)
		}
		if err := c.client().Me(ctx(), "todo-done", map[string]int64{"id": id}); err != nil {
			return err
		}
		fmt.Fprintln(c.out, "Pendência concluída.")
	default:
		return usagef(u)
	}
	return nil
}

func (c *cli) done(args []string) error {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	force := fs.Bool("force", false, "marca como pronto mesmo com pendências")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 0 {
		return usagef("uso: nobre done [--force]")
	}
	if err := c.client().Me(ctx(), "done", map[string]bool{"force": *force}); err != nil {
		return err
	}
	fmt.Fprintln(c.out, "Card marcado como pronto para revisão.")
	return nil
}

func (c *cli) status() error {
	st, err := c.client().Status(ctx())
	if err != nil {
		return err
	}
	if st.Card == nil {
		return fmt.Errorf(`Esta sessão ainda não tem card. Use nobre start "<título>" ou nobre resume <KEY>.`)
	}
	k := st.Card
	fmt.Fprintf(c.out, "%s  %s  [%s]\n", k.Key, k.Title, k.Column)
	where := k.WhereStopped
	if where == "" {
		where = "(vazio)"
	}
	fmt.Fprintf(c.out, "Onde parei: %s\n", where)
	fmt.Fprintf(c.out, "Prontidão: %d/%d\n", k.Readiness.Met, k.Readiness.Total)
	for _, it := range k.Readiness.Items {
		if !it.OK {
			fmt.Fprintf(c.out, "  falta: %s\n", it.Label)
		}
	}
	var open []string
	for _, q := range k.Content.Questions {
		if q.Answer == "" {
			open = append(open, fmt.Sprintf("- [%d] %s", q.ID, q.Text))
		}
	}
	if len(open) > 0 {
		fmt.Fprintf(c.out, "Perguntas em aberto:\n%s\n", strings.Join(open, "\n"))
	}
	return nil
}
