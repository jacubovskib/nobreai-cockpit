# Terminais em grade — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Trocar a tabela de `/terminals` por uma grade de terminais com abas por projeto e subprojeto, arrastar, redimensionar, modos Simples/Detalhado, e performance medida com 9 terminais ligados.

**Architecture:** Lógica pura (modelo da grade, persistência, orçamento de ligados) separada dos componentes. O `TerminalView` existente ganha opções para a grade (WebGL com fallback, escrita por quadro, agrupamento) sem mudar o padrão que a página do card usa. O daemon agrupa a saída só para quem pede `?batch=1`.

**Tech Stack:** Go (daemon, `github.com/coder/websocket`), React 18 + TypeScript, vitest, `@xterm/xterm` 5.5.0, `@xterm/addon-webgl` 0.18.0 (novo).

**Spec:** `docs/superpowers/specs/2026-11-26-terminais-grade-design.md`. Mock: https://claude.ai/artifact/RsgZfBpZQjhPz6kSDJhuJM

## Global Constraints

- Textos da interface em português, no tom das telas atuais. Comentários de código em português.
- Sem dependência nova além de `@xterm/addon-webgl` na versão exata `0.18.0`.
- Orçamento de ligados: `MAX_LIVE = 9`. Altura do tile: padrão `360`, mínimo `200`, máximo `900` px. Colunas: 1, 2 ou 3 (padrão 3). `scrollback` na grade: `1000`.
- Pausa por janela escondida depois de `30_000` ms. Agrupamento do daemon: janela de `100` ms.
- Quebras: 2 colunas abaixo de 1100 px, 1 coluna abaixo de 700 px.
- Nada na grade manda resize ao PTY sem interação do usuário. Todo tile abre só assistindo.
- A página do card (`CardTerminal.tsx`) e o CLI não mudam de comportamento: sem `?batch=1`, o WebSocket entrega como hoje; as opções novas do `TerminalView` têm como padrão o comportamento atual.
- Links `/terminals?id=<id>` continuam funcionando.
- Chave do `localStorage`: `nobre.terminais.grade.v1`.
- Commits no padrão do repositório: `feat(web): … (NAC-63)`, `feat(api): … (NAC-63)`, com a linha `Co-Authored-By` da sessão.
- Rodar antes de cada commit: `cd web && npx tsc --noEmit && npx vitest run <arquivos da tarefa>`; para Go, `go test ./internal/api/...`.

## Review Focus

1. Layout salvo corrompido ou de versão antiga no `localStorage` (JSON inválido, tipos errados, ids que não existem): a tela abre com o padrão, sem erro. Teste na Task 2.
2. Mais de 9 tiles visíveis: os excedentes ficam em pausa e o tile em foco nunca pausa, mesmo sendo o mais antigo. Teste na Task 3.
3. Contexto WebGL perdido ou addon que falha ao carregar: o tile segue em DOM e continua mostrando a saída. Teste na Task 5.
4. Terminal que encerra enquanto o assinante agrupado tem bytes pendentes: o resto chega antes da mensagem de fim, na ordem. Teste na Task 4.
5. Link `?id=` de um terminal que não existe mais: a tela mostra o aviso de hoje ("Este terminal não existe mais…") e a grade segue utilizável. Teste na Task 7.

---

### Task 1: Modelo da grade (lógica pura)

**Files:**
- Create: `web/src/pages/terminals/gridModel.ts`
- Test: `web/src/pages/terminals/gridModel.test.ts`

**Interfaces:**
- Consumes: `TerminalInfo`, `isLive` de `web/src/api/terminals.ts`; `Project`, `CardSummary` de `web/src/api/types.ts`; `projectForPath` de `web/src/app/project.ts`.
- Produces:

