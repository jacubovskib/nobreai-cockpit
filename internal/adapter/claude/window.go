package claude

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/memory"
	"github.com/nobretech/nobreai-cockpit/internal/store"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
)

// promptKey é o resumo (sha256) do texto normalizado do prompt
// (memory.Normalize: sem acento, pontuação nem espaço repetido). É o que vai
// para window_prompts.text_hash: o texto nunca é gravado (sessão sem captura
// completa não guarda prompt). Texto que normaliza para vazio dá "".
func promptKey(text string) string {
	n := memory.Normalize(text)
	if n == "" {
		return ""
	}
	h := sha256.Sum256([]byte(n))
	return hex.EncodeToString(h[:])
}

// windowTolerance é a diferença máxima entre o horário do prompt no hook e o
// da linha do transcript para casar os dois.
const windowTolerance = 30 * time.Second

// assignWindows atribui cada linha desta leitura (lines, em ordem de arquivo)
// a uma janela da conversa da sessão principal mainID. Para cada linha:
//  0. já atribuída numa leitura anterior (known, de window_lines; o id da
//     principal vale como principal): fica;
//  1. prompt do usuário com prompt aberto de mesmo texto normalizado, de uma
//     sessão da conversa, até 30 s antes ou depois: a janela é essa sessão
//     (com vários, o mais próximo no tempo; o prompt fica usado);
//  2. senão herda a janela do parentUuid (visto nesta leitura ou em known);
//  3. compact_boundary (sem pai): a janela do logicalParentUuid, e o resumo
//     de compactação herda dela pelo parentUuid;
//  4. senão a principal (linhas antes do primeiro prompt, conversa com uma
//     janela só, fronteira sem pai lógico conhecido).
//
// Devolve as linhas de janela filha (uuid → id da janela), as raízes da
// principal que penduram num galho de filha (mainRoots) e os prompts usados.
// Um prompt casado com a principal também é consumido: separa o "ok" das duas
// janelas e impede que o prompt da principal herde um galho da filha. Quando o
// pai dele é de uma filha (um --resume novo continuou do fim do arquivo), a
// raiz é guardada em window_lines com o id da principal: numa releitura o
// prompt já está usado e a linha herdaria a filha.
func assignWindows(mainID string, lines []usage.LineRef, prompts []usage.Prompt, known map[string]string, open []store.WindowPrompt) (byUUID map[string]string, mainRoots []string, used []int64) {
	byUUID = map[string]string{}
	seen := map[string]string{} // uuid → janela nesta leitura ("" = principal)
	promptOf := make(map[string]usage.Prompt, len(prompts))
	for _, p := range prompts {
		if p.UUID != "" {
			promptOf[p.UUID] = p
		}
	}
	taken := map[int64]bool{}
	// parentWin é a janela do pai: desta leitura, senão de window_lines,
	// senão a principal.
	parentWin := func(parent string) string {
		win, ok := seen[parent]
		if !ok {
			win = known[parent]
		}
		if win == mainID {
			return ""
		}
		return win
	}
	for _, l := range lines {
		win, ok := known[l.UUID]
		if !ok {
			if p, isPrompt := promptOf[l.UUID]; isPrompt {
				if i := nearestPrompt(open, taken, promptKey(p.Text), p.At); i >= 0 {
					taken[open[i].ID] = true
					used = append(used, open[i].ID)
					win, ok = open[i].SessionID, true
					if win == mainID && l.ParentUUID != "" && parentWin(l.ParentUUID) != "" {
						mainRoots = append(mainRoots, l.UUID)
					}
				}
			}
		}
		if !ok && l.ParentUUID != "" {
			win = parentWin(l.ParentUUID)
		} else if !ok && l.Compact != 0 && l.LogicalParentUUID != "" {
			// compact_boundary: sem pai, fica com a janela que compactou; o
			// resumo e o que vem depois herdam dela pelo parentUuid.
			win = parentWin(l.LogicalParentUUID)
		}
		if win == mainID {
			win = ""
		}
		seen[l.UUID] = win
		if win != "" {
			byUUID[l.UUID] = win
		}
	}
	return byUUID, mainRoots, used
}

// compactionWindows devolve a janela de cada compactação desta leitura
// (segmento → janela, "" = principal): a do pai lógico (logicalParentUuid),
// atribuído nesta leitura (byUUID) ou numa anterior (known), senão a
// principal. A fronteira e o resumo herdam essa mesma janela (assignWindows); isto
// serve à contagem de compactações de cada janela.
func compactionWindows(mainID string, lines []usage.LineRef, byUUID, known map[string]string) map[int]string {
	var out map[int]string
	for _, l := range lines {
		if l.Compact == 0 {
			continue
		}
		win, ok := byUUID[l.LogicalParentUUID]
		if !ok {
			win = known[l.LogicalParentUUID]
		}
		if win == mainID {
			win = ""
		}
		if out == nil {
			out = map[int]string{}
		}
		out[l.Compact] = win
	}
	return out
}

// nearestPrompt devolve o índice do prompt aberto (não tomado) com o resumo
// key mais próximo de at dentro da tolerância; -1 se nenhum.
func nearestPrompt(open []store.WindowPrompt, taken map[int64]bool, key string, at time.Time) int {
	best, bestD := -1, time.Duration(0)
	if key == "" {
		return best
	}
	for i, p := range open {
		if taken[p.ID] || p.TextHash != key {
			continue
		}
		d := p.At.Sub(at)
		if d < 0 {
			d = -d
		}
		if d <= windowTolerance && (best < 0 || d < bestD) {
			best, bestD = i, d
		}
	}
	return best
}
