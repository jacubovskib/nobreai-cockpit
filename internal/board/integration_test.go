package board

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

func gitI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// integFixture cria repo com main e feat/x (com commit próprio) e um card na coluna col.
func integFixture(t *testing.T, col domain.Column, branch string) (*store.Store, string, domain.Card) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não instalado")
	}
	d := t.TempDir()
	gitI(t, d, "init", "-q", "-b", "main")
	gitI(t, d, "commit", "-q", "--allow-empty", "-m", "init")
	gitI(t, d, "checkout", "-q", "-b", "feat/x")
	gitI(t, d, "commit", "-q", "--allow-empty", "-m", "feat")
	gitI(t, d, "checkout", "-q", "main")
	st, err := store.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p, err := st.CreateProject("api", d)
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateCard(domain.Card{Title: "x", ProjectID: p.ID, Branch: branch, Column: col})
	if err != nil {
		t.Fatal(err)
	}
	return st, d, c
}

var integNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func runCheck(t *testing.T, st *store.Store) bool {
	t.Helper()
	ch, err := CheckIntegrations(st, gitinfo.Exec{}, integNow)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func decisionsOf(t *testing.T, st *store.Store, c domain.Card) []string {
	t.Helper()
	rows, err := st.DB.Query(`SELECT what FROM decisions WHERE card_id=?`, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var w string
		rows.Scan(&w)
		out = append(out, w)
	}
	return out
}

func mergeFeat(t *testing.T, d string) {
	gitI(t, d, "merge", "-q", "--no-ff", "-m", "merge", "feat/x")
}

func TestIntegrationConcludesReviewCard(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	tip := gitI(t, d, "rev-parse", "feat/x")
	if !runCheck(t, st) {
		t.Fatal("1ª passada deveria mudar")
	}
	got, _ := st.GetCardByID(c.ID)
	if !got.BranchDiverged || got.BranchHead != tip || got.BranchMergedAt != nil || got.Column != domain.Revisao {
		t.Fatalf("após 1ª: %+v", got)
	}
	mergeFeat(t, d)
	if !runCheck(t, st) {
		t.Fatal("2ª passada deveria mudar")
	}
	got, _ = st.GetCardByID(c.ID)
	if got.Column != domain.Concluido || got.BranchMergedAt == nil {
		t.Fatalf("após 2ª: %+v", got)
	}
	ds := decisionsOf(t, st, c)
	want := "Branch feat/x integrada em main (" + tip[:7] + ")"
	if len(ds) != 1 || ds[0] != want {
		t.Fatalf("decisões = %v, want %q", ds, want)
	}
}

func TestIntegrationKeepsInProgressCard(t *testing.T) {
	st, d, c := integFixture(t, domain.EmAndamento, "feat/x")
	runCheck(t, st)
	mergeFeat(t, d)
	runCheck(t, st)
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.EmAndamento || got.BranchMergedAt == nil {
		t.Fatalf("card = %+v", got)
	}
	if len(decisionsOf(t, st, c)) != 1 {
		t.Fatal("deveria gravar a decisão")
	}
}

func TestIntegrationDeletedBranchAfterSeen(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	runCheck(t, st)
	mergeFeat(t, d)
	gitI(t, d, "branch", "-D", "feat/x")
	if !runCheck(t, st) {
		t.Fatal("deveria integrar pelo branch_head")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.Concluido || got.BranchMergedAt == nil {
		t.Fatalf("card = %+v", got)
	}
}

func TestIntegrationDeletedBranchNeverSeen(t *testing.T) {
	st, _, c := integFixture(t, domain.Revisao, "feat/nunca")
	if runCheck(t, st) {
		t.Fatal("nada deveria mudar")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.Revisao || got.BranchMergedAt != nil || len(decisionsOf(t, st, c)) != 0 {
		t.Fatalf("card = %+v", got)
	}
}

func TestIntegrationIgnoresBaseBranchCard(t *testing.T) {
	st, _, c := integFixture(t, domain.Revisao, "main")
	if runCheck(t, st) {
		t.Fatal("nada deveria mudar")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.Revisao || got.BranchMergedAt != nil {
		t.Fatalf("card = %+v", got)
	}
}

func TestIntegrationRemoteOnlyMerge(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	runCheck(t, st)
	tip := gitI(t, d, "rev-parse", "feat/x")
	gitI(t, d, "update-ref", "refs/remotes/origin/main", tip)
	if !runCheck(t, st) {
		t.Fatal("deveria integrar via origin/main")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.Concluido || got.BranchMergedAt == nil {
		t.Fatalf("card = %+v", got)
	}
}

func TestIntegrationRunsOnce(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	runCheck(t, st)
	mergeFeat(t, d)
	runCheck(t, st)
	if runCheck(t, st) {
		t.Fatal("3ª passada não deveria mudar")
	}
	if n := len(decisionsOf(t, st, c)); n != 1 {
		t.Fatalf("decisões = %d", n)
	}
}

func TestIntegrationIgnoresBranchResetOntoBase(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	runCheck(t, st) // diverge: head gravado
	gitI(t, d, "checkout", "-q", "feat/x")
	gitI(t, d, "reset", "-q", "--hard", "main")
	gitI(t, d, "checkout", "-q", "main")
	if runCheck(t, st) {
		t.Fatal("reset sem merge não deveria mudar nada")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.Revisao || got.BranchMergedAt != nil || len(decisionsOf(t, st, c)) != 0 {
		t.Fatalf("card = %+v", got)
	}
}

// Branch integrada que ganha commit novo volta a ser acompanhada: a marca some
// e, depois do novo merge, o card em revisão conclui (NAC-5 da spec).
func TestIntegrationRearmsAfterNewCommit(t *testing.T) {
	st, d, c := integFixture(t, domain.EmAndamento, "feat/x")
	runCheck(t, st)
	mergeFeat(t, d)
	if !runCheck(t, st) {
		t.Fatal("merge deveria mudar")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.BranchMergedAt == nil || got.Column != domain.EmAndamento {
		t.Fatalf("após merge: %+v", got)
	}
	if runCheck(t, st) {
		t.Fatal("passada ociosa com branch integrada não deveria mudar")
	}
	if n := len(decisionsOf(t, st, c)); n != 1 {
		t.Fatalf("decisões após passada ociosa = %d", n)
	}
	gitI(t, d, "checkout", "-q", "feat/x")
	gitI(t, d, "commit", "-q", "--allow-empty", "-m", "parte 2")
	gitI(t, d, "checkout", "-q", "main")
	tip2 := gitI(t, d, "rev-parse", "feat/x")
	if !runCheck(t, st) {
		t.Fatal("commit novo deveria rearmar")
	}
	got, _ = st.GetCardByID(c.ID)
	if got.BranchMergedAt != nil || got.BranchHead != tip2 || !got.BranchDiverged {
		t.Fatalf("não rearmou: %+v", got)
	}
	if err := st.MoveCard(c.Key, domain.Revisao); err != nil {
		t.Fatal(err)
	}
	mergeFeat(t, d)
	if !runCheck(t, st) {
		t.Fatal("2º merge deveria mudar")
	}
	got, _ = st.GetCardByID(c.ID)
	if got.Column != domain.Concluido || got.BranchMergedAt == nil {
		t.Fatalf("após 2º merge: %+v", got)
	}
	ds := decisionsOf(t, st, c)
	if len(ds) != 2 || ds[1] != "Branch feat/x integrada em main ("+tip2[:7]+")" {
		t.Fatalf("decisões = %v", ds)
	}
}

// O card saiu de revisão entre a leitura e a gravação: registra a integração,
// mas não conclui.
func TestIntegrationStaleColumnNotConcluded(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	runCheck(t, st)
	snap, _ := st.GetCardByID(c.ID)
	mergeFeat(t, d)
	if err := st.MoveCard(c.Key, domain.EmAndamento); err != nil {
		t.Fatal(err)
	}
	p, _ := st.ListProjects()
	if _, err := checkCardIntegration(st, gitinfo.Exec{}, integNow, p[0], snap); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Column != domain.EmAndamento || got.BranchMergedAt == nil || len(decisionsOf(t, st, c)) != 1 {
		t.Fatalf("card = %+v", got)
	}
}

// O branch do card mudou entre a leitura e a gravação: nada é gravado no
// branch novo (nem o head, nem a integração).
func TestIntegrationStaleBranchWritesNothing(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	p, _ := st.ListProjects()
	snap, _ := st.GetCardByID(c.ID)
	if _, err := st.SetCardBranch(c.ID, "feat/y"); err != nil {
		t.Fatal(err)
	}
	if ch, err := checkCardIntegration(st, gitinfo.Exec{}, integNow, p[0], snap); err != nil || ch {
		t.Fatalf("head no branch velho = %v, %v", ch, err)
	}
	got, _ := st.GetCardByID(c.ID)
	if got.Branch != "feat/y" || got.BranchHead != "" || got.BranchDiverged {
		t.Fatalf("head gravado no branch novo: %+v", got)
	}
	// Mesmo caso na integração: snapshot divergente em feat/x, card já em feat/y.
	if _, err := st.SetCardBranch(c.ID, "feat/x"); err != nil {
		t.Fatal(err)
	}
	runCheck(t, st)
	snap, _ = st.GetCardByID(c.ID)
	if _, err := st.SetCardBranch(c.ID, "feat/y"); err != nil {
		t.Fatal(err)
	}
	mergeFeat(t, d)
	if ch, err := checkCardIntegration(st, gitinfo.Exec{}, integNow, p[0], snap); err != nil || ch {
		t.Fatalf("integração no branch velho = %v, %v", ch, err)
	}
	got, _ = st.GetCardByID(c.ID)
	if got.BranchMergedAt != nil || got.Column != domain.Revisao || len(decisionsOf(t, st, c)) != 0 {
		t.Fatalf("integração gravada no branch novo: %+v", got)
	}
}

// O pai de um monorepo cobre os cards dos apps: o branch do card do app
// integrado na base do pai ganha BranchMergedAt.
func TestIntegrationCoversFamilyCards(t *testing.T) {
	st, d, c := integFixture(t, domain.Revisao, "feat/x")
	parents, err := st.ListProjects()
	if err != nil || len(parents) != 1 {
		t.Fatalf("projetos = %v, %v", parents, err)
	}
	if err := os.MkdirAll(filepath.Join(d, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	app, _, err := st.CreateChild("web", filepath.Join(d, "web"), parents[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetProjectBase(app.ID, "x"); err != nil { // a base do app não vale
		t.Fatal(err)
	}
	if err := st.SetCardProject(c.ID, app.ID); err != nil {
		t.Fatal(err)
	}
	runCheck(t, st) // 1ª passada: registra o branch divergente
	mergeFeat(t, d)
	if !runCheck(t, st) {
		t.Fatal("a passada após o merge deveria mudar")
	}
	got, _ := st.GetCardByID(c.ID)
	if got.BranchMergedAt == nil || got.Column != domain.Concluido {
		t.Fatalf("card do app: %+v", got)
	}
}