```ts
export const DEF_H = 360, MIN_H = 200, MAX_H = 900, DEF_COLS = 3
export type Cols = 1 | 2 | 3
export type StateFilter = 'all' | 'live'
export interface TileSize { span: number; h: number }
export interface Layout { order: string[]; sizes: Record<string, TileSize>; cols: Cols; simple: boolean; tab: number | 'all'; sub: number | 'all' | 'geral' }
export const defaultLayout: () => Layout
export interface Placed { t: TerminalInfo; rootId: number | null; subId: number | null }  // subId null = no projeto raiz
export function place(terms: TerminalInfo[], projects: Project[], cards: CardSummary[]): Placed[]
export interface Tab { id: number | 'all'; name: string; count: number }
export interface Chip { id: number | 'all' | 'geral'; name: string; count: number }
export function tabs(placed: Placed[], projects: Project[]): Tab[]                 // "Todos" + raízes com terminal
export function chips(placed: Placed[], projects: Project[], tab: number | 'all'): Chip[]  // [] se a raiz não tem filhos
export function visible(placed: Placed[], l: Layout, f: StateFilter): TerminalInfo[]       // filtrado e ordenado
export function reconcile(l: Layout, ids: string[], newestFirst: string[]): Layout  // tira ids mortos, põe novos no início
export function move(order: string[], id: string, target: string, after: boolean): string[]
export function sizeOf(l: Layout, id: string): TileSize                            // span limitado a l.cols
export function snapSpan(widthPx: number, colW: number, gap: number, cols: Cols): number
export function clampH(h: number): number
export function step(s: TileSize, dir: 1 | -1, cols: Cols): TileSize              // span ±1, h ±90
export function toggleFull(s: TileSize, cols: Cols): TileSize                      // {cols, 520} <-> {1, DEF_H}
```

- [ ] **Step 1: Write the failing tests** em `gridModel.test.ts`. Fixtures: projetos `{id:1,name:'NobreTech',parent_id:null,kind:'group'}`, `{id:2,name:'cockpit',parent_id:1,kind:'repo',path:'/w/cockpit'}`, `{id:3,name:'site',parent_id:1,kind:'repo',path:'/w/site'}`, `{id:4,name:'Pessoal',parent_id:null,kind:'repo',path:'/w/pessoal'}`. Casos, um `it` cada:
  - `place`: terminal com `card_key` segue o projeto do card (mesmo com `dir` de outro projeto); sem card usa `dir`; sem projeto dá `rootId: null`.
  - `tabs`: devolve "Todos" com o total e só raízes com terminal; `chips(…, 1)` devolve "Todos", "Geral" (só se houver terminal na raiz) e os filhos com terminal; `chips(…, 4)` devolve `[]`.
  - `visible`: `tab: 'all'` inclui terminal sem projeto; `tab: 1, sub: 'geral'` só os da raiz; `f: 'live'` tira os encerrados; a ordem segue `l.order`.
  - `reconcile`: remove ids ausentes de `order` e `sizes`; ids novos entram no início, na ordem de `newestFirst`.
  - `move(['a','b','c'],'c','a',false)` → `['c','a','b']`; `move(['a','b','c'],'a','b',true)` → `['b','a','c']`; mover para si mesmo não muda.
  - `sizeOf` limita `span: 3` a `2` quando `cols: 2`; id desconhecido dá `{span:1,h:360}`.
  - `snapSpan(900, 440, 12, 3)` → `2`; nunca menor que 1 nem maior que `cols`. `clampH(50)` → `200`, `clampH(2000)` → `900`.
  - `step({span:1,h:360},1,3)` → `{span:2,h:450}`; `step({span:1,h:200},-1,3)` → `{span:1,h:200}`.
  - `toggleFull({span:1,h:360},3)` → `{span:3,h:520}` e de volta para `{span:1,h:360}`.
- [ ] **Step 2:** Run `cd web && npx vitest run src/pages/terminals/gridModel.test.ts`. Expected: FAIL (módulo não existe).
- [ ] **Step 3:** Implementar `gridModel.ts` com as assinaturas acima. `place` usa a mesma regra de `Terminals.tsx` hoje (card vence o diretório); raiz = subir por `parent_id` até `null`.
- [ ] **Step 4:** Run o mesmo comando e `npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): modelo da grade de terminais (NAC-63)`.

