#!/bin/sh
# Instala ou atualiza o NobreAI Cockpit compilado (nobre e nobred).
# Uso: curl -fsSL https://raw.githubusercontent.com/jacubovskib/nobreai-cockpit/main/install.sh | sh
#
# Variáveis (todas opcionais):
#   NOBRE_VERSION      versão a instalar (ex.: v0.1.0); padrão: a última release
#   NOBRE_BINDIR       onde instalar; padrão: $HOME/.local/bin
#   NOBRE_REPO         repositório no GitHub; padrão: jacubovskib/nobreai-cockpit
#   NOBRE_RELEASE_DIR  diretório local com os pacotes e o checksums.txt; nada é
#                      baixado e NOBRE_VERSION passa a ser obrigatória
#   NOBRE_ADDR         endereço do nobred; padrão: 127.0.0.1:7420
# Só para teste:
#   NOBRE_UNAME_S, NOBRE_UNAME_M  substituem uname -s e uname -m
#   NOBRE_HEALTH_TRIES            tentativas de 0,5 s no health; padrão: 20
#   NOBRE_SKIP_SETUP=1            pula o nobre setup, o reinício e a espera
#
# Tudo fica dentro de main, chamada na última linha: um download interrompido
# do script não roda pela metade.

set -eu

say() { printf '%s\n' "$*"; }
die() {
	printf '%s\n' "$*" >&2
	exit 1
}

# Responde 0 se o nobred atende em http://$ADDR/api/health.
health() { curl -fsS -o /dev/null "http://$ADDR/api/health" >/dev/null 2>&1; }

# Espera o health por até TRIES × 0,5 s. Se responder, guarda em RUN_VER a
# versão informada (vazia se o daemon não informa) e responde 0.
espera_health() {
	i=0
	while [ "$i" -lt "$TRIES" ]; do
		if BODY=$(curl -fsS "http://$ADDR/api/health" 2>/dev/null); then
			RUN_VER=$(printf '%s\n' "$BODY" |
				sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
			return 0
		fi
		sleep 0.5
		i=$((i + 1))
	done
	return 1
}

# Falha com a dica de onde ver o log do serviço.
sem_api() {
	if [ "$OS" = darwin ]; then
		LOGCMD="tail -n 50 ~/Library/Logs/nobred.log"
	else
		LOGCMD="journalctl --user -u nobred -n 50"
	fi
	die "A API não respondeu em http://$ADDR. Veja o log: $LOGCMD"
}

# Hash sha256 de um arquivo (Linux: sha256sum; macOS: shasum).
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		die "Não achei sha256sum nem shasum para conferir o pacote."
	fi
}

# Copia (NOBRE_RELEASE_DIR) ou baixa um arquivo da release para $TMP.
obtem() { # <arquivo>
	if [ -n "$RELEASE_DIR" ]; then
		[ -f "$RELEASE_DIR/$1" ] && cp "$RELEASE_DIR/$1" "$TMP/$1" && return 0
	else
		curl -fsSL -o "$TMP/$1" "https://github.com/$REPO/releases/download/$VER/$1" && return 0
	fi
	die "Não consegui obter $1 da versão $VER."
}

# Copia um binário para BINDIR sem quebrar uma cópia em uso: grava ao lado
# e troca com mv.
instala_bin() { # <nome>
	cp "$TMP/x/$1" "$BINDIR/.$1.novo.$$"
	chmod 755 "$BINDIR/.$1.novo.$$"
	mv -f "$BINDIR/.$1.novo.$$" "$BINDIR/$1"
}

