package api

import (
	"strconv"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func TestUsageTotalsProject(t *testing.T) {
	a := newTestAPI(t)
	a.card(t)
	if rec := a.do(t, "GET", "/api/usage/totals?group=project", nil, 0); rec.Code != 200 {
		t.Fatalf("group=project = %d %s", rec.Code, rec.Body)
	}
	if rec := a.do(t, "GET", "/api/usage/totals?group=agent&project=1", nil, 0); rec.Code != 200 {
		t.Fatalf("project=1 = %d %s", rec.Code, rec.Body)
	}
	rec := a.do(t, "GET", "/api/usage/totals?project=abc", nil, 0)
	if rec.Code != 400 || decode[map[string]string](t, rec)["error"] != "projeto inválido" {
		t.Fatalf("project=abc = %d %s", rec.Code, rec.Body)
	}
}

func TestUsageTotalsExpandsFamily(t *testing.T) {
	a := newTestAPI(t)
	pai, _ := a.st.CreateProject("loja", "/r/loja")
	filho, _, _ := a.st.CreateChild("web", "/r/loja/web", pai.ID)
	now := testNow
	for i, p := range []domain.Project{pai, filho} {
		id := "claude:" + p.Name
		if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", ProviderSessionID: p.Name, PID: 10 + i, Cwd: p.Path, ProjectID: &p.ID, FullCapture: true, StartedAt: now, LastEventAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: id, MessageID: "m" + p.Name, Model: "m", Input: int64(100 * (i + 1)), At: now}}); err != nil {
			t.Fatal(err)
		}
	}
	sumQ := func(project int64, extra string) int64 {
		rec := a.do(t, "GET", "/api/usage/totals?project="+strconv.FormatInt(project, 10)+extra, nil, 0)
		if rec.Code != 200 {
			t.Fatalf("totals = %d %s", rec.Code, rec.Body)
		}
		var n int64
		for _, x := range decode[[]store.Total](t, rec) {
			n += x.Input
		}
		return n
	}
	sum := func(project int64) int64 { return sumQ(project, "") }
	if got := sumQ(pai.ID, "&exact=1"); got != 100 {
		t.Fatalf("pai exact = %d", got)
	}
	if got := sum(pai.ID); got != 300 {
		t.Fatalf("pai = %d", got)
	}
	if got := sum(filho.ID); got != 200 {
		t.Fatalf("filho = %d", got)
	}
}

func TestListSessionsEffectiveProject(t *testing.T) {
	a := newTestAPI(t)
	p1, _ := a.st.CreateProject("api", "/w/api")
	p2, _ := a.st.CreateProject("web", "/w/web")
	card, err := a.st.CreateCard(domain.Card{Title: "x", ProjectID: p2.ID})
	if err != nil {
		t.Fatal(err)
	}
	now := testNow
	for i, id := range []string{"claude:com-card", "claude:sem-card"} {
		if err := a.st.StartSession(domain.Session{ID: id, Agent: "claude", ProviderSessionID: id, PID: 10 + i, Cwd: "/w/api", ProjectID: &p1.ID, FullCapture: true, StartedAt: now.Add(time.Duration(i) * time.Second), LastEventAt: now}); err != nil {
			t.Fatal(err)
		}
		if err := a.st.UpsertUsage([]domain.UsageRow{{SessionID: id, MessageID: "m" + id, Model: "m", Input: 100, At: now}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.st.AttachSession("claude:com-card", card.Key, false); err != nil {
		t.Fatal(err)
	}
	rec := a.do(t, "GET", "/api/sessions", nil, 0)
	if rec.Code != 200 {
		t.Fatalf("sessions = %d %s", rec.Code, rec.Body)
	}
	got := map[string]*int64{}
	for _, s := range decode[[]SessionWithUsage](t, rec) {
		got[s.ID] = s.EffectiveProjectID
	}
	if got["claude:com-card"] == nil || *got["claude:com-card"] != p2.ID {
		t.Fatalf("com card: %v, quer %d", got["claude:com-card"], p2.ID)
	}
	if got["claude:sem-card"] == nil || *got["claude:sem-card"] != p1.ID {
		t.Fatalf("sem card: %v, quer %d", got["claude:sem-card"], p1.ID)
	}
	rec = a.do(t, "GET", "/api/usage/totals?group=project", nil, 0)
	by := map[string]int64{}
	for _, x := range decode[[]store.Total](t, rec) {
		by[x.Key] = x.Input
	}
	if by[strconv.FormatInt(p2.ID, 10)] != 100 || by[strconv.FormatInt(p1.ID, 10)] != 100 {
		t.Fatalf("totais por projeto = %v", by)
	}
}
