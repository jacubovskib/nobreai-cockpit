package claude

import (
	"log"

	"github.com/nobretech/nobreai-cockpit/internal/agentcore"
	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/memory"
)

// childPhrase avisa a sessão filha (segundo processo na mesma conversa) de
// que os comandos nobre dela não mexem na sessão da outra janela.
const childPhrase = "Esta conversa também está aberta em outra janela, com sessão própria no Cockpit; os comandos nobre desta janela valem só para ela."

// StartNote é o aviso da sessão filha no início dela (agentcore.Noter).
func (a *Adapter) StartNote(sess domain.Session) string {
	if sess.ParentSessionID != "" {
		return childPhrase
	}
	return ""
}

// PromptNote registra o prompt para a atribuição por janela (só o resumo do
// texto, em toda sessão: o uso de cada janela depende dele) e devolve o aviso
// da filha que nasceu fora do SessionStart, uma vez (agentcore.Noter). Falhas
// só vão para o log.
func (a *Adapter) PromptNote(sess domain.Session, ev agentcore.Event) string {
	if err := a.st.AddWindowPrompt(sess.ID, promptKey(ev.Prompt), ev.At); err != nil {
		log.Printf("hook claude: prompt da janela %s: %v", sess.ID, err)
	}
	if sess.ParentSessionID == "" {
		return ""
	}
	pending, err := a.st.ClaimChildNotice(sess.ID)
	if err != nil {
		log.Printf("hook claude: aviso da filha %s: %v", sess.ID, err)
		return ""
	}
	if pending {
		return childPhrase
	}
	return ""
}

// GuideName é como o aviso de repositório não cadastrado cita as instruções
// do nobre no Claude (agentcore.Guide).
func (a *Adapter) GuideName() string { return "a skill nobre-cockpit" }

// MemoriesBlock é o bloco de memórias do projeto efetivo da sessão, com o
// limite de tokens da configuração e no máximo room caracteres ("" com o
// limite em 0, sem memórias, sem espaço ou se a leitura falhar: o hook nunca
// falha por isso) (agentcore.Memories).
func (a *Adapter) MemoriesBlock(sess domain.Session, set domain.Settings, room int) string {
	if set.MemoryBudgetTokens <= 0 {
		return ""
	}
	app, root, global, err := a.st.MemoriesForSession(sess)
	if err != nil {
		log.Printf("hook claude: memórias da sessão %s: %v", sess.ID, err)
		return ""
	}
	var items []memory.Item
	for scope, ms := range [][]domain.Memory{app, root, global} {
		for _, m := range ms {
			items = append(items, memory.Item{ID: m.ID, Scope: scope, Position: m.Position,
				Category: memory.Category(m.Category), Rule: m.Rule, Do: m.Do})
		}
	}
	return memory.BlockWithin(items, set.MemoryBudgetTokens, room)
}
