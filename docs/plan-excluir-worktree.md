# Excluir worktree que não está em uso: plano de implementação (NAC-10)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Excluir pela plataforma um worktree ligado que não está em uso, com travas de segurança, e opcionalmente o branch dele quando já integrado.

**Architecture:**
- **Backend:** rota `DELETE /api/worktrees` que reaproveita `projectWorktrees`/`worktreeDetail` para achar o worktree e o estado dele.
- **Travas:** só worktree ligado; sem sessão viva; não travado; confirmação para alterações não commitadas ou processos rodando.
- **Git:** duas operações de escrita novas, explícitas e só por pedido do usuário: `git worktree remove [--force]` e `git branch -d`.
- **UI:** um botão "Excluir worktree" na página do worktree, com confirmação inline.

**Tech Stack:** Go 1.26 (`net/http`, `os/exec` do git), React + TanStack Query + Vitest.

**Decisão do usuário (2026-11-15):** "Seguro, branch opcional":
- só sem sessão viva nele e nunca o checkout principal;
- com alterações não commitadas, mostrar a lista e exigir confirmação;
- remove a pasta do worktree;
- apagar o branch é uma caixa extra, liberada só se o branch já estiver integrado na base.

## Global Constraints

- **Escopo:** valem as constraints de `docs/plan.md`; textos da UI e mensagens em pt-BR.
- **Ambiente:** nunca tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado (porta 7420).
  - Testes em `t.TempDir()` e `httptest`.
  - Os testes de git criam repositórios reais, com `commit.gpgsign=false` (padrão de `internal/api/worktrees_test.go`).
- **Git de leitura:** continua por `gitRO`.
- **Git de escrita:** existe só nesta rota e só com estes argumentos:
  - `git -C <repo> worktree remove [--force] -- <path>`;
  - `git -C <repo> worktree prune` (worktree cujo diretório sumiu);
  - `git -C <repo> branch -d -- <branch>`, nunca `-D`.
- **Base para "integrado":** `board.ProjectBase(git, projeto)`. Integrado = a ponta do branch é ancestral da base local ou de `origin/<base>` (mesma regra do detector do NAC-9).
- **Diálogos:** nunca abrir diálogo do navegador (`confirm`/`alert`); a confirmação é inline, como no "Encerrar agente".

## Review Focus

1. **Worktree com sessão viva** (inclusive a desta própria tela): recusado com 409, mesmo com `force`.
2. **Checkout principal ou caminho que o git não lista para o projeto:** recusado (400 ou 404). Nada é apagado fora da lista do `git worktree list`.
3. **Branch não integrado com `delete_branch: true`:** a pasta é removida e o branch fica. A resposta diz que o branch foi mantido e por quê. Nada de `-D`.
4. **Worktree prunable** (diretório já apagado à mão): limpa o registro com `git worktree prune`, sem erro.
5. **Pedido repetido** (worktree já removido): 404 claro, sem quebrar a lista.

---

### Task 1: Backend — `DELETE /api/worktrees`

**Files:**
- Create: `internal/gitinfo/remove.go` (escritas do git), `internal/api/worktree_remove.go`
- Modify: `internal/api/worktrees.go` (registrar a rota)
- Test: `internal/gitinfo/remove_test.go`, `internal/api/worktree_remove_test.go`

**Interfaces:**
- Produces (`internal/gitinfo/remove.go`):

  ```go
  type WorktreeRemover interface {
      RemoveWorktree(repo, path string, force bool) error // git worktree remove [--force] -- path
      PruneWorktrees(repo string) error                   // git worktree prune
      DeleteBranch(repo, branch string) error             // git branch -d -- branch (valida o nome com validBranch)
  }
  var _ WorktreeRemover = Exec{}
  ```

- Produces: `DELETE /api/worktrees` com corpo `{project int64, path string, force bool, delete_branch bool}`.
  - **Projeto inexistente:** 404 `projeto não encontrado`.
  - **`path` fora da lista do git para o projeto:** 404 `worktree não encontrado`.
  - **Worktree principal** (`Main`): 400 `o checkout principal não pode ser excluído`.
  - **Travado** (`Locked`): 409 `worktree travado (<motivo>); destrave com git worktree unlock`. Sem motivo, só `worktree travado; destrave com git worktree unlock`.
  - **Com sessão viva** (alguma das sessões do worktree com `Live`): 409 `há uma sessão viva neste worktree; encerre-a antes de excluir`.
  - **Sem `force`, com alterações** (`TotalFiles > 0` no `Inspect`) **ou com processos** atribuídos ao worktree (mesma atribuição do detalhe): 409 com corpo JSON:

    ```
    {"error": "o worktree tem alterações não commitadas ou processos rodando; confirme para excluir mesmo assim",
     "needs_confirm": true, "files": [...até 50 FileChange], "total_files": n, "processes": [{pid, name}]}
    ```

    (`writeError` não serve; use `writeJSON(w, 409, …)`).
  - **Prunable:** `PruneWorktrees(repo)`. **Nos outros casos:** `RemoveWorktree(repo, path, force)`. Falha do git: 500 com a mensagem do git em pt-BR, prefixada `não consegui excluir o worktree: `.
  - **`delete_branch`,** com o worktree tendo branch (não destacado) e o branch integrado na base: `DeleteBranch`.
    - Se não estiver integrado, ou se `git branch -d` falhar, a resposta diz `branch_kept_reason` com o motivo: `não está integrado em <base>` ou o erro do git.
    - Branch igual à base: também é mantido (`é a base do projeto`).
  - **Sucesso:** 200 `{"removed": true, "branch_deleted": bool, "branch_kept_reason": string}`; publica `"worktrees"` (se existir) e `"sessions"`.
  - O repositório usado nos comandos é o checkout principal do projeto (o worktree `Main` da lista).

