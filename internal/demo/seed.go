// Package demo carrega os dados ilustrativos do design num banco vazio
// (nobred --seed-demo), para conferir as telas sem depender de hooks.
package demo

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// ErrNotEmpty é devolvido por Seed quando o banco já tem cards.
var ErrNotEmpty = errors.New("o banco não está vazio; use outro --db")

const (
	projectName = "nobretech/portal-api"
	projectPath = "/tmp/nobreai-demo/portal-api"

	opus   = "claude-opus-5-5"
	sonnet = "claude-sonnet-5-5"
	codex  = "gpt-5-codex"
	kiro   = "auto"
)

// Preços só do banco de demonstração (USD por milhão de tokens); dão custos
// na mesma ordem de grandeza do design.
var demoPrices = []domain.Price{
	{Model: opus, InputPerM: 3.5, OutputPerM: 15, CacheWritePerM: 4.375, CacheReadPerM: 0.35},
	{Model: sonnet, InputPerM: 3, OutputPerM: 15, CacheWritePerM: 3.75, CacheReadPerM: 0.3},
	{Model: codex, InputPerM: 2.5, OutputPerM: 12, CacheReadPerM: 0.25},
	{Model: kiro, InputPerM: 2, OutputPerM: 12, CacheReadPerM: 0.2},
}

// Gasto diário por agente nos 7 dias do gráfico (do mais antigo até hoje).
var dailyCost = map[string][7]float64{
	"claude": {6.10, 7.40, 2.30, 1.10, 8.20, 6.90, 5.73},
	"codex":  {3.20, 2.10, 1.40, 0, 4.10, 6.30, 8.20},
	"kiro":   {0.40, 1.10, 0.58, 0, 0.90, 1.70, 0.69},
}

var defaultModel = map[string]string{"claude": opus, "codex": codex, "kiro": kiro}

// ready indica quais itens de prontidão o card cumpre: objetivo, onde parei,
// arquivo essencial, falta (todo aberto) e como testar.
type ready struct{ goal, where, file, todo, test bool }

func firstN(n int) ready {
	return ready{n >= 1, n >= 2, n >= 3, n >= 4, n >= 5}
}

type sessSpec struct {
	agent, model string
	day, h, m    int // início: dia relativo a hoje e hora local (hoje: relativo a "agora = 14:32")
	durMin       int
	in, out, cac int64  // tokens exatos; zero = derivar de rest
	live         bool   // sessão viva (PID = livePID)
	prompt       string // prompt capturado no início da sessão
	promptAt     [3]int // dia, h, m do prompt (se prompt != "")
}

type cardSpec struct {
	title, where, branch string
	col                  domain.Column
	r                    ready
	targetUSD            float64 // custo do card; as sessões sem tokens dividem o resto
	sessions             []sessSpec
}

