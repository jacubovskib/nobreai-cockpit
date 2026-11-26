import { useEffect, useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../api/client'
import { useCard, useLimits, useProjects, useSettings } from '../api/queries'
import { ApiError } from '../api/client'
import { agentInfo } from '../ui/agents'
import { Credits, Money } from '../ui/Money'
import { StatusBadge } from '../ui/StatusBadge'
import { formatTokens } from '../ui/Tokens'
import { WorktreeTag } from '../ui/WorktreeTag'
import { cardMarks, minutesSince, plural } from './boardModel'
import { BlockedBy } from './card/BlockedBy'
import { CardTerminal } from './card/CardTerminal'
import { CliPanel } from './card/CliPanel'
import { ContinuePanel } from './card/ContinuePanel'
import { Decisions } from './card/Decisions'
import { Files } from './card/Files'
import { EpicChildren } from './card/EpicChildren'
import { Goal } from './card/Goal'
import { Hierarchy } from './card/Hierarchy'
import { LimitPanel } from './card/LimitPanel'
import { Questions } from './card/Questions'
import { ReadinessPanel } from './card/ReadinessPanel'
import { ResumePreview } from './card/ResumePreview'
import { Sessions } from './card/Sessions'
import { Plan } from './card/Plan'
import { Todos } from './card/Todos'
import { WhereStopped } from './card/WhereStopped'
import { CardContext } from './context/ContextSessions'
import { ErrorLine, btnPrimary, input, muted } from './card/shared'
import { displayName, familyIds, isGroup, useWithProject } from '../app/project'

export function CardPage() {
  const withP = useWithProject()
  const { key = '' } = useParams()
  const qc = useQueryClient()
  const card = useCard(key)
  const settings = useSettings()
  const limits = useLimits()
  const projects = useProjects()
  const [preview, setPreview] = useState(false)
  const [focusTerminal, setFocusTerminal] = useState('')
  const approve = useMutation({
    mutationFn: () => patch(`/api/cards/${key}`, { column: 'concluido' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['card', key] }),
  })

  const move = useMutation({
    mutationFn: (projectId: number) => patch(`/api/cards/${key}`, { project_id: projectId }),
    onSuccess: () => Promise.all([qc.invalidateQueries({ queryKey: ['card', key] }), qc.invalidateQueries({ queryKey: ['cards'] })]),
  })

  // Âncora na URL (ex.: #cont-t, vinda de "Continuar" no board): rola até ela quando o card carrega.
  const { hash } = useLocation()
  const loaded = !!card.data
  useEffect(() => {
    if (loaded && hash.length > 1) document.getElementById(hash.slice(1))?.scrollIntoView?.({ block: 'start' })
  }, [loaded, hash])

  if (card.isLoading) return <p className="placeholder">Carregando…</p>
  if (card.error instanceof ApiError && card.error.status === 404) {
    return (
      <section>
        <h1 style={{ fontSize: 24 }}>Card não encontrado</h1>
        <p style={muted}>Nenhum card com a chave {key}. <Link to={withP('/board')}>Voltar ao board</Link></p>
      </section>
    )
  }
  if (card.error || !card.data) return <ErrorLine error={card.error ?? new Error('Não foi possível carregar o card.')} />

  const c = card.data
  const currency = settings.data?.currency
  const rate = settings.data?.brl_rate
  const project = projects.data?.find((p) => p.id === c.project_id)
  const family = project ? familyIds(projects.data ?? [], project.parent_id ?? project.id) : []
  const agents = new Set(c.sessions.map((s) => s.agent)).size
  const lastAgent = [...c.sessions].sort((a, b) => Date.parse(b.last_event_at) - Date.parse(a.last_event_at))[0]?.agent
  const t = c.totals
  const tokens = t.input + t.output + t.cache_write + t.cache_read
  // Sessão dona cujo processo sumiu (carência antes de encerrar): não é viva.
  const stale = c.live ? undefined : c.sessions.find((s) => s.card_id === c.id && !s.ended_at && s.pid_dead_at)
  // Worktree da sessão viva ou, sem ela, da sessão mais recente do card que tinha um.
  const where = c.live?.worktree ? c.live : [...c.sessions].filter((s) => s.worktree).sort((a, b) => Date.parse(b.last_event_at) - Date.parse(a.last_event_at))[0]

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <nav aria-label="Trilha" style={muted}>
        <Link to={withP('/board')}>Board</Link> <span aria-hidden="true">/</span> <span>{c.key}</span>
      </nav>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
          <span className="mono" style={muted}>{c.key}</span>
          <StatusBadge column={c.column} live={!!c.live} />
          {cardMarks(c, c.readiness.ready).map((m) => <span key={m} className="badge">{m}</span>)}
          {c.branch_merged_at && c.column !== 'concluido' && <span className="badge">branch integrada</span>}
          {c.live && <span style={muted}>{agentInfo(c.live.agent).name} ao vivo</span>}
          {stale && <span style={{ color: 'var(--warn)' }}>{agentInfo(stale.agent).name} · processo encerrado há {minutesSince(stale.pid_dead_at)} min</span>}
        </div>
        <h1 style={{ fontSize: 26 }}>{c.title}</h1>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', ...muted }}>
          {project && family.length > 1 ? (
            <label>Projeto{' '}
              <select style={{ ...input, minHeight: 32 }} value={project.id} disabled={move.isPending} onChange={(e) => move.mutate(Number(e.target.value))}>
                {family.map((id) => {
                  const p = projects.data?.find((x) => x.id === id)
                  return p && (!isGroup(p) || p.path !== '') && <option key={id} value={id}>{displayName(p, projects.data ?? [])}</option>
                })}
              </select>
            </label>
          ) : project && <span>Repositório <span className="mono">{project.name}</span></span>}
          {c.branch && <span>Branch <span className="mono">{c.branch}</span></span>}
          {where && <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>Worktree <WorktreeTag path={where.worktree} linked={where.worktree_linked} /></span>}
          <span>{plural(c.sessions.length, 'sessão', 'sessões')} · {plural(agents, 'agente', 'agentes')}</span>
          <span>{formatTokens(tokens)} tokens</span>
          <span><Money usd={t.cost_usd} unpriced={t.unpriced} currency={currency} rate={rate} /><Credits credits={t.credits} /> no total</span>
        </div>
        <Hierarchy key={`${c.key}:${c.type}:${c.parent_key}:${c.priority}:${(c.labels ?? []).join(',')}:${c.points}`} card={c} />
        <BlockedBy key={`${c.key}:${c.blocked_by}`} card={c} />
        {c.column === 'revisao' && (
          <div>
            <button type="button" style={btnPrimary} disabled={approve.isPending} onClick={() => approve.mutate()}>
              Aprovar e concluir
            </button>
            <ErrorLine error={approve.error} />
          </div>
        )}
        <ErrorLine error={move.error} />
      </header>
      <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-start' }}>
        <div style={{ flex: '2 1 480px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          {c.type === 'epico' && <EpicChildren card={c} />}
          <CardTerminal cardKey={c.key} focusId={focusTerminal} />
          <CardContext card={c} />
          <WhereStopped card={c} />
          <Goal card={c} />
          <Todos card={c} />
          <Plan card={c} />
          <Decisions card={c} />
          <Files card={c} />
          <Sessions card={c} currency={currency} rate={rate} />
          <Questions card={c} />
        </div>
        <aside aria-label="Handoff" style={{ flex: '1 1 320px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <ContinuePanel cardKey={c.key} enabled onTerminal={setFocusTerminal} lastAgent={lastAgent} />
          <CliPanel cardKey={c.key} previewOpen={preview} onTogglePreview={() => setPreview(!preview)} />
          {c.type !== 'epico' && <ReadinessPanel readiness={c.readiness} />}
          <LimitPanel card={c} limits={limits.data ?? []} currency={currency} rate={rate} />
        </aside>
      </div>
      {preview && <ResumePreview cardKey={c.key} />}
    </div>
  )
}
