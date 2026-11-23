# Integração com o Kiro (kiro-cli) e núcleo de agentes (NAC-42)

Combinado com o usuário em 2026-11-23: o Kiro entra no Cockpit primeiro pelo `kiro-cli`, e deve funcionar igual ao Claude Code. A plataforma tem de ficar aberta para novos agentes de forma relativamente simples: as regras do Cockpit ficam desacopladas do que cada agente implementa, com padrões de projeto que resolvam isso.

## Por quê

O usuário também trabalha com o `kiro-cli`. Hoje essas sessões não aparecem no board: não se ligam a cards, não guardam histórico, não recebem os avisos, não abrem pelo terminal do Cockpit e o gasto não é medido. A interface `internal/adapter.Adapter` foi pensada para Claude, Codex e Kiro, mas só o Claude existe, e vários pontos da plataforma têm `claude` escrito direto no código (`internal/api/terminals.go`, `continue.go`, `planlimits.go`, `overview.go`, `client.DetectAgentPID`, o reconciliador no `nobred`). Um segundo agente obrigaria a duplicar as regras; com um núcleo comum, o Kiro agora e o Codex depois são só um driver novo.

## Objetivos

1. Sessões do `kiro-cli` aparecem no board como as do Claude: criadas pelo hook, ligadas a card (`nobre start`/`resume`, `NOBRE_RESUME`), com prompts, arquivos lidos/editados e comandos registrados, branch/worktree acompanhados, encerradas pelo `SessionEnd` ou pelo reconciliador.
2. O agente Kiro recebe as mesmas instruções e avisos que o Claude: as instruções do `nobre` (equivalente à skill), o aviso de card/sessão registrada, os avisos de configuração, os avisos de contexto (60%/80%) e de saída grande.
3. Comandos `nobre` rodados pelo agente Kiro (note, decide, file, todo, ask, done...) acham a sessão dele.
4. Gasto do Kiro medido em **créditos**, por turno, somado por sessão, card, projeto e período, **separado do custo em dólares** (sem conversão).
5. Contexto do Kiro no medidor do NAC-40 em **porcentagem** da janela, com os mesmos destaques e avisos.
6. Abrir e continuar pelo terminal do Cockpit com o Kiro: a tela de nova tarefa escolhe o agente; "Continuar" e "Recomeçar limpo" usam o agente da última sessão do card.
7. Saúde e instalação: `nobre setup` instala a integração do Kiro quando o `kiro-cli` existe; a Configuração mostra o checklist de saúde do Kiro; `nobre setup --remover` desfaz.
8. Núcleo comum: as regras de sessão do Cockpit saem do adaptador do Claude para um núcleo que não conhece agente nenhum; Claude e Kiro viram drivers.

## Não objetivos

- Kiro IDE (editor). Fica para depois; o núcleo não deve impedir.
- Codex. O núcleo deve deixá-lo simples, mas ele não entra agora.
- Converter créditos em dólares, ou limites de plano do Kiro (créditos do mês contra a cota). Só a soma de créditos.
- Recursos do Claude sem equivalente no Kiro: status line (o Kiro já mostra os créditos no rodapé), importação de memórias, janelas duplas (sessão filha `<id>@<pid>`), RTK.
- Mudar o comportamento do Claude. A extração do núcleo é refatoração: o comportamento visível do Claude não muda.

## Anti-objetivos (seria falha mesmo funcionando)

- Regras de sessão duplicadas entre os drivers (duas cópias de "como ligar ao card", "quando avisar").
- Núcleo com `if agent == "claude"` / `"kiro"`: diferença de agente se expressa no driver ou numa capacidade opcional.
- Créditos somados com dólares, ou uma coluna "custo" que mistura as duas unidades.
- Regressão no Claude: a suíte atual do adaptador do Claude é a rede e tem de passar sem afrouxar testes.
- Exigir que o usuário confie em cada pasta no Kiro só para o Cockpit funcionar.

## Fatos verificados (teste com kiro-cli 3.4.0, V3, em 2026-11-23)

Sessões de teste rodadas numa pasta temporária, com hooks que só gravavam o que recebiam (headless `kiro-cli chat --v3 --no-interactive` e interativo `kiro-cli chat` via pty):

