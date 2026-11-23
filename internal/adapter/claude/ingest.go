package claude

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/store"
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
		changed, err := a.readLocked(sessionID, path)
		if err != nil {
			log.Printf("hook claude: transcript da sessão %s: %v", sessionID, err)
		}
		if changed {
			a.publish("usage")
		}
	}()
}

// readLocked lê os transcripts da sessão com o mutex dela e devolve se há
// dado novo (uso ou contexto). Um pânico aqui não pode derrubar o daemon:
// vira log e a leitura termina sem mudança.
func (a *Adapter) readLocked(sessionID, path string) (changed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hook claude: pânico ao ler o transcript da sessão %s: %v\n%s", sessionID, r, debug.Stack())
			changed, err = false, nil
		}
	}()
	a.readerMu.RLock()
	defer a.readerMu.RUnlock()
	mu, _ := a.ingest.LoadOrStore(sessionID, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	return a.readTranscripts(sessionID, path)
}

// readTranscripts grava o uso novo do transcript principal e dos subagentes.
// O offset de cada arquivo fica em transcript_offsets, por (sessão, caminho):
// se o transcript_path da sessão muda, o arquivo novo é lido do começo. As
// linhas carregam o sessionId do Claude Code: passam a usar o ID da sessão do
// Cockpit. Transcript que ainda não existe é ignorado.
//
// Em cada arquivo o contexto é lido antes do uso: a leitura de contexto
// atribui as linhas às janelas, e o uso é lido só até onde ela chegou (uma
// linha escrita entre as duas leituras ficaria com o card da principal para
// sempre). Se a leitura de contexto falha, o uso é lido mesmo assim, até o
// fim, e fica com a principal.
func (a *Adapter) readTranscripts(sessionID, mainPath string) (bool, error) {
	changed, ctxChanged := false, false
	// Fora do hook: os prompts velhos de window_prompts somem a cada leitura.
	if err := a.st.PruneWindowPrompts(a.deps.Now()); err != nil {
		log.Printf("hook claude: limpar prompts das janelas: %v", err)
	}
	for i, path := range append([]string{mainPath}, usage.SubagentTranscripts(mainPath)...) {
		// Falha (erro ou pânico) na medição de contexto não interrompe a
		// leitura do uso.
		n, wins, end := a.readContextSafe(sessionID, path, i == 0)
		ctxChanged = ctxChanged || n > 0
		off, err := a.st.FileOffset(sessionID, path)
		if err != nil {
			return false, err
		}
		rows, next, err := a.readFile(sessionID, path, off, end, wins)
		if err != nil {
			return false, err
		}
		if next != off {
			if err := a.st.SetFileOffset(sessionID, path, next); err != nil {
				return false, err
			}
		}
		if i == 0 && len(rows) > 0 {
			// O modelo de cada sessão é o do último uso da própria janela no
			// transcript principal (subagentes podem usar outro).
			last := map[string]string{} // janela dona → modelo
			for _, r := range rows {
				owner := sessionID
				if r.WindowSessionID != "" {
					owner = r.WindowSessionID
				}
				last[owner] = r.Model
			}
			for owner, model := range last {
				if err := a.st.SetSessionModel(owner, model); err != nil {
					return false, err
				}
			}
		}
		changed = changed || len(rows) > 0
	}
	return changed || ctxChanged, nil
}

// parseContext é o leitor de contexto (trocado nos testes para simular falha).
var parseContext = usage.ParseContext

// readContextSafe roda readContext e transforma erro ou pânico em log com
// end=-1 (o uso é lido até o fim, com a principal): a medição de contexto
// nunca impede a leitura do uso.
func (a *Adapter) readContextSafe(sessionID, path string, main bool) (n int, wins map[string]string, end int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hook claude: pânico no contexto do transcript %s: %v\n%s", path, r, debug.Stack())
			n, wins, end = 0, nil, -1
		}
	}()
	n, wins, end, err := a.readContext(sessionID, path, main)
	if err != nil {
		log.Printf("hook claude: contexto do transcript %s: %v", path, err)
		return 0, nil, -1
	}
	return n, wins, end
}

// readContext grava saídas e turnos novos do transcript path (medição de
// contexto), com cursor próprio (tool_offset), atribuídos às janelas da
// conversa (assignWindows). Só o transcript principal (main) casa prompts do
// hook: o prompt de um subagente é escrito pelo modelo, nunca digitado numa
// janela. Devolve quantos registros leu, a janela de cada turno lido
// (message.id → janela, "" = principal) e até onde leu (end; -1 se o arquivo
// não existe). Arquivo inexistente é ignorado.
func (a *Adapter) readContext(sessionID, path string, main bool) (n int, wins map[string]string, end int64, err error) {
	off, st, err := a.st.ContextCursor(sessionID, path)
	if err != nil {
		return 0, nil, 0, err
	}
	outs, turns, prompts, lines, next, newSt, reset, err := parseContext(path, off, st)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil, -1, nil
	}
	if err != nil {
		return 0, nil, 0, err
	}
	n = len(outs) + len(turns)
	if next == off && !reset && n == 0 {
		return 0, nil, next, nil
	}
	// reset (arquivo truncado ou trocado) apaga as linhas do path: nada de
	// antes vale, e o contexto relido volta para a principal até um prompt
	// novo casar. O uso já gravado mantém a janela e o card (nunca são
	// reescritos).
	var known map[string]string
	if !reset {
		if known, err = a.st.WindowLines(sessionID, path); err != nil {
			return 0, nil, 0, err
		}
	}
	var open []store.WindowPrompt
	if main {
		if open, err = a.st.OpenWindowPrompts(sessionID); err != nil {
			return 0, nil, 0, err
		}
	}
	byUUID, roots, used := assignWindows(sessionID, lines, prompts, known, open)
	w := store.ContextWindows{ByUUID: byUUID, MainRoots: roots, UsedPrompts: used,
		Compactions: compactionWindows(sessionID, lines, byUUID, known)}
	if err := a.st.SaveContextWindows(sessionID, path, reset, outs, turns, next, newSt, w); err != nil {
		return 0, nil, 0, err
	}
	wins = make(map[string]string, len(turns))
	for _, t := range turns {
		wins[t.MessageID] = byUUID[t.UUID]
	}
	return n, wins, next, nil
}

