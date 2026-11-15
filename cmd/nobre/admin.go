package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func (c *cli) project(args []string) error {
	const u = "uso: nobre project add [path] [--name <nome>] | nobre project base <branch> [path] | nobre project scan [path] | nobre project list"
	if len(args) == 0 {
		return usagef(u)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ContinueOnError)
		name := fs.String("name", "", "nome do projeto (padrão: nome da pasta)")
		pos, err := c.parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) > 1 {
			return usagef(u)
		}
		path := "."
		if len(pos) == 1 {
			path = pos[0]
		}
		if !filepath.IsAbs(path) {
			wd, err := c.cwd()
			if err != nil {
				return err
			}
			path = filepath.Join(wd, path)
		}
		path = filepath.Clean(path)
		// De dentro de um worktree ligado, cadastra o checkout principal.
		if c.git != nil {
			if main := c.git.Locate(path).Main; main != "" {
				path = main
			}
		}
		p, err := c.client().AddProject(ctx(), *name, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Projeto %s cadastrado: %s (base %s, %d sessões religadas)\n", p.Name, p.Path, p.BaseBranch, p.Reassigned)
	case "base":
		pos, err := c.parse(flag.NewFlagSet("project base", flag.ContinueOnError), args[1:])
		if err != nil {
			return err
		}
		if len(pos) < 1 || len(pos) > 2 {
			return usagef(u)
		}
		path := "."
		if len(pos) == 2 {
			path = pos[1]
		}
		if !filepath.IsAbs(path) {
			wd, err := c.cwd()
			if err != nil {
				return err
			}
			path = filepath.Join(wd, path)
		}
		path = filepath.Clean(path)
		ps, err := c.client().Projects(ctx())
		if err != nil {
			return err
		}
		var found *domain.Project
		for i := range ps {
			if filepath.Clean(ps[i].Path) == path {
				found = &ps[i]
			}
		}
		if found == nil {
			return fmt.Errorf("projeto não cadastrado: %s (use nobre project add)", path)
		}
		if err := c.client().SetProjectBase(ctx(), found.ID, pos[0]); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Base do projeto %s: %s\n", found.Name, pos[0])
	case "scan":
		return c.projectScan(u, args[1:])
	case "list":
		ps, err := c.client().Projects(ctx())
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Fprintln(c.out, "Nenhum projeto cadastrado. Use nobre project add.")
		}
		for _, p := range ps {
			fmt.Fprintf(c.out, "%s\t%s\n", p.Name, p.Path)
		}
	default:
		return usagef(u)
	}
	return nil
}

