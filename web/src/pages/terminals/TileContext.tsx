import { barPct } from '../context/format'
import { CTX_COLOR, ctxDetail, ctxLevel, ctxWarning } from './contextLevel'

// Peças do medidor de contexto do tile: HTML simples, sem estado.
// Todo elemento colorido pelo nível leva data-level (os testes conferem o atributo, não a cor).

const link: React.CSSProperties = { marginLeft: 'auto', border: 0, background: 'transparent', padding: 0, color: 'var(--accent, #6ea8ff)', font: 'inherit', textDecoration: 'underline', cursor: 'pointer' }

export function CtxChip(p: { pct: number; limit: number }) {
  const level = ctxLevel(p.pct, p.limit)
  return (
    <span data-testid="ctx-chip" data-level={level} title="Contexto usado" style={{ color: CTX_COLOR[level], fontVariantNumeric: 'tabular-nums' }}>
      ctx {p.pct}%
    </span>
  )
}

export function CtxStrip(p: { pct: number; tokens: number; windowTokens: number; pctOnly: boolean; limit: number; onClear?: () => void }) {
  const level = ctxLevel(p.pct, p.limit)
  return (
    <div data-testid="ctx-strip" style={{ padding: '4px 8px', borderBottom: '1px solid var(--border)', fontSize: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
        <strong>Contexto {p.pct}%</strong>
        <span style={{ color: 'var(--text-2)' }}>· {ctxDetail(p.tokens, p.windowTokens, p.pctOnly)}</span>
        {p.onClear && <button type="button" style={link} onClick={p.onClear}>Limpar</button>}
      </div>
      <div
        role="progressbar"
        aria-label="Contexto usado"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(barPct(p.pct))}
        style={{ position: 'relative', height: 6, marginTop: 4, borderRadius: 3, background: 'var(--border)' }}
      >
        <div data-testid="ctx-bar" data-level={level} style={{ width: `${barPct(p.pct)}%`, height: '100%', borderRadius: 3, background: CTX_COLOR[level] }} />
        <div data-testid="ctx-limit" title={`Limite configurado ${p.limit}%`} style={{ position: 'absolute', top: -2, bottom: -2, left: `${p.limit}%`, width: 2, background: 'var(--text-2)' }} />
      </div>
    </div>
  )
}

export function CtxWarning(p: { pct: number; limit: number; simple: boolean; onClear: () => void }) {
  const level = ctxLevel(p.pct, p.limit) === 'crit' ? 'crit' : 'warn'
  return (
    <div
      role="status"
      data-testid="ctx-warning"
      data-level={level}
      style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, padding: '4px 8px', borderBottom: '1px solid var(--border)', fontSize: 12, color: CTX_COLOR[level] }}
    >
      <span>{ctxWarning(p.pct, p.limit, p.simple)}</span>
      <button type="button" style={{ ...link, marginLeft: 'auto', color: 'inherit' }} onClick={p.onClear}>Limpar e continuar</button>
    </div>
  )
}
