# Sessão compartilhada Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dois processos do Claude Code na mesma conversa passam a ter sessões separadas no Cockpit (a segunda é uma sessão filha), e um não troca o card do outro.

**Architecture:** O roteamento de sessão no hook do adaptador Claude passa a considerar (conversa, PID): PID novo com o PID registrado ainda vivo cria uma sessão filha ligada à principal. `nobre` já resolve por PID. A ingestão de transcript da filha usa o id da principal.

**Tech Stack:** Go (SQLite), React + TypeScript.

**Spec:** docs/superpowers/specs/2026-11-20-sessao-compartilhada-design.md

## Global Constraints

- Id da filha: `claude:<session_id>@<pid>`; `sessions.parent_session_id` = id da principal (vazio na principal).
- Migração `internal/store/migrations/016_session_parent.sql`: `ALTER TABLE sessions ADD COLUMN parent_session_id TEXT NOT NULL DEFAULT '';`
- Filha herda da principal: projeto, captura (`full_capture`), cwd, branch, git_head, worktree, worktree_linked, transcript_path; sem card.
- `pid <= 0` ou PID registrado morto ou principal não viva → comportamento atual (reabrir a principal).
- Ingestão de transcript de uma filha roda com o id da principal.
- Linha extra no SessionStart da filha, exata: "Esta conversa também está aberta em outra janela, com sessão própria no Cockpit; os comandos nobre desta janela valem só para ela."
- Marca na tela: "outra janela".

## Review Focus

- `--resume` normal (PID antigo morto) continua reaproveitando a principal, sem criar filha (Task 1 `TestSession_ResumeAfterDeathReusesMain`).
- Subagentes (mesmo PID, `agent_id` no payload) nunca criam filha (Task 1).
- Hooks alternando entre os dois PIDs não criam filhas novas a cada hook: a segunda vez do mesmo PID acha a filha existente (Task 1).
- Uso não duplicado: transcript compartilhado lido uma vez (Task 1 `TestIngest_ChildUsesMainSession`).
- Filha encerrada (SessionEnd ou PID morto) e o processo volta a mandar hook: reabre a filha certa, não a principal (Task 1).

---

### Task 1: Store, roteamento, ingestão e contexto

**Files:**
- Create: `internal/store/migrations/016_session_parent.sql`, `internal/adapter/claude/shared_session_test.go`
- Modify: `internal/domain/types.go` (`Session.ParentSessionID string json:"parent_session_id"`), `internal/store/sessions.go` (colunas, `SessionByProviderPID(providerID string, pid int) (*domain.Session, error)` que acha a sessão — viva ou não, mais recente — com aquele provider_session_id e pid), `internal/store/store_test.go` (user_version 16), `internal/adapter/claude/claude.go` (dependência `alive func(pid int) bool`, padrão `proc.Alive("/proc", pid)`, com setter para testes), `internal/adapter/claude/hooks.go` (`session()`), `internal/adapter/claude/ingest.go` ou o ponto de chamada (`ingestAsync` com o id da principal quando `ParentSessionID != ""`), `internal/adapter/claude/context.go` (linha extra da filha)

**Interfaces:**
- Produces: `domain.Session.ParentSessionID`; `Store.SessionByProviderPID`; o roteamento descrito no spec.

- [ ] **Step 1: Failing tests** (`shared_session_test.go`, usando os helpers de `hooks_test.go`):
  - `TestSession_SecondLiveProcessGetsChild`: SessionStart com PID 100 cria `claude:S`; hook com PID 200 enquanto 100 está vivo → sessão `claude:S@200` com parent `claude:S`, mesmo projeto e captura, sem card; `claude:S` mantém PID 100 e o card.
  - `TestSession_AlternatingHooksRouteToOwnSession`: depois disso, hooks de 100 e 200 alternados → cada um toca a própria sessão; nenhuma terceira sessão.
  - `TestSession_ResumeAfterDeathReusesMain`: PID 100 morto (alive=false) e hook de 300 → reabre `claude:S` com PID 300; nenhuma filha.
  - `TestSession_SubagentNeverForks`: payload com `agent_id` e mesmo PID → sem filha.
  - `TestSession_ChildReopensItself`: filha encerrada (SessionEnd do 200) e novo hook de 200 → reabre `claude:S@200`.
  - `TestNobreCommandsPerProcess`: `/api/me/start` com o cabeçalho de PID 200 cria card ligado à filha; `/api/me/status` com PID 100 continua no card da principal (pode ficar em `internal/api` se for mais natural, com o store real).
  - `TestIngest_ChildUsesMainSession`: hook da filha dispara leitura do transcript com o id da principal (linhas de uso só com `session_id = claude:S`).
  - `TestSessionStart_ChildLine`: SessionStart da filha contém a linha exata das Global Constraints; a principal não.
- [ ] **Step 2:** FAIL → implementar → `go test ./... && go vet ./...` PASS.
- [ ] **Step 3: Commit** `feat: sessao filha para segundo processo na mesma conversa`

---

### Task 2: Marca "outra janela" na tela

**Files:**
- Modify: `web/src/api/types.ts` (`parent_session_id`), `web/src/pages/card/Sessions.tsx` e a tabela "Sessões recentes" de `web/src/pages/Analytics.tsx` (marca "outra janela" quando `parent_session_id` não vazio), testes correspondentes.

- [ ] **Step 1: Failing tests:** sessão com `parent_session_id` mostra "outra janela" nas duas listas; sem, não mostra.
- [ ] **Step 2:** implementar; `cd web && pnpm test --run && pnpm build` PASS.
- [ ] **Step 3: Commit** `feat(web): marca outra janela nas sessoes filhas`
