import { tokens } from './data'
import type { ContextTurnPoint } from '../../api/types'

const W = 600
const H = 160
const PAD = { l: 8, r: 8, t: 20, b: 8 }

// ContextCurve: tamanho do contexto por turno; linhas tracejadas marcam as
// compactações e o rótulo marca o pico. Sessão só com % (o agente não informa
// tokens) desenha a curva em % da janela, de 0 a 100.
export function ContextCurve({ turns }: { turns: ContextTurnPoint[] }) {
  const pctOnly = turns.length > 0 && turns.every((t) => t.pct_only)
  const val = (t: ContextTurnPoint) => (pctOnly ? t.pct ?? 0 : t.tokens)
  const max = pctOnly ? 100 : Math.max(1, ...turns.map(val))
  const x = (i: number) => PAD.l + (turns.length <= 1 ? 0 : (i / (turns.length - 1)) * (W - PAD.l - PAD.r))
  const y = (v: number) => PAD.t + (1 - v / max) * (H - PAD.t - PAD.b)
  const peak = turns.reduce((p, t, i) => (val(t) > val(turns[p]) ? i : p), 0)
  const px = x(peak)
  return (
    <svg viewBox={`0 0 ${W} ${H}`} width="100%" role="img" aria-label="Contexto por turno" style={{ display: 'block' }}>
      {turns.map((t, i) => t.after_compact && (
        <line key={`c${i}`} data-testid="compact" x1={x(i)} x2={x(i)} y1={PAD.t} y2={H - PAD.b} stroke="var(--text-2)" strokeDasharray="4 3" />
      ))}
      <polyline fill="none" stroke="var(--claude)" strokeWidth={2} points={turns.map((t, i) => `${x(i)},${y(val(t))}`).join(' ')} />
      {pctOnly && (
        <text x={W - PAD.r} y={PAD.t + 10} textAnchor="end" fontSize={11} fill="var(--text-2)">100%</text>
      )}
      {turns.length > 0 && (
        <text x={px} y={PAD.t - 6} textAnchor={px > W / 2 ? 'end' : 'start'} fontSize={12} fill="var(--text-2)">
          pico {pctOnly ? `${Math.round(val(turns[peak]))}%` : tokens(turns[peak].tokens)}
        </text>
      )}
    </svg>
  )
}
