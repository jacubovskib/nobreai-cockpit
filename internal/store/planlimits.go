package store

import (
	"database/sql"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// planRetention: por quanto tempo as leituras ficam.
const planRetention = 30 * 24 * time.Hour

// planVigenteOrder escolhe a leitura vigente de uma janela: a de resets_at mais
// novo; com o mesmo resets_at, a de maior used_pct; resets_at nulo só vale se
// nenhuma tiver resets_at, e aí vale a mais recente (received_at). Sessões
// ociosas reenviam o rate_limits cacheado (refreshInterval): "a última
// recebida" alternaria entre elas.
const planVigenteOrder = `ORDER BY resets_at IS NULL, julianday(resets_at) DESC,
	CASE WHEN resets_at IS NULL THEN NULL ELSE used_pct END DESC,
	julianday(received_at) DESC, id DESC LIMIT 1`

// RecordPlanLimits grava as janelas presentes em r e diz se a leitura vigente
// de alguma janela mudou (used_pct ou resets_at). r.At é o "agora" de quem
// grava. Por janela:
//   - descarta (sem gravar) a leitura com resets_at mais antigo que o da
//     vigente ainda não expirada, ou com o mesmo resets_at e used_pct menor que
//     o máximo já visto para ele;
//   - coalesce por minuto do relógio (UTC): no máximo uma linha por janela,
//     minuto e resets_at, com o maior used_pct (nunca reduz) e o received_at
//     mais recente.
func (s *Store) RecordPlanLimits(r domain.PlanReading) (changed bool, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Retenção: leituras com mais de 30 dias saem a cada gravação.
	for _, name := range []string{domain.PlanFiveHour, domain.PlanSevenDay} {
		if _, err := tx.Exec(`DELETE FROM plan_limits WHERE window=? AND julianday(received_at) < julianday(?)`,
			name, fmtTime(r.At.Add(-planRetention))); err != nil {
			return false, err
		}
	}
	at := fmtTime(r.At)
	minute := r.At.UTC().Format("2006-01-02T15:04")
	for _, w := range []struct {
		name string
		v    *domain.PlanWindow
	}{{domain.PlanFiveHour, r.FiveHour}, {domain.PlanSevenDay, r.SevenDay}} {
		if w.v == nil {
			continue
		}
		before, ok, err := vigentePlan(tx, w.name)
		if err != nil {
			return false, err
		}
		var resets any
		if w.v.ResetsAt != nil {
			resets = fmtTime(*w.v.ResetsAt)
			if ok && before.ResetsAt != nil && before.ResetsAt.After(r.At) && w.v.ResetsAt.Before(*before.ResetsAt) {
				continue // sessão parada numa janela que já passou
			}
			var max sql.NullFloat64
			if err := tx.QueryRow(`SELECT MAX(used_pct) FROM plan_limits WHERE window=? AND julianday(resets_at) = julianday(?)`,
				w.name, resets).Scan(&max); err != nil {
				return false, err
			}
			if max.Valid && w.v.UsedPct < max.Float64 {
				continue // leitura cacheada de uma sessão ociosa
			}
		}
		res, err := tx.Exec(`UPDATE plan_limits SET used_pct=MAX(used_pct, ?), received_at=?, session_id=?
			WHERE window=? AND resets_at IS ? AND strftime('%Y-%m-%dT%H:%M', received_at) = ?`,
			w.v.UsedPct, at, r.SessionID, w.name, resets, minute)
		if err != nil {
			return false, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			if _, err := tx.Exec(`INSERT INTO plan_limits(window, used_pct, resets_at, received_at, session_id) VALUES (?,?,?,?,?)`,
				w.name, w.v.UsedPct, resets, at, r.SessionID); err != nil {
				return false, err
			}
		}
		after, _, err := vigentePlan(tx, w.name)
		if err != nil {
			return false, err
		}
		if !ok || after.UsedPct != before.UsedPct || !sameTime(after.ResetsAt, before.ResetsAt) {
			changed = true
		}
	}
	return changed, tx.Commit()
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

// queryRower é *sql.DB ou *sql.Tx.
type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

// vigentePlan lê a leitura vigente da janela (ver planVigenteOrder).
func vigentePlan(q queryRower, window string) (domain.PlanWindowState, bool, error) {
	var st domain.PlanWindowState
	var resets sql.NullString
	var recv string
	err := q.QueryRow(`SELECT used_pct, resets_at, received_at FROM plan_limits WHERE window=? `+planVigenteOrder,
		window).Scan(&st.UsedPct, &resets, &recv)
	if err == sql.ErrNoRows {
		return st, false, nil
	}
	if err != nil {
		return st, false, err
	}
	if st.ReceivedAt, err = time.Parse(time.RFC3339Nano, recv); err != nil {
		return st, false, err
	}
	if resets.Valid {
		t, err := time.Parse(time.RFC3339Nano, resets.String)
		if err != nil {
			return st, false, err
		}
		st.ResetsAt = &t
	}
	return st, true, nil
}

// LatestPlanLimits devolve a leitura vigente de cada janela que já teve
// leitura (ver planVigenteOrder). Expired fica a cargo da API (Deps.Now).
func (s *Store) LatestPlanLimits() (map[string]domain.PlanWindowState, error) {
	out := map[string]domain.PlanWindowState{}
	for _, name := range []string{domain.PlanFiveHour, domain.PlanSevenDay} {
		st, ok, err := vigentePlan(s.DB, name)
		if err != nil {
			return nil, err
		}
		if ok {
			out[name] = st
		}
	}
	return out, nil
}

// PlanLimitsHistory devolve os pontos da janela recebidos desde since, do mais
// antigo ao mais novo.
func (s *Store) PlanLimitsHistory(window string, since time.Time) ([]domain.PlanPoint, error) {
	rows, err := s.DB.Query(`SELECT received_at, used_pct FROM plan_limits
		WHERE window=? AND julianday(received_at) >= julianday(?)
		ORDER BY julianday(received_at), id`, window, fmtTime(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pts := []domain.PlanPoint{}
	for rows.Next() {
		var p domain.PlanPoint
		var at string
		if err := rows.Scan(&at, &p.UsedPct); err != nil {
			return nil, err
		}
		if p.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		pts = append(pts, p)
	}
	return pts, rows.Err()
}
