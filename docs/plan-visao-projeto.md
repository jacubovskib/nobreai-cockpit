# Visão por projeto — Parte 1 (seletor, etiquetas e página Projetos): plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Seletor de projeto global, etiqueta de projeto e sinais "parado" / "aguardando revisão" nos cards, e uma página Projetos que compara os projetos e mostra o cronograma de entregas de cada um.

**Architecture:** O daemon calcula o sinal de cada card (`domain.CardSignal`) e o devolve em `CardSummary`. O custo por projeto vem de um novo agrupamento `project` em `store.Totals`, que também aceita filtro por projeto. O resto é interface: o projeto escolhido vive na URL (`?projeto=<id>`). As telas filtram no cliente com os dados que já buscam. A página Projetos junta `/api/projects`, `/api/cards` e `/api/usage/totals?group=project`, sem endpoint novo.

**Tech Stack:** Go 1.26 (SQLite, `net/http`), React + React Router + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-10-visao-por-projeto-design.md`. Card NAC-5. A Parte 2 (plano no card, `nobre plan` / `nobre task`, lembretes do hook, blocos de progresso) tem plano próprio, escrito depois que o NAC-4 entrar na `main`, porque mexe nos mesmos hooks e na prontidão.

## Global Constraints

- Valem as constraints de `docs/plan.md`. Toda a UI em pt-BR.
- Nunca tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado (porta 7420). Testes em `t.TempDir()` e `httptest`.
- Datas no SQL são comparadas com `julianday(col)`, nunca como texto.
- Sem migração nesta parte.
- **Parado**: card em `em_andamento` ou `pausado` cuja última atividade foi há **mais de 24 h**.
- **Aguardando revisão**: card em `revisao` cuja última atividade foi há mais de 24 h. Nunca é chamado de "parado".
- Última atividade de um card = o maior entre `last_activity_at` (sessões) e `updated_at` (mudança de coluna ou "Onde parei").
- Backlog e Concluído nunca têm sinal.
- O projeto escolhido fica na URL como `?projeto=<id>`. Sem o parâmetro, ou com id desconhecido, vale "Todos".
- **Filtram pelo seletor:** Board, Cards, Analytics, Worktrees e Prompts (globais + do projeto). **Não filtram:** Sala de controle e Terminais (só mostram a etiqueta), Configuração.
- Etiqueta de projeto: sempre com o nome. Paleta fixa, por ordem de `id`, em ciclo: `#B07CE8`, `#C9A227`, `#E26D9C`, `#4FB3BF`, `#9DB255`, `#C38D6B`. Nunca as cores dos agentes (`#3987E5`, `#D95926`, `#199E70`).
- Textos exatos: "Todos os projetos", "Sem projeto", "parado há …", "aguardando revisão há …", "Aguardando você", "captura desligada", "nada em andamento".
- Período padrão da página Projetos: 7 dias (opções Hoje / 7 dias / 30 dias, como no Analytics).

## Review Focus

1. **`?projeto=` com id apagado ou lixo (`?projeto=abc`)**: as telas mostram "Todos" sem erro, e o seletor mostra "Todos os projetos" (teste na Task 2).
2. **Card exatamente com 24 h sem atividade**: ainda sem sinal. Com 24 h e 1 s, ganha o sinal (teste na Task 1).
3. **Card sem nenhuma sessão** (criado pela UI, nunca trabalhado) em Pausado há dias: o sinal usa `updated_at` (teste na Task 1).
4. **Uso de sessão sem card e sem projeto**: entra no grupo `""` de `group=project`. A página Projetos mostra a linha "Sem projeto" só se o custo for maior que zero (testes nas Tasks 1 e 5).
5. **Trocar de tela com um projeto escolhido**: o parâmetro acompanha os links do menu. O botão "Todos os projetos" o remove (teste na Task 2).

---

### Task 1: Daemon — sinal do card e custo por projeto

