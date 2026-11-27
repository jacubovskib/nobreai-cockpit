# Barra de entrada nos Terminais (NAC-63, entrega 3 de 3)

Data: 2026-11-27. Card: NAC-63. Mock aprovado (v4): https://claude.ai/artifact/RsgZfBpZQjhPz6kSDJhuJM

## Intenção

Na grade de Terminais o usuário acompanha vários agentes, mas para mandar algo a um deles precisa trocar o tile para "Interagir" e digitar dentro do xterm. Ele quer mandar texto direto do Cockpit, reaproveitar os prompts que já salvou na tela Prompts e puxar contexto da plataforma (cards e memórias) sem copiar e colar.

Esta é a última entrega do NAC-63. As duas primeiras (grade e medidores) estão no main.

## Objetivos

- Cada tile ligado ganha uma barra de entrada do Cockpit, abaixo do terminal. O texto vai ao agente sem trocar o tile para "Interagir" e sem mudar o tamanho do PTY.
- `#` no começo da barra abre os prompts salvos; escolher um preenche a barra com o texto já renderizado, para editar antes de enviar.
- `Ctrl+K` abre uma paleta de busca com prompts salvos, cards e memórias; escolher um item insere o texto na barra do tile em foco.
- `/`, `@` e `!` não são interceptados: seguem crus ao agente (decisão 161 do card).

## Fora desta entrega

- Interceptar teclas dentro do xterm. A barra é separada; o xterm em "Interagir" continua como hoje.
- Histórico de envios (seta para cima), envio para vários terminais de uma vez, anexos.
- Criar ou editar prompt pela barra. Isso continua na tela Prompts.
- Estado "espera você" por tile e o filtro por estado do mock. O Cockpit não tem esse dado hoje.
- "Retomar em novo terminal" no tile encerrado (aparece no mock).
- Barra de entrada no terminal da página do card (`CardTerminal.tsx`).

## O que seria um fracasso

- Texto de várias linhas chegar ao agente quebrado em vários envios, ou ficar no campo dele sem ser enviado.
- A barra mudar o tamanho do PTY de alguém, em especial de um `nobre run`.
- Digitar na barra deixar a grade lenta: a meta de performance da entrega 1 continua valendo.
- Um prompt escolhido por engano contar como uso, ou ser enviado sem o usuário ver o texto.
- Quebrar o que as entregas 1 e 2 fazem: arrastar, redimensionar, medidores, "Limpar e continuar", `?id=`.

## Como funciona hoje (o que esta entrega toca)

- `internal/api/terminals.go`: o WebSocket `GET /api/terminals/{id}/ws` aceita entrada binária e resize; com `?readonly=1` ignora os dois. Não existe endpoint HTTP de entrada. Todo tile da grade abre com `readonly=1`.
- `internal/pty/manager.go`: `Terminal.Write` grava no PTY com prazo; devolve `ErrEnded` se o processo acabou.
- `internal/api/server.go`: todo pedido que não é GET exige `Content-Type: application/json` (protege contra formulário de outro site) e o corpo é limitado a 1 MiB.
- Prompts salvos: `GET /api/prompts`; `POST /api/prompts/{slug}/render` com `{card, body, use}` devolve o texto com as variáveis do card (`{{card.onde_parei}}` etc.) e só conta uso com `use=true`. Cada prompt tem `agents` (quais agentes aceita) e `project_id` (nulo = todos os projetos).
- `web/src/pages/terminals/GridTile.tsx`: tile memoizado; recebe só valores simples e callbacks estáveis. `web/src/pages/Terminals.tsx` guarda qual tile está em foco.
- Cards: `useCards`. Memórias: `useMemories`.

## Medição que sustentou o protocolo (2026-11-27)

Testei num terminal descartável da plataforma, no daemon instalado, digitando pelo WebSocket.

