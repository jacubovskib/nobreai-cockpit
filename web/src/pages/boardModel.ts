import type { Card, CardSummary, Column } from '../api/types'

export const COLUMNS: { id: Column; label: string; rule: string; dot: string }[] = [
  { id: 'backlog', label: 'Backlog', rule: 'Criado por você ou por um agente, sem sessão ainda', dot: '#6B7280' },
  { id: 'em_andamento', label: 'Em andamento', rule: 'Uma sessão viva é dona do card', dot: '#3987E5' },
  { id: 'pausado', label: 'Pausado / Handoff', rule: 'Sessão terminou sem concluir; qualquer agente pode retomar', dot: '#FAB219' },
  { id: 'revisao', label: 'Revisão', rule: 'O agente declarou pronto; aguarda você', dot: '#9085E9' },
  { id: 'concluido', label: 'Concluído', rule: 'Aprovado por você', dot: '#0CA30C' },
]

// groupByColumn devolve sempre as 5 colunas, na ordem fixa, mesmo vazias.
export function groupByColumn(cards: CardSummary[]): Record<Column, CardSummary[]> {
  const out = {} as Record<Column, CardSummary[]>
  for (const c of COLUMNS) out[c.id] = []
  for (const card of cards) out[card.column]?.push(card)
  return out
}

// minutesSince: minutos inteiros desde um instante RFC3339 (0 se vazio/futuro).
export function minutesSince(iso: string | null, now: number = Date.now()): number {
  if (!iso) return 0
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return 0
  return Math.max(0, Math.floor((now - t) / 60000))
}

export const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

// signalText: "parado há 30 h" / "aguardando você há 3 dias" / "bloqueado por NAC-4". Abaixo
// de 48 h mostra horas; a partir daí, dias inteiros (arredondados para baixo).
// Sem sinal, vazio.
export function signalText(card: Pick<CardSummary, 'signal' | 'signal_since' | 'blocked_by'>, now: Date = new Date()): string {
  const { signal, signal_since: since } = card
  if (!signal || !since) return ''
  if (signal === 'bloqueado') return `bloqueado por ${card.blocked_by}`
  if (signal === 'pronto_para_retomar') return `pronto para retomar (${card.blocked_by} concluído)`
  const t = Date.parse(since)
  if (Number.isNaN(t)) return ''
  const hours = Math.max(0, Math.floor((now.getTime() - t) / 3600000))
  const label = signal === 'parado' ? 'parado' : signal === 'aguardando_voce' ? 'aguardando você' : 'aguardando revisão'
  return hours < 48 ? `${label} há ${hours} h` : `${label} há ${Math.floor(hours / 24)} dias`
}

// signalColor: parado = alerta, pronto para retomar = ok, demais = secundária.
export function signalColor(signal: CardSummary['signal']): string {
  return signal === 'parado' ? 'var(--warn)' : signal === 'pronto_para_retomar' ? 'var(--ok)' : 'var(--text-2)'
}

// cardMarks: marcas do card (limite de custo e handoff incompleto, este só em
// Pausado e até ficar pronto).
export function cardMarks(card: Pick<Card, 'column' | 'pause_reason' | 'handoff_incomplete'>, ready: boolean): string[] {
  const out: string[] = []
  if (card.pause_reason === 'limite de custo') out.push('pausado por limite de custo')
  if (card.handoff_incomplete && !ready && card.column === 'pausado') out.push('handoff incompleto')
  return out
}
