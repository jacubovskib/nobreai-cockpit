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
	var b strings.Builder
	fmt.Fprintf(&b, "# %s · %s\n", c.Key, c.Title)

	meta := []string{}
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
