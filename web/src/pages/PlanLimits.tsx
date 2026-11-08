import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { usePlanLimits } from '../api/queries'
import type { PlanPoint, PlanWindowState } from '../api/types'
import { panel } from './card/shared'

const h2 = { fontSize: 15, fontWeight: 600 } as const
const WEEKDAYS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb']
const pad = (n: number) => String(n).padStart(2, '0')

const clock = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

// duration: "2 h 14 min", "45 min", "1 d 3 h". Menos de 1 min = "menos de 1 min".
export function duration(ms: number): string {
  const min = Math.floor(ms / 60000)
  if (min < 1) return 'menos de 1 min'
  if (min < 60) return `${min} min`
  const h = Math.floor(min / 60)
  if (h < 24) return `${h} h ${min % 60} min`
  return `${Math.floor(h / 24)} d ${h % 24} h`
}

// resetText: "reinicia em 2 h 14 min (às 17:30)"; semanal: "reinicia em 2 d 3 h (sáb às 09:00)".
export function resetText(iso: string, now: number, weekday: boolean): string {
  const at = new Date(iso)
  const when = `${weekday ? `${WEEKDAYS[at.getDay()]} ` : ''}às ${clock(at)}`
  return `reinicia em ${duration(at.getTime() - now)} (${when})`
}

const pct = (v: number) => `${Math.round(v)}%`

// Sparkline em SVG puro. Eixo X: as últimas `hours` h; eixo Y: 0–100%.
export function Sparkline({ points, now, hours, label }: { points: PlanPoint[]; now: number; hours: number; label: string }) {
  if (points.length === 0) return <span style={{ fontSize: 12, color: 'var(--text-2)' }}>sem histórico nas últimas {hours} h</span>
  const W = 160, H = 32, span = hours * 3600000
  const x = (t: number) => Math.max(0, Math.min(W, ((t - (now - span)) / span) * W))
  const y = (v: number) => H - 2 - (Math.max(0, Math.min(100, v)) / 100) * (H - 4)
  const vals = points.map((p) => p.used_pct)
  const min = Math.min(...vals), max = Math.max(...vals), last = vals[vals.length - 1]
  const coords = points.map((p) => `${x(new Date(p.at).getTime()).toFixed(1)},${y(p.used_pct).toFixed(1)}`)
  const aria = `${label}, últimas ${hours} h: mínimo ${pct(min)}, máximo ${pct(max)}, último ${pct(last)}`
  return (
    <svg role="img" aria-label={aria} viewBox={`0 0 ${W} ${H}`} width="100%" height={H} preserveAspectRatio="none" style={{ maxWidth: 220, display: 'block' }}>
      <title>{aria}</title>
      {points.length > 1
        ? <polyline points={coords.join(' ')} fill="none" stroke="var(--claude)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        : <circle cx={coords[0].split(',')[0]} cy={coords[0].split(',')[1]} r="2" fill="var(--claude)" />}
    </svg>
  )
}

function WindowRow({ name, label, w, history, warn, now, weekday }: {
  name: string; label: string; w: PlanWindowState | null; history: PlanPoint[]; warn: number; now: number; weekday: boolean
}) {
  const sub: React.CSSProperties = { fontSize: 13, color: 'var(--text-2)' }
  let body: React.ReactNode
  if (!w) {
    body = <span style={sub}>sem dados ainda</span>
  } else if (w.expired) {
    body = <span style={sub}>reiniciada — aguardando nova leitura</span>
  } else {
    const over = w.used_pct >= warn
    body = (
      <>
        <div
          role="meter" aria-label={`${label}: ${pct(w.used_pct)} usado`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(w.used_pct)}
          style={{ position: 'relative', height: 8, borderRadius: 4, background: 'var(--border)' }}
        >
          <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, borderRadius: 4, width: `${Math.min(w.used_pct, 100)}%`, background: 'var(--claude)', boxShadow: over ? '0 0 0 2px #FAB219' : undefined }} />
          <div data-testid={`plan-warn-marker-${name}`} aria-hidden="true" style={{ position: 'absolute', left: `${warn}%`, top: -3, width: 2, height: 14, background: '#6B7280' }} />
        </div>
        <span style={sub}>
          {w.resets_at ? resetText(w.resets_at, now, weekday) : 'sem horário de reinício'} · atualizado há {duration(now - new Date(w.received_at).getTime())}
        </span>
      </>
    )
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, gap: 8 }}>
        <span>{label}</span>
        {w && !w.expired && (
          <strong style={{ fontWeight: 600 }}>{pct(w.used_pct)}{w.used_pct >= warn && <span style={{ color: 'var(--warn)', fontWeight: 400 }}> · aviso</span>}</strong>
        )}
      </div>
      {body}
      <Sparkline points={history} now={now} hours={24} label={label} />
    </div>
  )
}

export function PlanLimits() {
  const { data, isPending, isError } = usePlanLimits(24)
  // Reavalia os tempos relativos a cada 30 s, sem novo pedido.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30000)
    return () => clearInterval(t)
  }, [])

  let body: React.ReactNode
  if (isError) body = <p role="alert" style={{ margin: 0, color: 'var(--err)', fontSize: 13 }}>Não foi possível carregar os limites do plano.</p>
  else if (isPending || !data) body = <p className="placeholder">Carregando…</p>
  else if (!data.five_hour && !data.seven_day) {
    // Nunca mostra 0%: sem leitura é "sem dados", não "plano zerado".
    body = (
      <p style={{ margin: 0, fontSize: 13, color: 'var(--text-2)' }}>
        Sem dados do plano ainda. Os percentuais chegam pela status line do Claude Code, depois da primeira resposta de uma sessão
        (contas por assinatura; plano sem limites de uso não envia nada). Confira a integração em <Link to="/config#agentes">Configuração</Link>.
      </p>
    )
  } else {
    body = (
      <>
        <WindowRow name="five_hour" label="Sessão de 5 h" w={data.five_hour} history={data.history.five_hour ?? []} warn={data.warn_pct} now={now} weekday={false} />
        <WindowRow name="seven_day" label="Semana" w={data.seven_day} history={data.history.seven_day ?? []} warn={data.warn_pct} now={now} weekday />
        <p style={{ margin: 0, fontSize: 12, color: 'var(--text-2)' }}>Marcador cinza = aviso em {data.warn_pct}%.</p>
      </>
    )
  }
  return (
    <section id="plano-limites" aria-labelledby="plano-t" style={{ ...panel, gap: 14 }}>
      <h2 id="plano-t" style={h2}>Limites do plano</h2>
      {body}
    </section>
  )
}