- **Hooks:** arquivo JSON `{"version":"v1","hooks":[{"name","trigger","matcher"?,"action":{"type":"command","command"},"timeout"}]}`. Triggers PascalCase. Dispararam: `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`; `SessionEnd` disparou no interativo ao sair com `/quit` (não no headless). `PreCompact`/`PostCompact` existem (CLI ≥ 3.1) e não foram exercitados.
- **Entrada do hook (stdin, JSON):** todos trazem `session_id` (`sess_<uuid>`), `hook_event_name`, `cwd`. `UserPromptSubmit` traz `prompt`. `PreToolUse` traz `tool_name` e `tool_input`; `PostToolUse` traz também `tool_response` (texto). `SessionEnd` traz `reason` (`"shutdown"`). Não há `transcript_path`.
- **Ferramentas (V3):** `read_file` (`tool_input.path`, `offset`, `limit`), `fs_write` (`path`, `text`), `execute_bash` (`command`, `cwd`...). A documentação cita também `str_replace`, `fs_append`, `delete_file`, `grep_search`, `file_search`, `execute_pwsh`, `run_command`. Ferramentas MCP: `mcp_<servidor>_<ferramenta>`.
- **Resposta do hook:** saída 0 → stdout entra no contexto do agente. Outro código → stderr vai ao agente como erro; no `PreToolUse` bloqueia a ferramenta. O `kiro-cli` não bloqueia `UserPromptSubmit`. Não há `additionalContext` em JSON documentado: o texto vai puro no stdout.
- **Ambiente:** o hook e os comandos de shell do agente recebem `KIRO_SESSION_ID=<session_id>`.
- **Processos:** interativo `kiro-cli` → `bun` → `node` (o `node` roda as ferramentas e os hooks); headless `kiro-cli` → `node`. O nome `kiro-cli` é o do processo do agente; `node` é genérico e não serve para identificar. No `SessionEnd` o `kiro-cli` já saiu (o `node` foi reparentado ao systemd).
- **Confiança na pasta:** o modo interativo pergunta "Trust this workspace?" antes de usar hooks/agentes da pasta (`.kiro/hooks` do projeto); a resposta fica em `~/.kiro/workspace-roots/<hash>/permissions.yaml`. Hooks globais ficam em `~/.kiro/hooks/` (documentação: valem junto com os do projeto).
- **Sessão em disco:** `~/.kiro/sessions/<hash da pasta>/<session_id>/` com `session.json` (`modelId`, `workspacePaths`, `createdAt`, `lastModifiedAt`, `title`...) e `messages.jsonl` (uma linha por evento: `{"id","timestamp","payload":{"type",...}}`). Tipos vistos: `session_start`, `user`, `turn_start`, `assistant`, `tool_call`, `tool_result`, `turn_end`, `usage_summary`, `session_metadata`, `session_event`, `sub_agent_start`/`sub_agent_complete`, `pending_interaction`/`interaction_resolved`. Índice em `~/.kiro/session-index/<hash>.jsonl`.
- **Gasto:** `usage_summary` por turno: `promptTurnSummaries[].usage` (número, `unit: "credit"`), `executionId`, `turnId`, `elapsedTime`, `usedTools`. Não há tokens nem dólares.
- **Contexto:** `session_metadata` com `key: "contextUsage"`, `value.usagePercentage` (porcentagem da janela, várias por turno). Não há contagem de tokens.
- **Lançar e retomar:** `kiro-cli chat` (interativo), `--resume-id <session_id>`, `--resume`, `--agent`, `--model`, `--trust-all-tools`/`--trust-tools`; a saída imprime `kiro-cli --resume-id <id>`.
- **Instruções permanentes:** `~/.kiro/steering/*.md` (globais, sempre incluídas por padrão) e `AGENTS.md`.

## Abordagens consideradas

- **A. Núcleo comum e drivers por agente (escolhida pelo usuário).** As regras de sessão saem do adaptador do Claude para um núcleo; cada agente traduz os próprios hooks e o próprio histórico para ele. Regras num lugar só e um caminho direto para o Codex. Custo: mexer no adaptador do Claude, que funciona, com a suíte atual como rede.
- **B. Adaptador do Kiro à parte, copiando o que precisa.** Mais rápido e sem tocar no Claude, mas duas cópias das regras divergem a cada correção; a paridade se perde com o tempo.
- **C. Kiro disfarçado de Claude** (converte o payload para o formato do Claude e reaproveita o adaptador). Pouco código, mas créditos, contexto em % e outro formato de histórico não cabem no molde; vira gambiarra.

## Desenho

### Portas e adaptadores

