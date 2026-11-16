# Fazer valer as configurações — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cada opção da tela Configuração ter efeito real no daemon e nos hooks (ou dizer honestamente que é "em breve").

**Architecture:** Duas opções novas em `Settings` (sem migração: `settings` é chave → JSON) passam a controlar o SessionStart. Três colunas novas em `cards` (`pause_reason`, `handoff_incomplete`, `blocked_by`) guardam o efeito da pausa por limite, a marca de handoff incompleto e a dependência de outro card. As três são limpas quando o card volta para Em andamento. O fim de sessão passa a ter um único caminho (`board.FinishSession`), usado pelo hook e pelo reconciliador. O sinal do card distingue "parado" (o agente travou) de "aguardando você", "bloqueado por NAC-x" e "pronto para retomar". A UI ganha os campos que faltam e as marcas no board e no card.

**Item 3 do NAC-9 (incluído em 2026-11-15, pedido do usuário):** "Pausado" hoje mistura motivos. O NAC-4 de 8/11 aparecia como **parado**, embora esperasse a revisão do usuário e o merge de outro branch. Esses motivos agora são estruturados (Task 4).

**Tech Stack:** Go 1.26 (SQLite, `net/http`), React + TanStack Query + Vitest.

**Spec:** `docs/spec.md` (§6.0, §6.3, §8). Decisões do usuário registradas no card NAC-4 (2026-11-08).

**Pré-requisito:** começar só depois que `feat/limites-plano` entrar na `main` (decisão do usuário). Criar um worktree novo a partir da `main` atualizada (`superpowers:using-git-worktrees`), branch `feat/configuracoes`.

**Atenção (NAC-9, 2026-11-15):** o NAC-9 já mudou o mesmo bloco de `sessionStart` em `internal/adapter/claude/context.go` (aviso de repositório não cadastrado, `guidance` separado de `registeredPhrase`, cards pausados listados por base/branch) e o `meDone` (recusa sem "Como testar" atual). A Task 1 deste plano deve partir desse código, e as opções `agent_creates_cards`/`inject_paused_cards` devem respeitar também o aviso de não cadastrado.

## Global Constraints

- Valem as constraints de `docs/plan.md`. Textos da UI e mensagens em pt-BR.
- Nunca tocar o `~/.claude` real, o `systemctl` nem o `nobred` instalado (porta 7420). Testes em `t.TempDir()` e `httptest`.
- A migração nova é a próxima livre depois das do limites-plano (`010_card_flags.sql`: a `009_branch_tracking.sql` do NAC-9 já está na `main`). O teste de `user_version` em `internal/store/store_test.go` sobe para 10.
- Datas em SQL são comparadas com `julianday(col)`, nunca como texto.
- Padrões novos: `agent_creates_cards = true` e `inject_paused_cards = true`. O banco atual mantém o comportamento de hoje.
- Texto exato de `domain.PauseReasonLimit`: `"limite de custo"`.
- Sinais novos (valores exatos no JSON `signal`): `aguardando_voce`, `bloqueado`, `pronto_para_retomar`. Textos na UI e no SessionStart: `aguardando você há …`, `bloqueado por <KEY>`, `pronto para retomar (<KEY> concluído)`.
- O NAC-9 já está na `main`: `instructions = registeredPhrase + ".\n" + guidance`, e o aviso de não cadastrado termina em `guidance`. O critério de criação de card fica dentro de `guidance`.

## Review Focus

1. **Limite elevado no meio da sessão:** o card pausado por limite volta para Em andamento na próxima ferramenta permitida (teste na Task 2).
2. **Troca de `mark` para `ask_agent` com cards já marcados:** o alerta da Sala some (depende da opção atual), mas a marca continua guardada (teste na Task 3).
3. **Card pausado por limite e retomado por outra sessão (Continuar com forçar):** `pause_reason` e `handoff_incomplete` são limpos ao voltar para Em andamento (teste na Task 2).
4. **Sessão encerrada pelo reconciliador (processo morreu):** também marca handoff incompleto, não só o SessionEnd do hook (teste na Task 3).
5. **Banco existente sem as chaves novas em `settings`:** `GetSettings` devolve `true` para as duas (teste na Task 1).
6. **O card bloqueador é apagado ou a chave não existe mais:** o card bloqueado não quebra a lista nem o board. O sinal cai para a regra normal, e o `nobre block` recusa uma chave inexistente (teste na Task 4).

