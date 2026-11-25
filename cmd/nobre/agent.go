package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/client"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/resume"
)

// fieldFlags são as flags de tipo, épico, prioridade, labels e points.
type fieldFlags struct {
	typ, epic, priority *string
	noEpic, noPoints    *bool
	labels              *listFlag
	points              *float64
}

func addFieldFlags(fs *flag.FlagSet) fieldFlags {
	f := fieldFlags{labels: &listFlag{}}
	f.typ = fs.String("type", "", "tipo: epico, historia, tarefa ou bug")
	f.epic = fs.String("epic", "", "chave do épico pai")
	f.noEpic = fs.Bool("no-epic", false, "tira o card do épico")
	f.priority = fs.String("priority", "", "prioridade: mais_alta, alta, media, baixa ou mais_baixa")
	fs.Var(f.labels, "label", "label (repetível)")
	f.points = fs.Float64("points", 0, "estimativa em pontos")
	f.noPoints = fs.Bool("no-points", false, "tira a estimativa")
	return f
}

// fields monta o CardFields a partir das flags dadas em fs.
func (f fieldFlags) fields(fs *flag.FlagSet) (client.CardFields, error) {
	set := map[string]bool{}
	fs.Visit(func(x *flag.Flag) { set[x.Name] = true })
	if set["epic"] && *f.noEpic {
		return client.CardFields{}, usagef("%s: --epic e --no-epic não combinam", fs.Name())
	}
	if set["points"] && *f.noPoints {
		return client.CardFields{}, usagef("%s: --points e --no-points não combinam", fs.Name())
	}
	cf := client.CardFields{Type: *f.typ, Priority: *f.priority, Labels: []string(*f.labels), ClearPoints: *f.noPoints}
	switch {
	case *f.noEpic:
		empty := ""
		cf.ParentKey = &empty
	case set["epic"]:
		k := *f.epic
		cf.ParentKey = &k
	}
	if set["points"] {
		p := *f.points
		cf.Points = &p
	}
	return cf, nil
}

func (c *cli) start(args []string) error {
	const u = `uso: nobre start "<título>" [--goal ...] [--done-when ...] [--type epico|historia|tarefa|bug] [--epic <KEY>] [--priority ...] [--label ...] [--points n]`
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	goal := fs.String("goal", "", "objetivo do card")
	done := fs.String("done-when", "", "critério de pronto")
	ff := addFieldFlags(fs)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	title := joinText(pos)
	if title == "" {
		return usagef(u)
	}
	cf, err := ff.fields(fs)
	if err != nil {
		return err
	}
	card, err := c.client().Start(ctx(), title, *goal, *done, cf)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Card %s criado e ligado a esta sessão.\n", card.Key)
	return nil
}

func (c *cli) card(args []string) error {
	const u = `uso: nobre card add "<título>" [--goal ...] [--done-when ...] [--branch ...] [--where "<onde parei>"] [--column backlog|pausado] [--type ...] [--epic <KEY>] [--priority ...] [--label ...] [--points n]
     nobre card set [<KEY>] [--type ...] [--epic <KEY> | --no-epic] [--priority ...] [--label ...] [--points n | --no-points]`
	if len(args) > 0 && args[0] == "set" {
		return c.cardSet(args[1:], u)
	}
	if len(args) == 0 || args[0] != "add" {
		return usagef(u)
	}
	fs := flag.NewFlagSet("card add", flag.ContinueOnError)
	goal := fs.String("goal", "", "objetivo do card")
	done := fs.String("done-when", "", "critério de pronto")
	branch := fs.String("branch", "", "branch do card")
	where := fs.String("where", "", `"Onde parei" do card`)
	column := fs.String("column", "", "backlog (padrão) ou pausado")
	ff := addFieldFlags(fs)
	pos, err := c.parse(fs, args[1:])
	if err != nil {
		return err
	}
	title := joinText(pos)
	if title == "" {
		return usagef(u)
	}
	cf, err := ff.fields(fs)
	if err != nil {
		return err
	}
	card, err := c.client().AddCard(ctx(), map[string]string{"title": title, "goal": *goal, "done_when": *done,
		"branch": *branch, "where_stopped": *where, "column": *column}, cf)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Card %s criado no %s (sessão não ligada).\n", card.Key, card.Column)
	return nil
}

// cardSet muda tipo, épico, prioridade, labels e points do card (o da sessão sem KEY).
func (c *cli) cardSet(args []string, u string) error {
	fs := flag.NewFlagSet("card set", flag.ContinueOnError)
	ff := addFieldFlags(fs)
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 1 {
		return usagef("%s", u)
	}
	cf, err := ff.fields(fs)
	if err != nil {
		return err
	}
	if cf.Type == "" && cf.Priority == "" && cf.ParentKey == nil && len(cf.Labels) == 0 && cf.Points == nil && !cf.ClearPoints {
		return usagef("%s", u)
	}
	key := ""
	if len(pos) == 1 {
		key = pos[0]
	}
	card, err := c.client().CardSet(ctx(), key, cf)
	if err != nil {
		return err
	}
	parts := []string{domain.TypeLabel(card.Type)}
	if card.Priority != domain.PrioMedia {
		parts = append(parts, domain.PriorityLabel(card.Priority))
	}
	if card.ParentKey != "" {
		parts = append(parts, "épico "+card.ParentKey)
	}
	fmt.Fprintf(c.out, "Card %s atualizado: %s.\n", card.Key, strings.Join(parts, " · "))
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
	if k.ProjectName != "" {
		fmt.Fprintf(c.out, "Projeto: %s\n", k.ProjectName)
	}
	fmt.Fprintf(c.out, "Onde parei: %s\n", where)
	fmt.Fprintln(c.out, statusKind(k))
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
	if plan := resume.PlanSection(k.Content.Plan); plan != "" {
		fmt.Fprintf(c.out, "Plano:\n%s\n", plan)
	}
	return nil
}

