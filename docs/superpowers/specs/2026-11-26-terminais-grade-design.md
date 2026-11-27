# Terminais em grade (NAC-63, entrega 1 de 3)

Data: 2026-11-26. Card: NAC-63. Mock aprovado (v4): https://claude.ai/artifact/RsgZfBpZQjhPz6kSDJhuJM

## Intenção

Hoje a tela `/terminals` é uma tabela e um único terminal aberto por vez. O usuário roda vários agentes ao mesmo tempo, em projetos diferentes, e quer acompanhar todos lado a lado, organizados por projeto e subprojeto, e arrumar a tela do jeito dele. A tela não pode travar: tem que seguir leve com vários agentes escrevendo ao mesmo tempo.

O NAC-63 foi dividido em três entregas, nesta ordem (decisão do usuário):

1. **Grade** (este documento).
2. Medidores de contexto e planos por terminal, e "Limpar e continuar".
3. Barra de entrada com prompts salvos (`#`) e paleta (`Ctrl+K`).

## Objetivos

- Trocar a tabela por uma grade de terminais, com 1, 2 ou 3 colunas (padrão 3).
- Abas por projeto (nível 1) e chips por subprojeto (nível 2), com contagem.
- Reordenar por arrastar e redimensionar cada terminal (largura em colunas, altura livre).
- Modos **Simples** e **Detalhado**.
- Performance medida: com 9 terminais ligados recebendo saída pesada, nenhuma tarefa acima de 50 ms na thread principal e digitação respondendo em menos de 100 ms, em máquina com GPU.

## Fora desta entrega

- Medidor de contexto, janelas de plano, créditos e "Limpar e continuar" (entrega 2). O modo Detalhado desta entrega só reserva o lugar.
- Barra de entrada do Cockpit, `#`, `Ctrl+K` e o endpoint de input (entrega 3). A digitação continua sendo direto no xterm, como hoje.
- Mudar a regra dos terminais de `nobre run`: abrem só assistindo e o botão "Interagir" segue disponível, como hoje.
- Layout salvo no servidor ou compartilhado pelo time.
- Arrastar por toque (celular e tablet).

## O que seria um fracasso

- A tela travar ou engasgar com vários agentes ativos.
- Perder saída de terminal ao pausar, religar, mover ou redimensionar um tile.
- Mover ou redimensionar um tile mudar o tamanho do PTY de quem está no `nobre run`.
- Quebrar os links existentes `/terminals?id=<id>`: Sala de Controle (`ControlRoom.tsx`), "Recomeçar limpo" (`RestartCleanDialog.tsx`) e o CLI (`cmd/nobre/run.go`).
- Mudar o comportamento do terminal na página do card (`CardTerminal.tsx`).

## Como funciona hoje (o que esta entrega toca)

- `web/src/pages/Terminals.tsx`: tabela + um `LazyTerminalView`. `readonly` quando o terminal encerrou ou é de `nobre run` (linha 86).
- `web/src/ui/TerminalView.tsx`: uma instância do xterm 5.5.0 com renderizador de DOM, `scrollback: 5000`, `cursorBlink: true`, um WebSocket por terminal (`/api/terminals/{id}/ws`), reconexão com até 3 tentativas. Quem só assiste segue o tamanho do PTY e encolhe a fonte (`fontToFit`); o resize só vai ao PTY depois que o usuário interage: primeira tecla, colar ou clicar em "Interagir" (`engage`). `readonly` é só o estado inicial: o botão "Interagir" aparece em qualquer terminal vivo, inclusive de `nobre run`, e o servidor não confere a origem.
- `internal/pty/manager.go`: o daemon guarda 2 MiB de saída por terminal e manda um snapshot a cada assinante novo. Desmontar e remontar o xterm não perde nada. Limite de 20 terminais vivos.
- `internal/api/terminals.go` (`terminalWS`): repassa cada bloco de saída como uma mensagem binária.
- Projetos: `Project { id, name, path, parent_id, kind: 'repo' | 'group' }`. `web/src/app/project.ts` tem `projectForPath`, `familyIds`, `displayName`, `isGroup` e o filtro global de projeto (`useProjectFilter`).

## Abordagens consideradas

- **A. Tudo vivo.** Todo terminal da aba monta xterm e WebSocket. Mais simples, mas o custo cresce com a quantidade.
- **B. Só o visível fica ligado, com WebGL** (escolhida pelo usuário). Tiles fora da tela, em outra aba ou com a janela escondida desmontam o xterm e fecham o WebSocket; religam com o snapshot.
- **C. Prévia em texto.** O tile mostra as últimas linhas como texto e só o terminal em foco vira xterm. Descartada: Claude Code e Kiro redesenham a tela inteira, e um rabo de texto fica ilegível; ainda pediria backend novo.

