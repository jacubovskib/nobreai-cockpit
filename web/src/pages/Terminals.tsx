import { Link, useSearchParams } from 'react-router-dom'
import { isLive, useTerminals, type TerminalInfo } from '../api/terminals'
import { agentInfo } from '../ui/agents'
import { TerminalView } from '../ui/TerminalView'
import { minutesSince } from './board'

const th: React.CSSProperties = { textAlign: 'left', fontWeight: 500, fontSize: 12, color: 'var(--text-2)', padding: '6px 10px' }
const td: React.CSSProperties = { padding: '8px 10px', borderTop: '1px solid var(--border)' }

function Row({ t, selected, onSelect }: { t: TerminalInfo; selected: boolean; onSelect: () => void }) {
  const live = isLive(t)
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
      <td style={td}>{t.card_key ? <Link to={`/cards/${t.card_key}`} className="mono">{t.card_key}</Link> : <span style={{ color: 'var(--text-2)' }}>sem card</span>}</td>
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
                <th style={th} scope="col">Agente</th><th style={th} scope="col">Card</th><th style={th} scope="col">Origem</th>
                <th style={th} scope="col">Iniciado</th><th style={th} scope="col">Estado</th><th style={th} scope="col">Clientes</th>
              </tr>
            </thead>
            <tbody>
              {list.map((t) => (
                <Row key={t.id} t={t} selected={t.id === selected} onSelect={() => setParams({ id: t.id })} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {current && (
        <div style={{ height: 'max(420px, calc(100vh - 360px))' }}>
          <TerminalView key={current.id} id={current.id} readonly={!isLive(current)} />
        </div>
      )}
    </div>
  )
}
