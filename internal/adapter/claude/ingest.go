package claude

import (
	"errors"
	"io/fs"
	"log"
	"runtime/debug"
	"sync"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// ingestAsync lê o transcript da sessão (e os dos subagentes) em segundo
// plano, depois da resposta do hook. Leituras da mesma sessão são
// serializadas; Wait espera as pendentes.
func (a *Adapter) ingestAsync(sessionID, path string) {
	if path == "" {
		return
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// Um pânico aqui não pode derrubar o daemon.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("hook claude: pânico ao ler o transcript da sessão %s: %v\n%s", sessionID, r, debug.Stack())
			}
		}()
		mu, _ := a.ingest.LoadOrStore(sessionID, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		if err := a.readTranscripts(sessionID, path); err != nil {
			log.Printf("hook claude: transcript da sessão %s: %v", sessionID, err)
		}
	}()
}

// readTranscripts grava o uso novo do transcript principal e dos subagentes.
// O offset de cada arquivo fica em transcript_offsets, por (sessão, caminho):
// se o transcript_path da sessão muda, o arquivo novo é lido do começo. As
// linhas carregam o sessionId do Claude Code: passam a usar o ID da sessão do
// Cockpit. Transcript que ainda não existe é ignorado.
func (a *Adapter) readTranscripts(sessionID, mainPath string) error {
	changed := false
	for i, path := range append([]string{mainPath}, usage.SubagentTranscripts(mainPath)...) {
		off, err := a.st.FileOffset(sessionID, path)
		if err != nil {
			return err
		}
		rows, next, err := a.readFile(sessionID, path, off)
		if err != nil {
			return err
		}
		if next != off {
			if err := a.st.SetFileOffset(sessionID, path, next); err != nil {
				return err
			}
		}
		if i == 0 && len(rows) > 0 {
			// O modelo da sessão é o do transcript principal (subagentes
			// podem usar outro).
			if err := a.st.SetSessionModel(sessionID, rows[len(rows)-1].Model); err != nil {
				return err
			}
		}
		changed = changed || len(rows) > 0
	}
	if changed {
		a.publish("usage")
	}
	return nil
}

func (a *Adapter) readFile(sessionID, path string, off int64) ([]domain.UsageRow, int64, error) {
	rows, next, err := usage.ParseClaudeTranscript(path, off)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, off, nil
	}
	if err != nil {
		return nil, off, err
	}
	for i := range rows {
		rows[i].SessionID = sessionID
	}
	if err := a.st.UpsertUsage(rows); err != nil {
		return nil, off, err
	}
	return rows, next, nil
}
