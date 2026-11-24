package domain

import (
	"testing"
	"time"
)

func TestReadiness(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t0 := now.Add(-time.Hour)
	ago := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	at := func(x time.Time) *time.Time { return &x }
	s := DefaultSettings()

	full := Card{Goal: "g", DoneWhen: "d", WhereStopped: "aqui", WhereStoppedAt: ago(time.Minute), HowToTest: "go test"}
	fullCt := Content{
		Files: []EssentialFile{{Path: "a.go", Why: "porque"}},
		Todos: []Todo{{Text: "x"}},
	}

	byRule := func(r ReadinessResult, rule string) bool {
		for _, it := range r.Items {
			if it.Rule == rule {
				return it.OK
			}
		}
		t.Fatalf("regra %s ausente", rule)
		return false
	}

	t.Run("card vazio", func(t *testing.T) {
		r := Readiness(Card{}, Content{}, false, nil, s, now)
		if r.Met != 0 || r.Total != 5 || r.Ready || len(r.Items) != 5 {
			t.Fatalf("r = %+v", r)
		}
		want := []string{"goal", "where_stopped", "essential_file", "todo_open", "how_to_test"}
		for i, it := range r.Items {
			if it.Rule != want[i] || it.Label == "" {
				t.Fatalf("item %d = %+v", i, it)
			}
		}
	})
	t.Run("sessão viva dentro do TTL", func(t *testing.T) {
		c := Card{WhereStopped: "aqui", WhereStoppedAt: ago(10 * time.Minute)}
		if !byRule(Readiness(c, Content{}, true, nil, s, now), "where_stopped") {
			t.Fatal("esperava OK")
		}
	})
	t.Run("sessão viva fora do TTL", func(t *testing.T) {
		c := Card{WhereStopped: "aqui", WhereStoppedAt: ago(20 * time.Minute)}
		if byRule(Readiness(c, Content{}, true, nil, s, now), "where_stopped") {
			t.Fatal("esperava não OK")
		}
	})
	t.Run("sem sessão, edição depois", func(t *testing.T) {
		c := Card{WhereStopped: "aqui", WhereStoppedAt: at(t0)}
		if byRule(Readiness(c, Content{}, false, at(t0.Add(time.Minute)), s, now), "where_stopped") {
			t.Fatal("esperava não OK")
		}
	})
	t.Run("sem sessão, edição antes", func(t *testing.T) {
		c := Card{WhereStopped: "aqui", WhereStoppedAt: at(t0)}
		if !byRule(Readiness(c, Content{}, false, at(t0.Add(-time.Minute)), s, now), "where_stopped") {
			t.Fatal("esperava OK")
		}
	})
	t.Run("onde parei em branco não conta, mesmo com data recente", func(t *testing.T) {
		for _, text := range []string{"", "   \n"} {
			c := Card{WhereStopped: text, WhereStoppedAt: ago(time.Minute)}
			if byRule(Readiness(c, Content{}, true, nil, s, now), "where_stopped") {
				t.Fatalf("%q: esperava não OK", text)
			}
		}
	})
	t.Run("sem WhereStoppedAt", func(t *testing.T) {
		if byRule(Readiness(Card{}, Content{}, false, nil, s, now), "where_stopped") {
			t.Fatal("esperava não OK")
		}
	})
	t.Run("arquivo sem motivo não conta", func(t *testing.T) {
		ct := Content{Files: []EssentialFile{{Path: "a.go"}}}
		if byRule(Readiness(Card{}, ct, false, nil, s, now), "essential_file") {
			t.Fatal("esperava não OK")
		}
	})
	t.Run("só todos abertos contam", func(t *testing.T) {
		ct := Content{Todos: []Todo{{Text: "a", Done: true}}}
		if byRule(Readiness(Card{}, ct, false, nil, s, now), "todo_open") {
			t.Fatal("esperava não OK")
		}
		ct.Todos = append(ct.Todos, Todo{Text: "b"})
		if !byRule(Readiness(Card{}, ct, false, nil, s, now), "todo_open") {
			t.Fatal("esperava OK")
		}
	})
	t.Run("tudo preenchido", func(t *testing.T) {
		r := Readiness(full, fullCt, true, nil, s, now)
		if !r.Ready || r.Met != 5 || r.Total != 5 {
			t.Fatalf("r = %+v", r)
		}
	})
	t.Run("pergunta em aberto", func(t *testing.T) {
		s2 := DefaultSettings()
		s2.Rules["no_open_questions"] = true
		ct := fullCt
		ct.Questions = []Question{{Text: "?"}}
		r := Readiness(full, ct, true, nil, s2, now)
		if r.Total != 6 || r.Ready || byRule(r, "no_open_questions") {
			t.Fatalf("r = %+v", r)
		}
		ct.Questions = []Question{{Text: "?", Answer: "sim"}}
		if r := Readiness(full, ct, true, nil, s2, now); !r.Ready || r.Total != 6 {
			t.Fatalf("r = %+v", r)
		}
	})
}

func TestReadinessOpenTaskSatisfiesTodo(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	todo := func(ct Content) bool {
		for _, it := range Readiness(Card{}, ct, false, nil, DefaultSettings(), now).Items {
			if it.Rule == "todo_open" {
				return it.OK
			}
		}
		t.Fatal("regra todo_open ausente")
		return false
	}
	ct := Content{Plan: &Plan{Tasks: []PlanTask{{N: 1, Status: TaskTodo}}}}
	if !todo(ct) {
		t.Fatal("tarefa a_fazer deveria satisfazer todo_open")
	}
	ct.Plan.Tasks[0].Status = TaskDone
	if todo(ct) {
		t.Fatal("tudo concluído deveria deixar pendente")
	}
}