**Files:**
- Create: `internal/domain/signal.go`, `internal/domain/signal_test.go`
- Modify: `internal/api/detail.go` (`CardSummary`, `cardSummary`), `internal/store/usage.go` (`TotalsFilter`, `Totals`), `internal/api/usage.go` (`usageTotals`)
- Test: `internal/store/usage_test.go` (ou o teste de totais existente), `internal/api/cards_test.go`, `internal/api/usage_test.go` (crie se não existir)

**Interfaces:**
- Produces:
  - `domain.StaleAfter = 24 * time.Hour`
  - `type Signal string` com `SignalNone Signal = ""`, `SignalParado Signal = "parado"`, `SignalAguardandoRevisao Signal = "aguardando_revisao"`
  - `func CardSignal(col Column, lastActivity, now time.Time) Signal`
  - `CardSummary` ganha `Signal domain.Signal \`json:"signal"\`` e `SignalSince *time.Time \`json:"signal_since"\``. `SignalSince` é a última atividade usada no cálculo, preenchida só quando `Signal != ""`.
  - `store.TotalsFilter` ganha `ProjectID *int64`. O `GroupBy` aceita `"project"`, cuja chave é o id do projeto em decimal, ou `""` quando não há projeto.
  - `GET /api/usage/totals` aceita `group=project` e `project=<id>`. Com id não numérico, devolve 400 "projeto inválido".

- [ ] **Step 1: Write the failing test** `TestCardSignal` (tabela) em `internal/domain/signal_test.go`. Com `now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)`:
  - `(EmAndamento, now-24h) → SignalNone`
  - `(EmAndamento, now-24h-1s) → SignalParado`
  - `(Pausado, now-72h) → SignalParado`
  - `(Revisao, now-25h) → SignalAguardandoRevisao`
  - `(Revisao, now-2h) → SignalNone`
  - `(Backlog, now-30*24h) → SignalNone`
  - `(Concluido, now-30*24h) → SignalNone`
- [ ] **Step 2: Run** `go test ./internal/domain/ -run TestCardSignal`. Expected: FAIL (`undefined: CardSignal`).
- [ ] **Step 3: Implement** `CardSignal` em `internal/domain/signal.go` (regra: `now.Sub(last) > StaleAfter`).
- [ ] **Step 4: Run** o mesmo comando. Expected: PASS.
- [ ] **Step 5: Write the failing tests** em `internal/api/cards_test.go`, com `testNow` do pacote:
  - `TestListCardsSignal`: card em `pausado` cuja sessão encerrou em `testNow-48h` → `signal == "parado"` e `signal_since` igual a esse instante. Card em `revisao` com sessão encerrada em `testNow-30h` → `"aguardando_revisao"`. Card em `em_andamento` com sessão viva → `""`.
  - `TestListCardsSignalWithoutSessions`: card criado pelo store sem sessão, movido para `pausado` com `updated_at` em `testNow-48h` (gravado direto no banco do teste) → `"parado"`.
- [ ] **Step 6: Run** `go test ./internal/api/ -run TestListCardsSignal`. Expected: FAIL (campo ausente).
- [ ] **Step 7: Implement.** Em `cardSummary`, calcule `last = max(sum.LastActivityAt, c.UpdatedAt)`, depois `sum.Signal = domain.CardSignal(c.Column, last, s.now())` e `SignalSince = &last` quando houver sinal. Use o relógio injetado (`Deps.Now`) que o servidor já usa.
- [ ] **Step 8: Run** o mesmo comando. Expected: PASS.
- [ ] **Step 9: Write the failing tests** de totais:
  - **Store**, `TestTotalsByProject`: projeto 1 com uma sessão com card e outra sem card, projeto 2 com uma sessão, e uma sessão sem projeto. `GroupBy: "project"` devolve as chaves `"1"`, `"2"` e `""`, com os custos somados. O projeto do uso é o **projeto do card** do uso quando existe, senão o da sessão (`COALESCE(c.project_id, s.project_id)`).
  - **Store**, `TestTotalsProjectFilter`: `ProjectID: &one` com `GroupBy: "agent"` soma só o uso do projeto 1.
  - **API**, `TestUsageTotalsProject`: `group=project` → 200. `project=abc` → 400.
