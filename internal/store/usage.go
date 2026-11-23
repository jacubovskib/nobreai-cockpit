package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// UpsertUsage grava as linhas de uso; a chave é (session_id, message_id). O
// card_id é o card da janela dona (WindowSessionID, senão a própria sessão) no
// momento da ingestão e nunca é sobrescrito por uma reingestão: o custo fica no
// card em que foi gasto. A janela também fica a da primeira gravação, junto
// com o card (uso gravado antes da atribuição por janela fica na principal).
func (s *Store) UpsertUsage(rows []domain.UsageRow) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		owner := r.SessionID
		if r.WindowSessionID != "" {
			owner = r.WindowSessionID
		}
		if _, err := tx.Exec(`INSERT INTO usage (session_id, card_id, message_id, model, input, output, cache_write, cache_read, at, window_session_id, credits)
			VALUES (?, (SELECT card_id FROM sessions WHERE id=?), ?,?,?,?,?,?,?,?,?)
			ON CONFLICT(session_id, message_id) DO UPDATE SET model=excluded.model, input=excluded.input,
			output=excluded.output, cache_write=excluded.cache_write, cache_read=excluded.cache_read, at=excluded.at, credits=excluded.credits,
			card_id=COALESCE(usage.card_id, excluded.card_id)`,
			r.SessionID, owner, r.MessageID, r.Model, r.Input, r.Output, r.CacheWrite, r.CacheRead, fmtTime(r.At), r.WindowSessionID, r.Credits); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetTranscriptOffset guarda até onde o transcript da sessão já foi lido.
func (s *Store) SetTranscriptOffset(sessionID string, off int64) error {
	_, err := s.DB.Exec(`UPDATE sessions SET transcript_offset=? WHERE id=?`, off, sessionID)
	return err
}

// FileOffset devolve até onde o transcript path da sessão (principal ou de
// subagente) já foi lido; 0 se nunca foi.
func (s *Store) FileOffset(sessionID, path string) (int64, error) {
	var off int64
	err := s.DB.QueryRow(`SELECT offset FROM transcript_offsets WHERE session_id=? AND path=?`, sessionID, path).Scan(&off)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return off, err
}

// SetFileOffset guarda até onde o transcript path da sessão já foi lido.
func (s *Store) SetFileOffset(sessionID, path string, off int64) error {
	_, err := s.DB.Exec(`INSERT INTO transcript_offsets(session_id, path, offset) VALUES (?,?,?)
		ON CONFLICT(session_id, path) DO UPDATE SET offset=excluded.offset`, sessionID, path, off)
	return err
}

// SetSessionModel grava o modelo usado pela sessão (vazio não sobrescreve).
func (s *Store) SetSessionModel(id, model string) error {
	if model == "" {
		return nil
	}
	_, err := s.DB.Exec(`UPDATE sessions SET model=? WHERE id=?`, model, id)
	return err
}

