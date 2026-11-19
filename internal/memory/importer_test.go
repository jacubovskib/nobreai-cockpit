package memory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestGroupImportRepetidaViraGlobal(t *testing.T) {
	per := map[int64][]NapkinItem{
		1: {{Category: CategoryShell, Rule: "Rodar go vet antes do commit", Do: "go vet ./..."}, {Category: CategoryDominio, Rule: "Só do projeto 1"}},
		2: {{Category: CategoryShell, Rule: "rodar GO VET antes do commit!", Do: "outro"}, {Category: CategoryDominio, Rule: "Só do projeto 2"}},
		3: {{Category: CategoryDominio, Rule: "Regra solta"}},
	}
	global, local := GroupImport(per)
	if len(global) != 1 {
		t.Fatalf("global = %+v", global)
	}
	g := global[0]
	if g.Item.Rule != "Rodar go vet antes do commit" || len(g.ProjectIDs) != 2 || g.ProjectIDs[0] != 1 || g.ProjectIDs[1] != 2 {
		t.Fatalf("g = %+v", g)
	}
	if len(local[1]) != 1 || local[1][0].Rule != "Só do projeto 1" || len(local[2]) != 1 || len(local[3]) != 1 {
		t.Fatalf("local = %+v", local)
	}
}

func TestGroupImportParecidaVaiParaGlobal(t *testing.T) {
	per := map[int64][]NapkinItem{
		1: {{Rule: "antes do commit rode os testes completos"}},
		2: {{Rule: "antes de commit rode os testes completos agora"}},
	}
	global, local := GroupImport(per)
	if len(global) != 1 || len(local) != 0 {
		t.Fatalf("global=%+v local=%+v", global, local)
	}
}

func TestGroupImportRepeticaoNoMesmoProjetoFicaLocal(t *testing.T) {
	per := map[int64][]NapkinItem{
		1: {{Rule: "mesma regra"}, {Rule: "mesma regra"}},
		2: {{Rule: "outra coisa bem diferente"}},
	}
	global, local := GroupImport(per)
	if len(global) != 0 || len(local[1]) != 2 || len(local[2]) != 1 {
		t.Fatalf("global=%+v local=%+v", global, local)
	}
}

func TestGroupImportVazio(t *testing.T) {
	global, local := GroupImport(nil)
	if len(global) != 0 || len(local) != 0 || local == nil {
		t.Fatalf("%+v %+v", global, local)
	}
}

func TestGroupImportDesempenho(t *testing.T) {
	vocab := make([]string, 3000)
	for i := range vocab {
		vocab[i] = fmt.Sprintf("palavra%d", i)
	}
	per := map[int64][]NapkinItem{}
	for n := 0; n < 500; n++ {
		var b strings.Builder
		for k := 0; k < 350; k++ {
			b.WriteString(vocab[(n*37+k*11)%len(vocab)])
			b.WriteByte(' ')
		}
		per[int64(n%5)+1] = append(per[int64(n%5)+1], NapkinItem{Rule: b.String()})
	}
	start := time.Now()
	GroupImport(per)
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("GroupImport levou %v", d)
	}
}