var cards = []cardSpec{
	{title: "Webhook de pagamento com retry exponencial", where: "Objetivo definido. Nenhuma sessão iniciada.", branch: "feat/webhook-retry", col: domain.Backlog, r: firstN(1)},
	{title: "Exportar pedidos em CSV", where: "Objetivo e critério de pronto preenchidos.", branch: "feat/export-csv", col: domain.Backlog, r: ready{goal: true, todo: true}},
	{title: heroTitle, branch: "feat/oauth2", col: domain.EmAndamento, r: firstN(4), sessions: []sessSpec{
		{agent: "claude", model: opus, day: -1, h: 10, m: 12, durMin: 52, in: 296000, out: 41000, cac: 43000},
		{agent: "codex", model: codex, day: -1, h: 16, m: 40, durMin: 25, in: 221000, out: 33000, cac: 36000,
			prompt: "continue o NAC-42", promptAt: [3]int{-1, 16, 40}},
		{agent: "claude", model: opus, day: 0, h: 13, m: 54, in: 303000, out: 47000, cac: 62000, live: true,
			prompt: "mantém o cookie antigo válido por 7 dias pra ninguém ser deslogado no deploy", promptAt: [3]int{0, 14, 5}},
	}},
	{title: "Paginação por cursor na API de pedidos", where: "cursor base64 pronto; falta teste de borda.", branch: "feat/cursor-pagination",
		col: domain.EmAndamento, r: firstN(3), targetUSD: 3.05, sessions: []sessSpec{
			{agent: "codex", model: codex, day: -1, h: 11, m: 0, durMin: 40},
			{agent: "codex", model: codex, day: 0, h: 14, m: 11, in: 204000, out: 29000, cac: 35000, live: true},
		}},
	{title: "Especificar módulo de faturas", where: "requisitos escritos; desenhando tabelas.", branch: "feat/invoices-spec",
		col: domain.EmAndamento, r: firstN(2), sessions: []sessSpec{
			{agent: "kiro", model: kiro, day: 0, h: 14, m: 23, in: 49000, out: 8000, cac: 7000, live: true},
		}},
	{title: "Cache de catálogo com Redis", where: "invalidação por evento em andamento.", branch: "feat/catalog-cache",
		col: domain.EmAndamento, r: firstN(3), sessions: []sessSpec{
			{agent: "claude", model: sonnet, day: 0, h: 13, m: 41, durMin: 39, in: 188000, out: 22000, cac: 30000},
		}},
	{title: "Corrigir teste instável no checkout", branch: "fix/checkout-flaky", col: domain.Pausado,
		r: ready{goal: true, file: true}, targetUSD: 2.48, sessions: []sessSpec{
			{agent: "codex", model: codex, day: -1, h: 15, m: 0, durMin: 30},
			{agent: "codex", model: codex, day: 0, h: 12, m: 20, durMin: 42, in: 311000, out: 46000, cac: 52000,
				prompt: "roda o teste de checkout 20 vezes seguidas e me mostra quais execuções falham e por quê", promptAt: [3]int{0, 12, 24}},
		}},
	{title: "Atualizar dependências Go para 1.25", where: "falta ajustar golangci-lint; tudo mais compila.", branch: "chore/go-1-25",
		col: domain.Pausado, r: firstN(5), targetUSD: 1.64, sessions: []sessSpec{
			{agent: "claude", model: sonnet, day: -4, h: 10, m: 0, durMin: 40},
			{agent: "claude", model: sonnet, day: -3, h: 14, m: 0, durMin: 35},
		}},
	{title: "Endpoint de métricas Prometheus", where: "PR #128 aberto. Testes passando. Aguardando revisão humana.", branch: "feat/prometheus-metrics",
		col: domain.Revisao, r: firstN(5), targetUSD: 6.10, sessions: []sessSpec{
			{agent: "claude", model: opus, day: -6, h: 10, m: 0, durMin: 50},
			{agent: "claude", model: opus, day: -5, h: 13, m: 0, durMin: 45},
			{agent: "claude", model: opus, day: -2, h: 15, m: 0, durMin: 40},
			{agent: "claude", model: opus, day: -1, h: 14, m: 2, durMin: 64, in: 402000, out: 58000, cac: 71000},
		}},
	{title: "Logs estruturados com slog", where: "Concluído em 3 de novembro. Mesclado na main.", branch: "feat/slog",
		col: domain.Concluido, r: firstN(5), targetUSD: 3.92, sessions: []sessSpec{
			{agent: "codex", model: codex, day: -2, h: 9, m: 0, durMin: 40},
			{agent: "codex", model: codex, day: -2, h: 11, m: 30, durMin: 35},
			{agent: "codex", model: codex, day: -2, h: 15, m: 0, durMin: 50},
		}},
	{title: "Healthcheck e readiness probe", where: "Concluído em 1º de novembro.", branch: "feat/healthcheck",
		col: domain.Concluido, r: firstN(5), targetUSD: 0.58, sessions: []sessSpec{
			{agent: "kiro", model: kiro, day: -1, h: 10, m: 0, durMin: 30,
				prompt: "antes de codar, escreve requisitos e desenho e espera eu aprovar", promptAt: [3]int{-1, 10, 2}},
		}},
}