### Medição que sustentou a escolha (2026-11-26)

Teste descartável, 9 terminais, 6 s por cenário, Chrome com GPU, carga sintética (um gerador que imita um agente redesenhando 8 linhas a cada 80 ms, e uma enxurrada de 2.000 linhas/s por terminal).

| Cenário | DOM | WebGL |
|---|---|---|
| Agente | 41–46 quadros/s, 16–23 tarefas longas | 59 quadros/s, 0 |
| Enxurrada | 5 quadros/s, ~5 s de tarefas longas em 6 s | 56–58 quadros/s, 0 |
| Atraso da thread principal na enxurrada (p95) | 340–540 ms | 0,6–21 ms |

20 terminais com WebGL não perderam contexto. Agrupar a saída a cada 100 ms baixou o atraso p95 na enxurrada com WebGL de 14–21 ms para 0,6 ms. Limites: carga sintética, uma máquina só; sem GPU o resultado cai para o do DOM.

## Desenho

### Tela

- Cabeçalho: título, alternância Simples/Detalhado, colunas (1/2/3), filtro de estado (Todos / Ao vivo), "Restaurar layout". O filtro "Esperam você" e o botão "Novo terminal" do mock ficam para depois (decisão minha: nenhum dos dois tem dado ou fluxo pronto nesta entrega).
- Abas de projeto: "Todos" e uma aba por projeto raiz (`parent_id == null`) que tenha terminal. Chips de subprojeto: "Todos" e um por filho do projeto escolhido que tenha terminal. Um projeto sem filhos não mostra a segunda linha.
- Projeto do terminal: o do card, se houver card; senão `projectForPath(dir)`. Terminal sem projeto aparece só em "Todos" (decisão minha).
- Com o filtro global de projeto ativo, a tela abre na aba daquele projeto (decisão minha).
- Tile: cabeçalho (pega de arrastar, cor e nome do agente, card com link, projeto/subprojeto, estado, botões − + e ⤢), corpo com o terminal, e canto inferior direito para redimensionar. Encerrado mostra "encerrado (código N)".
- `/terminals?id=<id>`: abre na aba "Todos", rola até o tile e o deixa em foco e ampliado (linha toda).
- Tela estreita: 2 colunas abaixo de 1100 px, 1 coluna abaixo de 700 px.

### Mover e redimensionar

- Arrastar pelo cabeçalho, com drag and drop nativo do browser. Solta antes ou depois do alvo conforme o lado do cursor; um marcador mostra onde cai. A ordem é global (uma lista de ids) e vale em todas as abas.
- Redimensionar pelo canto: largura encaixa em colunas (1 até o número de colunas), altura livre entre 200 e 900 px. Durante o gesto só o CSS do tile muda; o ajuste do xterm roda uma vez, ao soltar.
- Teclado: − e + mudam um passo de largura e altura; ⤢ alterna entre linha toda e o padrão (1 coluna, 360 px).
- Ordem, tamanhos, colunas e modo ficam em `localStorage`, com leitura e escrita protegidas e padrão seguro se falhar. Ids de terminais que não existem mais são descartados ao carregar.

### Simples e Detalhado

- Detalhado: o tile mostra projeto/subprojeto, origem, tempo desde o início e número de clientes.
- Simples: só agente, card e estado.
- A entrega 2 acrescenta os medidores ao Detalhado e um chip de contexto ao Simples.

### Performance (parte do contrato)

