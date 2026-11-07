package prompts

import (
	"errors"
	"reflect"
	"testing"
)

func allVars() Vars {
	v := Vars{}
	for _, n := range KnownVars {
		v[n] = "<" + n + ">"
	}
	return v
}

func TestRenderKnownVars(t *testing.T) {
	got, err := Render("Card {{card.id}} em {{ git.branch }}", Vars{"card.id": "NAC-42", "git.branch": "feat/oauth2"})
	if err != nil || got != "Card NAC-42 em feat/oauth2" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRenderUnknownVars(t *testing.T) {
	got, err := Render("{{card.foo}} {{x}} {{card.foo}} {{card.id}}", Vars{"card.id": "1"})
	var ue *UnknownVarsError
	if !errors.As(err, &ue) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(ue.Names, []string{"card.foo", "x"}) {
		t.Fatalf("names = %v", ue.Names)
	}
	if got != "" {
		t.Fatalf("renderização parcial: %q", got)
	}
}

func TestRenderLeavesNonNamesUntouched(t *testing.T) {
	in := "{{ 1+1 }} {{Card.Id}} {{}} {{a b}} ${{x-y}}"
	got, err := Render(in, Vars{})
	if err != nil || got != in {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestRenderKnownButEmptyValue(t *testing.T) {
	got, err := Render("[{{card.id}}]", Vars{})
	if err != nil || got != "[]" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSeedsRenderWithAllVars(t *testing.T) {
	seeds := Seeds()
	if len(seeds) != 5 {
		t.Fatalf("seeds = %d", len(seeds))
	}
	defaults := 0
	for _, p := range seeds {
		if p.IsDefaultHandoff {
			defaults++
			if p.Slug != "retomar-card" {
				t.Fatalf("default = %s", p.Slug)
			}
		}
		out, err := Render(p.Body, allVars())
		if err != nil || out == "" {
			t.Fatalf("%s: %q, %v", p.Slug, out, err)
		}
	}
	if defaults != 1 {
		t.Fatalf("defaults = %d", defaults)
	}
}
