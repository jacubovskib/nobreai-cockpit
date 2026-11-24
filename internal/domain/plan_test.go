package domain

import "testing"

func TestParseTaskStatus(t *testing.T) {
	for in, want := range map[string]TaskStatus{
		"andamento": TaskDoing, "em_andamento": TaskDoing, "a-fazer": TaskTodo, "a_fazer": TaskTodo,
		"revisao": TaskReview, "concluida": TaskDone,
	} {
		if got, ok := ParseTaskStatus(in); !ok || got != want {
			t.Errorf("ParseTaskStatus(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"feito", ""} {
		if _, ok := ParseTaskStatus(in); ok {
			t.Errorf("ParseTaskStatus(%q) deveria falhar", in)
		}
	}
	if TaskReview.Label() != "em revisão" {
		t.Fatalf("label = %q", TaskReview.Label())
	}
}

func TestMatchPlan(t *testing.T) {
	pats := DefaultSettings().PlanPatterns
	for _, p := range []string{"docs/plan-visao.md", "docs/plan.md", "docs/superpowers/plans/2026-11-24-x.md", ".kiro/specs/login/tasks.md"} {
		if !MatchPlan(pats, p) {
			t.Errorf("%q deveria casar", p)
		}
	}
	for _, p := range []string{"docs/superpowers/specs/x-design.md", ".kiro/specs/login/design.md", "/w/api/docs/plan.md", "web/docs/plan.md"} {
		if MatchPlan(pats, p) {
			t.Errorf("%q não deveria casar", p)
		}
	}
	if MatchPlan(nil, "docs/plan.md") {
		t.Error("sem padrões nada casa")
	}
}

func TestPlanOpen(t *testing.T) {
	var p *Plan
	if p.Open() != 0 {
		t.Fatal("nil")
	}
	p = &Plan{Tasks: []PlanTask{{Status: TaskDone}, {Status: TaskDoing}, {Status: TaskTodo}}}
	if p.Open() != 2 {
		t.Fatalf("open = %d", p.Open())
	}
}

func TestIsGitCommit(t *testing.T) {
	for cmd, want := range map[string]bool{
		"git commit -m x":            true,
		"rtk git commit -m x":        true,
		"git -C /w/api commit -am x": true,
		"cd x && git commit":         true,
		"git -c user.name=a commit":  true,
		"git log --grep commit":      false,
		"git status":                 false,
		`grep -r "git commit" docs`:  false,
		"":                           false,
	} {
		if got := IsGitCommit(cmd); got != want {
			t.Errorf("IsGitCommit(%q) = %v, quer %v", cmd, got, want)
		}
	}
}
