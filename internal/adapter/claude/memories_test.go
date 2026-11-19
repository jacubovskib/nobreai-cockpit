package claude

import (
	"strings"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func addMem(t *testing.T, e *env, project *int64, rule string) {
	t.Helper()
	if _, err := e.st.CreateMemory(domain.Memory{ProjectID: project, Category: "shell", Rule: rule, Do: "faça assim"}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStart_InjectsMemories(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	addMem(t, e, &p.ID, "regra do projeto um")
	addMem(t, e, &p.ID, "regra do projeto dois")
	addMem(t, e, nil, "regra global")
	ctx := additionalContext(t, e.call(t, "session_start"))
	i1, i2, i3 := strings.Index(ctx, "regra do projeto um"), strings.Index(ctx, "regra do projeto dois"), strings.Index(ctx, "regra global")
	if i1 < 0 || i2 < i1 || i3 < i2 {
		t.Fatalf("ordem das memórias:\n%s", ctx)
	}
	if !strings.Contains(ctx, "Memórias deste projeto") || strings.Index(ctx, "Memórias deste projeto") < strings.Index(ctx, registeredPhrase) {
		t.Fatalf("bloco fora do lugar:\n%s", ctx)
	}
}

func TestSessionStart_MemoriesFollowCardApp(t *testing.T) {
	e, loja, web := newMonorepoWorktreeEnv(t)
	api, _, err := e.st.CreateChild("api", "/r/loja/apps/api", loja.ID)
	if err != nil {
		t.Fatal(err)
	}
	addMem(t, e, &web.ID, "regra do app web")
	addMem(t, e, &api.ID, "regra do app api")
	addMem(t, e, &loja.ID, "regra da raiz")
	addMem(t, e, nil, "regra global")
	if _, err := e.st.CreateCard(domain.Card{Title: "Tela", ProjectID: loja.ID, Branch: "feat/x"}); err != nil {
		t.Fatal(err)
	}
	e.call(t, "session_start", with("cwd", "/r/loja"))
	if err := e.st.AttachSession(sessID, "NAC-1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.DB.Exec(`UPDATE cards SET project_id=? WHERE key='NAC-1'`, web.ID); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start", with("cwd", "/r/loja"), with("source", "resume")))
	for _, want := range []string{"regra do app web", "regra da raiz", "regra global"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("sem %q:\n%s", want, ctx)
		}
	}
	if strings.Contains(ctx, "regra do app api") {
		t.Fatalf("memória de outro app:\n%s", ctx)
	}
}

func TestSessionStart_MemoriesOnCompact(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	addMem(t, e, &p.ID, "regra no compact")
	e.call(t, "session_start")
	ctx := additionalContext(t, e.call(t, "session_start", with("source", "compact")))
	if !strings.Contains(ctx, "regra no compact") {
		t.Fatalf("compact sem memórias:\n%s", ctx)
	}
}

func TestSessionStart_NoMemoriesWithoutCapture(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	addMem(t, e, &p.ID, "regra escondida")
	if _, err := e.st.DB.Exec(`UPDATE projects SET capture=0 WHERE id=?`, p.ID); err != nil {
		t.Skip("coluna de captura: ", err)
	}
	r := e.call(t, "session_start")
	if strings.Contains(r.Stdout, "regra escondida") {
		t.Fatalf("injetou sem captura:\n%s", r.Stdout)
	}
}

func TestSessionStart_BudgetZero(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	addMem(t, e, &p.ID, "regra desligada")
	set, _ := e.st.GetSettings()
	set.MemoryBudgetTokens = 0
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
	ctx := additionalContext(t, e.call(t, "session_start"))
	if strings.Contains(ctx, "regra desligada") || strings.Contains(ctx, "Memórias deste projeto") {
		t.Fatalf("budget 0 injetou:\n%s", ctx)
	}
}

func TestSkillTemSecaoMemorias(t *testing.T) {
	for _, want := range []string{"## Memórias", "nobre lembrar", "nobre memorias"} {
		if !strings.Contains(skillContent, want) {
			t.Fatalf("skill.md sem %q", want)
		}
	}
}