---

### Task 1: Hooks respeitam as opções de criação de card e a regra "Onde parei"

**Files:**
- Modify: `internal/domain/readiness.go` (Settings + padrões), `internal/adapter/claude/context.go` (instruções e cards pausados), `internal/adapter/claude/hooks.go` (`stop`)
- Test: `internal/adapter/claude/hooks_test.go`, `internal/store/store_test.go` (ou o teste existente de settings)

**Interfaces:**
- Produces: `Settings.AgentCreatesCards bool` (json `agent_creates_cards`), `Settings.InjectPausedCards bool` (json `inject_paused_cards`), ambos `true` em `DefaultSettings()`. `types.ts` ganha os dois campos na Task 4.

- [ ] **Step 1: Escrever os testes que falham**

```go
func TestSettingsDefaultsForNewKeys(t *testing.T)            // banco sem as chaves → ambos true; PutSettings(false) → GetSettings false
func TestSessionStartOmitsPausedCardsWhenDisabled(t *testing.T) // como TestSessionStartInjectsPausedCards, com InjectPausedCards=false: contexto NÃO contém "Cards pausados neste repositório"
func TestSessionStartWithoutAgentCreatesCards(t *testing.T)   // AgentCreatesCards=false: contexto começa com registeredPhrase, contém "Só crie card quando o usuário pedir" e NÃO contém "quando começar algo que precisa de histórico"
func TestStopIgnoresWhereStoppedWhenRuleDisabled(t *testing.T) // Rules["where_stopped"]=false + edit depois do "Onde parei" → stop.Stdout == ""
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/adapter/claude/ ./internal/store/ -run 'SettingsDefaultsForNewKeys|OmitsPausedCards|WithoutAgentCreatesCards|IgnoresWhereStopped'` → FAIL (campos indefinidos).

- [ ] **Step 3: Implementar**
  - `context.go`: hoje `instructions = registeredPhrase + ".\n" + guidance` (NAC-9). Separe `guidance` em duas partes, o critério de criação (primeira linha) e os comandos, e use as mesmas partes no aviso de não cadastrado. Com `AgentCreatesCards=false` o critério vira exatamente: `Só crie card quando o usuário pedir ("crie um card para isso"); se ele citar uma chave NAC-, use nobre resume <KEY>.` Ler `GetSettings` uma vez no `sessionStart`. Só chamar `pausedCards` com `InjectPausedCards`.
  - `hooks.go` `stop`: com card, se `!set.Rules["where_stopped"]`, não bloquear (é o único critério por turno). Atualizar o comentário da função.

- [ ] **Step 4: Rodar e ver passar** — o mesmo comando → PASS. Depois `go vet ./... && go test ./...`.

- [ ] **Step 5: Commit** — `feat: hooks respeitam criação de card, cards pausados e a regra do onde parei`

---

### Task 2: Pausa por limite move o card para Pausado

**Files:**
- Create: `internal/store/migrations/010_card_flags.sql`
- Modify: `internal/domain/types.go` (Card), `internal/domain/column.go` (`PauseReasonLimit`, `EvLimitPause`), `internal/store/cards.go` (`cardCols`/`scanCard`, funções novas), `internal/store/sessions.go` (limpar marcas ao ir para Em andamento), `internal/adapter/claude/hooks.go` (`preToolUse`)
- Test: `internal/store/cards_test.go`, `internal/adapter/claude/hooks_test.go`, `internal/store/store_test.go` (user_version)

