package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/client"
)

const (
	statuslineBudget = 300 * time.Millisecond
	maxStatuslineIn  = 1 << 20
)

// statuslineJSON é a parte do JSON da status line que interessa ao Cockpit.
type statuslineJSON struct {
	SessionID  string          `json:"session_id"`
	RateLimits json.RawMessage `json:"rate_limits"`
}

type statuslineWindow struct {
	UsedPercentage *float64 `json:"used_percentage"`
}

// statusline implementa `nobre statusline [--resumo]`, que entra no pipe da
// status line do Claude Code. Sem --resumo escreve no stdout exatamente os
// bytes recebidos; com --resumo escreve "5h 23% · semana 41%". Só depois
// envia os limites ao daemon, com 300 ms no total. Sai 0 sempre e nunca
// escreve em stderr.
func (c *cli) statusline(args []string) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	resumo := false
	for _, a := range args {
		if a == "--resumo" {
			resumo = true
		}
	}
	in, _ := io.ReadAll(io.LimitReader(c.in, maxStatuslineIn+1))
	tooBig := len(in) > maxStatuslineIn
	if tooBig && !resumo {
		// Mais que o limite: repassa tudo o que veio, sem enviar nada.
		c.out.Write(in)
		io.Copy(c.out, c.in)
		return 0
	}
	var sl statuslineJSON
	parsed := !tooBig && json.Unmarshal(in, &sl) == nil && len(sl.RateLimits) > 0 && string(sl.RateLimits) != "null"
	if !resumo {
		c.out.Write(in) // antes de qualquer rede
	} else if parsed {
		io.WriteString(c.out, statuslineSummary(sl.RateLimits))
	}
	if !parsed {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), statuslineBudget)
	defer cancel()
	client.NewBare(c.addr()).PlanLimits(ctx, sl)
	return 0
}

// statuslineSummary monta "5h 23% · semana 41%" (janelas ausentes omitidas;
// vazio, sem nova linha, se não houver nenhuma).
func statuslineSummary(raw json.RawMessage) string {
	var rl struct {
		FiveHour *statuslineWindow `json:"five_hour"`
		SevenDay *statuslineWindow `json:"seven_day"`
	}
	if json.Unmarshal(raw, &rl) != nil {
		return ""
	}
	var parts []string
	for _, w := range []struct {
		label string
		v     *statuslineWindow
	}{{"5h", rl.FiveHour}, {"semana", rl.SevenDay}} {
		if w.v == nil || w.v.UsedPercentage == nil {
			continue
		}
		if u := *w.v.UsedPercentage; !math.IsNaN(u) && u >= 0 && u <= 100 {
			parts = append(parts, fmt.Sprintf("%s %d%%", w.label, int(math.Round(u))))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + "\n"
}
