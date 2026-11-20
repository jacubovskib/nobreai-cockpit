package claude

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/memory"
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
		t.Fatal(err)
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

func setBudget(t *testing.T, e *env, n int) {
	t.Helper()
	set, _ := e.st.GetSettings()
	set.MemoryBudgetTokens = n
	if err := e.st.PutSettings(set); err != nil {
		t.Fatal(err)
	}
}

func addMany(t *testing.T, e *env, project *int64, n int) {
	t.Helper()
	for i := range n {
		addMem(t, e, project, fmt.Sprintf("regra número %03d com acentuação e um texto comprido o bastante para pesar", i))
	}
}

// O Claude Code corta o additionalContext em 10.000 caracteres: com o limite
// máximo, muitas memórias e um pacote de retomada grande, o total cabe e o
// pacote vai inteiro.
func TestSessionStart_MemoriesFitContextCap(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	setBudget(t, e, 2000)
	addMany(t, e, &p.ID, 120)
	where := strings.TrimSpace(strings.Repeat("Parei no passo de migração do esquema, faltam os testes de ponta a ponta. ", 68))
	if _, err := e.st.CreateCard(domain.Card{Title: "Grande", ProjectID: p.ID, Branch: "main",
		Column: domain.Pausado, WhereStopped: where}); err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(where) < 4900 {
		t.Fatalf("pacote curto demais: %d", utf8.RuneCountInString(where))
	}
	ctx := additionalContext(t, e.call(t, "session_start", withEnv("NOBRE_RESUME", "NAC-1")))
	if n := utf8.RuneCountInString(ctx); n > 10000 {
		t.Fatalf("contexto com %d caracteres", n)
	}
	if !strings.Contains(ctx, where) || !strings.Contains(ctx, "## Onde parei") {
		t.Fatalf("pacote de retomada incompleto:\n%s", ctx)
	}
	if !strings.Contains(ctx, "regra número 000") || !strings.Contains(ctx, "memórias fora do limite") {
		t.Fatalf("bloco de memórias:\n%s", ctx)
	}
	if strings.Index(ctx, "Memórias deste projeto") > strings.Index(ctx, "## Onde parei") {
		t.Fatal("bloco depois do pacote")
	}
}

// Com pouco resto, quem limita é o teto de tokens da configuração.
func TestSessionStart_SmallRestUsesBudget(t *testing.T) {
	e := newEnv(t)
	p := e.project(t)
	setBudget(t, e, 300)
	addMany(t, e, &p.ID, 40)
	ctx := additionalContext(t, e.call(t, "session_start"))
	items := make([]memory.Item, 0, 40)
	_, ms, _, _ := e.st.MemoriesForSession(domain.Session{ProjectID: &p.ID}) // p é raiz
	for _, m := range ms {
		items = append(items, memory.Item{ID: m.ID, Position: m.Position, Category: memory.Category(m.Category), Rule: m.Rule, Do: m.Do})
	}
	chosen, left := memory.Select(items, 300)
	want := strings.TrimRight(memory.FormatBlock(chosen, left), "\n")
	if left == 0 || !strings.Contains(ctx, want) {
		t.Fatalf("bloco diferente do teto de tokens (left=%d):\n%s", left, ctx)
	}
}

// Sem espaço para nenhuma linha, vai só o cabeçalho com o "+N" (se couber);
// o pacote de retomada sempre vai inteiro.
func TestSessionStart_NoRoomOnlyHeader(t *testing.T) {
	onlyHeader := false
	for size := 7900; size <= 8400; size += 20 {
		e := newEnv(t)
		p := e.project(t)
		addMany(t, e, &p.ID, 3)
		where := strings.Repeat("x", size)
		if _, err := e.st.CreateCard(domain.Card{Title: "Enorme", ProjectID: p.ID, Branch: "main",
			Column: domain.Pausado, WhereStopped: where}); err != nil {
			t.Fatal(err)
		}
		ctx := additionalContext(t, e.call(t, "session_start", withEnv("NOBRE_RESUME", "NAC-1")))
		if !strings.Contains(ctx, where) {
			t.Fatalf("size %d: pacote cortado", size)
		}
		if strings.Contains(ctx, "Memórias deste projeto") && utf8.RuneCountInString(ctx) > 9500 {
			t.Fatalf("size %d: memórias passaram do teto (%d)", size, utf8.RuneCountInString(ctx))
		}
		if strings.Contains(ctx, "Memórias deste projeto") && !strings.Contains(ctx, "regra número") {
			if !strings.Contains(ctx, "+3 memórias fora do limite") {
				t.Fatalf("size %d: cabeçalho sem +N:\n%s", size, ctx)
			}
			onlyHeader = true
		}
	}
	if !onlyHeader {
		t.Fatal("nenhum tamanho deixou só o cabeçalho")
	}
}
