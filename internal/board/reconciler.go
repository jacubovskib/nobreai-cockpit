package board

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// IdleNoPID é por quanto tempo uma sessão sem PID conhecido pode ficar sem
// eventos antes de ser encerrada.
const IdleNoPID = 2 * time.Hour

// Reconcile confere as sessões vivas com /proc (procRoot). Sessão cujo PID não
// existe mais recebe pid_dead_at na primeira passada; quando now passa de
// pid_dead_at + GraceMin, a sessão é encerrada ("processo encerrado") e o card
// sai de em_andamento. Sessão sem PID conhecido (pid <= 0) não tem processo
// para vigiar: é encerrada ("sem atividade") quando fica mais de IdleNoPID sem
// eventos. PID que volta a existir limpa pid_dead_at. Erros por sessão não
// interrompem as demais (voltam juntos via errors.Join). changed diz se algo
// foi gravado (para publicar cards/sessions).
func Reconcile(st *store.Store, procRoot string, now time.Time, s domain.Settings) (changed bool, err error) {
	live, err := st.LiveSessions()
	if err != nil {
		return false, err
	}
	grace := time.Duration(s.GraceMin) * time.Minute
	var errs []error
	for _, x := range live {
		if x.PID <= 0 {
			if now.Sub(x.LastEventAt) > IdleNoPID {
				if err := st.EndSession(x.ID, "sem atividade", now); err != nil {
					errs = append(errs, fmt.Errorf("sessão %s: %w", x.ID, err))
					continue
				}
				changed = true
			}
			continue
		}
		if proc.Alive(procRoot, x.PID) {
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
			if err := st.EndSession(x.ID, "processo encerrado", now); err != nil {
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
func RunReconciler(ctx context.Context, st *store.Store, procRoot string, every time.Duration,
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
			changed, err = Reconcile(st, procRoot, now(), set)
		}
		if err != nil && logf != nil {
			logf("reconciliação: %v", err)
		}
		if changed && onChange != nil {
			onChange()
		}
	}
}
