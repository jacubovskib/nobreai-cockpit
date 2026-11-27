import { memo, useEffect, useState } from 'react'
import { useOverview, usePlanLimits } from '../../api/queries'
import { agentInfo } from '../../ui/agents'
import { claudeChip, claudeWindows, kiroText, worstTone, type PlanTone } from './planStripModel'

const COLOR: Record<PlanTone, string> = { ok: 'var(--ok)', warn: 'var(--warn)', crit: 'var(--err)', stale: 'var(--text-2)' }
const sub: React.CSSProperties = { fontSize: 12, color: 'var(--text-2)' }
const card: React.CSSProperties = {
  display: 'flex', flexDirection: 'column', gap: 6, minWidth: 220, flex: '1 1 220px', padding: '8px 12px',
  border: '1px solid var(--border)', borderRadius: 8, background: 'var(--surface)',
}

// Faixa do plano sob o cabeçalho dos Terminais: reusa as consultas da Sala de Controle
// e some por inteiro quando não há leitura alguma.
export const PlanStrip = memo(function PlanStrip({ simple, hasKiro }: { simple: boolean; hasKiro: boolean }) {
  const plan = usePlanLimits(24)
  const overview = useOverview()
  // Reavalia os tempos relativos a cada 30 s sem redesenhar a página.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30000)
    return () => clearInterval(t)
  }, [])

  const ws = claudeWindows(plan.data, now)
  const perAgent = overview.data?.today?.per_agent
  const kiro = kiroText(Array.isArray(perAgent) ? perAgent : undefined, hasKiro)
  if (ws.length === 0 && kiro === null) return null

  if (simple) {
    const chip = (text: string, tone: PlanTone) => (
      <span key={text} data-tone={tone} style={{ ...sub, color: COLOR[tone], border: `1px solid ${COLOR[tone]}`, borderRadius: 999, padding: '2px 10px' }}>{text}</span>
    )
    return (
      <section aria-label="Uso do plano" style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {ws.length > 0 && chip(claudeChip(ws), worstTone(ws))}
        {kiro !== null && chip(`Kiro ${kiro}`, 'ok')}
      </section>
    )
  }
  return (
    <section aria-label="Uso do plano" style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
      {ws.length > 0 && (
        <div style={card}>
          <strong style={{ fontSize: 13, color: agentInfo('claude').color }}>Claude</strong>
          {ws.map((w) => (
            <div key={w.key} style={{ display: 'flex', flexDirection: 'column', gap: 3 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, gap: 8 }}>
                <span>{w.label}</span>
                <strong style={{ fontWeight: 600, color: COLOR[w.tone] }}>{w.value}</strong>
              </div>
              {w.pct !== null && (
                <div
                  role="meter" data-tone={w.tone} aria-label={`${w.label}: ${w.value} usado`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(w.pct)}
                  style={{ height: 6, borderRadius: 3, background: 'var(--border)', overflow: 'hidden' }}
                >
                  <div style={{ height: '100%', width: `${Math.min(w.pct, 100)}%`, background: COLOR[w.tone] }} />
                </div>
              )}
              {w.reset && <span style={sub}>{w.reset}</span>}
            </div>
          ))}
        </div>
      )}
      {kiro !== null && (
        <div style={card}>
          <strong style={{ fontSize: 13, color: agentInfo('kiro').color }}>Kiro</strong>
          <span style={{ fontSize: 13 }}>{kiro}</span>
        </div>
      )}
    </section>
  )
})
