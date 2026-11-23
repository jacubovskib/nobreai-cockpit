package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/agents"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// namedAgent é um agente falso com binário e nome próprios.
type namedAgent struct {
	binAdapter
	n string
}

func (a *namedAgent) Name() string { return a.n }

func newNamedTermEnv(t *testing.T, names ...string) *termEnv {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/cockpit.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := NewHub()
	t.Cleanup(hub.Close)
	mgr := pty.NewManager(pty.Options{})
	t.Cleanup(mgr.Close)
	var list []adapter.Adapter
	for _, n := range names {
		list = append(list, &namedAgent{binAdapter: binAdapter{bin: "/bin/cat"}, n: n})
	}
	srv := httptest.NewServer(New(st, Deps{Hub: hub, Terminals: mgr, Agents: agents.New(list...)}))
	t.Cleanup(srv.Close)
	return &termEnv{srv: srv, mgr: mgr, hub: hub}
}

func TestTerminalsAceitamQualquerAgenteRegistrado(t *testing.T) {
	e := newNamedTermEnv(t, "kiro", "claude")
	resp, info := e.create(t, map[string]any{"agent": "claude"})
	if resp.StatusCode != http.StatusCreated || info.Agent != "claude" {
		t.Fatalf("claude: %d %+v", resp.StatusCode, info)
	}
	resp, info = e.create(t, map[string]any{})
	if resp.StatusCode != http.StatusCreated || info.Agent != "kiro" {
		t.Fatalf("padrão deve ser o primeiro registrado: %d %+v", resp.StatusCode, info)
	}
	if resp, _ = e.create(t, map[string]any{"agent": "codex"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("codex não registrado = %d", resp.StatusCode)
	}
}

func TestContinueRecusaAgenteNaoRegistrado(t *testing.T) {
	a, l := continueAPI(t, nil, "")
	got := errOf(t, a, http.StatusBadRequest, map[string]any{"agent": "codex"})
	if got != msgAgentUnavailable || len(l.calls) != 0 {
		t.Fatalf("got %q", got)
	}
}

func TestAgentsListaInstalavelEStatusLine(t *testing.T) {
	a, _, _ := agentsAPI(t)
	rec := a.do(t, "GET", "/api/agents", nil, 0)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	rows := decode[[]map[string]any](t, rec)
	if len(rows) != 3 {
		t.Fatalf("rows = %v", rows)
	}
	for i, want := range []struct{ inst, sl bool }{{true, true}, {false, false}, {false, false}} {
		if rows[i]["installable"] != want.inst || rows[i]["status_line"] != want.sl {
			t.Errorf("%v: installable/status_line = %v/%v", rows[i]["agent"], rows[i]["installable"], rows[i]["status_line"])
		}
	}
	if !strings.Contains(rec.Body.String(), `"installable"`) {
		t.Fatal("campo ausente")
	}
}
