# Seletor de projeto em dois passos (NAC-30): design curto e plano

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development ou superpowers:executing-plans.

**Pedido do usuário (2026-11-18, aprovado):** primeiro escolher o projeto; se ele tiver sub-projetos, aparece um segundo select com eles.

## Design (aprovado)

- **Primeiro select ("Projeto"):** "Todos os projetos" mais os projetos principais e soltos (sem `parent_id`), ordenados por nome. Os sub-projetos saem dele.
- **Segundo select ("App"):** só aparece quando o projeto escolhido tem filhos. Opções, nesta ordem:
  - "Todos" (padrão): a família inteira, como hoje;
  - "geral (raiz)": só o próprio principal;
  - cada filho, por nome.
- **URL:** `?projeto=<id do principal>` continua. O app vai em `&app=<id do filho>` ou `&app=geral`.
- **Trocar o projeto:** remove o `app`.
- **Link antigo** com `?projeto=<id de um filho>`: normalizado para `?projeto=<pai>&app=<filho>`.
- **Filtro efetivo (`ids`):**
  - Todos: `familyIds(pai)`;
  - geral: `[pai]`;
  - app: `[filho]`;
  - projeto sem filhos: `[id]`.
- **`projectId`** (quem consome hoje, como "Nova tarefa" e prompts do projeto): o filho quando há app escolhido, senão o principal.
- **Telas:** Board, Cards, Analytics, Worktrees e Prompts continuam usando `ids` e `projectId` de `useProjectFilter()`, sem mudar. `withProject`/`useWithProject` carregam também o `app` nos links internos.
- **Fora do escopo:** mudar a página Projetos ou a do projeto.

## Plano

### Task 1: `useProjectFilter` com app, seletor em dois selects

**Files:**
- Modify: `web/src/app/project.ts`, `web/src/app/Layout.tsx`
- Test: `web/src/app/project.test.ts`, `web/src/app/Layout.test.tsx`

**Interfaces:**
- Produces: `useProjectFilter()` devolve também `app: number | 'geral' | null`, `setApp(app: number | 'geral' | null): void` e `root: Project | null` (o principal escolhido). `ids` e `projectId` seguem as regras acima. `withProject(to, projectId, app?)` acrescenta `&app=` quando houver.

- [ ] **Step 1: testes que falham:**
  - `project.test.ts`: `effectiveIds` (função pura exportada para o hook) para "Todos", "geral", um app e um projeto sem filhos; `withProject` com `app`.
  - `Layout.test.tsx`:
    - `second select appears only for a project with children`;
    - `choosing an app sets &app= and changing project clears it`;
    - `legacy ?projeto=<child> is shown as parent + app`.
- [ ] **Step 2:** `cd web && pnpm test --run project Layout`. Esperado: FAIL.
- [ ] **Step 3:** implementar. O segundo select tem `aria-label="App"`; o primeiro mantém o `aria-label` atual.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`. Esperado: PASS. Ajustar os testes antigos que esperavam os filhos indentados no primeiro select.
- [ ] **Step 5: Commit** `feat(web): seletor de projeto em dois passos (projeto e app)`

### Task 2: instalar e conferir

- [ ] `make update`; na tela, escolher bravonix → aparece "App" com geral e os 8 repositórios; escolher `turing-front-2` filtra o Board; trocar para mscloud → o segundo select some.