- **Orçamento de terminais ligados: 9.** Ligado = xterm montado e WebSocket aberto. Uma constante, não uma configuração (decisão minha).
- **Quem liga:** tile visível na tela (IntersectionObserver), na aba atual, com a janela visível. Fora disso, pausa. Passando do orçamento, pausa o que está há mais tempo sem interação; o tile em foco nunca pausa.
- **Tile em pausa:** mostra o cabeçalho e um corpo com "Em pausa. Clique para ligar." Sem xterm e sem WebSocket. Terminal encerrado também fica em pausa até o usuário clicar.
- **Pausa por janela escondida:** depois de 30 s escondida (decisão minha), para não desligar tudo numa troca rápida de aba.
- **Renderizador:** `@xterm/addon-webgl` 0.18.0 nos tiles ligados. Se o addon falhar ao carregar ou o contexto for perdido, o tile continua em DOM, sem erro para o usuário.
- **Na grade:** `scrollback` de 1.000 linhas e cursor piscando só no tile em foco. A visão de um terminal só (página do card) segue como hoje.
- **Escrita agrupada:** o frontend junta os blocos recebidos e escreve no xterm uma vez por quadro. O daemon agrupa a saída por assinante em janelas de até 100 ms só quando o WebSocket pede com um parâmetro novo (`?batch=1`), que a grade usa nos tiles que só assistem. Sem o parâmetro nada muda: a página do card e o CLI recebem como hoje, e quem interage recebe sem atraso.
- **Isolamento:** cada tile com `contain` no CSS e memoizado; a lista de terminais mudando (evento `terminals` do stream) não remonta tiles que não mudaram.
- **PTY:** nada na grade manda resize sem interação do usuário. Vale a regra de hoje: só depois da primeira tecla num tile interativo.

### Teste de performance

Um teste repetível fica no repositório, fora da suíte normal: uma página de bancada que monta 9 tiles com o componente real e alimenta cada um com saída sintética (os dois perfis da medição), mede tarefas longas, quadros por segundo e atraso da thread principal, e mostra aprovado ou reprovado contra a meta. Roda à mão num browser com GPU e janela visível; o Chrome sem janela não tem WebGL nesta máquina, então não entra no `make test`.

### Interação no tile (decisões minhas, após a revisão)

- **Todo tile abre só assistindo.** Assim abrir a grade nunca mexe no tamanho de um PTY, e o agrupamento vale para a grade inteira.
- **O tile tem um menu (⋯)** com "Interagir" / "Somente assistir" e "Encerrar agente", os mesmos controles de hoje. Trocar de modo remonta o xterm e o WebSocket, como hoje.
- **Depois de "Interagir"**, o PTY passa a ter o tamanho do tile. Redimensionar o tile ou trocar colunas manda um resize ao PTY, uma vez, ao fim do gesto.
- **Pausar e religar volta para "só assistindo".** O tile em foco não pausa, então quem está digitando não perde o modo.
- **Subprojeto:** terminal do projeto raiz que tem filhos fica no chip "Geral", como no filtro global (`effectiveIds`).
- **Abas e chips são estado local da tela**, salvos junto com o layout. Não gravam `?projeto=` na URL. Abertura: `?id=` vence e abre "Todos"; sem `id`, o filtro global escolhe a aba; sem os dois, a última aba usada.
- **Ordem inicial:** do mais novo para o mais antigo, como a tabela de hoje. Terminal novo entra no início.
- **Meta de performance:** vale para o regime estável. Religar escreve um snapshot de até 2 MiB; a bancada mede e mostra o custo de religar à parte, sem reprovar por ele. "Digitação" é medida pelo atraso da thread principal (p95 abaixo de 50 ms, máximo abaixo de 100 ms).

## Erros e casos de borda

- Terminal some da lista (encerrado e removido após a retenção de 30 min): o tile sai da grade e o id sai do layout salvo.
- WebSocket cai: vale a reconexão de hoje; esgotadas as tentativas, o tile mostra "desconectado" com botão de tentar de novo.
- `localStorage` indisponível: a tela funciona com o layout padrão, sem salvar.
- Mais de 9 tiles visíveis ao mesmo tempo (tiles pequenos em monitor grande): os excedentes aparecem em pausa.

## Testes

- Unidade (vitest): modelo da grade (filtro por projeto e subprojeto, contagens, ordem, encaixe de largura, limpeza de ids), orçamento de ligados (quem liga, quem pausa, foco nunca pausa), persistência com `localStorage` falhando.
- Componente: abas, chips, modos, teclado (− + ⤢), `?id=`, tile de `nobre run` só leitura, pausa e religar. Arrastar testado pelo modelo e pelos eventos de drag.
- Daemon (Go): agrupamento para assinante `readonly` (junta blocos, respeita os 100 ms, não reordena, entrega o resto no encerramento) e nenhum atraso para quem interage.
- Bancada de performance: rodada manual registrada no card antes do merge.

## Deixado para quem implementa

- Estrutura de arquivos e nomes dos módulos da grade.
- Como compartilhar o código entre a grade e o `TerminalView` de hoje (opções no componente ou extração), desde que a página do card não mude de comportamento.
- Implementação do agrupamento no daemon (no `pump` do assinante ou no laço do WebSocket).
- Passos exatos de − e +.
