package board

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// CheckIntegrations percorre os cards em em_andamento, pausado ou revisao com
// branch e registra quando o branch entrou na base do projeto (local ou
// origin/<base>). Só o card em revisao vai para concluido; os demais ganham
// apenas o registro. Branch já integrada que ganha commit novo fora da base
// perde a marca e volta a ser acompanhada. Erros por card não interrompem os outros
// (voltam juntos via errors.Join). changed diz se algo foi gravado.
func CheckIntegrations(st *store.Store, git gitinfo.BranchReader, now time.Time) (changed bool, err error) {
	if git == nil {
		return false, nil
	}
	cards, err := st.ListIntegrationCandidates()
	if err != nil {
		return false, err
	}
	ps, err := st.ListProjects()
	if err != nil {
		return false, err
	}
	projects := map[int64]domain.Project{}
	for _, p := range ps {
		projects[p.ID] = p
	}
	var errs []error
	for _, c := range cards {
		p, ok := projects[c.ProjectID]
		if !ok {
			continue
		}
		// App de monorepo (pai lido do mapa já carregado, sem uma consulta por card): o repositório é o do pai, que cobre os cards da família.
		if p.ParentID != nil {
			if parent, ok := projects[*p.ParentID]; ok && !parent.IsGroup() {
				p = parent
			}
		}
		if p.IsGroup() || p.Path == "" {
			continue
		}
		ch, err := checkCardIntegration(st, git, now, p, c)
		if err != nil {
			errs = append(errs, fmt.Errorf("card %s: %w", c.Key, err))
		}
		changed = changed || ch
	}
	return changed, errors.Join(errs...)
}

func checkCardIntegration(st *store.Store, git gitinfo.BranchReader, now time.Time, p domain.Project, c domain.Card) (bool, error) {
	base := ProjectBase(git, p)
	if base == "" || c.Branch == base {
		return false, nil
	}
	in := func(commit string) bool {
		return git.IsAncestor(p.Path, commit, base) || git.IsAncestor(p.Path, commit, "origin/"+base)
	}
	var merged string // commit com que o branch entrou na base
	tip, ok := git.BranchTip(p.Path, c.Branch)
	if c.BranchMergedAt != nil {
		// Já integrada: só volta a contar se o branch ganhou commit novo fora da base.
		if ok && tip != c.BranchHead && !in(tip) {
			return st.SetBranchHead(c.ID, c.Branch, tip, true)
		}
		return false, nil
	}
	switch {
	case ok && !in(tip):
		// Tem commits próprios: só grava se mudou (evita escrita e publicação a cada passada).
		if c.BranchHead == tip && c.BranchDiverged {
			return false, nil
		}
		return st.SetBranchHead(c.ID, c.Branch, tip, true)
	case ok && c.BranchDiverged && c.BranchHead != "" && in(c.BranchHead):
		// Exige o último tip divergente visto também na base: tip na base com head
		// fora dela é reset/recriação do branch, não merge.
		merged = tip
	case !ok && c.BranchDiverged && c.BranchHead != "" && in(c.BranchHead):
		merged = c.BranchHead
	default:
		return false, nil
	}
	// Marca, decisão e ida para concluido (só se ainda em revisao) numa
	// transação, condicionadas ao branch lido: a leitura pode estar velha.
	recorded, err := st.RecordIntegration(c.ID, c.Branch, now, domain.Decision{
		What: fmt.Sprintf("Branch %s integrada em %s (%s)", c.Branch, base, merged[:min(7, len(merged))]),
		Why:  "detectado pelo Cockpit",
		At:   now,
	})
	if err != nil || !recorded || c.Column != domain.Revisao {
		return recorded, err
	}
	// O card pode ter ido para concluido: gera as memórias candidatas das decisões.
	// Falha aqui não desfaz a integração; volta junto para o log.
	if cur, gerr := st.GetCardByID(c.ID); gerr == nil && cur.Column == domain.Concluido {
		if _, merr := st.CardMemoryCandidates(c.ID); merr != nil {
			return recorded, fmt.Errorf("memórias candidatas: %w", merr)
		}
	}
	return recorded, nil
}

// RunIntegrations chama CheckIntegrations a cada every até ctx terminar
// (bloqueia; rode numa goroutine). onChange é chamado quando algo mudou; erros
// vão para logf (nil = ignora).
func RunIntegrations(ctx context.Context, st *store.Store, git gitinfo.BranchReader, every time.Duration,
	now func() time.Time, onChange func(), logf func(format string, args ...any)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		changed, err := CheckIntegrations(st, git, now())
		if err != nil && logf != nil {
			logf("integração: %v", err)
		}
		if changed && onChange != nil {
			onChange()
		}
	}
}
