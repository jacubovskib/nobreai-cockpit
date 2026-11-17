# Mudanças de configuração e saúde da integração (NAC-6): plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** o Cockpit percebe sozinho quando a integração com o Claude Code quebra e oferece a correção num clique, e avisa as sessões abertas, uma vez, quando muda uma opção que elas só leem no início.

**Architecture:**
- Um monitor em `internal/integration` roda no `nobred`. Ele chama uma conferência leve do adaptador (os mesmos checks do `Detect`, sem `--version`), compara com uma linha de base guardada no banco e expõe problemas para a Sala de controle.
- As correções são as rotas de instalação que já existem, agora avisando o monitor.
- Cada sessão guarda em `seen_config` o que ouviu. A API aplica a mudança de captura nas sessões vivas, e o hook (`UserPromptSubmit`/`PostToolUse`) só compara e responde com `additionalContext`.
- Um histórico novo (`config_log`) registra tudo.

**Tech Stack:** Go 1.x (stdlib, SQLite via o `store` existente), React + TanStack Query + Vitest.

**Spec:** `docs/superpowers/specs/2026-11-17-mudancas-de-configuracao-design.md`

## Global Constraints

- Nada escreve em `~/.claude/settings.json` ou `~/.claude/skills` sem um clique do usuário (ou o `nobre setup` que ele rodou).
- Sem dependência nova (nada de `fsnotify`). A conferência compara data de modificação e tamanho dos arquivos.
- O monitor roda ao subir e a cada 30 s. A conferência completa roda quando algum arquivo mudou ou a cada 5 min.
- "Removidos de novo" vale quando o mesmo item quebra em menos de 10 min depois de uma correção.
- O hook não roda git nem lê `~/.claude`: a conferência de configuração é uma leitura de banco.
- O aviso ao agente vai só ao agente principal (`agent_id` vazio) e sai uma vez por mudança.
- O daemon de demonstração (`--seed-demo`) não roda o monitor.
- Textos em pt-BR, sem travessão no meio de frase. Mensagens ao agente começam com "NobreAI Cockpit:".
- `config_log` guarda as últimas 500 linhas.
- Toda tarefa: `go test ./...` e `cd web && pnpm test --run` verdes antes do commit.

## Review Focus

1. **`settings.json` com JSON inválido** (outra ferramenta salvou pela metade): o monitor alerta "o ~/.claude/settings.json não pôde ser lido" sem botão de reinstalar, e não derruba o daemon. Teste na Task 4.
2. **Hooks removidos com o daemon parado:** a linha de base sobrevive ao reinício, então o alerta aparece na primeira conferência depois de subir. Teste na Task 4.
3. **Duas chamadas de ferramenta em paralelo** (dois `PostToolUse` ao mesmo tempo depois de uma mudança): só um responde com o aviso. A gravação de `seen_config` é condicional (compare-and-set). Teste na Task 2.
4. **Desligar e religar antes do próximo passo da sessão:** nenhum aviso, porque o que a sessão viu é igual ao atual. Teste na Task 2.
5. **Sessão antiga retomada** (`claude --resume` de uma sessão anterior à migração, `seen_config` vazio): nenhum aviso falso, e o primeiro hook grava os valores atuais. Teste na Task 2.

---

### Task 1: Banco: `seen_config`, `config_log` e chaves livres em `settings`

**Files:**
- Create: `internal/store/migrations/011_config_changes.sql`
- Create: `internal/store/configlog.go`, `internal/store/configlog_test.go`
- Modify: `internal/store/sessions.go` (coluna em `sessionCols`/`scanSession`, funções novas)
- Modify: `internal/store/settings.go` (KV)
- Modify: `internal/domain/types.go` (`SeenConfig`, `Session.SeenConfig`, `ConfigLogEntry`)

