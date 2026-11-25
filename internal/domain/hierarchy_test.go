package domain

import (
	"fmt"
	"strings"
	"testing"
)

func TestEpicColumn(t *testing.T) {
	cases := []struct {
		name     string
		cur      Column
		children []Column
		want     Column
	}{
		{"sem filhas em backlog", Backlog, nil, Backlog},
		{"sem filhas em andamento volta ao backlog", EmAndamento, nil, Backlog},
		{"sem filhas concluído fica", Concluido, nil, Concluido},
		{"todas backlog", Revisao, []Column{Backlog, Backlog}, Backlog},
		{"uma em andamento", Backlog, []Column{EmAndamento, Backlog}, EmAndamento},
		{"filha pausada", Backlog, []Column{Pausado}, EmAndamento},
		{"todas concluídas vai a revisão", EmAndamento, []Column{Concluido, Concluido}, Revisao},
		{"concluído com filha concluída fica", Concluido, []Column{Concluido}, Concluido},
		{"concluído com filha reaberta", Concluido, []Column{Concluido, Backlog}, EmAndamento},
		{"concluído com só filha em backlog", Concluido, []Column{Backlog}, Backlog},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EpicColumn(c.cur, c.children); got != c.want {
				t.Fatalf("EpicColumn(%s, %v) = %s, quer %s", c.cur, c.children, got, c.want)
			}
		})
	}
}

func TestEpicManualMove(t *testing.T) {
	cases := []struct {
		name     string
		cur, to  Column
		children []Column
		want     Column
		wantErr  bool
	}{
		{"revisão para concluído", Revisao, Concluido, []Column{Concluido}, Concluido, false},
		{"sem filhas para concluído", Backlog, Concluido, nil, Concluido, false},
		{"concluir com filha aberta", EmAndamento, Concluido, []Column{Concluido, EmAndamento}, "", true},
		{"reabrir recalcula", Concluido, Backlog, []Column{Concluido}, Revisao, false},
		{"reabrir sem filhas", Concluido, EmAndamento, nil, Backlog, false},
		{"pausar é erro", Backlog, Pausado, nil, "", true},
		{"em andamento manual é erro", Backlog, EmAndamento, []Column{EmAndamento}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := EpicManualMove(c.cur, c.to, c.children)
			if c.wantErr {
				if err == nil || err.Error() != "a coluna do épico segue as filhas" {
					t.Fatalf("erro = %v", err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %s, %v; quer %s", got, err, c.want)
			}
		})
	}
}

func TestCheckHierarchy(t *testing.T) {
	ok := func(h HierarchyCheck) HierarchyCheck {
		if h.Card.Type == "" {
			h.Card.Type = TypeTarefa
		}
		if h.Card.Priority == "" {
			h.Card.Priority = PrioMedia
		}
		if h.OldType == "" {
			h.OldType = h.Card.Type
		}
		return h
	}
	epic := &Card{ID: 10, Type: TypeEpico}
	cases := []struct {
		name string
		h    HierarchyCheck
		want string
	}{
		{"válido", ok(HierarchyCheck{Parent: epic}), ""},
		{"tipo inválido", ok(HierarchyCheck{Card: Card{Type: "x"}, OldType: TypeTarefa}), "tipo inválido (use epico, historia, tarefa ou bug)"},
		{"prioridade inválida", ok(HierarchyCheck{Card: Card{Priority: "x"}}), "prioridade inválida (use mais_alta, alta, media, baixa ou mais_baixa)"},
		{"pai de si mesmo", ok(HierarchyCheck{Card: Card{ID: 10, Type: TypeTarefa}, Parent: epic}), "um card não pode ser pai de si mesmo"},
		{"pai não épico", ok(HierarchyCheck{Parent: &Card{ID: 11, Type: TypeTarefa}}), "o pai precisa ser um épico"},
		{"épico com pai", ok(HierarchyCheck{Card: Card{Type: TypeEpico}, OldType: TypeEpico, Parent: epic}), "um épico não pode ter pai"},
		{"virar épico com pai", ok(HierarchyCheck{Card: Card{Type: TypeEpico}, OldType: TypeTarefa, Parent: epic}), "tire o card do épico antes"},
		{"deixar de ser épico com filhas", ok(HierarchyCheck{Card: Card{Type: TypeTarefa}, OldType: TypeEpico, ChildCount: 3}), "remova as 3 filhas do épico antes"},
		{"virar épico ao vivo", ok(HierarchyCheck{Card: Card{Type: TypeEpico}, OldType: TypeTarefa, Live: true}), "o card tem uma sessão ao vivo; encerre a sessão antes de transformá-lo em épico"},
		{"deixar de ser épico ao vivo", ok(HierarchyCheck{Card: Card{Type: TypeTarefa}, OldType: TypeEpico, Live: true}), "o épico tem uma sessão ao vivo; encerre a sessão antes de mudar o tipo"},
		{"card novo épico ao vivo", ok(HierarchyCheck{Card: Card{Type: TypeEpico}, OldType: TypeEpico, Live: true, New: true}), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := CheckHierarchy(c.h)
			if c.want == "" {
				if err != nil {
					t.Fatalf("erro inesperado: %v", err)
				}
				return
			}
			if err == nil || err.Error() != c.want {
				t.Fatalf("erro = %v, quer %q", err, c.want)
			}
		})
	}
}

