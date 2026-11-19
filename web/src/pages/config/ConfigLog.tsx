import { useQuery } from '@tanstack/react-query'
import { get } from '../../api/client'
import { useProjects } from '../../api/queries'
import type { ConfigLogEntry, Project } from '../../api/types'
import { displayName } from '../../app/project'
import { muted, panel, h2 } from '../card/shared'

const str = (v: unknown) => (typeof v === 'string' ? v : '')

// Itens da integração: uma chave só ("key") ou várias ("keys").
function items(d: Record<string, unknown>): string {
  const keys = Array.isArray(d.keys) ? d.keys.map(str).filter(Boolean) : []
  if (keys.length > 0) return keys.join(', ')
  return str(d.key) || 'integração'
}

export function logPhrase(e: ConfigLogEntry, projects: Project[]): string {
  const d = e.data ?? {}
  switch (e.kind) {
    case 'setting_changed':
      return `Opção ${str(d.key)} alterada`
    case 'project_capture_changed': {
      const p = projects.find((x) => x.id === d.project_id)
      return `Captura do projeto ${p ? displayName(p, projects) : str(d.project)} ${d.capture ? 'ligada' : 'desligada'}`
    }
    case 'integration_broken':
      return `${items(d)}: problema detectado`
    case 'integration_fixed':
      return `${items(d)} corrigido por você`
    case 'integration_ignored':
      return `${items(d)}: alerta ignorado`
    case 'integration_removed':
      return `${items(d)} removido por você`
    case 'rtk_on':
      return 'RTK ativado no Claude Code'
    case 'rtk_off':
      return 'RTK desativado'
    case 'agent_notified':
      return `Sessão ${str(d.session).replace(/^[^:]+:/, '').slice(0, 8)} avisada`
    default:
      return e.kind
  }
}

const hhmm = (at: string) => new Date(at).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' })

export function ConfigLog() {
  const log = useQuery({ queryKey: ['config-log'], queryFn: () => get<ConfigLogEntry[]>('/api/config-log?limit=50') })
  const projects = useProjects().data ?? []
  const rows = log.data ?? []
  return (
    <section id="historico" aria-labelledby="historico-t" style={panel}>
      <h2 id="historico-t" style={h2}>Histórico de mudanças</h2>
      {log.isError && <p role="alert" style={{ margin: 0, color: 'var(--err)' }}>Não foi possível carregar o histórico.</p>}
      {log.data && rows.length === 0 && <p style={{ ...muted, margin: 0 }}>Nenhuma mudança registrada.</p>}
      {rows.length > 0 && (
        <ul style={{ margin: 0, padding: 0, listStyle: 'none', display: 'flex', flexDirection: 'column', gap: 6, fontSize: 13 }}>
          {rows.map((e) => (
            <li key={e.id}><span className="mono" style={muted}>{hhmm(e.at)}</span> · {logPhrase(e, projects)}</li>
          ))}
        </ul>
      )}
    </section>
  )
}
