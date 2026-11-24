import { Link, useParams } from 'react-router-dom'
import { useMemo } from 'react'
import { useCards, useProjects, useSessions, useTotals } from '../api/queries'
import type { CardSummary, Project, SessionWithUsage } from '../api/types'
import { familyIds, useWithProject, withProject } from '../app/project'
import { Credits, Money } from '../ui/Money'
import { range } from './analytics/data'
import { ProjectTag } from '../ui/ProjectTag'
import { PlanBlocks } from '../ui/PlanBlocks'
import { signalText } from './board'
import { btn, h2, muted, panel } from './card/shared'
import { buildRows, contextLine, GROUPS, sessionProject, stateLabel, timeline } from './projects'

// Cronograma das entregas do projeto da rota (não usa o seletor da barra lateral).
export function ProjectPage() {
  const withP = useWithProject()
  const { id } = useParams()
  const projects = useProjects()
  const cards = useCards()
  const { from, to } = useMemo(() => range(7), [])
  const totals = useTotals('project', from, to)
  const sessions = useSessions(200)
  if (projects.isError || cards.isError) return <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar o projeto.</p>
  if (projects.isPending || cards.isPending) return <p className="placeholder">Carregando…</p>
  const list = projects.data ?? []
  const project = /^\d+$/.test(id ?? '') ? list.find((p) => p.id === Number(id)) : undefined
  if (!project) return <p className="placeholder">Projeto não encontrado</p>
  const family = familyIds(list, project.id)
  const parent = project.parent_id == null ? undefined : list.find((p) => p.id === project.parent_id)
  const isParent = family.length > 1
  const items = timeline((cards.data ?? []).filter((c) => c.project_id !== null && family.includes(c.project_id)))

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 900 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <Link to={withP('/projetos')} style={muted}>← Projetos</Link>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: '4px 0' }}>{project.name}</h1>
          <ProjectTag project={project} projects={list} />
          {parent && <p style={{ ...muted, margin: '6px 0 0' }}>parte de <Link to={withP(`/projetos/${parent.id}`)} style={{ color: 'inherit' }}>{parent.name}</Link></p>}
        </div>
        <Link to={withProject('/board', project.id)} style={btn}>Ver no board</Link>
      </header>
      {isParent && <ByApp project={project} list={list} cards={cards.data ?? []} totals={totals} sessions={sessions} />}
      {GROUPS.map((g) => {
        const group = items.filter((i) => i.group === g.id)
        return (
          <section key={g.id} style={panel}>
            <h2 style={h2}>{g.label}</h2>
            {group.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhuma entrega</p>}
            <ol style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
              {group.map(({ card }) => (
                <li key={card.key}>
                  <Link to={withP(`/cards/${card.key}`)} style={{ display: 'flex', flexWrap: 'wrap', gap: 10, alignItems: 'baseline', color: 'inherit', textDecoration: 'none' }}>
                    <span style={{ fontFamily: 'var(--mono, monospace)', fontSize: 12.5, color: 'var(--text-2)' }}>{card.key}</span>
                    <span>{card.title}</span>
                    {isParent && card.project_id !== project.id && <span style={{ ...muted, fontSize: 12.5 }}>{appName(card.project_id, list)}</span>}
                    <span style={muted}>{stateLabel(card)}</span>
                    {card.signal && card.signal !== 'parado' && card.signal !== 'aguardando_revisao' && <span style={muted}>{signalText(card)}</span>}
                    {contextLine(card) && <span style={muted}>{contextLine(card)}</span>}
                    {card.plan && card.plan.tasks.length > 0 && <PlanBlocks tasks={card.plan.tasks} />}
                  </Link>
                </li>
              ))}
            </ol>
          </section>
        )
      })}
    </div>
  )
}

