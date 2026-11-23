package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/client"
)

const (
	hookBudget   = 2 * time.Second
	maxHookStdin = 32 << 20
)

// hook implementa `nobre hook <agente> <Evento>`. Falha aberta: qualquer
// problema (daemon fora, prazo de 2 s, status não 2xx, JSON inválido) sai com
// 0 e sem nenhuma saída, nem em stderr, para nunca atrapalhar o agente.
func (c *cli) hook(args []string) (code int) {
	// Nem um pânico pode atrapalhar o agente: sai com 0 e sem saída (a
	// resposta só é escrita no fim, então nada saiu ainda).
	defer func() {
		if recover() != nil {
			code = 0
		}
	}()
	start := time.Now()
	if len(args) != 2 {
		return 0
	}
	ctx, cancel := context.WithDeadline(context.Background(), start.Add(hookBudget))
	defer cancel()

	// O stdin é lido numa goroutine para o prazo valer também enquanto o
	// agente demora a fechar o pipe; o processo termina logo em seguida.
	type read struct {
		b   []byte
		err error
	}
	ch := make(chan read, 1)
	go func() {
		// Pânico aqui derrubaria o processo: vira erro de leitura.
		defer func() {
			if r := recover(); r != nil {
				ch <- read{nil, fmt.Errorf("leitura do stdin: %v", r)}
			}
		}()
		b, err := io.ReadAll(io.LimitReader(c.in, maxHookStdin+1))
		ch <- read{b, err}
	}()
	var in read
	select {
	case in = <-ch:
	case <-ctx.Done():
		return 0
	}
	if in.err != nil || len(in.b) == 0 || len(in.b) > maxHookStdin {
		return 0
	}

	call := adapter.HookCall{Event: args[1], Payload: in.b}
	cl := client.New(c.addr())
	call.AgentPID = cl.PID
	if v := c.getenv("NOBRE_RESUME"); v != "" {
		call.Env = map[string]string{"NOBRE_RESUME": v}
	}
	reply, err := cl.Hook(ctx, args[0], call)
	if err != nil {
		return 0
	}
	io.WriteString(c.out, reply.Stdout)
	if reply.Stderr != "" {
		io.WriteString(c.err, reply.Stderr)
	}
	return reply.Exit
}
