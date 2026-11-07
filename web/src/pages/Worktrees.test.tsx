import { render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Worktrees } from './Worktrees'
import type { ProjectWorktrees, WorktreeSession } from '../api/types'

function mockApi(routes: Record<string, unknown>) {
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    const r = routes[url]
    if (r === undefined) return new Response('{}', { status: 404 })
    return new Response(JSON.stringify(r), { status: 200 })
  }))
}
function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={qc}><MemoryRouter><Worktrees /></MemoryRouter></QueryClientProvider>)
}
afterEach(() => vi.unstubAllGlobals())

const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()
const session = (o: Partial<WorktreeSession>): WorktreeSession => ({
  id: 'claude:x', agent: 'claude', provider_session_id: 'x', card_id: null, project_id: 1, pid: 1, cwd: '/w/api',
  branch: 'main', git_head: '', worktree: '/w/api', worktree_linked: false, model: 'claude-opus-5-5', transcript_path: '',
  full_capture: true, started_at: ago(12), last_event_at: ago(1), ended_at: null, end_reason: '', pid_dead_at: null,
  live: true, last_activity: '', card: null, ...o,
})
const wt = { head: 'a1b2c3d4e5f6a7b8c9d0a1b2c3d4e5f6a7b8c9d0', bare: false, detached: false, locked: false, prunable: false, resources: null }

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
          last_activity: 'Edit auth/oauth.go', card: { key: 'NAC-7', title: 'Login com OAuth', column: 'em_andamento' } })] },
      { ...wt, path: '/w/api-vazio', branch: '', detached: true, main: false, cards: [], sessions: [] },
    ],
    orphans: [session({ id: 'claude:orfa', worktree: '/w/api-sumiu', worktree_linked: true, pid_dead_at: ago(5), live: false })],
  },
  { project: { id: 2, name: 'solto', path: '/w/solto', capture: true }, error: 'não consegui listar os worktrees de /w/solto: não é um repositório git?', worktrees: [], orphans: [] },
]

describe('Worktrees', () => {
  it('mostra cada worktree com branch, agentes e cards', async () => {
    mockApi({ '/api/worktrees': data })
    wrap()
    const oauth = await screen.findByRole('article', { name: /api-oauth/ })
    expect(within(oauth).getByText('feat/oauth')).toBeInTheDocument()
    expect(within(oauth).getByText('worktree ligado')).toBeInTheDocument()
    expect(within(oauth).getByText('travado')).toBeInTheDocument()
    expect(within(oauth).getByText('a1b2c3d')).toBeInTheDocument()
    expect(within(oauth).getByText(/Ao vivo · 12 min/)).toBeInTheDocument()
    expect(within(oauth).getByText('Edit auth/oauth.go')).toBeInTheDocument()
    expect(within(oauth).getByRole('link', { name: /NAC-7/ })).toHaveAttribute('href', '/cards/NAC-7')
    // Card do branch sem sessão aparece à parte; o que já tem sessão não se repete.
    const extra = within(oauth).getByRole('list', { name: 'Outros cards do branch' })
    expect(within(extra).getAllByRole('link')).toHaveLength(1)
    expect(within(extra).getByRole('link', { name: /NAC-9/ })).toBeInTheDocument()

    const main = screen.getByRole('article', { name: /^api / })
    expect(within(main).getByText('principal')).toBeInTheDocument()
    expect(within(main).getByText(/encerrada há 2 h/)).toBeInTheDocument()

    const empty = screen.getByRole('article', { name: /api-vazio/ })
    expect(within(empty).getByText('HEAD destacado')).toBeInTheDocument()
    expect(within(empty).getByText('Nenhum agente aqui nas últimas 24 h.')).toBeInTheDocument()
  })

  it('resume os agentes vivos, lista sessões em worktrees removidos e erros do projeto', async () => {
    mockApi({ '/api/worktrees': data })
    wrap()
    expect(await screen.findByText(/3 worktrees · 1 agente ao vivo/)).toBeInTheDocument()
    const orphans = screen.getByRole('region', { name: /Sessões em worktrees que não existem mais/ })
    expect(within(orphans).getByText('/w/api-sumiu')).toBeInTheDocument()
    expect(within(orphans).getByText(/processo encerrado há 5 min/)).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('não é um repositório git?')
  })

  it('sem projetos explica como cadastrar', async () => {
    mockApi({ '/api/worktrees': [] })
    wrap()
    expect(await screen.findByText(/Nenhum projeto cadastrado/)).toBeInTheDocument()
  })
})

