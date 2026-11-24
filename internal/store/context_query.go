package store

import (
	"database/sql"
	"errors"
	"sort"
	"time"
)

// ContextFilter limita as consultas de peso. From/To (zero = sem limite)
// valem como [From, To) sobre o instante da saída (ou do turno, para o total
// de contexto). ProjectIDs vazio = todos; o projeto de uma linha é o da
// janela dona (ctxProject): o do card da sessão da janela, senão o da sessão.
type ContextFilter struct {
	From, To   time.Time
	ProjectIDs []int64
}

type ContextKey struct {
	Tool      string `json:"tool"`
	Key       string `json:"key"`
	Count     int64  `json:"count"`
	Tokens    int64  `json:"tokens"`
	InTokens  int64  `json:"in_tokens"`
	AvgTokens int64  `json:"avg_tokens"` // (tokens + in_tokens) / count
	Weight    int64  `json:"weight"`
}

type ContextProject struct {
	ProjectID int64  `json:"project_id"`
	Weight    int64  `json:"weight"`
	TopTool   string `json:"top_tool"`
	TopKey    string `json:"top_key"` // chave de maior peso no projeto
}

type ContextTotals struct {
	ContextTotal int64 `json:"context_total"` // Σ context_turns.tokens com at no filtro (todos os paths)
	WeightTotal  int64 `json:"weight_total"`  // Σ pesos de todas as saídas no filtro
}

// ContextSession é uma janela de conversa (a principal ou uma filha) na
// lista de sessões; card, título e início são os da sessão da janela.
type ContextSession struct {
	SessionID   string  `json:"session_id"`
	CardKey     string  `json:"card_key"`
	CardTitle   string  `json:"card_title"`
	StartedAt   string  `json:"started_at"`
	PeakContext int64   `json:"peak_context"` // maior tokens de turno da janela no transcript principal
	PeakPct     float64 `json:"peak_pct"`     // maior % dos turnos só com % (agente sem tokens)
	Compactions int64   `json:"compactions"`  // compactações da janela (regra do medidor)
	Weight      int64   `json:"weight"`       // soma dos pesos das saídas da janela no filtro
}

type ContextOutput struct {
	Tool     string `json:"tool"`
	Key      string `json:"key"`
	Tokens   int64  `json:"tokens"`
	InTokens int64  `json:"in_tokens"`
	Rereads  int64  `json:"rereads"`
	Weight   int64  `json:"weight"`
	At       string `json:"at"`
	Subagent bool   `json:"subagent"` // path ≠ sessions.transcript_path
}

type ContextTurnPoint struct {
	Turn         int     `json:"turn"`
	At           string  `json:"at"`
	Tokens       int64   `json:"tokens"`
	AfterCompact bool    `json:"after_compact"` // compactação da própria janela desde o turno anterior
	Pct          float64 `json:"pct"`           // % da janela informada pelo agente (só com PctOnly)
	PctOnly      bool    `json:"pct_only"`      // só a % é conhecida (sem tokens)
}

// ctxOwner é a janela dona da linha de turno ou saída de alias a: a filha
// gravada em window_session_id, senão a sessão da linha (a principal).
func ctxOwner(a string) string {
	return `COALESCE(NULLIF(` + a + `.window_session_id,''), ` + a + `.session_id)`
}

// ctxJoins junta à linha de alias a a sessão gravada (s, card c) e a sessão
// da janela em window_session_id (ws, card wc; NULL na principal).
func ctxJoins(a string) string {
	return ` JOIN sessions s ON s.id = ` + a + `.session_id LEFT JOIN cards c ON c.id = s.card_id
	LEFT JOIN sessions ws ON ws.id = NULLIF(` + a + `.window_session_id,'') LEFT JOIN cards wc ON wc.id = ws.card_id`
}

// ctxProject é o projeto da janela dona (aliases de ctxJoins): o do card da
// sessão da janela, senão o da sessão.
const ctxProject = `CASE WHEN ws.id IS NULL THEN COALESCE(c.project_id, s.project_id) ELSE COALESCE(wc.project_id, ws.project_id) END`

