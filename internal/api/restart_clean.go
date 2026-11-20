package api

import (
	"net/http"
	"strings"
	"time"
)

// whereStoppedMaxAge é a idade máxima do "Onde parei" para recomeçar sem
// registrar um novo.
const restartLaunchFailed = " — o card ficou pausado; use Continuar no card para abrir de novo."

const whereStoppedMaxAge = 30 * time.Minute

func (s *server) restartCleanRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/sessions/{id}/restart-clean", s.restartClean)
}

// restartClean: POST /api/sessions/{id}/restart-clean {where_stopped}
// garante um "Onde parei" recente, desliga o card da sessão (que segue viva,
// sem card), pausa o card e abre um agente novo pelo mesmo caminho do
// Continuar (mesmo agente, modo padrão das configurações).
func (s *server) restartClean(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WhereStopped string `json:"where_stopped"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	sess, err := s.st.GetSession(r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err, "sessão não encontrada")
		return
	}
	if sess.CardID == nil {
		writeError(w, http.StatusBadRequest, "sessão sem card")
		return
	}
	c, err := s.st.GetCardByID(*sess.CardID)
	if err != nil {
		writeStoreError(w, err, msgCardNotFound)
		return
	}
	now := s.now()
	if text := strings.TrimSpace(in.WhereStopped); text != "" {
		if err := s.st.SetWhereStopped(c.ID, text, now); err != nil {
			writeStoreError(w, err, msgCardNotFound)
			return
		}
	} else if strings.TrimSpace(c.WhereStopped) == "" || c.WhereStoppedAt == nil ||
		now.Sub(*c.WhereStoppedAt) > whereStoppedMaxAge {
		writeError(w, http.StatusBadRequest, "registre onde parou antes de recomeçar")
		return
	}
	// Validações sem efeito colateral antes de mexer em qualquer coisa: se o
	// Continuar vai recusar, nada muda. Sem checar sessão viva: a atual ainda
	// é dona do card e é a que sai.
	pl, ok := s.prepareContinue(w, c.Key, continueInput{Agent: sess.Agent}, false)
	if !ok {
		return
	}
	if err := s.st.DetachSessionCard(sess.ID); err != nil {
		writeStoreError(w, err, "sessão não encontrada")
		return
	}
	s.publish("sessions")
	s.publish("cards")
	// Se a abertura falhar agora, o card já ficou pausado e desligado.
	s.runContinue(w, pl, restartLaunchFailed)
}
