import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Worktrees } from './Worktrees'
import type { ProjectWorktrees } from '../api/types'
import { ago, res, session, wt } from './worktreeFixtures'

function mockApi(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    if (r === undefined) return new Response('{}', { status: 404 })
    return new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap(at = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter initialEntries={[at]}><Worktrees /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const data: ProjectWorktrees[] = [
  {
    project: { id: 1, name: 'api', path: '/w/api', capture: true },
    error: '',
    worktrees: [
      { ...wt, path: '/w/api', branch: 'main', main: true, cards: [],
        sessions: [session({ id: 'claude:fim', live: false, ended_at: ago(120) })] },
      { ...wt, path: '/w/api-oauth', branch: 'feat/oauth', main: false, locked: true,
        cards: [{ key: 'NAC-7', title: 'Login com OAuth', column: 'em_andamento' }, { key: 'NAC-9', title: 'Refresh token', column: 'backlog' }],
        sessions: [session({ id: 'claude:oauth', cwd: '/w/api-oauth', worktree: '/w/api-oauth', worktree_linked: true, branch: 'feat/oauth',
          last_activity: 'Edit auth/oauth.go', card: { key: 'NAC-7', title: 'Login com OAuth', column: 'em_andamento' } })],
        resources: res({ cpu_pct: 180, rss_bytes: 1288490188, procs: 7, disk: { bytes: 3650722201, measured_at: ago(4), pending: false },
          top: [{ pid: 20, comm: 'node', cpu_pct: 120, rss_bytes: 943718400 }] }) },
      { ...wt, path: '/w/api-vazio', branch: '', detached: true, main: false, cards: [], sessions: [] },
    ],
    orphans: [session({ id: 'claude:orfa', worktree: '/w/api-sumiu', worktree_linked: true, pid_dead_at: ago(5), live: false })],
  },
  { project: { id: 2, name: 'solto', path: '/w/solto', capture: true }, error: 'não consegui listar os worktrees de /w/solto: não é um repositório git?', worktrees: [], orphans: [] },
]

describe('Worktrees · projeto', () => {
  it('com ?projeto=2 mostra só a seção do projeto 2', async () => {
    mockApi({ '/api/worktrees': data, '/api/projects': data.map((d) => d.project) })
    wrap('/worktrees?projeto=2')
    expect(await screen.findByRole('heading', { name: 'solto' })).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'api' })).toBeNull()
  })
})

describe('Worktrees', () => {
  it('cada cartão é um link enxuto para o detalhe', async () => {
    mockApi({ '/api/worktrees': data })
    wrap()
    const oauth = await screen.findByRole('link', { name: /api-oauth/ })
    expect(oauth).toHaveAttribute('href', '/worktrees/detalhe?project=1&path=%2Fw%2Fapi-oauth')
    expect(within(oauth).getByText('feat/oauth')).toBeInTheDocument()
    expect(within(oauth).getByText('worktree ligado')).toBeInTheDocument()
    expect(within(oauth).getByText('travado')).toBeInTheDocument()
    expect(within(oauth).getByText('a1b2c3d')).toBeInTheDocument()
    expect(within(oauth).getByText(/1 agente ao vivo · NAC-7/)).toBeInTheDocument()
    expect(within(oauth).getByText(/2 cards no branch/)).toBeInTheDocument()
    // Recursos numa linha só; detalhes (processos, atividade) ficam na página.
    expect(within(oauth).getByText('CPU 15% · 1,2 GB · 3,4 GB em disco')).toBeInTheDocument()
    expect(within(oauth).queryByText('Edit auth/oauth.go')).not.toBeInTheDocument()
    expect(within(oauth).queryByText('node')).not.toBeInTheDocument()
    expect(within(oauth).queryByRole('link')).not.toBeInTheDocument()

    const main = screen.getByRole('link', { name: /^api main/ })
    expect(within(main).getByText('principal')).toBeInTheDocument()
    expect(within(main).getByText(/último agente há 2 h/)).toBeInTheDocument()

    const empty = screen.getByRole('link', { name: /api-vazio/ })
    expect(within(empty).getByText('HEAD destacado')).toBeInTheDocument()
    expect(within(empty).getByText('Nenhum agente nas últimas 24 h')).toBeInTheDocument()
  })

  it('resume os agentes vivos, lista sessões em worktrees removidos e erros do projeto', async () => {
    mockApi({ '/api/worktrees': data })
    wrap()
    expect(await screen.findByText(/3 worktrees · 1 agente ao vivo · 15% da CPU · 1,2 GB de memória/)).toBeInTheDocument()
    const orphans = screen.getByRole('region', { name: /Sessões em worktrees que não existem mais/ })
    expect(within(orphans).getByText('/w/api-sumiu')).toBeInTheDocument()
    expect(within(orphans).getByText(/processo encerrado há 5 min/)).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('não é um repositório git?')
  })

  it('antes da segunda amostra mostra que a CPU está sendo medida', async () => {
    const warming: ProjectWorktrees[] = [{ ...data[0], orphans: [], worktrees: [{ ...data[0].worktrees[0], resources: res({ ready: false, rss_bytes: 1048576, disk: { bytes: 0, measured_at: null, pending: true } }) }] }]
    mockApi({ '/api/worktrees': warming })
    wrap()
    const main = await screen.findByRole('link', { name: /^api main/ })
    expect(within(main).getByText('CPU medindo… · 1 MB · disco medindo…')).toBeInTheDocument()
  })

  it('sem projetos explica como cadastrar', async () => {
    mockApi({ '/api/worktrees': [] })
    wrap()
    expect(await screen.findByText(/Nenhum projeto cadastrado/)).toBeInTheDocument()
  })
})
