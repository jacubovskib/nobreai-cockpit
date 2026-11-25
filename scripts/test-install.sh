#!/usr/bin/env bash
# Testa o install.sh com binários e comandos falsos.
# Cada caso tem HOME, BINDIR e pacotes próprios num diretório temporário.
# curl, launchctl e systemctl são falsos: nada sai para a rede nem toca o
# serviço real. Roda no bash 3.2 do macOS; o install.sh roda com sh.
# Uso: bash scripts/test-install.sh

ROOT=$(cd "$(dirname "$0")/.." && pwd)
INSTALL="$ROOT/install.sh"
BASE=$(mktemp -d)
trap 'rm -rf "$BASE"' EXIT

N=0
FALHAS=0
CASO=""
CASO_OK=1

# Hash sha256 de um arquivo (Linux: sha256sum; macOS: shasum).
sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

falha() {
	printf '  FALHOU [%s] %s\n' "$CASO" "$1"
	CASO_OK=0
}

fim_caso() {
	if [ -n "$CASO" ]; then
		if [ "$CASO_OK" = 1 ]; then
			printf 'ok    %s\n' "$CASO"
		else
			printf 'FALHA %s\n' "$CASO"
			printf '%s\n' "$OUT" | sed 's/^/    | /'
			FALHAS=$((FALHAS + 1))
		fi
	fi
	CASO=""
}

assert_contains() { # <texto> <trecho> <descrição>
	case "$1" in
	*"$2"*) ;;
	*) falha "$3: falta \"$2\"" ;;
	esac
}

assert_not_contains() { # <texto> <trecho> <descrição>
	case "$1" in
	*"$2"*) falha "$3: não devia ter \"$2\"" ;;
	esac
}

assert_exit() { # <esperado: 0 | nao0>
	if [ "$1" = 0 ] && [ "$CODE" != 0 ]; then
		falha "saída $CODE, esperava 0"
	elif [ "$1" = nao0 ] && [ "$CODE" = 0 ]; then
		falha "saída 0, esperava diferente de 0"
	fi
}

assert_count() { # <arquivo> <trecho> <vezes>
	n=0
	if [ -f "$1" ]; then
		n=$(grep -cF -- "$2" "$1")
	fi
	if [ "$n" != "$3" ]; then
		falha "$(basename "$1"): \"$2\" aparece $n vez(es), esperava $3"
	fi
}

assert_file() { # <arquivo> -> existe e é executável
	if [ ! -x "$1" ]; then
		falha "$1 não existe ou não é executável"
	fi
}

assert_no_file() {
	if [ -e "$1" ]; then
		falha "$1 não devia existir"
	fi
}

# Binários falsos que vão dentro dos pacotes. O nobre grava as chamadas em
# $T_LOG; o setup marca stdin-com-dados se ler algo do stdin e cria
# $T_STATE/daemon_up (a menos que T_SETUP_SEM_DAEMON=1). T_SETUP_EXIT dá o
# código de saída do setup.
escreve_falsos() { # <dir>
	mkdir -p "$1"
	cat >"$1/nobre" <<'EOF'
#!/bin/sh
echo "nobre $*" >>"$T_LOG"
case "$1" in
setup)
	if read -r _x; then echo stdin-com-dados >>"$T_LOG"; fi
	if [ "${T_SETUP_SEM_DAEMON:-}" != 1 ]; then : >"$T_STATE/daemon_up"; fi
	exit "${T_SETUP_EXIT:-0}"
	;;
version)
	echo "nobre vT"
	echo "nobred vT"
	;;
esac
exit 0
EOF
	cat >"$1/nobred" <<'EOF'
#!/bin/sh
echo "nobred $*" >>"$T_LOG"
EOF
	chmod 755 "$1/nobre" "$1/nobred"
}

# Comandos falsos no começo do PATH.
escreve_fakebin() { # <dir>
	mkdir -p "$1"
	cat >"$1/curl" <<'EOF'
#!/bin/sh
echo "curl $*" >>"$T_LOG"
case "$*" in
*/api/health*) [ -f "$T_STATE/daemon_up" ] && exit 0; exit 7 ;;
esac
exit 22
EOF
	cat >"$1/launchctl" <<'EOF'
#!/bin/sh
echo "launchctl $*" >>"$T_LOG"
: >"$T_STATE/daemon_up"
EOF
	cat >"$1/systemctl" <<'EOF'
#!/bin/sh
echo "systemctl $*" >>"$T_LOG"
: >"$T_STATE/daemon_up"
EOF
	chmod 755 "$1/curl" "$1/launchctl" "$1/systemctl"
}

# Gera os quatro pacotes da versão vT e o checksums.txt.
escreve_release() { # <dir>
	mkdir -p "$1"
	escreve_falsos "$1/work"
	for t in darwin_arm64 darwin_amd64 linux_amd64 linux_arm64; do
		tar -C "$1/work" -czf "$1/nobreai-cockpit_vT_$t.tar.gz" nobre nobred
	done
	rm -rf "$1/work"
	(cd "$1" && for f in *.tar.gz; do printf '%s  %s\n' "$(sha256 "$f")" "$f"; done >checksums.txt)
}

# Começa um caso: diretórios novos e variáveis padrão (macOS arm64, zsh).
caso() {
	fim_caso
	CASO=$1
	CASO_OK=1
	N=$((N + 1))
	D="$BASE/$N"
	H="$D/home"
	BINDIR="$H/.local/bin"
	FAKEBIN="$D/fakebin"
	REL="$D/release"
	T_LOG="$D/log"
	T_STATE="$D/state"
	mkdir -p "$H" "$T_STATE"
	: >"$T_LOG"
	escreve_fakebin "$FAKEBIN"
	escreve_release "$REL"
	P="$FAKEBIN:/usr/bin:/bin:/usr/sbin:/sbin"
	OUT=""
	CODE=""
}

