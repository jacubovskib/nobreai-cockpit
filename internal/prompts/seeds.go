package prompts

import "github.com/nobretech/nobreai-cockpit/internal/domain"

// Seeds são os prompts iniciais (corpos da tela Prompts do design). Todos
// globais: não há projeto no momento do seed.
func Seeds() []domain.Prompt {
	all := []string{"claude", "codex", "kiro"}
	return []domain.Prompt{
		{Name: "Retomar card", Slug: "retomar-card", Tag: "Retomada", IsDefaultHandoff: true, Agents: all,
			Description: "Entrega o pacote de retomada e pede para continuar do ponto exato.",
			Body:        "Você está continuando o card {{card.id}} ({{card.titulo}}).\nLeia o pacote de retomada abaixo e continue a partir de:\n{{card.onde_parei}}\n\nNão pergunte o que estava sendo feito: está tudo no card. Atualize-o com nobre note a cada passo relevante.\n\n{{card.pacote}}"},
		{Name: "Revisar antes do PR", Slug: "revisar-pr", Tag: "Revisão", Agents: []string{"claude", "codex"},
			Description: "Revisão do diff do branch, com cada problema registrado em Falta.",
			Body:        "Revise o diff de {{git.branch}} contra main.\nListe bugs, riscos e testes faltando, nessa ordem.\nRegistre cada problema como item em Falta no card {{card.id}}."},
		{Name: "Testes de borda", Slug: "testes-borda", Tag: "Testes", Agents: all,
			Description: "Testes de borda para os arquivos essenciais do card.",
			Body:        "Para os arquivos essenciais do card {{card.id}}:\n{{card.arquivos}}\nescreva testes de borda e rode {{card.como_testar}}."},
		{Name: "Especificar módulo", Slug: "especificar", Tag: "Spec", Agents: []string{"kiro"},
			Description: "Requisitos, desenho e tarefas antes de codar.",
			Body:        "Antes de escrever código, especifique {{card.titulo}}: requisitos, desenho e lista de tarefas.\nRegistre as decisões com nobre decide."},
		{Name: "Fechar sessão com handoff", Slug: "fechar-handoff", Tag: "Retomada", Agents: all,
			Description: "Enviado pelo hook de fim de sessão quando a regra de handoff não foi cumprida.",
			Body:        "Antes de encerrar, atualize o card {{card.id}}:\n- Onde parei (o próximo passo exato)\n- Falta\n- arquivos essenciais e o porquê de cada um\nItens pendentes da regra: {{handoff.faltando}}"},
	}
}
