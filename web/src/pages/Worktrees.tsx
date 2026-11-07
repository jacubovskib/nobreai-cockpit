import { Link } from 'react-router-dom'
import { useWorktrees } from '../api/queries'
import type { CardRef, ProjectWorktrees, Worktree, WorktreeResources, WorktreeSession } from '../api/types'
import { AgentSwatch } from '../ui/AgentSwatch'
import { StatusBadge } from '../ui/StatusBadge'
import { formatBytes, formatPct } from '../ui/bytes'
import { minutesSince, plural } from './board'
import { h2, muted, panel } from './card/shared'

const tag: React.CSSProperties = { display: 'inline-flex', alignItems: 'center', minHeight: 22, padding: '0 8px', borderRadius: 6, fontSize: 12, border: '1px solid var(--border)', color: 'var(--text-2)' }
const tagWarn: React.CSSProperties = { ...tag, color: 'var(--warn)', borderColor: '#4A3B17' }
const mono: React.CSSProperties = { fontFamily: 'var(--mono)' }

// baseName é o último segmento do caminho: o nome curto do worktree.
export const baseName = (path: string) => path.split('/').filter(Boolean).pop() ?? path

// agoText: "N min" até 1 h, depois "N h".
function agoText(iso: string) {
  const min = minutesSince(iso)
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h`
}

function BranchIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true">
      <circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="8" r="2" />
      <path d="M6 7v10M18 10c0 4-6 3-12 7" />
    </svg>
  )
}

function SessionStatus({ s }: { s: WorktreeSession }) {
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

function CardLink({ c }: { c: CardRef }) {
  return (
    <Link to={`/cards/${c.key}`} style={{ display: 'inline-flex', flexWrap: 'wrap', alignItems: 'baseline', gap: 8, color: 'var(--text)', textDecoration: 'none' }}>
      <span style={{ ...mono, fontSize: 12.5, color: 'var(--text-2)' }}>{c.key}</span>
      <span style={{ fontWeight: 500 }}>{c.title}</span>
    </Link>
  )
}

function SessionRow({ s, showWorktree }: { s: WorktreeSession; showWorktree?: boolean }) {
  return (
    <li style={{ display: 'flex', flexDirection: 'column', gap: 8, padding: 12, borderRadius: 8, background: '#0F1116' }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10, fontSize: 13 }}>
          <AgentSwatch agent={s.agent} />
          {s.model && <span style={{ ...mono, fontSize: 12, color: 'var(--text-2)' }}>{s.model}</span>}
        </span>
        <SessionStatus s={s} />
      </div>
      {showWorktree && <span style={{ ...mono, fontSize: 12.5 }}>{s.worktree}</span>}
      {s.card ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          <CardLink c={s.card} />
          <StatusBadge column={s.card.column} />
        </div>
      ) : (
        <span style={muted}>sem card</span>
      )}
      {s.last_activity && <span style={{ ...mono, fontSize: 12.5, color: '#C3C8D2', overflowWrap: 'anywhere' }}>{s.last_activity}</span>}
    </li>
  )
}

const oneDec = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })

// machinePct converte % de um núcleo em % da máquina.
const machinePct = (r: WorktreeResources) => r.cpu_pct / Math.max(1, r.cpu_cores)

function Stat({ label, value, sub, title }: { label: string; value: string; sub?: string; title?: string }) {
  return (
    <li title={title} style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
      <span style={{ fontSize: 11.5, color: 'var(--text-2)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>{label}</span>
      <span style={{ fontSize: 17, fontWeight: 600, letterSpacing: '-0.01em' }}>{value}</span>
      {sub && <span style={{ fontSize: 12, color: 'var(--text-2)' }}>{sub}</span>}
    </li>
  )
}

function ResourceStrip({ name, r }: { name: string; r: WorktreeResources }) {
  const cores = r.cpu_pct / 100
  const disk = r.disk
  const diskValue = disk?.measured_at ? formatBytes(disk.bytes) : 'medindo…'
  const diskSub = disk?.measured_at ? (disk.pending ? 'atualizando…' : `há ${agoText(disk.measured_at)}`) : undefined
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, padding: 12, borderRadius: 8, background: '#0F1116' }}>
      <ul aria-label={`Recursos de ${name}`} style={{ margin: 0, padding: 0, listStyle: 'none', display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(70px, 1fr))', gap: 10 }}>
        <Stat
          label="CPU"
          value={r.ready ? formatPct(machinePct(r)) : 'medindo…'}
          sub={r.ready ? `${oneDec.format(cores)} núcleo${cores >= 2 ? 's' : ''}` : undefined}
          title={`Processos neste diretório: ${oneDec.format(cores)} de ${r.cpu_cores} núcleos`}
        />
        <Stat label="Memória" value={formatBytes(r.rss_bytes)} title="Soma da memória residente (RSS); páginas compartilhadas contam mais de uma vez" />
        <Stat label="Processos" value={String(r.procs)} title="Processos com o diretório de trabalho neste worktree, e os que eles abriram" />
        {disk && <Stat label="Disco" value={diskValue} sub={diskSub} title="Espaço ocupado pelo worktree, sem os worktrees aninhados" />}
      </ul>
      {r.ready && r.top.length > 0 && (
        <details>
          <summary style={{ cursor: 'pointer', fontSize: 12.5, color: 'var(--text-2)' }}>Processos que mais consomem (CPU: 100% = 1 núcleo)</summary>
          <ul aria-label="Processos que mais consomem" style={{ margin: '8px 0 0', padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 4 }}>
            {r.top.map((p) => (
              <li key={p.pid} style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) auto auto', gap: 12, fontSize: 12.5 }}>
                <span style={{ ...mono, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={`PID ${p.pid}`}>{p.comm}</span>
                <span style={{ ...mono, color: 'var(--text-2)' }}>{formatPct(p.cpu_pct)}</span>
                <span style={{ ...mono, color: 'var(--text-2)' }}>{formatBytes(p.rss_bytes)}</span>
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

function WorktreeCard({ w }: { w: Worktree }) {
  const name = baseName(w.path)
  const label = `${name} ${w.branch || 'HEAD destacado'}`
  const withSession = new Set(w.sessions.flatMap((s) => (s.card ? [s.card.key] : [])))
  const otherCards = w.cards.filter((c) => !withSession.has(c.key))
  const live = w.sessions.some((s) => s.live)
  return (
    <article aria-label={label} style={{ ...panel, gap: 14, borderColor: live ? '#1F3A24' : 'var(--border)' }}>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          <h3 style={{ ...mono, fontSize: 15, fontWeight: 600 }}>{name}</h3>
          {w.main ? <span style={tag}>principal</span> : <span style={tag}>worktree ligado</span>}
          {w.locked && <span style={tag}>travado</span>}
          {w.prunable && <span style={tagWarn}>diretório sumiu</span>}
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '4px 14px', ...muted }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--text)' }}>
            <BranchIcon />
            {w.detached || !w.branch ? <span>HEAD destacado</span> : <span style={mono}>{w.branch}</span>}
          </span>
          {w.head && <span style={mono} title={w.head}>{w.head.slice(0, 7)}</span>}
        </div>
        <span style={{ ...mono, fontSize: 12, color: 'var(--text-2)', overflowWrap: 'anywhere' }}>{w.path}</span>
      </header>
      {w.resources && <ResourceStrip name={name} r={w.resources} />}
      {w.sessions.length === 0 ? (
        <p style={{ ...muted, margin: 0 }}>Nenhum agente aqui nas últimas 24 h.</p>
      ) : (
        <ul aria-label={`Agentes em ${name}`} style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 8 }}>
          {w.sessions.map((s) => <SessionRow key={s.id} s={s} />)}
        </ul>
      )}
      {otherCards.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <span style={{ fontSize: 12, color: 'var(--text-2)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>
            {w.sessions.length === 0 ? 'Cards do branch' : 'Outros cards do branch'}
          </span>
          <ul aria-label="Outros cards do branch" style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 6 }}>
            {otherCards.map((c) => (
              <li key={c.key} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                <CardLink c={c} />
                <StatusBadge column={c.column} />
              </li>
            ))}
          </ul>
        </div>
      )}
    </article>
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
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(min(100%, 340px), 1fr))', gap: 12, alignItems: 'start' }}>
          {p.worktrees.map((w) => <WorktreeCard key={w.path} w={w} />)}
        </div>
      )}
      {p.orphans.length > 0 && (
        <section aria-labelledby={`${id}-orfas`} style={{ ...panel, borderColor: '#4A3B17' }}>
          <h3 id={`${id}-orfas`} style={h2}>Sessões em worktrees que não existem mais</h3>
          <p style={{ ...muted, margin: 0 }}>O diretório foi removido com a sessão ainda aberta.</p>
          <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 8 }}>
            {p.orphans.map((s) => <SessionRow key={s.id} s={s} showWorktree />)}
          </ul>
        </section>
      )}
    </section>
  )
}

export function Worktrees() {
  const { data, isPending, isError } = useWorktrees()
  if (isError) return <p role="alert" style={{ color: 'var(--err)' }}>Não foi possível carregar os worktrees.</p>
  if (isPending || !data) return <p className="placeholder">Carregando…</p>
  return (
    <div style={{ maxWidth: 1180, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 28 }}>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Worktrees</h1>
        <p style={{ margin: 0, color: 'var(--text-2)' }}>
          Onde cada agente está trabalhando: worktrees git de cada projeto, com branch, sessões das últimas 24 h e cards · atualizado ao vivo
        </p>
      </header>
      {data.length === 0 && (
        <p style={{ margin: 0, color: 'var(--text-2)' }}>
          Nenhum projeto cadastrado. Cadastre com <span style={mono}>nobre project add</span> ou em <Link to="/config">Configuração</Link>.
        </p>
      )}
      {data.map((p) => <ProjectSection key={p.project.id} p={p} />)}
    </div>
  )
}
