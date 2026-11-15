import { Link, useParams } from 'react-router-dom'
import { useCards, useProjects } from '../api/queries'
import { useWithProject, withProject } from '../app/project'
import { ProjectTag } from '../ui/ProjectTag'
import { signalText } from './board'
import { btn, h2, muted, panel } from './card/shared'
import { GROUPS, timeline } from './projects'

// Cronograma das entregas do projeto da rota (não usa o seletor da barra lateral).
export function ProjectPage() {
  const withP = useWithProject()
  const { id } = useParams()
  const projects = useProjects()
  const cards = useCards()
  if (projects.isError || cards.isError) return <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar o projeto.</p>
  if (projects.isPending || cards.isPending) return <p className="placeholder">Carregando…</p>
  const list = projects.data ?? []
  const project = /^\d+$/.test(id ?? '') ? list.find((p) => p.id === Number(id)) : undefined
  if (!project) return <p className="placeholder">Projeto não encontrado</p>
  const items = timeline((cards.data ?? []).filter((c) => c.project_id === project.id))

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 900 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 16 }}>
        <div>
          <Link to={withP('/projetos')} style={muted}>← Projetos</Link>
          <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em', margin: '4px 0' }}>{project.name}</h1>
          <ProjectTag project={project} projects={list} />
        </div>
        <Link to={withProject('/board', project.id)} style={btn}>Ver no board</Link>
      </header>
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
                    {card.signal && <span style={muted}>{signalText(card.signal, card.signal_since)}</span>}
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
