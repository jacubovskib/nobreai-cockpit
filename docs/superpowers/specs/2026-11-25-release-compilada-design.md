# Release compilada e instalador de um comando (NAC-52)

## Por quê

Hoje instalar o Cockpit exige clonar o repositório e ter Go 1.26.5, Node e
pnpm na máquina (`make setup` compila tudo). O usuário quer instalar num Mac
só para **usar** o Cockpit com o kiro-cli, sem mexer no código: um comando que
baixa a versão pronta, instala, configura e deixa o ambiente funcionando.

## Objetivos

- Uma tag `v*` no GitHub gera, pelo CI, uma release com os binários prontos.
- No Mac (e no Linux), um comando instala ou atualiza tudo:
  `curl -fsSL https://raw.githubusercontent.com/jacubovskib/nobreai-cockpit/main/install.sh | sh`
- `nobre version` diz qual versão está instalada e rodando.

## Fora do escopo

- Instalar o kiro-cli, o Claude Code, o Homebrew ou qualquer ferramenta de
  desenvolvimento. O kiro-cli tem instalador e login próprios: o script só
  avisa se ele faltar.
- Assinatura/notarização da Apple. Binário baixado por `curl` não recebe o
  atributo de quarentena, e o linker do Go já faz a assinatura ad-hoc que o
  Apple Silicon exige.
- Homebrew tap, pacotes .deb/.pkg, atualização automática.
- Mudar o fluxo de quem compila do código: `make setup`/`make update` continuam
  iguais (ganham só a versão embutida).

## Anti-objetivos

- O script não pode deixar a máquina meio instalada sem dizer: qualquer passo
  que falhe para com mensagem clara.
- Rodar o script de novo nunca pode apagar o banco nem duplicar hooks.

## Abordagens consideradas

1. **CI do GitHub dispara na tag (escolhida pelo usuário).** Workflow novo
   compila a web uma vez e os binários por cross-compile num runner Linux;
   publica a release. Versão sai de ambiente limpo e do commit exato da tag.
2. `make release` local publicando com `gh`: sem CI, mas a versão depende do
   estado da máquina de quem publica.
3. GoReleaser: mesmo resultado da 1 com mais uma ferramenta para manter, sem
   ganho real com dois binários.

## Como fica

### Versão embutida

- `nobre` e `nobred` ganham uma variável de versão preenchida por
  `-ldflags -X` (no CI: a tag; no `make build`: `git describe --tags --always
  --dirty`; sem nada: `dev`).
- `nobre version` imprime a versão do CLI e, se o daemon responder, a do
  `nobred` (o `/api/health` passa a informar a versão). Versões diferentes →
  uma linha avisando para reiniciar o serviço.

### Workflow de release (`.github/workflows/release.yml`)

- Dispara em push de tag `v*`.
- Roda `go vet` e `go test ./...` antes de publicar (não publica release
  quebrada).
- Compila `web/dist` uma vez (pnpm) e depois, com `CGO_ENABLED=0`,
  `nobre` e `nobred` para `darwin/arm64`, `darwin/amd64`, `linux/amd64` e
  `linux/arm64` (o SQLite é o modernc, puro Go).
- Um pacote por alvo: `nobreai-cockpit_<versão>_<os>_<arch>.tar.gz` com os dois
  binários, mais `checksums.txt` (sha256).
- Publica a release da tag com `gh release create` e as notas geradas
  automaticamente.

### Instalador (`install.sh` na raiz do repositório)

POSIX `sh` (roda por `curl | sh` no zsh do macOS e no bash do Linux). Passos:

1. Detecta sistema (`Darwin`/`Linux`) e arquitetura (`arm64`/`aarch64`,
   `x86_64`); combinação sem pacote → erro claro.
2. Escolhe a versão: `NOBRE_VERSION=v0.1.0` se definida, senão a última
   release (`/releases/latest`).
3. Baixa o pacote e o `checksums.txt` para um diretório temporário, confere o
   sha256 (`shasum -a 256` no macOS, `sha256sum` no Linux) e só então extrai.
4. Instala os dois binários em `${NOBRE_BINDIR:-$HOME/.local/bin}`.
5. Garante o diretório no PATH do shell de login: se faltar, acrescenta uma
   linha marcada ao `~/.zprofile` (zsh) ou `~/.profile`/`~/.bashrc` (bash),
   uma vez só. Necessário porque o agente (Kiro ou Claude) roda `nobre start`
   pelo shell.
6. Roda `nobre setup </dev/null` (sem pergunta interativa: stdin é o próprio
   script). Isso cria o serviço (launchd/systemd) e instala hooks do Claude e,
   se o kiro-cli estiver presente, do Kiro.
7. Se já havia um `nobred` rodando antes (atualização), reinicia o serviço
   (`launchctl kickstart -k` / `systemctl --user restart`) e avisa que os
   terminais abertos pela plataforma são encerrados.
8. Espera o `/api/health` responder e imprime: versão instalada, endereço da
   interface, se o kiro-cli foi encontrado (e, se não, como instalá-lo depois
   e rodar `nobre setup` de novo) e o próximo passo (`nobre ui`, cadastrar o
   projeto com `nobre project add` dentro do repositório).

### Documentação e verificação

- README: seção de instalação começa pelo comando de uma linha; compilar do
  código vira a segunda opção. Corrige a frase que diz que o adaptador do Kiro
  não existe e deixa o Claude Code como opcional (Claude ou Kiro).
- `make check` deixa de exigir o `claude`: basta um dos dois agentes.
- CI do macOS ganha um passo que roda o `install.sh` contra pacotes locais
  (variável para apontar o script a um diretório em vez do GitHub), para
  testar o instalador sem depender de uma release publicada.

## Decisões minhas (o usuário pode corrigir)

- Linux entra junto (custa pouco e o script serve para os dois).
- Versão inicial `v0.1.0`; a tag é criada e enviada só depois do merge, com
  confirmação do usuário (é um passo público).
- O script não usa `sudo` em nenhum passo.

## Fica para quem implementar

- Nome exato da variável de versão e onde ela mora (pacote `internal/version`
  ou `main`).
- Formato das linhas de saída do script, desde que em pt-BR e curtas.
- Como o teste do instalador no CI fornece os pacotes locais.
