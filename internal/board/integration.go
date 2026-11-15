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
// branch ainda não integrado e registra quando o branch entrou na base do
// projeto (local ou origin/<base>). Só o card em revisao vai para concluido; os
// demais ganham apenas o registro. Erros por card não interrompem os outros
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
		if !ok || p.Path == "" {
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
	switch {
	case ok && !in(tip):
		// Tem commits próprios: só grava se mudou (evita escrita e publicação a cada passada).
		if c.BranchHead == tip && c.BranchDiverged {
			return false, nil
		}
		return true, st.SetBranchHead(c.ID, tip, true)
	case ok && c.BranchDiverged:
		merged = tip
	case !ok && c.BranchDiverged && c.BranchHead != "" && in(c.BranchHead):
		merged = c.BranchHead
	default:
		return false, nil
	}
	if err := st.MarkBranchMerged(c.ID, now); err != nil {
		return false, err
	}
	if _, err := st.AddDecision(domain.Decision{
		CardID: c.ID,
		What:   fmt.Sprintf("Branch %s integrada em %s (%s)", c.Branch, base, merged[:min(7, len(merged))]),
		Why:    "detectado pelo Cockpit",
		At:     now,
	}); err != nil {
		return true, err
	}
	if c.Column == domain.Revisao {
		if err := st.MoveCard(c.Key, domain.Concluido); err != nil {
			return true, err
		}
	}
	return true, nil
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