- **Núcleo de sessões** (pacote novo, p.ex. `internal/agentcore` — nome a critério do builder): as regras do Cockpit, sem conhecer agente. Recebe **eventos normalizados** e devolve **respostas normalizadas**:
  - eventos: `SessionStarted{source: startup|resume|clear...}`, `PromptSubmitted{text}`, `ToolUsed{fase: antes|depois, tipo: leitura|edição|comando|outro, path, command, exit_code, tamanho da saída}`, `TurnEnded`, `SessionEnded{reason}`, `Compacted` — cada um com id da sessão do agente, cwd, PID do agente, ambiente relevante (`NOBRE_RESUME`) e horário;
  - respostas: texto para o contexto do agente (aviso de card, de configuração, de contexto, de saída grande), bloqueio de ferramenta com motivo (o que o `PreToolUse` do Claude já faz hoje com os limites), segurar o fim do turno com motivo (o que o `Stop` do Claude já faz), nada.
  - Tudo o que hoje vive em `internal/adapter/claude/hooks.go`, `notice.go`, `alerts.go`, `nobrecmd.go` e que não depende do formato do Claude vai para o núcleo: criar/retomar/encerrar sessão, ligar ao card, eventos de prompt/arquivo/comando, acompanhar branch/worktree, montar e combinar avisos (com o estado em `session_alerts`), localizar o projeto.
- **Driver de agente** (um pacote por agente: `internal/adapter/claude`, `internal/adapter/kiro`): implementa peças pequenas, cada uma uma interface (segregação de interfaces), descobertas por asserção de tipo como já se faz com `StatusLiner`/`InstallPlanner`:
  - **Tradutor de hooks** (camada anticorrupção): `(evento, payload bruto) → evento normalizado`; mapeia nomes de ferramentas para leitura/edição/comando.
  - **Formatador de resposta:** resposta normalizada → stdout/código de saída do agente (Claude: JSON `hookSpecificOutput.additionalContext`/decisão; Kiro: texto puro no stdout, código ≠ 0 com stderr para bloquear).
  - **Leitor de uso:** histórico do agente → registros normalizados de uso (tokens e modelo, ou créditos) e de contexto (tokens, ou %), com cursor incremental. Claude: o leitor de transcript atual. Kiro: `messages.jsonl`.
  - **Instalador** (com plano/dry-run e remoção), **detector de saúde**, **lançador** (argv para novo e para retomar), **nomes de processo**, **variável de sessão** (Kiro: `KIRO_SESSION_ID`).
  - Opcionais, só no Claude: status line, memórias, janelas duplas (sessão filha), RTK.
- **Registro de agentes** (padrão registro): os drivers se registram no `nobred` e no `nobre`. Tudo que hoje assume `claude` pergunta ao registro: reconciliador (`board.Liveness.Names`), `nobre hook <agente>` (já roteia por agente em `POST /api/hooks/{agent}`), `client.DetectAgentPID` (nomes de todos os drivers), `/api/terminals`, `continue`/`restart-clean`, nomes e ordem de agentes no overview e em `/api/agents`, saúde/instalação na Configuração, alertas de integração.
- **Teste de contrato:** uma bateria única (sessão nasce, liga ao card por `NOBRE_RESUME`, prompt registrado, leitura/edição/comando viram eventos, aviso chega no formato do agente, sessão encerra) roda contra cada driver com fixtures do próprio agente. Driver novo = passar no contrato.

### Kiro

