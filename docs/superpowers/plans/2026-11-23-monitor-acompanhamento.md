# Acompanhamento do monitor de contexto (NAC-43) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fechar os itens que as revisões do NAC-40 e do NAC-42 deixaram para depois: uso e compactação atribuídos à janela certa, turnos sem duplicata, telas de contexto do Analytics por janela e para sessões só com %, e faixas de aviso livres na Configuração.

**Architecture:** Correções no leitor de transcript do Claude (`internal/usage`, `internal/adapter/claude`), no store (`internal/store`) e nas telas (`web/`). Sem regra nova de produto; cada item vem de um achado de revisão com o caminho de correção já apontado.

**Tech Stack:** Go (SQLite), React + TypeScript + Vitest.

**Spec:** docs/superpowers/specs/2026-11-20-monitor-de-contexto-design.md (monitor) e docs/superpowers/specs/2026-11-23-integracao-kiro-design.md (sessões só com %, Kiro).

## Global Constraints

- Releitura nunca duplica uso nem turnos; uso (`usage`) nunca é apagado.
- Conversa com uma janela só: nada muda (tudo na principal).
- Nenhum `if agent == "claude"`/`"kiro"` em `internal/agentcore`, `internal/api`, `internal/board`, `internal/store` ou `web/`.
- Mudança de numeração de turnos exige versão nova do leitor (`context_reader_version`) com releitura única que apaga os turnos e saídas do arquivo relido antes de regravar (uso fica).
- Textos de tela em português, curtos.

## Review Focus

- Subagente chamado pela janela filha antes de a linha de resultado do Agent existir (leitura incremental): o uso dele termina no card da filha depois da leitura seguinte (Task 1).
- Releitura do histórico com a versão nova termina em segundos e não muda nenhum total de uso (Task 2).
- Conversa de uma janela só que compacta várias vezes: segmentos e avisos iguais aos de hoje (Task 3).
- Sessão Claude sem turnos, filha sem turnos e sessão Kiro com 1 turno: telas de contexto sem erro (Task 4).
- Configuração com faixas `[]` salva e desliga os avisos de faixa sem quebrar o aviso de saída grande (Task 5).

---

### Task 1: Uso de subagente da janela filha no card da filha (I3)

**Files:** `internal/usage/context.go` (+ testes), `internal/adapter/claude/ingest.go`, `internal/adapter/claude/window.go` (+ `window_test.go`), `internal/store/usage.go`/`context.go` se precisar de atualização de janela.

- Fato: a linha `user` do transcript principal com o `tool_result` do Agent traz `toolUseResult.agentId`, igual ao id do arquivo `subagents/agent-<id>.jsonl`; a janela dessa linha já é conhecida pela atribuição atual.
- Regra: mapear `agentId → janela` ao ler o principal; o arquivo do subagente usa essa janela como padrão para uso, turnos e saídas (`window_session_id` e `usage.card_id` do card da janela). Uso do subagente gravado antes de o mapeamento existir é corrigido quando o mapeamento aparece (atualiza janela e card das linhas daquele arquivo).
- [ ] Testes: filha chama Agent → uso do arquivo do subagente com `window_session_id` = filha e custo no card da filha; mesma coisa com o arquivo do subagente lido ANTES da linha de resultado (leitura incremental), corrigido na leitura seguinte; principal chama Agent → nada muda.
- [ ] Commit `fix(claude): uso de subagente da janela filha no card da filha`.

### Task 2: Turnos sem duplicata com `message.id` intercalado (M1)

**Files:** `internal/usage/context.go` (+ testes), `internal/store/context.go` (+ testes), `internal/adapter/claude/ingest.go` (versão do leitor).

- Fato: blocos do mesmo `message.id` podem vir intercalados com linhas da outra janela (m1, c1, m1); hoje viram turnos 0:m1, 1:c1, 2:m1.
- Regra: um turno por `message.id` — mapa `message_id → turno` na leitura e, entre leituras, consulta pelo índice `idx_context_turns_message`. Leitor v4: na subida com versão < 4, releitura única que apaga `context_turns` e `tool_outputs` dos arquivos relidos antes de regravar (uso intacto), seguindo o mecanismo de `context_reader_version`.
- [ ] Testes: m1,c1,m1 → dois turnos (m1, c1) com tokens do último bloco de m1; m1 numa leitura e m1 de novo na seguinte → mesmo turno; v3→v4 relê uma vez, totais de uso iguais antes e depois, `ContextTotals` sem dupla contagem.
- [ ] Commit `fix(usage): um turno por message.id e leitor v4`.

### Task 3: Compactação da filha sem prompt entre elas

**Files:** `internal/adapter/claude/window.go` (+ `window_test.go`).

- Fato: hoje o resumo de compactação e o que vem depois vão para a principal até o próximo prompt da filha (`TestWindow_CompactSummaryGoesToMain`); se a filha compacta duas vezes sem prompt, a 2ª compactação fica com a principal.
- Regra nova (substitui a anterior): o resumo de compactação herda a janela da `compact_boundary` que o antecede (resolvida pelo `logicalParentUuid`, como já se faz); as linhas seguintes herdam pelo `parentUuid` normalmente. Resumo sem fronteira resolvida → principal (como hoje).
- [ ] Testes: filha compacta duas vezes sem prompt → as duas compactações e os turnos depois delas na filha; principal compacta → igual a hoje; ajustar `TestWindow_CompactSummaryGoesToMain` para a regra nova (resumo da principal continua na principal).
- [ ] Commit `fix(claude): resumo de compactacao fica na janela que compactou`.

### Task 4: Telas de contexto do Analytics por janela e só com % (M6 + Kiro)

**Files:** `internal/store/context_query.go` (+ testes), `internal/api/context.go` (+ testes), `web/src/pages/analytics/ContextCurve.tsx`, `ContextSection.tsx` (+ testes), `web/src/api/types.ts`.

- Regra: `ContextSessionDetail(id)` de uma filha lê os turnos gravados sob a principal com `window_session_id` = filha; o da principal exclui os turnos das filhas. Sessão só com % (Kiro) devolve a curva em % (`pct` por turno) e a tela mostra o eixo em % com "janela informada pelo agente", sem tokens.
- [ ] Testes: filha com turnos → curva só dela; principal → sem os da filha; sessão Kiro com 3 turnos → curva em %; sessão sem turnos → vazio sem erro; Vitest da curva em %.
- [ ] Commit `fix: curva de contexto por janela e em porcentagem`.

### Task 5: Faixas de aviso livres na Configuração (M7)

**Files:** `web/src/pages/Config.tsx` (seção do monitor) (+ `Config.test.tsx`).

- Fato: a API aceita qualquer lista crescente de faixas entre 1 e 99, inclusive `[]`; a tela força exatamente duas e corta `[50,70,90]`.
- Regra: lista editável (acrescentar e remover faixa; vazia = avisos de faixa desligados, com uma linha dizendo isso), validação igual à da API antes de salvar.
- [ ] Testes: `[]` salva e mostra "sem avisos de faixa"; `[50,70,90]` aparece e salva inteira; ordem decrescente ou fora de 1–99 bloqueia salvar com mensagem.
- [ ] Commit `fix(web): faixas de aviso de contexto livres`.