func (c *cli) prompt(args []string) error {
	const u = "uso: nobre prompt list | nobre prompt use <slug> [--card KEY] | nobre prompt save --name <n> --tag <t>"
	if len(args) == 0 {
		return usagef(u)
	}
	switch args[0] {
	case "list":
		ps, err := c.client().Prompts(ctx())
		if err != nil {
			return err
		}
		for _, p := range ps {
			fmt.Fprintf(c.out, "%s\t%s\t%s\n", p.Slug, p.Tag, p.Name)
		}
	case "use":
		fs := flag.NewFlagSet("prompt use", flag.ContinueOnError)
		card := fs.String("card", "", "card cujas variáveis preenchem o prompt")
		pos, err := c.parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return usagef(u)
		}
		cl := c.client()
		if *card == "" && cl.PID > 0 {
			// Sem --card, usa o card da sessão, se houver.
			if st, err := cl.Status(ctx()); err == nil && st.Card != nil {
				*card = st.Card.Key
			}
		}
		text, err := cl.RenderPrompt(ctx(), pos[0], *card)
		if err != nil {
			return err
		}
		fmt.Fprint(c.out, text)
		if !strings.HasSuffix(text, "\n") {
			fmt.Fprintln(c.out)
		}
	case "save":
		fs := flag.NewFlagSet("prompt save", flag.ContinueOnError)
		name := fs.String("name", "", "nome do prompt")
		tag := fs.String("tag", "", "tag do prompt")
		pos, err := c.parse(fs, args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 0 || strings.TrimSpace(*name) == "" {
			return usagef(u)
		}
		body, err := io.ReadAll(c.in)
		if err != nil {
			return err
		}
		p, err := c.client().SavePrompt(ctx(), domain.Prompt{Name: *name, Slug: slugify(*name), Tag: *tag,
			Body: strings.TrimRight(string(body), "\n"), Agents: []string{}})
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Prompt salvo: %s\n", p.Slug)
	default:
		return usagef(u)
	}
	return nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

func (c *cli) ui() error {
	url := "http://" + c.addr()
	if err := c.opener(url); err != nil {
		return fmt.Errorf("não consegui abrir o navegador (%v). Abra %s manualmente.", err, url)
	}
	fmt.Fprintf(c.out, "Abrindo %s\n", url)
	return nil
}

// projectScan imprime o levantamento do projeto que contém path (padrão: cwd).
func (c *cli) projectScan(u string, args []string) error {
	pos, err := c.parse(flag.NewFlagSet("project scan", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("%s", u)
	}
	path := "."
	if len(pos) == 1 {
		path = pos[0]
	}
	if !filepath.IsAbs(path) {
		wd, err := c.cwd()
		if err != nil {
			return err
		}
		path = filepath.Join(wd, path)
	}
	path = filepath.Clean(path)
	if c.git != nil { // de dentro de um worktree ligado, vale o checkout principal
		if main := c.git.Locate(path).Main; main != "" {
			path = main
		}
	}
	ps, err := c.client().Projects(ctx())
	if err != nil {
		return err
	}
	var found *domain.Project
	for i := range ps {
		pp := filepath.Clean(ps[i].Path)
		if (path == pp || strings.HasPrefix(path, pp+string(filepath.Separator))) &&
			(found == nil || len(pp) > len(filepath.Clean(found.Path))) {
			found = &ps[i]
		}
	}
	if found == nil {
		return fmt.Errorf("projeto não cadastrado: %s (use nobre project add)", path)
	}
	s, err := c.client().ScanProject(ctx(), found.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	age := func(t time.Time) string {
		d := int(now.Sub(t).Hours() / 24)
		if d <= 0 {
			return "hoje"
		}
		return fmt.Sprintf("há %dd", d)
	}
	fmt.Fprintf(c.out, "Projeto: %s (%s)\nBase: %s\n", found.Name, s.Repo, s.Base)
	fmt.Fprintf(c.out, "\nBranches com trabalho fora da base (%d):\n", len(s.Branches))
	for _, b := range s.Branches {
		wt := ""
		if b.Worktree != "" {
			wt = "  [worktree " + b.Worktree + "]"
		}
		rec := ""
		if b.Recent {
			rec = " (recente)"
		}
		fmt.Fprintf(c.out, "  %s%s  %s, %d à frente  \"%s\"%s\n", b.Name, rec, age(b.LastCommit), b.Ahead, b.Subject, wt)
	}
	fmt.Fprintf(c.out, "\nPlanos com tarefas abertas (%d):\n", len(s.Plans))
	for _, p := range s.Plans {
		fmt.Fprintf(c.out, "  %s  %d abertas, %d feitas (%s)\n", p.Path, p.Open, p.Done, age(p.Modified))
	}
	fmt.Fprintf(c.out, "\nSessões recentes com custo (%d):\n", len(s.Sessions))
	for _, x := range s.Sessions {
		fmt.Fprintf(c.out, "  %s  %s  US$ %.2f  branch %s  %s\n", x.ID, age(x.StartedAt), x.CostUSD, x.Branch, x.Cwd)
	}
	fmt.Fprintf(c.out, "\nCards que já existem (%d):\n", len(s.Cards))
	for _, k := range s.Cards {
		fmt.Fprintf(c.out, "  %s  [%s]  %s  %s\n", k.Key, k.Column, k.Title, k.Branch)
	}
	return nil
}
