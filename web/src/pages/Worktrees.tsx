import { Link, useLocation } from 'react-router-dom'
import { useWorktrees } from '../api/queries'
import { useProjectFilter, useWithProject } from '../app/project'
import type { ProjectWorktrees, Worktree, WorktreeResources, WorktreeSession } from '../api/types'
import { AgentSwatch } from '../ui/AgentSwatch'
import { StatusBadge } from '../ui/StatusBadge'
import { formatBytes, formatPct } from '../ui/bytes'
import { plural } from './board'
import { h2, muted, panel } from './card/shared'
import { BranchIcon, CardLink, SessionStatus, WorktreeTags, agoText, baseName, detailHref, machinePct, mono } from './worktreeUI'

// resourceLine resume os recursos numa linha: "CPU 15% · 1,2 GB · 3,4 GB em disco".
function resourceLine(r: WorktreeResources) {
  const parts = [r.ready ? `CPU ${formatPct(machinePct(r))}` : 'CPU medindo…', formatBytes(r.rss_bytes)]
  if (r.disk) parts.push(r.disk.measured_at ? `${formatBytes(r.disk.bytes)} em disco` : 'disco medindo…')
  return parts.join(' · ')
}

// agentsLine resume os agentes: vivos com seus cards, ou o último que passou.
function agentsLine(w: Worktree) {
  const live = w.sessions.filter((s) => s.live)
  if (live.length > 0) {
    const keys = [...new Set(live.flatMap((s) => (s.card ? [s.card.key] : [])))]
    return `${live.length} ${live.length === 1 ? 'agente ao vivo' : 'agentes ao vivo'}${keys.length ? ` · ${keys.join(', ')}` : ''}`
  }
  const last = w.sessions[0]
  if (last) return `último agente há ${agoText(last.ended_at ?? last.last_event_at)}`
  return 'Nenhum agente nas últimas 24 h'
}

// WorktreeCard é um link para o detalhe, só com o essencial (sem links dentro).
function WorktreeCard({ projectID, w }: { projectID: number; w: Worktree }) {
  const withP = useWithProject()
  const name = baseName(w.path)
  const live = w.sessions.some((s) => s.live)
  return (
    <Link
      to={withP(detailHref(projectID, w.path))}
      aria-label={`${name} ${w.branch || 'HEAD destacado'}`}
      className="worktree-card"
      style={{ ...panel, gap: 8, color: 'var(--text)', textDecoration: 'none', borderColor: live ? '#1F3A24' : 'var(--border)' }}
    >
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
        <span style={{ ...mono, fontSize: 15, fontWeight: 600 }} title={w.path}>{name}</span>
        <WorktreeTags w={w} />
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 14px', ...muted }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--text)' }}>
          <BranchIcon />
          {w.detached || !w.branch ? <span>HEAD destacado</span> : <span style={mono}>{w.branch}</span>}
        </span>
        {w.head && <span style={mono}>{w.head.slice(0, 7)}</span>}
      </div>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8, fontSize: 13 }}>
        {live && <svg width="8" height="8" viewBox="0 0 8 8" aria-hidden="true"><circle cx="4" cy="4" r="3.5" fill="var(--ok)" /></svg>}
        <span style={{ color: live ? 'var(--text)' : 'var(--text-2)' }}>{agentsLine(w)}</span>
        {w.cards.length > 0 && <span style={muted}>· {plural(w.cards.length, 'card no branch', 'cards no branch')}</span>}
      </div>
      {w.resources && <span style={{ ...mono, fontSize: 12, color: 'var(--text-2)' }}>{resourceLine(w.resources)}</span>}
    </Link>
  )
}

