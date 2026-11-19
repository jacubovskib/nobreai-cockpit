# Integração com o RTK (NAC-34)

Escolhido pelo usuário em 2026-11-18: "opção 2 completa" (Cockpit gerencia o RTK, com monitor e antes/depois por chave), depois de ver a medição real de quanto o RTK economiza nos comandos dele.

## Por quê

A medição do NAC-31 mostrou que as saídas de terminal pesam ~1,07 bi de tokens de releitura em 30 dias. O RTK (Rust Token Killer, github.com/rtk-ai/rtk) comprime a saída de comandos Bash antes de o modelo ver. O usuário quer o RTK ativo e acompanhado pelo Cockpit, do mesmo jeito que o Cockpit já acompanha hooks, status line e skill: sabe se está instalado, ativa e desativa com confirmação, avisa se quebrou e mostra quanto economizou, de forma comparável ao ranking "O que enche o contexto".

## Fatos verificados (rtk 1.2.0, instalado pelo usuário com `cargo install --git https://github.com/rtk-ai/rtk --branch master rtk`, em `~/.cargo/bin/rtk`)

- `rtk --version` → `rtk 1.2.0`.
- `rtk init -g --hook-only --auto-patch` só acrescenta um hook `PreToolUse` com matcher `"Bash"` no `~/.claude/settings.json` (dry-run confirma: sem `RTK.md`, sem mexer no `CLAUDE.md`, sem pergunta). `rtk init -g --uninstall` remove hook, `RTK.md`, referência no `CLAUDE.md` e a entrada do settings. `--dry-run` existe nos dois.
- O hook é `rtk hook claude`: lê o JSON do PreToolUse e, quando reescreve, responde `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecisionReason":"RTK auto-rewrite","updatedInput":{...,"command":"cd /tmp && rtk git status"}}}`; quando não reescreve, não imprime nada (exit 0). Nunca dá `permissionDecision`. O hook do Cockpit no PreToolUse só nega (limite de custo) e não usa `updatedInput`: os dois convivem.
- `rtk rewrite "<cmd>"` imprime a reescrita (exit 3) ou nada (exit 1). `cat f` → `rtk read f` (nível padrão `none`: conteúdo idêntico), `head -50 f` → `rtk read f --head-lines 50`; `sed -n`, `python3` não são reescritos; compostos reescrevem cada parte (`cd /x && git status` → `cd /x && rtk git status`, `cat f | grep y` → `cat f | rtk grep y`).
- Economia medida nos comandos do usuário: `grep -rn` 153 KB → 21 KB; `git log -20` 6,0 → 3,6 KB; `go test -v` 1,9 KB → 33 B; `cat` de .go/.md: 0%.
- `rtk gain --daily --format json` → `{"summary":{"total_commands","total_input","total_output","total_saved","avg_savings_pct","total_time_ms","avg_time_ms","oversized_excluded"},"daily":[{"date":"AAAA-MM-DD","commands","input_tokens","output_tokens","saved_tokens","savings_pct","total_time_ms","avg_time_ms"}]}`. Tokens do RTK = bytes/4, igual à estimativa do Cockpit. Histórico em `~/.local/share/rtk/history.db`, 90 dias.
- O transcript do Claude Code guarda o `tool_use` original do modelo (sem `rtk`) e o `tool_result` já comprimido; o `BashKey` do NAC-31 já ignora o prefixo `rtk`. As chaves do ranking não mudam com o RTK; só o tamanho das saídas.

## Objetivos

1. **Bloco "RTK" na Configuração**, junto dos agentes:
   - Sem binário: "RTK não instalado" e o comando de instalação para copiar (o `cargo install` acima) e o link do projeto.
   - Com binário: versão, caminho e se está ativo no Claude Code.
   - Botões "Ativar no Claude Code" e "Desativar", cada um com confirmação que diz exatamente o que muda ("acrescenta um hook PreToolUse (Bash) no ~/.claude/settings.json" / "remove o hook do RTK e os arquivos que o RTK criou no ~/.claude"). Ativar roda `rtk init -g --hook-only --auto-patch`; desativar roda `rtk init -g --uninstall`. Saída do comando aparece no resultado, como no "Instalar o que falta".
   - Cada ativação/desativação entra no histórico de configuração (config_log) e guarda a data de ativação.
2. **Monitor de integrações** reconhece o RTK: quando o hook do RTK já funcionou e some (ou o binário some) fora do Cockpit, aparece o alerta, com ação "Reativar". Desativar pelo Cockpit não alerta (marca de remoção deliberada, como a status line). Quem nunca ativou nunca recebe alerta.
3. **"Economia do RTK" no Analytics**, logo acima de "O que enche o contexto", no mesmo período:
   - Totais do `rtk gain` no período: comandos, tokens economizados, % médio (dias do `daily` dentro de [from, to)).
   - "Antes e depois" por chave: para as chaves Bash com chamadas suficientes dos dois lados da data de ativação (mínimo 5 de cada lado), média de tokens de saída por chamada antes e depois e a variação. Só aparece quando há data de ativação.
   - Sem RTK instalado/ativo: o bloco mostra uma linha curta com o atalho para a Configuração.
   - Nota curta: o RTK não comprime `cat` (lê o arquivo inteiro) nem reescreve `sed`/`python3`.

## Não objetivos

