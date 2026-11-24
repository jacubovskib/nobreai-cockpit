package store

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// ErrInvalid marca entrada inválida vinda do chamador (vira 400 na API).
var ErrInvalid = errors.New("entrada inválida")

// SetPlan registra o plano do card: o arquivo e a lista de títulos. Cada título reaproveita a primeira
// tarefa antiga de mesmo título ainda não usada (com estado, sessão e data); os novos entram como a_fazer
// em at; as antigas que sobram somem. A numeração segue a nova ordem, de 1 em diante.
func (s *Store) SetPlan(cardID int64, path string, titles []string, sessionID string, at time.Time) (domain.Plan, error) {
	if len(titles) == 0 {
		return domain.Plan{}, fmt.Errorf("%w: o plano precisa de ao menos uma tarefa", ErrInvalid)
	}
	titles = slices.Clone(titles)
	for i, t := range titles {
		titles[i] = strings.TrimSpace(t)
		if titles[i] == "" {
			return domain.Plan{}, fmt.Errorf("%w: título de tarefa vazio", ErrInvalid)
		}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return domain.Plan{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM cards WHERE id = ?`, cardID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return domain.Plan{}, ErrNotFound
	} else if err != nil {
		return domain.Plan{}, err
	}
	old, err := queryTasks(tx, cardID)
	if err != nil {
		return domain.Plan{}, err
	}
	used := make([]bool, len(old))
	next := make([]domain.PlanTask, len(titles))
	for i, title := range titles {
		nt := domain.PlanTask{N: i + 1, Title: title, Status: domain.TaskTodo, UpdatedAt: at.UTC()}
		for j, o := range old {
			if !used[j] && o.Title == title {
				used[j] = true
				nt.Status, nt.SessionID, nt.UpdatedAt = o.Status, o.SessionID, o.UpdatedAt
				break
			}
		}
		next[i] = nt
	}
	if _, err := tx.Exec(`DELETE FROM plan_tasks WHERE card_id = ?`, cardID); err != nil {
		return domain.Plan{}, err
	}
	for _, t := range next {
		if _, err := tx.Exec(`INSERT INTO plan_tasks(card_id, n, title, status, session_id, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			cardID, t.N, t.Title, string(t.Status), t.SessionID, fmtTime(t.UpdatedAt)); err != nil {
			return domain.Plan{}, err
		}
	}
	if _, err := tx.Exec(`UPDATE cards SET plan_path = ?, plan_updated_at = ? WHERE id = ?`, path, fmtTime(at), cardID); err != nil {
		return domain.Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Plan{}, err
	}
	return domain.Plan{Path: path, Tasks: next}, nil
}

// SetTaskStatus muda o estado da tarefa n do card (ErrNotFound se não existe).
func (s *Store) SetTaskStatus(cardID int64, n int, st domain.TaskStatus, sessionID string, at time.Time) (domain.PlanTask, error) {
	res, err := s.DB.Exec(`UPDATE plan_tasks SET status = ?, session_id = ?, updated_at = ? WHERE card_id = ? AND n = ?`,
		string(st), sessionID, fmtTime(at), cardID, n)
	if err != nil {
		return domain.PlanTask{}, err
	}
	if err := requireAffected(res); err != nil {
		return domain.PlanTask{}, err
	}
	ts, err := queryTasks(s.DB, cardID)
	if err != nil {
		return domain.PlanTask{}, err
	}
	for _, t := range ts {
		if t.N == n {
			return t, nil
		}
	}
	return domain.PlanTask{}, ErrNotFound
}

// LastPlanChange devolve o maior entre plan_updated_at e os updated_at das tarefas; nil sem plano.
func (s *Store) LastPlanChange(cardID int64) (*time.Time, error) {
	var at sql.NullString
	err := s.DB.QueryRow(`SELECT t FROM (
			SELECT plan_updated_at AS t FROM cards WHERE id = ?
			UNION ALL SELECT updated_at FROM plan_tasks WHERE card_id = ?)
		WHERE t IS NOT NULL ORDER BY julianday(t) DESC LIMIT 1`, cardID, cardID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !at.Valid) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339Nano, at.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// cardPlan devolve o plano do card, ou nil quando não tem tarefa.
func (s *Store) cardPlan(cardID int64) (*domain.Plan, error) {
	ts, err := queryTasks(s.DB, cardID)
	if err != nil || len(ts) == 0 {
		return nil, err
	}
	var path string
	if err := s.DB.QueryRow(`SELECT plan_path FROM cards WHERE id = ?`, cardID).Scan(&path); err != nil {
		return nil, err
	}
	return &domain.Plan{Path: path, Tasks: ts}, nil
}

func queryTasks(q querier, cardID int64) ([]domain.PlanTask, error) {
	rows, err := q.Query(`SELECT n, title, status, session_id, updated_at FROM plan_tasks WHERE card_id = ? ORDER BY n`, cardID)
	if err != nil {
		return nil, err
	}
	var out []domain.PlanTask
	for rows.Next() {
		var t domain.PlanTask
		var st, at string
		if err := rows.Scan(&t.N, &t.Title, &st, &t.SessionID, &at); err != nil {
			rows.Close()
			return nil, err
		}
		t.Status = domain.TaskStatus(st)
		if t.UpdatedAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	return out, closeRows(rows)
}
