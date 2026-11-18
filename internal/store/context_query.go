package store

import (
	"database/sql"
	"errors"
	"time"
)

// ContextFilter limita as consultas de peso. From/To (zero = sem limite)
// valem como [From, To) sobre o instante da saída (ou do turno, para o total
// de contexto). ProjectIDs vazio = todos; o projeto de uma linha é o do card
// da sessão, senão o da sessão (como em Totals).
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
	TopKey    string `json:"top_key"` // chave de maior peso no projeto
}

type ContextTotals struct {
	ContextTotal int64 `json:"context_total"` // Σ context_turns.tokens com at no filtro (todos os paths)
	WeightTotal  int64 `json:"weight_total"`  // Σ pesos de todas as saídas no filtro
}

type ContextSession struct {
	SessionID   string `json:"session_id"`
	CardKey     string `json:"card_key"`
	CardTitle   string `json:"card_title"`
	StartedAt   string `json:"started_at"`
	PeakContext int64  `json:"peak_context"` // maior tokens de turno do transcript principal
	Compactions int64  `json:"compactions"`  // MAX(segment) do principal (turnos e saídas)
	Weight      int64  `json:"weight"`       // soma dos pesos das saídas no filtro
}

type ContextOutput struct {
	Tool     string `json:"tool"`
	Key      string `json:"key"`
	Tokens   int64  `json:"tokens"`
	Rereads  int64  `json:"rereads"`
	Weight   int64  `json:"weight"`
	At       string `json:"at"`
	Subagent bool   `json:"subagent"` // path ≠ sessions.transcript_path
}

type ContextTurnPoint struct {
	Turn         int    `json:"turn"`
	At           string `json:"at"`
	Tokens       int64  `json:"tokens"`
	AfterCompact bool   `json:"after_compact"` // segmento maior que o do turno anterior
}

// ctxOutputsCTE junta cada saída à sua sessão/card e calcula releituras e peso.
// Releituras = MAX(turn) do mesmo (sessão, path, segmento) menos o turno da
// saída, mínimo 0 (0 se o segmento não tem turno). O máximo por segmento vem
// de um CTE agregado uma vez, não de um COUNT correlacionado por saída.
const ctxOutputsCTE = `WITH seg_max AS (
	SELECT session_id, path, segment, MAX(turn) AS max_turn FROM context_turns GROUP BY session_id, path, segment
), outs AS (
	SELECT o.session_id, o.path, o.tool, o.key, o.tokens, o.in_tokens, o.at, s.transcript_path,
	       COALESCE(c.project_id, s.project_id) AS project_id,
	       MAX(0, COALESCE(m.max_turn, o.turn) - o.turn) AS rereads,
	       (o.tokens + o.in_tokens) * MAX(0, COALESCE(m.max_turn, o.turn) - o.turn) AS weight
	FROM tool_outputs o
	JOIN sessions s ON s.id = o.session_id
	LEFT JOIN cards c ON c.id = s.card_id
	LEFT JOIN seg_max m ON m.session_id = o.session_id AND m.path = o.path AND m.segment = o.segment
`

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
	w, args := f.where("o.at", "COALESCE(c.project_id, s.project_id)")
	return ctxOutputsCTE + w + `) ` + sel + ` FROM outs ` + tail, args
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

