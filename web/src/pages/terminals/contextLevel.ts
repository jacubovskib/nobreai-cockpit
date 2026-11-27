import type { TerminalInfo } from '../../api/terminals'
import type { ActiveSession, LiveContextSummary } from '../../api/types'
import { formatTokensPt } from '../context/format'

// Crítico fixo em 90%; o limite do usuário (última faixa de context_warn_pcts) vale a partir de 80 por padrão.
export const CTX_CRIT = 90
export const CTX_DEFAULT_LIMIT = 80

export type CtxLevel = 'ok' | 'warn' | 'crit'

export const CTX_COLOR: Record<CtxLevel, string> = {
  ok: 'var(--ok)',
  warn: 'var(--warn)',
  crit: 'var(--err)',
}

// ctxLimit: a última faixa de aviso; lista vazia, ausente ou que não é lista → 80.
export function ctxLimit(warnPcts: readonly number[] | undefined): number {
  if (!Array.isArray(warnPcts) || warnPcts.length === 0) return CTX_DEFAULT_LIMIT
  return warnPcts[warnPcts.length - 1]
}

// ctxLevel: pct já arredondado, o mesmo número que o tile mostra.
export function ctxLevel(pct: number, limit: number): CtxLevel {
  if (pct >= Math.max(CTX_CRIT, limit)) return 'crit'
  if (pct >= limit) return 'warn'
  return 'ok'
}

export function ctxDetail(tokens: number, windowTokens: number, pctOnly: boolean): string {
  if (pctOnly) return 'o agente informa só a %'
  return `${formatTokensPt(tokens)} de ${formatTokensPt(windowTokens)}`
}

export function ctxWarning(pct: number, limit: number, simple: boolean): string {
  const tail = 'Limpe e continue de onde parou.'
  return simple ? `Contexto em ${pct}%. ${tail}` : `Contexto em ${pct}%, acima do limite de ${limit}%. ${tail}`
}

// TileMeter: o que o tile recebe, só primitivos. projectId serve ao place (não vai ao tile).
export interface TileMeter {
  sessionId: string // '' = sem sessão casada
  cardKey: string // sessão casada: o card dela ('' se não tem); sem sessão: t.card_key
  projectId: number | null
  ctxPct: number | null // null = sem dado
  ctxTokens: number
  ctxWindow: number
  ctxPctOnly: boolean
  whereStopped: string
  whereStoppedAt: string | null
}

export function tileMeter(
  t: TerminalInfo,
  s: Pick<ActiveSession, 'id' | 'card' | 'effective_project_id'> | undefined,
  c: LiveContextSummary | undefined,
): TileMeter {
  // Sem turno medido o daemon devolve tokens 0 e pct 0: não há o que mostrar.
  const measured = c !== undefined && !(c.tokens === 0 && c.pct === 0)
  return {
    sessionId: s?.id ?? '',
    cardKey: s ? (s.card?.key ?? '') : t.card_key,
    projectId: s ? (s.effective_project_id ?? null) : null,
    ctxPct: measured ? Math.round(c.pct) : null,
    ctxTokens: c?.tokens ?? 0,
    ctxWindow: c?.window_tokens ?? 0,
    ctxPctOnly: c?.pct_only ?? false,
    whereStopped: s?.card?.where_stopped ?? '',
    whereStoppedAt: s?.card?.where_stopped_at ?? null,
  }
}