| Envio | Claude Code 2.1.332 | Kiro CLI 3.5.0 |
|---|---|---|
| Uma linha + Enter no mesmo envio | enviou | não registrei o resultado |
| Duas linhas com quebra crua + Enter no mesmo envio | ficou no campo, **não enviou** | não testei |
| Colagem marcada (`ESC[200~ … ESC[201~`), 150 ms, Enter em envio separado | enviou as duas linhas juntas | enviou as duas linhas juntas |
| Colagem começando com `#` | enviou, mas o `#` inicial sumiu (com um espaço antes também) | enviou com o `#` |
| Colagem começando com `/` | tratou como comando | não testei |
| Colagem começando com `!` | rodou no shell; com um espaço antes, não rodou | não testei |
| Texto enviado com o agente ocupado | entrou na fila e rodou depois | o Kiro avisa "Type to steer" |

Codex não está instalado nesta máquina: vale o mesmo protocolo, sem teste.

Não testei o envio com o agente parado num menu de permissão. Ver "Riscos aceitos".

## Abordagens consideradas

1. **Endpoint no daemon que faz a colagem (escolhida).** `POST /api/terminals/{id}/input` recebe o texto; o daemon grava a colagem marcada, espera e grava o Enter. O protocolo fica num lugar só, testável em Go; funciona com o tile só assistindo e até em pausa; não mexe no tamanho do PTY; o mesmo pedido registra o uso do prompt.
2. **A barra escreve pelo WebSocket do tile.** O tile teria de abrir sem `readonly`, e aí o xterm passa a mandar teclas e resize: é o modo "Interagir" de hoje, que muda o tamanho do PTY. Separar "aceita entrada" de "aceita resize" no WebSocket dá o mesmo trabalho do endpoint e deixa o protocolo e a espera de 150 ms no browser.
3. **Abrir um segundo WebSocket só para cada envio.** Funciona, mas é uma conexão por envio e o protocolo continua no browser.

## Desenho

### Endpoint de entrada

`POST /api/terminals/{id}/input`, corpo `{"text": "...", "prompt": "slug-opcional"}`.

- O daemon troca `\r\n` e `\r` por `\n`, remove do texto todo caractere `ESC` e todo `U+009B` (assim o texto não fecha a colagem por conta própria nem carrega sequências de controle; card e memória inseridos pela paleta são texto escrito por agente) e tira quebras de linha do fim.
- Grava `ESC[200~` + texto + `ESC[201~`, espera 150 ms e grava `\r`. Dois envios ao mesmo terminal não se misturam: um termina antes de o outro começar.
- Não redimensiona o PTY e não depende de haver alguém conectado ao WebSocket.
- A sequência não é cancelada se o browser desistir do pedido no meio: depois da colagem, o Enter é sempre tentado. Se o terminal encerrar entre a colagem e o Enter, a resposta é `409` e o uso do prompt não é contado.
- Se `prompt` vier e o envio der certo, conta um uso do prompt (`MarkPromptUsed`) e publica `prompts`. Slug que não existe não derruba o envio.
- Respostas: `204` enviado; `400` texto vazio depois da limpeza; `404` terminal não existe; `409` terminal encerrado; `413` texto acima de 256 KiB; `504` se o agente não leu a entrada no prazo de escrita do PTY.
- Mesma proteção das outras rotas: só `application/json`, daemon em loopback. Não há checagem de origem a mais.
- Vale para terminais de qualquer origem. Quem decide se o `nobre run` recebe é a tela (abaixo).

### Barra no tile

- Fica no rodapé do tile, sob o terminal, em todo tile **ligado e não encerrado**. Tile em pausa e tile encerrado não mostram a barra.
- Campo de texto que cresce de 1 até 6 linhas, com rolagem depois disso; a área do terminal encolhe na mesma medida (a barra não cobre o terminal). `Enter` envia; `Shift+Enter` quebra linha; `Enter` durante a composição de um caractere (IME) não envia. Botão "Enviar" ao lado.
- Vale para os três agentes (Claude, Codex, Kiro).
- Enviar chama o endpoint. Com `204` o campo esvazia e mantém o foco; se o usuário digitou outra coisa durante o envio, o texto novo fica. O rascunho e o "Liberar entrada" sobrevivem à pausa do tile (ficam guardados por terminal até recarregar a página). Com erro o texto fica no campo e uma linha mostra o motivo (encerrado, grande demais, daemon sem a rota).
- Enquanto um envio está pendente, o botão e o `Enter` ficam desligados.
- **Terminal de `nobre run`:** o rodapé mostra "Sessão do `nobre run`: você só assiste." e o link "Liberar entrada", como no mock. Clicar mostra a barra naquele tile, até recarregar a página. Não muda o tamanho do PTY (diferente de "Interagir").
- **Detalhado:** uma linha de dicas sob a barra (`/` `@` vão direto ao agente · `#` prompts · `Ctrl K` buscar). **Simples:** sem a linha de dicas.
- O menu ⋯ e o modo "Interagir" continuam como estão. A barra não substitui "Interagir": setas, `Esc` e menus do agente ainda pedem o xterm.
- O texto digitado é estado do próprio tile. Digitar não re-renderiza a página nem os outros tiles.