### Task 2: Persistência do layout

**Files:**
- Create: `web/src/pages/terminals/gridStore.ts`
- Test: `web/src/pages/terminals/gridStore.test.ts`

**Interfaces:**
- Consumes: `Layout`, `defaultLayout`, `clampH` da Task 1.
- Produces: `export const KEY = 'nobre.terminais.grade.v1'`; `export function loadLayout(s?: Pick<Storage,'getItem'>): Layout`; `export function saveLayout(l: Layout, s?: Pick<Storage,'setItem'>): void`. O parâmetro tem `window.localStorage` como padrão, lido dentro de `try`.

- [ ] **Step 1: Write the failing tests:** ida e volta (`saveLayout` depois `loadLayout` devolve o mesmo objeto); `getItem` que lança → `defaultLayout()`; JSON inválido → padrão; campos com tipo errado (`cols: 7`, `order: 'x'`, `sizes: {a:{span:'z',h:-5}}`) → cada campo inválido volta ao padrão e `h` passa por `clampH`; `setItem` que lança não propaga o erro.
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/gridStore.test.ts`. Expected: FAIL.
- [ ] **Step 3:** Implementar. Validar campo a campo; nunca devolver referência compartilhada do padrão.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): layout da grade salvo no navegador (NAC-63)`.

### Task 3: Orçamento de terminais ligados

**Files:**
- Create: `web/src/pages/terminals/liveBudget.ts`
- Test: `web/src/pages/terminals/liveBudget.test.ts`

**Interfaces:**
- Produces:

```ts
export const MAX_LIVE = 9
export const HIDDEN_PAUSE_MS = 30_000
export interface BudgetInput {
  ids: string[]                       // tiles da aba atual, na ordem da grade
  onScreen: Set<string>               // IntersectionObserver
  ended: Set<string>                  // encerrados
  woken: Set<string>                  // encerrados que o usuário clicou para ligar
  focused: string | null
  lastUsed: Record<string, number>    // ms da última interação; ausente = 0
  pageHidden: boolean                 // escondida há mais de HIDDEN_PAUSE_MS
}
export function liveSet(i: BudgetInput, max?: number): Set<string>
```

- [ ] **Step 1: Write the failing tests:** fora da tela não liga; `pageHidden` → conjunto vazio; encerrado só liga se estiver em `woken`; 12 na tela com `max` padrão → 9 ligados, os de maior `lastUsed` (empate: ordem da grade); o `focused` entra sempre, mesmo com o menor `lastUsed`, e ainda assim o total é 9; id fora de `ids` nunca liga.
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/liveBudget.test.ts`. Expected: FAIL.
- [ ] **Step 3:** Implementar `liveSet`.
- [ ] **Step 4:** Run. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): orçamento de terminais ligados na grade (NAC-63)`.

### Task 4: Agrupamento da saída no daemon (`?batch=1`)

**Files:**
- Create: `internal/api/wsbatch.go`, `internal/api/wsbatch_test.go`
- Modify: `internal/api/terminals.go` (função `terminalWS`, o laço de saída)
- Test: `internal/api/terminals_test.go` (caso de ponta a ponta)

**Interfaces:**
- Produces: `const wsBatchWindow = 100 * time.Millisecond`; `func batchChunks(ctx context.Context, in <-chan []byte, window time.Duration) <-chan []byte`. Junta os blocos recebidos em cada janela num só, na ordem; quando `in` fecha, entrega o que restar e fecha a saída; quando `ctx` termina, fecha a saída.
- Em `terminalWS`: `batch := r.URL.Query().Get("batch") == "1"`; se `batch`, o canal `out` do `Subscribe` passa por `batchChunks`. Sem o parâmetro, o caminho é o de hoje, byte a byte igual.