- O Cockpit não baixa, instala nem atualiza o binário do RTK.
- Sem `RTK.md` nem instruções no `CLAUDE.md` (ativação só com o hook, para não pôr mais texto no contexto).
- Sem filtros próprios, sem mudar o nível do `rtk read`, sem tratar `sed`/`python3` (fica para uma regra na skill depois, se os números pedirem).
- Sem suporte a outros agentes do RTK (só Claude Code).
- Não filtra o projeto: o RTK é global ao usuário; o "Antes e depois" usa o filtro de projeto da página só sobre os dados do Cockpit; os totais do `rtk gain` são globais (dito na tela).

## Restrições

- O daemon roda como unidade do systemd com PATH mínimo: achar o `rtk` no PATH do usuário (`launch.UserPATH`, como o CLI do Claude) e, de reserva, em `~/.cargo/bin/rtk` e `~/.local/bin/rtk`.
- Rodar o `rtk` com timeout (5 s para versão/gain, 20 s para init) e com `HOME` do usuário; nunca bloquear um hook nem a resposta de outra rota.
- O `rtk gain` em cache curto (60 s) para não rodar a cada refetch da tela.
- Detectar o hook ativo lendo o `settings.json`: entrada em `hooks.PreToolUse` com matcher que casa `Bash` e algum comando contendo `rtk hook claude` (aceita qualquer caminho do binário).
- Ações que escrevem no `~/.claude` só com confirmação explícita na tela (padrão do NAC-6).
- Textos em português, curtos.

## Abordagens consideradas

1. **RTK por fora, Cockpit só mede**: rápido, mas o Cockpit não sabe se o RTK está ativo nem avisa se quebrou. Descartada pelo usuário.
2. **Cockpit gerencia o RTK (escolhida, completa)**: detecção, ativar/desativar com confirmação, monitor, economia e antes/depois.
3. **Compressor próprio no PreToolUse**: reinventa o RTK. Descartada.

Ajuste depois do bloqueio de download: o Cockpit não baixa binário; o usuário instala e o Cockpit só roda o que já está instalado, quando mandado.

## Decisões fechadas depois da leitura do revisor (minhas, para conferir)

1. **Fonte do antes/depois:** `tool_outputs.tokens` do NAC-31, estimado do `tool_result` do transcript (o que o modelo recebeu, já comprimido quando o RTK atuou), mesma fonte para linhas antigas e novas. A consulta é média de `tokens` por chamada dividida pela data; nada novo a gravar nas saídas.
2. **Períodos de ativação:** guardar uma lista de intervalos `[{on, off}]` (KV `rtk_activations`; `off` vazio = ativo). O Claude Code lê o settings ao abrir a sessão, então "depois" = saídas de sessões que começaram com o RTK ativo (`sessions.started_at` dentro de um intervalo) e "antes" = saídas com `at` antes da primeira ativação. Desativar fecha o intervalo; reativar abre outro. Hook encontrado já ativo sem intervalo (ativado à mão) abre um intervalo na primeira vez que o Cockpit o vê.
3. **Monitor:** chave nova `rtk` na saúde do adaptador Claude (OK = hook no settings e binário encontrado). Usa a linha de base do monitor: "funcionou" = foi visto OK ao menos uma vez (inclusive ativado à mão). Hook presente e binário sumido → código `dead_binary`, texto com o comando de reinstalação, ação `config` (leva à Configuração). Hook sumido → código `missing`, ação nova `rtk` ("Reativar", roda a ativação). Desativar pelo Cockpit chama `Removed(["rtk"], "config")` antes de rodar o comando.
4. **Histórico e "Instalar o que falta":** o RTK não entra no "Instalar o que falta"; tem só os próprios botões. Ativar/desativar gravam no config_log com tipos novos `rtk_on`/`rtk_off` (com a versão e a saída resumida).
5. **Onde procurar o hook:** só `~/.claude/settings.json` (é onde o `rtk init -g` escreve). Arquivo ausente ou inválido = inativo, com o detalhe na tela. A confirmação mostra a saída do `--dry-run` do próprio comando (em vez de texto fixo), para refletir o que realmente vai mudar.
6. **Totais do `rtk gain` no período:** linhas do `daily` filtradas pela data local (AAAA-MM-DD) dentro de [from, to); % recalculado como Σsaved / Σinput. Se o período começa antes do dia mais antigo que o RTK guarda (ele guarda 90 dias), a tela diz "o RTK guarda 90 dias".
7. **Dados da tela:** rota nova `GET /api/rtk` (estado: instalado, versão, caminho, ativo, intervalos) e `GET /api/rtk/savings?from=&to=&project=&exact=1` (totais do gain + antes/depois). Gain falhou ou expirou → totais "indisponível" e o antes/depois aparece mesmo assim. O bloco renderiza independente do ranking de contexto.
8. **Tabela antes/depois:** só `tool = Bash`; chave = a do ranking; mínimo 5 chamadas de cada lado; ordenada pelo total de chamadas, até 15 linhas; variação = % da média de tokens por chamada; mostra também chaves que o RTK não reescreve (`cat` ≈ 0%), com a nota.
9. **Segurança do settings.json:** antes de rodar `rtk init`, o Cockpit copia o `settings.json` para `settings.json.bak-rtk-<data>` (mantém os 3 mais recentes). Timeout ou erro: a tela mostra a saída e onde está a cópia; o Cockpit não restaura sozinho. Rodar de novo é seguro (o `rtk init` é idempotente).

## Deixado ao implementador

Nomes de rotas e tipos, layout fino dos blocos, onde guardar a data de ativação (settings KV), formato exato do texto do alerta.
