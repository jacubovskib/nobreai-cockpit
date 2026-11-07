package board

import (
	"context"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/proc"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

// Reconcile confere as sessões vivas com /proc (procRoot). Sessão cujo PID não
// existe mais recebe pid_dead_at na primeira passada; quando now passa de
// pid_dead_at + GraceMin, a sessão é encerrada ("processo encerrado") e o card
// sai de em_andamento. Sessões sem PID conhecido (pid <= 0) são ignoradas: não
// há como saber se morreram. changed diz se algo foi gravado (para publicar
// cards/sessions).
func Reconcile(st *store.Store, procRoot string, now time.Time, s domain.Settings) (changed bool, err error) {
	live, err := st.LiveSessions()
	if err != nil {
		return false, err
	}
	grace := time.Duration(s.GraceMin) * time.Minute
	for _, x := range live {
		if x.PID <= 0 || proc.Alive(procRoot, x.PID) {
			continue
		}
		dead := x.PIDDeadAt
		if dead == nil {
			if err := st.MarkPIDDead(x.ID, now); err != nil {
				return changed, err
			}
			dead, changed = &now, true
		}
		if now.Sub(*dead) > grace {
			if err := st.EndSession(x.ID, "processo encerrado", now); err != nil {
				return changed, err
			}
			changed = true
		}
	}
	return changed, nil
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
