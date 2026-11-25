#!/usr/bin/env bash
# Smoke test de ponta a ponta do ciclo de uma sessão contra um nobred no ar
# (NOBRE_ADDR): um agente falso, instalado como o Claude Code nativo
# (versions/<v> atrás do link "claude"), roda o hook SessionStart e
# `nobre start`/`note`/`status` pelo shell. Confere que o nobre achou o PID do
# agente (o card fica com a sessão viva e esse PID) e que, quando o agente
# sai, o reconciliador encerra a sessão.
#
# Grava um projeto, um card e uma sessão de teste no banco do nobred: use com
# um daemon de teste (CI ou `nobred --db` temporário), não com o seu banco.
#
# Uso: NOBRE_ADDR=127.0.0.1:7499 scripts/smoke/agent.sh <dir com o nobre>
set -euo pipefail

BIN_DIR=${1:?uso: agent.sh <diretório com o binário nobre>}
ADDR=${NOBRE_ADDR:-127.0.0.1:7420}
export NOBRE_ADDR=$ADDR
export PATH="$BIN_DIR:$PATH"
ROOT=$(cd "$(dirname "$0")/../.." && pwd -P)
T=$(cd "$(mktemp -d)" && pwd -P)
AGENT=""
cleanup() {
  [ -n "$AGENT" ] && kill "$AGENT" 2>/dev/null || true
  exec 3>&- 2>/dev/null || true
}
trap cleanup EXIT

say() { printf '\n== %s\n' "$*"; }
fail() { printf '\nFALHOU: %s\n' "$*" >&2; [ -f "$T/agent.out" ] && sed 's/^/  agente| /' "$T/agent.out" >&2; exit 1; }
api() { curl -fsS "http://$ADDR$1"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); $1"; }

say "daemon em http://$ADDR"
api /api/health || fail "o nobred não responde em $ADDR"

say "projeto de teste"
git init -q "$T/repo"
git -C "$T/repo" -c user.name=smoke -c user.email=smoke@example.invalid commit -q --allow-empty -m init
nobre project add "$T/repo"

say "agente falso: versions/9.9.9 atrás do link claude"
mkdir -p "$T/claude/versions" "$T/claude/bin"
(cd "$ROOT" && go build -o "$T/claude/versions/9.9.9" ./scripts/smoke/fakeagent)
ln -s "$T/claude/versions/9.9.9" "$T/claude/bin/claude"
SID=$(python3 -c 'import uuid; print(uuid.uuid4())')
mkfifo "$T/in"
(cd "$T/repo" && exec "$T/claude/bin/claude" "$SID" \
  'nobre start "Smoke macOS" --goal "rodar no macOS" --done-when "smoke passa"' \
  'nobre note "smoke ok"' \
  'nobre status' <"$T/in" >"$T/agent.out" 2>&1) &
AGENT=$!
exec 3>"$T/in" # mantém o stdin do agente aberto: ele vive até fecharmos
for _ in $(seq 1 60); do
  grep -q '^ready$' "$T/agent.out" 2>/dev/null && break
  kill -0 "$AGENT" 2>/dev/null || fail "o agente saiu antes de ficar pronto"
  sleep 0.5
done
grep -q '^ready$' "$T/agent.out" || fail "o agente não ficou pronto em 30 s"
AGENT_PID=$(sed -n 's/^agent_pid=//p' "$T/agent.out")
sed 's/^/  agente| /' "$T/agent.out"

say "card com a sessão viva do agente (PID $AGENT_PID)"
KEY=$(api /api/cards | json 'print(next(c["key"] for c in d if c["title"] == "Smoke macOS"))') || fail "o nobre start não criou o card"
api "/api/cards/$KEY" | json "
live = d['live']
assert live, 'card sem sessão viva: o nobre não achou o PID do agente'
assert live['pid'] == $AGENT_PID, 'sessão viva com PID %s, agente é $AGENT_PID' % live['pid']
assert live['id'].endswith('$SID'), 'sessão viva %s não é a do agente' % live['id']
assert d['where_stopped'] == 'smoke ok', 'nobre note não gravou: %r' % d['where_stopped']
print('  %s: sessão %s viva, PID %s' % (d['key'], live['id'], live['pid']))
" || fail "card $KEY sem a sessão do agente"

say "agente sai: o reconciliador encerra a sessão (até 90 s)"
exec 3>&-
for _ in $(seq 1 90); do
  if api "/api/cards/$KEY" | json 'sys.exit(0 if d["live"] is None else 1)'; then
    echo "  sessão encerrada"
    say "OK"
    exit 0
  fi
  sleep 1
done
fail "a sessão continuou viva 90 s depois de o agente sair"
