package claude

import (
	"errors"
	"io/fs"
	"log"
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
		mu, _ := a.ingest.LoadOrStore(sessionID, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		if err := a.readTranscripts(sessionID, path); err != nil {
			log.Printf("hook claude: transcript da sessão %s: %v", sessionID, err)
		}
	}()
}

// readTranscripts grava o uso novo do transcript principal (offset em
// sessions.transcript_offset) e dos subagentes (offset por arquivo). As
// linhas carregam o sessionId do Claude Code: passam a usar o ID da sessão do
// Cockpit. Transcript que ainda não existe é ignorado.
func (a *Adapter) readTranscripts(sessionID, mainPath string) error {
	off, err := a.st.TranscriptOffset(sessionID)
	if err != nil {
		return err
	}
	rows, next, err := a.readFile(sessionID, mainPath, off)
	if err != nil {
		return err
	}
	if next != off {
		if err := a.st.SetTranscriptOffset(sessionID, next); err != nil {
			return err
		}
	}
	changed := len(rows) > 0
	if changed {
		if err := a.st.SetSessionModel(sessionID, rows[len(rows)-1].Model); err != nil {
			return err
		}
	}
	for _, sub := range usage.SubagentTranscripts(mainPath) {
		off, err := a.st.FileOffset(sessionID, sub)
		if err != nil {
			return err
		}
		subRows, next, err := a.readFile(sessionID, sub, off)
		if err != nil {
			return err
		}
		if next != off {
			if err := a.st.SetFileOffset(sessionID, sub, next); err != nil {
				return err
			}
		}
		changed = changed || len(subRows) > 0
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