**Interfaces:**
- Produces:
  - `domain.SeenConfig{AgentCreatesCards bool \`json:"agent_creates_cards"\`; Capture bool \`json:"capture"\`}`
  - `domain.Session.SeenConfig *domain.SeenConfig \`json:"seen_config"\`` (nil = coluna vazia)
  - `domain.ConfigLogEntry{ID int64; At time.Time; Kind string; Data json.RawMessage}` (tags snake_case)
  - `const domain.LogSettingChanged = "setting_changed"`, `LogProjectCapture = "project_capture_changed"`, `LogIntegrationBroken = "integration_broken"`, `LogIntegrationFixed = "integration_fixed"`, `LogIntegrationIgnored = "integration_ignored"`, `LogIntegrationRemoved = "integration_removed"`, `LogAgentNotified = "agent_notified"`
  - `(*Store) SwapSeenConfig(id string, old *domain.SeenConfig, nu domain.SeenConfig) (bool, error)`: compare-and-set; `old == nil` casa a coluna vazia; devolve false se outro hook gravou antes.
  - `(*Store) SetProjectSessionsCapture(projectID int64, on bool) ([]string, error)`: põe `full_capture` nas sessões **vivas** do projeto e devolve os ids alterados.
  - `(*Store) AdoptSession(id string, projectID int64) error`: `project_id = ?`, `full_capture = 1`.
  - `(*Store) GetKV(key string, v any) (bool, error)` e `(*Store) PutKV(key string, v any) error`, na tabela `settings`. `GetSettings` continua ignorando chaves que não são campos de `domain.Settings`.
  - `(*Store) AddConfigLog(kind string, data any, at time.Time) error` (apaga além das 500 mais recentes) e `(*Store) RecentConfigLog(limit int) ([]domain.ConfigLogEntry, error)` (mais recente primeiro).

- [ ] **Step 1: Write the failing tests** (`internal/store/configlog_test.go`, seguindo o padrão `openTest(t)` dos testes do store):
  - `TestSwapSeenConfig`: sessão nova tem `SeenConfig == nil`. `SwapSeenConfig(id, nil, {true,true})` devolve true. Repetir com `old=nil` devolve false. Com `old=&{true,true}` para `{false,true}`, devolve true, e `GetSession` mostra `{false,true}`.
  - `TestSetProjectSessionsCaptureOnlyLive`: duas sessões do projeto 1 (uma encerrada) e uma do projeto 2, todas com `full_capture=1`. `SetProjectSessionsCapture(1,false)` devolve só o id da viva do projeto 1, e as outras ficam com `FullCapture == true`.
  - `TestAdoptSession`: sessão sem projeto, depois de `AdoptSession(id, p)`, tem `ProjectID == &p` e `FullCapture == true`.
  - `TestKVDoesNotLeakIntoSettings`: `PutKV("integration_baseline", map[string]bool{"hooks":true})`; `GetKV` devolve o mesmo mapa e true; `GetKV("ausente", &v)` devolve false; `GetSettings()` não dá erro e continua igual a `DefaultSettings()`.
  - `TestConfigLogKeeps500`: 502 `AddConfigLog`; `RecentConfigLog(1000)` tem 500 linhas, e a primeira é a última gravada.
