package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func (c *cli) project(args []string) error {
	const u = "uso: nobre project add [path] [--name <nome>] | nobre project list"
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
		p, err := c.client().AddProject(ctx(), *name, filepath.Clean(path))
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Projeto %s cadastrado: %s\n", p.Name, p.Path)
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
