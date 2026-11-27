import { duration } from '../PlanLimits'
import type { AgentSpend, PlanLimits, PlanWindowState } from '../../api/types'

// Vermelho a partir de 90%, fixo; âmbar a partir do warn_pct do plano.
export const PLAN_CRIT = 90
export type PlanTone = 'ok' | 'warn' | 'crit' | 'stale'
export interface PlanWindowView {
  key: 'five_hour' | 'seven_day'
  label: string
  short: string
  pct: number | null // null = vencida
  value: string
  reset: string
  tone: PlanTone
}

const WEEKDAYS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb']
const pad = (n: number) => String(n).padStart(2, '0')
const META = {
  five_hour: { label: 'Sessão de 5 h', short: '5 h', weekday: false },
  seven_day: { label: 'Semana', short: 'sem', weekday: true },
} as const

// resetShort: "reinicia em 2 h 14 min (17:30)"; semanal: "reinicia em 2 d 3 h (sáb 09:00)".
export function resetShort(iso: string, now: number, weekday: boolean): string {
  const at = new Date(iso)
  const clock = `${pad(at.getHours())}:${pad(at.getMinutes())}`
  return `reinicia em ${duration(at.getTime() - now)} (${weekday ? `${WEEKDAYS[at.getDay()]} ` : ''}${clock})`
}

export function planWindow(
  key: 'five_hour' | 'seven_day', w: PlanWindowState | null | undefined, warnPct: number, now: number,
): PlanWindowView | null {
  if (!w || typeof w !== 'object' || typeof w.used_pct !== 'number') return null
  const m = META[key]
  const base = { key, label: m.label, short: m.short }
  const resetAt = w.resets_at ? new Date(w.resets_at).getTime() : NaN
  // Vencida pelo daemon ou pelo relógio do cliente.
  if (w.expired || (w.resets_at && resetAt <= now)) {
    return { ...base, pct: null, value: 'sem leitura recente', reset: '', tone: 'stale' }
  }
  const tone: PlanTone = w.used_pct >= PLAN_CRIT ? 'crit' : w.used_pct >= warnPct ? 'warn' : 'ok'
  return {
    ...base,
    pct: w.used_pct,
    value: `${Math.round(w.used_pct)}%`,
    reset: w.resets_at ? resetShort(w.resets_at, now, m.weekday) : 'sem horário de reinício',
    tone,
  }
}

export function claudeWindows(p: PlanLimits | undefined, now: number): PlanWindowView[] {
  if (!p || typeof p !== 'object' || Array.isArray(p)) return []
  const warn = typeof p.warn_pct === 'number' ? p.warn_pct : 80
  return [planWindow('five_hour', p.five_hour, warn, now), planWindow('seven_day', p.seven_day, warn, now)]
    .filter((v): v is PlanWindowView => v !== null)
}

export function claudeChip(ws: PlanWindowView[]): string {
  return `Claude ${ws.map((w) => `${w.short} ${w.value}`).join(' · ')}`
}

const RANK: Record<PlanTone, number> = { ok: 0, stale: 1, warn: 2, crit: 3 }
export function worstTone(ws: PlanWindowView[]): PlanTone {
  return ws.reduce<PlanTone>((t, w) => (RANK[w.tone] > RANK[t] ? w.tone : t), 'ok')
}

export function kiroText(perAgent: readonly AgentSpend[] | undefined, hasKiro: boolean): string | null {
  if (!hasKiro || !Array.isArray(perAgent)) return null
  const credits = perAgent.find((a) => a?.agent === 'kiro')?.credits
  if (typeof credits !== 'number' || !(credits > 0)) return null
  return `${credits.toLocaleString('pt-BR', { maximumFractionDigits: 1 })} créditos hoje`
}