- [ ] **Step 2:** `go test ./internal/store/ -run 'Swap|ProjectSessionsCapture|Adopt|KV|ConfigLog'`. Esperado: FAIL (não compila).
- [ ] **Step 3: Migration `011_config_changes.sql`:** `ALTER TABLE sessions ADD COLUMN seen_config TEXT NOT NULL DEFAULT '';` e `CREATE TABLE config_log (id INTEGER PRIMARY KEY AUTOINCREMENT, workspace_id TEXT NOT NULL DEFAULT 'local', owner_id TEXT NOT NULL DEFAULT 'me', at TEXT NOT NULL, kind TEXT NOT NULL, data TEXT NOT NULL DEFAULT '{}');`. Depois, os tipos de domínio e as funções acima. `seen_config` é lido em `scanSession` (vazio vira nil, senão `json.Unmarshal`).
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(store): seen_config, config_log e chaves livres em settings`

### Task 2: Hook: o que a sessão viu e o aviso único

**Files:**
- Create: `internal/adapter/claude/notice.go`, `internal/adapter/claude/notice_test.go`
- Modify: `internal/adapter/claude/context.go` (`sessionStart` grava `seen_config`)
- Modify: `internal/adapter/claude/hooks.go` (`HandleHook`: `UserPromptSubmit` e `PostToolUse`)

**Interfaces:**
- Consumes: `SwapSeenConfig`, `AddConfigLog`, `domain.SeenConfig` (Task 1); `guidanceFor(set)`, `instructionsFor(set)` (já existem em `context.go`).
- Produces:
  - `configNotice(seen *domain.SeenConfig, fullCapture bool, set domain.Settings) (text string, nu domain.SeenConfig)`, uma função pura: `text == ""` = nada a dizer.
  - `(a *Adapter) noticeReply(sess domain.Session, p payload, event string, now time.Time) (adapter.HookReply, bool)`.

Regras de `configNotice` (texto exato fixado pelo spec):
- `seen == nil`: devolve `""` e `{set.AgentCreatesCards, fullCapture}`.
- Captura desligada (`seen.Capture && !fullCapture`): o texto é `"NobreAI Cockpit: a captura deste projeto foi desligada. Não use mais os comandos nobre nesta sessão."` e não inclui a parte de cards.
- Captura ligada (`!seen.Capture && fullCapture`): o texto é `"NobreAI Cockpit: a captura deste projeto foi ligada.\n" + instructionsFor(set)`, que já traz a orientação de cards atual.
- Só cards mudou, com captura ligada: `"NobreAI Cockpit: a configuração mudou.\n" + guidanceFor(set)`.

- [ ] **Step 1: Write the failing tests:**
  - `notice_test.go` (pura):
    - `TestConfigNoticeNilSeen`: texto vazio.
    - `TestConfigNoticeCardsOff`: `seen={true,true}`, `set.AgentCreatesCards=false`. O texto contém `"a configuração mudou"` e `"Só crie card quando o usuário pedir"`.
    - `TestConfigNoticeCaptureOff`: o texto contém `"foi desligada"` e não contém `"Só crie card"`, mesmo com cards também mudado.
    - `TestConfigNoticeCaptureOn`: o texto contém `"foi ligada"` e `registeredPhrase`.
    - `TestConfigNoticeFlipBack`: `seen={true,true}`, `set` com cards true e captura true: texto vazio (Review Focus 4).
  - `hooks_test.go` (com `newEnv(t)`):
    - `TestSessionStartRecordsSeenConfig`: depois do `SessionStart`, `SeenConfig == &{true, true}` numa sessão de projeto com captura.
    - `TestPromptAfterCardsSettingChangeNotifiesOnce`: depois do `SessionStart`, `PutSettings(AgentCreatesCards=false)`. O `UserPromptSubmit` responde JSON com `hookSpecificOutput.hookEventName == "UserPromptSubmit"` e `additionalContext` contendo `"Só crie card"`. O segundo `UserPromptSubmit` responde vazio, e `RecentConfigLog` tem uma linha `agent_notified`.
    - `TestPostToolUseCarriesNotice`: o mesmo, via `PostToolUse` (`hookEventName == "PostToolUse"`).
    - `TestSubagentGetsNoNotice`: `PostToolUse` com `agent_id` preenchido responde vazio; o agente principal ainda recebe o aviso depois.
    - `TestConcurrentPostToolUseSingleNotice`: duas goroutines mandam `PostToolUse` ao mesmo tempo depois da mudança; exatamente uma resposta contém `additionalContext` (Review Focus 3).
    - `TestLegacySessionNoFalseNotice`: sessão com `seen_config` vazio (criada direto no store); o `UserPromptSubmit` responde vazio e grava `SeenConfig` com os valores atuais (Review Focus 5).
- [ ] **Step 2:** `go test ./internal/adapter/claude/ -run 'ConfigNotice|SeenConfig|Notice|LegacySession'`. Esperado: FAIL.
- [ ] **Step 3: Implementar.**
  - **`sessionStart`:** grava `SwapSeenConfig(sess.ID, nil, {set.AgentCreatesCards, sess.FullCapture})` (ignora false).
  - **`noticeReply`:** só roda com `p.AgentID == ""`. Chama `configNotice`, e com `nu != *seen` faz `SwapSeenConfig(sess.ID, sess.SeenConfig, nu)`. Só responde se o swap devolveu true e há texto. Nesse caso grava `AddConfigLog(LogAgentNotified, {"session": sess.ID, "text": primeira linha})`.
  - **`HandleHook`:** em `UserPromptSubmit` e `PostToolUse`, roda `noticeReply` **antes** do filtro `sess.FullCapture`. Em `PostToolUse`, o `postToolUse` existente continua rodando e o aviso é a resposta.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(hooks): sessão aberta recebe uma vez a mudança de configuração`

### Task 3: API: aplicar mudanças, contar sessões avisadas, histórico

