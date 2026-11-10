// Peças compartilhadas pela lista e pelo detalhe de worktrees.
import { Link } from 'react-router-dom'
import type { CardRef, Worktree, WorktreeResources, WorktreeSession } from '../api/types'
import { minutesSince } from './board'
import { muted } from './card/shared'
import { useWithProject } from '../app/project'

export const tag: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 22, padding: '0 8px', borderRadius: 6, fontSize: 12, border: '1px solid var(--border)', color: 'var(--text-2)' }
export const tagWarn: React.CSSProperties = { ...tag, color: 'var(--warn)', borderColor: '#4A3B17' }
export const mono: React.CSSProperties = { fontFamily: 'var(--mono)' }
export const oneDec = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })

// baseName é o último segmento do caminho: o nome curto do worktree.
export const baseName = (path: string) => path.split('/').filter(Boolean).pop() ?? path

// agoText: "N min" até 1 h, "N h" até 1 dia, depois "N d".
export function agoText(iso: string) {
  const min = minutesSince(iso)
  if (min < 60) return `${min} min`
  if (min < 24 * 60) return `${Math.floor(min / 60)} h`
  return `${Math.floor(min / (24 * 60))} d`
}

// machinePct converte % de um núcleo em % da máquina.
export const machinePct = (r: WorktreeResources) => r.cpu_pct / Math.max(1, r.cpu_cores)

export function detailHref(projectID: number, path: string) {
  return `/worktrees/detalhe?${new URLSearchParams({ project: String(projectID), path })}`
}

export function BranchIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="8" r="2" />
      <path d="M6 7v10M18 10c0 4-6 3-12 7" />
    </svg>
  )
}

// WorktreeTags: principal/ligado, travado e diretório sumiu (com os motivos se pedido).
export function WorktreeTags({ w, reasons }: { w: Worktree; reasons?: boolean }) {
  return (
    <>
      {w.main ? <span style={tag}>principal</span> : <span style={tag}>worktree ligado</span>}
      {w.locked && <span style={tag}>{reasons && w.lock_reason ? `travado: ${w.lock_reason}` : 'travado'}</span>}
      {w.prunable && <span style={tagWarn}>{reasons && w.prune_reason ? `diretório sumiu: ${w.prune_reason}` : 'diretório sumiu'}</span>}
    </>
  )
}

export function SessionStatus({ s }: { s: WorktreeSession }) {
  if (s.live) {
    return (
      <span className="badge badge-live">
        <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="3.5" fill="var(--ok)" /></svg>
        Ao vivo · {minutesSince(s.started_at)} min
      </span>
    )
  }
  if (!s.ended_at && s.pid_dead_at) return <span style={{ fontSize: 13, color: 'var(--warn)' }}>processo encerrado há {minutesSince(s.pid_dead_at)} min</span>
  return <span style={muted}>encerrada há {agoText(s.ended_at ?? s.last_event_at)}</span>
}

export function CardLink({ c }: { c: CardRef }) {
  const withP = useWithProject()
  return (
    <Link to={withP(`/cards/${c.key}`)} style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 8, color: 'var(--text)', textDecoration: 'none' }}>
      <span style={{ ...mono, fontSize: 12.5, color: 'var(--text-2)' }}>{c.key}</span>
      <span style={{ fontWeight: 500 }}>{c.title}</span>
    </Link>
  )
}