function OrphanRow({ s }: { s: WorktreeSession }) {
  return (
    <li style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderRadius: 8, background: '#0F1116' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <AgentSwatch agent={s.agent} />
        <SessionStatus s={s} />
      </div>
      <span style={{ ...mono, fontSize: 12.5 }}>{s.worktree}</span>
      {s.card ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          <CardLink c={s.card} />
          <StatusBadge column={s.card.column} />
        </div>
      ) : (
        <span style={muted}>sem card</span>
      )}
    </li>
  )
}

function ProjectSection({ p }: { p: ProjectWorktrees }) {
  const live = p.worktrees.reduce((n, w) => n + w.sessions.filter((s) => s.live).length, 0)
  const measured = p.worktrees.flatMap((w) => (w.resources?.ready ? [w.resources] : []))
  const cpu = measured.reduce((n, r) => n + machinePct(r), 0)
  const mem = p.worktrees.reduce((n, w) => n + (w.resources?.rss_bytes ?? 0), 0)
  const id = `proj-${p.project.id}`
  return (
    <section aria-labelledby={id} style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'baseline', gap: '4px 12px' }}>
        <h2 id={id} style={{ ...h2, fontSize: 17 }}>{p.project.name}</h2>
        <span style={{ ...mono, fontSize: 12, color: 'var(--text-2)' }}>{p.project.path}</span>
        {!p.error && (
          <span style={muted}>
            {plural(p.worktrees.length, 'worktree', 'worktrees')} · {live} {live === 1 ? 'agente ao vivo' : 'agentes ao vivo'}
            {measured.length > 0 && ` · ${formatPct(cpu)} da CPU · ${formatBytes(mem)} de memória`}
          </span>
        )}
      </div>
      {p.error && <p role="alert" style={{ margin: 0, fontSize: 13, color: 'var(--err)' }}>{p.error}</p>}
      {p.worktrees.length > 0 && (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 300px), 1fr))', gap: 12, alignItems: 'start' }}>
          {p.worktrees.map((w) => <WorktreeCard key={w.path} projectID={p.project.id} w={w} />)}
        </div>
      )}
      {p.orphans.length > 0 && (
        <section aria-labelledby={`${id}-orfas`} style={{ ...panel, borderColor: '#4A3B17' }}>
          <h3 id={`${id}-orfas`} style={h2}>Sessões em worktrees que não existem mais</h3>
          <p style={{ ...muted, margin: 0 }}>O diretório foi removido com a sessão ainda aberta.</p>
          <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 8 }}>
            {p.orphans.map((s) => <OrphanRow key={s.id} s={s} />)}
          </ul>
        </section>
      )}
    </section>
  )
}

export function Worktrees() {
  const { data, isPending, isError } = useWorktrees()
  const { projectId } = useProjectFilter()
  const notice = (useLocation().state as { notice?: string } | null)?.notice
  if (isError) return <p role="alert" style={{ color: 'var(--err)' }}>Não foi possível carregar os worktrees.</p>
  if (isPending || !data) return <p className="placeholder">Carregando…</p>
  const shown = data.filter((p) => projectId === null || p.project.id === projectId)
  return (
    <div style={{ maxWidth: 1180, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 28 }}>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Worktrees</h1>
        <p style={{ margin: 0, color: 'var(--text-2)' }}>
          Onde cada agente está trabalhando. Clique num worktree para ver recursos, processos, git, sessões e atividade · atualizado ao vivo
        </p>
      </header>
      {notice && <p role="status" style={{ margin: 0, fontSize: 13, color: 'var(--warn)' }}>{notice}</p>}
      {data.length === 0 && (
        <p style={{ margin: 0, color: 'var(--text-2)' }}>
          Nenhum projeto cadastrado. Cadastre com <span style={mono}>nobre project add</span> ou em <Link to="/config">Configuração</Link>.
        </p>
      )}
      {data.length > 0 && shown.length === 0 && <p style={{ margin: 0, color: 'var(--text-2)' }}>Nenhum worktree neste projeto.</p>}
      {shown.map((p) => <ProjectSection key={p.project.id} p={p} />)}
    </div>
  )
}
