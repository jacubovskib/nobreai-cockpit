import { Link, useSearchParams } from 'react-router-dom'
import { ApiError } from '../api/client'
import { useWorktreeDetail } from '../api/queries'
import type { GitDetail, ProcDetail, WorktreeDetail, WorktreeResources, WorktreeSessionDetail } from '../api/types'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Money } from '../ui/Money'
import { StatusBadge } from '../ui/StatusBadge'
import { Tokens } from '../ui/Tokens'
import { formatBytes, formatPct } from '../ui/bytes'
import { plural } from './board'
import { ErrorLine, h2, muted, panel } from './card/shared'
import { BranchIcon, CardLink, SessionStatus, WorktreeTags, agoText, baseName, machinePct, mono, oneDec } from './worktreeUI'

const cell: React.CSSProperties = { padding: '7px 10px', textAlign: 'left', whiteSpace: 'nowrap' }
const num: React.CSSProperties = { ...cell, textAlign: 'right' }
const listReset: React.CSSProperties = { margin: 0, padding: 0, listStyle: 'none' }

function Section({ title, aside, children }: { title: string; aside?: React.ReactNode; children: React.ReactNode }) {
  const id = `sec-${title.toLowerCase().replace(/[^a-z]+/g, '-')}`
  return (
    <section aria-labelledby={id} style={panel}>
      <div style={{ display: 'flex', flexWrap: 'wrap', justifyContent: 'space-between', alignItems: 'baseline', gap: 8 }}>
        <h2 id={id} style={h2}>{title}</h2>
        {aside && <span style={muted}>{aside}</span>}
      </div>
      {children}
    </section>
  )
}

