# Release compilada e instalador de um comando — Plano de implementação (NAC-52)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** uma tag `v*` publica pelo CI uma release com `nobre`/`nobred` compilados para macOS e Linux, e um `install.sh` (rodado por `curl | sh`) instala, configura e atualiza o Cockpit sem Go, Node ou pnpm na máquina.

**Architecture:** a versão vive num pacote `internal/version` preenchido por `-ldflags -X`; `make dist` empacota os quatro alvos com `checksums.txt`; o workflow `release.yml` roda testes, `make dist` e `gh release create`; `install.sh` baixa (ou lê de um diretório local, para testes), confere o sha256, instala em `~/.local/bin`, ajusta o PATH, roda `nobre setup` e reinicia o serviço quando é atualização.

**Tech Stack:** Go 1.26.5 (cross-compile `CGO_ENABLED=0`, SQLite modernc puro Go), pnpm/Vite para `web/dist`, POSIX `sh` para o instalador, bash para o teste do instalador, GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-11-25-release-compilada-design.md`

## Global Constraints

- Textos de saída, comentários e docs em pt-BR, frases curtas.
- Repositório público `jacubovskib/nobreai-cockpit`; módulo Go `github.com/nobretech/nobreai-cockpit`.
- Alvos: `darwin/arm64`, `darwin/amd64`, `linux/amd64`, `linux/arm64`, todos com `CGO_ENABLED=0`.
- Pacote: `nobreai-cockpit_<versão>_<os>_<arch>.tar.gz` contendo `nobre` e `nobred` na raiz do tar; `checksums.txt` no formato do `sha256sum` (`<hash>  <arquivo>`). `<versão>` é a tag com o `v` (ex.: `v0.1.0`).
- Versão: tag no CI; `git describe --tags --always --dirty` no `make build`; `dev` sem ldflags.
- `install.sh` é POSIX `sh` (tem de rodar com `dash` e com o `sh` do macOS), nunca usa `sudo`, nunca apaga o banco, e para com mensagem clara em qualquer falha (`set -eu`).
- `nobre setup` sempre com `</dev/null` dentro do script (stdin é o próprio script no `curl | sh`).
- Serviço: rótulo launchd `io.nobretech.nobred` (`launchctl kickstart -k gui/$(id -u)/io.nobretech.nobred`); unidade systemd do usuário `nobred` (`systemctl --user restart nobred`).
- `make setup` / `make update` continuam funcionando como hoje.

## Review Focus

1. **Rodar o script de novo** numa máquina já instalada: troca os binários, reinicia o serviço uma vez, não duplica a linha do PATH nem os hooks, não toca no banco. → teste `reexecucao` na Task 3 e passo de atualização no CI do macOS (Task 4).
2. **Versão que não existe ou download que falha** (`NOBRE_VERSION=v9.9.9`, rede fora): mensagem dizendo o que faltou e nada instalado em `BINDIR`. → teste `pacote_ausente` na Task 3.
3. **sha256 que não confere**: aborta antes de tocar em `BINDIR`. → teste `checksum_errado` na Task 3.
4. **Sistema/arquitetura sem pacote** (`Linux armv7l`, `MINGW64_NT`): erro claro listando os alvos suportados. → teste `arquitetura_sem_pacote` na Task 3.
5. **`nobre setup` falha ou a API não sobe**: o script sai com código ≠ 0 e diz onde ver o log (`~/Library/Logs/nobred.log` ou `journalctl --user -u nobred`). → teste `setup_falha` na Task 3.

---

### Task 1: versão embutida e `nobre version`

**Files:**
- Create: `internal/version/version.go`
- Modify: `internal/api/server.go:22-23,79` (tira a `const Version`; `/api/health` usa `version.Version`)
- Modify: `cmd/nobre/main.go` (linha no `usage` na seção Administração, `case "version"`)
- Create: `cmd/nobre/version.go`, `cmd/nobre/version_test.go`
- Modify: `internal/api/server_test.go` (asserção da versão no health)
- Modify: `Makefile` (`VERSION`, `LDFLAGS`, alvo `build`)

**Interfaces:**
- Produces: `package version; var Version = "dev"` — caminho para o ldflag: `github.com/nobretech/nobreai-cockpit/internal/version.Version`.
- Produces: Makefile `VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)` e `GO_LDFLAGS = -X github.com/nobretech/nobreai-cockpit/internal/version.Version=$(VERSION)`; `build` passa `-ldflags "$(GO_LDFLAGS)"` aos dois `go build`.
- Produces: `nobre version` — imprime `nobre <v>`; consulta `GET /api/health` (`c.client().Do(ctx(), "GET", "/api/health", nil, &out)` com `out struct{ Version string }`) e imprime `nobred <v>`; se diferentes, linha `As versões diferem: reinicie o serviço (make restart ou rode o install.sh de novo).`; daemon fora: `nobred: não respondeu em <addr>`. Sai com 0 nos três casos.

- [ ] **Step 1: testes que falham**

Em `internal/api/server_test.go`, no teste do health existente, decodificar o corpo e afirmar `body["version"] == version.Version`.

Em `cmd/nobre/version_test.go` (use o helper `fake(t, handler)` de `hook_test.go` e o `cli{...}` como em `admin_test.go`; troque `version.Version` no teste e restaure com `t.Cleanup`):
- `TestCLIVersionIguais`: versão do CLI `v0.1.0`, health responde `{"ok":true,"version":"v0.1.0"}` → saída contém `nobre v0.1.0` e `nobred v0.1.0`, não contém `diferem`, código 0.
- `TestCLIVersionDiferentes`: CLI `v0.2.0`, health `v0.1.0` → saída contém `As versões diferem`, código 0.
- `TestCLIVersionDaemonFora`: `NOBRE_ADDR` apontando para porta fechada (`127.0.0.1:1`) → saída contém `nobre v0.1.0` e `nobred: não respondeu em 127.0.0.1:1`, código 0.

- [ ] **Step 2:** `go test ./internal/api/ ./cmd/nobre/ -run 'Health|Version'` → FALHA (pacote `version` inexistente / comando desconhecido).

- [ ] **Step 3:** criar `internal/version/version.go`, trocar a const em `internal/api/server.go`, adicionar `func (c *cli) version() error` em `cmd/nobre/version.go`, o `case "version"` no `switch` de `run` e a linha `  nobre version                                           mostra a versão do nobre e do nobred` no `usage` (seção Administração, mesmo alinhamento das vizinhas). Atualizar o `Makefile` como em Interfaces.

- [ ] **Step 4:** `go test ./internal/api/ ./cmd/nobre/` → PASSA. Depois `make build && ./nobre version` → primeira linha `nobre <saída do git describe>`.

- [ ] **Step 5: commit** `feat: versão embutida nos binários e nobre version (NAC-52)`

---

### Task 2: `make dist` (pacotes da release)

**Files:**
- Modify: `Makefile` (alvo `dist`, `.PHONY`, `clean` apaga `dist/`)
- Modify: `.gitignore` (`/dist/`)

**Interfaces:**
- Consumes: `VERSION`, `GO_LDFLAGS` da Task 1.
- Produces: `make dist [VERSION=v0.1.0]` → `dist/nobreai-cockpit_$(VERSION)_<os>_<arch>.tar.gz` para os quatro alvos e `dist/checksums.txt`. Depende do alvo `web`. Usado pelas Tasks 3 (teste manual), 4 (CI) e pelo `install.sh`.

- [ ] **Step 1:** escrever o alvo. Para cada alvo: `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -trimpath -ldflags "-s -w $(GO_LDFLAGS)"` de `./cmd/nobre` e `./cmd/nobred` num diretório de trabalho `dist/<os>_<arch>/`, depois `tar -C dist/<os>_<arch> -czf dist/<pacote> nobre nobred` e apagar o diretório de trabalho. Começa com `rm -rf dist && mkdir dist`. `checksums.txt` gerado dentro de `dist/` só com os nomes dos arquivos (`cd dist && sha256sum *.tar.gz > checksums.txt`, caindo para `shasum -a 256` quando `sha256sum` não existe — macOS).

- [ ] **Step 2: verificar**

Run: `make dist VERSION=v0.0.0-teste && ls dist && (cd dist && sha256sum -c checksums.txt) && tar -tzf dist/nobreai-cockpit_v0.0.0-teste_darwin_arm64.tar.gz && d=$(mktemp -d) && tar -C $d -xzf dist/nobreai-cockpit_v0.0.0-teste_linux_amd64.tar.gz && $d/nobre version | head -1`
Expected: 4 `.tar.gz` + `checksums.txt`; `sha256sum -c` com 4 `OK`; o tar lista exatamente `nobre` e `nobred`; a última linha é `nobre v0.0.0-teste`.

- [ ] **Step 3: commit** `feat: make dist empacota os binários de macOS e Linux (NAC-52)`

---

### Task 3: `install.sh` e o teste dele

**Files:**
- Create: `install.sh` (raiz, executável)
- Create: `scripts/test-install.sh` (bash, executável)
- Modify: `Makefile` (alvo `test-install: ## testa o install.sh com binários falsos`; `test` passa a rodá-lo no fim)

