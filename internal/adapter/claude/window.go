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
// para window_prompts.text_norm: o texto nunca é gravado (sessão sem captura
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
//  0. já atribuída numa leitura anterior (known, de window_lines): fica;
//  1. prompt do usuário com prompt aberto de mesmo texto normalizado, de uma
//     sessão da conversa, até 30 s antes ou depois: a janela é essa sessão
//     (com vários, o mais próximo no tempo; o prompt fica usado);
//  2. senão herda a janela do parentUuid (visto nesta leitura ou em known);
//  3. senão a principal (resumo de compactação, linhas antes do primeiro
//     prompt, conversa com uma janela só).
//
// Devolve só as linhas de janela filha (uuid → id da janela) e os prompts
// usados. Um prompt casado com a principal também é consumido: separa o "ok"
// das duas janelas e impede que o prompt da principal herde um galho da filha.
func assignWindows(mainID string, lines []usage.LineRef, prompts []usage.Prompt, known map[string]string, open []store.WindowPrompt) (byUUID map[string]string, used []int64) {
	byUUID = map[string]string{}
	seen := map[string]string{} // uuid → janela nesta leitura ("" = principal)
	promptOf := make(map[string]usage.Prompt, len(prompts))
	for _, p := range prompts {
		if p.UUID != "" {
			promptOf[p.UUID] = p
		}
	}
	taken := map[int64]bool{}
	for _, l := range lines {
		win, ok := known[l.UUID]
		if !ok {
			if p, isPrompt := promptOf[l.UUID]; isPrompt {
				if i := nearestPrompt(open, taken, promptKey(p.Text), p.At); i >= 0 {
					taken[open[i].ID] = true
					used = append(used, open[i].ID)
					win, ok = open[i].SessionID, true
				}
			}
		}
		if !ok && l.ParentUUID != "" {
			if win, ok = seen[l.ParentUUID]; !ok {
				win, ok = known[l.ParentUUID]
			}
		}
		if win == mainID {
			win = ""
		}
		seen[l.UUID] = win
		if win != "" {
			byUUID[l.UUID] = win
		}
	}
	return byUUID, used
}

// nearestPrompt devolve o índice do prompt aberto (não tomado) com o resumo
// key mais próximo de at dentro da tolerância; -1 se nenhum.
func nearestPrompt(open []store.WindowPrompt, taken map[int64]bool, key string, at time.Time) int {
	best, bestD := -1, time.Duration(0)
	if key == "" {
		return best
	}
	for i, p := range open {
		if taken[p.ID] || p.TextNorm != key {
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
