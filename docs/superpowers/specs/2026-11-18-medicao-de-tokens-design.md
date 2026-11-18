# Medição de tokens: o que enche o contexto (NAC-31)

Aprovado pelo usuário em 2026-11-18 ("comite tudo ... depois implemente", sobre o desenho apresentado).

## Por quê

Nos últimos 7 dias o uso foi de ~2,26 bilhões de tokens de cache lidos contra ~8,6 milhões de saída. Quase todo o custo é o modelo relendo o contexto a cada turno. Antes de cortar (RTK, enxugar o que o Cockpit injeta) é preciso saber o que entra no contexto e quanto cada coisa custa em releituras. Esta medição também é o "antes" para comparar com o "depois" do RTK.

## Objetivos

- Medir, por chamada de ferramenta, o tamanho da saída que o modelo recebeu.
- Medir o contexto injetado por hooks (separando o do Cockpit).
- Calcular o peso de cada saída: tamanho × releituras (turnos seguintes até a próxima compactação).
- Mostrar no Analytics: ranking por chave, sessões que mais pesaram e, por sessão, a curva do contexto com as compactações e as 10 saídas mais pesadas.
- Cobrir o histórico já existente (todas as sessões com transcript no disco).

## Não objetivos

- Não guardar conteúdo de saída nem argumentos de comando (só números e uma chave curta).
- Não reduzir nada ainda (RTK e enxugar a injeção vêm depois).
- Não medir sessões que o Cockpit nunca registrou.
- Sem página nova de sessão: a curva por sessão fica no Analytics, em "Sessões que mais pesaram".

## Abordagens

1. **Pelo transcript (escolhida).** Lê os blocos `tool_result` dos transcripts que o Cockpit já ingere. É o que o modelo viu e permite medir o passado.
2. **Pelo hook PostToolUse.** Mede na hora, só daqui para frente, e o tamanho do hook pode diferir do que o modelo recebe. Descartada.

## Formato do transcript (verificado)

- `type:"assistant"`: `message.id`, `message.usage` (input, cache_read, cache_write), `message.content[]` com blocos `tool_use` (`id`, `name`, `input`). Uma mensagem pode vir em várias linhas com o mesmo `message.id`.
- `type:"user"`: `message.content[]` com blocos `tool_result` (`tool_use_id`, `content` string ou lista de blocos `text`/`image`/`tool_reference`, `is_error`).
- `type:"system"`, `subtype:"compact_boundary"`: compactação.
- `type:"attachment"`, `attachment.type:"hook_success"`: `hookEvent`, `command`, `stdout` (JSON com `hookSpecificOutput.additionalContext`, ou texto puro). `attachment.type:"hook_blocking_error"`: `hookEvent`, `blockingError` (o bloqueio do Stop do Cockpit vira contexto).
- Linhas têm `uuid` e `timestamp`.

## Definições

- **Turno:** cada `message.id` distinto de assistant com usage e com algum token (mensagens de 0 token, as sintéticas, não contam, como na ingestão de uso), em ordem, por arquivo (principal ou subagente). Contexto do turno = input + cache_read + cache_write (a última linha do id vence).
- **Segmento:** quantas compactações (`compact_boundary`) já passaram no arquivo. Turnos e saídas guardam o segmento em que apareceram.
- **Saída:** um `tool_result` (chave por `tool_use_id`) ou uma injeção de hook (chave pelo `uuid` da linha). Fica ligada ao último turno visto antes dela (`turn`; -1 se nenhum) e ao segmento atual.
- **Entrada da ferramenta:** o tamanho do `input` do `tool_use` (Write, Edit e prompts de Agent são grandes na entrada e pequenos na saída). Fica na mesma linha da saída (`in_tokens`).
- **Tokens estimados:** ceil(bytes de texto / 4); cada bloco `image` conta 1600.
- **Releituras:** turnos do mesmo arquivo e do mesmo segmento da saída com índice > `turn`, isto é, MAX(turn do segmento) − `turn`, ou 0 se o segmento não tem turno depois. Saída logo depois de uma compactação (a injeção do SessionStart pós-compactação) conta os turnos do segmento novo.
- **Peso:** (tokens + in_tokens) × releituras.
- **Resto:** Σ do contexto dos turnos no filtro menos Σ dos pesos: o prompt de sistema, listas de ferramentas e skills, prompts do usuário e respostas. Aparece como linha própria para o ranking fechar com o total.