**Interfaces:**
- Migração: `ALTER TABLE cards ADD COLUMN pause_reason TEXT NOT NULL DEFAULT '';`, `ALTER TABLE cards ADD COLUMN handoff_incomplete INTEGER NOT NULL DEFAULT 0;` e `ALTER TABLE cards ADD COLUMN blocked_by TEXT NOT NULL DEFAULT '';` (`blocked_by` é usado na Task 4. Crie a coluna aqui para haver uma migração só.)
- Produces: `Card.PauseReason string` (json `pause_reason`), `Card.HandoffIncomplete bool` (json `handoff_incomplete`); `domain.PauseReasonLimit = "limite de custo"`; `domain.EvLimitPause = "limit_pause"`.
- Produces: `(s *Store) PauseForLimit(cardID int64, at time.Time) (bool, error)`: só age se `col = em_andamento`; grava `col = pausado`, `pause_reason = PauseReasonLimit` e `updated_at`; devolve se mudou.
- Produces: `(s *Store) ResumeFromLimit(cardID int64, at time.Time) (bool, error)`: só age se `col = pausado` e `pause_reason = PauseReasonLimit`; volta para `em_andamento` e limpa `pause_reason`.
- Produces: `(s *Store) SetHandoffIncomplete(cardID int64, v bool) error` (usado na Task 3).
- Produces: `Card.BlockedBy string` (json `blocked_by`), lido e gravado por `cardCols`/`scanCard`/`UpdateCard`.
- Regra: todo caminho que põe o card em `em_andamento` (`moveCardTx`, o `UPDATE` do `ReopenSession`, a movimentação manual da API) zera `pause_reason`, `handoff_incomplete` e `blocked_by`. Para achar todos, rodar `grep -rn "em_andamento\|EmAndamento" internal/store`.

- [ ] **Step 1: Escrever os testes que falham**

```go
// store
func TestPauseForLimitOnlyFromInProgress(t *testing.T)   // em_andamento → true, col pausado, PauseReason "limite de custo"; segunda chamada → false
func TestResumeFromLimitOnlyWhenPausedByLimit(t *testing.T) // pausado sem motivo → false; pausado por limite → true, em_andamento, PauseReason ""
func TestAttachClearsCardFlags(t *testing.T)             // PauseForLimit + SetHandoffIncomplete(true); outra sessão AttachSession(force) → PauseReason "" e HandoffIncomplete false
// hooks
func TestPreToolUsePauseMovesCardToPaused(t *testing.T)  // cardWithSession + overLimit("pause") → deny; GetCard: col Pausado, PauseReason "limite de custo"; 1 evento EvLimitPause; published("cards")
func TestPreToolUseWarnKeepsCardInProgress(t *testing.T) // overLimit("warn") → col continua EmAndamento
func TestPreToolUseResumesCardWhenLimitRaised(t *testing.T) // pausa por limite, depois PutLimit com DailyUSD maior → próxima pre_tool_use_bash: sem deny, col EmAndamento, PauseReason ""
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/store/ ./internal/adapter/claude/ -run 'PauseForLimit|ResumeFromLimit|AttachClearsCardFlags|PreToolUse'` → FAIL.

- [ ] **Step 3: Implementar** a migração, os campos, as funções do store e, no `preToolUse` (só quando há `sess.CardID`): ao negar, `PauseForLimit`. Se mudou, gravar o evento `EvLimitPause` com `{"detail": detail}` e publicar `cards`. Fora do limite, `ResumeFromLimit`; se mudou, publicar `cards`. A mensagem de negação continua igual (o teste existente fixa o texto).

- [ ] **Step 4: Rodar e ver passar** — o mesmo comando → PASS. Depois `go vet ./... && go test ./...`.

- [ ] **Step 5: Commit** — `feat: limite com ação pausar move o card para Pausado`

---

### Task 3: "Marcar handoff incompleto" grava a marca no card

**Files:**
- Create: `internal/board/finish.go`
- Modify: `internal/board/reconciler.go` (usar `FinishSession`), `internal/adapter/claude/hooks.go` (`sessionEnd` usa `FinishSession`), `internal/api/overview.go` (alerta `incomplete`)
- Test: `internal/board/finish_test.go`, `internal/board/reconciler_test.go`, `internal/api/overview_test.go`

**Interfaces:**
- Consumes: `Store.SetHandoffIncomplete` e `Card.HandoffIncomplete` (Task 2).
- Produces: `func FinishSession(st *store.Store, sessionID, reason string, now time.Time) error` em `internal/board/finish.go`. Faz `st.EndSession`. Depois, se a sessão tinha card, o card ficou em `pausado`, não há outra sessão viva nele (`HasLiveSession`) e `IncompleteAction == "mark"`, avalia `domain.Readiness(card, content, false, lastEdit(EvFileEdit), settings, now)`; se não estiver pronto, chama `SetHandoffIncomplete(cardID, true)`.
- Alerta da Sala: o `incomplete` passa a sair só quando `set.IncompleteAction == "mark" && c.HandoffIncomplete && !rd.Ready`. Texto inalterado: `"%s foi pausado com handoff incompleto."`.

