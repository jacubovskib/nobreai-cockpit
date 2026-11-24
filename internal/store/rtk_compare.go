package store

import (
	"strings"

	"github.com/nobretech/nobreai-cockpit/internal/rtk"
)

// RTKCompare compara o tamanho médio da saída de um mesmo comando antes e
// depois do RTK. Médias em tokens da saída.
type RTKCompare struct {
	Key         string  `json:"key"`
	BeforeCount int64   `json:"before_count"`
	BeforeAvg   int64   `json:"before_avg"`
	AfterCount  int64   `json:"after_count"`
	AfterAvg    int64   `json:"after_avg"`
	ChangePct   float64 `json:"change_pct"` // (depois − antes) / antes × 100
}

// RTKBeforeAfter compara as saídas Bash por chave. "Antes" = saídas com at
// anterior à primeira ativação; "depois" = saídas de sessões que começaram
// dentro de algum intervalo [on, off) (off nil = sem fim). Uma sessão que
// começou antes do on e teve saídas depois dele não conta como "depois".
// Só entram chaves com ao menos minEach saídas de cada lado, ordenadas por
// chamadas (antes + depois) desc, até limit linhas. Ignora período: usa todo
// o histórico, mas respeita o filtro de projeto (projectIDs vazio = todos).
func (s *Store) RTKBeforeAfter(projectIDs []int64, intervals []rtk.Interval, minEach, limit int) ([]RTKCompare, error) {
	out := []RTKCompare{}
	if len(intervals) == 0 {
		return out, nil
	}
	first := intervals[0].On
	for _, iv := range intervals {
		if iv.On.Before(first) {
			first = iv.On
		}
	}
	var after []string
	var args []any
	args = append(args, fmtTime(first)) // antes
	for _, iv := range intervals {
		c := `(julianday(s.started_at) >= julianday(?)`
		args = append(args, fmtTime(iv.On))
		if iv.Off != nil {
			c += ` AND julianday(s.started_at) < julianday(?)`
			args = append(args, fmtTime(*iv.Off))
		}
		after = append(after, c+`)`)
	}
	q := `SELECT key,
		SUM(is_before), COALESCE(SUM(is_before * tokens), 0),
		SUM(is_after), COALESCE(SUM(is_after * tokens), 0)
	FROM (
		SELECT o.key, o.tokens,
			CASE WHEN julianday(o.at) < julianday(?) THEN 1 ELSE 0 END AS is_before,
			CASE WHEN ` + strings.Join(after, ` OR `) + ` THEN 1 ELSE 0 END AS is_after
		FROM tool_outputs o
		JOIN sessions s ON s.id = o.session_id
		LEFT JOIN cards c ON c.id = s.card_id
		WHERE o.tool = 'Bash'`
	if len(projectIDs) > 0 {
		q += ` AND ` + sessionProjectSQL + ` IN (` + placeholders(len(projectIDs)) + `)`
		for _, id := range projectIDs {
			args = append(args, id)
		}
	}
	q += `) WHERE is_before + is_after > 0
	GROUP BY key
	HAVING SUM(is_before) >= ? AND SUM(is_after) >= ?
	ORDER BY SUM(is_before) + SUM(is_after) DESC, key
	LIMIT ?`
	args = append(args, minEach, minEach, sqlLimit(limit))
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c RTKCompare
		var bSum, aSum int64
		if err := rows.Scan(&c.Key, &c.BeforeCount, &bSum, &c.AfterCount, &aSum); err != nil {
			return nil, err
		}
		c.BeforeAvg = bSum / c.BeforeCount
		c.AfterAvg = aSum / c.AfterCount
		if c.BeforeAvg > 0 {
			c.ChangePct = float64(c.AfterAvg-c.BeforeAvg) / float64(c.BeforeAvg) * 100
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