**Files:**
- Modify: `internal/api/config.go` (`putSettings`, `patchProject`)
- Create: `internal/api/configlog.go`, `internal/api/configlog_test.go`

**Interfaces:**
- Consumes: Task 1 (`SetProjectSessionsCapture`, `AdoptSession`, `AddConfigLog`, `RecentConfigLog`); o `projectFor`/`gitinfo` usado por `createProject` para achar as sessões de uma pasta e dos worktrees ligados (`ReassignSessions`/`AssignSessionsInWorktree` mostram o padrão).
- Produces:
  - `PUT /api/settings` responde `domain.Settings` mais `"notified_sessions": int`.
  - `PATCH /api/projects/{id}` responde o projeto mais `"notified_sessions": int`.
  - `GET /api/config-log?limit=50` responde `[]domain.ConfigLogEntry` (limite padrão 50, máximo 500).
  - Publica o tipo `"config_log"` no stream sempre que grava no log.

Regras:
- **`putSettings`:** compara o antigo com o novo campo a campo (JSON de cada chave). Para cada chave diferente, grava `AddConfigLog(LogSettingChanged, {"key","from","to"})`. Com `agent_creates_cards` mudado, `notified_sessions` = sessões vivas com `FullCapture`, `SeenConfig != nil` e `SeenConfig.AgentCreatesCards != novo`.
- **`patchProject`, captura desligada:** `SetProjectSessionsCapture(id,false)`; `notified_sessions` = número de ids devolvidos.
- **`patchProject`, captura ligada:** para cada sessão viva sem captura cujo `cwd` está dentro de `p.Path` (ou cujo worktree pertence ao projeto, como em `createProject`), roda `AdoptSession`; conta-as.
- Nos dois casos grava `LogProjectCapture`.

- [ ] **Step 1: Write the failing tests** (`configlog_test.go`, com o `testAPI` existente):
  - `TestPutSettingsCountsNotifiedSessions`: duas sessões vivas com captura e `seen_config` `{true,true}`, uma encerrada. `PUT` com `agent_creates_cards=false` responde `notified_sessions == 2`; o log tem `setting_changed` com `key == "agent_creates_cards"`.
  - `TestPutSettingsUnchangedNoLog`: `PUT` com os mesmos valores responde `notified_sessions == 0`, e o log fica vazio.
  - `TestPatchProjectCaptureOffStopsLiveSessions`: sessão viva do projeto com captura. O `PATCH capture=false` responde `notified_sessions == 1`, a sessão fica com `FullCapture == false` e com `ProjectID` mantido.
  - `TestPatchProjectCaptureOnAdoptsSessions`: sessão viva com `cwd` dentro do projeto, sem projeto. O `PATCH capture=true` responde 1, e a sessão fica com `ProjectID` do projeto e `FullCapture == true`.
  - `TestConfigLogRoute`: duas entradas; `GET /api/config-log?limit=1` devolve só a mais recente.
- [ ] **Step 2:** `go test ./internal/api/ -run 'NotifiedSessions|SettingsUnchanged|CaptureOff|CaptureOn|ConfigLogRoute'`. Esperado: FAIL.
- [ ] **Step 3:** implementar como descrito; publicar `"sessions"` quando sessões mudarem.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(api): captura e opções aplicadas às sessões vivas, com histórico`

### Task 4: Monitor da integração

**Files:**
- Modify: `internal/adapter/adapter.go` (`Check.Code`)
- Modify: `internal/adapter/claude/detect.go` (separar `checks(withVersion bool)`, preencher `Code`)
- Create: `internal/adapter/claude/health.go`, `internal/adapter/claude/health_test.go`
- Create: `internal/integration/monitor.go`, `internal/integration/monitor_test.go`

**Interfaces:**
- Consumes: `GetKV`, `PutKV`, `AddConfigLog` (Task 1).
- Produces:
  - `adapter.Check.Code string \`json:"code,omitempty"\``, com valores `"missing"`, `"outdated"`, `"dead_binary"`, `"unreadable"`, `"not_found"`. O `Detect()` existente continua com as mesmas chaves e textos.
  - `(a *claude.Adapter) Health() []adapter.Check`: os checks `cli`, `hooks`, `statusline` e `skill`, sem `--version` e sem `events`.
  - `(a *claude.Adapter) WatchedFiles() []string`: `SettingsPath`, o `SKILL.md` do Cockpit e `NobreBin`.
  - No pacote `integration`:

```go
type Store interface {
    GetKV(key string, v any) (bool, error)
    PutKV(key string, v any) error
    AddConfigLog(kind string, data any, at time.Time) error
}
type Action string // "reinstall", "statusline", "restart", "make_install", "config"
type Problem struct {
    Key    string `json:"key"`    // chave do check
    Text   string `json:"text"`
    Action Action `json:"action"`
    Again  bool   `json:"again"`  // quebrou de novo < 10 min depois de corrigido
}
type Monitor struct {
    Health      func() []adapter.Check
    Files       func() []string
    NobreBin    func() string // binário nobre atual em disco (para "restart")
    Store       Store
    Now         func() time.Time
    DaemonStart time.Time
    Publish     func(kind string)
    Logf        func(string, ...any)
}
func (m *Monitor) Run(ctx context.Context, every, full time.Duration) // every=30s, full=5min
func (m *Monitor) CheckNow() []Problem
func (m *Monitor) Problems() []Problem
func (m *Monitor) Ignore(key string) error
func (m *Monitor) Fixed(keys []string, changed []string)
func (m *Monitor) Removed(keys []string, origin string) // origin: "config" ou "cli"
```

Regras (do spec):
- **Linha de base:** `integration_baseline` (map key→bool) guarda os checks que já estiveram OK. Check OK sempre entra. Check com problema só vira `Problem` se a linha de base o tem como true. Na primeira conferência sem linha de base, nada alerta.
- **Transição:** de OK para problema grava `LogIntegrationBroken` uma vez e publica `"integration"`. Volta a OK publica de novo.
- **Ignorar:** `integration_ignored` (map key→impressão). A impressão é `code + "|" + detail + "|" + mtime e tamanho do settings.json`. Problema com a impressão igual some. `Ignore` grava `LogIntegrationIgnored`.
- **Ação por check:**
  - `hooks` e `skill` com `missing` ou `dead_binary`: `reinstall`.
  - `skill` `outdated`: `restart` se o `NobreBin()` em disco tem mtime posterior a `DaemonStart`, senão `reinstall`.
  - `statusline`: `statusline`.
  - `cli`: `config`.
  - `hooks` `unreadable`: `config`, com o texto `"O ~/.claude/settings.json não pôde ser lido (JSON inválido?). O Cockpit não vai mexer nele; corrija o arquivo."`
  - Se `Health` disser que o binário dos hooks morreu e `NobreBin()` for `""`: `make_install`.
- **`Fixed`:** grava `LogIntegrationFixed` com `changed`, guarda a hora por chave, limpa a impressão de Ignorar dessas chaves e roda `CheckNow`. `Again` = mesmo check quebrou em menos de 10 min depois de `Fixed`.
- **`Removed`:** tira as chaves da linha de base (vira false), grava `LogIntegrationRemoved` com `origin`, e roda `CheckNow`.
- **`Run`:** guarda mtime e tamanho de `Files()`; a cada `every`, se mudou ou passou `full`, roda `CheckNow`.

- [ ] **Step 1: Write the failing tests:**
  - `health_test.go` (diretório temporário via `SetPaths`):
    - `TestHealthCodes`: sem hooks, `Code == "missing"`; hooks apontando para `/nao/existe/nobre`, `"dead_binary"`; skill com conteúdo diferente, `"outdated"`; `settings.json` com `{` , `hooks.Code == "unreadable"`.
    - `TestHealthSkipsVersion`: `Paths.Version` que faz `t.Fatal`; `Health()` não o chama.
  - `monitor_test.go` (store fake em memória, `Health` controlado pelo teste):
    - `TestNoBaselineNoAlert`: `Health` com hooks faltando e sem linha de base: `CheckNow()` vazio.
    - `TestBrokenAfterOK`: OK depois faltando: um `Problem{Key:"hooks", Action:"reinstall"}`, uma linha `integration_broken`, `Publish("integration")` chamado.
    - `TestBaselineSurvivesRestart`: monitor A vê OK; monitor B novo com o mesmo store vê faltando: alerta (Review Focus 2).
    - `TestIgnoreUntilFingerprintChanges`: `Ignore("hooks")` some; detalhe diferente volta.
    - `TestAgainWithin10Min`: `Fixed(["hooks"])`, OK, depois de 5 min faltando: `Again == true`; depois de 11 min, `false`.
    - `TestRemovedDoesNotAlert`: `Removed(["statusline"],"config")` depois statusline com problema: sem alerta, com uma linha `integration_removed`.
    - `TestOutdatedSkillAfterUpgradeAsksRestart`: `NobreBin` aponta para um arquivo com mtime depois de `DaemonStart`: `Action == "restart"`.
    - `TestUnreadableSettings`: `Code "unreadable"`: `Action == "config"`, texto do spec, sem panic (Review Focus 1).
