import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { patch } from '../api/client'
import { useCard, useLimits, useProjects, useSettings } from '../api/queries'
import { ApiError } from '../api/client'
import { agentInfo } from '../ui/agents'
import { Money } from '../ui/Money'
import { StatusBadge } from '../ui/StatusBadge'
import { formatTokens } from '../ui/Tokens'
import { minutesSince, plural } from './board'
import { CliPanel } from './card/CliPanel'
import { ContinuePanel } from './card/ContinuePanel'
import { Decisions } from './card/Decisions'
import { Files } from './card/Files'
import { Goal } from './card/Goal'
import { LimitPanel } from './card/LimitPanel'
import { Questions } from './card/Questions'
import { ReadinessPanel } from './card/ReadinessPanel'
import { ResumePreview } from './card/ResumePreview'
import { Sessions } from './card/Sessions'
import { Todos } from './card/Todos'
import { WhereStopped } from './card/WhereStopped'
import { ErrorLine, btnPrimary, muted } from './card/shared'

export function CardPage() {
  const { key = '' } = useParams()
  const qc = useQueryClient()
  const card = useCard(key)
  const settings = useSettings()
  const limits = useLimits()
  const projects = useProjects()
  const [preview, setPreview] = useState(false)
  const approve = useMutation({
    mutationFn: () => patch(`/api/cards/${key}`, { column: 'concluido' }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['card', key] }),
  })

  if (card.isLoading) return <p className="placeholder">Carregando…</p>
  if (card.error instanceof ApiError && card.error.status === 404) {
    return (
      <section>
        <h1 style={{ fontSize: 24 }}>Card não encontrado</h1>
        <p style={muted}>Nenhum card com a chave {key}. <Link to="/board">Voltar ao board</Link></p>
      </section>
    )
  }
  if (card.error || !card.data) return <ErrorLine error={card.error ?? new Error('Não foi possível carregar o card.')} />

  const c = card.data
  const currency = settings.data?.currency
  const rate = settings.data?.brl_rate
  const project = projects.data?.find((p) => p.id === c.project_id)
  const agents = new Set(c.sessions.map((s) => s.agent)).size
  const t = c.totals
  const tokens = t.input + t.output + t.cache_write + t.cache_read
  // Sessão dona cujo processo sumiu (carência antes de encerrar): não é viva.
  const stale = c.live ? undefined : c.sessions.find((s) => s.card_id === c.id && !s.ended_at && s.pid_dead_at)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
      <nav aria-label="Trilha" style={muted}>
        <Link to="/board">Board</Link> <span aria-hidden="true">/</span> <span>{c.key}</span>
      </nav>
      <header style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        <div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
          <span className="mono" style={muted}>{c.key}</span>
          <StatusBadge column={c.column} live={!!c.live} />
          {c.live && <span style={muted}>{agentInfo(c.live.agent).name} ao vivo</span>}
          {stale && <span style={{ color: 'var(--warn)' }}>{agentInfo(stale.agent).name} · processo encerrado há {minutesSince(stale.pid_dead_at)} min</span>}
        </div>
        <h1 style={{ fontSize: 26 }}>{c.title}</h1>
        <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', ...muted }}>
          {project && <span>Repositório <span className="mono">{project.name}</span></span>}
          {c.branch && <span>Branch <span className="mono">{c.branch}</span></span>}
          <span>{plural(c.sessions.length, 'sessão', 'sessões')} · {plural(agents, 'agente', 'agentes')}</span>
          <span>{formatTokens(tokens)} tokens</span>
          <span><Money usd={t.cost_usd} unpriced={t.unpriced} currency={currency} rate={rate} /> no total</span>
        </div>
        {c.column === 'revisao' && (
          <div>
            <button type="button" style={btnPrimary} disabled={approve.isPending} onClick={() => approve.mutate()}>
              Aprovar e concluir
            </button>
            <ErrorLine error={approve.error} />
          </div>
        )}
      </header>
      <div style={{ display: 'flex', gap: 20, flexWrap: 'wrap', alignItems: 'flex-start' }}>
        <div style={{ flex: '2 1 480px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <WhereStopped card={c} />
          <Goal card={c} />
          <Todos card={c} />
          <Decisions card={c} />
          <Files card={c} />
          <Sessions card={c} currency={currency} rate={rate} />
          <Questions card={c} />
        </div>
        <aside aria-label="Handoff" style={{ flex: '1 1 320px', minWidth: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
          <ContinuePanel cardKey={c.key} />
          <CliPanel cardKey={c.key} previewOpen={preview} onTogglePreview={() => setPreview(!preview)} />
          <ReadinessPanel readiness={c.readiness} />
          <LimitPanel card={c} limits={limits.data ?? []} currency={currency} rate={rate} />
        </aside>
      </div>
      {preview && <ResumePreview cardKey={c.key} />}
    </div>
  )
}
