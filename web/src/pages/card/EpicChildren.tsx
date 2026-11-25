import { Link } from 'react-router-dom'
import { useProjects, useSettings } from '../../api/queries'
import type { CardDetail } from '../../api/types'
import { useWithProject } from '../../app/project'
import { AgentSwatch } from '../../ui/AgentSwatch'
import { Credits, Money } from '../../ui/Money'
import { ProjectTag } from '../../ui/ProjectTag'
import { StatusBadge } from '../../ui/StatusBadge'
import { TYPE_ICON, TYPE_LABEL } from '../boardModel'
import { NewTask } from '../NewTask'
import { h2, muted, panel, td, th, thead } from './shared'

// Filhas do épico: tabela com a última sessão e o custo, o resumo de custo e "Nova filha".
export function EpicChildren({ card }: { card: CardDetail }) {
  const withP = useWithProject()
  const projects = useProjects().data ?? []
  const settings = useSettings().data
  const kids = card.child_cards ?? []
  const own = card.totals
  const kidsT = card.children_totals
  const tree = card.tree_totals
  const m = (t: { cost_usd: number; unpriced: boolean } | null) =>
    t ? <Money usd={t.cost_usd} unpriced={t.unpriced} currency={settings?.currency} rate={settings?.brl_rate} /> : <span>—</span>
  return (
    <section aria-label="Filhas do épico" style={panel}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', justifyContent: 'space-between', gap: 10 }}>
        <h2 style={h2}>Filhas{card.children ? ` · ${card.children.done} de ${card.children.total} concluídas` : ''}</h2>
        <NewTask epic={card.key} label="Nova filha" />
      </div>
      <p style={{ margin: 0, ...muted }}>
        custo do épico {m(own)} · das filhas {m(kidsT)} · total {m(tree)}
      </p>
      {kids.length === 0 ? <p style={{ margin: 0, ...muted }}>Nenhuma filha ainda.</p> : (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
            <thead>
              <tr style={thead}>
                <th style={th}>Chave</th><th style={th}>Tipo</th><th style={th}>Título</th><th style={th}>Coluna</th>
                <th style={th}>Projeto</th><th style={th}>Último agente</th><th style={th}>Custo</th>
              </tr>
            </thead>
            <tbody>
              {kids.map((c) => (
                <tr key={c.key} style={{ borderTop: '1px solid var(--border)' }}>
                  <td className="mono" style={{ ...td, color: 'var(--text-2)' }}>{c.key}</td>
                  <td style={td}><span aria-hidden="true">{TYPE_ICON[c.type]}</span> {TYPE_LABEL[c.type]}</td>
                  <td style={td}><Link to={withP(`/cards/${c.key}`)}>{c.title}</Link></td>
                  <td style={td}><StatusBadge column={c.column} live={!!c.live} /></td>
                  <td style={td}><ProjectTag project={projects.find((p) => p.id === c.project_id) ?? null} projects={projects} /></td>
                  <td style={td}>{c.agent ? <AgentSwatch agent={c.agent} /> : '—'}</td>
                  <td style={td}><Money usd={c.totals.cost_usd} unpriced={c.totals.unpriced} /><Credits credits={c.totals.credits} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}
