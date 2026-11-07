package store

import (
	"fmt"
	"sort"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// UpsertUsage grava as linhas de uso; a chave é (session_id, message_id).
func (s *Store) UpsertUsage(rows []domain.UsageRow) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO usage (session_id, message_id, model, input, output, cache_write, cache_read, at)
			VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(session_id, message_id) DO UPDATE SET model=excluded.model, input=excluded.input,
			output=excluded.output, cache_write=excluded.cache_write, cache_read=excluded.cache_read, at=excluded.at`,
			r.SessionID, r.MessageID, r.Model, r.Input, r.Output, r.CacheWrite, r.CacheRead, fmtTime(r.At)); err != nil {
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

// TotalsFilter filtra usage.at em [From, To); zero em qualquer ponta = sem limite.
type TotalsFilter struct {
	From, To time.Time
	GroupBy  string // agent | card | session | day_agent | model
}

type Total struct {
	Key                                  string
	Agent                                string
	Day                                  string
	Sessions                             int
	Input, Output, CacheWrite, CacheRead int64
	CostUSD                              float64
	Unpriced                             bool
}

// Totals agrega o uso por grupo; o custo é calculado em Go (usage.Cost) com a
// tabela de preços, de modo que a regra "sem preço" fica num só lugar.
func (s *Store) Totals(f TotalsFilter) ([]Total, error) {
	switch f.GroupBy {
	case "agent", "card", "session", "day_agent", "model":
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

	q := `SELECT u.session_id, u.model, u.input, u.output, u.cache_write, u.cache_read, u.at, s.agent, COALESCE(c.key, '')
		FROM usage u JOIN sessions s ON s.id = u.session_id LEFT JOIN cards c ON c.id = s.card_id WHERE 1=1`
	var args []any
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
		if err := rows.Scan(&r.SessionID, &r.Model, &r.Input, &r.Output, &r.CacheWrite, &r.CacheRead, &at, &agent, &cardKey); err != nil {
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
