package usage

import (
	"math"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func f(v float64) *float64 { return &v }

func TestCost(t *testing.T) {
	p := &domain.Price{Model: "m", InputPerM: 3, OutputPerM: 15, CacheWritePerM: f(3.75), CacheReadPerM: f(0.3)}
	r := domain.UsageRow{Input: 1_000_000, Output: 100_000, CacheWrite: 0, CacheRead: 2_000_000}
	usd, ok := Cost(r, p)
	if !ok || math.Abs(usd-5.10) > 1e-9 {
		t.Fatalf("got %v %v", usd, ok)
	}
	if usd, ok := Cost(r, nil); ok || usd != 0 {
		t.Fatalf("nil price: %v %v", usd, ok)
	}
}

// Preço de cache em branco (nil) é desconhecido: linha com tokens de cache fica
// sem preço. 0 explícito é grátis.
func TestCostCacheUnknown(t *testing.T) {
	base := domain.Price{Model: "m", InputPerM: 1, OutputPerM: 1}
	cases := []struct {
		name   string
		cw, cr *float64
		row    domain.UsageRow
		priced bool
	}{
		{"sem tokens de cache", nil, nil, domain.UsageRow{Input: 10}, true},
		{"leitura sem preço", f(1), nil, domain.UsageRow{CacheRead: 10}, false},
		{"escrita sem preço", nil, f(1), domain.UsageRow{CacheWrite: 10}, false},
		{"leitura grátis", nil, f(0), domain.UsageRow{CacheRead: 10}, true},
		{"escrita grátis", f(0), nil, domain.UsageRow{CacheWrite: 10}, true},
	}
	for _, c := range cases {
		p := base
		p.CacheWritePerM, p.CacheReadPerM = c.cw, c.cr
		if _, ok := Cost(c.row, &p); ok != c.priced {
			t.Errorf("%s: priced = %v, want %v", c.name, ok, c.priced)
		}
	}
}