- [ ] **Step 1: Write the failing tests** em `wsbatch_test.go`: (a) três blocos `"a"`, `"b"`, `"c"` enviados dentro de uma janela de 50 ms saem como um bloco `"abc"`; (b) blocos em janelas diferentes saem separados e em ordem; (c) fechar `in` com bytes pendentes entrega o resto e depois fecha; (d) cancelar o `ctx` fecha a saída sem travar (teste com prazo de 1 s).
- [ ] **Step 2:** Run `go test ./internal/api/ -run Batch -v`. Expected: FAIL (função não definida).
- [ ] **Step 3:** Implementar `batchChunks` com uma goroutine e um `time.Timer` armado no primeiro bloco da janela. Copiar os bytes ao juntar (o chamador pode reusar o slice).
- [ ] **Step 4:** Ligar em `terminalWS`. O snapshot inicial continua indo inteiro e antes de qualquer bloco. A mensagem de fim (`exit`) só sai depois do último bloco agrupado.
- [ ] **Step 5:** Adicionar em `terminals_test.go`, no estilo dos testes de WebSocket que já existem lá: um cliente com `?batch=1` recebe toda a saída de um terminal que escreve e encerra, seguida do `exit`; um cliente sem o parâmetro recebe a mesma sequência de bytes.
- [ ] **Step 6:** Run `go test ./internal/api/... ./internal/pty/...`. Expected: PASS.
- [ ] **Step 7:** Commit `feat(api): saída agrupada para quem assiste com ?batch=1 (NAC-63)`.

### Task 5: Opções de grade no `TerminalView`

**Files:**
- Modify: `web/src/ui/TerminalView.tsx`, `web/src/ui/LazyTerminalView.tsx`, `web/src/api/terminals.ts` (`wsUrl`), `web/package.json` (+ lockfile)
- Test: `web/src/ui/TerminalView.test.tsx`, teste de `wsUrl` onde ele já é testado

**Interfaces:**
- Produces, em `terminals.ts`: `wsUrl(id: string, readonly: boolean, loc?, batch?: boolean)`; com `batch` acrescenta `batch=1` à query (junto de `readonly=1` quando houver).
- Produces, props novas do `TerminalView` (todas opcionais; ausentes = comportamento de hoje):

```ts
grid?: boolean            // modo grade: scrollback 1000, WebGL, escrita por quadro, batch=1 enquanto só assiste, sem a barra de botões própria
active?: boolean          // cursor piscando só quando true (na grade)
onMode?: (ro: boolean) => void   // avisa a troca assistir/interagir
```

- `TermFactory` passa a aceitar opções: `type TermFactory = (o?: { scrollback?: number; cursorBlink?: boolean; webgl?: boolean }) => TermLike`. `TermLike` ganha `cursorBlink: boolean` (setter) e `renderer: 'dom' | 'webgl'` (leitura).
- Expor por `ref` (`forwardRef`): `export interface TerminalHandle { setReadonly(ro: boolean): void; kill(): void; fit(): void; reconnect(): void }`. O tile usa isso no menu ⋯ e ao fim do gesto de redimensionar.
- `onStatus?: (s: 'conectando' | 'conectado' | 'reconectando' | 'desconectado') => void`: com `grid`, o tile é quem mostra o estado da conexão. Teste: esgotadas as 3 tentativas, `onStatus('desconectado')` é chamado; `reconnect()` abre uma conexão nova. No `GridTile` (Task 6), `desconectado` mostra "Desconectado" e o botão "Tentar de novo" sobre o corpo, com teste próprio.

- [ ] **Step 1:** `cd web && npm install --save-exact @xterm/addon-webgl@0.18.0`.
- [ ] **Step 2: Write the failing tests** em `TerminalView.test.tsx`, com as fábricas falsas que o arquivo já usa:
  - sem `grid`, a URL do WebSocket não tem `batch=1` e a barra de botões aparece (regressão do padrão);
  - com `grid` e `readonly`, a URL tem `readonly=1` e `batch=1`; depois de `setReadonly(false)` a nova conexão não tem nenhum dos dois;
  - com `grid`, a fábrica recebe `{ scrollback: 1000, webgl: true }`; sem `grid`, recebe o padrão de hoje;
  - com `grid`, três mensagens binárias recebidas antes do próximo quadro viram uma única chamada de `write` (usar `vi.useFakeTimers` e um `requestAnimationFrame` falso);
  - `active` falso → `cursorBlink` falso; `active` verdadeiro → verdadeiro;
  - com `grid`, a barra de botões própria não é renderizada.