- [ ] **Step 1: Escrever os testes que falham**

```go
func TestFinishSessionMarksIncompleteInMarkMode(t *testing.T)   // mark + card sem objetivo → HandoffIncomplete true, col Pausado
func TestFinishSessionDoesNotMarkInAskAgentMode(t *testing.T)   // ask_agent → false
func TestFinishSessionDoesNotMarkReadyCard(t *testing.T)        // mark + card cumprindo todas as regras → false
func TestFinishSessionKeepsFlagOffWhenAnotherSessionLive(t *testing.T) // card com outra sessão viva → não marca
func TestReconcilerMarksIncompleteOnDeadProcess(t *testing.T)   // mark + PID morto além da carência → card marcado
func TestOverviewIncompleteAlertDependsOnSetting(t *testing.T)  // card pausado incompleto sem marca → sem alerta; marcado + mark → alerta; marcado + ask_agent → sem alerta (marca continua no banco)
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/board/ ./internal/api/ -run 'FinishSession|MarksIncomplete|IncompleteAlert'` → FAIL.

- [ ] **Step 3: Implementar** `FinishSession` e trocar as chamadas de `st.EndSession` no reconciliador (as duas) e no `sessionEnd` do adaptador. O seed de demo continua com `st.EndSession`. Ajustar os testes antigos do overview que esperavam o alerta para qualquer card pausado incompleto.

- [ ] **Step 4: Rodar e ver passar** — o mesmo comando → PASS. Depois `go vet ./... && go test ./...`.

- [ ] **Step 5: Commit** — `feat: marca de handoff incompleto gravada no card`

---

### Task 4: Motivo da pausa: aguardando você, bloqueado por outro card, pronto para retomar

**Files:**
- Modify:
  - `internal/domain/signal.go`: `SignalInput`, `CardStatusSignal` e os sinais novos;
  - `internal/api/detail.go`: `CardSummary` usa `CardStatusSignal` e devolve `blocked_by`;
  - `internal/api/me.go`: rota `POST /api/me/block`;
  - `internal/api/cards.go`: o `PATCH` aceita `blocked_by`;
  - `internal/adapter/claude/context.go`: motivo na lista de cards pausados, e `nobre block` em `guidance`;
  - `internal/adapter/claude/skill.md`: `nobre block` em Comandos e em "Como manter o card útil";
  - `internal/client/client.go`, `cmd/nobre/agent.go` e `cmd/nobre/main.go`: `nobre block <KEY>` e `nobre block --clear`.
- Test: `internal/domain/signal_test.go`, `internal/api/me_test.go`, `internal/api/cards_test.go` (ou `detail_test.go`), `internal/adapter/claude/hooks_test.go`, `cmd/nobre/agent_test.go`

**Interfaces:**
- Consumes: `Card.BlockedBy`, `Card.PauseReason` (Task 2).
- Produces (`internal/domain/signal.go`):

  ```go
  const (
      SignalAguardandoVoce    Signal = "aguardando_voce"
      SignalBloqueado         Signal = "bloqueado"
      SignalProntoParaRetomar Signal = "pronto_para_retomar"
  )
  type SignalInput struct {
      Column           Column
      LastActivity     time.Time
      Now              time.Time
      BlockedBy        string // chave do card bloqueador ("" = sem bloqueio)
      BlockerConcluded bool   // o bloqueador existe e está em concluido
      BlockerExists    bool
      OpenQuestions    int    // perguntas sem resposta no card
  }
  func CardStatusSignal(in SignalInput) Signal
  ```

  Regras, nesta ordem:
  1. Backlog e Concluído só podem ter `pronto_para_retomar`.
  2. Com `BlockedBy` e `BlockerExists`: `BlockerConcluded` dá `pronto_para_retomar`; senão, `bloqueado`. Vale em `pausado` e `backlog`.
  3. Card em `pausado` com `OpenQuestions > 0` dá `aguardando_voce`.
  4. Nos outros casos, a regra atual `CardSignal(col, last, now)` ("parado" e "aguardando revisão" depois de 24 h).

  Os três sinais novos valem na hora, sem esperar 24 h. Eles também valem com sessão viva, exceto `aguardando_voce`: com sessão viva, nenhum sinal de pausa vale.