**Interfaces:**
- Consumes: formato de pacote/checksums da Task 2; `nobre version` (Task 1) para a linha final.
- Produces — variáveis de ambiente do `install.sh` (todas opcionais):
  - `NOBRE_VERSION` (ex.: `v0.1.0`; padrão: última release)
  - `NOBRE_BINDIR` (padrão `$HOME/.local/bin`)
  - `NOBRE_REPO` (padrão `jacubovskib/nobreai-cockpit`)
  - `NOBRE_RELEASE_DIR` — diretório local com os pacotes e o `checksums.txt`; quando definido, nada é baixado e `NOBRE_VERSION` é obrigatório (usado pelo CI e pelo teste)
  - `NOBRE_ADDR` (padrão `127.0.0.1:7420`)
  - `NOBRE_UNAME_S` / `NOBRE_UNAME_M` — substituem `uname -s` / `uname -m` (só para teste)
  - `NOBRE_HEALTH_TRIES` (padrão 20, de 0,5 s) e `NOBRE_SKIP_SETUP=1` (pula os passos 7–9) — só para teste

Comportamento (ordem do spec):
1. Mapeia `Darwin→darwin`, `Linux→linux`; `arm64|aarch64→arm64`, `x86_64|amd64→amd64`. Outro → `Sistema <s> <m> sem pacote pronto. Pacotes: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64.` e sai 1.
2. Versão: `NOBRE_VERSION` ou, baixando, a tag tirada de `curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/$REPO/releases/latest` (último segmento depois de `/tag/`). Vazio → erro `Não achei a última versão em github.com/<repo>/releases.`
3. Diretório temporário (`mktemp -d`, removido por `trap ... EXIT`). Copia de `NOBRE_RELEASE_DIR` ou baixa de `https://github.com/$REPO/releases/download/$VER/<arquivo>` o pacote e o `checksums.txt`. Falha → `Não consegui obter <arquivo> da versão <ver>.` e sai 1.
4. Confere: a linha do pacote no `checksums.txt` tem de existir e bater com `sha256sum`/`shasum -a 256` do arquivo; senão `O pacote baixado não confere com o checksums.txt; nada foi instalado.` e sai 1.
5. Extrai no temporário; `nobred` estava respondendo? (`curl -fsS -o /dev/null http://$ADDR/api/health`) → guarda em `ERA_ATUALIZACAO`. Instala os dois com `mkdir -p` + `cp` + `chmod 755` em `BINDIR` (cópia para arquivo temporário no mesmo diretório e `mv`, para não quebrar um binário em uso).
6. PATH: se `BINDIR` não está no `$PATH` atual, escolhe o perfil por `basename "${SHELL:-sh}"` (`zsh→~/.zprofile`; `bash→~/.bash_profile` no darwin, `~/.bashrc` no linux; outro → `~/.profile`) e, se a linha marcadora `# nobreai-cockpit: PATH` não está nele, acrescenta essa linha e `export PATH="<BINDIR>:$PATH"`. Diz qual arquivo mudou e que vale para terminais novos.
7. `"$BINDIR/nobre" setup </dev/null`; código ≠ 0 → `O nobre setup terminou com pendências (veja acima).` e sai 1.
8. Se `ERA_ATUALIZACAO`: reinicia (darwin `launchctl kickstart -k gui/$(id -u)/io.nobretech.nobred`; linux `systemctl --user restart nobred`) e avisa `Serviço reiniciado com a versão nova (terminais abertos pela plataforma foram encerrados).`
9. Espera o health até 10 s (20 × 0,5 s); se não vier → `A API não respondeu em http://<addr>. Veja o log: <tail -n 50 ~/Library/Logs/nobred.log | journalctl --user -u nobred -n 50>` e sai 1.
10. Final: `"$BINDIR/nobre" version`; `Interface: http://<addr> (ou rode nobre ui)`; kiro-cli (`command -v kiro-cli` ou `~/.local/bin/kiro-cli`) encontrado → `Kiro: hooks instalados.`; senão `Kiro não encontrado: instale o kiro-cli e rode nobre setup de novo.`; e `Próximo passo: dentro do repositório, rode nobre project add.`

