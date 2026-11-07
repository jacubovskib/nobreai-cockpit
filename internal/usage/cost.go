// Package usage lê tokens dos transcripts dos agentes e calcula o custo.
package usage

import "github.com/nobretech/nobreai-cockpit/internal/domain"

// Cost calcula o custo em USD da linha; sem preço devolve (0, false).
func Cost(r domain.UsageRow, p *domain.Price) (float64, bool) {
	if p == nil {
		return 0, false
	}
	usd := (float64(r.Input)*p.InputPerM + float64(r.Output)*p.OutputPerM +
		float64(r.CacheWrite)*p.CacheWritePerM + float64(r.CacheRead)*p.CacheReadPerM) / 1e6
	return usd, true
}
