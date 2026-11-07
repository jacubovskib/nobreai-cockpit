package store

import (
	"database/sql"
	"sort"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// SetWhereStopped grava o "onde parei" do card e o instante da edição.
func (s *Store) SetWhereStopped(cardID int64, text string, at time.Time) error {
	res, err := s.DB.Exec(`UPDATE cards SET where_stopped = ?, where_stopped_at = ?, updated_at = ? WHERE id = ?`,
		text, fmtTime(at), fmtTime(time.Now()), cardID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (s *Store) AddDecision(d domain.Decision) (domain.Decision, error) {
	if d.At.IsZero() {
		d.At = time.Now().UTC()
	}
	res, err := s.DB.Exec(`INSERT INTO decisions(card_id, what, why, session_id, at) VALUES (?, ?, ?, ?, ?)`,
		d.CardID, d.What, d.Why, d.SessionID, fmtTime(d.At))
	if err != nil {
		return d, err
	}
	d.ID, err = res.LastInsertId()
	return d, err
}

// UpsertEssentialFile insere o arquivo ou, se o path já existir no card, atualiza lines e why.
func (s *Store) UpsertEssentialFile(f domain.EssentialFile) error {
	_, err := s.DB.Exec(`INSERT INTO essential_files(card_id, path, lines, why) VALUES (?, ?, ?, ?)
		ON CONFLICT(card_id, path) DO UPDATE SET lines = excluded.lines, why = excluded.why`,
		f.CardID, f.Path, f.Lines, f.Why)
	return err
}

func (s *Store) AddTodo(t domain.Todo) (domain.Todo, error) {
	res, err := s.DB.Exec(`INSERT INTO todos(card_id, text, done, session_id) VALUES (?, ?, ?, ?)`,
		t.CardID, t.Text, t.Done, t.SessionID)
	if err != nil {
		return t, err
	}
	t.ID, err = res.LastInsertId()
	return t, err
}

func (s *Store) SetTodoDone(id int64, done bool) error {
	res, err := s.DB.Exec(`UPDATE todos SET done = ? WHERE id = ?`, done, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

func (s *Store) AddQuestion(q domain.Question) (domain.Question, error) {
	res, err := s.DB.Exec(`INSERT INTO questions(card_id, text, answer, session_id) VALUES (?, ?, ?, ?)`,
		q.CardID, q.Text, q.Answer, q.SessionID)
	if err != nil {
		return q, err
	}
	q.ID, err = res.LastInsertId()
	return q, err
}

func (s *Store) AnswerQuestion(id int64, answer string) error {
	res, err := s.DB.Exec(`UPDATE questions SET answer = ? WHERE id = ?`, answer, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// CardContent devolve decisões, arquivos essenciais, pendências e perguntas do card, em ordem de criação.
func (s *Store) CardContent(cardID int64) (domain.Content, error) {
	var ct domain.Content

	rows, err := s.DB.Query(`SELECT id, card_id, what, why, session_id, at FROM decisions WHERE card_id = ? ORDER BY id`, cardID)
	if err != nil {
		return ct, err
	}
	for rows.Next() {
		var d domain.Decision
		var at string
		if err := rows.Scan(&d.ID, &d.CardID, &d.What, &d.Why, &d.SessionID, &at); err != nil {
			rows.Close()
			return ct, err
		}
		if d.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			rows.Close()
			return ct, err
		}
		ct.Decisions = append(ct.Decisions, d)
	}
	if err := closeRows(rows); err != nil {
		return ct, err
	}

	rows, err = s.DB.Query(`SELECT id, card_id, path, lines, why FROM essential_files WHERE card_id = ? ORDER BY id`, cardID)
	if err != nil {
		return ct, err
	}
	for rows.Next() {
		var f domain.EssentialFile
		if err := rows.Scan(&f.ID, &f.CardID, &f.Path, &f.Lines, &f.Why); err != nil {
			rows.Close()
			return ct, err
		}
		ct.Files = append(ct.Files, f)
	}
	if err := closeRows(rows); err != nil {
		return ct, err
	}

	rows, err = s.DB.Query(`SELECT id, card_id, text, done, session_id FROM todos WHERE card_id = ? ORDER BY id`, cardID)
	if err != nil {
		return ct, err
	}
	for rows.Next() {
		var t domain.Todo
		if err := rows.Scan(&t.ID, &t.CardID, &t.Text, &t.Done, &t.SessionID); err != nil {
			rows.Close()
			return ct, err
		}
		ct.Todos = append(ct.Todos, t)
	}
	if err := closeRows(rows); err != nil {
		return ct, err
	}

	rows, err = s.DB.Query(`SELECT id, card_id, text, answer, session_id FROM questions WHERE card_id = ? ORDER BY id`, cardID)
	if err != nil {
		return ct, err
	}
	for rows.Next() {
		var q domain.Question
		if err := rows.Scan(&q.ID, &q.CardID, &q.Text, &q.Answer, &q.SessionID); err != nil {
			rows.Close()
			return ct, err
		}
		ct.Questions = append(ct.Questions, q)
	}
	return ct, closeRows(rows)
}

func closeRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	return rows.Close()
}

// LastEventAt devolve o instante do evento mais recente do tipo kind no card, ou nil se não houver.
func (s *Store) LastEventAt(cardID int64, kind string) (*time.Time, error) {
	var at sql.NullString
	err := s.DB.QueryRow(`SELECT at FROM events WHERE card_id = ? AND kind = ? ORDER BY julianday(at) DESC LIMIT 1`, cardID, kind).Scan(&at)
	if err == sql.ErrNoRows || (err == nil && !at.Valid) {
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

// TouchedFile é um arquivo lido ou editado pelas sessões do card.
type TouchedFile struct {
	Path   string    `json:"path"`
	Kind   string    `json:"kind"` // "editado" ou "lido"
	Count  int       `json:"count"`
	LastAt time.Time `json:"last_at"`
}

// TouchedFiles agrega os eventos file_edit/file_read do card por caminho. Um
// caminho lido e editado conta como "editado". Ordem: editados primeiro,
// depois o toque mais recente primeiro.
func (s *Store) TouchedFiles(cardID int64) ([]TouchedFile, error) {
	rows, err := s.DB.Query(`SELECT COALESCE(json_extract(data, '$.path'), ''), kind, at FROM events
		WHERE card_id = ? AND kind IN (?, ?)`, cardID, domain.EvFileEdit, domain.EvFileRead)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byPath := map[string]*TouchedFile{}
	for rows.Next() {
		var path, kind, at string
		if err := rows.Scan(&path, &kind, &at); err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		f, ok := byPath[path]
		if !ok {
			f = &TouchedFile{Path: path, Kind: "lido", LastAt: t}
			byPath[path] = f
		}
		f.Count++
		if kind == domain.EvFileEdit {
			f.Kind = "editado"
		}
		if t.After(f.LastAt) {
			f.LastAt = t
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TouchedFile, 0, len(byPath))
	for _, f := range byPath {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Kind == "editado") != (b.Kind == "editado") {
			return a.Kind == "editado"
		}
		if !a.LastAt.Equal(b.LastAt) {
			return a.LastAt.After(b.LastAt)
		}
		return a.Path < b.Path
	})
	return out, nil
}
