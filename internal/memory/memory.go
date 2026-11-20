// Package memory reúne as regras puras das memórias do agente: normalização,
// similaridade, limpeza de texto, seleção com teto de tokens e formatação do
// bloco injetado na sessão. Não acessa banco nem rede.
package memory

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Category classifica a memória.
type Category string

const (
	CategoryExecucao Category = "execucao"
	CategoryShell    Category = "shell"
	CategoryDominio  Category = "dominio"
	CategoryDiretiva Category = "diretiva"
)

// ValidCategory diz se c é uma categoria conhecida.
func ValidCategory(c string) bool {
	switch Category(c) {
	case CategoryExecucao, CategoryShell, CategoryDominio, CategoryDiretiva:
		return true
	}
	return false
}

var accents = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

// Normalize põe em minúsculas, tira acentos e pontuação e deixa espaços simples.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if a, ok := accents[r]; ok {
			r = a
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Similarity é o Jaccard das palavras normalizadas (1 = iguais, 0 = nada em comum).
func Similarity(a, b string) float64 {
	return jaccard(wordSet(a), wordSet(b))
}

func jaccard(wa, wb map[string]bool) float64 {
	if len(wa) == 0 && len(wb) == 0 {
		return 1
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// Words devolve as palavras significativas (normalizadas, sem palavras vazias)
// usadas na similaridade; quem compara muitas vezes calcula uma vez só.
func Words(s string) map[string]bool { return wordSet(s) }

// stopwords são palavras vazias do português ignoradas na similaridade; sem
// isso "antes do commit" x "antes de commitar" ficaria abaixo do limiar 0,6.
var stopwords = map[string]bool{
	"a": true, "o": true, "as": true, "os": true, "um": true, "uma": true,
	"de": true, "do": true, "da": true, "dos": true, "das": true,
	"em": true, "no": true, "na": true, "nos": true, "nas": true,
	"e": true, "ou": true, "para": true, "por": true, "com": true, "que": true,
}

func wordSet(s string) map[string]bool {
	all := strings.Fields(Normalize(s))
	m := map[string]bool{}
	for _, w := range all {
		if !stopwords[w] {
			m[w] = true
		}
	}
	if len(m) == 0 { // texto só de palavras vazias: usa todas
		for _, w := range all {
			m[w] = true
		}
	}
	return m
}

// CleanText troca quebras por espaço, apara e corta em max runas com "…".
// cut é true quando houve corte.
func CleanText(s string, max int) (out string, cut bool) {
	out = strings.Join(strings.Fields(s), " ")
	if max > 0 && utf8.RuneCountInString(out) > max {
		r := []rune(out)
		return string(r[:max-1]) + "…", true
	}
	return out, false
}

// Item é uma memória candidata à injeção.
type Item struct {
	ID       int64
	Scope    int // 0 = projeto efetivo (app), 1 = raiz, 2 = global
	Position int
	Category Category
	Rule, Do string
}

func (it Item) line() string {
	l := "- [" + string(it.Category) + "] " + it.Rule
	if it.Do != "" {
		l += " → " + it.Do
	}
	return l
}

// tokens estima ceil(bytes/4) da linha formatada.
func (it Item) tokens() int { return (len(it.line()) + 3) / 4 }

// ordered põe os itens na ordem de injeção (Scope, Position, ID) e tira as
// regras repetidas em mais de um escopo: fica só a do escopo mais específico
// (repetidas no mesmo escopo continuam, como a página as mostra).
func ordered(items []Item) []Item {
	sorted := append([]Item(nil), items...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return a.ID < b.ID
	})
	scopeOf := map[string]int{} // escopo da primeira ocorrência de cada regra
	out := sorted[:0]
	for _, it := range sorted {
		n := Normalize(it.Rule)
		if s, ok := scopeOf[n]; ok && s < it.Scope {
			continue
		}
		scopeOf[n] = it.Scope
		out = append(out, it)
	}
	return out
}

// pick escolhe na ordem dada o que cabe no teto de tokens e no de runas (cada
// linha conta com a quebra; usedRunes já traz o que está fora das linhas).
func pick(sorted []Item, budgetTokens, maxRunes, usedRunes int) (chosen []Item, left int) {
	used := 0
	for _, it := range sorted {
		t, r := it.tokens(), utf8.RuneCountInString(it.line())+1
		if used+t <= budgetTokens && usedRunes+r <= maxRunes {
			used += t
			usedRunes += r
			chosen = append(chosen, it)
		} else {
			left++
		}
	}
	return chosen, left
}

// Select escolhe, na ordem (Scope, Position, ID), as memórias que cabem no teto;
// a que não cabe é pulada. A mesma regra em mais de um escopo entra uma vez só,
// pela mais específica (a repetida não conta como fora do limite). Devolve as
// escolhidas e quantas ficaram de fora.
func Select(items []Item, budgetTokens int) (chosen []Item, left int) {
	return pick(ordered(items), budgetTokens, math.MaxInt, 0)
}

// BlockWithin monta o bloco como Select e FormatBlock, mas com no máximo
// maxRunes caracteres no total (cabeçalho, linhas, quebras e a linha "+N").
// Sem espaço para nenhuma memória, sai só o cabeçalho com a linha "+N", se
// couber; senão, "".
func BlockWithin(items []Item, budgetTokens, maxRunes int) string {
	sorted := ordered(items)
	if len(sorted) == 0 {
		return ""
	}
	head := utf8.RuneCountInString(blockHeader)
	chosen, left := pick(sorted, budgetTokens, maxRunes, head)
	if left > 0 {
		// Reserva a linha "+N" no pior caso (N = todas) e escolhe de novo.
		reserve := utf8.RuneCountInString(leftLine(len(sorted)))
		chosen, left = pick(sorted, budgetTokens, maxRunes, head+reserve)
	}
	blk := FormatBlock(chosen, left)
	if utf8.RuneCountInString(blk) > maxRunes {
		return ""
	}
	return blk
}

const blockHeader = "Memórias deste projeto (do Cockpit; use nobre lembrar para gravar e nobre memorias <termo> para buscar; não leia nem edite .claude/napkin.md):\n"

// FormatBlock monta o bloco injetado; vazio quando não há nada a mostrar.
func FormatBlock(chosen []Item, left int) string {
	if len(chosen) == 0 && left <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(blockHeader)
	for _, it := range chosen {
		b.WriteString(it.line())
		b.WriteByte('\n')
	}
	if left > 0 {
		b.WriteString(leftLine(left))
	}
	return b.String()
}

// leftLine é a linha das memórias que ficaram fora do limite.
func leftLine(left int) string {
	return "+" + strconv.Itoa(left) + " memórias fora do limite: nobre memorias <termo>\n"
}

// UsedTokens soma os tokens estimados das linhas escolhidas.
func UsedTokens(chosen []Item) int {
	n := 0
	for _, it := range chosen {
		n += it.tokens()
	}
	return n
}