describe('Worktrees · recursos', () => {
  const res = (o: object) => ({ ready: true, cpu_pct: 0, rss_bytes: 0, procs: 0, top: [], cpu_cores: 12, disk: null, ...o })
  const withRes: ProjectWorktrees[] = [{
    project: { id: 1, name: 'api', path: '/w/api', capture: true }, error: '', orphans: [],
    worktrees: [
      { ...wt, path: '/w/api-oauth', branch: 'feat/oauth', main: false, cards: [], sessions: [],
        resources: res({ cpu_pct: 180, rss_bytes: 1288490188, procs: 7,
          top: [{ pid: 20, comm: 'node', cpu_pct: 120, rss_bytes: 943718400 }, { pid: 21, comm: 'esbuild', cpu_pct: 60, rss_bytes: 104857600 }],
          disk: { bytes: 3650722201, measured_at: ago(4), pending: false } }) },
      { ...wt, path: '/w/api', branch: 'main', main: true, cards: [], sessions: [],
        resources: res({ ready: false, disk: { bytes: 0, measured_at: null, pending: true } }) },
    ],
  }]

  it('mostra CPU, memória, processos e disco de cada worktree', async () => {
    mockApi({ '/api/worktrees': withRes })
    wrap()
    const oauth = await screen.findByRole('article', { name: /api-oauth/ })
    const stats = within(oauth).getByRole('list', { name: 'Recursos de api-oauth' })
    // 180% de um núcleo em 12 núcleos = 15% da máquina.
    expect(within(stats).getByText('15%')).toBeInTheDocument()
    expect(within(stats).getByText('1,8 núcleo')).toBeInTheDocument()
    expect(within(stats).getByText('1,2 GB')).toBeInTheDocument()
    expect(within(stats).getByText('7')).toBeInTheDocument()
    expect(within(stats).getByText('3,4 GB')).toBeInTheDocument()
    expect(within(stats).getByText('há 4 min')).toBeInTheDocument()
    const top = within(oauth).getByRole('list', { name: 'Processos que mais consomem' })
    expect(within(top).getAllByRole('listitem')).toHaveLength(2)
    expect(within(top).getByText('node')).toBeInTheDocument()
    expect(within(top).getByText(/120%/)).toBeInTheDocument()
    // Total do projeto no cabeçalho.
    expect(screen.getByText(/15% da CPU · 1,2 GB de memória/)).toBeInTheDocument()
  })

  it('antes da segunda amostra e do primeiro du mostra que está medindo', async () => {
    mockApi({ '/api/worktrees': withRes })
    wrap()
    const main = await screen.findByRole('article', { name: /^api main/ })
    const stats = within(main).getByRole('list', { name: 'Recursos de api' })
    expect(within(stats).getAllByText('medindo…')).toHaveLength(2)
    expect(within(main).queryByRole('list', { name: 'Processos que mais consomem' })).not.toBeInTheDocument()
  })

  it('sem medição de recursos não mostra a faixa', async () => {
    mockApi({ '/api/worktrees': data })
    wrap()
    const oauth = await screen.findByRole('article', { name: /api-oauth/ })
    expect(within(oauth).queryByRole('list', { name: /Recursos de/ })).not.toBeInTheDocument()
  })
})