main() {
	REPO=${NOBRE_REPO:-jacubovskib/nobreai-cockpit}
	BINDIR=${NOBRE_BINDIR:-$HOME/.local/bin}
	ADDR=${NOBRE_ADDR:-127.0.0.1:7420}
	RELEASE_DIR=${NOBRE_RELEASE_DIR:-}
	VER=${NOBRE_VERSION:-}
	TRIES=${NOBRE_HEALTH_TRIES:-20}

	# 1. Sistema e arquitetura.
	S=${NOBRE_UNAME_S:-$(uname -s)}
	M=${NOBRE_UNAME_M:-$(uname -m)}
	case "$S" in
	Darwin) OS=darwin ;;
	Linux) OS=linux ;;
	*) OS= ;;
	esac
	case "$M" in
	arm64 | aarch64) ARCH=arm64 ;;
	x86_64 | amd64) ARCH=amd64 ;;
	*) ARCH= ;;
	esac
	if [ -z "$OS" ] || [ -z "$ARCH" ]; then
		die "Sistema $S $M sem pacote pronto. Pacotes: darwin/arm64, darwin/amd64, linux/amd64, linux/arm64."
	fi

	# 2. Versão.
	if [ -z "$VER" ]; then
		[ -z "$RELEASE_DIR" ] || die "NOBRE_RELEASE_DIR exige NOBRE_VERSION."
		URL=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null) || URL=
		case "$URL" in
		*/tag/*) VER=${URL##*/tag/} ;;
		esac
		[ -n "$VER" ] || die "Não achei a última versão em github.com/$REPO/releases."
	fi
	PKG="nobreai-cockpit_${VER}_${OS}_${ARCH}.tar.gz"
	say "Instalando o NobreAI Cockpit $VER ($OS/$ARCH) em $BINDIR."

	# 3. Pacote e checksums num diretório temporário.
	TMP=$(mktemp -d)
	trap 'rm -rf "$TMP"' EXIT
	trap 'exit 1' HUP INT TERM
	obtem "$PKG"
	obtem checksums.txt

	# 4. Confere o sha256.
	WANT=$(awk -v f="$PKG" '$2 == f || $2 == "*" f { print $1; exit }' "$TMP/checksums.txt")
	GOT=$(sha256 "$TMP/$PKG")
	if [ -z "$WANT" ] || [ "$WANT" != "$GOT" ]; then
		die "O pacote baixado não confere com o checksums.txt; nada foi instalado."
	fi

	# 5. Extrai e instala.
	mkdir "$TMP/x"
	tar -xzf "$TMP/$PKG" -C "$TMP/x"
	[ -f "$TMP/x/nobre" ] && [ -f "$TMP/x/nobred" ] ||
		die "O pacote $PKG não traz nobre e nobred; nada foi instalado."
	ERA_ATUALIZACAO=0
	if health; then ERA_ATUALIZACAO=1; fi
	mkdir -p "$BINDIR"
	instala_bin nobre
	instala_bin nobred
	say "Instalados nobre e nobred em $BINDIR."

	# 6. PATH do shell de login.
	case ":$PATH:" in
	*":$BINDIR:"*) ;;
	*)
		case "$(basename "${SHELL:-sh}")" in
		zsh) PROFILE="$HOME/.zprofile" ;;
		bash)
			if [ "$OS" = darwin ]; then PROFILE="$HOME/.bash_profile"; else PROFILE="$HOME/.bashrc"; fi
			;;
		*) PROFILE="$HOME/.profile" ;;
		esac
		if [ -f "$PROFILE" ] && grep -qxF '# nobreai-cockpit: PATH' "$PROFILE"; then
			say "O PATH já inclui $BINDIR em $PROFILE (vale para terminais novos)."
		else
			# Garante que a linha nova não grude na última linha do arquivo.
			if [ -s "$PROFILE" ] && [ -n "$(tail -c 1 "$PROFILE")" ]; then
				printf '\n' >>"$PROFILE"
			fi
			# shellcheck disable=SC2016 # o $PATH fica literal no perfil
			printf '# nobreai-cockpit: PATH\nexport PATH="%s:$PATH"\n' "$BINDIR" >>"$PROFILE"
			say "Acrescentei $BINDIR ao PATH em $PROFILE (vale para terminais novos)."
		fi
		PATH="$BINDIR:$PATH"
		export PATH
		;;
	esac

	if [ "${NOBRE_SKIP_SETUP:-}" != 1 ]; then
		# 7. Serviço e hooks. O stdin é o próprio script (curl | sh): /dev/null
		# evita a pergunta interativa.
		if ! "$BINDIR/nobre" setup </dev/null; then
			die "O nobre setup terminou com pendências (veja acima)."
		fi

		# 8. Atualização: se o daemon que já rodava não está na versão nova,
		# reinicia. O setup às vezes já reinicia sozinho (quando a unit ou o
		# plist mudam); aí a versão bate e não reinicia de novo.
		RUN_VER=
		if espera_health; then OK=1; else OK=0; fi
		if [ "$ERA_ATUALIZACAO" = 1 ] && [ "$RUN_VER" != "$VER" ]; then
			if [ "$OS" = darwin ]; then
				launchctl kickstart -k "gui/$(id -u)/io.nobretech.nobred" </dev/null ||
					die "Não consegui reiniciar o serviço (launchctl kickstart)."
			else
				systemctl --user restart nobred </dev/null ||
					die "Não consegui reiniciar o serviço (systemctl --user restart nobred)."
			fi
			say "Serviço reiniciado com a versão nova (terminais abertos pela plataforma foram encerrados)."
			# 9. Espera a API de novo.
			if espera_health; then OK=1; else OK=0; fi
		fi
		[ "$OK" = 1 ] || sem_api
	fi

	# 10. Resumo.
	say ""
	"$BINDIR/nobre" version </dev/null || true
	say "Interface: http://$ADDR (ou rode nobre ui)"
	if command -v kiro-cli >/dev/null 2>&1 || [ -x "$HOME/.local/bin/kiro-cli" ]; then
		say "Kiro: hooks instalados."
	else
		say "Kiro não encontrado: instale o kiro-cli e rode nobre setup de novo."
	fi
	say "Próximo passo: dentro do repositório, rode nobre project add."
}

main "$@"