### `#`: prompts salvos

- Quando o texto da barra **começa** com `#`, abre uma lista acima da barra com os prompts salvos. O que vem depois do `#` filtra por nome, slug e tag.
- Entram na lista os prompts cujo `agents` inclui o agente do tile e cujo `project_id` é nulo, o projeto do tile ou o pai dele (a mesma regra da tela Prompts). Tile sem projeto: só os de `project_id` nulo. Ordem: mais usados primeiro.
- Setas movem a seleção; `Enter` ou clique escolhe; `Esc` fecha a lista e deixa o texto como está.
- Escolher renderiza o prompt com o card do tile (`POST /api/prompts/{slug}/render`, sem `use`) e **troca** o texto da barra pelo resultado. Aparece a linha "Prévia: o agente vai receber este texto. Edite antes de enviar." com "Limpar".
- Tile sem card: o prompt entra com as variáveis do card vazias e a prévia avisa "sem card: as variáveis do card ficaram vazias".
- Variável desconhecida (422): a barra não muda e a lista mostra o erro com o nome da variável. Card do tile que não existe mais (404): renderiza como tile sem card.
- Se o texto da barra começa com `#`, `/` ou `!` depois de um prompt entrar, a prévia avisa: "O agente pode tratar o primeiro caractere como comando."
- O uso só é contado no envio: a barra manda o slug junto. Se o usuário apagar o texto inteiro, substituir o texto inteiro por outro ou clicar em "Limpar", o slug é esquecido. "Substituir inteiro" é detectado por aproximação: o texto novo não começa nem termina com o mesmo caractere do anterior.
- A lista do `#` abre por cima do terminal, sem mudar a altura dele.
- Com a lista fechada por `Esc`, `Enter` envia o texto como está, com o `#`.

### `Ctrl+K`: paleta

- Botão "Buscar `Ctrl K`" no cabeçalho da página e o atalho `Ctrl+K` (ou `⌘K`).
- **Com o foco dentro de um xterm em "Interagir", o atalho não abre a paleta:** a tecla segue ao agente, onde `Ctrl+K` apaga até o fim da linha. Em qualquer outro lugar da tela, inclusive na barra, abre a paleta.
- A paleta é um diálogo com um campo de busca e três grupos: **Prompts salvos**, **Cards**, **Memórias**. Sem texto mostra os prompts e os cards mais recentes; com texto filtra os três. No máximo 8 itens por grupo.
- Alvo: o tile em foco na página. O cabeçalho da paleta diz qual é ("Inserir em: Claude · NAC-63"). Sem tile em foco, ou com o tile em foco sem barra, a paleta abre só para consulta e os itens ficam desligados com o aviso "Clique num terminal para escolher onde inserir".
- Escolher um item fecha a paleta, põe o foco na barra do alvo e:
  - **Prompt:** mesmo efeito do `#` (texto renderizado troca o conteúdo da barra, com prévia). Os prompts da paleta seguem o mesmo filtro de agente e projeto do alvo.
  - **Card:** insere na posição do cursor `NAC-12 · <título>` e, se houver, uma segunda linha `Onde parei: <texto>`.
  - **Memória:** insere na posição do cursor a regra e, se houver, ` — <o que fazer>`.
