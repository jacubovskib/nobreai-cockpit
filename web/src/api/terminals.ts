import { useQuery } from '@tanstack/react-query'
import { get } from './client'

// Espelha pty.Info do daemon (snake_case).
export interface TerminalInfo {
  id: string
  card_key: string
  agent: string
  origin: string
  title: string
  pid: number
  started_at: string
  ended_at: string | null
  exit_code: number | null
  cols: number
  rows: number
  clients: number
  // end_reason: 'agent' | 'killed' | 'daemon' depois do fim.
  end_reason?: string
}

export const isLive = (t: TerminalInfo) => !t.ended_at

export const useTerminals = () =>
  useQuery({ queryKey: ['terminals'], queryFn: () => get<TerminalInfo[]>('/api/terminals'), retry: false })

const startedMs = (t: TerminalInfo) => Date.parse(t.started_at) || 0

// forCard: terminais do card, mais novo primeiro.
export const forCard = (list: TerminalInfo[] | undefined, key: string) =>
  (list ?? []).filter((t) => t.card_key === key).sort((a, b) => startedMs(b) - startedMs(a))

// wsUrl monta ws(s)://host/api/terminals/{id}/ws a partir de window.location.
export function wsUrl(id: string, readonly: boolean, loc: Pick<Location, 'protocol' | 'host'> = window.location) {
  const proto = loc.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${loc.host}/api/terminals/${encodeURIComponent(id)}/ws${readonly ? '?readonly=1' : ''}`
}