# Roda o install.sh com sh, num ambiente limpo, com stdin vindo de um pipe
# (como no curl | sh). Argumentos extras: VAR=valor que sobrescrevem o padrão.
roda() {
	OUT=$(printf 'dados\n' | env -i \
		HOME="$H" PATH="$P" SHELL=/bin/zsh \
		T_LOG="$T_LOG" T_STATE="$T_STATE" \
		NOBRE_BINDIR="$BINDIR" NOBRE_RELEASE_DIR="$REL" NOBRE_VERSION=vT \
		NOBRE_ADDR=127.0.0.1:1 NOBRE_UNAME_S=Darwin NOBRE_UNAME_M=arm64 \
		"$@" sh "$INSTALL" 2>&1)
	CODE=$?
}

log() { cat "$T_LOG"; }

# --- casos -----------------------------------------------------------------

caso instalacao_nova_darwin
roda
assert_exit 0
assert_file "$BINDIR/nobre"
assert_file "$BINDIR/nobred"
assert_count "$H/.zprofile" "# nobreai-cockpit: PATH" 1
assert_contains "$(log)" "nobre setup" "log"
assert_not_contains "$(log)" "stdin-com-dados" "log"
assert_not_contains "$(log)" "kickstart" "log"
assert_contains "$OUT" "Próximo passo" "saída"

caso reexecucao
roda
assert_exit 0
roda
assert_exit 0
assert_count "$H/.zprofile" "# nobreai-cockpit: PATH" 1
assert_count "$T_LOG" "launchctl kickstart -k gui/" 1
assert_contains "$OUT" "Serviço reiniciado" "saída"

caso linux_bash
: >"$T_STATE/daemon_up"
roda NOBRE_UNAME_S=Linux NOBRE_UNAME_M=x86_64 SHELL=/bin/bash
assert_exit 0
assert_count "$H/.bashrc" "# nobreai-cockpit: PATH" 1
assert_contains "$(log)" "systemctl --user restart nobred" "log"
assert_not_contains "$(log)" "launchctl" "log"

caso bindir_no_path
roda PATH="$BINDIR:$P"
assert_exit 0
for f in .zprofile .bash_profile .bashrc .profile; do
	assert_no_file "$H/$f"
done

caso checksum_errado
awk '{print "0000000000000000000000000000000000000000000000000000000000000000  " $2}' \
	"$REL/checksums.txt" >"$REL/c" && mv "$REL/c" "$REL/checksums.txt"
roda
assert_exit nao0
assert_contains "$OUT" "não confere" "saída"
assert_no_file "$BINDIR/nobre"

caso pacote_ausente
roda NOBRE_VERSION=v9.9.9
assert_exit nao0
assert_contains "$OUT" "Não consegui obter" "saída"
assert_no_file "$BINDIR/nobre"

caso arquitetura_sem_pacote
roda NOBRE_UNAME_S=Linux NOBRE_UNAME_M=armv7l
assert_exit nao0
assert_contains "$OUT" "sem pacote pronto" "saída"
assert_contains "$OUT" "darwin/arm64" "saída"

caso setup_falha
roda T_SETUP_EXIT=3
assert_exit nao0
assert_contains "$OUT" "pendências" "saída"

caso api_nao_sobe
roda T_SETUP_SEM_DAEMON=1 NOBRE_HEALTH_TRIES=2
assert_exit nao0
assert_contains "$OUT" "Veja o log" "saída"

caso sem_kiro
roda
assert_exit 0
assert_contains "$OUT" "Kiro não encontrado" "saída sem kiro"
printf '#!/bin/sh\nexit 0\n' >"$FAKEBIN/kiro-cli"
chmod 755 "$FAKEBIN/kiro-cli"
roda
assert_exit 0
assert_contains "$OUT" "Kiro: hooks instalados" "saída com kiro"

# Pacote de verdade (make dist): só instala, sem setup, e confere a versão.
if [ -f "$ROOT/dist/checksums.txt" ]; then
	caso pacote_real
	PKG=$(awk '{print $2}' "$ROOT/dist/checksums.txt" | head -n 1)
	VER=$(printf '%s\n' "$PKG" | sed 's/^nobreai-cockpit_\(.*\)_[a-z]*_[a-z0-9]*\.tar\.gz$/\1/')
	roda NOBRE_RELEASE_DIR="$ROOT/dist" NOBRE_VERSION="$VER" NOBRE_SKIP_SETUP=1 \
		NOBRE_UNAME_S= NOBRE_UNAME_M=
	assert_exit 0
	assert_not_contains "$(log)" "launchctl" "log"
	assert_not_contains "$(log)" "systemctl" "log"
	V=$(env -i HOME="$H" PATH="$P" NOBRE_ADDR=127.0.0.1:1 "$BINDIR/nobre" version </dev/null 2>&1)
	case "$V" in
	"nobre $VER"*) ;;
	*) falha "nobre version: \"$V\", esperava começar com \"nobre $VER\"" ;;
	esac
else
	printf 'pulado pacote_real (rode make dist antes)\n'
fi

fim_caso
printf '%s casos, %s falhas\n' "$N" "$FALHAS"
[ "$FALHAS" = 0 ]
