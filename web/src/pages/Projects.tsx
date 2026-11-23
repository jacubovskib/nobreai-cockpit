import { useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useCards, useProjects, useTotals } from '../api/queries'
import { Credits, Money } from '../ui/Money'
import { ProjectTag } from '../ui/ProjectTag'
import { projectColor, useWithProject } from '../app/project'
import { range } from './analytics/data'
import { signalText } from './board'
import { btn, muted, panel } from './card/shared'
import type { Project } from '../api/types'
import { buildRows, type ProjectRow } from './projects'

const PERIODS = [
  { id: 1, label: 'Hoje' },
  { id: 7, label: '7 dias' },
  { id: 30, label: '30 dias' },
]
const th: CSSProperties = { padding: '8px 12px', fontWeight: 500 }
const td: CSSProperties = { padding: '10px 12px', verticalAlign: 'top' }
const thead: CSSProperties = { textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }
const dateFmt = new Intl.DateTimeFormat('pt-BR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

function Count({ n, since, signal }: { n: number; since: string | null; signal: 'parado' | 'aguardando_revisao' }) {
  if (n === 0) return <span style={muted}>0</span>
  return (
    <span>
      <strong>{n}</strong>
      <span style={{ ...muted, display: 'block' }}>{signalText({ signal, signal_since: since, blocked_by: '' })}</span>
    </span>
  )
}

type RowProps = { r: ProjectRow; list: Project[]; max: number; withP: (to: string) => string }

// Uma linha da tabela. label: o que aparece na primeira coluna; sub: linha de app (indentada, sem link).
function Line({ r, list, max, withP, label, sub, action }: RowProps & { label?: string; sub?: boolean; action?: ReactNode }) {
  return (
    <tr style={{ borderTop: '1px solid var(--border)', background: sub ? 'rgba(255,255,255,0.02)' : undefined }}>
      <td style={{ ...td, paddingLeft: sub ? 32 : 12 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
          {label !== undefined ? (
            <span style={{ fontSize: 12.5, color: 'var(--text-2)' }}>{label}</span>
          ) : r.project ? (
            <Link to={withP(`/projetos/${r.project.id}`)} style={{ color: 'inherit', textDecoration: 'none' }}>
              <ProjectTag project={r.project} projects={list} />
            </Link>
          ) : (
            <ProjectTag project={null} projects={list} />
          )}
          {action}
        </div>
      </td>
      <td style={{ ...td, minWidth: 150 }}>
        {r.project && !r.project.capture ? (
          <span style={muted}>captura desligada</span>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
            <span><Money usd={r.costUsd} unpriced={r.unpriced && r.costUsd === 0} /><Credits credits={r.credits} /></span>
            <div aria-hidden="true" style={{ height: 6, borderRadius: 3, background: 'var(--border)' }}>
              <div style={{ height: 6, borderRadius: 3, width: `${(r.costUsd / max) * 100}%`, background: r.project ? projectColor(r.project.id, list) : 'var(--text-2)' }} />
            </div>
          </div>
        )}
      </td>
      <td style={td}>
        {r.current ? (
          <Link to={withP(`/cards/${r.current.key}`)} style={{ color: 'inherit' }}>
            <span style={{ fontFamily: 'var(--mono, monospace)', fontSize: 12.5 }}>{r.current.key}</span> {r.current.title}
          </Link>
        ) : (
          <span style={muted}>nada em andamento</span>
        )}
      </td>
      <td style={td}><Count n={r.stale.count} since={r.stale.oldest} signal="parado" /></td>
      <td style={td}><Count n={r.awaiting.count} since={r.awaiting.oldest} signal="aguardando_revisao" /></td>
      <td style={{ ...td, color: 'var(--text-2)' }}>{r.lastActivity ? dateFmt.format(new Date(r.lastActivity)) : '—'}</td>
    </tr>
  )
}

// Um pai mostra o total da família e abre os apps (e o "geral") sob demanda.
function FamilyRows(props: RowProps) {
  const [open, setOpen] = useState(false)
  const { r } = props
  if (r.children.length === 0) return <Line {...props} />
  const toggle = (
    <button type="button" aria-expanded={open} onClick={() => setOpen((v) => !v)} style={{ ...btn, minHeight: 28, padding: '0 10px', fontSize: 12.5 }}>
      {open ? '▾' : '▸'} apps
    </button>
  )
  return (
    <>
      <Line {...props} action={toggle} />
      {open && r.children.map((c) => <Line key={c.project?.id} {...props} r={c} sub label={c.project?.name} />)}
      {open && r.general && <Line {...props} r={r.general} sub label="geral" />}
    </>
  )
}

// Compara todos os projetos: ignora o seletor da barra lateral.
export function Projects() {
  const withP = useWithProject()
  const [period, setPeriod] = useState(7)
  const { from, to } = useMemo(() => range(period), [period])
  const projects = useProjects()
  const cards = useCards()
  const totals = useTotals('project', from, to)
  const failed = projects.isError || cards.isError || totals.isError
  const loading = projects.isPending || cards.isPending || totals.isPending
  const list = projects.data ?? []
  const rows = useMemo(() => buildRows(list, cards.data ?? [], totals.data ?? []), [list, cards.data, totals.data])
  const max = Math.max(0.0001, ...rows.map((r) => r.costUsd))

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1180 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Projetos</h1>
          <p style={{ ...muted, margin: '4px 0 0' }}>Custo, entrega atual e o que está esperando por você, projeto a projeto</p>
        </div>
        <div role="group" aria-label="Período" style={{ display: 'flex', gap: 8 }}>
          {PERIODS.map((p) => (
            <button
              key={p.id}
              type="button"
              aria-pressed={period === p.id}
              onClick={() => setPeriod(p.id)}
              style={{ ...btn, borderRadius: 999, borderColor: period === p.id ? '#4A505D' : '#2E333E', background: period === p.id ? '#2A2F3A' : 'transparent', color: period === p.id ? '#fff' : '#C3C8D2' }}
            >
              {p.label}
            </button>
          ))}
        </div>
      </header>

      {failed && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os projetos.</p>}
      {loading && !failed && <p className="placeholder">Carregando…</p>}

      {!loading && !failed && (
        <section style={{ ...panel, overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
            <thead>
              <tr style={thead}>
                <th style={th}>Projeto</th>
                <th style={th}>Custo</th>
                <th style={th}>Entrega atual</th>
                <th style={th}>Parados</th>
                <th style={th}>Aguardando você</th>
                <th style={th}>Última atividade</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <FamilyRows key={r.project?.id ?? 'none'} r={r} list={list} max={max} withP={withP} />
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}