- [ ] **Step 3:** Run `npx vitest run src/ui/TerminalView.test.tsx`. Expected: FAIL nos casos novos.
- [ ] **Step 4:** Implementar. Em `xtermFactory`: com `webgl`, carregar `WebglAddon` dentro de `try`; em `onContextLoss`, descartar o addon (o xterm volta ao DOM) e marcar `renderer = 'dom'`; falha ao carregar também fica em DOM. A escrita por quadro vale só com `grid` e não pode atrasar o snapshot inicial (a regra "entrada só depois do snapshot escrito" continua).
- [ ] **Step 5:** Adicionar um teste da fábrica real para o fallback: com `WebglAddon` simulado (`vi.mock('@xterm/addon-webgl')`) lançando no construtor, `xtermFactory({ webgl: true }).renderer` é `'dom'` e `write` funciona; com o addon simulado disparando a perda de contexto, `renderer` passa a `'dom'`.
- [ ] **Step 6:** Run `npx vitest run src/ui && npx tsc --noEmit`. Expected: PASS, inclusive os testes antigos do arquivo.
- [ ] **Step 7:** Commit `feat(web): TerminalView com opções de grade — WebGL, escrita por quadro, batch (NAC-63)`.

### Task 6: Tile da grade

**Files:**
- Create: `web/src/pages/terminals/GridTile.tsx`
- Test: `web/src/pages/terminals/GridTile.test.tsx`

**Interfaces:**
- Consumes: `TerminalHandle`, `LazyTerminalView` (Task 5); `TileSize`, `Cols` (Task 1); `agentInfo` de `web/src/ui/agents`; `ProjectTag`; `minutesSince` de `../boardModel`.
- Produces:

```tsx
export interface GridTileProps {
  t: TerminalInfo; project: Project | null; projects: Project[]
  size: TileSize; cols: Cols; simple: boolean
  live: boolean            // ligado pelo orçamento
  focused: boolean
  onFocus(): void          // clique ou foco dentro do tile
  onWake(): void           // clique no corpo em pausa
  onStep(dir: 1 | -1): void; onToggleFull(): void
  onResize(s: TileSize): void          // ao soltar o canto
  onDragStart(): void; onDragOverTile(after: boolean): void; onDropTile(after: boolean): void; onDragEnd(): void
  dropMark: 'before' | 'after' | null
}
export const GridTile: React.NamedExoticComponent<GridTileProps>   // React.memo
```

- O elemento raiz é `<section data-tile={t.id}>` com `style={{ gridColumn: 'span N', height }}` e `contain: 'layout paint'`.
- Cabeçalho (`draggable`): pega ⠿, cor e nome do agente, card com link (`useWithProject`), estado ("ao vivo" / "encerrado (código N)"), botões `−`, `+`, `⤢` e o menu `⋯`. No Detalhado, também projeto/subprojeto, origem, "há N min" e clientes.
- Menu `⋯` (só com o tile ligado e o terminal vivo): "Interagir" ou "Somente assistir" (chama `setReadonly`) e "Encerrar agente" (chama `kill`, com a mesma confirmação em painel que o `TerminalView` usa hoje; não usar `confirm()` do navegador).
- Corpo: se `live`, `<LazyTerminalView grid readonly active={focused} …>`; senão um botão ocupando o corpo com o texto "Em pausa. Clique para ligar." que chama `onWake`.
- Canto de redimensionar: `pointerdown` captura o ponteiro; no movimento muda só `style.height` e `style.gridColumn` do próprio elemento (via `snapSpan` e `clampH`) e mostra o indicador "N de C col · H px"; ao soltar chama `onResize` uma vez e `handle.fit()`.