## Chave (sem conteúdo)

- Bash: programa (basename do primeiro token do primeiro comando, pulando `cd X &&`, `export …`, atribuições `VAR=x` e os invólucros `sudo`, `env`, `time`, `timeout N`, `nice`, `nohup` e `rtk`, para o antes/depois do RTK cair na mesma chave) e, só para programas com subcomando conhecido (git, go, npm, pnpm, yarn, npx, docker, kubectl, cargo, make, gh, nobre, systemctl, pip, uv, bun, deno, terraform, helm), o subcomando quando for uma palavra simples. Programa fora de `^[A-Za-z0-9._+-]{1,32}$` vira `outro`.
- Read: `Read .go` (extensão do arquivo; `Read sem extensão`).
- Task/Agent: `Agent <subagent_type>` (`Agent geral` se vazio).
- Skill: `Skill <nome>`.
- Demais ferramentas: o nome da ferramenta.
- Hook: `<hookEvent> · cockpit` quando o `command` contém `nobre hook`, senão `<hookEvent> · outros`; bloqueio: `<hookEvent> · bloqueio`. Tamanho = `hookSpecificOutput.additionalContext` do stdout JSON; stdout em texto puro só conta em SessionStart e UserPromptSubmit (verificado: cada hook tem seu próprio `hook_success`, o do Cockpit separado do superpowers).

## Armazenamento

- `tool_outputs`: session_id, path, item_id, tool, key, bytes, tokens, in_tokens, turn, segment, at, is_error. Chave (session_id, item_id).
- `context_turns`: session_id, path, turn, segment, message_id, at, tokens. Chave (session_id, path, turn).
- `transcript_offsets` ganha `tool_offset` (padrão 0) e `segment` (padrão 0): o leitor de saídas tem offset próprio. Com padrão 0, todo arquivo já conhecido é relido do começo uma vez: isso é o histórico.
- Arquivo menor que o `tool_offset` (truncado/trocado): apaga as linhas daquele (sessão, path) e relê do começo.

## Leitura

- Junto com a leitura de uso que já roda em segundo plano depois de cada hook, para o principal e os subagentes.
- Leitura em fluxo (linhas grandes), só até a última linha completa.
- Ao subir o daemon, uma passada em segundo plano percorre todas as sessões com transcript e lê o que falta (sob a mesma trava por sessão). Para entre sessões quando o daemon desliga (contexto cancelado) e entra no WaitGroup do adaptador, para o banco não fechar no meio de uma escrita. Nas subidas seguintes não há nada novo para ler.

## API

- `GET /api/context/top?from=&to=&project=&exact=1`: `keys` (tool, key, count, tokens, in_tokens, avg_tokens, weight; ordenado por weight, até 30), `sessions` (até 10 por weight: id, card key/título, started_at, peak_context, compactions, weight), `projects` (por projeto: project_id, weight, top_key) e os totais `context_total` (Σ contexto dos turnos) e `weight_total` (Σ pesos de todas as chaves). Projeto como no uso: o do card da sessão, senão o da sessão; família expandida salvo `exact=1`.
- `GET /api/context/sessions/{id}`: `turns` do transcript principal (turn, at, tokens, after_compact) e `top` (10 saídas por weight: tool, key, tokens, rereads, weight, at, subagent).

## Tela (Analytics)

- Seção "O que enche o contexto", no mesmo período e filtro de projeto/app da página: tabela Chave · Chamadas · Entrada · Saída · Média · Peso (com releituras), e no fim a linha "Resto (base e conversa)" com context_total − weight_total. Hooks do Cockpit aparecem como linhas `… · cockpit`.
- "Por projeto" (só com o filtro em Todos): peso total e a chave que mais pesou.
- "Sessões que mais pesaram": lista; clicar abre a curva do contexto por turno (SVG, marcas nas compactações) e as 10 saídas mais pesadas.
- Textos curtos em português; números de tokens no formato já usado (`tokens()` de analytics/data).

## Deixado ao implementador

Nomes internos, layout fino da tabela e do gráfico, ordem dos índices.
