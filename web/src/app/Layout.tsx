import type { ReactNode } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import { useHealth, useProjects, useSettings } from '../api/queries'
import type { Project } from '../api/types'
import { useProjectFilter, withProject } from './project'
import { useStream } from '../api/useStream'
import { CurrencyContext } from '../ui/money'

const svgProps = {
  width: 18, height: 18, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor',
  strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true,
}

const NAV: { to: string; label: string; end?: boolean; icon: ReactNode }[] = [
  {
    to: '/', label: 'Sala de controle', end: true,
    icon: <svg {...svgProps}><circle cx="12" cy="12" r="9" /><path d="M12 12 L16.5 7.5" /><path d="M12 3v2M21 12h-2M12 21v-2M3 12h2" /></svg>,
  },
  {
    to: '/board', label: 'Board',
    icon: <svg {...svgProps}><rect x="3" y="4" width="5" height="16" rx="1.5" /><rect x="10" y="4" width="5" height="10" rx="1.5" /><rect x="17" y="4" width="4" height="13" rx="1.5" /></svg>,
  },
  {
    to: '/cards', label: 'Cards',
    icon: <svg {...svgProps}><rect x="4" y="3" width="16" height="18" rx="2" /><path d="M8 8h8M8 12h8M8 16h5" /></svg>,
  },
  {
    to: '/projetos', label: 'Projetos',
    icon: <svg {...svgProps}><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>,
  },
  {
    to: '/worktrees', label: 'Worktrees',
    icon: <svg {...svgProps}><circle cx="6" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="8" r="2" /><path d="M6 7v10M18 10c0 4-6 3-12 7" /></svg>,
  },
  {
    to: '/terminals', label: 'Terminais',
    icon: <svg {...svgProps}><rect x="3" y="4" width="18" height="16" rx="2" /><path d="M7 9l3 3-3 3M12 15h5" /></svg>,
  },
  {
    to: '/prompts', label: 'Prompts',
    icon: <svg {...svgProps}><path d="M4 5h16v11H10l-6 4z" /><path d="M8 9h8M8 12h5" /></svg>,
  },
  {
    to: '/memorias', label: 'Memórias',
    icon: <svg {...svgProps}><path d="M6 3h12v18l-6-4-6 4z" /><path d="M9 8h6M9 11h4" /></svg>,
  },
  {
    to: '/analytics', label: 'Analytics',
    icon: <svg {...svgProps}><path d="M4 20V10M10 20V4M16 20v-7M22 20H2" /></svg>,
  },
  {
    to: '/config', label: 'Configuração',
    icon: <svg {...svgProps}><circle cx="12" cy="12" r="3" /><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1" /></svg>,
  },
]

function DaemonStatus() {
  const { data, isError, isPending } = useHealth()
  if (isPending) return <div className="daemon" aria-live="polite"><span className="daemon-status">verificando…</span></div>
  if (isError || !data?.ok) {
    return (
      <div className="daemon down" role="status">
        <div className="daemon-status">
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><circle cx="5" cy="5" r="4" fill="var(--err)" /></svg>
          nobred fora do ar
        </div>
      </div>
    )
  }
  return (
    <div className="daemon" role="status">
      <div className="daemon-status">
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><circle cx="5" cy="5" r="4" fill="var(--ok)" /></svg>
        nobred ativo
      </div>
      <span className="mono">127.0.0.1:7420 · v{data.version}</span>
    </div>
  )
}

// Primeiro select: principais e soltos, por nome. Os sub-projetos ficam no segundo select.
function rootProjects(list: Project[]): Project[] {
  const ids = new Set(list.map((p) => p.id))
  return list.filter((p) => p.parent_id == null || !ids.has(p.parent_id)).sort((a, b) => a.name.localeCompare(b.name))
}

const selectStyle = { width: '100%', minHeight: 44, padding: '0 10px', borderRadius: 8, border: '1px solid #2E333E', background: 'var(--surface)', color: 'var(--text)', font: 'inherit' } as const

export function Layout() {
  useStream()
  const settings = useSettings()
  // Toda tela abaixo mostra custo na moeda configurada (Money/useMoney).
  const projects = useProjects()
  const { root, app, setProjectId, setApp } = useProjectFilter()
  const children = root ? (projects.data ?? []).filter((p) => p.parent_id === root.id).sort((a, b) => a.name.localeCompare(b.name)) : []
  const currency = { currency: settings.data?.currency, rate: settings.data?.brl_rate }
  return (
    <CurrencyContext.Provider value={currency}>
    <div className="shell">
      <nav className="sidebar" aria-label="Principal">
        <div className="brand">
          <svg width="28" height="28" viewBox="0 0 28 28" aria-hidden="true">
            <rect x="1" y="1" width="26" height="26" rx="7" fill="#E8EAEE" />
            <path d="M8 19 L20 9" stroke="#0E1014" strokeWidth="3" strokeLinecap="round" />
            <circle cx="20" cy="9" r="2.5" fill="#0E1014" />
          </svg>
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            <span className="brand-name">NobreAI Cockpit</span>
            <span className="brand-sub">pessoal · local</span>
          </div>
        </div>
        <div style={{ padding: '0 12px 12px' }}>
          <select
            aria-label="Projeto"
            value={root?.id ?? ''}
            onChange={(e) => setProjectId(e.target.value === '' ? null : Number(e.target.value))}
            style={selectStyle}
          >
            <option value="">Todos os projetos</option>
            {rootProjects(projects.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </select>
          {children.length > 0 && (
            <select
              aria-label="App"
              value={app ?? ''}
              onChange={(e) => setApp(e.target.value === '' ? null : e.target.value === 'geral' ? 'geral' : Number(e.target.value))}
              style={{ ...selectStyle, marginTop: 8 }}
            >
              <option value="">Todos</option>
              <option value="geral">geral (raiz)</option>
              {children.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </select>
          )}
        </div>
        <div className="nav">
          {NAV.map((n) => (
            <NavLink key={n.to} to={withProject(n.to, root ? root.id : null, app)} end={n.end} className={({ isActive }) => (isActive ? 'active' : '')}>
              {n.icon}
              {n.label}
            </NavLink>
          ))}
        </div>
        <DaemonStatus />
      </nav>
      <div className="content">
        {settings.data?.demo && (
          <div className="demo-banner" role="status">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
              <path d="M12 3 L22 20 H2 Z" /><path d="M12 10v4M12 17v.5" />
            </svg>
            Dados de demonstração
          </div>
        )}
        <main className="main">
          <Outlet />
        </main>
      </div>
    </div>
    </CurrencyContext.Provider>
  )
}
