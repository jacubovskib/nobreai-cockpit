import type { CardSummary, Column } from '../api/types'

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
