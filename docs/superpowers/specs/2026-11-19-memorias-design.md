# Memórias do agente no Cockpit (NAC-38), primeira entrega

Combinado com o usuário em 2026-11-19: o Cockpit guarda as memórias (opção 1), com as melhorias sobre o napkin listadas na conversa; primeira entrega = escopos com herança, seleção com teto e reinjeção na compactação, `nobre lembrar` e `nobre memorias`, página "Memórias" (editar, ordenar, arquivar, duplicadas), importação dos `napkin.md` com triagem e candidatas vindas das decisões de card ao concluir. Segunda entrega (fora daqui): candidatas por erros repetidos, marcação de memória velha, pedido no fim de sessão.

## Por quê

O napkin (skill `napkin`, arquivo `.claude/napkin.md` por repositório) dá ao agente regras curtas e reaproveitáveis ("faça assim"), mas:
- o agente lê o arquivo inteiro em toda sessão e o relê a cada turno no contexto; os arquivos do usuário vão de 1 KB a 239 KB (13 arquivos; bravonix 49 KB/650 linhas), apesar do teto de 10 por categoria;
- a curadoria depende do agente, toda sessão, pagando tokens e sem cumprir o teto;
- a mesma regra se repete em vários repositórios (pt-BR, aspas no zsh);
- depois de uma compactação o conteúdo some até alguém reler.

O Cockpit já injeta contexto no início de cada sessão registrada e mede o peso disso (NAC-31). Com as memórias nele, a plataforma escolhe o que entra, com teto, por contexto, e a curadoria passa para a tela.

## Objetivos

1. **Memória** = regra curta + "faça assim" + categoria + escopo + ordem + datas. Categorias (as do napkin): `execucao` (Execução e validação), `shell` (Shell e comandos), `dominio` (Regras do domínio), `diretiva` (Diretivas do usuário).
2. **Escopos com herança:** `global` (todas as sessões registradas), ou um projeto (que pode ser grupo, projeto solto ou app filho). Uma sessão recebe: globais + as do projeto principal da família (grupo/raiz) + as do projeto/app da sessão. (Família = `FamilyIDs`/`parent_id` do NAC-23/27.)
3. **Injeção no SessionStart** (início, resume, clear e compactação) de sessões com captura completa: as memórias ativas que valem para a sessão, dentro de um teto (padrão 1500 tokens estimados = bytes/4; ajustável na Configuração). Ordem de escolha: escopo mais específico primeiro (app → raiz → global), dentro do escopo a ordem manual; entra inteira ou não entra; ao estourar o teto, as que ficaram de fora são contadas na linha final ("+N memórias: nobre memorias <termo>"). Bloco compacto, uma linha por memória: `- [shell] <regra> → <faça assim>`.
4. **CLI:**
   - `nobre lembrar "<regra>" --faca "<ação>" [--cat execucao|shell|dominio|diretiva] [--escopo projeto|raiz|global]`: grava ativa, no projeto da sessão (padrão `projeto` = o projeto/app da sessão; `raiz` = o principal da família; `global`). Sem sessão registrada, recusa como os outros comandos. Avisa se parecer duplicada de outra (mesma regra normalizada ou semelhança alta) e grava mesmo assim.
   - `nobre memorias [termo]`: lista as ativas que valem para a sessão (todas, não só as do teto), filtrando por termo na regra/ação; mostra id, escopo e categoria.
5. **Página "Memórias"** (menu lateral), no filtro de projeto/app da página:
   - **Fila**: candidatas (importadas e de cards) com Aprovar / Descartar, individual e em lote, editando antes de aprovar.
   - **Ativas** por categoria: editar regra/ação/categoria/escopo, subir/descer, arquivar; indicação "duplicada de #N" quando o texto se parece com outra ativa do mesmo alcance; quantas entram no teto da próxima sessão daquele projeto (prévia do que será injetado, com o tamanho).
   - **Arquivadas**: restaurar ou apagar.
   - Botão "Importar napkin.md".
6. **Importação dos `napkin.md`:** para cada projeto cadastrado com `<caminho>/.claude/napkin.md`, lê (só leitura; nunca altera o arquivo) os itens no formato do napkin (`## <categoria>` + `1. **[data] regra**` + `Do instead: ação`), mapeia a categoria pelo título (Execution/Validation → execucao, Shell/Command → shell, Domain → dominio, User Directives → diretiva, outros → dominio) e cria **candidatas** no projeto, com a data original. Item sem "Do instead" vira candidata com ação vazia (editar antes de aprovar). Reimportar não duplica (mesma regra normalizada no mesmo projeto é ignorada).
7. **Candidatas de cards:** quando um card vai para Concluído, cada decisão dele (`nobre decide`, com o porquê) vira candidata no projeto do card (regra = decisão, ação = porquê, categoria `dominio`, origem = chave do card). Descartar uma candidata não a recria.
8. **Napkin nos repositórios registrados:** o texto injetado diz "Neste projeto as memórias ficam no Cockpit: use nobre lembrar e nobre memorias; não leia nem edite .claude/napkin.md." A skill `napkin` continua valendo fora do Cockpit. A skill `nobre-cockpit` ganha uma seção curta sobre quando gravar memória (regra recorrente, armadilha que se repete, diretiva do usuário; nunca log de sessão).

