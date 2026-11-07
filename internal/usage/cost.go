// Package usage lê tokens dos transcripts dos agentes e calcula o custo.
package usage

import "github.com/nobretech/nobreai-cockpit/internal/domain"

// Cost calcula o custo em USD da linha; sem preço devolve (0, false). Também
// fica sem preço a linha com tokens de cache cujo preço de cache é
// desconhecido (nil): nunca vira zero em silêncio.
func Cost(r domain.UsageRow, p *domain.Price) (float64, bool) {
	if p == nil {
		return 0, false
	}
	if (r.CacheWrite > 0 && p.CacheWritePerM == nil) || (r.CacheRead > 0 && p.CacheReadPerM == nil) {
		return 0, false
	}
	usd := float64(r.Input)*p.InputPerM + float64(r.Output)*p.OutputPerM
	if p.CacheWritePerM != nil {
		usd += float64(r.CacheWrite) * *p.CacheWritePerM
	}
	if p.CacheReadPerM != nil {
		usd += float64(r.CacheRead) * *p.CacheReadPerM
	}
	return usd / 1e6, true
}
