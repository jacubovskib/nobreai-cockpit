package memory

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	if got := Normalize("Use  ‘aspas’ no Zsh!"); got != "use aspas no zsh" {
		t.Errorf("got %q", got)
	}
	if got := Normalize("Execução"); got != "execucao" {
		t.Errorf("got %q", got)
	}
}

func TestSimilarity(t *testing.T) {
	if s := Similarity("rode go test", "Rode go test!"); s != 1 {
		t.Errorf("idênticas: %v", s)
	}
	if s := Similarity("abc def", "xyz uvw"); s != 0 {
		t.Errorf("disjuntas: %v", s)
	}
	if s := Similarity("rode go test antes do commit", "rode go test antes de commitar"); s < 0.6 {
		t.Errorf("parecidas: %v", s)
	}
}

func TestCleanText(t *testing.T) {
	out, cut := CleanText("a\nb\r\n  c ", 300)
	if out != "a b c" || cut {
		t.Errorf("got %q %v", out, cut)
	}
	out, cut = CleanText(strings.Repeat("é", 301), 300)
	if !cut || len([]rune(out)) != 300 || !strings.HasSuffix(out, "…") {
		t.Errorf("cut=%v len=%d", cut, len([]rune(out)))
	}
}

func TestSelect_Order(t *testing.T) {
	items := []Item{
		{ID: 1, Scope: 2, Position: 0, Rule: "g"},
		{ID: 2, Scope: 0, Position: 5, Rule: "a2"},
		{ID: 3, Scope: 1, Position: 0, Rule: "r"},
		{ID: 4, Scope: 0, Position: 1, Rule: "a1"},
	}
	got, left := Select(items, 1000)
	if left != 0 || len(got) != 4 {
		t.Fatalf("left=%d len=%d", left, len(got))
	}
	want := []int64{4, 2, 3, 1}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("pos %d: id %d, quer %d", i, got[i].ID, id)
		}
	}
}

func TestSelect_SkipsLargeKeepsSmall(t *testing.T) {
	items := []Item{
		{ID: 1, Category: "shell", Rule: strings.Repeat("x", 400), Do: "y"},
		{ID: 2, Category: "shell", Rule: "curta", Do: "a"},
		{ID: 3, Category: "shell", Rule: "outra", Do: "b"},
	}
	got, left := Select(items, 20)
	if len(got) != 2 || left != 1 || got[0].ID != 2 || got[1].ID != 3 {
		t.Errorf("got %+v left %d", got, left)
	}
}

func TestSelect_ZeroBudget(t *testing.T) {
	items := []Item{{ID: 1, Rule: "a"}, {ID: 2, Rule: "b"}}
	got, left := Select(items, 0)
	if len(got) != 0 || left != 2 {
		t.Errorf("got %d left %d", len(got), left)
	}
}

func TestFormatBlock(t *testing.T) {
	if FormatBlock(nil, 0) != "" {
		t.Error("esperava vazio")
	}
	chosen := []Item{{Category: "shell", Rule: "regra", Do: "ação"}}
	head := "Memórias deste projeto (do Cockpit; use nobre lembrar para gravar e nobre memorias <termo> para buscar; não leia nem edite .claude/napkin.md):\n"
	want := head + "- [shell] regra → ação\n"
	if got := FormatBlock(chosen, 0); got != want {
		t.Errorf("sem +N:\n%q", got)
	}
	want2 := want + "+3 memórias fora do limite: nobre memorias <termo>\n"
	if got := FormatBlock(chosen, 3); got != want2 {
		t.Errorf("com +N:\n%q", got)
	}
	if got := FormatBlock(nil, 2); !strings.Contains(got, "+2 memórias") {
		t.Errorf("só +N: %q", got)
	}
}
