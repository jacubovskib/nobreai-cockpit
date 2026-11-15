package board

import (
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// ProjectBase devolve o branch base do projeto: o configurado ou, se vazio, o
// detectado pelo git ("" se não houver git ou não achar).
func ProjectBase(git gitinfo.BranchReader, p domain.Project) string {
	if p.BaseBranch != "" || git == nil {
		return p.BaseBranch
	}
	return git.DetectBase(p.Path)
}

// SyncCardBranch faz o branch do card acompanhar o branch de trabalho da
// sessão. Ignora "", "HEAD" (detached) e a base do projeto, para o card nunca
// ficar na base. Devolve se o branch do card mudou.
func SyncCardBranch(st *store.Store, git gitinfo.BranchReader, cardID int64, branch string) (bool, error) {
	if branch == "" || branch == "HEAD" {
		return false, nil
	}
	if git != nil {
		c, err := st.GetCardByID(cardID)
		if err != nil {
			return false, err
		}
		ps, err := st.ListProjects()
		if err != nil {
			return false, err
		}
		for _, p := range ps {
			if p.ID == c.ProjectID && ProjectBase(git, p) == branch {
				return false, nil
			}
		}
	}
	return st.SetCardBranch(cardID, branch)
}
