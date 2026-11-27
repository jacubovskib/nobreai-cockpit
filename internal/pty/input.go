package pty

import (
	"strings"
	"time"
)

const (
	// MaxInput é o maior texto aceito por Submit (depois de CleanInput).
	MaxInput = 256 << 10

	defaultPasteSettle = 150 * time.Millisecond

	pasteStart = "\x1b[200~"
	pasteEnd   = "\x1b[201~"
)

// CleanInput normaliza o texto a colar: quebras de linha viram \n, os
// marcadores de colagem são removidos (nem os que se formam ao remover outros
// sobram) e os \n do fim saem. Espaços do começo ficam.
func CleanInput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	for {
		n := strings.ReplaceAll(strings.ReplaceAll(text, pasteStart, ""), pasteEnd, "")
		if n == text {
			break
		}
		text = n
	}
	return strings.TrimRight(text, "\n")
}

// Submit digita text (já limpo por CleanInput) como colagem marcada, espera
// pasteSettle e envia Enter em escrita separada. Uma sequência por vez no
// terminal. Se a colagem falha, o Enter não é tentado; depois dela, sempre.
func (t *Terminal) Submit(text string) error {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	if err := t.Write([]byte(pasteStart + text + pasteEnd)); err != nil {
		return err
	}
	time.Sleep(t.pasteSettle)
	return t.Write([]byte("\r"))
}
