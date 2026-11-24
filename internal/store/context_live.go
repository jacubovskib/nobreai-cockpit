package store

import (
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

const (
	defaultWindowTokens  = 200000
	observedWindowTokens = 1000000
	// limitPct é a fração da janela usada para estimar os turnos até o limite.
	limitPct = 0.95
	// rewriteMinTokens é o menor cache_write num turno que conta como regravação grande.
	rewriteMinTokens = 50000
	rewriteRecent    = 60 * time.Minute
	cacheHitTurns    = 20
	idleCtxMin       = 200000 // só importa perder o cache acima disso
	topOutputsLimit  = 5
)

// ContextRewrite é uma regravação grande de cache: o turno e quanto foi gravado.
type ContextRewrite struct {
	Tokens int64     `json:"tokens"`
	At     time.Time `json:"at"`
}

// ContextLiveOutput é uma das maiores saídas da janela.
type ContextLiveOutput struct {
	Tool   string `json:"tool"`
	Key    string `json:"key"`
	Tokens int64  `json:"tokens"`
	At     string `json:"at"`
}

// ContextLive é o medidor de contexto de uma sessão (uma janela de conversa).
type ContextLive struct {
	Tokens            int64               `json:"tokens"` // último turno da janela no transcript principal
	Model             string              `json:"model"`
	WindowTokens      int64               `json:"window_tokens"`
	WindowEstimated   bool                `json:"window_estimated"` // janela padrão por modelo desconhecido
	Pct               float64             `json:"pct"`
	PctOnly           bool                `json:"pct_only"`        // só a % da janela é conhecida (Kiro): sem tokens, cache nem TTL; crescimento e turnos em pontos percentuais
	GrowthPerTurn     int64               `json:"growth_per_turn"` // média desde a última compactação; 0 com menos de 3 turnos
	TurnsToLimit      int64               `json:"turns_to_limit"`  // até 95% da janela; 0 sem crescimento
	AtLimit           bool                `json:"at_limit"`        // já em 95% da janela ou mais (TurnsToLimit fica 0)
	TurnsSinceCompact int                 `json:"turns_since_compact"`
	Compactions       int                 `json:"compactions"`
	CacheHitPct       float64             `json:"cache_hit_pct"` // últimos 20 turnos
	RecentRewrite     *ContextRewrite     `json:"recent_rewrite"`
	TTL               string              `json:"ttl"`
	IdleSec           int64               `json:"idle_sec"`
	IdleOverTTL       bool                `json:"idle_over_ttl"` // parada além do cache, com contexto > 200.000
	TopOutputs        []ContextLiveOutput `json:"top_outputs"`
	// Segment é o segmento (do arquivo) do primeiro turno depois da última
	// compactação da janela (turnos com tokens ou só com %): só muda numa compactação da
	// própria janela.
	Segment int `json:"-"`
}

// liveRow é um turno lido para o cálculo do medidor.
type liveRow struct {
	tokens, input, read, write int64
	pct                        float64
	pctOnly                    bool
	segment                    int
	at                         time.Time
	ttl                        string
}

// SessionContext mede o contexto da sessão agora.
func (s *Store) SessionContext(sessionID string) (ContextLive, error) {
	return s.SessionContextAt(sessionID, time.Now())
}

// SessionContextAt é SessionContext com o relógio dado. As linhas da janela:
// na principal, as do próprio session_id com window_session_id ” ou o dela;
// na filha, as da principal marcadas com o id da filha. Só o transcript
// principal (sem subagentes) conta.
func (s *Store) SessionContextAt(sessionID string, now time.Time) (ContextLive, error) {
	set, err := s.GetSettings()
	if err != nil {
		return ContextLive{TopOutputs: []ContextLiveOutput{}}, err
	}
	return s.SessionContextWith(sessionID, now, set)
}

// SessionContextWith é SessionContextAt com as preferências já lidas (uma leitura por requisição).
func (s *Store) SessionContextWith(sessionID string, now time.Time, set domain.Settings) (ContextLive, error) {
	out := ContextLive{TopOutputs: []ContextLiveOutput{}}
	var parent, model, mainPath, lastEvent string
	err := s.DB.QueryRow(`SELECT parent_session_id, model, transcript_path, last_event_at FROM sessions WHERE id=?`, sessionID).
		Scan(&parent, &model, &mainPath, &lastEvent)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	owner, mainPath, where, args, err := s.windowScope(sessionID, parent, mainPath)
	if err != nil {
		return out, err
	}
	if model == "" {
		err := s.DB.QueryRow(`SELECT model FROM usage WHERE session_id=? AND `+where+` AND model<>'' ORDER BY at DESC LIMIT 1`,
			append([]any{owner}, args...)...).Scan(&model)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
	}
	out.Model = model

	rows, err := s.DB.Query(`SELECT tokens, input, cache_read, cache_write, segment, at, ttl, pct FROM context_turns
		WHERE session_id=? AND path=? AND `+where+` ORDER BY turn`, append([]any{owner, mainPath}, args...)...)
	if err != nil {
		return out, err
	}
	var turns []liveRow
	for rows.Next() {
		var t liveRow
		var at string
		var pct sql.NullFloat64
		if err := rows.Scan(&t.tokens, &t.input, &t.read, &t.write, &t.segment, &at, &t.ttl, &pct); err != nil {
			rows.Close()
			return out, err
		}
		t.pct, t.pctOnly = pct.Float64, pct.Valid && t.tokens == 0
		t.at, _ = time.Parse(time.RFC3339Nano, at)
		turns = append(turns, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	foreign, err := s.foreignCompactions(owner, mainPath, where, args)
	if err != nil {
		return out, err
	}

	var maxTokens int64
	for _, t := range turns {
		maxTokens = max(maxTokens, t.tokens)
	}
	fillWindow(&out, set, maxTokens)
	if n := len(turns); n > 0 {
		last := turns[n-1]
		out.Tokens = last.tokens
		// TTL do cache: o gravado mais recentemente (um turno só de leitura não o muda).
		for i := n - 1; i >= 0; i-- {
			if turns[i].ttl != "" {
				out.TTL = turns[i].ttl
				break
			}
		}
		if last.pctOnly {
			out.PctOnly, out.WindowTokens, out.WindowEstimated, out.Pct = true, 0, false, last.pct
			fillPctStats(&out, turns, foreign)
		} else {
			out.Pct = float64(last.tokens) * 100 / float64(out.WindowTokens)
			fillTurnStats(&out, turns, foreign, now)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, lastEvent); err == nil {
		out.IdleSec = max(0, int64(now.Sub(t).Seconds()))
		ttl := 5 * time.Minute
		if out.TTL == "1h" {
			ttl = time.Hour
		}
		out.IdleOverTTL = out.Tokens > idleCtxMin && now.Sub(t) > ttl
	}

	orows, err := s.DB.Query(`SELECT tool, key, tokens, at FROM tool_outputs
		WHERE session_id=? AND path=? AND `+where+` ORDER BY tokens DESC, at LIMIT ?`,
		append([]any{owner, mainPath}, append(args, topOutputsLimit)...)...)
	if err != nil {
		return out, err
	}
	defer orows.Close()
	for orows.Next() {
		var o ContextLiveOutput
		if err := orows.Scan(&o.Tool, &o.Key, &o.Tokens, &o.At); err != nil {
			return out, err
		}
		out.TopOutputs = append(out.TopOutputs, o)
	}
	return out, orows.Err()
}

// windowScope resolve as linhas da janela sessionID (parent é o
// parent_session_id dela, path o transcript_path): na principal, as do
// próprio id com window_session_id ” ou o dela; na filha, as gravadas sob a
// principal (owner) com o id da filha. mainPath é o transcript principal (o da
// principal, para a filha); where e args filtram window_session_id.
func (s *Store) windowScope(sessionID, parent, path string) (owner, mainPath, where string, args []any, err error) {
	owner, mainPath, where, args = sessionID, path, `window_session_id IN ('', ?)`, []any{sessionID}
	if parent != "" {
		owner, where = parent, `window_session_id = ?`
		if err := s.DB.QueryRow(`SELECT transcript_path FROM sessions WHERE id=?`, parent).Scan(&mainPath); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", "", "", nil, err
		}
	}
	return owner, mainPath, where, args, nil
}

// foreignCompactions devolve os segmentos abertos por compactação de outra
// janela (context_compactions fora de where): não contam para esta.
func (s *Store) foreignCompactions(owner, mainPath, where string, args []any) (map[int]bool, error) {
	foreign := map[int]bool{}
	rows, err := s.DB.Query(`SELECT segment FROM context_compactions WHERE session_id=? AND path=? AND NOT (`+where+`)`,
		append([]any{owner, mainPath}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var seg int
		if err := rows.Scan(&seg); err != nil {
			return nil, err
		}
		foreign[seg] = true
	}
	return foreign, rows.Err()
}

// fillWindow define a janela: tabela por modelo, "[1m]" no nome, senão 200.000
// (estimada); contexto observado acima da janela vale 1.000.000, e como o máximo
// só cresce a janela fica fixa para a sessão.
func fillWindow(out *ContextLive, set domain.Settings, maxTokens int64) {
	switch w, ok := set.ModelWindows[out.Model]; {
	case ok:
		out.WindowTokens = int64(w)
	case strings.Contains(out.Model, "[1m]"):
		out.WindowTokens = observedWindowTokens
	default:
		out.WindowTokens, out.WindowEstimated = defaultWindowTokens, true
	}
	if maxTokens > out.WindowTokens {
		out.WindowTokens, out.WindowEstimated = observedWindowTokens, false
	}
}

// fillSegments conta as compactações da janela e devolve os turnos desde a
// última (turns não vazio). Compactação = salto de segmento entre turnos
// consecutivos com alguma compactação que não é de outra janela no meio.
// foreign são os segmentos abertos por compactação de outra janela.
func fillSegments(out *ContextLive, turns []liveRow, foreign map[int]bool) []liveRow {
	from := 0
	for i := 1; i < len(turns); i++ {
		if ownCompaction(turns[i-1].segment, turns[i].segment, foreign) {
			out.Compactions++
			from = i
		}
	}
	out.Segment = turns[from].segment
	out.TurnsSinceCompact = len(turns) - from
	return turns[from:]
}

// fillPctStats é fillTurnStats para o agente que só informa a % da janela:
// crescimento em pontos percentuais por turno e turnos até 95%, sem cache.
func fillPctStats(out *ContextLive, turns []liveRow, foreign map[int]bool) {
	seg := fillSegments(out, turns, foreign)
	if len(seg) >= 3 {
		if g := (seg[len(seg)-1].pct - seg[0].pct) / float64(len(seg)-1); g > 0 {
			out.GrowthPerTurn = int64(math.Round(g))
			if room := limitPct*100 - out.Pct; room > 0 {
				out.TurnsToLimit = int64(math.Ceil(room / g)) // para cima, como no caminho de tokens; usa a média sem arredondar (GrowthPerTurn só exibe)
			}
		}
	}
	out.AtLimit = out.Pct >= limitPct*100
}

// fillTurnStats calcula crescimento, compactações, cache e regravação (turns
// não vazio).
func fillTurnStats(out *ContextLive, turns []liveRow, foreign map[int]bool, now time.Time) {
	seg := fillSegments(out, turns, foreign)
	if len(seg) >= 3 {
		if g := (seg[len(seg)-1].tokens - seg[0].tokens) / int64(len(seg)-1); g > 0 {
			out.GrowthPerTurn = g
			if room := int64(float64(out.WindowTokens)*limitPct) - out.Tokens; room > 0 {
				out.TurnsToLimit = (room + g - 1) / g
			}
		}
	}
	out.AtLimit = float64(out.Tokens) >= float64(out.WindowTokens)*limitPct
	recent := turns[max(0, len(turns)-cacheHitTurns):]
	var read, total int64
	for _, t := range recent {
		read += t.read
		total += t.input + t.read + t.write
	}
	if total > 0 {
		out.CacheHitPct = float64(read) * 100 / float64(total)
	}
	for _, t := range turns {
		if t.write >= rewriteMinTokens && now.Sub(t.at) <= rewriteRecent &&
			(out.RecentRewrite == nil || t.write > out.RecentRewrite.Tokens) {
			out.RecentRewrite = &ContextRewrite{Tokens: t.write, At: t.at}
		}
	}
}

// ownCompaction diz se entre um turno do segmento prev e o seguinte, do
// segmento cur, houve compactação da janela: algum segmento em (prev, cur]
// que não foi aberto por outra janela (sem registro conta como da janela).
func ownCompaction(prev, cur int, foreign map[int]bool) bool {
	if cur < prev {
		return true
	}
	for k := prev + 1; k <= cur; k++ {
		if !foreign[k] {
			return true
		}
	}
	return false
}
