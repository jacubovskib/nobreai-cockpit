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

// RepoBase é a base do repositório de p: um app de monorepo usa a base do pai,
// pois o git é do projeto do repositório.
func RepoBase(st *store.Store, git gitinfo.BranchReader, p domain.Project) string {
	repo, err := st.RepoProject(p)
	if err != nil {
		repo = p
	}
	return ProjectBase(git, repo)
}

// SyncCardBranch faz o branch do card acompanhar o branch de trabalho da
// sessão. Ignora "", "HEAD" (detached) e a base do projeto, para o card nunca
// ficar na base (sem git, só a base configurada conta). Devolve se o branch do card mudou.
func SyncCardBranch(st *store.Store, git gitinfo.BranchReader, cardID int64, branch string) (bool, error) {
	if branch == "" || branch == "HEAD" {
		return false, nil
	}
	c, err := st.GetCardByID(cardID)
	if err != nil {
		return false, err
	}
	ps, err := st.ListProjects()
	if err != nil {
		return false, err
	}
	for _, p := range ps {
		// Sem git só vale a base configurada (ProjectBase não detecta com git nil).
		if p.ID == c.ProjectID && RepoBase(st, git, p) == branch {
			return false, nil
		}
	}
	return st.SetCardBranch(cardID, branch)
}
