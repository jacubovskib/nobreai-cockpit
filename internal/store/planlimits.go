package store

import (
	"database/sql"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// planCoalesce: leitura da mesma janela e mesmo resets_at dentro deste prazo
// da anterior atualiza a linha em vez de inserir (no máximo ~1 linha/minuto).
// planRetention: por quanto tempo as leituras ficam.
const planRetention = 30 * 24 * time.Hour

const planCoalesce = 60 * time.Second

// RecordPlanLimits grava uma linha por janela presente, coalescendo com a
// última linha da janela quando tem menos de 60 s e o mesmo resets_at.
func (s *Store) RecordPlanLimits(r domain.PlanReading) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Retenção: leituras com mais de 30 dias saem a cada gravação.
	for _, name := range []string{domain.PlanFiveHour, domain.PlanSevenDay} {
		if _, err := tx.Exec(`DELETE FROM plan_limits WHERE window=? AND julianday(received_at) < julianday(?)`,
			name, fmtTime(r.At.Add(-planRetention))); err != nil {
			return err
		}
	}
	for _, w := range []struct {
		name string
		v    *domain.PlanWindow
	}{{domain.PlanFiveHour, r.FiveHour}, {domain.PlanSevenDay, r.SevenDay}} {
		if w.v == nil {
			continue
		}
		var resets any
		if w.v.ResetsAt != nil {
			resets = fmtTime(*w.v.ResetsAt)
		}
		at := fmtTime(r.At)
		var id int64
		err := tx.QueryRow(`SELECT id FROM plan_limits WHERE window=? AND resets_at IS ?
			AND id = (SELECT id FROM plan_limits WHERE window=? ORDER BY julianday(received_at) DESC, id DESC LIMIT 1)
			AND ABS(julianday(?) - julianday(received_at)) * 86400 < ?`,
			w.name, resets, w.name, at, planCoalesce.Seconds()).Scan(&id)
		switch err {
		case nil:
			_, err = tx.Exec(`UPDATE plan_limits SET used_pct=?, received_at=?, session_id=? WHERE id=?`,
				w.v.UsedPct, at, r.SessionID, id)
		case sql.ErrNoRows:
			_, err = tx.Exec(`INSERT INTO plan_limits(window, used_pct, resets_at, received_at, session_id) VALUES (?,?,?,?,?)`,
				w.name, w.v.UsedPct, resets, at, r.SessionID)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestPlanLimits devolve a leitura mais recente (por horário de recebimento)
// de cada janela que já teve leitura. Expired fica a cargo da API (Deps.Now).
func (s *Store) LatestPlanLimits() (map[string]domain.PlanWindowState, error) {
	out := map[string]domain.PlanWindowState{}
	for _, name := range []string{domain.PlanFiveHour, domain.PlanSevenDay} {
		var st domain.PlanWindowState
		var resets sql.NullString
		var recv string
		err := s.DB.QueryRow(`SELECT used_pct, resets_at, received_at FROM plan_limits WHERE window=?
			ORDER BY julianday(received_at) DESC, id DESC LIMIT 1`, name).Scan(&st.UsedPct, &resets, &recv)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		if st.ReceivedAt, err = time.Parse(time.RFC3339Nano, recv); err != nil {
			return nil, err
		}
		if resets.Valid {
			t, err := time.Parse(time.RFC3339Nano, resets.String)
			if err != nil {
				return nil, err
			}
			st.ResetsAt = &t
		}
		out[name] = st
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