- [ ] **Step 1: Escrever os testes que falham** (repositórios reais em `t.TempDir()`)

```go
// gitinfo
func TestRemoveWorktreeAndPrune(t *testing.T)     // add worktree → RemoveWorktree(force=false) some da lista; outro com arquivo novo → sem force erro, com force ok; diretório apagado à mão → PruneWorktrees limpa
func TestDeleteBranchSafe(t *testing.T)           // branch integrado → DeleteBranch ok; branch com commit não integrado → erro (nunca -D); nome "-x" → erro
// api
func TestDeleteWorktreeClean(t *testing.T)        // worktree limpo sem sessão → 200 removed; some de GET /api/worktrees
func TestDeleteWorktreeRefusesMain(t *testing.T)  // path do principal → 400
func TestDeleteWorktreeRefusesLiveSession(t *testing.T) // sessão viva com worktree=path → 409 mesmo com force
func TestDeleteWorktreeNeedsConfirm(t *testing.T) // arquivo não commitado → 409 needs_confirm com files; com force → 200
func TestDeleteWorktreeBranchOnlyIfMerged(t *testing.T) // delete_branch: branch integrado → branch_deleted true; não integrado → pasta removida, branch fica, branch_kept_reason "não está integrado em main"
func TestDeleteWorktreePrunable(t *testing.T)     // diretório apagado → 200 e registro limpo
func TestDeleteWorktreeUnknownPath404(t *testing.T) // caminho qualquer (ou já removido) → 404
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/gitinfo/ ./internal/api/ -run 'RemoveWorktree|DeleteBranch|DeleteWorktree'` → FAIL.

- [ ] **Step 3: Implementar.** Reaproveite `projectWorktrees` para achar o worktree e as sessões, e `Inspect` e a atribuição de processos como em `worktreeDetail`. Siga o padrão CSRF do servidor: o `DELETE` tem corpo JSON, então confira o que `localOnly` exige.

- [ ] **Step 4: Rodar e ver passar** — o mesmo comando, depois `go vet ./... && go test ./...`.

- [ ] **Step 5: Commit** — `feat: excluir worktree sem sessão viva pela API, branch só se integrado`

---

### Task 2: Interface — botão "Excluir worktree"

**Files:**
- Modify: `web/src/pages/WorktreeDetail.tsx` (e um componente novo em `web/src/pages/worktree/` se o arquivo crescer), `web/src/api/` (chamada `DELETE` com corpo, se o helper `del` não aceitar corpo)
- Test: `web/src/pages/WorktreeDetail.test.tsx`

**Comportamento e textos exatos:**
- **Onde o botão aparece:** `Excluir worktree` (estilo de perigo, como "Encerrar agente") só para worktree ligado (não `main`).
- **Quando fica desabilitado:**
  - com sessão viva, o texto ao lado é `Há uma sessão viva neste worktree.`;
  - com worktree travado, o texto é `Worktree travado.`
- **Ao clicar, a confirmação é inline:**
  - pergunta `Excluir a pasta <path>?`;
  - caixa `Também apagar o branch <branch>` quando houver branch;
  - botões `Sim, excluir` e `Cancelar`.
- **Resposta `needs_confirm`:**
  - mostra `O worktree tem alterações não commitadas ou processos rodando:`;
  - lista até 10 arquivos (`<code> <path>`) e `+N` se houver mais, e os processos (`<nome> (pid N)`);
  - troca o botão para `Excluir mesmo assim`, que reenvia com `force: true`.
- **Sucesso:**
  - volta para a lista (`/worktrees`, mantendo `?projeto=` se houver) e invalida a query de worktrees;
  - se `branch_kept_reason` vier preenchido, mostra na lista (ou num aviso) `Branch <branch> mantido: <motivo>.`
- **Erros:** aparecem em `ErrorLine`, `role="alert"`.

- [ ] **Step 1: Escrever os testes que falham** (mock de `fetch` no padrão dos testes da página)

```ts
it('não mostra Excluir no checkout principal')
it('desabilita Excluir com sessão viva e explica')
it('confirma inline e envia DELETE com project, path e delete_branch')
it('needs_confirm lista arquivos e reenvia com force')
it('após excluir volta para a lista')
```

- [ ] **Step 2: Rodar e ver falhar** — `pnpm -C web test --run WorktreeDetail` → FAIL.
- [ ] **Step 3: Implementar.**
- [ ] **Step 4: Rodar e ver passar** — `pnpm -C web test --run && (cd web && npx tsc --noEmit) && make build`.
- [ ] **Step 5: Commit** — `feat: botão para excluir worktree que não está em uso`