- Produces: `CardSummary.BlockedBy` (json `blocked_by`), e `signal_since` = `updated_at` do card para os sinais novos.
- Produces: `POST /api/me/block` `{key string, why string}`:
  - `key` vazia limpa o bloqueio;
  - chave inexistente dá 404 `card <KEY> não encontrado`;
  - a própria chave do card dá 400 `um card não pode bloquear a si mesmo`;
  - no sucesso, grava, registra a decisão `Bloqueado por <KEY>` (porquê: o texto de `--why`, ou vazio) e devolve o card.
- Produces: `nobre block <KEY> [--why "<motivo>"]` imprime `Card <ATUAL> bloqueado por <KEY>.`, e `nobre block --clear` imprime `Bloqueio removido.`.
- Produces: na lista de cards pausados do SessionStart, cada linha ganha o motivo, quando houver: ` · bloqueado por <KEY>`, ` · pronto para retomar (<KEY> concluído)`, ` · aguardando você (<n> pergunta(s))` ou ` · pausado por limite de custo`.
- `guidance` e skill: `nobre block <KEY>` quando o card só puder continuar depois de outro, antes de parar.

- [ ] **Step 1: Escrever os testes que falham**

```go
func TestCardStatusSignalBlocked(t *testing.T)         // pausado + BlockedBy "NAC-4", existe, não concluído → bloqueado (mesmo com last de 1 min)
func TestCardStatusSignalReadyToResume(t *testing.T)   // bloqueador concluído → pronto_para_retomar (pausado e backlog)
func TestCardStatusSignalWaitingUser(t *testing.T)     // pausado, sem bloqueio, OpenQuestions 1 → aguardando_voce; 0 perguntas e 25 h → parado
func TestCardStatusSignalMissingBlocker(t *testing.T)  // BlockedBy com BlockerExists false → regra normal (Review Focus 6)
func TestMeBlock(t *testing.T)                         // {"key":"NAC-2"} → 200, card.BlockedBy; key própria → 400; inexistente → 404; "" → limpa
func TestCardSummaryBlockedSignal(t *testing.T)        // GET /api/cards: card pausado bloqueado por card em andamento → signal "bloqueado", blocked_by; bloqueador movido para concluido → "pronto_para_retomar"
func TestSessionStartPausedListShowsReason(t *testing.T) // linha do card pausado contém " · bloqueado por NAC-1"
func TestCLIBlock(t *testing.T)                        // nobre block NAC-2 --why x → POST /api/me/block {"key":"NAC-2","why":"x"}; nobre block --clear → {"key":""}
```

- [ ] **Step 2: Rodar e ver falhar** — `go test ./internal/domain/ ./internal/api/ ./internal/adapter/claude/ ./cmd/nobre/ -run 'CardStatusSignal|MeBlock|BlockedSignal|ShowsReason|CLIBlock'` → FAIL.

- [ ] **Step 3: Implementar.** No `CardSummary`, busque o bloqueador por chave (`GetCard`): `ErrNotFound` conta como `BlockerExists=false`. `OpenQuestions` vem do `CardContent` que o resumo já lê. O teste da skill (`binary_test.go`) passa a exigir `nobre block`.

- [ ] **Step 4: Rodar e ver passar** — o mesmo comando → PASS. Depois `go vet ./... && go test ./...`.

- [ ] **Step 5: Commit** — `feat: motivo da pausa (aguardando você, bloqueado por, pronto para retomar)`

---

### Task 5: Interface — opções reais, campos que faltavam e marcas no card

**Files:**
- Modify: `web/src/api/types.ts`, `web/src/pages/Config.tsx`, `web/src/pages/Board.tsx`, `web/src/pages/CardPage.tsx`
- Create: `web/src/pages/Config.test.tsx`
- Test: `web/src/pages/board.test.ts` ou `CardPage.test.tsx` (marcas)

