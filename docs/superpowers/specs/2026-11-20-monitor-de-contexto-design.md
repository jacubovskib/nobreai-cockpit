# Monitor de contexto e sessão (NAC-40)

Combinado com o usuário em 2026-11-20: monitor de contexto e sessão com as boas práticas atuais, mais os acabamentos da sessão filha (NAC-39). Princípio pedido: não aumentar custo — nada entra no contexto do agente sem ser medido, toda injeção tem teto, recurso que chama modelo é opcional (aqui não há nenhum).

## Por quê

Nos últimos 30 dias o contexto somou ~3,5 bi de tokens, quase tudo releitura de cache (NAC-31). O custo cresce com o tamanho do contexto × número de turnos, com regravações de cache depois de pausa longa e com saídas grandes que ficam sendo relidas. O Cockpit já mede o contexto turno a turno (`context_turns`), mas só olhando para trás.

## Boas práticas que o monitor aplica

- Sessões focadas: `/clear` entre tarefas; o card guarda o histórico, então recomeçar não perde nada.
- Compactar numa pausa natural (`/compact` dizendo o que preservar) é melhor do que a compactação automática no fim da janela.
- Cache de prompt: releitura custa ~10% da entrada; regravar custa 125% (cache de 5 min) ou 200% (cache de 1 h). Pausa maior que o tempo de vida do cache faz o próximo turno regravar tudo.
- Leitura sob demanda: trechos com offset/grep em vez de arquivo inteiro.
- Subagentes para explorar: só o resumo volta ao contexto principal.

## Fatos verificados nos dados do usuário

- `claude-opus-5-5` chegou a 700k de contexto: janela de 1M, não 200k.
- `usage.cache_creation` traz `ephemeral_1h_input_tokens` (ou `ephemeral_5m_input_tokens`): o tempo de vida do cache pode ser lido por turno.
- O tamanho do contexto de cada turno já está em `context_turns` (NAC-31), gravado pela leitura assíncrona do transcript depois de cada hook.

## Objetivos

1. **Medidor ao vivo por sessão** (Sala de controle, card e um detalhe por sessão):
   - contexto atual (último turno) sobre a janela do modelo, em tokens e %;
   - crescimento médio por turno nos últimos 10 turnos e turnos estimados até a compactação automática;
   - turnos desde a última compactação e número de compactações;
   - taxa de acerto do cache (cache lido / (entrada + cache gravado + cache lido)) e regravações grandes recentes;
   - tempo parado desde o último evento, comparado ao tempo de vida do cache da sessão;
   - as 5 maiores saídas da sessão (do NAC-31).
2. **Janela do modelo:** tabela de janelas por modelo na Configuração (padrão 200.000; `[1m]` no nome ou contexto observado acima de 200k na sessão → 1.000.000), editável.
3. **Avisos ao agente (via hook, curtos, uma vez por faixa):**
   - no `UserPromptSubmit`, quando o contexto cruza 60% e 80% da janela pela primeira vez na sessão (e de novo só depois de uma compactação): uma frase, ex. "Contexto em 82% da janela (820 mil tokens). Numa pausa natural, registre onde parou com nobre note e use /compact dizendo o que preservar, ou recomece limpo pelo card.";
   - no `PostToolUse`, quando uma saída passa de 15.000 tokens estimados: "Essa saída teve ~22 mil tokens e fica sendo relida a cada turno. Prefira ler trechos (offset/limit, grep) em vez do arquivo inteiro." — no máximo 3 vezes por sessão.
   - Os textos dos avisos contam no peso do Cockpit no Analytics (já medido como hook).
4. **Avisos ao usuário (tela):** a Sala de controle destaca sessões acima de 80%, com regravação grande recente ou paradas além do tempo de vida do cache com contexto grande (> 200k).
5. **Recomeçar limpo:** botão no card e no medidor. Pede (ou usa) o "Onde parei", encerra o vínculo da sessão atual com o card e abre um terminal novo no mesmo card com o pacote de retomada (o fluxo "Continuar" que já existe). Não fecha o terminal antigo sozinho (o usuário fecha).
6. **Acabamentos da sessão filha (NAC-39):**
   - custo por janela: atribuir cada mensagem do transcript compartilhado à janela certa pelo galho de `parentUuid` a partir do prompt de cada janela (o hook de prompt traz texto e PID; a linha `user` com esse texto é a raiz do galho daquela janela);
   - aviso "outra janela" persistido (não se perde se o daemon reiniciar);
   - sessão filha localizada pelo próprio cwd ao nascer;
   - reconciliador conferindo o nome do processo (`claude`), como o roteamento.
