# Sessão compartilhada entre dois processos do Claude (NAC-39)

Abordagem escolhida pelo usuário em 2026-11-20 ("vamos no sugerido"): uma sessão filha por processo vivo.

## Problema (verificado)

- O hook manda o `session_id` da conversa; dois processos do Claude Code na mesma conversa (ex.: um aberto e um `claude --continue` em outra janela) mandam o mesmo id.
- `internal/adapter/claude/hooks.go` `session()`: PID diferente do registrado → `ReopenSession` troca o PID. Com os dois vivos o PID alterna a cada hook.
- `nobre` acha a sessão por PID (`store.SessionByPID`): quem mandou o último hook leva sessão e card; o outro recebe o card alheio ou "nenhuma sessão". O Stop hook cobra o card errado; `nobre start`/`note` num afetam o card do outro.
- Os dois escrevem no mesmo transcript, intercalados.

## Desenho

1. **Roteamento no hook** (`session()`), com `pid > 0`:
   - existe sessão viva com `provider_session_id = sid` e `pid = pid` → é ela (a principal ou uma filha);
   - senão, a principal (`claude:<sid>`) está viva, tem outro PID e esse PID ainda está vivo (`/proc`) → cria a **sessão filha** `claude:<sid>@<pid>`: mesmo projeto/captura/cwd/worktree/transcript da principal, `parent_session_id` = id da principal, sem card;
   - senão (principal encerrada ou PID antigo morto) → comportamento de hoje (reabre a principal com o PID novo; é o `--resume` normal).
   - `pid <= 0` → comportamento de hoje.
2. **`nobre`** continua achando a sessão por PID: cada processo passa a ter a sua.
3. **Stop hook, SessionStart e demais hooks** usam a sessão roteada, então cada janela vê e cobra só o próprio card.
4. **SessionStart da filha** acrescenta: "Esta conversa também está aberta em outra janela, com sessão própria no Cockpit; os comandos nobre desta janela valem só para ela."
5. **Uso e contexto (NAC-31):** o transcript é um só; a ingestão de uma filha roda com o id da **principal** (mesmos offsets e linhas, sem contar em dobro). O custo fica no card da principal (limitação desta entrega). Separar o custo por processo precisa de outro sinal: o `sessionId` das linhas do transcript não serve, porque os dois processos gravam o mesmo. Fica em aberto.
6. **Encerramento:** a filha encerra pelo reconciliador quando o PID dela morre (já é por PID) ou pelo SessionEnd dela. Encerrar a principal não encerra a filha.
7. **Tela:** sessões filhas aparecem com a marca "outra janela" onde as sessões são listadas (card e Analytics).

## Restrições

- Migração nova `016_session_parent.sql`: `sessions.parent_session_id TEXT NOT NULL DEFAULT ''`.
- Verificação de PID vivo injetável no adaptador (testes sem `/proc` real).
- Sem mudança para quem usa uma janela por conversa.

## Fora

- Separar custo por processo; impedir o segundo processo; avisos na Sala de controle.