// Versão do leitor de contexto, guardada na chave livre contextReaderVersionKey
// das configurações. Subir a versão faz a próxima passada do histórico refazer
// toda a medição de contexto uma vez. Versão 2: detalhe de cache e TTL por
// turno (NAC-40). Versão 3: janela de cada compactação (context_compactions).
const (
	contextReaderVersion    = 3
	contextReaderVersionKey = "context_reader_version"
)

// upgradeReader volta os cursores de contexto ao começo quando a versão
// gravada é menor que a atual (sem apagar nada: a releitura faz upsert) e diz
// se a passada que vem precisa gravar a versão no fim. A versão só é gravada
// depois de uma passada inteira (sem ctx cancelado): se o daemon cair no meio,
// a próxima subida volta os cursores e relê de novo, sem pular ninguém.
func (a *Adapter) upgradeReader() (pending bool, err error) {
	var v int
	if _, err := a.st.GetKV(contextReaderVersionKey, &v); err != nil {
		return false, err
	}
	if v >= contextReaderVersion {
		return false, nil
	}
	a.readerMu.Lock()
	defer a.readerMu.Unlock()
	if err := a.st.ResetContextCursors(); err != nil {
		return false, err
	}
	log.Printf("medição de contexto: leitor v%d → v%d, relendo o histórico", v, contextReaderVersion)
	return true, nil
}

// StartBackfill roda BackfillContext em segundo plano; Wait espera o fim.
func (a *Adapter) StartBackfill(ctx context.Context) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.BackfillContext(ctx)
	}()
}

// BackfillContext lê o histórico: todos os transcripts das sessões do Claude
// ainda sem medição de contexto (e os já medidos, só do ponto onde pararam).
// Para entre sessões quando ctx termina; um erro ou pânico numa sessão não
// interrompe as outras. Com o leitor numa versão antiga, antes volta os
// cursores ao começo (upgradeReader) e, se a passada for até o fim, grava a
// versão nova; erro numa sessão não segura a versão (senão um arquivo ilegível
// faria tudo ser relido a cada subida). Transcript que não existe mais mantém
// a medição antiga, com as colunas novas zeradas.
func (a *Adapter) BackfillContext(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	start := time.Now()
	pending, err := a.upgradeReader()
	if err != nil {
		log.Printf("medição de contexto: versão do leitor: %v", err)
		return
	}
	transcripts, err := a.st.ClaudeTranscripts()
	if err != nil {
		log.Printf("medição de contexto: listar sessões: %v", err)
		return
	}
	done, changed := 0, false
	for id, path := range transcripts {
		if ctx.Err() != nil {
			break
		}
		c, err := a.readLocked(id, path)
		if err != nil {
			log.Printf("medição de contexto: sessão %s: %v", id, err)
		}
		changed = changed || c
		done++
	}
	if changed {
		a.publish("usage")
	}
	if pending && ctx.Err() == nil {
		if err := a.st.PutKV(contextReaderVersionKey, contextReaderVersion); err != nil {
			log.Printf("medição de contexto: gravar versão do leitor: %v", err)
		}
	}
	log.Printf("medição de contexto: %d de %d sessões em %s", done, len(transcripts), time.Since(start).Round(time.Millisecond))
}

// readFile grava o uso novo do transcript path, de off até end (end < 0 =
// até o fim). A janela de cada mensagem vem de wins (a leitura de contexto que
// acabou de rodar) ou, se o turno foi atribuído numa leitura anterior, de
// context_turns; sem nenhum dos dois, a principal.
func (a *Adapter) readFile(sessionID, path string, off, end int64, wins map[string]string) ([]domain.UsageRow, int64, error) {
	rows, next, err := usage.ParseClaudeTranscriptUntil(path, off, end)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, off, nil
	}
	if err != nil {
		return nil, off, err
	}
	for i := range rows {
		rows[i].SessionID = sessionID
		win, ok := wins[rows[i].MessageID]
		if !ok {
			if win, _, err = a.st.TurnWindow(sessionID, path, rows[i].MessageID); err != nil {
				return nil, off, err
			}
		}
		rows[i].WindowSessionID = win
	}
	if err := a.st.UpsertUsage(rows); err != nil {
		return nil, off, err
	}
	return rows, next, nil
}