**Interfaces:**
- Consumes: os campos json `agent_creates_cards`, `inject_paused_cards` (Task 1), `pause_reason`, `handoff_incomplete` (Task 2) e os já existentes `grace_min` e `terminal_cmd`.
- `Settings` em `types.ts`: `agent_creates_cards: boolean; inject_paused_cards: boolean`. `Card`: `pause_reason: string; handoff_incomplete: boolean`.

Textos exatos:
- Criação de cards: as duas checkboxes ficam habilitadas e sem o selo "em breve", ligadas a `agent_creates_cards` e `inject_paused_cards`. Elas salvam junto com o lembrete, no mesmo `SaveRow`.
- Handoff: o campo novo `Carência antes de encerrar uma sessão cujo processo sumiu (minutos)` → `grace_min`, inteiro ≥ 0. O campo novo `Comando do terminal (separado por espaços)` aparece só com o modo `terminal` e mostra `terminal_cmd.join(' ')`; ao salvar, vira `split(/\s+/)` sem vazios. Erro do daemon aparece no `ErrorLine`.
- Opção `mark`: `Marcar o card como “handoff incompleto” e avisar na Sala de controle`.
- Ação do limite `pause`: `Bloquear o agente e mover o card para Pausado`.
- Marcas (badge `className="badge"`) no card do Board e no cabeçalho do CardPage: `pausado por limite de custo` quando `pause_reason === 'limite de custo'`, e `handoff incompleto` quando `handoff_incomplete && !readiness.ready`.
- Sinais novos (Task 4) em `signalText` (`web/src/pages/board.ts`):
  - `aguardando_voce` → `aguardando você há …` (mesmo formato de horas e dias);
  - `bloqueado` → `bloqueado por <blocked_by>`;
  - `pronto_para_retomar` → `pronto para retomar (<blocked_by> concluído)`.

  `signalText` recebe o card (ou `blocked_by`) para montar o texto. `bloqueado` usa a cor secundária; `pronto_para_retomar` usa `var(--ok)`.
- CardPage: campo `Bloqueado por` com a chave do card (texto, ex.: `NAC-4`). Salva com `PATCH /api/cards/{key}` `{blocked_by}`; vazio limpa.

- [ ] **Step 1: Escrever os testes que falham** (mock de `fetch` no padrão de `CardPage.test.tsx`)

```ts
it('salva as opções de criação de card')        // desmarca "Mostrar cards pausados…" → PUT /api/settings com inject_paused_cards: false
it('salva carência e comando do terminal')      // grace 20, "kitty -e" → PUT com grace_min: 20, terminal_cmd: ['kitty','-e']
it('esconde o comando do terminal fora do modo terminal') // modo background → campo ausente
it('mostra as marcas do card')                  // pause_reason 'limite de custo' → "pausado por limite de custo"; handoff_incomplete + ready false → "handoff incompleto"; ready true → sem a marca
it('mostra os sinais de pausa')                 // signal bloqueado + blocked_by NAC-4 → "bloqueado por NAC-4"; pronto_para_retomar → "pronto para retomar (NAC-4 concluído)"; aguardando_voce → "aguardando você há"
it('salva o bloqueio do card')                  // CardPage: digita NAC-4 em "Bloqueado por" → PATCH /api/cards/NAC-9 {blocked_by: 'NAC-4'}
```

- [ ] **Step 2: Rodar e ver falhar** — `pnpm -C web test --run Config CardPage` → FAIL.

- [ ] **Step 3: Implementar** os campos, os textos e as marcas acima.

- [ ] **Step 4: Rodar e ver passar** — `pnpm -C web test --run` → PASS. Depois `make build`.

- [ ] **Step 5: Commit** — `feat: configuração mostra e salva todas as opções que o daemon usa`

---

### Task 6: Documentação

**Files:**
- Modify: `docs/status-piloto.md` (seção "Configurações em vigor"), `docs/spec.md` §6.3 (o que `mark` faz) e §8 (a pausa move o card)

- [ ] **Step 1:** Descrever cada opção da Configuração e onde ela vale, e os sinais do card (parado, aguardando revisão, aguardando você, bloqueado, pronto para retomar). Listar o que continua "em breve": tmux, Codex/Kiro, limites mensais, moeda nos textos gerados pelo daemon e continuar com outro agente ao pausar por limite.
- [ ] **Step 2: Commit** — `docs: configurações em vigor`