- [ ] **Step 1: o teste que falha — `scripts/test-install.sh`**

Estrutura: para cada caso, um `HOME` temporário, um `NOBRE_RELEASE_DIR` temporário com pacotes gerados na hora a partir de binários falsos, e um diretório `fakebin` no começo do `PATH` com `curl`, `launchctl` e `systemctl` falsos. Os falsos gravam cada chamada em `$LOG` (`echo "<nome> $*" >> $LOG`). O `nobre` falso grava `nobre $*` e, se conseguir ler uma linha do stdin (`if read -r _x; then echo stdin-com-dados >> $LOG; fi`), marca que o stdin não era `/dev/null`; o teste roda o instalador como `printf 'dados\n' | sh install.sh`, imitando o `curl | sh`. `nobre version` falso imprime `nobre vT`. O `curl` falso responde ao `/api/health` com sucesso ou falha conforme um arquivo-bandeira `$STATE/daemon_up`, que o `launchctl`/`systemctl`/`nobre setup` falsos criam. Rode o `install.sh` com `sh` (dash no Linux). Uma função `caso <nome>` e `assert_contains`/`assert_not_contains`/`assert_exit`; no fim, `N casos, M falhas` e código ≠ 0 se houver falha.

Casos (nomes exatos):
- `instalacao_nova_darwin`: `NOBRE_UNAME_S=Darwin NOBRE_UNAME_M=arm64 SHELL=/bin/zsh`, daemon fora antes → sai 0; `BINDIR/nobre` e `BINDIR/nobred` existem e são executáveis; `~/.zprofile` contém `# nobreai-cockpit: PATH` uma vez; `$LOG` contém `nobre setup` e não contém `stdin-com-dados`; `$LOG` não contém `kickstart`; saída contém `Próximo passo`.
- `reexecucao`: rodar o caso anterior duas vezes no mesmo `HOME` (na 2ª o daemon já está de pé) → `~/.zprofile` com a marcadora exatamente 1 vez; `$LOG` contém `launchctl kickstart -k gui/` uma vez; saída contém `Serviço reiniciado`.
- `linux_bash`: `NOBRE_UNAME_S=Linux NOBRE_UNAME_M=x86_64 SHELL=/bin/bash`, daemon de pé → `~/.bashrc` com a marcadora; `$LOG` contém `systemctl --user restart nobred`.
- `bindir_no_path`: `PATH` já contém `BINDIR` → nenhum arquivo de perfil criado.
- `checksum_errado`: `checksums.txt` com hash trocado → sai ≠ 0; saída contém `não confere`; `BINDIR/nobre` não existe.
- `pacote_ausente`: `NOBRE_VERSION=v9.9.9` sem esse pacote no diretório → sai ≠ 0; saída contém `Não consegui obter`; `BINDIR/nobre` não existe.
- `arquitetura_sem_pacote`: `NOBRE_UNAME_S=Linux NOBRE_UNAME_M=armv7l` → sai ≠ 0; saída contém `sem pacote pronto` e `darwin/arm64`.
- `setup_falha`: `nobre` falso sai 3 no `setup` → sai ≠ 0; saída contém `pendências`.
- `api_nao_sobe`: `nobre setup` falso não cria `daemon_up` → sai ≠ 0; saída contém `Veja o log`.
- `sem_kiro`: sem `kiro-cli` no `PATH` nem em `~/.local/bin` → saída contém `Kiro não encontrado`; com um `kiro-cli` falso em `fakebin` → `Kiro: hooks instalados`.