- [ ] **Step 1: Write the failing tests** (com `LazyTerminalView` simulado por `vi.mock`): em pausa mostra "Em pausa. Clique para ligar." e clicar chama `onWake`; ligado renderiza o terminal com `grid` e `readonly`; Simples esconde origem e clientes, Detalhado mostra; `−`/`+`/`⤢` chamam `onStep(-1)`, `onStep(1)`, `onToggleFull`; o menu não aparece em terminal encerrado; "Interagir" chama `setReadonly(false)` no handle; `dragstart` no cabeçalho chama `onDragStart`; `dragover` na metade direita chama `onDragOverTile(true)`; gesto no canto (`pointerdown` → `pointermove` → `pointerup`) chama `onResize` uma vez com a altura limitada; os botões têm `aria-label` ("Diminuir terminal", "Aumentar terminal", "Ocupar a linha toda").
- [ ] **Step 2:** Run `npx vitest run src/pages/terminals/GridTile.test.tsx`. Expected: FAIL.
- [ ] **Step 3:** Implementar `GridTile.tsx`.
- [ ] **Step 4:** Run e `npx tsc --noEmit`. Expected: PASS.
- [ ] **Step 5:** Commit `feat(web): tile da grade de terminais (NAC-63)`.

### Task 7: Página Terminais em grade

**Files:**
- Create: `web/src/pages/terminals/useOnScreen.ts`, `web/src/pages/terminals/usePageHidden.ts`
- Modify: `web/src/pages/Terminals.tsx` (substitui a tabela), `web/src/pages/Terminals.test.tsx`
- Test: `web/src/pages/Terminals.test.tsx`, `web/src/pages/terminals/usePageHidden.test.ts`

**Interfaces:**
- Consumes: tudo das Tasks 1, 2, 3 e 6; `useTerminals`, `useProjects`, `useCards`, `useProjectFilter`.
- Produces: `useOnScreen(root: RefObject<HTMLElement>): Set<string>` (IntersectionObserver sobre `[data-tile]`, com `rootMargin: '200px'`; sem `IntersectionObserver` no ambiente, devolve todos os ids); `usePageHidden(ms = HIDDEN_PAUSE_MS): boolean` (vira `true` só depois de `ms` com `document.hidden`, volta a `false` na hora em que a página aparece).

- [ ] **Step 1: Write the failing tests** em `Terminals.test.tsx` (reaproveitar as fixtures e o `GridTile` real com `LazyTerminalView` simulado):
  - abas com contagem; clicar numa aba filtra os tiles; chips aparecem só para projeto com filhos; "Geral" lista os da raiz;
  - alternar Simples/Detalhado e colunas regrava o `localStorage` na chave `nobre.terminais.grade.v1`;
  - filtro "Ao vivo" esconde os encerrados;
  - "Restaurar layout" volta ordem e tamanhos ao padrão;
  - arrastar o tile A para depois do B (eventos de drag) muda a ordem na tela e no layout salvo;
  - `?id=X` abre na aba "Todos" com o tile X em foco e ocupando a linha toda; `?id=inexistente` mostra "Este terminal não existe mais (encerrado e removido)." e a grade continua;
  - com o filtro global de projeto ativo e sem `id`, a aba inicial é a do projeto;
  - terminal de `nobre run` e terminal da plataforma abrem ambos com `readonly`;
  - com 12 terminais vivos, 9 tiles ligados e 3 em pausa;
  - estados de carregando, erro e "Nenhum terminal aberto…" seguem com os textos de hoje.
  - `usePageHidden.test.ts`: com timers falsos, `false` antes de 30 s escondida, `true` depois, `false` ao voltar.