const appName = (id: number | null, list: Project[]) => list.find((p) => p.id === id)?.name ?? ''
const cell = { padding: '8px 12px', verticalAlign: 'top' } as const
const COLS: { id: CardSummary['column']; label: string }[] = [
  { id: 'em_andamento', label: 'Em andamento' },
  { id: 'pausado', label: 'Pausado' },
  { id: 'revisao', label: 'Revisão' },
  { id: 'concluido', label: 'Concluído' },
]

// Quadro "Por app" de um pai: cada app, o "geral" (o que caiu no pai) e o total, que é a soma das linhas.
function ByApp({ project, list, cards, totals, sessions }: { project: Project; list: Project[]; cards: CardSummary[]; totals: ReturnType<typeof useTotals>; sessions: ReturnType<typeof useSessions> }) {
  if (totals.isError || sessions.isError) return <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar o quadro por app.</p>
  if (totals.isPending || sessions.isPending) return <p className="placeholder">Carregando…</p>
  const row = buildRows(list, cards, totals.data ?? [], sessions.data ?? []).find((r) => r.project?.id === project.id)
  if (!row || !row.general) return null
  const folder = (p: Project) => (project.path && p.path.startsWith(project.path.replace(/\/+$/, '') + '/') ? p.path.slice(project.path.replace(/\/+$/, '').length + 1) : p.path)
  const lines: { id: number; key: string; name: string; path: string; cost: number; credits: number; unpriced: boolean; cards: CardSummary[] }[] = [
    ...row.children.map((c) => ({ id: c.project!.id, key: String(c.project!.id), name: c.project!.name, path: folder(c.project!), cost: c.costUsd, credits: c.credits, unpriced: c.unpriced, cards: cards.filter((x) => x.project_id === c.project!.id) })),
    { id: project.id, key: 'geral', name: 'geral', path: '(raiz)', cost: row.general.costUsd, credits: row.general.credits, unpriced: row.general.unpriced, cards: cards.filter((x) => x.project_id === project.id) },
  ]
  // Sessão viva: não encerrada e com o processo existindo. As do pai caem em "geral".
  const liveOf = (id: number) => (sessions.data ?? []).filter((s: SessionWithUsage) => sessionProject(s) === id && !s.ended_at && !s.pid_dead_at).length
  return (
    <section style={{ ...panel, overflowX: 'auto' }}>
      <h2 style={h2}>Por app</h2>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
        <thead>
          <tr style={{ textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>
            <th style={cell}>app</th>
            <th style={cell}>pasta</th>
            <th style={cell}>custo no período (7 dias)</th>
            {COLS.map((c) => <th key={c.id} style={cell}>{c.label}</th>)}
            <th style={cell}>sessões vivas</th>
          </tr>
        </thead>
        <tbody>
          {lines.map((l) => (
            <tr key={l.key} style={{ borderTop: '1px solid var(--border)' }}>
              <td style={cell}>{l.name}</td>
              <td style={{ ...cell, ...muted }}>{l.path || '—'}</td>
              <td style={cell}><Money usd={l.cost} unpriced={l.unpriced && l.cost === 0} /><Credits credits={l.credits} /></td>
              {COLS.map((c) => <td key={c.id} style={cell}>{l.cards.filter((x) => x.column === c.id).length}</td>)}
              <td style={cell}>{liveOf(l.id)}</td>
            </tr>
          ))}
          <tr style={{ borderTop: '1px solid var(--border)', fontWeight: 600 }}>
            <td style={cell}>total</td>
            <td style={cell} />
            <td style={cell}><Money usd={row.costUsd} unpriced={row.unpriced && row.costUsd === 0} /><Credits credits={row.credits} /></td>
            {COLS.map((c) => <td key={c.id} style={cell}>{lines.reduce((n, l) => n + l.cards.filter((x) => x.column === c.id).length, 0)}</td>)}
            <td style={cell}>{lines.reduce((n, l) => n + liveOf(l.id), 0)}</td>
          </tr>
        </tbody>
      </table>
    </section>
  )
}
