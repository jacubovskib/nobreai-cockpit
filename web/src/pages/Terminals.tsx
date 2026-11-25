import { Link, useSearchParams } from 'react-router-dom'
import { isLive, useTerminals, type TerminalInfo } from '../api/terminals'
import { useCards, useProjects } from '../api/queries'
import type { CardSummary, Project } from '../api/types'
import { projectForPath, useWithProject } from '../app/project'
import { ProjectTag } from '../ui/ProjectTag'
import { agentInfo } from '../ui/agents'
import { LazyTerminalView } from '../ui/LazyTerminalView'
import { minutesSince } from './boardModel'

const th: React.CSSProperties = { textAlign: 'left', fontWeight: 500, fontSize: 12, color: 'var(--text-2)', padding: '6px 10px' }
const td: React.CSSProperties = { padding: '8px 10px', borderTop: '1px solid var(--border)' }

function Row({ t, selected, onSelect, projects, cards }: { t: TerminalInfo; selected: boolean; onSelect: () => void; projects: Project[]; cards: CardSummary[] }) {
  const live = isLive(t)
  const withP = useWithProject()
  // Terminal com card segue o projeto do card; só sem card vale o diretório.
  const card = t.card_key ? cards.find((c) => c.key === t.card_key) : undefined
  const project = card ? projects.find((p) => p.id === card.project_id) ?? null : t.dir ? projectForPath(projects, t.dir) : null
  return (
    <tr style={{ background: selected ? '#1F232C' : undefined }}>
      <td style={td}>
        <button
          type="button"
          onClick={onSelect}
          aria-pressed={selected}
          aria-label={`Abrir terminal ${agentInfo(t.agent).name}${t.card_key ? ` do card ${t.card_key}` : ''}`}
          style={{ font: 'inherit', color: 'var(--text)', background: 'transparent', border: 0, cursor: 'pointer', padding: 0 }}
        >
          {agentInfo(t.agent).name}
        </button>
      </td>
      <td style={td}>{t.card_key ? <Link to={withP(`/cards/${t.card_key}`)} className="mono">{t.card_key}</Link> : <span style={{ color: 'var(--text-2)' }}>sem card</span>}</td>
      <td style={td}><ProjectTag project={project} projects={projects} /></td>
      <td style={td}>{t.origin}</td>
      <td style={td}>há {minutesSince(t.started_at)} min</td>
      <td style={td}>
        {live ? <span style={{ color: 'var(--ok)' }}>ao vivo</span> : <span style={{ color: 'var(--text-2)' }}>encerrado{t.exit_code != null ? ` (código ${t.exit_code})` : ''}</span>}
      </td>
      <td style={td}>{t.clients}</td>
    </tr>
  )
}

export function Terminals() {
  const { data, isPending, isError } = useTerminals()
  const projects = useProjects().data ?? []
  const cardsData = useCards().data
  const cards = Array.isArray(cardsData) ? cardsData : []
  const [params, setParams] = useSearchParams()
  const selected = params.get('id') ?? ''
  const list = [...(data ?? [])].sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at))
  const current = list.find((t) => t.id === selected)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, height: '100%' }}>
      <h1 style={{ fontSize: 26, fontWeight: 600, letterSpacing: '-0.02em' }}>Terminais</h1>
      {isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar os terminais.</p>}
      {isPending && <p className="placeholder">Carregando…</p>}
      {!isPending && !isError && list.length === 0 && (
        <p style={{ margin: 0, color: 'var(--text-2)' }}>Nenhum terminal aberto. Inicie um agente em um card, em segundo plano, para vê-lo aqui.</p>
      )}
      {list.length > 0 && (
        <div style={{ overflowX: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr>
                <th style={th} scope="col">Agente</th><th style={th} scope="col">Card</th><th style={th} scope="col">Projeto</th><th style={th} scope="col">Origem</th>
                <th style={th} scope="col">Iniciado</th><th style={th} scope="col">Estado</th><th style={th} scope="col">Clientes</th>
              </tr>
            </thead>
            <tbody>
              {list.map((t) => (
                <Row key={t.id} t={t} selected={t.id === selected} projects={projects} cards={cards} onSelect={() => setParams((prev) => { const n = new URLSearchParams(prev); n.set('id', t.id); return n })} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {!isPending && !isError && selected && !current && (
        <p role="status" style={{ margin: 0, color: 'var(--text-2)' }}>Este terminal não existe mais (encerrado e removido).</p>
      )}
      {current && (
        <div style={{ height: 'max(420px, calc(100vh - 360px))' }}>
          {/* Sessão do `nobre run` é de alguém num terminal local: abre só assistindo. */}
          <LazyTerminalView key={current.id} id={current.id} readonly={!isLive(current) || current.origin === 'nobre run'} autoFocus />
        </div>
      )}
    </div>
  )
}