7. **Configuração:** limites dos avisos (60/80%, 15.000 tokens, 3 avisos) e liga/desliga dos avisos ao agente.

## Não objetivos

- Compactar ou limpar sozinho (o agente ou o usuário decide).
- Chamar modelo para resumir ou criticar.
- Medidor dentro da status line do Claude Code (pode vir depois).

## Restrições

- Nenhum texto novo no contexto além dos avisos acima, cada um com teto de vezes.
- Hooks continuam rápidos: o aviso de contexto usa o último turno já gravado; o de saída grande usa o tamanho do `tool_response` do próprio hook.
- Português, curto.

## Abordagens

1. **Medir pelo que o Cockpit já grava + avisos por hook (escolhida):** sem custo extra de modelo; reaproveita `context_turns`, `tool_outputs` e o "Continuar".
2. **Status line do Claude Code como fonte:** a status line recebe o JSON da sessão a cada atualização, mas só existe com a integração ligada e não alcança o agente. Fica como complemento futuro.
3. **Compactação automática pela plataforma:** contraria "o agente ou o usuário decide" e pode perder contexto útil. Descartada.

## Decisões fechadas depois da leitura do revisor (minhas, para conferir)

1. **Dados de cache:** `context_turns` ganha `input`, `cache_read`, `cache_write`, `ttl` (`1h`, `5m` ou vazio) e `window_session_id` (a sessão/janela dona do turno). Para preencher o histórico, o leitor ganha uma versão: ao subir com a versão nova, zera o `tool_offset` de todos os transcripts e relê tudo uma vez (a passada do NAC-31 levou ~25 s). "Regravação grande" = turno com `cache_write` ≥ 50.000 tokens; "recente" = últimos 60 minutos.
2. **Entrega dos avisos:** resposta JSON `hookSpecificOutput.additionalContext` no `UserPromptSubmit` (o mesmo formato que os avisos de configuração já usam em `notice.go`, combinando os dois quando coincidirem) e no `PostToolUse`. O tamanho da saída no `PostToolUse` = bytes do `tool_response` serializado ÷ 4, para qualquer ferramenta.
3. **Estado dos avisos:** persistido por sessão (sobrevive a reinício): faixa já avisada (60/80) com o segmento de compactação em que foi avisada e contador de avisos de saída grande. Compactação = segmento novo no transcript (NAC-31): as faixas voltam a valer. É por janela: sessão filha tem o próprio estado.
4. **Medidor por janela:** o contexto de cada sessão é o último turno do transcript principal (sem subagentes) atribuído àquela janela. A atribuição por galho de `parentUuid` (objetivo 6) vem primeiro e vale para turnos, saídas e uso: cada linha herda a janela do pai; a raiz de cada galho é a linha `user` cujo texto bate com o prompt recebido no hook daquela janela (texto + PID + horário, tolerância de alguns segundos). Linha sem dono conhecido (resumo de compactação, linhas antes do primeiro prompt, conversa sem segunda janela) fica com a sessão principal.
5. **Recomeçar limpo:** abre um diálogo com o "Onde parei" atual para confirmar ou editar (obrigatório se estiver vazio ou com mais de 30 minutos); salva; desliga o card da sessão atual (a sessão continua viva, sem card) e põe o card em pausado; chama o "Continuar" que já existe (mesmo agente e modo) para abrir o terminal novo com o pacote de retomada. Vale para sessão principal ou filha (cada uma com o seu card).
6. **Janelas por modelo:** tabela nas configurações (`model_windows`: modelo → tokens), na seção de preços da Configuração. Padrão: `claude-opus-5-5` → 1.000.000; demais `claude-*` → 200.000; `[1m]` no nome → 1.000.000. Contexto observado acima da janela da tabela numa sessão passa a valer 1.000.000 para aquela sessão (fixo depois de visto). Modelo fora da tabela usa 200.000 e a tela diz "janela estimada".
7. **Avisos na tela (fixos nesta entrega):** acima de 80%; regravação ≥ 50.000 tokens nos últimos 60 min; parada além do tempo de vida do cache (1 h se o último turno gravou cache de 1 h, senão 5 min) com contexto acima de 200.000.
8. **Pequenos:** "parado há" conta do último evento de hook; crescimento = média por turno desde a última compactação, só com 3 ou mais turnos (senão "—"); o detalhe por sessão fica no card (seção) e numa linha expansível da Sala de controle, sem rota nova.

## Deixado ao implementador

Nomes de rotas e tabelas, layout do medidor, onde guardar as faixas já avisadas por sessão.
