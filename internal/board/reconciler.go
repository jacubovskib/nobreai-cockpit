package board

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// IdleNoPID é por quanto tempo uma sessão sem PID conhecido pode ficar sem
// eventos antes de ser encerrada.
const IdleNoPID = 2 * time.Hour

// Liveness decide se o processo de uma sessão ainda é o agente dela: o PID
// existe em ProcRoot (/proc) e, para agente que está em Names, o nome do
// processo (comm) é um dos nomes dele, a mesma checagem do roteamento dos
// hooks. Um PID reaproveitado por outro programa conta como morto. Agente
// fora de Names (ou Names vazio, como na demonstração): basta o PID existir.
type Liveness struct {
	ProcRoot string
	Names    map[string][]string
}

// Alive diz se o processo da sessão x ainda é o agente dela.
func (l Liveness) Alive(x domain.Session) bool {
	if !proc.Alive(l.ProcRoot, x.PID) {
		return false
	}
	names, ok := l.Names[x.Agent]
	if !ok {
		return true
	}
	return slices.Contains(names, proc.Comm(l.ProcRoot, x.PID))
}

// Reconcile confere as sessões vivas com o processo de cada uma (live). Sessão
// cujo processo não é mais o agente recebe pid_dead_at na primeira passada;
// quando now passa de pid_dead_at + GraceMin, a sessão é encerrada ("processo encerrado") e o card
// sai de em_andamento. Sessão sem PID conhecido (pid <= 0) não tem processo
// para vigiar: é encerrada ("sem atividade") quando fica mais de IdleNoPID sem
// eventos. Processo que volta a ser o agente limpa pid_dead_at. Erros por sessão não
// interrompem as demais (voltam juntos via errors.Join). changed diz se algo
// foi gravado (para publicar cards/sessions).
func Reconcile(st *store.Store, live Liveness, now time.Time, s domain.Settings) (changed bool, err error) {
	sessions, err := st.LiveSessions()
	if err != nil {
		return false, err
	}
	grace := time.Duration(s.GraceMin) * time.Minute
	var errs []error
	for _, x := range sessions {
		if x.PID <= 0 {
			if now.Sub(x.LastEventAt) > IdleNoPID {
				if err := FinishSession(st, x.ID, "sem atividade", now); err != nil {
					errs = append(errs, fmt.Errorf("sessão %s: %w", x.ID, err))
					continue
				}
				changed = true
			}
			continue
		}
		if live.Alive(x) {
			// PID voltou (ou nunca morreu de fato): zera a carência.
			if x.PIDDeadAt != nil {
				if err := st.ClearPIDDead(x.ID); err != nil {
					errs = append(errs, fmt.Errorf("sessão %s: %w", x.ID, err))
				} else {
					changed = true
				}
			}
			continue
		}
		dead := x.PIDDeadAt
		if dead == nil {
			if err := st.MarkPIDDead(x.ID, now); err != nil {
				errs = append(errs, fmt.Errorf("sessão %s: %w", x.ID, err))
				continue
			}
			dead, changed = &now, true
		}
		if now.Sub(*dead) > grace {
			if err := FinishSession(st, x.ID, "processo encerrado", now); err != nil {
				errs = append(errs, fmt.Errorf("sessão %s: %w", x.ID, err))
				continue
			}
			changed = true
		}
	}
	return changed, errors.Join(errs...)
}

// RunReconciler chama Reconcile a cada every até ctx terminar (bloqueia; rode
// numa goroutine). As Settings são relidas a cada passada. onChange é chamado
// quando algo mudou; erros vão para logf (nil = ignora).
func RunReconciler(ctx context.Context, st *store.Store, live Liveness, every time.Duration,
	now func() time.Time, onChange func(), logf func(format string, args ...any)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		set, err := st.GetSettings()
		var changed bool
		if err == nil {
			changed, err = Reconcile(st, live, now(), set)
		}
		if err != nil && logf != nil {
			logf("reconciliação: %v", err)
		}
		if changed && onChange != nil {
			onChange()
		}
	}
}
