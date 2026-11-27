package api

import (
	"context"
	"time"
)

// wsBatchWindow é a janela de agrupamento da saída para quem assiste com
// ?batch=1 (a grade de terminais): blocos que chegam dentro dela viram um só.
const wsBatchWindow = 100 * time.Millisecond

// batchChunks junta, em cada janela, os blocos recebidos de in num só, na
// ordem. A janela abre no primeiro bloco e fecha depois de window. Quando in
// fecha, entrega o que restar e fecha a saída; quando ctx termina, fecha a
// saída (descartando o pendente). Os bytes são copiados: o chamador pode
// reusar o slice.
func batchChunks(ctx context.Context, in <-chan []byte, window time.Duration) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		var pending []byte
		timer := time.NewTimer(window)
		if !timer.Stop() {
			<-timer.C
		}
		defer timer.Stop()
		var tick <-chan time.Time // nil enquanto não há janela aberta

		flush := func() bool {
			b := pending
			pending, tick = nil, nil
			select {
			case out <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case b, open := <-in:
				if !open {
					if len(pending) > 0 {
						flush()
					}
					return
				}
				if len(b) == 0 {
					continue
				}
				if tick == nil {
					timer.Reset(window)
					tick = timer.C
				}
				pending = append(pending, b...)
			case <-tick:
				if !flush() {
					return
				}
			}
		}
	}()
	return out
}
