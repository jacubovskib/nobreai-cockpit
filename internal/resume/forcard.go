package resume

import (
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// ForCard monta o pacote de retomada do card a partir do banco. "Desde a
// última sessão" é o diff no diretório do projeto desde o git_head da sessão
// mais recente do card que não seja exclude (a que acabou de entrar no card,
// cujo HEAD é o atual; "" não exclui nenhuma). git nil = sem diff.
func ForCard(st *store.Store, git gitinfo.GitInfo, c domain.Card, exclude string) (string, error) {
	ct, err := st.CardContent(c.ID)
	if err != nil {
		return "", err
	}
	sessions, err := st.SessionsForCard(c.ID)
	if err != nil {
		return "", err
	}
	// Uso de cada sessão só naquele card.
	totals, err := st.Totals(store.TotalsFilter{GroupBy: "session", CardID: &c.ID})
	if err != nil {
		return "", err
	}
	projects, err := st.ListProjects()
	if err != nil {
		return "", err
	}
	label := ""
	for _, p := range projects {
		if p.ID != c.ProjectID || p.ParentID == nil {
			continue
		}
		for i := range projects {
			if projects[i].ID == *p.ParentID {
				label = store.DisplayName(p, &projects[i])
			}
		}
	}
	diff := ""
	if git != nil {
		since := ""
		for _, x := range sessions { // ordenadas por início: fica a mais recente
			if x.ID != exclude {
				since = x.GitHead
			}
		}
		for _, p := range projects {
			if p.ID == c.ProjectID && !p.IsGroup() && p.Path != "" {
				diff = git.DiffStat(p.Path, since)
			}
		}
	}
	var fam Family
	if c.ParentID != nil {
		if e, err := st.GetCardByID(*c.ParentID); err == nil {
			fam.Epic = &e
		}
	}
	if c.Type == domain.TypeEpico {
		if fam.Children, err = st.ListChildren(c.ID); err != nil {
			return "", err
		}
	}
	return BuildWith(c, label, ct, sessions, totals, diff, fam), nil
}