- [ ] **Step 2:** `go test ./internal/adapter/claude/ -run Health ./internal/integration/`. Esperado: FAIL.
- [ ] **Step 3:** implementar. `Detect()` passa a ser `checks(true)` mais `events`; `Health()` é `checks(false)`. Para isso, `installedHooks` precisa distinguir JSON inválido (`unreadable`) de arquivo ausente (`missing`).
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(integration): monitor da integração com linha de base e ignorar`

### Task 5: Correção sob confirmação: rotas, alerta e `nobre` re-resolvido

**Files:**
- Modify: `internal/adapter/claude/install.go` (`Install`/`InstallStatusLine` re-resolvem o nobre; `PlanInstall`)
- Modify: `internal/adapter/adapter.go` (interface opcional `InstallPlanner`)
- Modify: `internal/api/server.go` (`Deps.Integration`), `internal/api/agents.go`, `internal/api/overview.go`
- Create: `internal/api/integration.go`, `internal/api/integration_test.go`
- Modify: `cmd/nobred/main.go`

**Interfaces:**
- Consumes: `integration.Monitor` (Task 4).
- Produces:
  - `adapter.InstallPlanner interface{ PlanInstall() (InstallReport, error) }`: `PlanHooks` + `PlanSkill`, sem escrever.
  - `(a *Adapter) resolveNobre() string`: repete a lógica de `DefaultPaths` (ao lado do `nobred`, depois `launch.LookPathIn("nobre", SearchPATH())`). Atualiza `a.paths.NobreBin` quando acha.
  - `api.IntegrationSource interface{ Problems() []integration.Problem; CheckNow() []integration.Problem; Ignore(string) error; Fixed(keys, changed []string); Removed(keys []string, origin string) }` em `Deps.Integration` (nil = rotas 503 e sem alerta).
  - Rotas:
    - `GET /api/integration` → `[]Problem`;
    - `POST /api/integration/check` → `[]Problem`;
    - `POST /api/integration/{key}/ignore`;
    - `POST /api/integration/removed` com `{"keys":[...],"origin":"cli"}`;
    - `GET /api/agents/{agent}/install/plan` → `InstallReport`.
  - `Alert.Integration *integration.Problem \`json:"integration,omitempty"\``. Cada problema vira `Alert{Kind:"integration", Agent:"claude", Text: p.Text, Integration:&p}` em `buildOverview`.

Regras:
- **`installAgent`, com sucesso:** chama `Fixed([...]{"hooks","skill"}, rep.Changed)`.
- **`agentStatusLine install`:** chama `Fixed(["statusline"], rep.Changed)`.
- **`agentStatusLine uninstall`:** chama `Removed(["statusline"], "config")`.
- **Texto dos problemas** (o monitor monta, a API repassa):
  - `hooks` `missing`: `"Os hooks do Cockpit sumiram do ~/.claude/settings.json. As sessões novas do Claude Code não serão registradas."`
  - Com `Again`: `"Os hooks do Cockpit foram removidos de novo logo depois de reinstalados: outra ferramenta pode estar reescrevendo o ~/.claude/settings.json."`
  - `dead_binary`: `"Os hooks apontam para um nobre que não existe mais (<caminho>)."`
  - `skill`: `"A skill do Cockpit está desatualizada."`
  - `restart`: `"O Cockpit foi atualizado; reinicie o daemon (make restart)."`
  - `statusline`: `"A status line do Cockpit foi trocada por outro comando."`
  - `cli`: `"O claude não foi encontrado no PATH."`
  - `make_install`: `"O nobre não foi encontrado; rode make install."`

