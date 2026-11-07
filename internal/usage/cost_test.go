package usage

import (
	"math"
	"testing"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

func TestCost(t *testing.T) {
	p := &domain.Price{Model: "m", InputPerM: 3, OutputPerM: 15, CacheWritePerM: 3.75, CacheReadPerM: 0.3}
	r := domain.UsageRow{Input: 1_000_000, Output: 100_000, CacheWrite: 0, CacheRead: 2_000_000}
	usd, ok := Cost(r, p)
	if !ok || math.Abs(usd-5.10) > 1e-9 {
		t.Fatalf("got %v %v", usd, ok)
	}
	if usd, ok := Cost(r, nil); ok || usd != 0 {
		t.Fatalf("nil price: %v %v", usd, ok)
	}
}