No `api_nao_sobe`, use `NOBRE_HEALTH_TRIES=2` para não esperar 10 s.

- [ ] **Step 2:** `bash scripts/test-install.sh` → FALHA (sem `install.sh`).

- [ ] **Step 3:** escrever `install.sh` conforme o comportamento acima. Cabeçalho com uso em uma linha (`curl -fsSL https://raw.githubusercontent.com/jacubovskib/nobreai-cockpit/main/install.sh | sh`) e as variáveis. Corpo inteiro numa função `main` chamada na última linha (um download interrompido do script não executa pela metade).

- [ ] **Step 4:** `bash scripts/test-install.sh` → todos os casos passam; `dash -n install.sh` sem erro; `make test-install` passa.

- [ ] **Step 5: caso com o pacote real**

Acrescente ao `test-install.sh` o caso `pacote_real`, que só roda quando `dist/checksums.txt` existe: usa `NOBRE_RELEASE_DIR=dist`, a versão tirada do nome do pacote, `NOBRE_SKIP_SETUP=1` e o `uname` real, e confere que `BINDIR/nobre version` começa com `nobre <versão do pacote>`. Não rode o `install.sh` completo nesta máquina: o `nobre setup` mexeria no serviço real; a ponta a ponta é a do CI do macOS (Task 4).