func TestNormalizeLabels(t *testing.T) {
	got, err := NormalizeLabels([]string{" front ", "front", "api"})
	if err != nil || len(got) != 2 || got[0] != "front" || got[1] != "api" {
		t.Fatalf("got %v, %v", got, err)
	}
	if got, err := NormalizeLabels(nil); err != nil || got != nil {
		t.Fatalf("nil: got %v, %v", got, err)
	}
	if _, err := NormalizeLabels([]string{"a b"}); err == nil || err.Error() != `label sem espaços: "a b"` {
		t.Fatalf("espaço interno: %v", err)
	}
	for _, bad := range []string{"a,b", "a\x1fb", "a\x00"} {
		want := fmt.Sprintf("label sem vírgula nem caractere de controle: %q", bad)
		if _, err := NormalizeLabels([]string{bad}); err == nil || err.Error() != want {
			t.Fatalf("%q: erro = %v", bad, err)
		}
	}
	if _, err := NormalizeLabels([]string{""}); err == nil {
		t.Fatal("vazia deveria dar erro")
	}
	if _, err := NormalizeLabels([]string{strings.Repeat("é", 51)}); err == nil {
		t.Fatal("51 runas deveria dar erro")
	}
	if _, err := NormalizeLabels([]string{strings.Repeat("é", 50)}); err != nil {
		t.Fatalf("50 runas deveria passar: %v", err)
	}
}

func TestValidPoints(t *testing.T) {
	for _, v := range []float64{0, 0.5, 13} {
		v := v
		if err := ValidPoints(&v); err != nil {
			t.Fatalf("%v: %v", v, err)
		}
	}
	if err := ValidPoints(nil); err != nil {
		t.Fatal(err)
	}
	neg := -1.0
	if err := ValidPoints(&neg); err == nil || err.Error() != "points não pode ser negativo" {
		t.Fatalf("erro = %v", err)
	}
}

func TestWaitingOnYou(t *testing.T) {
	cases := []struct {
		name string
		in   WaitingInput
		ok   bool
		why  string
	}{
		{"pergunta em andamento", WaitingInput{Column: EmAndamento, OpenQuestions: 1}, true, "pergunta aberta"},
		{"pergunta em concluído", WaitingInput{Column: Concluido, OpenQuestions: 1}, false, ""},
		{"revisão", WaitingInput{Column: Revisao}, true, "revisão"},
		{"handoff incompleto", WaitingInput{Column: Pausado, HandoffIncomplete: true}, true, "handoff incompleto"},
		{"handoff com pronto", WaitingInput{Column: Pausado, HandoffIncomplete: true, Ready: true}, false, ""},
		{"limite de custo", WaitingInput{Column: Pausado, PauseReason: PauseReasonLimit}, true, "limite de custo"},
		{"pronto para retomar", WaitingInput{Column: Pausado, Signal: SignalProntoParaRetomar}, true, "pronto para retomar"},
		{"nada", WaitingInput{Column: EmAndamento}, false, ""},
		{"pergunta antes de revisão", WaitingInput{Column: Revisao, OpenQuestions: 2}, true, "pergunta aberta"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, why := WaitingOnYou(c.in)
			if ok != c.ok || why != c.why {
				t.Fatalf("got (%v,%q), quer (%v,%q)", ok, why, c.ok, c.why)
			}
		})
	}
}
