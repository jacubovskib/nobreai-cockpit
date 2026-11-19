package memory

import (
	"os"
	"strings"
	"testing"
)

func TestParseNapkin_Standard(t *testing.T) {
	src := `# Napkin

## Execution & Validation (Highest Priority)
1. **[2026-11-08] Rode os testes**
   Do instead: go test ./...

## Shell & Command Reliability
1. **[2026-11-09] zsh expande glob**
   Do instead: use aspas.

## Domain Behavior Guardrails
1. **[2026-11-10] Fim de sessão**
   Do instead: teste os dois caminhos.

## User Directives
1. **[2026-11-11] Projeto em pt-BR**
   Do instead: textos em português.
`
	items, err := ParseNapkin(strings.NewReader(src))
	if err != nil || len(items) != 4 {
		t.Fatalf("err=%v len=%d", err, len(items))
	}
	cats := []Category{"execucao", "shell", "dominio", "diretiva"}
	dates := []string{"2026-11-08", "2026-11-09", "2026-11-10", "2026-11-11"}
	for i, it := range items {
		if it.Category != cats[i] || it.Date != dates[i] || it.NeedsReview || strings.Contains(it.Rule, "*") {
			t.Errorf("item %d: %+v", i, it)
		}
	}
	if items[0].Rule != "Rode os testes" || items[0].Do != "go test ./..." {
		t.Errorf("item 0: %+v", items[0])
	}
}

func TestParseNapkin_Irregular(t *testing.T) {
	f, err := os.Open("testdata/napkin_irregular.md")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	items, err := ParseNapkin(f)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]NapkinItem{}
	for _, it := range items {
		by[it.Rule] = it
	}
	if len(items) != 8 {
		t.Fatalf("len=%d: %+v", len(items), items)
	}
	if it := by["Item sem data"]; it.Date != "" || it.Do != "faça X" || it.Category != "execucao" {
		t.Errorf("sem data: %+v", it)
	}
	if it := by["Item sem negrito"]; it.Date != "2026-01-02" || it.Category != "execucao" {
		t.Errorf("sem negrito: %+v", it)
	}
	if it := by["Item com marcador hífen"]; it.Do != "uma duas três" || it.NeedsReview {
		t.Errorf("hífen: %+v", it)
	}
	if it := by["Sem ação"]; !it.NeedsReview || it.Do != "" || it.Category != "shell" {
		t.Errorf("sem ação: %+v", it)
	}
	if it := by["Seção desconhecida"]; it.Category != "dominio" {
		t.Errorf("desconhecida: %+v", it)
	}
	if it := by["Diretiva em português"]; it.Category != "diretiva" {
		t.Errorf("diretiva: %+v", it)
	}
	if it := by["Domínio em português"]; it.Category != "dominio" {
		t.Errorf("domínio: %+v", it)
	}
	if _, ok := by["Regra de curadoria"]; ok {
		t.Error("Curation Rules deveria ser ignorada")
	}
	var long NapkinItem
	for _, it := range items {
		if len([]rune(it.Rule)) > 300 {
			long = it
		}
	}
	if !long.NeedsReview || strings.Contains(long.Rule, "…") {
		t.Errorf("longa: %+v", long)
	}
}