- [ ] **Step 10: Run** `go test ./internal/store/ ./internal/api/ -run 'Totals'`. Expected: FAIL.
- [ ] **Step 11: Implement.** No SELECT de `Totals`, adicione `COALESCE(c.project_id, s.project_id)`, lido como `sql.NullInt64`. Adicione o filtro `ProjectID` e o case `"project"`. No handler, acrescente `"project"` à lista de grupos, com a mensagem de erro listando os grupos, e o parâmetro `project`.
- [ ] **Step 12: Run** `go vet ./... && go test ./...`. Expected: PASS.
- [ ] **Step 13: Commit**

```bash
git add internal/domain/signal.go internal/domain/signal_test.go internal/api internal/store
git commit -m "feat: sinal parado/aguardando revisão no card e custo por projeto"
```

---

### Task 2: Interface — seletor de projeto, etiqueta e parâmetro na URL

**Files:**
- Create: `web/src/app/project.ts`, `web/src/app/project.test.ts`, `web/src/ui/ProjectTag.tsx`
- Modify: `web/src/app/Layout.tsx`, `web/src/app/Layout.test.tsx`, `web/src/api/types.ts` (`CardSummary.signal`, `signal_since`), `web/src/api/queries.ts` (`useTotals` com `project`)

**Interfaces:**
- Consumes: os campos `signal` e `signal_since` da Task 1. `GET /api/usage/totals?group=project&project=`.
- Produces:
  - `useProjectFilter(): { projectId: number | null; project: Project | null; setProjectId(id: number | null): void }`. Lê e escreve `?projeto=` com `useSearchParams` e preserva os outros parâmetros. Id que não está em `useProjects()` vira `null`.
  - `withProject(to: string, projectId: number | null): string` acrescenta `projeto=<id>` ao link (respeitando `?` já existente).
  - `projectColor(projectId: number, projects: Project[]): string` usa a paleta das Global Constraints, por posição do projeto na lista ordenada por `id`, em ciclo.
  - `projectForPath(projects: Project[], dir: string): Project | null`, com a mesma regra de `store.ProjectForPath`: o prefixo mais longo, respeitando o limite de diretório.
  - `<ProjectTag project={p} projects={all} />`: etiqueta colorida com o nome. Com `project === null`, mostra "Sem projeto" em cinza (`var(--text-2)`).
  - `useTotals(group, from?, to?, project?: number | null)`, com `project` na `queryKey`.
  - `CardSummary.signal: '' | 'parado' | 'aguardando_revisao'` e `signal_since: string | null`.

- [ ] **Step 1: Write the failing tests** em `web/src/app/project.test.ts`:
  - `withProject('/board', 3) === '/board?projeto=3'`
  - `withProject('/worktrees/detalhe?path=x', 3) === '/worktrees/detalhe?path=x&projeto=3'`
  - `withProject('/board', null) === '/board'`
  - `projectColor`: com projetos de id `[5, 2, 9]`, o id 2 recebe `#B07CE8`, o 5 recebe `#C9A227` e o 9 recebe `#E26D9C`. O sétimo projeto volta para `#B07CE8`.
  - `projectForPath`: com `/a` e `/a/b` cadastrados, `/a/b/c` → `/a/b`, `/ab` → `null` e `/a` → `/a`.
