# Barra de entrada nos Terminais — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cada tile ligado da grade de Terminais ganha uma barra que envia texto ao agente, com `#` para prompts salvos e `Ctrl+K` para uma paleta de prompts, cards e memórias.

**Architecture:** O daemon ganha `POST /api/terminals/{id}/input`, que faz colagem marcada, espera e manda Enter, sem redimensionar o PTY. No web, a barra (`TileInput`) vive dentro do tile com estado local e se registra num registro por id de terminal; a paleta vive na página e entrega a inserção pelo registro, sem passar texto por props dos tiles.

**Tech Stack:** Go (net/http, internal/pty), React 18 + TypeScript, @tanstack/react-query, vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-11-27-terminais-entrada-design.md`

## Global Constraints

- Trabalhe só no worktree `.worktrees/terminais`, branch `feat/terminais-entrada`. Nunca rode `nobre`, `make update`, `make install` nem `nobre setup` (há um daemon vivo).
- Textos da interface em português do Brasil, exatamente como escritos neste plano.
- Go: `go build ./... && go vet ./... && go test ./...` limpos. Web (em `web/`): `npx vitest run`, `npx tsc --noEmit` limpos. Dependências: `pnpm install --frozen-lockfile`; nenhuma dependência nova.
- `GridTile` e `TileSlot` continuam memoizados: props novas só de valores simples ou callbacks estáveis. O texto digitado é estado do tile; digitar não pode re-renderizar `Terminals`.
- A barra nunca manda resize ao PTY e não troca o modo do xterm.
- Protocolo de envio (medido): `ESC[200~` + texto + `ESC[201~`, espera de 150 ms, depois `\r` em escrita separada.
- Limite do texto: 256 KiB (262144 bytes) depois da limpeza.
- Commits em português, no padrão `feat(api): …`/`feat(web): …`, terminando com `(NAC-63)`.
- Existe um teste instável conhecido, fora deste plano: `web/src/ui/TerminalView.test.tsx` > "o LazyTerminalView repassa o ref ao TerminalView". Se só ele falhar, rode de novo.

## Review Focus

1. Texto que contém `ESC[201~` no meio: não pode fechar a colagem antes da hora (Task 1 testa a limpeza).
2. Dois envios quase simultâneos ao mesmo terminal: a colagem de um não pode cair entre a colagem e o Enter do outro (Task 1).
3. `Enter` na barra com a lista do `#` aberta e sem nenhum prompt casando: não pode enviar `#abc` por engano nem travar; envia só depois de `Esc` (Task 4).
4. Escolher um prompt, editar e apagar tudo, digitar outra coisa e enviar: o uso do prompt não pode ser contado (Task 4).
5. `Ctrl+K` com o foco num xterm em "Interagir": a paleta não abre e o evento não recebe `preventDefault` (Task 5).

---

### Task 1: Rota de entrada no daemon

**Files:**
- Modify: `internal/pty/manager.go` (campo em `Options`, método novo em `Terminal`)
- Create: `internal/pty/input.go`, `internal/pty/input_test.go`
- Modify: `internal/api/terminals.go` (rota e handler)
- Test: `internal/api/terminals_test.go` (guarde o `*store.Store` no `termEnv` se precisar)

**Interfaces:**
- Produces (pty):
  - `const MaxInput = 256 << 10`
  - `func CleanInput(text string) string` — troca `\r\n` e `\r` por `\n`, remove toda ocorrência de `"\x1b[200~"` e `"\x1b[201~"` (repita até não sobrar: `"\x1b[20\x1b[201~1~"` não pode virar um marcador), tira `\n` do fim.
  - `Options.PasteSettle time.Duration` — 0 vira 150 ms em `NewManager`.
  - `func (t *Terminal) Submit(text string) error` — `text` já limpo. Segura um mutex próprio do terminal (`inMu`) durante a sequência inteira; `Write("\x1b[200~"+text+"\x1b[201~")`, `time.Sleep(settle)`, `Write("\r")`. Erro da primeira escrita devolve sem tentar o Enter; depois da colagem o Enter é sempre tentado.
- Produces (HTTP): `POST /api/terminals/{id}/input`, corpo `{"text": string, "prompt": string}`. Respostas: `204`; `400` "texto vazio"; `404` `msgTerminalMissing`; `409` "terminal encerrado" (`pty.ErrEnded`, antes ou no meio); `413` "texto grande demais" (acima de `pty.MaxInput` depois da limpeza); `504` "o agente não leu a entrada" (`errors.Is(err, os.ErrDeadlineExceeded)`); outro erro `500`.
- Com `204` e `prompt != ""`: `s.st.MarkPromptUsed(prompt, s.now())`; `store.ErrNotFound` é ignorado; se contou, `s.publish("prompts")`.
- O handler não usa `r.Context()` para a sequência.

- [ ] **Step 1: Testes de `CleanInput` (falhando)** em `internal/pty/input_test.go`, tabela:

```go
{"a\r\nb\rc", "a\nb\nc"},
{"x\x1b[201~y\x1b[200~z", "xyz"},
{"\x1b[20\x1b[201~1~fim", "fim"},
{"oi\n\n", "oi"},
{"\n\n", ""},
{" !eco", " !eco"}, // espaços do começo ficam
```

- [ ] **Step 2: Testes de `Submit` (falhando)** em `internal/pty/input_test.go`, com `cat -v` e `Options{PasteSettle: 20 * time.Millisecond}`: (a) `Submit("l1\nl2")` faz a saída conter `^[[200~l1` e depois `l2^[[201~`; (b) duas goroutines chamando `Submit("AAAA")` e `Submit("BBBB")` ao mesmo tempo: na saída, o marcador `^[[200~` da segunda sequência só aparece depois de passados ao menos 15 ms do `^[[201~` da primeira (as sequências não se intercalam); (c) `Submit` em terminal encerrado devolve `ErrEnded`.
- [ ] **Step 3: Rode** `go test ./internal/pty/ -run 'CleanInput|Submit'` — falha por símbolo ausente.
- [ ] **Step 4: Implemente** `CleanInput`, `MaxInput`, `PasteSettle` e `Submit`.
- [ ] **Step 5: Rode** o mesmo comando — passa. Rode também com `-race`.
- [ ] **Step 6: Testes do handler (falhando)** em `internal/api/terminals_test.go`, com `cat -v` como agente e `PasteSettle: time.Millisecond`:
  - `TestTerminalInputWrites`: `POST …/input {"text":"oi\r\nmundo\n"}` → 204; a saída lida por um WebSocket `?readonly=1` contém `^[[200~oi` e `mundo^[[201~`; o tamanho do PTY (`Info().Cols/Rows`) não mudou.
  - `TestTerminalInputValidation`: `{"text":"\n\n"}` → 400; id inexistente → 404; terminal encerrado (mate e espere `Done`) → 409; texto de `pty.MaxInput+1` bytes → 413; sem `Content-Type: application/json` → 415.
  - `TestTerminalInputMarksPromptUsed`: crie um prompt no store, envie com `"prompt": slug` → `uses == 1`; envie com slug inexistente → 204; um envio que dá 409 não muda `uses`.
- [ ] **Step 7: Rode** `go test ./internal/api/ -run TerminalInput` — falha (404 da rota).
- [ ] **Step 8: Implemente** `terminalInput` e registre `mux.HandleFunc("POST /api/terminals/{id}/input", guard(s.terminalInput))`. Atualize o comentário de `terminalWS`/rotas para citar a entrada por HTTP.
- [ ] **Step 9: Rode** `go build ./... && go vet ./... && go test ./...` — tudo passa.
- [ ] **Step 10: Commit** `feat(api): entrada por HTTP nos terminais, com colagem marcada e Enter (NAC-63)`.

### Task 2: Modelo da barra e da paleta (funções puras e chamada da API)

**Files:**
- Create: `web/src/pages/terminals/inputModel.ts`, `web/src/pages/terminals/inputModel.test.ts`
- Create: `web/src/pages/terminals/inputRegistry.ts`, `web/src/pages/terminals/inputRegistry.test.ts`

**Interfaces (Produces):**

```ts
// inputModel.ts
export const MAX_GROUP = 8
export function promptsFor(prompts: Prompt[], agent: string, project: Project | null, query: string): Prompt[]
export function insertAt(text: string, start: number, end: number, piece: string): { text: string; caret: number }
export function cardPiece(c: Card): string
export function memoryPiece(m: Memory): string
export function paletteCards(cards: Card[], query: string): Card[]
export function paletteMemories(mems: Memory[], project: Project | null, query: string): Memory[]
export function commandWarning(text: string): boolean
export function sendInput(terminalId: string, text: string, prompt?: string): Promise<void>
export function inputError(e: unknown): string

// inputRegistry.ts
export interface TileInputHandle {
  insert(piece: string): void          // insere na posição do cursor e foca a barra
  pickPrompt(slug: string): void       // mesmo efeito de escolher na lista do #
  focus(): void
}
export function registerInput(terminalId: string, h: TileInputHandle): () => void   // devolve o cancelamento
export function inputFor(terminalId: string | null): TileInputHandle | null
```

Regras:
- `promptsFor`: entra o prompt cujo `agents` inclui `agent` e cujo `project_id` é `null`, `project.id` ou `project.parent_id`; com `project === null` só os de `project_id === null`. `query` (sem o `#`, sem diferenciar maiúsculas nem acentos) casa em `name`, `slug` ou `tag`. Ordem: `uses` decrescente, empate por `name`. Sem corte.
- `insertAt`: troca `text[start,end)` por `piece`; `caret = start + piece.length`.
- `cardPiece`: `"NAC-12 · Título"`; com `where_stopped` não vazio, mais `"\nOnde parei: " + where_stopped`.
- `memoryPiece`: `rule`; com `do` não vazio, `rule + " — " + do`.
- `paletteCards`: `column !== 'concluido'`, `updated_at` decrescente, `query` casa em `key` ou `title`; no máximo `MAX_GROUP`.
- `paletteMemories`: só `status === 'ativa'`; com `project === null`, só `project_id === null`; `query` casa em `rule` ou `do`; no máximo `MAX_GROUP`. (A lista por projeto já vem do servidor com as herdadas.)
- `commandWarning`: verdadeiro se o primeiro caractere de `text` é `#`, `/` ou `!`.
- `sendInput`: `post('/api/terminals/' + id + '/input', { text, prompt: prompt ?? '' })`.
- `inputError`: `ApiError` 404 ou 405 → `"Este daemon não aceita entrada pela barra. Atualize o Cockpit."`; 409 → `"Terminal encerrado."`; 413 → `"Texto grande demais para enviar."`; 504 → `"O agente não leu a entrada. Tente de novo."`; qualquer outro → `"Não foi possível enviar."`.
  - Atenção: o 404 de terminal inexistente e o de rota ausente são indistinguíveis; a mensagem do daemon antigo vale para os dois.
- Registro: um `Map` de módulo. Registrar o mesmo id de novo substitui; o cancelamento só apaga se o registrado ainda for o mesmo handle.

- [ ] **Step 1: Testes (falhando)** cobrindo cada regra acima com um caso positivo e um negativo; para `promptsFor`: agente fora da lista, projeto filho herdando o do pai, `project === null`, busca `"revi"` casando `"Revisão"`; para o registro: substituição e cancelamento velho que não apaga o novo.
- [ ] **Step 2: Rode** `cd web && npx vitest run src/pages/terminals/inputModel.test.ts src/pages/terminals/inputRegistry.test.ts` — falha por módulo ausente.
- [ ] **Step 3: Implemente** os dois módulos.
- [ ] **Step 4: Rode** os testes e `npx tsc --noEmit` — passam.
- [ ] **Step 5: Commit** `feat(web): modelo da barra de entrada e da paleta dos terminais (NAC-63)`.

### Task 3: Barra no tile (envio, erros, `nobre run`, dicas)

**Files:**
- Create: `web/src/pages/terminals/TileInput.tsx`, `web/src/pages/terminals/TileInput.test.tsx`
- Modify: `web/src/pages/terminals/GridTile.tsx`, `web/src/pages/terminals/GridTile.test.tsx`

**Interfaces:**
- Consumes: `sendInput`, `inputError`, `insertAt`, `registerInput`, `TileInputHandle` (Task 2).
- Produces:

```ts
export interface TileInputProps {
  terminalId: string; agent: string; agentName: string
  cardKey: string; project: Project | null
  run: boolean        // terminal de `nobre run`
  simple: boolean
}
export const TileInput: React.MemoExoticComponent<(p: TileInputProps) => JSX.Element>
```

Comportamento:
- `run && !liberado`: rodapé com o texto `Sessão do nobre run: você só assiste.` e o botão `Liberar entrada`; clicar mostra a barra (estado local, não persiste).
- Barra: `<textarea aria-label={"Enviar ao agente " + agentName} placeholder="Enviar ao agente…  # prompts salvos" rows={1}>` que cresce até 6 linhas (ajuste `rows` pela contagem de `\n`, entre 1 e 6) e o botão `Enviar`.
- `Enter` envia; `Shift+Enter` quebra linha; `Enter` com `e.nativeEvent.isComposing` não envia. Texto só com espaços: `Enviar` desligado e `Enter` não faz nada.
- Envio: `sendInput(terminalId, texto)`. Pendente: textarea continua editável, botão e `Enter` desligados. Sucesso: campo esvazia e mantém o foco. Erro: texto fica, `<p role="alert">` com `inputError(e)`; some na próxima edição.
- `!simple`: linha de dicas `/ @ vão direto ao agente · # prompts · Ctrl K buscar`.
- Registra `TileInputHandle` com `registerInput(terminalId, …)` enquanto a barra existe (não registra com `run` não liberado). `pickPrompt` nesta task só foca; a Task 4 completa.
- O rodapé tem `padding-right: 30px` (o canto de redimensionar fica por cima).
- `onClick`/`onFocus` do tile continuam valendo (a barra está dentro da `<section>`), então focar a barra põe o tile em foco.

`GridTile`: logo depois da `<div>` do corpo (a de `flex: '1 1 auto'`), renderize `{live && !ended && <TileInput … />}` com `terminalId={t.id}`, `agent={t.agent}`, `agentName={agent.name}`, `cardKey={card}`, `project={project}`, `run={t.origin === 'nobre run'}`, `simple={simple}`. Na `<div style={{ flex: 1, minWidth: 0, padding: 4 }}>` que envolve o `LazyTerminalView`, acrescente `data-interactive={ro ? undefined : '1'}` (a Task 5 lê).

- [ ] **Step 1: Testes do `TileInput` (falhando)**, com `fetch` falso:
  - Enter envia `{text:'oi', prompt:''}` para `/api/terminals/t1/input` e esvazia; Shift+Enter não envia; composição não envia; texto `'   '` não envia.
  - resposta 409 mantém o texto e mostra `Terminal encerrado.`; 404 mostra a mensagem do daemon antigo.
  - `run` mostra `Liberar entrada`; depois do clique a barra aparece e `inputFor('t1')` deixa de ser `null`.
  - `simple` esconde a linha de dicas.
  - `inputFor('t1')!.insert('abc')` com `'x|y'` (cursor no meio) dá `'xabcy'`.
  - desmontar tira o registro.
- [ ] **Step 2: Testes do `GridTile` (falhando):** tile ligado mostra a barra; tile em pausa (`live={false}`) e tile encerrado não mostram; `t.origin === 'nobre run'` mostra `Liberar entrada`; digitar na barra não chama nenhum callback do tile além de `onFocus`.
- [ ] **Step 3: Rode** `cd web && npx vitest run src/pages/terminals/TileInput.test.tsx src/pages/terminals/GridTile.test.tsx` — falham.
- [ ] **Step 4: Implemente** `TileInput` e a integração no `GridTile`.
- [ ] **Step 5: Rode** `npx vitest run src/pages` e `npx tsc --noEmit` — passam.
- [ ] **Step 6: Commit** `feat(web): barra de entrada no tile dos terminais (NAC-63)`.

### Task 4: `#` — prompts salvos na barra

**Files:**
- Modify: `web/src/pages/terminals/TileInput.tsx`, `web/src/pages/terminals/TileInput.test.tsx`

**Interfaces:**
- Consumes: `promptsFor`, `commandWarning` (Task 2); `usePrompts` de `web/src/api/queries.ts`; `POST /api/prompts/{slug}/render` com `{ card }` (sem `use`) → `{ text }`; 422 traz `body.unknown: string[]`; 404 com `card` não vazio → repita com `card: ''`.
- Produces: `TileInputHandle.pickPrompt(slug)` completo.

Comportamento:
- Texto começando com `#` e lista não fechada por `Esc`: `<ul role="listbox" aria-label="Prompts salvos">` acima da barra, com `promptsFor(prompts, agent, project, texto.slice(1))`; cada opção mostra `name` e a primeira linha de `description || body`. Lista vazia: `Nenhum prompt com “<busca>”.`
- Setas ↑/↓ movem `aria-selected`; `Enter` escolhe a opção marcada (com lista vazia, `Enter` não faz nada); clique escolhe; `Esc` fecha a lista e mantém o texto. Com a lista fechada por `Esc`, `Enter` envia o texto como está. A lista reabre quando o texto deixa de começar com `#` e volta a começar.
- Escolher: renderiza; no sucesso troca o texto da barra pelo resultado, guarda o slug e mostra `Prévia: o agente vai receber este texto. Edite antes de enviar.` com o botão `Limpar` (esvazia a barra e esquece o slug). Tile sem card (ou 404 do card): acrescenta `Sem card: as variáveis do card ficaram vazias.` Se `commandWarning(texto)`: acrescenta `O agente pode tratar o primeiro caractere como comando.`
- 422: a barra não muda; a lista mostra `<p role="alert">Variável desconhecida: {{a}}, {{b}}</p>`. Outro erro: `Não foi possível carregar o prompt.`
- Envio com slug guardado manda `prompt: slug`. O slug é esquecido quando o texto fica vazio, em `Limpar` e depois de um envio com sucesso.
- A lista do `#` só existe montada enquanto aberta. `usePrompts` é a mesma consulta em todos os tiles (uma chamada só).

- [ ] **Step 1: Testes (falhando):** abrir com `#`, filtrar com `#rev`, setas + Enter escolhendo o segundo, clique, Esc mantém o texto e Enter depois envia `#abc`, Enter com lista vazia não envia, 422 mostra a variável, tile sem card mostra o aviso, envio depois de escolher manda `prompt: 'slug'`, escolher → apagar tudo → digitar `x` → enviar manda `prompt: ''`, `Limpar` esvazia, `inputFor(id)!.pickPrompt('slug')` tem o mesmo efeito do clique.
- [ ] **Step 2: Rode** `cd web && npx vitest run src/pages/terminals/TileInput.test.tsx` — falham os novos.
- [ ] **Step 3: Implemente.**
- [ ] **Step 4: Rode** `npx vitest run src/pages` e `npx tsc --noEmit` — passam.
- [ ] **Step 5: Commit** `feat(web): # abre os prompts salvos na barra do terminal (NAC-63)`.

### Task 5: `Ctrl+K` — paleta

**Files:**
- Create: `web/src/pages/terminals/Palette.tsx`, `web/src/pages/terminals/Palette.test.tsx`
- Modify: `web/src/pages/Terminals.tsx`, `web/src/pages/Terminals.test.tsx`

**Interfaces:**
- Consumes: `promptsFor`, `paletteCards`, `paletteMemories`, `cardPiece`, `memoryPiece`, `MAX_GROUP`, `inputFor` (Task 2); `usePrompts`, `useCards`, `useMemories(projectId, true)`.
- Produces:

```ts
export interface PaletteProps {
  targetId: string | null       // tile em foco na página
  targetLabel: string           // ex.: "Claude Code · NAC-63"; '' sem alvo
  agent: string                 // '' sem alvo
  project: Project | null       // projeto do alvo
  onClose(): void
}
export function Palette(p: PaletteProps): JSX.Element
```

Comportamento:
- `<div role="dialog" aria-modal="true" aria-label="Buscar">` com `<input aria-label="Buscar" placeholder="Prompt salvo, card ou memória">` em foco ao abrir. `Esc` e clique fora fecham.
- Cabeçalho: com alvo utilizável (`inputFor(targetId) !== null`), `Inserir em: <targetLabel>`; senão `Clique num terminal para escolher onde inserir.` e todos os itens `disabled`.
- Grupos, cada um com título e no máximo `MAX_GROUP` itens: `Prompts salvos` (`promptsFor(prompts, agent, project, busca)`; sem alvo, todos os prompts filtrados só pela busca), `Cards` (`paletteCards`), `Memórias` (`paletteMemories(mems, project, busca)`; consulta `useMemories(project?.id ?? null, true)`). Sem busca, o grupo `Memórias` não aparece. Nada em nenhum grupo: `Nada encontrado.`
- Setas ↑/↓ percorrem todos os itens habilitados; `Enter` escolhe o marcado.
- Escolher fecha a paleta e chama, no handle do alvo: prompt → `pickPrompt(slug)`; card → `insert(cardPiece(c))`; memória → `insert(memoryPiece(m))`.

`Terminals.tsx`:
- Estado `palette: boolean`. Botão no cabeçalho, depois de `Restaurar layout`: `Buscar` com `<kbd>Ctrl K</kbd>`, visível quando `list.length > 0`.
- Um `keydown` em `window` (registrado uma vez): `(e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k'`. Se `document.activeElement?.closest('[data-interactive="1"]')` existe, não faz nada (sem `preventDefault`). Senão `e.preventDefault()` e abre a paleta.
- `<Palette>` só é montada com `palette === true`; recebe `targetId={focused}`, o agente e o projeto do terminal em foco (de `list`/`placedById`/`projectById`) e `targetLabel` = nome do agente + ` · ` + card do tile (`meters.get(id).cardKey`), sem o card quando vazio.
- `TileSlot`/`GridTile` não ganham props novas nesta task.

- [ ] **Step 1: Testes da `Palette` (falhando):** registre um handle falso com `registerInput('t1', …)`; escolher um card chama `insert('NAC-7 · Título\nOnde parei: x')`; escolher um prompt chama `pickPrompt('slug')`; memória só aparece com busca e insere `regra — fazer`; sem handle registrado os itens ficam `disabled` e aparece o aviso; setas + Enter; Esc chama `onClose`; card `concluido` não aparece.
- [ ] **Step 2: Testes da página (falhando)** em `Terminals.test.tsx`: o botão `Buscar` abre o diálogo; `Ctrl+K` em `window` abre e o evento tem `defaultPrevented === true`; com o foco num elemento dentro de `[data-interactive="1"]` (monte um à mão no documento) não abre e `defaultPrevented === false`; abrir e fechar a paleta não re-renderiza os tiles (use o contador de render que os testes de memoização da página já usam; se não houver, verifique que o `GridTile` falso não foi chamado de novo).
- [ ] **Step 3: Rode** `cd web && npx vitest run src/pages/terminals/Palette.test.tsx src/pages/Terminals.test.tsx` — falham os novos.
- [ ] **Step 4: Implemente** `Palette` e a integração na página.
- [ ] **Step 5: Rode** `npx vitest run`, `npx tsc --noEmit` e `npm run build` — passam.
- [ ] **Step 6: Commit** `feat(web): paleta Ctrl+K com prompts, cards e memórias nos terminais (NAC-63)`.

---

## Verificação final (quem coordena)

- Suíte inteira: `go build ./... && go vet ./... && go test ./...`; em `web/`: `npx vitest run && npx tsc --noEmit && npm run build`.
- Bancada: `cd web && npx vite --port 5199`, `/bench/index.html`, pelos botões, janela visível; mesma meta da entrega 1.
- Só depois do `make update` do usuário (o daemon instalado não tem a rota): enviar uma linha e várias linhas a um Claude e a um Kiro; `#` com um prompt que usa `{{card.onde_parei}}`; `Ctrl+K` inserindo um card; tile de `nobre run` não muda de tamanho.