function Stat({ label, value, sub, title }: { label: string; value: string; sub?: string; title?: string }) {
  return (
    <div title={title} style={{ display: 'flex', flexDirection: 'column', gap: 2, padding: 12, borderRadius: 8, background: '#0F1116', minWidth: 0 }}>
      <span style={{ fontSize: 11.5, color: 'var(--text-2)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>{label}</span>
      <span style={{ fontSize: 22, fontWeight: 600, letterSpacing: '-0.01em' }}>{value}</span>
      {sub && <span style={{ fontSize: 12, color: 'var(--text-2)' }}>{sub}</span>}
    </div>
  )
}

function Resources({ r, procs }: { r: WorktreeResources; procs: ProcDetail[] }) {
  const cores = r.cpu_pct / 100
  const d = r.disk
  return (
    <Section title="Recursos" aside="processos com o diretório neste worktree e os que eles abriram · a cada 5 s">
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 10 }}>
        <Stat label="CPU" value={r.ready ? formatPct(machinePct(r)) : 'medindo…'} sub={r.ready ? `${oneDec.format(cores)} de ${r.cpu_cores} núcleos` : undefined} />
        <Stat label="Memória" value={formatBytes(r.rss_bytes)} sub="RSS somado" title="Páginas compartilhadas contam mais de uma vez" />
        <Stat label="Processos" value={String(r.procs)} />
        {d && <Stat label="Disco" value={d.measured_at ? formatBytes(d.bytes) : 'medindo…'} sub={d.measured_at ? (d.pending ? 'atualizando…' : `medido há ${agoText(d.measured_at)}`) : undefined} title="Sem os worktrees aninhados" />}
      </div>
      {procs.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <table aria-label="Processos" style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ color: 'var(--text-2)' }}>
                <th scope="col" style={cell}>Processo</th>
                <th scope="col" style={num}>PID</th>
                <th scope="col" style={num} title="100% = 1 núcleo">CPU</th>
                <th scope="col" style={num}>Memória</th>
                <th scope="col" style={cell}>Diretório</th>
              </tr>
            </thead>
            <tbody>
              {procs.map((p) => (
                <tr key={p.pid} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={{ ...cell, ...mono }}>{p.comm}</td>
                  <td style={{ ...num, ...mono, color: 'var(--text-2)' }}>{p.pid}</td>
                  <td style={{ ...num, ...mono }}>{r.ready ? formatPct(p.cpu_pct) : '—'}</td>
                  <td style={{ ...num, ...mono }}>{formatBytes(p.rss_bytes)}</td>
                  <td style={{ ...cell, ...mono, color: 'var(--text-2)', maxWidth: 360, overflow: 'hidden', textOverflow: 'ellipsis' }} title={p.cwd}>{p.cwd}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

const statusLabel: Record<string, string> = { '??': 'novo', M: 'modificado', A: 'adicionado', D: 'removido', R: 'renomeado', C: 'copiado', U: 'conflito' }
function fileLabel(code: string) {
  if (code === '??') return statusLabel['??']
  const c = code.trim()[0] ?? ''
  return statusLabel[c] ?? code
}

function Git({ g, branch }: { g: GitDetail; branch: string }) {
  const counts = [
    g.staged && plural(g.staged, 'preparado', 'preparados'),
    g.modified && plural(g.modified, 'modificado', 'modificados'),
    g.untracked && plural(g.untracked, 'novo', 'novos'),
    g.conflicts && plural(g.conflicts, 'conflito', 'conflitos'),
  ].filter(Boolean)
  return (
    <Section title="Git">
      {g.error && <p role="alert" style={{ margin: 0, fontSize: 13, color: 'var(--warn)' }}>{g.error}</p>}
      {g.compared && (
        <p style={{ margin: 0, fontSize: 14 }}>
          <span style={mono}>{branch}</span> está {g.ahead} à frente e {g.behind} atrás de <span style={mono}>{g.base}</span>
        </p>
      )}
      {!g.error && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <span style={{ fontSize: 13 }}>{counts.length ? counts.join(' · ') : 'Árvore limpa: nenhuma alteração pendente.'}</span>
          {g.files.length > 0 && (
            <ul aria-label="Arquivos alterados" style={{ ...listReset, display: 'flex', flexDirection: 'column', gap: 3 }}>
              {g.files.map((f) => (
                <li key={f.path} style={{ display: 'grid', gridTemplateColumns: '90px minmax(0, 1fr)', gap: 10, fontSize: 12.5 }}>
                  <span style={{ color: f.code === '??' ? 'var(--ok)' : 'var(--text-2)' }}>{fileLabel(f.code)}</span>
                  <span style={{ ...mono, overflowWrap: 'anywhere' }}>{f.path}</span>
                </li>
              ))}
            </ul>
          )}
          {g.total_files > g.files.length && <span style={muted}>e mais {g.total_files - g.files.length} arquivos</span>}
        </div>
      )}
      {g.commits.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          <span style={{ fontSize: 12, color: 'var(--text-2)', textTransform: 'uppercase', letterSpacing: '0.06em' }}>Últimos commits</span>
          <ol style={{ ...listReset, display: 'flex', flexDirection: 'column', gap: 4 }}>
            {g.commits.map((c) => (
              <li key={c.hash} style={{ display: 'grid', gridTemplateColumns: '64px minmax(0, 1fr) auto', gap: 10, fontSize: 13 }}>
                <span style={{ ...mono, color: 'var(--text-2)' }} title={c.hash}>{c.hash.slice(0, 7)}</span>
                <span style={{ overflowWrap: 'anywhere' }}>{c.subject}</span>
                <span style={muted}>{c.author} · há {agoText(c.at)}</span>
              </li>
            ))}
          </ol>
        </div>
      )}
    </Section>
  )
}

function duration(start: string, end: string) {
  const min = Math.max(0, Math.round((Date.parse(end) - Date.parse(start)) / 60000))
  return min < 60 ? `${min} min` : `${Math.floor(min / 60)} h ${min % 60} min`
}

function Sessions({ sessions }: { sessions: WorktreeSessionDetail[] }) {
  return (
    <Section title="Sessões" aside={plural(sessions.length, 'sessão', 'sessões')}>
      {sessions.length === 0 ? (
        <p style={{ ...muted, margin: 0 }}>Nenhum agente trabalhou aqui ainda.</p>
      ) : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ color: 'var(--text-2)' }}>
                <th scope="col" style={cell}>Agente</th>
                <th scope="col" style={cell}>Estado</th>
                <th scope="col" style={cell}>Card</th>
                <th scope="col" style={cell}>Início</th>
                <th scope="col" style={cell}>Duração</th>
                <th scope="col" style={num}>Tokens</th>
                <th scope="col" style={num}>Custo</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={s.id} style={{ borderTop: '1px solid var(--border)' }}>
                  <td style={cell}><AgentSwatch agent={s.agent} /></td>
                  <td style={{ ...cell, whiteSpace: 'normal' }}><SessionStatus s={s} /></td>
                  <td style={{ ...cell, whiteSpace: 'normal', minWidth: 140 }}>{s.card ? <CardLink c={s.card} /> : <span style={muted}>sem card</span>}</td>
                  <td style={{ ...cell, whiteSpace: 'normal' }}>{new Date(s.started_at).toLocaleString('pt-BR', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' })}</td>
                  <td style={cell}>{duration(s.started_at, s.ended_at ?? s.last_event_at)}</td>
                  <td style={num}><Tokens n={s.usage.input + s.usage.output + s.usage.cache_write + s.usage.cache_read} /></td>
                  <td style={num}><Money usd={s.usage.cost_usd} unpriced={s.unpriced} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  )
}

const pad = (n: number) => String(n).padStart(2, '0')

function Body({ d }: { d: WorktreeDetail }) {
  const w = d.worktree
  const name = baseName(w.path)
  const live = d.sessions.filter((s) => s.live)
  return (
    <div style={{ maxWidth: 1180, margin: '0 auto', display: 'flex', flexDirection: 'column', gap: 20 }}>
      <nav aria-label="Trilha" style={muted}>
        <Link to="/worktrees">Worktrees</Link> <span aria-hidden="true">/</span> <span>{d.project.name}</span> <span aria-hidden="true">/</span> <span>{name}</span>
      </nav>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 10 }}>
          <h1 style={{ ...mono, fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>{name}</h1>
          <WorktreeTags w={w} reasons />
          {live.length > 0 && <span className="badge badge-live">{plural(live.length, 'agente ao vivo', 'agentes ao vivo')}</span>}
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '6px 18px', ...muted }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: 'var(--text)' }}>
            <BranchIcon />{w.detached || !w.branch ? 'HEAD destacado' : <span style={mono}>{w.branch}</span>}
          </span>
          <span>Projeto <span style={mono}>{d.project.name}</span></span>
          {w.head && <span>HEAD <span style={mono}>{w.head}</span></span>}
        </div>
        <span style={{ ...mono, fontSize: 12.5, color: 'var(--text-2)', overflowWrap: 'anywhere' }}>{w.path}</span>
      </header>

      <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-start' }}>
        <div style={{ flex: '3 1 560px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          {w.resources && <Resources r={w.resources} procs={d.processes} />}
          <Sessions sessions={d.sessions} />
          {d.git && <Git g={d.git} branch={w.branch} />}
        </div>
        <div style={{ flex: '2 1 320px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <Section title="Cards do branch" aside={w.branch ? undefined : 'HEAD destacado'}>
            {d.cards.length === 0 ? (
              <p style={{ ...muted, margin: 0 }}>Nenhum card com este branch.</p>
            ) : (
              <ul style={{ ...listReset, display: 'flex', flexDirection: 'column', gap: 8 }}>
                {d.cards.map((c) => (
                  <li key={c.key} style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
                    <CardLink c={c} />
                    <StatusBadge column={c.column} />
                  </li>
                ))}
              </ul>
            )}
          </Section>
          <Section title="Atividade recente">
            {d.events.length === 0 ? (
              <p style={{ ...muted, margin: 0 }}>Nenhum evento registrado aqui.</p>
            ) : (
              <ol style={{ ...listReset, display: 'flex', flexDirection: 'column', gap: 10 }}>
                {d.events.map((e) => {
                  const at = new Date(e.at)
                  return (
                    <li key={e.id} style={{ display: 'grid', gridTemplateColumns: '46px 1fr', gap: 10, fontSize: 13 }}>
                      <span style={{ ...mono, color: 'var(--text-2)' }}>{pad(at.getHours())}:{pad(at.getMinutes())}</span>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 2, minWidth: 0 }}>
                        {e.agent && <span style={{ color: 'var(--text-2)' }}><AgentSwatch agent={e.agent} /></span>}
                        <span style={{ ...mono, fontSize: 12.5, overflowWrap: 'anywhere' }}>{e.text}</span>
                      </div>
                    </li>
                  )
                })}
              </ol>
            )}
          </Section>
        </div>
      </div>
    </div>
  )
}

export function WorktreeDetailPage() {
  const [params] = useSearchParams()
  const project = params.get('project') ?? ''
  const path = params.get('path') ?? ''
  const q = useWorktreeDetail(project, path)
  if (!project || !path || (q.error instanceof ApiError && (q.error.status === 404 || q.error.status === 400))) {
    return (
      <section style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <h1 style={{ fontSize: 24 }}>Worktree não encontrado</h1>
        <p style={muted}>
          {path ? <><span style={mono}>{path}</span> não está na lista de worktrees do projeto. </> : null}
          <Link to="/worktrees">Voltar aos worktrees</Link>
        </p>
      </section>
    )
  }
  if (q.isPending) return <p className="placeholder">Carregando…</p>
  if (q.error || !q.data) return <ErrorLine error={q.error ?? new Error('Não foi possível carregar o worktree.')} />
  return <Body d={q.data} />
}