// statusKind é a linha "Tipo: ..." do nobre status.
func statusKind(k *client.StatusCard) string {
	parts := []string{"Tipo: " + domain.TypeLabel(k.Type)}
	if k.Priority != domain.PrioMedia && k.Priority != "" {
		parts = append(parts, "Prioridade: "+domain.PriorityLabel(k.Priority))
	}
	if k.ParentKey != "" {
		e := "Épico: " + k.ParentKey
		if k.ParentTitle != "" {
			e += " · " + k.ParentTitle
		}
		parts = append(parts, e)
	}
	if k.Children != nil {
		parts = append(parts, fmt.Sprintf("Filhas: %d/%d concluídas", k.Children.Done, k.Children.Total))
	}
	return strings.Join(parts, " · ")
}

func (c *cli) app(args []string) error {
	const u = `uso: nobre app <nome> | nobre app --geral`
	fs := flag.NewFlagSet("app", flag.ContinueOnError)
	geral := fs.Bool("geral", false, "devolve o card ao projeto principal")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if (*geral && len(pos) != 0) || (!*geral && len(pos) != 1) {
		return usagef(u)
	}
	name := ""
	if !*geral {
		name = pos[0]
	}
	cl := c.client()
	card, err := cl.SetApp(ctx(), name, *geral)
	if err != nil {
		return err
	}
	label := fmt.Sprintf("o projeto %d", card.ProjectID)
	if ps, err := cl.Projects(ctx()); err == nil {
		byID := map[int64]domain.Project{}
		for _, p := range ps {
			byID[p.ID] = p
		}
		if p, ok := byID[card.ProjectID]; ok {
			label = p.Name
			if par, ok := byID[derefID(p.ParentID)]; ok {
				label = par.Name + " › " + p.Name
			}
		}
	}
	fmt.Fprintf(c.out, "Card %s agora em %s.\n", card.Key, label)
	return nil
}

func derefID(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (c *cli) lembrar(args []string) error {
	const u = `uso: nobre lembrar "<regra>" --faca "<ação>" [--cat execucao|shell|dominio|diretiva] [--escopo projeto|raiz|global]`
	fs := flag.NewFlagSet("lembrar", flag.ContinueOnError)
	do := fs.String("faca", "", "o que fazer em vez disso (obrigatório)")
	cat := fs.String("cat", "dominio", "categoria: execucao, shell, dominio ou diretiva")
	scope := fs.String("escopo", "projeto", "escopo: projeto, raiz ou global")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	rule := joinText(pos)
	if rule == "" || strings.TrimSpace(*do) == "" {
		return usagef(u)
	}
	r, err := c.client().Remember(ctx(), rule, *do, *cat, *scope)
	if err != nil {
		return err
	}
	if r.Status == "candidata" {
		// Global do agente: só vale depois que o usuário aprovar.
		fmt.Fprintf(c.out, "Memória global #%d enviada para aprovação na página Memórias.\n", r.ID)
	} else {
		fmt.Fprintf(c.out, "Memória #%d gravada (%s).\n", r.ID, r.Scope)
	}
	for _, s := range r.Similar {
		fmt.Fprintf(c.out, "Parecida com #%d: %s\n", s.ID, s.Rule)
	}
	return nil
}

func (c *cli) memorias(args []string) error {
	pos, err := c.parse(flag.NewFlagSet("memorias", flag.ContinueOnError), args)
	if err != nil {
		return err
	}
	list, err := c.client().Memories(ctx(), joinText(pos))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(c.out, "Nenhuma memória.")
		return nil
	}
	for _, m := range list {
		line := fmt.Sprintf("#%d [%s] (%s) %s", m.ID, m.Category, m.Scope, m.Rule)
		if m.Do != "" {
			line += " → " + m.Do
		}
		fmt.Fprintln(c.out, line)
	}
	return nil
}

// listFlag é uma flag repetível: cada --task ou --label soma um item.
type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func (c *cli) plan(args []string) error {
	const u = `uso: nobre plan <arquivo> --task "<título>" [--task ...]`
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	var tasks listFlag
	fs.Var(&tasks, "task", "título de uma tarefa do plano (repita para cada tarefa, em ordem)")
	pos, err := c.parse(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || len(tasks) == 0 {
		return usagef(u)
	}
	body := map[string]any{"path": pos[0], "tasks": []string(tasks)}
	var plan domain.Plan
	if err := c.client().Do(ctx(), "POST", "/api/me/plan", body, &plan); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Plano registrado: %d tarefas.\n", len(plan.Tasks))
	return nil
}

func (c *cli) task(args []string) error {
	const u = "uso: nobre task <n> a-fazer|andamento|revisao|concluida"
	if len(args) != 2 {
		return usagef(u)
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 {
		return usagef(u)
	}
	var t domain.PlanTask
	if err := c.client().Do(ctx(), "POST", "/api/me/task", map[string]any{"n": n, "status": args[1]}, &t); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "Tarefa %d: %s.\n", t.N, t.Status.Label())
	return nil
}