func (s *Store) ListPrices() ([]domain.Price, error) {
	rows, err := s.DB.Query(`SELECT model, input_per_m, output_per_m, cache_write_per_m, cache_read_per_m FROM prices ORDER BY model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Price
	for rows.Next() {
		var p domain.Price
		if err := rows.Scan(&p.Model, &p.InputPerM, &p.OutputPerM, &p.CacheWritePerM, &p.CacheReadPerM); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PutPrice(p domain.Price) error {
	_, err := s.DB.Exec(`INSERT INTO prices (model, input_per_m, output_per_m, cache_write_per_m, cache_read_per_m)
		VALUES (?,?,?,?,?) ON CONFLICT(model) DO UPDATE SET input_per_m=excluded.input_per_m,
		output_per_m=excluded.output_per_m, cache_write_per_m=excluded.cache_write_per_m, cache_read_per_m=excluded.cache_read_per_m`,
		p.Model, p.InputPerM, p.OutputPerM, p.CacheWritePerM, p.CacheReadPerM)
	return err
}

func (s *Store) DeletePrice(model string) error {
	_, err := s.DB.Exec(`DELETE FROM prices WHERE model=?`, model)
	return err
}

func (s *Store) ListLimits() ([]domain.Limit, error) {
	rows, err := s.DB.Query(`SELECT agent, daily_usd, card_usd, warn_pct, action FROM limits ORDER BY agent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Limit
	for rows.Next() {
		var l domain.Limit
		if err := rows.Scan(&l.Agent, &l.DailyUSD, &l.CardUSD, &l.WarnPct, &l.Action); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) PutLimit(l domain.Limit) error {
	_, err := s.DB.Exec(`INSERT INTO limits (agent, daily_usd, card_usd, warn_pct, action) VALUES (?,?,?,?,?)
		ON CONFLICT(agent) DO UPDATE SET daily_usd=excluded.daily_usd, card_usd=excluded.card_usd,
		warn_pct=excluded.warn_pct, action=excluded.action`,
		l.Agent, l.DailyUSD, l.CardUSD, l.WarnPct, l.Action)
	return err
}

// TotalsFilter filtra usage.at em [From, To); zero em qualquer ponta = sem
// limite. CardID (opcional) restringe ao uso gasto naquele card.
type TotalsFilter struct {
	From, To time.Time
	GroupBy  string // agent | card | session | day_agent | model | project
	CardID   *int64
	// ProjectID restringe ao uso do projeto: o do card do uso, senão o da sessão.
	ProjectID *int64
	// ProjectIDs vale junto com ProjectID, como IN (...).
	ProjectIDs []int64
}

type Total struct {
	Key        string  `json:"key"`
	Agent      string  `json:"agent"`
	Day        string  `json:"day"`
	Sessions   int     `json:"sessions"`
	Input      int64   `json:"input"`
	Output     int64   `json:"output"`
	CacheWrite int64   `json:"cache_write"`
	CacheRead  int64   `json:"cache_read"`
	CostUSD    float64 `json:"cost_usd"`
	// Credits é o gasto em créditos (Kiro), separado do custo em dólares.
	Credits  float64 `json:"credits"`
	Unpriced bool    `json:"unpriced"`
}

// Totals agrega o uso por grupo; o custo é calculado em Go (usage.Cost) com a
// tabela de preços, de modo que a regra "sem preço" fica num só lugar. O card
// de cada linha é o gravado no uso (usage.card_id), não o card atual da sessão;
// a sessão é a janela dona (window_session_id, senão session_id).
func (s *Store) Totals(f TotalsFilter) ([]Total, error) {
	switch f.GroupBy {
	case "agent", "card", "session", "day_agent", "model", "project":
	default:
		return nil, fmt.Errorf("agrupamento inválido: %q", f.GroupBy)
	}
	prices, err := s.ListPrices()
	if err != nil {
		return nil, err
	}
	pm := map[string]*domain.Price{}
	for i := range prices {
		pm[prices[i].Model] = &prices[i]
	}

	// A sessão de cada linha é a janela dona (a filha grava o uso com o id da
	// principal): por sessão, cada janela tem o seu custo e a sua contagem.
	q := `SELECT COALESCE(NULLIF(u.window_session_id,''), u.session_id), u.model, u.input, u.output, u.cache_write, u.cache_read, u.credits, u.at, s.agent, COALESCE(c.key, ''), COALESCE(c.project_id, s.project_id)
		FROM usage u JOIN sessions s ON s.id = u.session_id LEFT JOIN cards c ON c.id = u.card_id WHERE 1=1`
	var args []any
	if f.CardID != nil {
		q += ` AND u.card_id = ?`
		args = append(args, *f.CardID)
	}
	if f.ProjectID != nil {
		q += ` AND COALESCE(c.project_id, s.project_id) = ?`
		args = append(args, *f.ProjectID)
	}
	if len(f.ProjectIDs) > 0 {
		q += ` AND COALESCE(c.project_id, s.project_id) IN (` + placeholders(len(f.ProjectIDs)) + `)`
		for _, id := range f.ProjectIDs {
			args = append(args, id)
		}
	}
	if !f.From.IsZero() {
		q += ` AND julianday(u.at) >= julianday(?)`
		args = append(args, fmtTime(f.From))
	}
	if !f.To.IsZero() {
		q += ` AND julianday(u.at) < julianday(?)`
		args = append(args, fmtTime(f.To))
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := map[string]*Total{}
	sess := map[string]map[string]bool{}
	for rows.Next() {
		var r domain.UsageRow
		var at, agent, cardKey string
		var projectID sql.NullInt64
		if err := rows.Scan(&r.SessionID, &r.Model, &r.Input, &r.Output, &r.CacheWrite, &r.CacheRead, &r.Credits, &at, &agent, &cardKey, &projectID); err != nil {
			return nil, err
		}
		if r.At, err = time.Parse(time.RFC3339Nano, at); err != nil {
			return nil, err
		}
		g := Total{}
		switch f.GroupBy {
		case "agent":
			g.Key = agent
		case "card":
			g.Key = cardKey
		case "session":
			g.Key = r.SessionID
		case "model":
			g.Key = r.Model
		case "project":
			if projectID.Valid {
				g.Key = strconv.FormatInt(projectID.Int64, 10)
			}
		case "day_agent":
			g.Day, g.Agent = r.At.Local().Format("2006-01-02"), agent
			g.Key = g.Day + "|" + agent
		}
		t, ok := groups[g.Key]
		if !ok {
			t = &g
			groups[g.Key] = t
			sess[g.Key] = map[string]bool{}
		}
		sess[g.Key][r.SessionID] = true
		t.Input += r.Input
		t.Output += r.Output
		t.CacheWrite += r.CacheWrite
		t.CacheRead += r.CacheRead
		t.Credits += r.Credits
		if r.Credits > 0 && r.Input+r.Output+r.CacheWrite+r.CacheRead == 0 {
			continue // gasto só em créditos: não há o que precificar nem ficar "sem preço"
		}
		if usd, priced := usage.Cost(r, pm[r.Model]); priced {
			t.CostUSD += usd
		} else {
			t.Unpriced = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Total, 0, len(groups))
	for k, t := range groups {
		t.Sessions = len(sess[k])
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
