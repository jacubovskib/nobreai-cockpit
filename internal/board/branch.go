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
// Erro de leitura do pai volta ao chamador: usar a base do app seria errado.
func RepoBase(st *store.Store, git gitinfo.BranchReader, p domain.Project) (string, error) {
	repo, err := st.RepoProject(p)
	if err != nil {
		return "", err
	}
	return ProjectBase(git, repo), nil
}

// SyncCardBranch faz o branch do card acompanhar o branch de trabalho da
// sessão. Ignora "", "HEAD" (detached) e a base do projeto, para o card nunca
// ficar na base (sem git, só a base configurada conta). Épico nunca guarda branch. Devolve se o branch do card mudou.
func SyncCardBranch(st *store.Store, git gitinfo.BranchReader, cardID int64, branch string) (bool, error) {
	if branch == "" || branch == "HEAD" {
		return false, nil
	}
	c, err := st.GetCardByID(cardID)
	if err != nil {
		return false, err
	}
	if c.Type == domain.TypeEpico {
		return false, nil // o épico nunca guarda branch
	}
	ps, err := st.ListProjects()
	if err != nil {
		return false, err
	}
	for _, p := range ps {
		// Sem git só vale a base configurada (ProjectBase não detecta com git nil).
		if p.ID != c.ProjectID {
			continue
		}
		if p.IsGroup() {
			return false, nil // grupo não tem branch
		}
		base, err := RepoBase(st, git, p)
		if err != nil {
			return false, err
		}
		if base == branch {
			return false, nil
		}
	}
	return st.SetCardBranch(cardID, branch)
}
