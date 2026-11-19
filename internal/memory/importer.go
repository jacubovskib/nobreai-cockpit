package memory

import "sort"

// similarImport é o limiar de parecença (Jaccard) para juntar regras de projetos diferentes.
const similarImport = 0.6

// Grouped é uma regra repetida em 2+ projetos: vira uma candidata global.
type Grouped struct {
	Item       NapkinItem // a primeira ocorrência (menor id de projeto)
	ProjectIDs []int64    // projetos onde apareceu, em ordem crescente
}

type cluster struct {
	rep     NapkinItem
	members map[int64][]NapkinItem
}

// GroupImport separa os itens lidos dos napkin.md: regra igual ou parecida
// (Similarity >= 0,6) em 2+ projetos vira global; o resto fica no projeto.
// É puro e determinístico (projetos em ordem de id).
func GroupImport(perProject map[int64][]NapkinItem) (global []Grouped, local map[int64][]NapkinItem) {
	local = map[int64][]NapkinItem{}
	ids := make([]int64, 0, len(perProject))
	for id := range perProject {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var clusters []*cluster
	for _, id := range ids {
		for _, it := range perProject[id] {
			var hit *cluster
			if Normalize(it.Rule) != "" {
				for _, c := range clusters {
					if Similarity(c.rep.Rule, it.Rule) >= similarImport {
						hit = c
						break
					}
				}
			}
			if hit == nil {
				hit = &cluster{rep: it, members: map[int64][]NapkinItem{}}
				clusters = append(clusters, hit)
			}
			hit.members[id] = append(hit.members[id], it)
		}
	}
	for _, c := range clusters {
		if len(c.members) >= 2 {
			g := Grouped{Item: c.rep}
			for id := range c.members {
				g.ProjectIDs = append(g.ProjectIDs, id)
			}
			sort.Slice(g.ProjectIDs, func(i, j int) bool { return g.ProjectIDs[i] < g.ProjectIDs[j] })
			global = append(global, g)
			continue
		}
		for id, its := range c.members {
			local[id] = append(local[id], its...)
		}
	}
	return global, local
}
