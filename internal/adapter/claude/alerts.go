package claude

import (
	"fmt"
	"log"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
)

// fmtTokens escreve uma quantidade de tokens em pt-BR: até 999 o número
// inteiro; de mil a menos de um milhão, milhares arredondados ("820 mil");
// de um milhão em diante, uma casa decimal com vírgula ("1,2 milhão",
// "2,5 milhões"; "1 milhão" quando a casa é zero). Plural a partir de 2.
func fmtTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if k := (n + 500) / 1000; k < 1000 {
		return fmt.Sprintf("%d mil", k)
	}
	tenths := (n + 50000) / 100000 // décimos de milhão
	unit := "milhão"
	if tenths >= 20 {
		unit = "milhões"
	}
	if tenths%10 == 0 {
		return fmt.Sprintf("%d %s", tenths/10, unit)
	}
	return fmt.Sprintf("%d,%d %s", tenths/10, tenths%10, unit)
}

// contextWarningText é o aviso de faixa de contexto ao agente.
func contextWarningText(pct int, tokens int64) string {
	return fmt.Sprintf("Contexto em %d%% da janela (%s tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card.", pct, fmtTokens(tokens))
}

// bigOutputText é o aviso de saída de ferramenta grande ao agente.
func bigOutputText(tokens int64) string {
	return fmt.Sprintf("Essa saída teve ~%s tokens e fica sendo relida a cada turno. Prefira ler trechos (offset/limit, grep) em vez do arquivo inteiro.", fmtTokens(tokens))
}

// contextBand devolve a maior faixa configurada que o percentual já alcançou
// (0 se nenhuma).
func contextBand(pcts []int, pct float64) int {
	band := 0
	for _, b := range pcts {
		if float64(b) <= pct && b > band {
			band = b
		}
	}
	return band
}

// contextWarning é o aviso de faixa de contexto do prompt ("" sem nada a
// dizer): só sessão com captura completa e avisos ligados. Avisa uma vez por
// faixa por segmento de compactação da janela (a compactação de outra janela
// da conversa não conta, ver ContextLive.Segment); um segmento novo reinicia
// as faixas e avisa a mais alta já cruzada. Falhas do banco só vão para o log.
func (a *Adapter) contextWarning(sess domain.Session, p payload) string {
	if !sess.FullCapture || p.AgentID != "" {
		return ""
	}
	set, err := a.st.GetSettings()
	if err != nil {
		log.Printf("hook claude: configurações: %v", err)
		return ""
	}
	if !set.AgentContextWarnings {
		return ""
	}
	live, err := a.st.SessionContextWith(sess.ID, a.deps.Now(), set)
	if err != nil || live.Tokens == 0 {
		if err != nil {
			log.Printf("hook claude: contexto de %s: %v", sess.ID, err)
		}
		return ""
	}
	al, err := a.st.GetSessionAlerts(sess.ID)
	if err != nil {
		log.Printf("hook claude: avisos de %s: %v", sess.ID, err)
		return ""
	}
	band := contextBand(set.ContextWarnPcts, live.Pct)
	newSegment := live.Segment > al.WarnedSegment
	if !newSegment && band <= al.WarnedPct {
		return ""
	}
	if err := a.st.SetContextWarned(sess.ID, band, live.Segment); err != nil {
		log.Printf("hook claude: avisos de %s: %v", sess.ID, err)
		return ""
	}
	if band == 0 {
		return ""
	}
	return contextWarningText(int(live.Pct), live.Tokens)
}

// bigOutputWarning é o aviso de saída grande do PostToolUse ("" sem nada a
// dizer): tamanho estimado em bytes/4 da resposta da ferramenta, acima de
// big_output_tokens e até big_output_max avisos por sessão. Só o agente
// principal (a saída de um subagente não entra no contexto dele).
func (a *Adapter) bigOutputWarning(sess domain.Session, p payload) string {
	if !sess.FullCapture || p.AgentID != "" || len(p.ToolResponse) == 0 {
		return ""
	}
	set, err := a.st.GetSettings()
	if err != nil {
		log.Printf("hook claude: configurações: %v", err)
		return ""
	}
	tokens := int64(len(p.ToolResponse)) / 4
	if !set.AgentContextWarnings || tokens <= int64(set.BigOutputTokens) {
		return ""
	}
	ok, err := a.st.ClaimBigOutputWarning(sess.ID, set.BigOutputMax)
	if err != nil {
		log.Printf("hook claude: avisos de %s: %v", sess.ID, err)
		return ""
	}
	if !ok {
		return ""
	}
	return bigOutputText(tokens)
}