## Não objetivos

- Candidatas por erros repetidos, marcação de memória velha, pedido no fim de sessão (segunda entrega).
- Busca semântica/embeddings; a semelhança é textual.
- Editar ou apagar os `napkin.md`; sincronizar arquivo ↔ banco.
- Memórias para outros agentes (Codex/Kiro) além do que a injeção do Claude já cobre.

## Restrições

- Português na tela, na CLI e no texto injetado; curto.
- O teto é aplicado pela plataforma, nunca pelo agente.
- Sessão sem captura completa (projeto não cadastrado ou captura desligada) não recebe memórias.
- O peso da injeção continua visível no Analytics (linha `hook SessionStart · cockpit` do NAC-31).
- Migração nova `015_*.sql`.
- Escrita de memória pela CLI passa pelo daemon (como os outros comandos `nobre`).

## Abordagens

1. **Cockpit guarda (escolhida):** banco + injeção com teto + página + importação.
2. **`napkin.md` continua a fonte:** simples e legível fora do Cockpit, mas o arquivo segue crescendo, o agente segue lendo tudo e não há herança. Descartada.
3. **Banco com espelho em arquivo:** manutenção dupla. Descartada.

## Decisões fechadas depois da leitura do revisor (minhas, para conferir)

1. **Decisões de card que viram candidata:** só as gravadas por `nobre decide` (com autor agente ou usuário). As automáticas do Cockpit (integração/merge de branch, "detectado pelo Cockpit") ficam de fora. Card reaberto e concluído de novo não recria o que já existe.
2. **Descartadas:** a candidata descartada fica guardada com status `descartada` (não aparece em nenhuma lista, só serve para não recriar). Deduplicação por regra normalizada (minúsculas, sem acento, sem pontuação, espaços simples) no mesmo projeto, contra ativas, candidatas, arquivadas e descartadas; vale para importação e para cards.
3. **Importação, de onde e para onde:** lê o `napkin.md` do caminho de cada projeto cadastrado que tenha pasta (raiz, solto ou app com pasta própria); a candidata cai nesse projeto. Grupo sem repositório só tem napkin se a pasta tiver um. A mesma regra normalizada (ou semelhante) em 2 ou mais projetos vira uma candidata só, com escopo sugerido `global` e a lista de origens.
4. **Parser permissivo:** item = linha numerada ou com marcador abaixo de um `##`; negrito e data opcionais (sem data → data da importação); "Do instead:" pode ter várias linhas (até o próximo item); linha que não casa vira parte do item anterior; seção fora do padrão → categoria `dominio`. Nada é traduzido.
5. **Duplicada:** compara só a regra, com as ativas que valem para o mesmo projeto (incluindo as herdadas: raiz e global). Igual normalizada = duplicada; semelhança de palavras ≥ 0,6 = "parecida com #N".
6. **Escopo na CLI:** `projeto` = o projeto da sessão no momento (o app, se definido com `nobre app`; senão o principal). `raiz` numa sessão já na raiz grava na raiz (sem erro). A injeção usa o app atual da sessão na hora em que roda.
7. **Mudança no meio da sessão:** memória nova ou editada só chega na próxima injeção (sessão nova, resume, clear ou compactação). A sessão em andamento não é avisada; o agente pode consultar com `nobre memorias`.
8. **Teto:** percorre na ordem; memória que não cabe é pulada e a seleção continua com as menores; a linha final "+N memórias…" e o cabeçalho do bloco ficam fora do teto. Padrão 1500 tokens (bytes/4), configurável entre 0 (desliga a injeção) e 6000.
9. **Limites de texto:** regra até 300 caracteres e ação até 400; quebras de linha viram espaço; texto vazio recusado. Valem na API, na CLI e na importação (texto maior é cortado na importação, com "…", e marcado para revisar na fila).

## Deixado ao implementador

Nomes de tabelas/rotas/componentes, o limiar exato de semelhança (sugestão: Jaccard de palavras normalizadas ≥ 0,6), layout fino da página, a formatação exata da saída de `nobre memorias`.
