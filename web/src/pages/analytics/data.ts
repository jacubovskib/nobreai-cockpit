import type { Total } from '../../api/types'

const MONTHS = ['jan', 'fev', 'mar', 'abr', 'mai', 'jun', 'jul', 'ago', 'set', 'out', 'nov', 'dez']

export interface DayCell { usd: number; unpriced: boolean }
export interface Day {
  day: string // AAAA-MM-DD (dia local)
  label: string // "30 out"
  cells: Record<string, DayCell>
}

const pad = (n: number) => String(n).padStart(2, '0')
export const ymd = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
export const dayLabel = (day: string) => {
  const [, m, d] = day.split('-').map(Number)
  return `${d} ${MONTHS[m - 1]}`
}

// range devolve [from, to) em dias locais para "últimos n dias" (hoje incluso).
export function range(n: number, now = new Date()) {
  const from = new Date(now.getFullYear(), now.getMonth(), now.getDate() - (n - 1))
  const to = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
  return { from: ymd(from), to: ymd(to) }
}

// buildDays preenche todos os dias do intervalo (inclusive os sem uso) com o
// custo por agente vindo de group=day_agent.
export function buildDays(totals: Total[], from: string, to: string): Day[] {
  const byDay = new Map<string, Record<string, DayCell>>()
  for (const t of totals) {
    const cells = byDay.get(t.day) ?? {}
    const c = cells[t.agent] ?? { usd: 0, unpriced: false }
    c.usd += t.cost_usd
    c.unpriced = c.unpriced || t.unpriced
    cells[t.agent] = c
    byDay.set(t.day, cells)
  }
  const days: Day[] = []
  const parse = (s: string) => {
    const [y, m, d] = s.split('-').map(Number)
    return new Date(y, m - 1, d)
  }
  const end = parse(to)
  for (let d = parse(from); d < end; d = new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1)) {
    const key = ymd(d)
    days.push({ day: key, label: dayLabel(key), cells: byDay.get(key) ?? {} })
  }
  return days
}

const nf = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
export const usd2 = (v: number) => nf.format(v)

export function tokens(n: number) {
  if (n >= 1e6) return `${(n / 1e6).toFixed(1).replace('.', ',')} M`
  if (n >= 1e3) return `${Math.round(n / 1e3)} k`
  return String(n)
}

// niceMax arredonda o topo do eixo para um valor "redondo" com 4 intervalos.
export function niceMax(max: number) {
  if (max <= 0) return 4
  const step = max / 4
  const pow = 10 ** Math.floor(Math.log10(step))
  const f = step / pow
  const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10
  return nice * pow * 4
}