- [ ] **Step 2: Run** `pnpm -C web test --run src/app/project.test.ts`. Expected: FAIL.
- [ ] **Step 3: Implement** `web/src/app/project.ts` e `web/src/ui/ProjectTag.tsx`.
- [ ] **Step 4: Run** o mesmo comando. Expected: PASS.
- [ ] **Step 5: Write the failing tests** em `Layout.test.tsx`, com `/api/projects` mockado com 2 projetos:
  - O seletor (`<select aria-label="Projeto">`) tem as opções "Todos os projetos", "cockpit" e "site".
  - Escolher "site" (id 2) leva os links do menu a terminarem com `?projeto=2`.
  - Rota inicial `/?projeto=99` mostra o seletor em "Todos os projetos", e os links ficam sem `projeto`.
  - Voltar para "Todos os projetos" remove o parâmetro dos links.
- [ ] **Step 6: Run** `pnpm -C web test --run src/app/Layout.test.tsx`. Expected: FAIL.
- [ ] **Step 7: Implement.** O seletor fica no `Layout`, entre a marca e o `.nav`, com `min-height` de 44 px como os outros controles. Os `NavLink` usam `withProject(n.to, projectId)`. O `isActive` deve continuar certo com a query (o `NavLink` compara só o pathname).
- [ ] **Step 8: Run** `pnpm -C web test --run`. Expected: PASS.
- [ ] **Step 9: Commit**

```bash
git add web/src/app web/src/ui/ProjectTag.tsx web/src/api
git commit -m "feat: seletor de projeto na barra lateral e etiqueta de projeto"
```

---

### Task 3: Board, Cards e Nova tarefa seguem o projeto

**Files:**
- Modify: `web/src/pages/Board.tsx`, `web/src/pages/CardsList.tsx`, `web/src/pages/NewTask.tsx`, `web/src/pages/board.ts` (texto do sinal)
- Test: `web/src/pages/board.test.ts`, um teste novo `web/src/pages/Board.test.tsx` (mesmo padrão de `mock`/`wrap` de `screens16.test.tsx`, com `MemoryRouter initialEntries`)

**Interfaces:**
- Consumes: `useProjectFilter`, `ProjectTag`, `projectColor` (Task 2). `CardSummary.signal` e `signal_since` (Task 1).
- Produces: `signalText(signal: CardSummary['signal'], since: string | null, now?: Date): string`, em `board.ts`. Exemplos: `'parado há 2 dias'`, `'aguardando revisão há 3 dias'` e `'parado há 30 h'` (abaixo de 48 h mostra horas; a partir de 48 h, dias inteiros arredondados para baixo). Com sinal vazio, devolve `''`.

- [ ] **Step 1: Write the failing tests:**
  - `signalText`, em `board.test.ts`: `('parado', now-30h) → 'parado há 30 h'`, `('parado', now-50h) → 'parado há 2 dias'`, `('aguardando_revisao', now-73h) → 'aguardando revisão há 3 dias'`, `('', …) → ''`.
  - `Board.test.tsx`, com 3 cards (2 do projeto 1 e 1 do projeto 2, um deles com `signal: 'parado'` e outro com `signal: 'aguardando_revisao'`):
    - Em `/board`, aparecem os 3 cards, cada um com a etiqueta do seu projeto.
    - O card parado mostra "parado há …". O card em revisão mostra "aguardando revisão há …" e **não** contém "parado".
    - Em `/board?projeto=2`, aparece só o card do projeto 2, sem etiqueta.
- [ ] **Step 2: Run** `pnpm -C web test --run src/pages/board.test.ts src/pages/Board.test.tsx`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **Board:** filtre por `project_id` antes do filtro de agente e mostre `ProjectTag` só em "Todos".
  - **Sinal:** mostre o texto ao lado da chave. "parado" usa a cor de aviso (`var(--warn)`) com ícone. "aguardando revisão" usa `var(--text-2)` com um ícone de relógio, sempre ícone e texto. Quando os dois existem, o sinal novo substitui o aviso atual "sem sessão há N min", que continua valendo abaixo de 24 h.
  - **CardsList:** adicione a coluna "Projeto" (com `ProjectTag`) e o mesmo filtro.
  - **NewTask:** com projeto escolhido, ele vem pré-selecionado.
