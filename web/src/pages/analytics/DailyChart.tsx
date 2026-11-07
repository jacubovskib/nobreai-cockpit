import { useState } from 'react'
import { agentInfo } from '../../ui/agents'
import { niceMax, usd2, type Day } from './data'

const H = 240
const GAP = 2

const total = (d: Day) => Object.values(d.cells).reduce((s, c) => s + c.usd, 0)
const anyUnpriced = (d: Day) => Object.values(d.cells).some((c) => c.unpriced)

// ariaFor descreve o dia em texto: serve ao leitor de tela e ao teste.
export function ariaFor(d: Day, agents: string[]) {
  const parts = agents.map((a) => {
    const c = d.cells[a]
    return `${agentInfo(a).name} ${c ? (c.unpriced ? 'sem preço' : `US$ ${usd2(c.usd)}`) : 'US$ 0,00'}`
  })
  const t = anyUnpriced(d) ? `US$ ${usd2(total(d))} mais uso sem preço` : `US$ ${usd2(total(d))}`
  return `${d.label}: total ${t}; ${parts.join(', ')}`
}

// DailyChart: barras empilhadas de custo por dia e agente, um eixo só.
// Uso sem preço nunca vira zero: o dia mostra o marcador "sem preço".
export function DailyChart({ days, agents }: { days: Day[]; agents: string[] }) {
  const [active, setActive] = useState(-1)
  const max = niceMax(Math.max(0, ...days.map(total)))
  const ticks = [0, 1, 2, 3, 4].map((i) => (max / 4) * i)
  const dense = days.length > 14
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '44px 1fr', columnGap: 8 }}>
      <div aria-hidden="true" style={{ position: 'relative', height: H, fontSize: 12, color: 'var(--text-2)' }}>
        {ticks.map((t) => (
          <span key={t} style={{ position: 'absolute', right: 0, bottom: (t / max) * H - 8, lineHeight: '16px' }}>
            {Number.isInteger(t) ? t : usd2(t)}
          </span>
        ))}
      </div>
      <div style={{ position: 'relative', height: H, borderBottom: '1px solid #3A3F4B' }}>
        {ticks.slice(1).map((t) => (
          <div key={t} aria-hidden="true" style={{ position: 'absolute', left: 0, right: 0, bottom: (t / max) * H, borderTop: '1px solid #1C2028' }} />
        ))}
        <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'flex-end', gap: dense ? 2 : 8 }}>
          {days.map((d, i) => {
            const segs = agents.filter((a) => (d.cells[a]?.usd ?? 0) > 0)
            const unpriced = anyUnpriced(d)
            const on = active === i
            return (
              <div
                key={d.day}
                role="img"
                tabIndex={0}
                aria-label={ariaFor(d, agents)}
                onMouseEnter={() => setActive(i)}
                onMouseLeave={() => setActive((v) => (v === i ? -1 : v))}
                onFocus={() => setActive(i)}
                onBlur={() => setActive((v) => (v === i ? -1 : v))}
                style={{ position: 'relative', flex: 1, minWidth: 0, height: '100%', display: 'flex', alignItems: 'flex-end', justifyContent: 'center', outline: 'none' }}
              >
                <div
                  style={{
                    display: 'flex', flexDirection: 'column-reverse', width: dense ? '80%' : 'min(44px, 70%)',
                    outline: on ? '2px solid #A0A7B4' : 'none', outlineOffset: 3, borderRadius: 4,
                  }}
                >
                  {segs.map((a, j) => {
                    const h = Math.max(2, Math.round(((d.cells[a]?.usd ?? 0) / max) * H) - (j > 0 ? GAP : 0))
                    const top = j === segs.length - 1 && !unpriced
                    return (
                      <div
                        key={a}
                        aria-hidden="true"
                        style={{ height: h, background: agentInfo(a).color, marginTop: j < segs.length - 1 || unpriced ? GAP : 0, borderRadius: top ? '4px 4px 0 0' : 0 }}
                      />
                    )
                  })}
                  {unpriced && (
                    <div
                      aria-hidden="true"
                      title="uso sem preço"
                      style={{ height: 14, border: '1px dashed var(--warn)', borderRadius: '4px 4px 0 0', color: 'var(--warn)', fontSize: 10, lineHeight: '12px', textAlign: 'center', order: 99 }}
                    >
                      ?
                    </div>
                  )}
                </div>
                {on && (
                  <div
                    role="tooltip"
                    style={{
                      position: 'absolute', bottom: Math.round((total(d) / max) * H) + (unpriced ? 28 : 12), zIndex: 2, minWidth: 190, padding: '10px 12px',
                      borderRadius: 8, background: '#232833', border: '1px solid #3A3F4B', fontSize: 12.5, lineHeight: 1.6, whiteSpace: 'nowrap',
                      boxShadow: '0 8px 24px rgba(0,0,0,0.4)', display: 'flex', flexDirection: 'column',
                      ...(i > days.length - 3 ? { right: 0 } : i < 2 ? { left: 0 } : { left: '50%', transform: 'translateX(-50%)' }),
                    }}
                  >
                    <strong>{d.label} · {unpriced ? `US$ ${usd2(total(d))} + sem preço` : `US$ ${usd2(total(d))}`}</strong>
                    {agents.map((a) => {
                      const c = d.cells[a]
                      return (
                        <span key={a} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                          <span aria-hidden="true" style={{ width: 10, height: 10, borderRadius: 3, background: agentInfo(a).color }} />
                          {agentInfo(a).name} {c?.unpriced ? 'sem preço' : `US$ ${usd2(c?.usd ?? 0)}`}
                        </span>
                      )
                    })}
                  </div>
                )}
              </div>
            )
          })}
        </div>
      </div>
      <div />
      <div aria-hidden="true" style={{ display: 'flex', gap: dense ? 2 : 8, paddingTop: 6, fontSize: dense ? 10 : 12, color: 'var(--text-2)' }}>
        {days.map((d, i) => (
          <span key={d.day} style={{ flex: 1, textAlign: 'center', minWidth: 0, overflow: 'hidden', visibility: dense && i % 3 !== 0 ? 'hidden' : 'visible' }}>{d.label}</span>
        ))}
      </div>
    </div>
  )
}
