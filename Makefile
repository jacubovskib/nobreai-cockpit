# NobreAI Cockpit: build, instalação e configuração.
# `make` (ou `make help`) lista os alvos.

PREFIX     ?= $(HOME)/.local
BINDIR     ?= $(PREFIX)/bin
NOBRE_ADDR ?= 127.0.0.1:7420
DEMO_ADDR  ?= 127.0.0.1:7421

.DEFAULT_GOAL := help
.PHONY: help check web build install setup setup-dry-run update restart status logs demo dev test clean

help: ## mostra esta ajuda
	@echo "NobreAI Cockpit"
	@echo
	@echo "Primeira instalação:  make check && make setup"
	@echo "Atualizar:            git pull && make update"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-15s %s\n", $$1, $$2}'
	@echo
	@echo "Variáveis: PREFIX=$(PREFIX)  BINDIR=$(BINDIR)  NOBRE_ADDR=$(NOBRE_ADDR)"

check: ## confere as dependências (go, node, pnpm, systemd, claude, PATH)
	@ok=1; \
	for t in go node pnpm git curl systemctl; do \
	  if command -v $$t >/dev/null 2>&1; then printf '✓ %-10s %s\n' $$t "$$(command -v $$t)"; \
	  else printf '! %-10s não encontrado\n' $$t; ok=0; fi; \
	done; \
	want=$$(sed -n 's/^go //p' go.mod); \
	have=$$(go env GOVERSION 2>/dev/null | sed 's/^go//'); \
	if [ -n "$$have" ] && [ "$$(printf '%s\n%s\n' "$$want" "$$have" | sort -V | head -1)" != "$$want" ]; then \
	  echo "! go $$have é mais antigo que o exigido pelo go.mod ($$want)"; ok=0; \
	elif [ -n "$$have" ]; then echo "✓ go $$have (go.mod pede $$want)"; fi; \
	if systemctl --user show-environment >/dev/null 2>&1; then echo "✓ systemd do usuário disponível"; \
	else echo "! systemctl --user não responde (o nobred roda como serviço de usuário do systemd)"; ok=0; fi; \
	if command -v claude >/dev/null 2>&1; then echo "✓ claude     $$(command -v claude)"; \
	else echo "! claude não está no PATH deste terminal (rode o setup num terminal onde o claude funciona)"; ok=0; fi; \
	case ":$$PATH:" in *":$(BINDIR):"*) echo "✓ $(BINDIR) está no PATH";; \
	  *) echo "! $(BINDIR) não está no PATH (adicione ao seu ~/.zshrc ou ~/.bashrc)"; ok=0;; esac; \
	if [ $$ok = 1 ]; then echo; echo "Tudo certo. Próximo passo: make setup"; \
	else echo; echo "Resolva os itens com ! antes de seguir."; exit 1; fi

web: ## compila a interface web (web/dist, embutida no nobred)
	pnpm -C web install && pnpm -C web build

build: web ## compila ./nobre e ./nobred (com a interface embutida)
	go build -o nobre ./cmd/nobre
	go build -o nobred ./cmd/nobred

install: build ## instala nobre e nobred em BINDIR (não reinicia o daemon)
	install -d "$(BINDIR)"
	install -m755 nobre nobred "$(BINDIR)/"
	@echo "Instalado em $(BINDIR)."
	@if [ "$(filter update,$(MAKECMDGOALS))" = "" ] && systemctl --user is-active --quiet nobred 2>/dev/null; then \
	  echo "O nobred está rodando com a versão anterior. Rode 'make restart' para trocar."; fi

setup: install ## instala e configura: hooks, skill e status line do Claude Code e o serviço nobred
	"$(BINDIR)/nobre" setup $(SETUP_ARGS)

setup-dry-run: install ## mostra o que o setup mudaria, sem gravar nada
	"$(BINDIR)/nobre" setup --dry-run $(SETUP_ARGS)

update: install ## reinstala os binários e reinicia o daemon (fecha os terminais da plataforma)
	@$(MAKE) --no-print-directory restart

restart: ## reinicia o nobred (os terminais abertos na plataforma são encerrados)
	systemctl --user restart nobred
	@$(MAKE) --no-print-directory status

status: ## mostra o serviço e se a API responde
	@printf 'Serviço nobred: '; systemctl --user is-active nobred || true
	@systemctl --user show nobred --property=MainPID,ActiveEnterTimestamp,FragmentPath 2>/dev/null | sed 's/^/  /'
	@if curl -fsS "http://$(NOBRE_ADDR)/api/health" 2>/dev/null; then \
	  echo; echo "Interface: http://$(NOBRE_ADDR)"; \
	else echo "! a API não respondeu em http://$(NOBRE_ADDR)"; fi

logs: ## acompanha o log do nobred
	journalctl --user -u nobred -f

demo: build ## sobe um nobred com dados de demonstração em DEMO_ADDR (banco separado)
	@echo "Demonstração em http://$(DEMO_ADDR) (Ctrl+C para sair)"
	NOBRE_ADDR=$(DEMO_ADDR) ./nobred --seed-demo

# Desenvolvimento: nobred em :7420 e Vite (com proxy de /api) em :5173.
dev: ## explica como rodar daemon e interface em modo de desenvolvimento
	@echo "Rode 'go run ./cmd/nobred' em um terminal e 'pnpm -C web dev' em outro."

test: ## roda go vet, testes Go e testes da interface
	go vet ./... && go test ./... && pnpm -C web test --run

clean: ## apaga os binários e o build da interface
	rm -f nobre nobred
	find web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
