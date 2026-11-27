package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/client"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/workspaces"
)

func (c *cli) project(args []string) error {
	const u = "uso: nobre project add [path] [--name <nome>] [--pai <id|pasta>] [--sem-filhos] | nobre project pai <id|pasta> <id|pasta|--nenhum> | nobre project grupo \"<nome>\" | nobre project base <branch> [path] | nobre project scan [path] [--nao-sugerir rel,rel] | nobre project list"
	if len(args) == 0 {
		return usagef(u)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ContinueOnError)
		name := fs.String("name", "", "nome do projeto (padrão: nome da pasta)")
		pai := fs.String("pai", "", "projeto principal (id ou pasta)")
		semFilhos := fs.Bool("sem-filhos", false, "numa pasta de repositórios, cadastra só o grupo")
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
			if *pai != "" {
				return fmt.Errorf("%s não é um repositório git: o NobreAI Cockpit só cadastra repositórios git", path)
			}
			return c.addGroupFolder(*name, path, *semFilhos)
		}
		var p client.AddedProject
		if *pai != "" {
			parent, perr := c.findProjectRef(*pai)
			if perr != nil {
				return perr
			}
			p, err = c.client().AddChildProject(ctx(), *name, path, parent.ID)
		} else {
			p, err = c.client().AddProject(ctx(), *name, path)
		}
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
		if p.AttachedTo != "" {
			fmt.Fprintf(c.out, "Ligado ao grupo %s.\n", p.AttachedTo)
		}
		if p.NotAttached != "" {
			fmt.Fprintf(c.out, "Não ligado ao grupo %s: já existe um projeto com esse nome nele.\n", p.NotAttached)
		}
		c.printMoved(p.Moved, p.Name)
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
	case "grupo":
		pos, err := c.parse(flag.NewFlagSet("project grupo", flag.ContinueOnError), args[1:])
		if err != nil {
			return err
		}
		if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
			return usagef(u)
		}
		g, err := c.client().AddGroup(ctx(), pos[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(c.out, "Grupo %s cadastrado (id %d). Use nobre project add <pasta> --pai %d para pôr repositórios nele.\n", g.Name, g.ID, g.ID)
	case "pai":
		return c.projectPai(u, args[1:])
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

// addGroupFolder cadastra uma pasta que não é git como grupo com pasta e liga
// a ele os repositórios que a varredura acha dentro dela: os novos e os
// projetos soltos; o que pertence a outro projeto fica de fora e é citado.
// Os repositórios vão sem nome: o daemon usa o da pasta e, se ele já existe no
// grupo, um nome distinto, que a saída mostra; o projeto solto cujo nome já
// existe no grupo não é ligado e é citado.
// Num grupo já cadastrado, só acrescenta os repositórios novos: um projeto
// solto dentro da pasta não é religado, só citado. Com semFilhos,
// cadastra só o grupo e lista os repositórios como sugestões. Pasta sem
// repositórios continua recusada.
func (c *cli) addGroupFolder(name, path string, semFilhos bool) error {
	notGit := fmt.Errorf("%s não é um repositório git: o NobreAI Cockpit só cadastra repositórios git", path)
	scan, err := c.client().ScanFolder(ctx(), path)
	if err != nil {
		// Só a recusa de "sem repositórios" vira a mensagem de não git; as
		// outras (a home, fora da home) chegam como o daemon as escreveu.
		var ae *client.APIError
		if errors.As(err, &ae) && ae.Status == http.StatusBadRequest && ae.Message == workspaces.MsgNoRepos {
			return notGit
		}
		return err
	}
	if len(scan.Suggestions) == 0 {
		return notGit
	}
	groupID := int64(0)
	if scan.Registered != nil {
		groupID = scan.Registered.ID
	}
	var kids []client.BatchChild
	var sent, open, out []string
	for _, x := range scan.Suggestions {
		switch reg := x.Registered; {
		case x.Blocked != "":
			out = append(out, fmt.Sprintf("%s (%s)", x.Rel, x.Blocked))
		case reg != nil && reg.ParentID != nil && *reg.ParentID == groupID:
			// já é filho deste grupo
		case reg != nil && scan.Registered != nil:
			// Projeto solto dentro de um grupo que já existia: pode ter sido
			// desligado pelo usuário (project pai --nenhum). Religar trocaria a
			// captura dele pela do grupo; só quem pede (project pai) religa.
			out = append(out, fmt.Sprintf("%s (projeto solto: ligue com nobre project pai)", x.Rel))
		default:
			if reg == nil {
				open = append(open, fmt.Sprintf("  %s  (%s)\n", x.Rel, x.Source))
			}
			if !semFilhos {
				kids = append(kids, client.BatchChild{Path: x.Path})
				sent = append(sent, x.Rel)
			}
		}
	}
	res, err := c.client().ProjectsBatch(ctx(), name, path, kids)
	if err != nil {
		return err
	}
	state := "cadastrado"
	if scan.Registered != nil {
		state = "já cadastrado"
	}
	fmt.Fprintf(c.out, "Grupo %s %s: %s\n", res.Project.Name, state, res.Project.Path)
	c.printMoved(res.Moved, res.Project.Name)
	if !semFilhos {
		// O que o daemon ligou, pelo caminho: o nome pode não ser o da pasta e
		// o que não voltou ficou de fora.
		names := map[string]string{}
		for _, k := range res.Children {
			names[k.Path] = k.Name
		}
		var linked, clash []string
		for i, k := range kids {
			switch name, ok := names[k.Path]; {
			case !ok:
				clash = append(clash, sent[i])
			case name != filepath.Base(k.Path):
				linked = append(linked, fmt.Sprintf("%s (como %s)", sent[i], name))
			default:
				linked = append(linked, sent[i])
			}
		}
		if len(linked) > 0 {
			fmt.Fprintf(c.out, "Repositórios ligados ao grupo: %s\n", strings.Join(linked, ", "))
		}
		if len(clash) > 0 {
			fmt.Fprintf(c.out, "Não ligados, já existe um projeto com esse nome no grupo: %s\n", strings.Join(clash, ", "))
		}
		if len(out) > 0 {
			fmt.Fprintf(c.out, "Fora do grupo: %s\n", strings.Join(out, ", "))
		}
		return nil
	}
	if len(open) == 0 {
		return nil
	}
	const max = 5
	fmt.Fprintln(c.out, "Sub-projetos sugeridos (liste ao usuário, não cadastre sem ele pedir):")
	for i, line := range open {
		if i == max {
			fmt.Fprintf(c.out, "  mais %d na Configuração → Projetos\n", len(open)-max)
			break
		}
		fmt.Fprint(c.out, line)
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

// printMoved avisa o histórico que passou do pai para o filho, se houve.
func (c *cli) printMoved(m client.Moved, name string) {
	if m.Sessions == 0 && m.Cards == 0 {
		return
	}
	sess, cards := "sessões", "cards"
	if m.Sessions == 1 {
		sess = "sessão"
	}
	if m.Cards == 1 {
		cards = "card"
	}
	fmt.Fprintf(c.out, "%d %s e %d %s passaram para %s.\n", m.Sessions, sess, m.Cards, cards, name)
}

// projectPai liga um projeto a um principal (ou desliga, com --nenhum).
func (c *cli) projectPai(u string, args []string) error {
	fs := flag.NewFlagSet("project pai", flag.ContinueOnError)
	nenhum := fs.Bool("nenhum", false, "desliga o projeto do principal")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if (*nenhum && len(pos) != 1) || (!*nenhum && len(pos) != 2) {
		return usagef("%s", u)
	}
	child, err := c.findProjectRef(pos[0])
	if err != nil {
		return err
	}
	if *nenhum {
		if _, err := c.client().SetProjectParent(ctx(), child.ID, nil); err != nil {
			return err
		}
		fmt.Fprintf(c.out, "%s não é mais sub-projeto.\n", child.Name)
		return nil
	}
	parent, err := c.findProjectRef(pos[1])
	if err != nil {
		return err
	}
	// A captura do filho passa a ser a do pai: avisa se elas diferiam.
	differ := child.Capture != parent.Capture
	m, err := c.client().SetProjectParent(ctx(), child.ID, &parent.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s agora é sub-projeto de %s.\n", child.Name, parent.Name)
	c.printMoved(m, parent.Name+" › "+child.Name)
	if differ {
		state := "desligada"
		if parent.Capture {
			state = "ligada"
		}
		fmt.Fprintf(c.out, "A captura de %s passa a seguir a de %s (%s).\n", child.Name, parent.Name, state)
	}
	return nil
}

// findProjectRef acha o projeto por id, por nome (grupo, com ou sem pasta) ou,
// como findProject, pela pasta.
func (c *cli) findProjectRef(arg string) (*domain.Project, error) {
	ps, err := c.client().Projects(ctx())
	if err != nil {
		return nil, err
	}
	if id, perr := strconv.ParseInt(arg, 10, 64); perr == nil {
		for i := range ps {
			if ps[i].ID == id {
				return &ps[i], nil
			}
		}
		return nil, fmt.Errorf("projeto %d não encontrado", id)
	}
	for i := range ps {
		if ps[i].ParentID == nil && ps[i].IsGroup() && strings.EqualFold(ps[i].Name, arg) {
			return &ps[i], nil
		}
	}
	return c.findProject(arg)
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