Run: `make dist VERSION=v0.0.0-teste && bash scripts/test-install.sh`
Expected: todos os casos, inclusive `pacote_real`, passam.

- [ ] **Step 6: commit** `feat: install.sh instala e atualiza o Cockpit compilado (NAC-52)`

---

### Task 4: workflows (release e instalador no CI do macOS)

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `.github/workflows/macos.yml` (passos novos no fim, antes de `log do nobred`)

**Interfaces:**
- Consumes: `make web`, `make test`, `make dist VERSION=...` (Tasks 1–3), `install.sh` com `NOBRE_RELEASE_DIR`/`NOBRE_VERSION` (Task 3), `nobre version` (Task 1).

- [ ] **Step 1: `release.yml`**

`on: push: tags: ["v*"]`; `permissions: contents: write`; um job `ubuntu-latest`, `timeout-minutes: 30`; mesmos passos de setup do `macos.yml` (checkout, setup-go com `go-version-file: go.mod`, pnpm 11, node 24 com cache do `web/pnpm-lock.yaml`); depois `make web`, `make test`, `make dist VERSION=${GITHUB_REF_NAME}`, e `gh release create "$GITHUB_REF_NAME" dist/*.tar.gz dist/checksums.txt --verify-tag --generate-notes` com `GH_TOKEN: ${{ github.token }}`.