const heroTitle = "Migrar autenticação para OAuth2"

type seeder struct {
	st      *store.Store
	now     time.Time
	livePID int
	prices  map[string]*domain.Price
	project domain.Project
	spent   map[string]float64 // "dia|agente" → custo já gravado
	events  []domain.Event
	ids     map[string][]string // título do card → ids das sessões, em ordem
	byTitle map[string]domain.Card
}

// Seed carrega os dados ilustrativos do design. Falha com ErrNotEmpty se o
// banco já tiver algum card. Todos os horários são relativos a now; as sessões
// ao vivo recebem PID = livePID para a reconciliação não as encerrar.
func Seed(st *store.Store, now time.Time, livePID int) error {
	existing, err := st.ListCards(store.CardFilter{})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return ErrNotEmpty
	}
	// Preços e limites seriam sobrescritos: um banco real ainda sem cards não serve.
	var configured int
	if err := st.DB.QueryRow(`SELECT (SELECT count(*) FROM prices) + (SELECT count(*) FROM limits)`).Scan(&configured); err != nil {
		return err
	}
	if configured > 0 {
		return ErrNotEmpty
	}
	s := &seeder{st: st, now: now, livePID: livePID, prices: map[string]*domain.Price{}, spent: map[string]float64{}, ids: map[string][]string{}, byTitle: map[string]domain.Card{}}
	steps := []func() error{s.setup, s.cards, s.history, s.noCardSession, s.fillDays, s.hero, s.feed, s.columns, s.flushEvents}
	for _, step := range steps {
		if err := step(); err != nil {
			return fmt.Errorf("dados de demonstração: %w", err)
		}
	}
	return nil
}

// RefreshLivePID aponta as sessões ainda abertas com PID conhecido para pid.
// Serve ao reinício do nobred sobre um banco de demonstração já carregado.
func RefreshLivePID(st *store.Store, pid int) error {
	_, err := st.DB.Exec(`UPDATE sessions SET pid=?, pid_dead_at=NULL WHERE ended_at IS NULL AND pid>0`, pid)
	return err
}

// at resolve um horário local: dias passados à hora indicada; hoje, relativo a
// "agora = 14:32" (o instante do design), para que "há 38 min" seja verdade.
func (s *seeder) at(day, h, m int) time.Time {
	if day == 0 {
		return s.now.Add(-time.Duration(14*60+32-(h*60+m)) * time.Minute)
	}
	l := s.now.In(time.Local)
	return time.Date(l.Year(), l.Month(), l.Day()+day, h, m, 0, 0, time.Local)
}

func (s *seeder) setup() error {
	var err error
	if s.project, err = s.st.CreateProject(projectName, projectPath); err != nil {
		return err
	}
	for i := range demoPrices {
		p := demoPrices[i]
		if err := s.st.PutPrice(p); err != nil {
			return err
		}
		s.prices[p.Model] = &p
	}
	for agent, daily := range map[string]float64{"claude": 15, "codex": 10, "kiro": 8} {
		if err := s.st.PutLimit(domain.Limit{Agent: agent, DailyUSD: daily, CardUSD: 12, WarnPct: 80, Action: "warn"}); err != nil {
			return err
		}
	}
	set, err := s.st.GetSettings()
	if err != nil {
		return err
	}
	set.Demo = true
	return s.st.PutSettings(set)
}

func (s *seeder) cost(model string, in, out, cacheRead int64) float64 {
	c, _ := usage.Cost(domain.UsageRow{Input: in, Output: out, CacheRead: cacheRead}, s.prices[model])
	return c
}

