import { isLive, type TerminalInfo } from '../../api/terminals'

// SessionRef: o mínimo de uma sessão (ActiveSession da overview) para casar com um terminal.
export interface SessionRef {
  id: string
  pid: number
  started_at: string
  ended_at: string | null
  pid_dead_at: string | null
}

// sessionAlive: sessão sem fim e sem a morte do processo registrada.
export const sessionAlive = (s: Pick<SessionRef, 'ended_at' | 'pid_dead_at'>) => !s.ended_at && !s.pid_dead_at

// samePid: pid 0 (desconhecido) nunca casa.
export const samePid = (terminalPid: number, sessionPid: number) =>
  terminalPid > 0 && sessionPid > 0 && terminalPid === sessionPid

// terminalOfSession: o terminal (já filtrado por isLive) de mesmo pid da sessão.
export function terminalOfSession(s: Pick<SessionRef, 'pid'>, liveTerminals: readonly TerminalInfo[]): TerminalInfo | undefined {
  return liveTerminals.find((t) => samePid(t.pid, s.pid))
}

const startedMs = (s: SessionRef) => Date.parse(s.started_at) || 0

// sessionOfTerminal: a sessão viva de mesmo pid do terminal vivo; com mais de
// uma, a de started_at mais recente (empate: a última da lista). Sem atalho por card.
export function sessionOfTerminal<S extends SessionRef>(t: TerminalInfo, sessions: readonly S[]): S | undefined {
  if (!isLive(t)) return undefined
  let best: S | undefined
  for (const s of sessions) {
    if (!sessionAlive(s) || !samePid(t.pid, s.pid)) continue
    if (!best || startedMs(s) >= startedMs(best)) best = s
  }
  return best
}

// matchSessions: id do terminal → sessão; só os terminais casados.
export function matchSessions<S extends SessionRef>(terms: readonly TerminalInfo[], sessions: readonly S[]): Map<string, S> {
  const out = new Map<string, S>()
  for (const t of terms) {
    const s = sessionOfTerminal(t, sessions)
    if (s) out.set(t.id, s)
  }
  return out
}