- [ ] **Step 2: passos novos no `macos.yml`** (depois de `nobre setup --remover`):
  - `teste do install.sh`: `bash scripts/test-install.sh`.
  - `install.sh com pacotes locais`: `make dist VERSION=v0.0.0-ci`; `NOBRE_RELEASE_DIR=dist NOBRE_VERSION=v0.0.0-ci sh install.sh </dev/null | tee inst.txt`; `grep -q 'nobre v0.0.0-ci' inst.txt`; `grep -q 'nobred v0.0.0-ci' inst.txt`; `plutil -lint "$HOME/Library/LaunchAgents/io.nobretech.nobred.plist"`; `grep -q 'nobreai-cockpit: PATH' ~/.zprofile || echo "$PATH" | grep -q "$HOME/.local/bin"`.
  - `install.sh de novo atualiza`: guarda o PID (mesmo `awk` dos passos existentes), roda o `install.sh` de novo, confere PID novo diferente do anterior e `grep -q 'Serviço reiniciado'`.
  - `remover depois do install.sh`: `"$HOME/.local/bin/nobre" setup --remover` e as mesmas checagens do passo `nobre setup --remover`.

- [ ] **Step 3: verificar a sintaxe**

Run: `python3 -c "import yaml,sys;[yaml.safe_load(open(f)) for f in sys.argv[1:]]" .github/workflows/release.yml .github/workflows/macos.yml && echo ok`
Expected: `ok`. (A execução real vem com o push do branch: o `macos.yml` roda em `pull_request`/`push` do `main`; o `release.yml` só na tag.)

- [ ] **Step 4: commit** `ci: release na tag e install.sh testado no macOS (NAC-52)`

---

### Task 5: README e `make check`

**Files:**
- Modify: `README.md` (estado atual, Requisitos, Instalação, Atualizar, Desinstalar)
- Modify: `Makefile:45-64` (`check`)

- [ ] **Step 1: `make check`** — trocar o teste do `claude` por: `claude` ou `kiro-cli` (no PATH ou em `~/.local/bin`) → `✓ agente     <caminhos achados>`; nenhum → `! nenhum agente encontrado (instale o Claude Code ou o kiro-cli)` e `ok=0`. Texto do `##` do alvo passa a dizer `claude ou kiro-cli`.

Verificar: `make check` nesta máquina continua terminando em `Tudo certo`; `PATH=/usr/bin:/bin:$(dirname $(command -v go)):$(dirname $(command -v node)):$(dirname $(command -v pnpm)) make check` sem claude/kiro mostra `! nenhum agente encontrado`. (O `~/.local/bin/kiro-cli` desta máquina, se existir, faz o segundo comando mostrar ✓ — nesse caso confira só a linha do agente.)

- [ ] **Step 2: README**
  - Bloco "Estado atual": piloto funcional com **Claude Code** e **Kiro (kiro-cli 3.1+)**, em Linux com systemd e macOS; tira "Kiro" da lista do que não existe.
  - Requisitos: dividir em "Para instalar a versão pronta" (macOS ou Linux com systemd, `curl`, Claude Code ou kiro-cli) e "Para compilar do código" (o que existe hoje).
  - Instalação: começa por `curl -fsSL https://raw.githubusercontent.com/jacubovskib/nobreai-cockpit/main/install.sh | sh`, uma lista curta do que o script faz (passos 1–10 do Task 3 resumidos), as variáveis `NOBRE_VERSION` e `NOBRE_BINDIR`, e que o kiro-cli deve estar instalado antes (ou rode `nobre setup` depois). A instalação atual por `make setup` vira a subseção "Compilar do código".
  - Atualizar: rodar o mesmo comando de novo (quem compila: `git pull && make update`); `nobre version` confere.
  - Desinstalar: para quem instalou pelo script, `nobre setup --remover` e `rm ~/.local/bin/nobre ~/.local/bin/nobred` (o banco fica).
  - Desenvolvimento: como publicar uma versão (`git tag v0.1.0 && git push origin v0.1.0`; o CI publica a release).

- [ ] **Step 3: commit** `docs: instalação de um comando no README e make check com Claude ou Kiro (NAC-52)`

---

## Depois das tarefas (fluxo do Cockpit)

Revisão final do branch inteiro, uma onda de correções, merge `--no-ff` no `main`, `make test` no merge, push (o CI do macOS roda o instalador de verdade). Só então, **com confirmação do usuário**, `git tag v0.1.0 && git push origin v0.1.0` e conferir a release publicada (4 pacotes + `checksums.txt`). O usuário roda o `curl … | sh` no Mac.
