import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useCards, useProjects } from '../api/queries'
import { AgentSwatch } from '../ui/AgentSwatch'
import { Money } from '../ui/Money'
import { StatusBadge } from '../ui/StatusBadge'
import { ProjectTag } from '../ui/ProjectTag'
import { useProjectFilter, useWithProject } from '../app/project'

export function CardsList() {
  const withP = useWithProject()
  const { data, isPending, isError } = useCards()
  const { projectId } = useProjectFilter()
  const projects = useProjects().data ?? []
  const [q, setQ] = useState('')
  const term = q.trim().toLowerCase()
  const rows = (data ?? []).filter((c) => projectId === null || c.project_id === projectId).filter(
    (c) => !term || c.key.toLowerCase().includes(term) || c.title.toLowerCase().includes(term),
  )
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, maxWidth: 1180 }}>
      <header style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-end', justifyContent: 'space-between', gap: 16 }}>
        <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Cards</h1>
        <div>
          <label htmlFor="busca-cards" style={{ position: 'absolute', width: 1, height: 1, overflow: 'hidden', clip: 'rect(0 0 0 0)' }}>
            Buscar cards
          </label>
          <input
            id="busca-cards"
            type="search"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Buscar por chave ou título…"
            style={{ minHeight: 44, minWidth: 260, padding: '0 14px', borderRadius: 8, border: '1px solid #2E333E', background: 'var(--surface)', color: 'var(--text)', font: 'inherit' }}
          />
        </div>
      </header>
      {isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os cards.</p>}
      {isPending && !isError && <p className="placeholder">Carregando…</p>}
      {data && rows.length === 0 && <p className="placeholder">Nenhum card encontrado.</p>}
      {rows.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
            <thead>
              <tr style={{ textAlign: 'left', color: 'var(--text-2)', fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.06em' }}>
                <th style={{ padding: '8px 12px' }}>Chave</th>
                <th style={{ padding: '8px 12px' }}>Título</th>
                <th style={{ padding: '8px 12px' }}>Projeto</th>
                <th style={{ padding: '8px 12px' }}>Coluna</th>
                <th style={{ padding: '8px 12px' }}>Último agente</th>
                <th style={{ padding: '8px 12px' }}>Custo</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => (
                <tr key={c.key} style={{ borderTop: '1px solid var(--border)' }}>
                  <td className="mono" style={{ padding: '10px 12px', color: 'var(--text-2)' }}>{c.key}</td>
                  <td style={{ padding: '10px 12px' }}><Link to={withP(`/cards/${c.key}`)}>{c.title}</Link></td>
                  <td style={{ padding: '10px 12px' }}><ProjectTag project={projects.find((p) => p.id === c.project_id) ?? null} projects={projects} /></td>
                  <td style={{ padding: '10px 12px' }}><StatusBadge column={c.column} live={c.live} /></td>
                  <td style={{ padding: '10px 12px' }}>{c.agent ? <AgentSwatch agent={c.agent} /> : '—'}</td>
                  <td style={{ padding: '10px 12px' }}><Money usd={c.totals.cost_usd} unpriced={c.totals.unpriced} /></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
