package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/client"
)

const (
	statuslineBudget      = 300 * time.Millisecond // sem processo destacado
	statuslineChildBudget = 2 * time.Second
	maxStatuslineIn       = 1 << 20
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
// bytes recebidos; com --resumo escreve "5h 23% · semana 41%". Depois fecha o
// stdout (o leitor do pipe vê EOF) e entrega os limites ao daemon num processo
// destacado (`nobre statusline --enviar`), sem esperar. Sai 0 sempre e nunca
// escreve em stderr; SIGPIPE (o comando original não lê o stdin) é ignorado.
func (c *cli) statusline(args []string) (code int) {
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	signal.Ignore(syscall.SIGPIPE)
	resumo, enviar := false, false
	for _, a := range args {
		switch a {
		case "--resumo":
			resumo = true
		case "--enviar":
			enviar = true
		}
	}
	if enviar {
		// Filho destacado: o JSON dos limites vem no stdin; prazo de 2 s.
		b, _ := io.ReadAll(io.LimitReader(c.in, maxStatuslineIn))
		c.postLimits(b, statuslineChildBudget)
		return 0
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
	if f, ok := c.out.(*os.File); ok {
		f.Close() // EOF para o comando original já
	}
	if !parsed {
		return 0
	}
	payload, err := json.Marshal(sl)
	if err != nil {
		return 0
	}
	if c.spawn != nil {
		c.spawn(payload)
	} else {
		c.postLimits(payload, statuslineBudget) // sem destacar (testes)
	}
	return 0
}

// postLimits envia o JSON ao daemon com o prazo dado; falhas são ignoradas.
func (c *cli) postLimits(payload []byte, budget time.Duration) {
	if len(payload) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	client.NewBare(c.addr()).PlanLimits(ctx, json.RawMessage(payload))
}

// spawnStatuslineSender re-executa o próprio nobre como `statusline --enviar`,
// em nova sessão, com o payload num pipe, stdout/stderr em /dev/null, sem
// esperar. Payload grande demais para o pipe (64 KiB) é descartado.
func spawnStatuslineSender(addr string) func([]byte) {
	return func(payload []byte) {
		if len(payload) > 60<<10 {
			return
		}
		exe, err := os.Executable()
		if err != nil {
			return
		}
		r, w, err := os.Pipe()
		if err != nil {
			return
		}
		defer r.Close()
		_, werr := w.Write(payload)
		w.Close()
		if werr != nil {
			return
		}
		cmd := exec.Command(exe, "statusline", "--enviar")
		cmd.Stdin = r
		cmd.Env = append(os.Environ(), "NOBRE_ADDR="+addr)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if cmd.Start() == nil {
			cmd.Process.Release()
		}
	}
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