// ctxOutputsCTE junta cada saída (já filtrada em base) à sua sessão/card e
// calcula releituras e peso. Releituras = MAX(turn) do mesmo (sessão, path,
// segmento) menos o turno da saída, mínimo 0 (0 se o segmento não tem turno).
// O máximo por segmento vem de um CTE agregado uma vez (só para as sessões
// presentes em base), não de um COUNT correlacionado por saída.
func ctxOutputsCTE(where string) string { return ctxOutputsCTEWin(where, "") }

// ctxOutputsCTEWin é ctxOutputsCTE com um filtro extra (ex.: " AND window_session_id = ?")
// nos turnos que dão o máximo por segmento, para a janela medir só os próprios turnos.
func ctxOutputsCTEWin(where, turnWhere string) string {
	return `WITH base AS (
	SELECT o.session_id, ` + ctxOwner("o") + ` AS owner, o.path, o.tool, o.key, o.tokens, o.in_tokens, o.at, o.turn, o.segment,
	       s.transcript_path, ` + ctxProject + ` AS project_id
	FROM tool_outputs o` + ctxJoins("o") + where + `
), seg_max AS (
	SELECT session_id, path, segment, MAX(turn) AS max_turn FROM context_turns
	WHERE session_id IN (SELECT session_id FROM base)` + turnWhere + ` GROUP BY session_id, path, segment
), outs AS (
	SELECT b.session_id, b.owner, b.path, b.tool, b.key, b.tokens, b.in_tokens, b.at, b.transcript_path, b.project_id,
	       MAX(0, COALESCE(m.max_turn, b.turn) - b.turn) AS rereads,
	       (b.tokens + b.in_tokens) * MAX(0, COALESCE(m.max_turn, b.turn) - b.turn) AS weight
	FROM base b
	LEFT JOIN seg_max m ON m.session_id = b.session_id AND m.path = b.path AND m.segment = b.segment
) `
}

// where monta o filtro sobre a coluna de tempo e o projeto (aliases do CTE).
func (f ContextFilter) where(timeCol, projectCol string) (string, []any) {
	q := ` WHERE 1=1`
	var args []any
	if !f.From.IsZero() {
		q += ` AND julianday(` + timeCol + `) >= julianday(?)`
		args = append(args, fmtTime(f.From))
	}
	if !f.To.IsZero() {
		q += ` AND julianday(` + timeCol + `) < julianday(?)`
		args = append(args, fmtTime(f.To))
	}
	if len(f.ProjectIDs) > 0 {
		q += ` AND ` + projectCol + ` IN (` + placeholders(len(f.ProjectIDs)) + `)`
		for _, id := range f.ProjectIDs {
			args = append(args, id)
		}
	}
	return q, args
}

// outsQuery devolve o CTE com o filtro aplicado: "…) SELECT … FROM outs".
func (f ContextFilter) outsQuery(sel, tail string) (string, []any) {
	w, args := f.where("o.at", ctxProject)
	return ctxOutputsCTE(w) + sel + ` FROM outs ` + tail, args
}

func sqlLimit(n int) int {
	if n <= 0 {
		return -1
	}
	return n
}

