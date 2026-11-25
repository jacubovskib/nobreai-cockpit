// Package resume monta o pacote de retomada de um card.
package resume

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/prompts"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Build monta o pacote de retomada em markdown. Seções vazias são omitidas,
// exceto "Onde parei".
func Build(c domain.Card, ct domain.Content, sessions []domain.Session, totals []store.Total, diffStat string) string {
	return BuildIn(c, "", ct, sessions, totals, diffStat)
}

// BuildIn é o Build com o nome exibido do projeto ("loja › web"), que no
// cabeçalho substitui o nome da pasta quando não é vazio.
func BuildIn(c domain.Card, project string, ct domain.Content, sessions []domain.Session, totals []store.Total, diffStat string) string {
	return BuildWith(c, project, ct, sessions, totals, diffStat, Family{})
}

// maxChildrenListed é quantas filhas do épico o pacote lista.
const maxChildrenListed = 10

// Family é a família do card: o épico pai (se houver) e as filhas (se o card
// for um épico).
type Family struct {
	Epic     *domain.Card
	Children []domain.Card
}

// BuildWith é o BuildIn com a família do card: o tipo e a prioridade na meta
// (só quando fogem do padrão), o épico pai logo após o cabeçalho e as filhas.
func BuildWith(c domain.Card, project string, ct domain.Content, sessions []domain.Session, totals []store.Total, diffStat string, fam Family) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n", c.Key, c.Title)
	if e := fam.Epic; e != nil {
		line := fmt.Sprintf("Épico: %s · %s", e.Key, e.Title)
		if g := firstLine(e.Goal); g != "" {
			line += " — " + g
		}
		b.WriteString(line + "\n")
	}

	meta := []string{}
	if c.Type != "" && c.Type != domain.TypeTarefa {
		meta = append(meta, domain.TypeLabel(c.Type))
	}
	if c.Priority != "" && c.Priority != domain.PrioMedia {
		meta = append(meta, "prioridade "+domain.PriorityLabel(c.Priority))
	}
	if project != "" {
		meta = append(meta, project)
	} else if repo := repoName(sessions); repo != "" {
		meta = append(meta, repo)
	}
	if c.Branch != "" {
		meta = append(meta, c.Branch)
	}
	meta = append(meta, fmt.Sprintf("%d sessões", len(sessions)))
	if cost, ok := totalCost(totals); ok {
		meta = append(meta, "US$ "+strings.Replace(fmt.Sprintf("%.2f", cost), ".", ",", 1))
	}
	b.WriteString(strings.Join(meta, " · ") + "\n")

	section := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		fmt.Fprintf(&b, "\n## %s\n%s\n", title, strings.TrimRight(body, "\n"))
	}

	where := strings.TrimSpace(c.WhereStopped)
	if where == "" {
		where = "(não registrado)"
	}
	section("Onde parei", where)

	var goal []string
	if g := strings.TrimSpace(c.Goal); g != "" {
		goal = append(goal, g)
	}
	if d := strings.TrimSpace(c.DoneWhen); d != "" {
		goal = append(goal, "Pronto quando: "+d)
	}
	section("Objetivo", strings.Join(goal, "\n"))

	var kids []string
	for i, k := range fam.Children {
		if i == maxChildrenListed {
			kids = append(kids, fmt.Sprintf("… e mais %d", len(fam.Children)-maxChildrenListed))
			break
		}
		kids = append(kids, fmt.Sprintf("- %s · %s · %s · %s", k.Key, domain.TypeLabel(k.Type), k.Title, domain.ColumnLabel(k.Column)))
	}
	section("Filhas", strings.Join(kids, "\n"))

	var open, done []string
	for _, t := range ct.Todos {
		if t.Done {
			done = append(done, "- [x] "+t.Text)
		} else {
			open = append(open, "- [ ] "+t.Text)
		}
	}
	section("Falta", strings.Join(open, "\n"))
	section("Feito", strings.Join(done, "\n"))
	section("Plano", PlanSection(ct.Plan))

	var dec []string
	for _, d := range ct.Decisions {
		dec = append(dec, fmt.Sprintf("- %s — porquê: %s", d.What, d.Why))
	}
	section("Decisões", strings.Join(dec, "\n"))

	var files []string
	for _, f := range ct.Files {
		p := f.Path
		if f.Lines != "" {
			p += ":" + f.Lines
		}
		files = append(files, fmt.Sprintf("- %s: %s", p, f.Why))
	}
	section("Arquivos essenciais", strings.Join(files, "\n"))

	var qs []string
	for _, q := range ct.Questions {
		if q.Answer == "" {
			qs = append(qs, fmt.Sprintf("- [%d] %s", q.ID, q.Text))
		}
	}
	section("Perguntas em aberto", strings.Join(qs, "\n"))

	section("Como testar", c.HowToTest)

	if strings.TrimSpace(diffStat) == "" {
		diffStat = "sem mudanças registradas"
	}
	section("Desde a última sessão", diffStat)
	return b.String()
}

// firstLine é a primeira linha não vazia do texto.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// repoName usa o diretório da sessão mais recente (maior StartedAt) como nome
// do repositório.
func repoName(sessions []domain.Session) string {
	var best *domain.Session
	for i := range sessions {
		if sessions[i].Cwd == "" {
			continue
		}
		if best == nil || sessions[i].StartedAt.After(best.StartedAt) {
			best = &sessions[i]
		}
	}
	if best == nil {
		return ""
	}
	return filepath.Base(best.Cwd)
}

// totalCost soma o custo; ok=false quando não há totais ou algum está sem preço.
func totalCost(totals []store.Total) (float64, bool) {
	if len(totals) == 0 {
		return 0, false
	}
	var sum float64
	for _, t := range totals {
		if t.Unpriced {
			return 0, false
		}
		sum += t.CostUSD
	}
	return sum, true
}

// Vars monta as variáveis de prompt para o card.
func Vars(c domain.Card, ct domain.Content, pkg string, readiness domain.ReadinessResult) prompts.Vars {
	var paths, missing []string
	for _, f := range ct.Files {
		paths = append(paths, f.Path)
	}
	for _, it := range readiness.Items {
		if !it.OK {
			missing = append(missing, it.Label)
		}
	}
	return prompts.Vars{
		"card.id":          c.Key,
		"card.titulo":      c.Title,
		"card.onde_parei":  c.WhereStopped,
		"card.pacote":      pkg,
		"card.arquivos":    strings.Join(paths, ", "),
		"card.como_testar": c.HowToTest,
		"git.branch":       c.Branch,
		"handoff.faltando": strings.Join(missing, "; "),
	}
}

// PlanSection resume o plano: arquivo e progresso, depois as tarefas em aberto.
func PlanSection(p *domain.Plan) string {
	if p == nil || len(p.Tasks) == 0 {
		return ""
	}
	var lines []string
	lines = append(lines, fmt.Sprintf("%s · %d de %d concluídas", p.Path, len(p.Tasks)-p.Open(), len(p.Tasks)))
	for _, t := range p.Tasks {
		if t.Status != domain.TaskDone {
			lines = append(lines, fmt.Sprintf("- %d. %s — %s", t.N, t.Title, t.Status.Label()))
		}
	}
	return strings.Join(lines, "\n")
}