- [ ] **Step 4: Run** `pnpm -C web test --run`. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add web/src/pages
git commit -m "feat: board e cards filtram por projeto e mostram parado/aguardando revisão"
```

---

### Task 4: Analytics, Worktrees e Prompts filtram; Sala de controle e Terminais mostram a etiqueta

**Files:**
- Modify: `web/src/pages/Analytics.tsx`, `web/src/pages/Worktrees.tsx`, `web/src/pages/Prompts.tsx`, `web/src/pages/ControlRoom.tsx`, `web/src/pages/Terminals.tsx`
- Test: `web/src/pages/screens16.test.tsx` (Analytics e Prompts), `web/src/pages/Worktrees.test.tsx`, `web/src/pages/ControlRoom.test.tsx`, `web/src/pages/Terminals.test.tsx`

**Interfaces:**
- Consumes: `useProjectFilter`, `ProjectTag`, `projectForPath`, `useTotals(..., project)` (Task 2).

- [ ] **Step 1: Write the failing tests:**
  - **Analytics** em `/analytics?projeto=1`: as chamadas de totais saem com `project=1` (confira as URLs pedidas ao `fetch`), e a lista de sessões mostra só sessões com `project_id === 1`.
  - **Worktrees** em `?projeto=2`: só a seção do projeto 2 aparece.
  - **Prompts** em `?projeto=1`: aparecem o prompt global (`project_id: null`) e o do projeto 1, mas não o do projeto 2.
  - **Sala de controle**, sem parâmetro e também com `?projeto=1`: as duas sessões ativas aparecem (uma do projeto 2 e uma com `project_id: null`), com as etiquetas "site" e "Sem projeto".
  - **Terminais**, com `?projeto=1`: o terminal de outro projeto continua visível, com a etiqueta do projeto achado por `projectForPath(dir)`.
- [ ] **Step 2: Run** `pnpm -C web test --run`. Expected: FAIL nos testes novos.
- [ ] **Step 3: Implement.** O filtro é feito no cliente, sobre os dados que cada tela já busca, exceto os totais do Analytics, que vão com `project`. Sala de controle e Terminais **ignoram** o seletor.
- [ ] **Step 4: Run** `pnpm -C web test --run`. Expected: PASS.
- [ ] **Step 5: Commit**

```bash
git add web/src/pages
git commit -m "feat: analytics, worktrees e prompts seguem o projeto; sala e terminais mostram a etiqueta"
```

---

### Task 5: Página Projetos e cronograma do projeto

**Files:**
- Create: `web/src/pages/Projects.tsx`, `web/src/pages/ProjectPage.tsx`, `web/src/pages/projects.ts` (cálculos puros), `web/src/pages/projects.test.ts`, `web/src/pages/Projects.test.tsx`
- Modify: `web/src/app/routes.tsx` (`projetos` e `projetos/:id`), `web/src/app/Layout.tsx` (item "Projetos" no menu, logo depois de "Cards"), `docs/spec.md` (§12, tela 7 "Projetos", com uma linha sobre o seletor)

**Interfaces:**
- Consumes: `useProjects`, `useCards`, `useTotals('project', from, to)`, `ProjectTag`, `signalText`, `withProject`.
- Produces, em `projects.ts`:
  - `interface ProjectRow { project: Project | null; costUsd: number; unpriced: boolean; current: CardSummary | null; stale: { count: number; oldest: string | null }; awaiting: { count: number; oldest: string | null }; lastActivity: string | null }`
  - `buildRows(projects: Project[], cards: CardSummary[], totals: Total[]): ProjectRow[]` devolve uma linha por projeto, na ordem de `id`. A linha "Sem projeto" (`project: null`) entra no fim **só** se o total com chave `""` tiver `cost_usd > 0`.
  - `current`: o card com a maior atividade (o maior entre `last_activity_at` e `updated_at`) entre os que estão em `em_andamento`, `pausado` ou `revisao`.
  - `stale` e `awaiting` contam por `signal`. `oldest` é o menor `signal_since`.
  - `lastActivity`: a maior atividade entre os cards do projeto.
  - `timeline(cards: CardSummary[]): { group: 'concluidas' | 'andamento' | 'paradas' | 'backlog'; card: CardSummary }[]`, nesta ordem de grupos:
    - concluídas (`concluido`), da conclusão mais antiga para a mais recente (`updated_at`);
    - andamento (`em_andamento`, `pausado` sem sinal e `revisao`, com ou sem sinal), por atividade, a mais recente primeiro;
    - paradas (`signal === 'parado'`), a parada há mais tempo primeiro;
    - backlog, por `created_at`.

- [ ] **Step 1: Write the failing tests** em `projects.test.ts`:
  - `buildRows`: 2 projetos. O projeto 1 tem um card `em_andamento` com atividade em T e outro `pausado` com `signal: 'parado'`. O projeto 2 tem um card `revisao` com `signal: 'aguardando_revisao'`. Os totais são `"1": 41`, `"2": 8` e `"": 0`. Resultado: 2 linhas, sem "Sem projeto". `rows[0].current.key` é o card em andamento, `rows[0].stale.count === 1` e `rows[1].awaiting.count === 1`. Com `"": 3`, aparece a terceira linha, com `project === null`.
  - `buildRows` com projeto `capture: false`: a linha existe (a tela mostra "captura desligada").
  - `timeline`: cards em todas as colunas saem na ordem de grupos acima. Um `pausado` sem sinal fica em `andamento`. Um `revisao` com sinal também fica em `andamento`.
- [ ] **Step 2: Run** `pnpm -C web test --run src/pages/projects.test.ts`. Expected: FAIL.
- [ ] **Step 3: Implement** `projects.ts`.
- [ ] **Step 4: Run** o mesmo comando. Expected: PASS.
- [ ] **Step 5: Write the failing tests** em `Projects.test.tsx`:
  - **`/projetos`:** a tabela tem as colunas "Projeto", "Custo", "Entrega atual", "Parados", "Aguardando você" e "Última atividade". Linha sem card ativo → "nada em andamento". Projeto sem captura → "captura desligada" na célula de custo. O período padrão é "7 dias", e o pedido de totais leva `group=project` com o `from` de 7 dias atrás. Clicar no nome do projeto leva a `/projetos/<id>`.
  - **`/projetos/1`:** mostra o cronograma com os grupos "Concluídas", "Em andamento", "Paradas" e "Backlog". Cada entrega tem chave, título e o texto do sinal, e clicar abre `/cards/<key>`. Um projeto inexistente (`/projetos/99`) mostra "Projeto não encontrado".
- [ ] **Step 6: Run** `pnpm -C web test --run src/pages/Projects.test.tsx`. Expected: FAIL.
- [ ] **Step 7: Implement.**
  - **Projetos:** a barra de custo é proporcional ao maior custo do período, usando o componente `Money` para o valor.
  - **Página do projeto:** traz um link "Ver no board", que leva a `withProject('/board', id)`.
  - **Rotas e menu:** registre as rotas e o item "Projetos" no menu, com um ícone no estilo dos outros (`svgProps`).
  - **Seletor:** a página Projetos ignora o seletor, porque compara todos. A página do projeto usa o `:id` da rota.
- [ ] **Step 8: Run** `go vet ./... && go test ./... && pnpm -C web test --run && pnpm -C web build`. Expected: tudo PASS e build sem erro de tipos.
- [ ] **Step 9: Commit**

```bash
git add web/src docs/spec.md
git commit -m "feat: página Projetos com comparação e cronograma de entregas"
```