// ContextTopKeys ranqueia (tool, key) por peso e depois por tokens.
func (s *Store) ContextTopKeys(f ContextFilter, limit int) ([]ContextKey, error) {
	q, args := f.outsQuery(`SELECT tool, key, COUNT(*), SUM(tokens), SUM(in_tokens), SUM(weight)`,
		`GROUP BY tool, key ORDER BY SUM(weight) DESC, SUM(tokens) DESC, tool, key LIMIT ?`)
	rows, err := s.DB.Query(q, append(args, sqlLimit(limit))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextKey{}
	for rows.Next() {
		var k ContextKey
		if err := rows.Scan(&k.Tool, &k.Key, &k.Count, &k.Tokens, &k.InTokens, &k.Weight); err != nil {
			return nil, err
		}
		if k.Count > 0 {
			k.AvgTokens = (k.Tokens + k.InTokens) / k.Count
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ContextTopSessions ranqueia as janelas de conversa (a principal e cada
// filha, ctxOwner) por peso das saídas no filtro. Entra toda janela com
// saída ou turno no filtro (sessão só com %, sem saídas, entra com peso 0).
// Card, título e início vêm da sessão da janela; pico, pico em % e
// compactações, de windowStats (a vida toda da janela, não só o filtro).
func (s *Store) ContextTopSessions(f ContextFilter, limit int) ([]ContextSession, error) {
	ow, args := f.where("o.at", ctxProject)
	tw, targs := f.where("t.at", ctxProject)
	q := ctxOutputsCTE(ow) + `, tw AS (
		SELECT DISTINCT ` + ctxOwner("t") + ` AS owner FROM context_turns t` + ctxJoins("t") + tw + `
	), own AS (
		SELECT owner, weight FROM outs UNION ALL SELECT owner, 0 FROM tw
	), agg AS (
		SELECT owner, SUM(weight) AS weight FROM own GROUP BY owner
	)
	SELECT agg.owner, COALESCE(c.key,''), COALESCE(c.title,''), COALESCE(x.started_at,''), agg.weight
	FROM agg LEFT JOIN sessions x ON x.id = agg.owner LEFT JOIN cards c ON c.id = x.card_id
	ORDER BY agg.weight DESC, x.started_at DESC, agg.owner LIMIT ?`
	args = append(append(args, targs...), sqlLimit(limit))
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	out := []ContextSession{}
	for rows.Next() {
		var x ContextSession
		if err := rows.Scan(&x.SessionID, &x.CardKey, &x.CardTitle, &x.StartedAt, &x.Weight); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].PeakContext, out[i].PeakPct, out[i].Compactions, err = s.windowStats(out[i].SessionID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// windowStats devolve o maior tokens de turno, a maior % dos turnos só com %
// e as compactações da janela id no transcript principal (subagentes não
// contam). As linhas e as compactações da janela seguem o medidor ao vivo
// (windowScope, foreignCompactions e ownCompaction): compactação de outra
// janela (context_compactions) não conta; salto de segmento sem registro
// (agente que não grava a janela da compactação) conta. Janela sem sessão: zeros.
func (s *Store) windowStats(id string) (peak int64, peakPct float64, compactions int64, err error) {
	var parent, path string
	err = s.DB.QueryRow(`SELECT parent_session_id, transcript_path FROM sessions WHERE id=?`, id).Scan(&parent, &path)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, err
	}
	owner, path, where, args, err := s.windowScope(id, parent, path)
	if err != nil {
		return 0, 0, 0, err
	}
	foreign, err := s.foreignCompactions(owner, path, where, args)
	if err != nil {
		return 0, 0, 0, err
	}
	rows, err := s.DB.Query(`SELECT tokens, segment, pct FROM context_turns WHERE session_id=? AND path=? AND `+where+` ORDER BY turn`,
		append([]any{owner, path}, args...)...)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	prev := -1
	for rows.Next() {
		var tokens int64
		var seg int
		var pct sql.NullFloat64
		if err := rows.Scan(&tokens, &seg, &pct); err != nil {
			return 0, 0, 0, err
		}
		peak = max(peak, tokens)
		if pct.Valid && tokens == 0 {
			peakPct = max(peakPct, pct.Float64)
		}
		if prev >= 0 && ownCompaction(prev, seg, foreign) {
			compactions++
		}
		prev = seg
	}
	return peak, peakPct, compactions, rows.Err()
}

// ContextByProject soma o peso por projeto e acha a chave mais pesada de cada
// um. Sessões sem projeto ficam de fora.
func (s *Store) ContextByProject(f ContextFilter) ([]ContextProject, error) {
	q, args := f.outsQuery(`SELECT project_id, tool, key, SUM(weight)`,
		`WHERE project_id IS NOT NULL GROUP BY project_id, tool, key ORDER BY project_id, SUM(weight) DESC, tool, key`)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextProject{}
	idx := map[int64]int{}
	for rows.Next() {
		var pid, w int64
		var tool, key string
		if err := rows.Scan(&pid, &tool, &key, &w); err != nil {
			return nil, err
		}
		i, ok := idx[pid]
		if !ok {
			// A primeira (tool, key) do projeto é a de maior peso (ordem da consulta).
			idx[pid] = len(out)
			out = append(out, ContextProject{ProjectID: pid, TopTool: tool, TopKey: key})
			i = len(out) - 1
		}
		out[i].Weight += w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Peso desc; projeto como desempate, para ser determinístico.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].ProjectID < out[j].ProjectID
	})
	return out, nil
}

// ContextTotals soma o contexto dos turnos e o peso das saídas no filtro (o
// projeto de cada linha é o da janela dona).
func (s *Store) ContextTotals(f ContextFilter) (ContextTotals, error) {
	var t ContextTotals
	w, args := f.where("t.at", ctxProject)
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(t.tokens),0) FROM context_turns t`+ctxJoins("t")+w+` AND t.pct IS NULL`, args...).Scan(&t.ContextTotal)
	if err != nil {
		return t, err
	}
	q, args := f.outsQuery(`SELECT COALESCE(SUM(weight),0)`, ``)
	err = s.DB.QueryRow(q, args...).Scan(&t.WeightTotal)
	return t, err
}

// ContextSessionDetail devolve a curva de contexto do transcript principal e
// as saídas mais pesadas da sessão (principal e subagentes). Como no medidor
// ao vivo, a janela decide as linhas: a principal lê as suas (window_session_id
// ” ou o dela); a filha lê as gravadas sob a principal com o id da filha.
// AfterCompact marca só as compactações da própria janela (ownCompaction, a
// regra do medidor). Turnos só com % (agente sem tokens) saem com PctOnly e Pct.
func (s *Store) ContextSessionDetail(sessionID string, limit int) (turns []ContextTurnPoint, top []ContextOutput, err error) {
	var path, parent string
	if err = s.DB.QueryRow(`SELECT transcript_path, parent_session_id FROM sessions WHERE id=?`, sessionID).Scan(&path, &parent); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return nil, nil, err
	}
	owner, path, where, args, err := s.windowScope(sessionID, parent, path)
	if err != nil {
		return nil, nil, err
	}
	foreign, err := s.foreignCompactions(owner, path, where, args)
	if err != nil {
		return nil, nil, err
	}
	turns, top = []ContextTurnPoint{}, []ContextOutput{}
	rows, err := s.DB.Query(`SELECT turn, at, tokens, segment, pct FROM context_turns WHERE session_id=? AND path=? AND `+where+` ORDER BY turn`,
		append([]any{owner, path}, args...)...)
	if err != nil {
		return nil, nil, err
	}
	prev := -1
	for rows.Next() {
		var p ContextTurnPoint
		var seg int
		var pct sql.NullFloat64
		if err := rows.Scan(&p.Turn, &p.At, &p.Tokens, &seg, &pct); err != nil {
			rows.Close()
			return nil, nil, err
		}
		p.Pct, p.PctOnly = pct.Float64, pct.Valid && p.Tokens == 0
		p.AfterCompact = prev >= 0 && ownCompaction(prev, seg, foreign)
		prev = seg
		turns = append(turns, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	oargs := append([]any{owner}, args...)
	orows, err := s.DB.Query(ctxOutputsCTEWin(` WHERE o.session_id = ? AND o.`+where, ` AND `+where)+`
		SELECT tool, key, tokens, in_tokens, rereads, weight, at, path <> transcript_path FROM outs
		ORDER BY weight DESC, tokens DESC, at LIMIT ?`, append(append(oargs, args...), sqlLimit(limit))...)
	if err != nil {
		return nil, nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o ContextOutput
		if err := orows.Scan(&o.Tool, &o.Key, &o.Tokens, &o.InTokens, &o.Rereads, &o.Weight, &o.At, &o.Subagent); err != nil {
			return nil, nil, err
		}
		top = append(top, o)
	}
	return turns, top, orows.Err()
}
