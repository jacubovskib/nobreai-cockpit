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
	words   []int32 // palavras da regra representante, ordenadas
	members map[int64][]NapkinItem
}

// internWords troca as palavras por números ordenados: a comparação vira uma
// interseção de listas, sem tabelas hash.
func internWords(ids map[string]int32, s string) []int32 {
	out := []int32{}
	for w := range Words(s) {
		id, ok := ids[w]
		if !ok {
			id = int32(len(ids))
			ids[w] = id
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// similarSets é o Jaccard de duas listas ordenadas, com o corte por tamanho:
// Jaccard >= limiar exige min/max >= limiar.
func similarSets(a, b []int32) bool {
	la, lb := len(a), len(b)
	if la == 0 && lb == 0 {
		return true
	}
	lo, hi := la, lb
	if lo > hi {
		lo, hi = hi, lo
	}
	if float64(lo) < similarImport*float64(hi) {
		return false
	}
	inter, i, j := 0, 0, 0
	for i < la && j < lb {
		switch {
		case a[i] == b[j]:
			inter++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return float64(inter)/float64(la+lb-inter) >= similarImport
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
	wordIDs := map[string]int32{}
	for _, id := range ids {
		for _, it := range perProject[id] {
			var hit *cluster
			words := internWords(wordIDs, it.Rule)
			if len(words) > 0 { // regra sem palavras nunca se junta a outra
				for _, c := range clusters {
					if len(c.words) > 0 && similarSets(c.words, words) {
						hit = c
						break
					}
				}
			}
			if hit == nil {
				hit = &cluster{rep: it, words: words, members: map[int64][]NapkinItem{}}
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
