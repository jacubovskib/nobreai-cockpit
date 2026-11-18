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
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

func (c *cli) project(args []string) error {
	const u = "uso: nobre project add [path] [--name <nome>] | nobre project base <branch> [path] | nobre project scan [path] [--nao-sugerir rel,rel] | nobre project list"
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
		arg := ""
		if len(pos) == 1 {
			arg = pos[0]
		}
		path, loc, err := c.projectPath(arg)
		if err != nil {
			return err
		}
		if c.git != nil && loc.Root == "" {
			return fmt.Errorf("%s não é um repositório git: o NobreAI Cockpit só cadastra repositórios git", path)
		}
		p, err := c.client().AddProject(ctx(), *name, path)
		if err != nil {
			return err
		}
		var info []string
		if p.BaseBranch != "" {
			info = append(info, "base "+p.BaseBranch)
		}
		if p.Reassigned == 1 {
			info = append(info, "1 sessão religada")
		} else {
			info = append(info, fmt.Sprintf("%d sessões religadas", p.Reassigned))
		}
		fmt.Fprintf(c.out, "Projeto %s cadastrado: %s (%s)\n", p.Name, p.Path, strings.Join(info, ", "))
		if sg := p.Suggestions; len(sg) > 0 {
			const max = 5
			fmt.Fprintln(c.out, "Sub-projetos sugeridos (liste ao usuário, não cadastre sem ele pedir):")
			for i, x := range sg {
				if i == max {
					fmt.Fprintf(c.out, "  mais %d na Configuração → Projetos\n", len(sg)-max)
					break
				}
				fmt.Fprintf(c.out, "  %s  (%s)\n", x.Rel, x.Source)
			}
		}
	case "base":
		pos, err := c.parse(flag.NewFlagSet("project base", flag.ContinueOnError), args[1:])
		if err != nil {
			return err
		}
		if len(pos) < 1 || len(pos) > 2 {
			return usagef(u)
		}
		arg := ""
		if len(pos) == 2 {
			arg = pos[1]
		}
		found, err := c.findProject(arg)
		if err != nil {
			return err
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
	fs := flag.NewFlagSet("project scan", flag.ContinueOnError)
	noSuggest := fs.String("nao-sugerir", "", "sub-projetos a não sugerir mais (pastas relativas, separadas por vírgula)")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("%s", u)
	}
	arg := ""
	if len(pos) == 1 {
		arg = pos[0]
	}
	found, err := c.findProject(arg)
	if err != nil {
		return err
	}
	if *noSuggest != "" {
		var rels []string
		for _, r := range strings.Split(*noSuggest, ",") {
			if r = strings.TrimSpace(r); r != "" {
				rels = append(rels, r)
			}
		}
		if err := c.client().DismissSuggestions(ctx(), found.ID, rels); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Não vou mais sugerir: %s\n", strings.Join(rels, ", "))
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
	if len(s.Suggestions) > 0 {
		fmt.Fprintf(c.out, "\nSub-projetos sugeridos (%d):\n", len(s.Suggestions))
		for _, x := range s.Suggestions {
			fmt.Fprintf(c.out, "  %s  (%s)\n", x.Rel, x.Source)
		}
	}
	return nil
}

// projectPath resolve arg (vazio = cwd) como o hook resolve o cwd da sessão:
// absoluto e, num worktree ligado, o mesmo lugar dentro do checkout principal.
// Fora disso fica como veio (uma subpasta continua subpasta). Devolve também
// onde o caminho fica no git (vazio sem git ou fora de repositório).
func (c *cli) projectPath(arg string) (string, gitinfo.Location, error) {
	path := arg
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		wd, err := c.cwd()
		if err != nil {
			return "", gitinfo.Location{}, err
		}
		path = filepath.Join(wd, path)
	}
	path = filepath.Clean(path)
	if c.git == nil {
		return path, gitinfo.Location{}, nil
	}
	loc := c.git.Locate(path)
	if loc.Linked && loc.Main != "" {
		if rel, err := filepath.Rel(loc.Root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			path = filepath.Join(loc.Main, rel)
		}
	}
	return path, loc, nil
}

// findProject acha o projeto que contém arg (vazio = cwd), resolvido como em
// projectPath: o de caminho mais longo que é o próprio caminho ou o contém
// (o mesmo limite de diretório de ProjectForPath).
func (c *cli) findProject(arg string) (*domain.Project, error) {
	path, _, err := c.projectPath(arg)
	if err != nil {
		return nil, err
	}
	ps, err := c.client().Projects(ctx())
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("projeto não cadastrado: %s (use nobre project add)", path)
	}
	return found, nil
}