- [ ] **Step 1: Write the failing tests:**
  - `integration_test.go` (com `testAPI` e um `IntegrationSource` fake):
    - `TestOverviewHasIntegrationAlert`: fake com um problema; `GET /api/overview` tem `Alert{kind:"integration", integration.action:"reinstall"}`.
    - `TestIgnoreRoute`: chama `Ignore("hooks")`.
    - `TestRemovedRoute`: chama `Removed(["statusline"],"cli")`; chave desconhecida responde 400.
    - `TestInstallMarksFixed`: `POST /api/agents/claude/install` (com o `agentsAPI` existente) chama `Fixed` com `"hooks","skill"`.
    - `TestStatusLineUninstallMarksRemoved`.
    - `TestInstallPlanDoesNotWrite`: `GET .../install/plan` lista as mudanças, e o `settings.json` não muda (compara bytes).
    - `TestNilIntegration503`.
  - `install_test.go`: `TestInstallReResolvesNobre`: `Paths.NobreBin` aponta para um caminho apagado e `SearchPATH` tem um diretório com `nobre` executável; depois de `Install()`, o hook gravado usa o caminho novo.
- [ ] **Step 2:** `go test ./internal/api/ -run 'Integration|IgnoreRoute|RemovedRoute|MarksFixed|MarksRemoved|InstallPlan' ./internal/adapter/claude/ -run ReResolves`. Esperado: FAIL.
- [ ] **Step 3:** implementar. No `cmd/nobred/main.go`, quando `!seedDemo`, criar o `integration.Monitor`:
  - `Health: claudeAdapter.Health`, `Files: claudeAdapter.WatchedFiles`, `DaemonStart: time.Now()`, `Publish: hub.Publish`;
  - rodar `go mon.Run(ctx, 30*time.Second, 5*time.Minute)`, terminando antes de `st.Close` como o reconciliador;
  - passar em `api.Deps.Integration`.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat: alerta de integração na Sala de controle com correção sob confirmação`

### Task 6: CLI: remoção feita de propósito não alerta

**Files:**
- Modify: `cmd/nobre/setup.go` (`removeAll`, `removeStatusLine`)
- Modify: `internal/client/client.go` (`IntegrationRemoved`)
- Test: `cmd/nobre/setup_test.go`

**Interfaces:**
- Consumes: `POST /api/integration/removed` (Task 5).
- Produces: `(c *Client) IntegrationRemoved(ctx context.Context, keys []string, origin string) error`.

- [ ] **Step 1: Write the failing tests:**
  - `TestRemoverStopsServiceFirst`: o `setupDeps.Run` fake grava a ordem; `systemctl --user disable --now nobred` vem antes de qualquer escrita no `settings.json` (o fake confere que o arquivo ainda tem os hooks na hora da chamada).
  - `TestRemoverStatusLineTellsDaemon`: com um servidor de teste, `--remover-statusline` faz `POST /api/integration/removed` com `{"keys":["statusline"],"origin":"cli"}` **antes** de escrever; com o daemon fora do ar, a remoção segue e o checklist não marca `!`.
- [ ] **Step 2:** `go test ./cmd/nobre/ -run 'RemoverStops|RemoverStatusLine'`. Esperado: FAIL.
- [ ] **Step 3:** implementar.
  - `removeAll` chama `removeService` primeiro e mantém as outras etapas na mesma ordem.
  - `removeStatusLine` (sem dry-run) chama `IntegrationRemoved` com timeout de 2 s e ignora o erro.
- [ ] **Step 4:** a mesma linha de teste e `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `fix(setup): remover de propósito não vira alerta de integração`

### Task 7: Interface: alerta com ação, sessões avisadas e histórico

**Files:**
- Modify: `web/src/api/types.ts` (`Alert.integration`, `ConfigLogEntry`, `notified_sessions`)
- Modify: `web/src/api/useStream.ts` (tipos `integration` e `config_log` invalidam `overview` e `config-log`)
- Modify: `web/src/pages/ControlRoom.tsx` (`AlertRow` para `kind === 'integration'`)
- Create: `web/src/pages/config/ConfigLog.tsx`
- Modify: `web/src/pages/Config.tsx` (`CardCreation`, `Projects`, seção de histórico)
- Test: `web/src/pages/ControlRoom.test.tsx`, `web/src/pages/Config.test.tsx`
- Modify: `docs/status-piloto.md` (seção "Mudanças de configuração e saúde da integração (NAC-6)"), `README.md` (uma linha em "Problemas comuns": o alerta de integração e o botão)