- **Instalação:** hooks globais num arquivo próprio do Cockpit em `~/.kiro/hooks/` (p.ex. `nobreai-cockpit.json`) chamando `nobre hook kiro <Evento>` para `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `Stop`, `SessionEnd`, `PostCompact`; instruções do `nobre` em `~/.kiro/steering/nobreai-cockpit.md` (conteúdo equivalente ao da skill do Claude, adaptado). Arquivos inteiros do Cockpit: instalar = escrever, remover = apagar; nada dos arquivos do usuário é editado. Primeira tarefa confirma que hooks globais rodam numa pasta não confiada; se não rodarem, o Cockpit avisa na saúde do Kiro (não instala nada por pasta).
- **Sessão:** id = `kiro:<session_id>`; PID do agente = ancestral `kiro-cli` (como `DetectAgentPID`). Comandos `nobre` do agente acham a sessão pelo PID; se o PID falhar, por `KIRO_SESSION_ID`. `SessionEnd` chega sem o `kiro-cli` vivo: encerra pela sessão do payload, sem depender do PID.
- **Uso e contexto:** depois de cada hook (como o Claude), o leitor acha `~/.kiro/sessions/*/<session_id>/messages.jsonl` e lê do último cursor: `usage_summary` → créditos do turno (chave: `turnId`/`executionId`); último `contextUsage.usagePercentage` do turno → contexto em %; `tool_result` → tamanho das saídas (para as maiores saídas). Modelo da sessão vem de `session.json`.
- **Avisos:** os mesmos textos do NAC-40. Faixas de contexto pela %; no Kiro a frase fica sem a contagem de tokens (ex.: "Contexto em 82% da janela. Numa pausa natural..."); o texto exato fica com o builder, curto, no mesmo tom. "Use /compact" vale no Kiro (`/compact` existe).
- **Lançar:** igual ao Claude: "Continuar", "Recomeçar limpo" e nova tarefa sempre abrem um processo novo (`kiro-cli chat --v3` com o prompt inicial) com `NOBRE_RESUME`, e o pacote de retomada chega pelo `SessionStart`. O Cockpit não usa `--resume-id` (o Claude também não retoma conversa). Sem flags de permissão: o Cockpit não tem configuração de modo de permissão e o Claude é lançado sem nenhuma; o Kiro pede aprovação das ferramentas como faz por padrão (nunca `--trust-all-tools`). Ligação ao card no `SessionStart` do Kiro: o payload não traz `source`; trata-se como início (`startup`).
- **Fim de turno (`Stop`):** as duas regras do Claude que seguram o fim do turno (cobrar o "Onde parei" com arquivos editados depois da última nota; lembrete único de arquivos editados sem card) valem para o Kiro. A documentação do Kiro V3 diz que um hook de `Stop` que sai com código 1 ou devolve `{"decision":"block","reason":"..."}` faz o agente continuar mais um turno; a primeira tarefa do driver confirma isso na prática. "Segurar o fim do turno" é uma resposta normalizada do núcleo; se o Kiro não a suportar, o driver declara a falta e o texto vai no próximo `UserPromptSubmit` (fica no teste de contrato como capacidade opcional).
- **Versão:** só o harness V3 (`kiro-cli` ≥ 3.1). O lançador passa `--v3`; a saúde do Kiro acusa versão antiga. Sessões V1/V2 ficam fora (outros nomes de ferramenta e outro formato de histórico).
- **Limites de gasto do Cockpit:** os limites atuais são em dólares (diário por agente e por card, com ação "pausar" que bloqueia ferramentas no `PreToolUse`). Valem só para sessões cujo gasto é em dólares: o Kiro não é bloqueado por um limite em dólares, nem pelo do card que o Claude estourou. Limites em créditos ficam fora (como as cotas do plano).

### Dados

- Créditos: coluna própria (`credits REAL`) nos registros de uso; somas de créditos separadas das de dólares nas APIs de uso/analytics, overview, card, projeto e sessão. Linhas do Claude ficam com `credits = 0`; linhas do Kiro, com tokens 0 e sem preço.
- Contexto: as amostras de contexto aceitam % sem tokens (coluna `pct`); o medidor do NAC-40 usa a % quando não há tokens e mostra "janela: informada pelo agente". Crescimento por turno e turnos até o limite, em pontos percentuais.
- Migração nova depois das do NAC-40 (a 017 é dele); conferir a numeração com o `main` no momento do plano.

### Telas

- Onde hoje aparece custo, o Kiro aparece com "N créditos" ao lado (nunca somado aos US$). Totais por projeto/período mostram as duas grandezas quando houver as duas.
- Nova tarefa: seletor de agente (Claude, Kiro) quando os dois estão instalados; padrão nas configurações.
- "Continuar" (`ContinuePanel`): já tem "Continuar com outro agente"; o seletor fica, com o agente da última sessão do card como padrão.
- Configuração: seção de saúde/instalação do Kiro como a do Claude.

## Ordem

1. Mesclar o NAC-40 no `main` (mexe nos mesmos arquivos do Claude).
2. Extrair o núcleo e o registro com o Claude como único driver; suíte atual passando; teste de contrato rodando contra o Claude.
3. Driver do Kiro (tradutor, formatador, instalador, saúde, lançador, leitor de uso), passando no contrato.
4. Créditos e contexto em % nos dados, APIs e telas; seletor de agente.
5. Revisão, merge, instalar e conferir ao vivo com uma sessão real do `kiro-cli`.

## Decisões tomadas na revisão do design (por Claude, para o usuário conferir)

- Lançamento sem `--resume-id` e sem flags de permissão (paridade com o Claude).
- `Stop` do Kiro segura o fim do turno como o do Claude, se o Kiro suportar; senão o texto vai no próximo prompt.
- Só V3; o lançador passa `--v3`.
- Limites em dólares não bloqueiam o Kiro; sem limites em créditos por ora.
- "Continuar" mantém o seletor de agente, com o da última sessão como padrão.

## Fica com o builder

- Nomes de pacotes, tipos e arquivos; a fronteira exata entre núcleo e driver, desde que respeite os anti-objetivos.
- Formato exato dos eventos normalizados e como o núcleo guarda estado por sessão.
- Texto exato do aviso de contexto do Kiro e das instruções em `steering`.
- Como mostrar "créditos" em cada tela, dentro da regra "separado dos dólares".
