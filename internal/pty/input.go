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
	csiC1      = "\u009b"
)

// CleanInput normaliza o texto a colar: quebras de linha viram \n, todo ESC
// (0x1b) e todo CSI C1 (U+009B) saem e os \n do fim saem. Sem ESC não há
// marcador de colagem nem sequência de controle no meio do texto; o resto de um
// marcador ("[201~") fica como texto comum. Espaços do começo ficam.
func CleanInput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\x1b", "")
	// Tirar um CSI C1 de texto inválido pode formar outro: repete até parar.
	for strings.Contains(text, csiC1) {
		text = strings.ReplaceAll(text, csiC1, "")
	}
	return strings.TrimRight(text, "\n")
}

// Submit digita text (já limpo por CleanInput) como colagem marcada, espera
// pasteSettle (por t.sleep) e envia Enter em escrita separada. Uma sequência por vez no
// terminal. Se a colagem falha, o Enter não é tentado; depois dela, sempre.
func (t *Terminal) Submit(text string) error {
	t.inMu.Lock()
	defer t.inMu.Unlock()
	if err := t.Write([]byte(pasteStart + text + pasteEnd)); err != nil {
		return err
	}
	t.sleep(t.pasteSettle)
	return t.Write([]byte("\r"))
}