**Interfaces:**
- Consumes: rotas das Tasks 3 e 5.

Comportamento:
- **`AlertRow` de integração, por ação:**
  - `reinstall`: botão "Reinstalar". Primeiro clique: `GET /api/agents/claude/install/plan` e mostra em linha "Vai alterar: <Changed separados por vírgula>. O resto do arquivo não muda." com "Confirmar" e "Cancelar". "Confirmar" faz `POST /api/agents/claude/install` e mostra `Messages`, mais o lembrete `"Sessões abertas enquanto os hooks estavam fora não são registradas; reinicie-as."`
  - `statusline`: "Restaurar a do Cockpit" (`POST .../statusline {action:"install"}`) e "Manter a atual" (`POST /api/integration/statusline/ignore`).
  - `restart` e `make_install`: só o texto, com o comando em `<span className="mono">`.
  - `config`: link "Ver Configuração".
  - Toda ação, menos as duas últimas, tem "Ignorar" (`POST /api/integration/{key}/ignore`).
- **`SaveRow`:** ganha `notified?: number`. Com `notified > 0`, mostra depois de "Salvo.": `"<n> sessão aberta vai receber o aviso no próximo passo."` ou `"<n> sessões abertas vão receber o aviso no próximo passo."`. `CardCreation` usa o retorno do `PUT`, e o toggle de captura em `Projects` mostra o mesmo texto ao lado do projeto.
- **`ConfigLog`:** seção "Histórico de mudanças". `GET /api/config-log?limit=50`, uma linha por entrada, `HH:MM · frase`. Frases por `kind`:
  - `setting_changed`: "Opção <chave> alterada";
  - `project_capture_changed`: "Captura do projeto <nome> ligada/desligada";
  - `integration_broken`: "<item>: problema detectado";
  - `integration_fixed`: "<item> corrigido por você";
  - `integration_ignored`: "<item>: alerta ignorado";
  - `integration_removed`: "<item> removido por você";
  - `agent_notified`: "Sessão <id curto> avisada".

  Sem linhas: "Nenhuma mudança registrada."

- [ ] **Step 1: Write the failing tests:**
  - `ControlRoom.test.tsx`:
    - `integration alert asks before reinstalling`: com o fetch mockado, o primeiro clique em "Reinstalar" não chama `POST .../install`, mostra "Vai alterar: hooks"; "Confirmar" chama o `POST`.
    - `statusline alert offers keep current`: "Manter a atual" chama `POST /api/integration/statusline/ignore`.
    - `restart alert has no buttons`: sem botão "Reinstalar".
  - `Config.test.tsx`:
    - `shows notified sessions after save`: `PUT` responde `notified_sessions: 2`; aparece "2 sessões abertas vão receber o aviso no próximo passo.".
    - `zero notified shows only Salvo`.
    - `config log lists entries`: `integration_fixed` com item `hooks` aparece como "hooks corrigido por você".
- [ ] **Step 2:** `cd web && pnpm test --run ControlRoom Config`. Esperado: FAIL.
- [ ] **Step 3:** implementar e atualizar os dois documentos.
- [ ] **Step 4:** `cd web && pnpm test --run && pnpm build`, depois `go test ./...`. Esperado: PASS.
- [ ] **Step 5: Commit** `feat(web): alerta de integração com confirmação, sessões avisadas e histórico`

### Task 8: Validação ao vivo e instalação

- [ ] **Step 1:** backup do banco com a API de backup do `sqlite3` do Python (`~/.local/share/nobreai-cockpit/backup-pre-nac6-<data>.db`), depois `make update`.
- [ ] **Step 2:** com uma cópia do `settings.json` guardada, apagar o bloco `hooks` à mão. Em até 30 s, a Sala de controle mostra o alerta. "Reinstalar" mostra o que vai mudar, e "Confirmar" o restaura. O histórico mostra "problema detectado" e "corrigido por você".
- [ ] **Step 3:** numa sessão aberta, desligar "agente cria card" na Configuração. A tela diz "1 sessão aberta…", e a próxima mensagem da sessão recebe o aviso uma vez. Religar.
- [ ] **Step 4:** registrar no card NAC-6 (`nobre test`, `nobre note`) e `nobre done`.