- [ ] **Step 2:** Run `npx vitest run src/pages/Terminals.test.tsx src/pages/terminals`. Expected: FAIL nos casos novos.
- [ ] **Step 3:** Implementar. Estado do layout num `useReducer` iniciado com `loadLayout()`; `saveLayout` num efeito. `reconcile` a cada mudança da lista de terminais. `lastUsed` e `woken` em `useRef` + estado mínimo, para que uma interação num tile não redesenhe os outros (os callbacks passados ao `GridTile` são estáveis, com `useCallback` por id). Grade com `gridTemplateColumns: repeat(cols, minmax(0, 1fr))`, `alignItems: 'start'`, e as quebras de 1100/700 px por `matchMedia` (limita `cols` efetivo, sem regravar a preferência).
- [ ] **Step 4:** Run `npx vitest run && npx tsc --noEmit` (suíte web inteira). Expected: PASS. Ajustar testes de outras telas só se dependiam da tabela antiga.
- [ ] **Step 5:** Commit `feat(web): Terminais em grade — abas, arrastar, redimensionar, Simples/Detalhado (NAC-63)`.

### Task 8: Bancada de performance

**Files:**
- Create: `web/bench/index.html`, `web/bench/main.tsx`, `web/bench/load.ts`
- Modify: `web/package.json` (script `"bench": "vite --open /bench/index.html"`), `docs/manual.md` se existir no branch (uma seção curta "Bancada de performance dos terminais")
- Test: `web/bench/load.test.ts`

**Interfaces:**
- Consumes: `TerminalView` com `grid` e uma `wsFactory` falsa que entrega os blocos gerados (o componente já aceita `wsFactory` por prop).
- Produces, em `load.ts`: `export type Profile = 'agente' | 'enxurrada'`; `export function tick(p: Profile, k: number): Uint8Array` (agente: redesenha 8 linhas a cada 80 ms e solta 30 linhas a cada 25 ticks; enxurrada: 20 linhas de 120 colunas a cada 10 ms); `export function verdict(m: { longMax: number; lagP95: number; lagMax: number }): 'aprovado' | 'reprovado'` com a meta `longMax < 50`, `lagP95 < 50`, `lagMax < 100`.

- [ ] **Step 1: Write the failing tests** em `load.test.ts`: `tick('agente', 1)` contém a sequência de subir 8 linhas (`ESC[8A`); `tick('enxurrada', 1)` tem 20 quebras de linha; `verdict({longMax:0,lagP95:10,lagMax:40})` → `'aprovado'`; `verdict({longMax:60,…})` → `'reprovado'`.
- [ ] **Step 2:** Run `npx vitest run bench/load.test.ts`. Expected: FAIL. (Se o vitest não incluir `bench/`, ajustar o `include` da configuração.)
- [ ] **Step 3:** Implementar `load.ts` e a página: 9 tiles de 440×300 com o `TerminalView` real em modo `grid`; botões "Agente" e "Enxurrada"; 1 s de aquecimento e 6 s de medição; mostra quadros por segundo, tarefas longas (número, soma, máxima), atraso da thread principal (p95 e máximo), o renderizador em uso (`dom` ou `webgl`) e o veredito. Um terceiro botão, "Religar", desmonta e remonta os 9 com um snapshot de 2 MiB e mostra o custo à parte, sem entrar no veredito. Avisar na página quando `document.visibilityState !== 'visible'` ou quando a taxa de quadros ficar abaixo de 5 (janela encoberta invalida a medição).
- [ ] **Step 4:** Run `npx vitest run bench && npx tsc --noEmit && npm run build`. Expected: PASS, e o build de produção não inclui a bancada.
- [ ] **Step 5:** Commit `test(web): bancada de performance da grade de terminais (NAC-63)`.
- [ ] **Step 6 (manual, antes do merge):** rodar `npm run bench` num Chrome com GPU e janela visível, nos dois perfis, e registrar os números no card com `nobre note`. A entrega só é mergeada com "aprovado" nos dois perfis com WebGL.

---

## Verificação final (antes da revisão do branch)

- `go build ./... && go test ./...`
- `cd web && npx tsc --noEmit && npx vitest run && npm run build`
- Conferir à mão, com o daemon de desenvolvimento: abrir `/terminals`, trocar de aba, arrastar, redimensionar, "Interagir" num tile, abrir a página de um card e ver que o terminal de lá não mudou.