- **Quais cards:** os não concluídos, de todos os projetos, do mais recente para o mais antigo por `updated_at`; a busca casa chave e título. O "Onde parei" entra mesmo se estiver vencido.
- **Quais memórias:** as de status `ativa` do projeto do alvo, com as herdadas da família; sem alvo ou alvo sem projeto, as globais. A busca casa regra e "o que fazer".
- O atalho chama `preventDefault`, para o browser não usar o `Ctrl+K` dele.
- Nada é enviado ao escolher. O usuário sempre revê e aperta `Enter`.
- Cards e memórias só são buscados no servidor quando a paleta abre.

### Performance (parte do contrato)

- O tile continua memoizado e recebe só valores simples e callbacks estáveis. A paleta vive na página e só existe montada enquanto aberta.
- A inserção vinda da paleta chega ao tile sem passar texto novo por props de todos os tiles.
- A bancada (`web/bench`) roda de novo antes do merge, com a mesma meta da entrega 1.
- Um tile em "Interagir" tem o PTY redimensionado uma vez quando a barra cresce ou encolhe de linhas e quando a linha de dicas aparece ou some (troca Simples/Detalhado), como já acontece com a faixa de contexto.

## Riscos aceitos (decisões minhas)

- **Agente parado num menu de permissão.** O Cockpit não sabe quando o agente está num menu. Um envio nessa hora termina com Enter, que confirma a opção marcada. O usuário vê o terminal logo acima da barra; a barra não tenta adivinhar. Se isso incomodar, o passo seguinte é o daemon expor o estado "espera você".
- **Texto começando com `#`, `/` ou `!`.** Segue cru. No Claude Code, `/` vira comando, `!` roda no shell e o `#` inicial some (um título markdown na primeira linha perde o `#`). Vale para texto digitado e para prompt salvo cujo corpo começa assim; a prévia avisa, mas não corrige.
- **Espera de 150 ms** calibrada com uma rodada por agente. Fica numa constante do daemon.
- **Origem do pedido.** O WebSocket confere a origem; o POST só exige `application/json`, como todas as outras rotas que mudam estado.
- **Colagem marcada com o agente fora do campo de texto.** Se o programa no terminal não estiver com a colagem marcada ligada, os marcadores aparecem como texto. Os terminais da plataforma sempre rodam um agente, que liga esse modo no campo dele.
- **Codex sem teste.**

## Erros e casos de borda

- Daemon antigo (sem a rota): o envio falha e a barra mostra "Este daemon não aceita entrada pela barra. Atualize o Cockpit." O texto fica no campo.
- Terminal encerra com texto na barra: a barra some junto com o estado ligado; o texto se perde.
- Tile pausa pelo orçamento com texto na barra: o texto volta quando o tile religa.
- Prompt apagado entre a lista e a escolha: erro na lista, barra intacta.
- Texto só com espaços: "Enviar" desligado.

## Testes

- Daemon (Go): limpeza do texto (quebras, marcadores no meio, fim), ordem colagem → espera → Enter com a espera injetada (sem relógio de parede), dois envios simultâneos que não se misturam, 400/404/409/413, uso do prompt contado só com envio bem-sucedido, PTY não redimensionado.
- Unidade (vitest): filtro e ordem dos prompts por agente, projeto e texto; texto inserido por card e por memória; inserção na posição do cursor.
- Componente: barra (Enter, Shift+Enter, pendente, erro mantém o texto), `nobre run` com "Liberar entrada", lista do `#` (abrir, filtrar, setas, escolher, Esc, 422, sem card), paleta (abrir pelo botão e pelo atalho, não abrir com foco no xterm em "Interagir", alvo, item desligado sem alvo, inserir).
- Bancada de performance: rodada manual registrada no card antes do merge.
- À mão, depois do `make update`: enviar uma linha e várias linhas a um Claude e a um Kiro; `#` com um prompt que usa `{{card.onde_parei}}`; `Ctrl+K` inserindo um card; conferir que o tile de `nobre run` não muda de tamanho.

## Deixado para quem implementa

- Nomes e divisão dos arquivos da barra, da lista e da paleta.
- Como a página entrega a inserção ao tile certo sem quebrar a memoização.
- Onde fica a exclusão entre envios ao mesmo terminal (no `Terminal` ou no handler).
- Altura exata da barra e da lista.
