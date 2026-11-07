package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func cardCost(t *testing.T, s *Store, key string) float64 {
	t.Helper()
	ts, err := s.Totals(TotalsFilter{GroupBy: "card"})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range ts {
		if x.Key == key {
			return x.CostUSD
		}
	}
	return 0
}

func sessionIDs(t *testing.T, s *Store, cardID int64) []string {
	t.Helper()
	xs, err := s.SessionsForCard(cardID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, x := range xs {
		ids = append(ids, x.ID)
	}
	return ids
}

// Reattach A→B: o custo gasto em A continua em A, mesmo reingerindo a mesma mensagem.
func TestUsageStaysOnCardAfterReattach(t *testing.T) {
	s := newTestStore(t)
	a := seedCard(t, s, domain.Backlog)
	b := seedCard(t, s, domain.Backlog)
	if err := s.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(newSession("claude:x", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:x", a.Key, false); err != nil {
		t.Fatal(err)
	}
	m1 := domain.UsageRow{SessionID: "claude:x", MessageID: "m1", Model: "m", Input: 2_000_000, At: time.Now()}
	if err := s.UpsertUsage([]domain.UsageRow{m1}); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:x", b.Key, false); err != nil {
		t.Fatal(err)
	}
	m2 := domain.UsageRow{SessionID: "claude:x", MessageID: "m2", Model: "m", Input: 1_000_000, At: time.Now()}
	if err := s.UpsertUsage([]domain.UsageRow{m1, m2}); err != nil {
		t.Fatal(err)
	}
	if got := cardCost(t, s, a.Key); got != 2 {
		t.Fatalf("custo de A = %v, want 2", got)
	}
	if got := cardCost(t, s, b.Key); got != 1 {
		t.Fatalf("custo de B = %v, want 1", got)
	}
	// A continua listando a sessão, com o uso só de A.
	if ids := sessionIDs(t, s, a.ID); len(ids) != 1 || ids[0] != "claude:x" {
		t.Fatalf("sessões de A = %v", ids)
	}
	ts, err := s.Totals(TotalsFilter{GroupBy: "session", CardID: &a.ID})
	if err != nil || len(ts) != 1 || ts[0].Input != 2_000_000 {
		t.Fatalf("uso da sessão em A = %+v, %v", ts, err)
	}
}

// force: a sessão deslocada perde a posse, mas continua no histórico do card.
func TestForceKeepsDisplacedSessionInHistory(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if err := s.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"claude:a", "claude:b"} {
		if err := s.StartSession(newSession(id, i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUsage([]domain.UsageRow{{SessionID: "claude:a", MessageID: "m1", Model: "m", Input: 3_000_000, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:b", c.Key, true); err != nil {
		t.Fatal(err)
	}
	if a, _ := s.GetSession("claude:a"); a.CardID != nil {
		t.Fatalf("claude:a ainda é dona: %v", a.CardID)
	}
	if ids := sessionIDs(t, s, c.ID); len(ids) != 2 {
		t.Fatalf("sessões do card = %v, want claude:a e claude:b", ids)
	}
	if got := cardCost(t, s, c.Key); got != 3 {
		t.Fatalf("custo do card = %v, want 3", got)
	}
	// Mensagem nova da sessão deslocada já não vai para o card.
	if err := s.UpsertUsage([]domain.UsageRow{{SessionID: "claude:a", MessageID: "m2", Model: "m", Input: 1_000_000, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if got := cardCost(t, s, c.Key); got != 3 {
		t.Fatalf("custo do card = %v, want 3", got)
	}
}

// Uso ingerido antes de a sessão ter card vai para o card no attach (como os eventos).
func TestAttachMovesUnattributedUsage(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if err := s.PutPrice(domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(newSession("claude:a", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertUsage([]domain.UsageRow{{SessionID: "claude:a", MessageID: "m1", Model: "m", Input: 1_000_000, At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if got := cardCost(t, s, ""); got != 1 {
		t.Fatalf("sem card = %v", got)
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if got := cardCost(t, s, c.Key); got != 1 {
		t.Fatalf("custo do card = %v, want 1", got)
	}
}

// Sessão viva cujo processo morreu (pid_dead_at) não bloqueia o card e é
// desligada dele no attach, ficando só no histórico.
func TestAttachIgnoresDeadProcessOwner(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	for i, id := range []string{"claude:a", "claude:b"} {
		if err := s.StartSession(newSession(id, i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkPIDDead("claude:a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:b", c.Key, false); err != nil {
		t.Fatalf("err = %v, want nil (processo de claude:a morreu)", err)
	}
	if a, _ := s.GetSession("claude:a"); a.CardID != nil {
		t.Fatalf("claude:a ainda ligada: %v", a.CardID)
	}
	// O reconciliador encerrar claude:a depois não pausa o card da nova dona.
	if err := s.EndSession("claude:a", "pid", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetCard(c.Key); got.Column != domain.EmAndamento {
		t.Fatalf("column = %s", got.Column)
	}
}

func TestHasLiveSession(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	if live, err := s.HasLiveSession(c.ID); err != nil || live {
		t.Fatalf("sem sessão: %v %v", live, err)
	}
	if err := s.StartSession(newSession("claude:a", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSession("claude:a", c.Key, false); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.HasLiveSession(c.ID); !live {
		t.Fatal("deveria estar viva")
	}
	if err := s.MarkPIDDead("claude:a", time.Now()); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.HasLiveSession(c.ID); live {
		t.Fatal("processo morto não é viva")
	}
}

func TestRecentEndedWithoutCard(t *testing.T) {
	s := newTestStore(t)
	c := seedCard(t, s, domain.Backlog)
	now := time.Now().UTC()
	mk := func(id string, ended time.Duration, card bool) {
		x := newSession(id, 1)
		if card {
			x.CardID = &c.ID
		}
		if err := s.StartSession(x); err != nil {
			t.Fatal(err)
		}
		if ended > 0 {
			if err := s.EndSession(id, "exit", now.Add(-ended)); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("claude:recent", time.Hour, false)
	mk("claude:old", 25*time.Hour, false)
	mk("claude:card", time.Hour, true)
	mk("claude:open", 0, false)
	got, err := s.EndedWithoutCardSince(now.Add(-24 * time.Hour))
	if err != nil || len(got) != 1 || got[0].ID != "claude:recent" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPriceCacheNullable(t *testing.T) {
	s := newTestStore(t)
	zero := 0.0
	if err := s.PutPrice(domain.Price{Model: "a", InputPerM: 1, OutputPerM: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutPrice(domain.Price{Model: "b", InputPerM: 1, OutputPerM: 1, CacheWritePerM: &zero, CacheReadPerM: &zero}); err != nil {
		t.Fatal(err)
	}
	ps, err := s.ListPrices()
	if err != nil || len(ps) != 2 {
		t.Fatalf("%+v %v", ps, err)
	}
	if ps[0].CacheWritePerM != nil || ps[0].CacheReadPerM != nil {
		t.Fatalf("a: em branco deveria ficar nil: %+v", ps[0])
	}
	if ps[1].CacheWritePerM == nil || *ps[1].CacheWritePerM != 0 || ps[1].CacheReadPerM == nil {
		t.Fatalf("b: 0 explícito deveria ficar 0: %+v", ps[1])
	}
}

// Banco na versão 2: a migração liga o uso ao card da sessão e transforma os
// preços de cache 0 (gravados quando o campo ficava em branco) em desconhecidos.
func TestMigrateFromV2(t *testing.T) {
	path := t.TempDir() + "/cockpit.db"
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"001_init.sql", "002_pid_dead_at.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`PRAGMA user_version = 2`,
		`INSERT INTO projects(name, path) VALUES ('p', '/p')`,
		`INSERT INTO cards(key, project_id, title, created_at, updated_at) VALUES ('NAC-1', 1, 't', '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`,
		`INSERT INTO sessions(id, agent, card_id, started_at, last_event_at) VALUES ('claude:1', 'claude', 1, '2026-11-01T00:00:00Z', '2026-11-01T00:00:00Z')`,
		`INSERT INTO usage(session_id, message_id, input, at) VALUES ('claude:1', 'm1', 10, '2026-11-01T00:00:00Z')`,
		`INSERT INTO prices(model, input_per_m, output_per_m, cache_write_per_m, cache_read_per_m) VALUES ('a', 3, 15, 0, 0.3)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var cardID sql.NullInt64
	if err := s.DB.QueryRow(`SELECT card_id FROM usage WHERE message_id='m1'`).Scan(&cardID); err != nil || cardID.Int64 != 1 {
		t.Fatalf("card_id = %v, %v", cardID, err)
	}
	ps, err := s.ListPrices()
	if err != nil || len(ps) != 1 {
		t.Fatalf("%+v %v", ps, err)
	}
	if ps[0].CacheWritePerM != nil || ps[0].CacheReadPerM == nil || *ps[0].CacheReadPerM != 0.3 || ps[0].InputPerM != 3 {
		t.Fatalf("preço migrado = %+v", ps[0])
	}
}