// ContextTopSessions ranqueia as sessões por peso das saídas no filtro. Pico e
// compactações vêm só do transcript principal.
func (s *Store) ContextTopSessions(f ContextFilter, limit int) ([]ContextSession, error) {
	q, args := f.outsQuery(`SELECT outs.session_id, COALESCE(c.key,''), COALESCE(c.title,''), s.started_at,
		COALESCE((SELECT MAX(t.tokens) FROM context_turns t WHERE t.session_id=outs.session_id AND t.path=s.transcript_path), 0),
		MAX(COALESCE((SELECT MAX(t.segment) FROM context_turns t WHERE t.session_id=outs.session_id AND t.path=s.transcript_path), 0),
		    COALESCE((SELECT MAX(x.segment) FROM tool_outputs x WHERE x.session_id=outs.session_id AND x.path=s.transcript_path), 0)),
		SUM(outs.weight)`,
		`JOIN sessions s ON s.id = outs.session_id LEFT JOIN cards c ON c.id = s.card_id
		GROUP BY outs.session_id ORDER BY SUM(outs.weight) DESC, s.started_at DESC, outs.session_id LIMIT ?`)
	rows, err := s.DB.Query(q, append(args, sqlLimit(limit))...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextSession{}
	for rows.Next() {
		var x ContextSession
		if err := rows.Scan(&x.SessionID, &x.CardKey, &x.CardTitle, &x.StartedAt, &x.PeakContext, &x.Compactions, &x.Weight); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ContextByProject soma o peso por projeto e acha a chave mais pesada de cada
// um. Sessões sem projeto ficam de fora.
func (s *Store) ContextByProject(f ContextFilter) ([]ContextProject, error) {
	q, args := f.outsQuery(`SELECT project_id, key, SUM(weight)`,
		`WHERE project_id IS NOT NULL GROUP BY project_id, key ORDER BY project_id, SUM(weight) DESC, key`)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ContextProject{}
	idx := map[int64]int{}
	for rows.Next() {
		var pid, w int64
		var key string
		if err := rows.Scan(&pid, &key, &w); err != nil {
			return nil, err
		}
		i, ok := idx[pid]
		if !ok {
			// A primeira chave do projeto é a de maior peso (ordem da consulta).
			idx[pid] = len(out)
			out = append(out, ContextProject{ProjectID: pid, TopKey: key})
			i = len(out) - 1
		}
		out[i].Weight += w
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Ordena por peso desc (projeto como desempate, para ser determinístico).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].Weight > out[j-1].Weight ||
			(out[j].Weight == out[j-1].Weight && out[j].ProjectID < out[j-1].ProjectID)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// ContextTotals soma o contexto dos turnos e o peso das saídas no filtro.
func (s *Store) ContextTotals(f ContextFilter) (ContextTotals, error) {
	var t ContextTotals
	w, args := f.where("t.at", "COALESCE(c.project_id, s.project_id)")
	err := s.DB.QueryRow(`SELECT COALESCE(SUM(t.tokens),0) FROM context_turns t
		JOIN sessions s ON s.id = t.session_id LEFT JOIN cards c ON c.id = s.card_id`+w, args...).Scan(&t.ContextTotal)
	if err != nil {
		return t, err
	}
	q, args := f.outsQuery(`SELECT COALESCE(SUM(weight),0)`, ``)
	err = s.DB.QueryRow(q, args...).Scan(&t.WeightTotal)
	return t, err
}

// ContextSessionDetail devolve a curva de contexto do transcript principal e
// as saídas mais pesadas da sessão (principal e subagentes).
func (s *Store) ContextSessionDetail(sessionID string, limit int) (turns []ContextTurnPoint, top []ContextOutput, err error) {
	var path string
	if err = s.DB.QueryRow(`SELECT transcript_path FROM sessions WHERE id=?`, sessionID).Scan(&path); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return nil, nil, err
	}
	turns, top = []ContextTurnPoint{}, []ContextOutput{}
	rows, err := s.DB.Query(`SELECT turn, at, tokens, segment FROM context_turns WHERE session_id=? AND path=? ORDER BY turn`, sessionID, path)
	if err != nil {
		return nil, nil, err
	}
	prev := 0
	for rows.Next() {
		var p ContextTurnPoint
		var seg int
		if err := rows.Scan(&p.Turn, &p.At, &p.Tokens, &seg); err != nil {
			rows.Close()
			return nil, nil, err
		}
		p.AfterCompact = seg > prev
		prev = seg
		turns = append(turns, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	orows, err := s.DB.Query(ctxOutputsCTE+` WHERE o.session_id = ?)
		SELECT tool, key, tokens, rereads, weight, at, path <> transcript_path FROM outs
		ORDER BY weight DESC, tokens DESC, at LIMIT ?`, sessionID, sqlLimit(limit))
	if err != nil {
		return nil, nil, err
	}
	defer orows.Close()
	for orows.Next() {
		var o ContextOutput
		if err := orows.Scan(&o.Tool, &o.Key, &o.Tokens, &o.Rereads, &o.Weight, &o.At, &o.Subagent); err != nil {
			return nil, nil, err
		}
		top = append(top, o)
	}
	return turns, top, orows.Err()
}