// tokensFor deriva tokens (50% do custo em entrada, 40% em saída, 10% em cache) para um custo alvo.
func (s *seeder) tokensFor(model string, usd float64) (in, out, cac int64) {
	p := s.prices[model]
	ceil := func(v float64) int64 { return int64(math.Ceil(v)) } // nunca abaixo do alvo: 8,20 de 10,00 tem de dar 82%
	return ceil(usd * 0.5e6 / p.InputPerM), ceil(usd * 0.4e6 / p.OutputPerM), ceil(usd * 0.1e6 / p.CacheReadPerM)
}

// cards cria os 11 cards do board; as sessões vêm em history (e hero para o
// conteúdo completo do card principal).
func (s *seeder) cards() error {
	for _, cs := range cards {
		c := domain.Card{
			Title: cs.title, ProjectID: s.project.ID, Branch: cs.branch, Column: domain.Backlog,
			WhereStopped: cs.where,
		}
		if cs.title == heroTitle {
			c.Goal = "Trocar a autenticação por sessão em cookie por OAuth2 (authorization code + PKCE) com o provedor de identidade da empresa, sem derrubar usuários logados."
			c.DoneWhen = "login, refresh e logout funcionam ponta a ponta; sessões antigas migram no primeiro acesso; go test ./internal/auth/... passa."
			c.WhereStopped = "Revogação no logout: /revoke chamado, falta store.Delete em handlers/logout.go:41 + teste."
		} else if cs.r.goal {
			c.Goal = "Entregar: " + cs.title + "."
			c.DoneWhen = "Testes passam e o PR é aprovado."
		}
		if cs.r.test {
			c.HowToTest = "go test ./..."
		}
		if cs.r.where {
			at := s.now.Add(-time.Minute)
			if !hasLive(cs) {
				at = s.now.Add(-2 * time.Hour)
			}
			c.WhereStoppedAt = &at
		}
		created, err := s.st.CreateCard(c)
		if err != nil {
			return err
		}
		if cs.title != heroTitle {
			if err := s.genericContent(created, cs); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasLive(cs cardSpec) bool {
	for _, x := range cs.sessions {
		if x.live {
			return true
		}
	}
	return false
}

func (s *seeder) genericContent(c domain.Card, cs cardSpec) error {
	if cs.r.file {
		_, name, _ := strings.Cut(c.Branch, "/")
		if err := s.st.UpsertEssentialFile(domain.EssentialFile{CardID: c.ID, Path: "internal/" + name + "/handler.go",
			Lines: "L1–80", Why: "Ponto de entrada da mudança."}); err != nil {
			return err
		}
	}
	if cs.r.todo {
		if _, err := s.st.AddTodo(domain.Todo{CardID: c.ID, Text: "Cobrir casos de borda com testes"}); err != nil {
			return err
		}
	}
	return nil
}

// history grava as sessões de todos os cards (menos o conteúdo do hero) e
// completa os dias do gráfico com uso de sessões anteriores à janela.
func (s *seeder) history() error {
	list, err := s.st.ListCards(store.CardFilter{})
	if err != nil {
		return err
	}
	for _, c := range list {
		s.byTitle[c.Title] = c
	}
	for _, cs := range cards {
		c := s.byTitle[cs.title]
		// Custo das sessões com tokens exatos; o resto do alvo vai às demais.
		var explicit float64
		unspecified := 0
		for _, x := range cs.sessions {
			if x.in > 0 {
				explicit += s.cost(x.model, x.in, x.out, x.cac)
			} else {
				unspecified++
			}
		}
		for i, x := range cs.sessions {
			if x.in == 0 {
				rest := max(cs.targetUSD-explicit, 0.05) / float64(unspecified)
				x.in, x.out, x.cac = s.tokensFor(x.model, rest)
			}
			if err := s.session(c, i+1, x); err != nil {
				return err
			}
		}
	}
	return nil
}

// session grava a sessão, o uso e os eventos básicos; encerra se não for viva.
func (s *seeder) session(c domain.Card, n int, x sessSpec) error {
	start := s.at(x.day, x.h, x.m)
	end := start.Add(time.Duration(x.durMin) * time.Minute)
	psid := fmt.Sprintf("demo-%s-%d", c.Key, n)
	id := x.agent + ":" + psid
	cid, pid := c.ID, s.project.ID
	sess := domain.Session{
		ID: id, Agent: x.agent, ProviderSessionID: psid, CardID: &cid, ProjectID: &pid,
		Cwd: projectPath, Branch: c.Branch, Model: x.model, FullCapture: true, StartedAt: start, LastEventAt: end,
	}
	if x.live {
		sess.PID = s.livePID
		sess.LastEventAt = s.now.Add(-time.Minute)
		end = s.now
	}
	if err := s.st.StartSession(sess); err != nil {
		return err
	}
	s.ids[c.Title] = append(s.ids[c.Title], id)
	if err := s.usageRows(id, x.model, x.agent, start, end, x.in, x.out, x.cac); err != nil {
		return err
	}
	s.event(id, domain.EvSessionStart, nil, start)
	if x.prompt != "" {
		s.event(id, domain.EvPrompt, map[string]any{"text": x.prompt}, s.at(x.promptAt[0], x.promptAt[1], x.promptAt[2]))
	}
	if x.live {
		return nil
	}
	s.event(id, domain.EvSessionEnd, nil, end)
	return s.st.EndSession(id, "sessão encerrada", end)
}

func dayKey(t time.Time, agent string) string {
	return t.In(time.Local).Format("2006-01-02") + "|" + agent
}

// usageRows divide os tokens da sessão em 3 mensagens (40%/35%/25%) ao longo
// de [start, end]; a última leva o resto, então a soma é exata.
func (s *seeder) usageRows(sessionID, model, agent string, start, end time.Time, in, out, cac int64) error {
	fr := []int64{40, 35, 25}
	var di, do, dc int64
	var rows []domain.UsageRow
	for i, f := range fr {
		ri, ro, rc := in*f/100, out*f/100, cac*f/100
		if i == len(fr)-1 {
			ri, ro, rc = in-di, out-do, cac-dc
		}
		di, do, dc = di+ri, do+ro, dc+rc
		rows = append(rows, domain.UsageRow{
			SessionID: sessionID, MessageID: fmt.Sprintf("%s#%d", sessionID, i+1), Model: model,
			Input: ri, Output: ro, CacheRead: rc,
			At: start.Add(end.Sub(start) * time.Duration(i+1) / time.Duration(len(fr))),
		})
	}
	for _, r := range rows {
		s.spent[dayKey(r.At, agent)] += s.cost(model, r.Input, r.Output, r.CacheRead)
	}
	return s.st.UpsertUsage(rows)
}

func (s *seeder) event(sessionID, kind string, data map[string]any, at time.Time) {
	raw := json.RawMessage("{}")
	if data != nil {
		raw, _ = json.Marshal(data)
	}
	s.events = append(s.events, domain.Event{SessionID: sessionID, Kind: kind, Data: raw, At: at})
}

// flushEvents grava os eventos em ordem cronológica, para que RecentEvents
// (por id) e "última atividade" reflitam a linha do tempo.
func (s *seeder) flushEvents() error {
	sort.SliceStable(s.events, func(i, j int) bool { return s.events[i].At.Before(s.events[j].At) })
	for _, e := range s.events {
		if err := s.st.AddEvent(e); err != nil {
			return err
		}
	}
	return nil
}

// noCardSession cria a sessão viva sem card (PID 0: a reconciliação a ignora).
func (s *seeder) noCardSession() error {
	pid := s.project.ID
	start := s.at(0, 14, 26)
	id := "claude:demo-sem-card"
	if err := s.st.StartSession(domain.Session{
		ID: id, Agent: "claude", ProviderSessionID: "demo-sem-card", ProjectID: &pid, Cwd: projectPath,
		Branch: "main", Model: sonnet, FullCapture: true, StartedAt: start, LastEventAt: s.now.Add(-time.Minute),
	}); err != nil {
		return err
	}
	s.event(id, domain.EvSessionStart, nil, start)
	return s.usageRows(id, sonnet, "claude", start, s.now, 12000, 2000, 3000)
}

// fillDays completa o gasto diário por agente do gráfico (7 dias locais até
// hoje) com uso de sessões históricas sem card, iniciadas antes da janela
// para não poluir a lista de sessões recentes.
func (s *seeder) fillDays() error {
	l := s.now.In(time.Local)
	dayStart := time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.Local)
	old := s.now.AddDate(0, 0, -14)
	for _, agent := range []string{"claude", "codex", "kiro"} {
		for i, target := range dailyCost[agent] {
			off := i - 6
			at := s.at(off, 12, 0)
			if off == 0 {
				at = s.now.Add(-3 * time.Hour)
				if at.Before(dayStart) {
					at = dayStart.Add(time.Minute)
				}
				if at.After(s.now) {
					at = s.now
				}
			}
			rem := target - s.spent[dayKey(at, agent)]
			if rem < 0.01 {
				continue
			}
			model := defaultModel[agent]
			id := fmt.Sprintf("%s:demo-hist-%d", agent, -off)
			if err := s.st.StartSession(domain.Session{ID: id, Agent: agent, ProviderSessionID: fmt.Sprintf("demo-hist-%d", -off),
				Cwd: projectPath, Model: model, StartedAt: old, LastEventAt: old}); err != nil {
				return err
			}
			in, out, cac := s.tokensFor(model, rem)
			if err := s.usageRows(id, model, agent, at, at, in, out, cac); err != nil {
				return err
			}
			if err := s.st.EndSession(id, "histórico", old); err != nil {
				return err
			}
		}
	}
	return nil
}

// columns coloca cada card na coluna do design (encerrar sessões moveu alguns).
func (s *seeder) columns() error {
	for _, cs := range cards {
		if err := s.st.MoveCard(s.byTitle[cs.title].Key, cs.col); err != nil {
			return err
		}
	}
	return nil
}

// hero completa o card principal: todos, decisões, arquivos essenciais,
// pergunta em aberto e os 14 eventos de arquivos tocados.
func (s *seeder) hero() error {
	c := s.byTitle[heroTitle]
	ss := s.ids[heroTitle]
	todos := []struct {
		text string
		by   int // 1..3 = sessão que concluiu; 0 = aberto
	}{
		{"Fluxo authorization code + PKCE", 1},
		{"Persistir refresh token criptografado", 2},
		{"Rotação automática do access token", 3},
		{"Apagar token local no logout e testar", 0},
		{"Migrar sessões antigas no primeiro acesso", 0},
		{"Atualizar docs/auth.md", 0},
	}
	for _, t := range todos {
		td := domain.Todo{CardID: c.ID, Text: t.text, Done: t.by > 0}
		if t.by > 0 {
			td.SessionID = ss[t.by-1]
		}
		if _, err := s.st.AddTodo(td); err != nil {
			return err
		}
	}
	decisions := []domain.Decision{
		{What: "PKCE obrigatório, mesmo para o backend", Why: "o provedor de identidade da empresa rejeita authorization code sem PKCE.", SessionID: ss[0], At: s.at(-1, 10, 31)},
		{What: "Refresh token criptografado em repouso com AES-GCM", Why: "o banco é compartilhado com o time de BI; a chave vem de AUTH_TOKEN_KEY.", SessionID: ss[1], At: s.at(-1, 16, 52)},
		{What: "Manter o cookie antigo válido por 7 dias", Why: "evita deslogar todo mundo no deploy; a migração acontece no primeiro acesso.", SessionID: ss[2], At: s.at(0, 14, 5)},
	}
	for _, d := range decisions {
		d.CardID = c.ID
		if _, err := s.st.AddDecision(d); err != nil {
			return err
		}
		s.event(d.SessionID, domain.EvDecision, map[string]any{"what": d.What, "why": d.Why}, d.At)
	}
	files := []domain.EssentialFile{
		{Path: "internal/auth/oauth.go", Lines: "todo", Why: "Fluxo authorization code + PKCE. Comece por aqui para entender o login."},
		{Path: "internal/auth/token_store.go", Lines: "L12–96", Why: "Persistência e criptografia do refresh token. Delete() ainda não é chamado por ninguém."},
		{Path: "handlers/logout.go", Lines: "L30–58", Why: "Onde parei: chamar store.Delete na linha 41 depois do /revoke."},
	}
	for _, f := range files {
		f.CardID = c.ID
		if err := s.st.UpsertEssentialFile(f); err != nil {
			return err
		}
	}
	s.event(ss[2], domain.EvEssentialFile, map[string]any{"path": files[1].Path, "why": files[1].Why, "lines": files[1].Lines}, s.at(0, 14, 28))
	if _, err := s.st.AddQuestion(domain.Question{CardID: c.ID, SessionID: ss[1],
		Text: "Revogar no deploy os refresh tokens de sessões antigas, ou deixar expirarem sozinhos?"}); err != nil {
		return err
	}

	touched := []struct {
		sess int
		kind string
		path string
		at   [3]int
	}{
		{0, domain.EvFileRead, "docs/auth.md", [3]int{-1, 10, 15}},
		{0, domain.EvFileRead, "internal/session/cookie.go", [3]int{-1, 10, 18}},
		{0, domain.EvFileEdit, "internal/auth/oauth_test.go", [3]int{-1, 10, 25}},
		{0, domain.EvFileEdit, "go.mod", [3]int{-1, 10, 30}},
		{0, domain.EvFileEdit, "go.sum", [3]int{-1, 10, 31}},
		{0, domain.EvFileEdit, "config/auth.go", [3]int{-1, 10, 40}},
		{1, domain.EvFileRead, ".env.example", [3]int{-1, 16, 43}},
		{1, domain.EvFileRead, "internal/db/migrations/0012.sql", [3]int{-1, 16, 45}},
		{1, domain.EvFileEdit, "internal/auth/token_store.go", [3]int{-1, 16, 50}},
		{1, domain.EvFileEdit, "cmd/api/main.go", [3]int{-1, 16, 58}},
		{2, domain.EvFileRead, "handlers/middleware.go", [3]int{0, 13, 58}},
		{2, domain.EvFileEdit, "handlers/login.go", [3]int{0, 14, 0}},
		{2, domain.EvFileEdit, "handlers/logout.go", [3]int{0, 14, 20}},
		{2, domain.EvFileEdit, "internal/auth/oauth.go", [3]int{0, 14, 30}},
	}
	for _, t := range touched {
		s.event(ss[t.sess], t.kind, map[string]any{"path": t.path}, s.at(t.at[0], t.at[1], t.at[2]))
	}
	return nil
}

// feed acrescenta a atividade recente das outras sessões (sala de controle).
func (s *seeder) feed() error {
	codexSess := s.ids["Paginação por cursor na API de pedidos"][1]
	kiroSess := s.ids["Especificar módulo de faturas"][0]
	staleSess := s.ids["Cache de catálogo com Redis"][0]
	s.event(codexSess, domain.EvNote, map[string]any{"text": "cursor codificado em base64 pronto; falta teste de borda"}, s.at(0, 14, 12))
	s.event(codexSess, domain.EvCommand, map[string]any{"command": "go test ./orders/... → 1 falha"}, s.at(0, 14, 31))
	d := domain.Decision{CardID: s.byTitle["Especificar módulo de faturas"].ID, SessionID: kiroSess, At: s.at(0, 14, 30),
		What: "Numeração de fatura por série, não global", Why: "cada série fiscal precisa de sequência própria sem lacunas."}
	if _, err := s.st.AddDecision(d); err != nil {
		return err
	}
	s.event(kiroSess, domain.EvDecision, map[string]any{"what": d.What, "why": d.Why}, d.At)
	s.event(staleSess, domain.EvHeartbeat, nil, s.at(0, 14, 20))
	return nil
}
