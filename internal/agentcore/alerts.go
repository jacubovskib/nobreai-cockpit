package agentcore

import (
	"fmt"

	"github.com/nobretech/nobreai-cockpit/internal/domain"
	"github.com/nobretech/nobreai-cockpit/internal/usage"
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

// contextWarningPctText é o aviso de faixa para o agente que só informa a % da janela.
func contextWarningPctText(pct int) string {
	return fmt.Sprintf("Contexto em %d%% da janela. Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card.", pct)
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

// ContextWarning é o aviso de faixa de contexto do prompt ("" sem nada a
// dizer): só sessão com captura completa e avisos ligados. Avisa uma vez por
// faixa por segmento de compactação da janela (a compactação de outra janela
// da conversa não conta, ver ContextLive.Segment); um segmento novo reinicia
// as faixas e avisa a mais alta já cruzada. Falhas do banco só vão para o log.
func (c *Core) ContextWarning(sess domain.Session, ev Event) string {
	if !sess.FullCapture || ev.SubagentID != "" {
		return ""
	}
	set, err := c.st.GetSettings()
	if err != nil {
		c.logf("configurações: %v", err)
		return ""
	}
	if !set.AgentContextWarnings {
		return ""
	}
	live, err := c.st.SessionContextWith(sess.ID, c.deps.Now(), set)
	if err != nil || (live.Tokens == 0 && !live.PctOnly) {
		if err != nil {
			c.logf("contexto de %s: %v", sess.ID, err)
		}
		return ""
	}
	al, err := c.st.GetSessionAlerts(sess.ID)
	if err != nil {
		c.logf("avisos de %s: %v", sess.ID, err)
		return ""
	}
	band := contextBand(set.ContextWarnPcts, live.Pct)
	newSegment := live.Segment > al.WarnedSegment
	if !newSegment && band <= al.WarnedPct {
		return ""
	}
	if err := c.st.SetContextWarned(sess.ID, band, live.Segment); err != nil {
		c.logf("avisos de %s: %v", sess.ID, err)
		return ""
	}
	if band == 0 {
		return ""
	}
	if live.PctOnly {
		return contextWarningPctText(int(live.Pct))
	}
	return contextWarningText(int(live.Pct), live.Tokens)
}

// bigOutputWarning é o aviso de saída grande do ToolAfter ("" sem nada a
// dizer): tamanho da resposta da ferramenta estimado como
// usage.EstimateTokens (bytes/4 arredondado para cima), acima de
// big_output_tokens e até big_output_max avisos por sessão. Só o agente
// principal (a saída de um subagente não entra no contexto dele).
func (c *Core) bigOutputWarning(sess domain.Session, ev Event) string {
	if !sess.FullCapture || ev.SubagentID != "" || ev.Tool.OutputBytes == 0 {
		return ""
	}
	set, err := c.st.GetSettings()
	if err != nil {
		c.logf("configurações: %v", err)
		return ""
	}
	tokens := usage.EstimateTokens(ev.Tool.OutputBytes, 0)
	if !set.AgentContextWarnings || tokens <= int64(set.BigOutputTokens) {
		return ""
	}
	ok, err := c.st.ClaimBigOutputWarning(sess.ID, set.BigOutputMax)
	if err != nil {
		c.logf("avisos de %s: %v", sess.ID, err)
		return ""
	}
	if !ok {
		return ""
	}
	return bigOutputText(tokens)
}
